package backups

import (
	"context"
	"errors"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/metadata"
)

var (
	ErrBackupFailed        = errors.New("backup operation failed")
	ErrBackupNotReady      = errors.New("backup is not ready")
	ErrDatabaseAttached    = errors.New("database is attached")
	ErrDatabaseUnavailable = errors.New("database is not available for backup or restore")
	ErrDeletionFailed      = errors.New("database deletion failed")
	ErrRestoreFailed       = errors.New("restore operation failed")
	ErrRetentionFailed     = errors.New("backup retention failed")
)

type metadataStore interface {
	GetDatabase(context.Context, string) (metadata.Database, error)
	ListDatabases(context.Context) ([]metadata.Database, error)
	CreateBackup(context.Context, string, metadata.BackupKind) (metadata.Backup, error)
	GetBackup(context.Context, string) (metadata.Backup, error)
	ListBackups(context.Context) ([]metadata.Backup, error)
	ListBackupsForDatabase(context.Context, string) ([]metadata.Backup, error)
	ListAttachmentsForDatabase(context.Context, string) ([]metadata.Attachment, error)
	UpdateDatabaseStatus(context.Context, string, metadata.DatabaseStatus) (metadata.Database, error)
	UpdateBackupReady(context.Context, string, int64) (metadata.Backup, error)
	UpdateBackupError(context.Context, string) (metadata.Backup, error)
	DeleteBackupMetadata(context.Context, string) error
	DeleteDatabaseMetadata(context.Context, string) error
}

type archiveStore interface {
	Create(string, string, func(io.Writer) error, func(io.Reader) error) (int64, error)
	Open(string, string) (*ArchiveReader, error)
	Delete(string, string) error
	DeleteDatabase(string) error
	RemovePartial(string, string) error
}

type provisioner interface {
	ProvisionDatabaseForRestore(context.Context, string) (metadata.Database, error)
	FinalizeRestoredDatabase(context.Context, metadata.Database) (metadata.Database, error)
	MarkDatabaseRestoring(context.Context, string) (metadata.Database, error)
	MarkDatabaseError(context.Context, string) error
	CleanupRestoreTarget(metadata.Database) error
	DeleteDatabaseResources(context.Context, metadata.Database) error
}

type Service struct {
	metadata    metadataStore
	archives    archiveStore
	postgres    Postgres
	provisioner provisioner
	now         func() time.Time
	operations  sync.Mutex
}

type AutomaticRunResult struct {
	DatabasesChecked int
	BackupsCreated   int
	BackupsPruned    int
}

func NewService(metadataStore metadataStore, archives archiveStore, postgres Postgres, provisioner provisioner) *Service {
	return &Service{
		metadata:    metadataStore,
		archives:    archives,
		postgres:    postgres,
		provisioner: provisioner,
		now:         time.Now,
	}
}

func (s *Service) ListBackups(ctx context.Context) ([]metadata.Backup, error) {
	return s.metadata.ListBackups(ctx)
}

func (s *Service) ListBackupsForDatabase(ctx context.Context, databaseID string) ([]metadata.Backup, error) {
	return s.metadata.ListBackupsForDatabase(ctx, databaseID)
}

func (s *Service) GetBackup(ctx context.Context, backupID string) (metadata.Backup, error) {
	return s.metadata.GetBackup(ctx, backupID)
}

func (s *Service) DeleteDatabase(ctx context.Context, databaseID string) error {
	s.operations.Lock()
	defer s.operations.Unlock()

	if !ids.ValidDatabaseID(databaseID) {
		return metadata.ErrInvalidIdentifier
	}
	database, err := s.metadata.GetDatabase(ctx, databaseID)
	if err != nil {
		return err
	}
	attachments, err := s.metadata.ListAttachmentsForDatabase(ctx, database.ID)
	if err != nil {
		return ErrDeletionFailed
	}
	if len(attachments) != 0 {
		return ErrDatabaseAttached
	}

	switch database.Status {
	case metadata.StatusReady:
		database, err = s.metadata.UpdateDatabaseStatus(ctx, database.ID, metadata.StatusError)
		if err != nil {
			return ErrDeletionFailed
		}
		attachments, err = s.metadata.ListAttachmentsForDatabase(ctx, database.ID)
		if err != nil {
			return ErrDeletionFailed
		}
		if len(attachments) != 0 {
			if _, restoreErr := s.metadata.UpdateDatabaseStatus(ctx, database.ID, metadata.StatusReady); restoreErr != nil {
				return ErrDeletionFailed
			}
			return ErrDatabaseAttached
		}
	case metadata.StatusError:
		// An interrupted deletion remains fail-closed and can resume safely.
	default:
		return ErrDatabaseUnavailable
	}

	backupRecords, err := s.metadata.ListBackupsForDatabase(ctx, database.ID)
	if err != nil {
		return ErrDeletionFailed
	}
	if s.provisioner == nil {
		return ErrDeletionFailed
	}
	if err := s.provisioner.DeleteDatabaseResources(ctx, database); err != nil {
		return ErrDeletionFailed
	}
	if err := s.archives.DeleteDatabase(database.ID); err != nil {
		return ErrDeletionFailed
	}
	for _, backup := range backupRecords {
		if err := s.metadata.DeleteBackupMetadata(ctx, backup.ID); err != nil &&
			!errors.Is(err, metadata.ErrNotFound) {

			return ErrDeletionFailed
		}
	}
	if err := s.metadata.DeleteDatabaseMetadata(ctx, database.ID); err != nil {
		return ErrDeletionFailed
	}
	return nil
}

func (s *Service) CreateBackup(ctx context.Context, databaseID string) (metadata.Backup, error) {
	s.operations.Lock()
	defer s.operations.Unlock()
	return s.createBackup(ctx, databaseID, metadata.BackupKindManual)
}

func (s *Service) createBackup(ctx context.Context, databaseID string, kind metadata.BackupKind) (metadata.Backup, error) {
	database, err := s.metadata.GetDatabase(ctx, databaseID)
	if err != nil {
		return metadata.Backup{}, err
	}
	if !usableDatabase(database) {
		return metadata.Backup{}, ErrDatabaseUnavailable
	}

	backup, err := s.metadata.CreateBackup(ctx, database.ID, kind)
	if err != nil {
		return metadata.Backup{}, err
	}
	sizeBytes, archiveErr := s.archives.Create(
		database.ID,
		backup.ID,
		func(output io.Writer) error {
			return s.postgres.Dump(ctx, database.InternalName, output)
		},
		func(archive io.Reader) error {
			return s.postgres.VerifyArchive(ctx, archive)
		},
	)
	if archiveErr != nil {
		_ = s.archives.RemovePartial(database.ID, backup.ID)
		_, _ = s.metadata.UpdateBackupError(ctx, backup.ID)
		return metadata.Backup{}, ErrBackupFailed
	}

	ready, err := s.metadata.UpdateBackupReady(ctx, backup.ID, sizeBytes)
	if err != nil {
		_ = s.archives.Delete(database.ID, backup.ID)
		_, _ = s.metadata.UpdateBackupError(ctx, backup.ID)
		return metadata.Backup{}, ErrBackupFailed
	}
	return ready, nil
}

func (s *Service) VerifyBackup(ctx context.Context, backupID string) (metadata.Backup, error) {
	backup, err := s.metadata.GetBackup(ctx, backupID)
	if err != nil {
		return metadata.Backup{}, err
	}
	if backup.Status != metadata.BackupStatusReady || backup.SizeBytes <= 0 {
		return metadata.Backup{}, ErrBackupNotReady
	}
	archive, err := s.archives.Open(backup.DatabaseID, backup.ID)
	if err != nil {
		return metadata.Backup{}, ErrBackupFailed
	}
	defer archive.Close()
	if archive.SizeBytes != backup.SizeBytes {
		return metadata.Backup{}, ErrBackupFailed
	}
	if err := s.postgres.VerifyArchive(ctx, archive); err != nil {
		return metadata.Backup{}, ErrBackupFailed
	}
	return backup, nil
}

func (s *Service) RestoreAsNewDatabase(ctx context.Context, backupID, displayName string) (metadata.Database, error) {
	s.operations.Lock()
	defer s.operations.Unlock()

	backup, err := s.VerifyBackup(ctx, backupID)
	if err != nil {
		return metadata.Database{}, err
	}
	target, err := s.provisioner.ProvisionDatabaseForRestore(ctx, displayName)
	if err != nil {
		return metadata.Database{}, err
	}
	cleanup := func() {
		_ = s.provisioner.CleanupRestoreTarget(target)
	}

	archive, err := s.archives.Open(backup.DatabaseID, backup.ID)
	if err != nil {
		cleanup()
		return metadata.Database{}, ErrRestoreFailed
	}
	if archive.SizeBytes != backup.SizeBytes {
		archive.Close()
		cleanup()
		return metadata.Database{}, ErrRestoreFailed
	}
	restoreErr := s.postgres.Restore(ctx, target.InternalName, target.RoleName, archive)
	closeErr := archive.Close()
	if restoreErr != nil || closeErr != nil {
		cleanup()
		return metadata.Database{}, ErrRestoreFailed
	}
	ready, err := s.provisioner.FinalizeRestoredDatabase(ctx, target)
	if err != nil {
		cleanup()
		return metadata.Database{}, ErrRestoreFailed
	}
	return ready, nil
}

func (s *Service) RestoreInPlace(ctx context.Context, backupID, targetDatabaseID string) (metadata.Database, error) {
	s.operations.Lock()
	defer s.operations.Unlock()

	backup, err := s.VerifyBackup(ctx, backupID)
	if err != nil {
		return metadata.Database{}, err
	}
	target, err := s.metadata.GetDatabase(ctx, targetDatabaseID)
	if err != nil {
		return metadata.Database{}, err
	}
	if !usableDatabase(target) {
		return metadata.Database{}, ErrDatabaseUnavailable
	}

	safetyBackup, err := s.createBackup(ctx, target.ID, metadata.BackupKindPreRestore)
	if err != nil || safetyBackup.Status != metadata.BackupStatusReady {
		return metadata.Database{}, ErrBackupFailed
	}
	restoring, err := s.provisioner.MarkDatabaseRestoring(ctx, target.ID)
	if err != nil {
		return metadata.Database{}, ErrRestoreFailed
	}
	fail := func() {
		_ = s.provisioner.MarkDatabaseError(context.Background(), target.ID)
	}

	if err := s.postgres.ResetDatabase(ctx, restoring.InternalName, restoring.RoleName); err != nil {
		fail()
		return metadata.Database{}, ErrRestoreFailed
	}
	archive, err := s.archives.Open(backup.DatabaseID, backup.ID)
	if err != nil {
		fail()
		return metadata.Database{}, ErrRestoreFailed
	}
	if archive.SizeBytes != backup.SizeBytes {
		archive.Close()
		fail()
		return metadata.Database{}, ErrRestoreFailed
	}
	restoreErr := s.postgres.Restore(ctx, restoring.InternalName, restoring.RoleName, archive)
	closeErr := archive.Close()
	if restoreErr != nil || closeErr != nil {
		fail()
		return metadata.Database{}, ErrRestoreFailed
	}
	ready, err := s.provisioner.FinalizeRestoredDatabase(ctx, restoring)
	if err != nil {
		fail()
		return metadata.Database{}, ErrRestoreFailed
	}
	return ready, nil
}

func AutomaticBackupDue(backups []metadata.Backup, now time.Time) bool {
	today := now.UTC().Format("2006-01-02")
	for _, backup := range backups {
		if backup.Kind == metadata.BackupKindAutomatic &&
			backup.CreatedAt.UTC().Format("2006-01-02") == today {
			return false
		}
	}
	return true
}

func (s *Service) ApplyRetention(ctx context.Context, databaseID string) (int, error) {
	s.operations.Lock()
	defer s.operations.Unlock()
	return s.applyRetention(ctx, databaseID)
}

func (s *Service) applyRetention(ctx context.Context, databaseID string) (int, error) {
	backups, err := s.metadata.ListBackupsForDatabase(ctx, databaseID)
	if err != nil {
		return 0, err
	}
	remove := automaticBackupsToRemove(backups)
	deleted := 0
	for _, backup := range remove {
		if err := s.archives.Delete(backup.DatabaseID, backup.ID); err != nil {
			return deleted, ErrRetentionFailed
		}
		if err := s.metadata.DeleteBackupMetadata(ctx, backup.ID); err != nil {
			return deleted, ErrRetentionFailed
		}
		deleted++
	}
	return deleted, nil
}

func (s *Service) RunDueAutomaticBackups(ctx context.Context) (AutomaticRunResult, error) {
	s.operations.Lock()
	defer s.operations.Unlock()

	databases, err := s.metadata.ListDatabases(ctx)
	if err != nil {
		return AutomaticRunResult{}, err
	}
	result := AutomaticRunResult{}
	var runErrors []error
	for _, database := range databases {
		if !usableDatabase(database) {
			continue
		}
		result.DatabasesChecked++
		existing, err := s.metadata.ListBackupsForDatabase(ctx, database.ID)
		if err != nil {
			runErrors = append(runErrors, ErrBackupFailed)
			continue
		}
		if AutomaticBackupDue(existing, s.now()) {
			if _, err := s.createBackup(ctx, database.ID, metadata.BackupKindAutomatic); err != nil {
				runErrors = append(runErrors, ErrBackupFailed)
				continue
			}
			result.BackupsCreated++
		}
		pruned, err := s.applyRetention(ctx, database.ID)
		if err != nil {
			runErrors = append(runErrors, err)
			continue
		}
		result.BackupsPruned += pruned
	}
	return result, errors.Join(runErrors...)
}

func automaticBackupsToRemove(backups []metadata.Backup) []metadata.Backup {
	automatic := make([]metadata.Backup, 0)
	for _, backup := range backups {
		if backup.Kind == metadata.BackupKindAutomatic &&
			backup.Status == metadata.BackupStatusReady &&
			ids.ValidDatabaseID(backup.DatabaseID) &&
			ids.ValidBackupID(backup.ID) {
			automatic = append(automatic, backup)
		}
	}
	sort.Slice(automatic, func(i, j int) bool {
		if automatic[i].CreatedAt.Equal(automatic[j].CreatedAt) {
			return automatic[i].ID > automatic[j].ID
		}
		return automatic[i].CreatedAt.After(automatic[j].CreatedAt)
	})

	keptDays := make(map[string]struct{})
	keptWeeks := make(map[string]struct{})
	remove := make([]metadata.Backup, 0)
	for _, backup := range automatic {
		day := backup.CreatedAt.UTC().Format("2006-01-02")
		if _, exists := keptDays[day]; exists {
			remove = append(remove, backup)
			continue
		}
		if len(keptDays) < 7 {
			keptDays[day] = struct{}{}
			continue
		}
		year, week := backup.CreatedAt.UTC().ISOWeek()
		weekKey := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC).Format("2006") + "-" + twoDigits(week)
		if len(keptWeeks) < 4 {
			if _, exists := keptWeeks[weekKey]; !exists {
				keptWeeks[weekKey] = struct{}{}
				continue
			}
		}
		remove = append(remove, backup)
	}
	return remove
}

func twoDigits(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}

func usableDatabase(database metadata.Database) bool {
	return ids.ValidDatabaseID(database.ID) &&
		ids.ValidDatabaseInternalName(database.InternalName) &&
		ids.ValidRoleInternalName(database.RoleName) &&
		database.Status == metadata.StatusReady
}
