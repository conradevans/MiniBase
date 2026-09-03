package backups

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/conradevans/MiniBase/internal/metadata"
)

const (
	serviceDatabaseID = "database_11111111111111111111111111111111"
	testDatabaseName  = "mb_db_22222222222222222222222222222222"
	testRoleName      = "mb_role_33333333333333333333333333333333"
	serviceBackupID   = "backup_44444444444444444444444444444444"
)

func TestCreateBackupWorkflowAndFailures(t *testing.T) {
	tests := []struct {
		name            string
		configure       func(*serviceFixture)
		wantErr         error
		wantStatus      metadata.BackupStatus
		wantArchive     bool
		wantPartialGone bool
	}{
		{name: "success", wantStatus: metadata.BackupStatusReady, wantArchive: true},
		{name: "dump failure", configure: func(f *serviceFixture) { f.postgres.dumpErr = errors.New("dump") }, wantErr: ErrBackupFailed, wantStatus: metadata.BackupStatusError, wantPartialGone: true},
		{name: "verify failure", configure: func(f *serviceFixture) { f.postgres.verifyErr = errors.New("verify") }, wantErr: ErrBackupFailed, wantStatus: metadata.BackupStatusError, wantPartialGone: true},
		{name: "ready metadata failure", configure: func(f *serviceFixture) { f.metadata.readyErr = errors.New("metadata") }, wantErr: ErrBackupFailed, wantStatus: metadata.BackupStatusError, wantPartialGone: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newBackupServiceFixture()
			if test.configure != nil {
				test.configure(&fixture)
			}
			backup, err := fixture.service.CreateBackup(context.Background(), serviceDatabaseID)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("CreateBackup() error = %v, want %v", err, test.wantErr)
			}
			stored := fixture.metadata.backups[serviceBackupID]
			if stored.Status != test.wantStatus {
				t.Fatalf("stored status = %q, want %q", stored.Status, test.wantStatus)
			}
			_, archiveExists := fixture.archives.files[archiveKey(serviceDatabaseID, serviceBackupID)]
			if archiveExists != test.wantArchive {
				t.Fatalf("archive exists = %v, want %v", archiveExists, test.wantArchive)
			}
			if test.wantErr == nil {
				if backup.SizeBytes == 0 || fixture.postgres.dumpDatabase != testDatabaseName || fixture.postgres.verifyCalls != 1 {
					t.Fatalf("backup=%#v dumpDatabase=%q verifyCalls=%d", backup, fixture.postgres.dumpDatabase, fixture.postgres.verifyCalls)
				}
			}
			if test.wantPartialGone && fixture.archives.partial {
				t.Fatal("partial archive remained after failure")
			}
		})
	}
}

func TestCreateBackupRejectsUnavailableDatabaseBeforeMetadata(t *testing.T) {
	fixture := newBackupServiceFixture()
	database := fixture.metadata.databases[serviceDatabaseID]
	database.Status = metadata.StatusError
	fixture.metadata.databases[serviceDatabaseID] = database
	if _, err := fixture.service.CreateBackup(context.Background(), serviceDatabaseID); !errors.Is(err, ErrDatabaseUnavailable) {
		t.Fatalf("CreateBackup() error = %v, want ErrDatabaseUnavailable", err)
	}
	if len(fixture.metadata.backups) != 0 || fixture.postgres.dumpDatabase != "" {
		t.Fatal("unavailable database caused side effects")
	}
}

func TestVerifyBackupRequiresReadyMatchingValidArchive(t *testing.T) {
	fixture := newBackupServiceFixture()
	backup, err := fixture.service.CreateBackup(context.Background(), serviceDatabaseID)
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if _, err := fixture.service.VerifyBackup(context.Background(), backup.ID); err != nil {
		t.Fatalf("VerifyBackup() error = %v", err)
	}
	stored := fixture.metadata.backups[backup.ID]
	stored.SizeBytes++
	fixture.metadata.backups[backup.ID] = stored
	if _, err := fixture.service.VerifyBackup(context.Background(), backup.ID); !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("VerifyBackup(size mismatch) error = %v", err)
	}
}

func TestAutomaticBackupDueUsesOneUTCLogicalDay(t *testing.T) {
	now := time.Date(2026, 9, 1, 0, 15, 0, 0, time.FixedZone("local", -4*60*60))
	yesterday := metadata.Backup{Kind: metadata.BackupKindAutomatic, CreatedAt: now.UTC().Add(-24 * time.Hour)}
	todayError := metadata.Backup{Kind: metadata.BackupKindAutomatic, Status: metadata.BackupStatusError, CreatedAt: now.UTC()}
	manualToday := metadata.Backup{Kind: metadata.BackupKindManual, CreatedAt: now.UTC()}
	if !AutomaticBackupDue([]metadata.Backup{yesterday, manualToday}, now) {
		t.Fatal("automatic backup should be due")
	}
	if AutomaticBackupDue([]metadata.Backup{todayError}, now) {
		t.Fatal("same UTC day automatic attempt should prevent a duplicate")
	}
}

func TestRetentionKeepsSevenDailyFourWeeklyAndPreservesOtherKinds(t *testing.T) {
	fixture := newBackupServiceFixture()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 7; day++ {
		fixture.addReadyBackup(metadata.BackupKindAutomatic, base.AddDate(0, 0, -day))
	}
	for week := 1; week <= 11; week++ {
		fixture.addReadyBackup(metadata.BackupKindAutomatic, base.AddDate(0, 0, -(7*week)))
	}
	manual := fixture.addReadyBackup(metadata.BackupKindManual, base.AddDate(0, 0, -400))
	preRestore := fixture.addReadyBackup(metadata.BackupKindPreRestore, base.AddDate(0, 0, -500))
	fixture.archives.unrelated = true

	deleted, err := fixture.service.ApplyRetention(context.Background(), serviceDatabaseID)
	if err != nil {
		t.Fatalf("ApplyRetention() error = %v", err)
	}
	if deleted != 7 {
		t.Fatalf("deleted = %d, want 7", deleted)
	}
	remaining, _ := fixture.metadata.ListBackupsForDatabase(context.Background(), serviceDatabaseID)
	automatic := 0
	for _, backup := range remaining {
		if backup.Kind == metadata.BackupKindAutomatic {
			automatic++
		}
	}
	if automatic != 11 {
		t.Fatalf("automatic backups remaining = %d, want 11", automatic)
	}
	if _, ok := fixture.metadata.backups[manual.ID]; !ok {
		t.Fatal("manual backup was pruned")
	}
	if _, ok := fixture.metadata.backups[preRestore.ID]; !ok {
		t.Fatal("pre-restore backup was pruned")
	}
	if !fixture.archives.unrelated {
		t.Fatal("retention removed unrelated file")
	}
}

func TestRetentionPreservesMetadataWhenArchiveDeletionFails(t *testing.T) {
	fixture := newBackupServiceFixture()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for day := 0; day < 12; day++ {
		fixture.addReadyBackup(metadata.BackupKindAutomatic, base.AddDate(0, 0, -day))
	}
	fixture.archives.deleteErr = errors.New("delete")
	before := len(fixture.metadata.backups)
	if _, err := fixture.service.ApplyRetention(context.Background(), serviceDatabaseID); !errors.Is(err, ErrRetentionFailed) {
		t.Fatalf("ApplyRetention() error = %v, want ErrRetentionFailed", err)
	}
	if len(fixture.metadata.backups) != before {
		t.Fatal("metadata removed after archive cleanup failure")
	}
}

func TestRestoreAsNewLeavesSourceUntouchedAndCleansFailedTarget(t *testing.T) {
	fixture := newBackupServiceFixture()
	backup, err := fixture.service.CreateBackup(context.Background(), serviceDatabaseID)
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	sourceBefore := fixture.metadata.databases[serviceDatabaseID]
	restored, err := fixture.service.RestoreAsNewDatabase(context.Background(), backup.ID, "Recovered")
	if err != nil {
		t.Fatalf("RestoreAsNewDatabase() error = %v", err)
	}
	if restored.ID == sourceBefore.ID || restored.Status != metadata.StatusReady {
		t.Fatalf("restored database = %#v", restored)
	}
	if fixture.metadata.databases[serviceDatabaseID] != sourceBefore {
		t.Fatal("source database metadata changed")
	}
	if fixture.postgres.restoredDatabase != restored.InternalName || fixture.postgres.restoredRole != restored.RoleName {
		t.Fatalf("restore target = %q/%q", fixture.postgres.restoredDatabase, fixture.postgres.restoredRole)
	}

	failed := newBackupServiceFixture()
	backup, _ = failed.service.CreateBackup(context.Background(), serviceDatabaseID)
	failed.postgres.restoreErr = errors.New("restore")
	if _, err := failed.service.RestoreAsNewDatabase(context.Background(), backup.ID, "Failed"); !errors.Is(err, ErrRestoreFailed) {
		t.Fatalf("RestoreAsNewDatabase() error = %v, want ErrRestoreFailed", err)
	}
	if !failed.provisioner.cleaned || !failed.provisioner.credentialDeleted {
		t.Fatal("failed new restore did not clean target resource and secret")
	}
	if _, ok := failed.metadata.backups[backup.ID]; !ok {
		t.Fatal("source backup was removed after failed restore")
	}
}

func TestRestoreInPlaceSafetyBackupIdentityAndFailureBehavior(t *testing.T) {
	fixture := newBackupServiceFixture()
	backup, _ := fixture.service.CreateBackup(context.Background(), serviceDatabaseID)
	original := fixture.metadata.databases[serviceDatabaseID]
	ready, err := fixture.service.RestoreInPlace(context.Background(), backup.ID, serviceDatabaseID)
	if err != nil {
		t.Fatalf("RestoreInPlace() error = %v", err)
	}
	if ready.ID != original.ID || ready.RoleName != original.RoleName || ready.InternalName != original.InternalName {
		t.Fatalf("target identity changed: before=%#v after=%#v", original, ready)
	}
	if fixture.provisioner.credentialDeleted {
		t.Fatal("in-place restore changed target credential")
	}
	if !fixture.hasReadyKind(metadata.BackupKindPreRestore) {
		t.Fatal("ready pre-restore safety backup was not retained")
	}
	if len(fixture.postgres.events) < 3 ||
		fixture.postgres.events[len(fixture.postgres.events)-2] != "reset" ||
		fixture.postgres.events[len(fixture.postgres.events)-1] != "restore" {
		t.Fatalf("destructive event order = %v", fixture.postgres.events)
	}

	failedSafety := newBackupServiceFixture()
	backup, _ = failedSafety.service.CreateBackup(context.Background(), serviceDatabaseID)
	failedSafety.postgres.dumpErr = errors.New("safety dump")
	if _, err := failedSafety.service.RestoreInPlace(context.Background(), backup.ID, serviceDatabaseID); !errors.Is(err, ErrBackupFailed) {
		t.Fatalf("RestoreInPlace(safety failure) error = %v", err)
	}
	if contains(failedSafety.postgres.events, "reset") || contains(failedSafety.postgres.events, "restore") {
		t.Fatalf("destructive restore ran after safety failure: %v", failedSafety.postgres.events)
	}

	failedRestore := newBackupServiceFixture()
	backup, _ = failedRestore.service.CreateBackup(context.Background(), serviceDatabaseID)
	failedRestore.postgres.restoreErr = errors.New("restore")
	if _, err := failedRestore.service.RestoreInPlace(context.Background(), backup.ID, serviceDatabaseID); !errors.Is(err, ErrRestoreFailed) {
		t.Fatalf("RestoreInPlace(restore failure) error = %v", err)
	}
	if !failedRestore.hasReadyKind(metadata.BackupKindPreRestore) {
		t.Fatal("restore failure removed safety backup")
	}
	if failedRestore.metadata.databases[serviceDatabaseID].Status != metadata.StatusError {
		t.Fatal("restore failure did not mark target error")
	}
	if failedRestore.provisioner.credentialDeleted {
		t.Fatal("restore failure removed existing credential")
	}
}

func TestDeleteDatabaseRefusesAttachedWithoutDestructiveChanges(t *testing.T) {
	fixture := newBackupServiceFixture()
	backup := fixture.addReadyBackup(metadata.BackupKindManual, fixture.metadata.now)
	fixture.metadata.attachments = []metadata.Attachment{{
		ID: "attachment_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DatabaseID: serviceDatabaseID,
	}}
	fixture.metadata.events = nil

	if err := fixture.service.DeleteDatabase(context.Background(), serviceDatabaseID); !errors.Is(err, ErrDatabaseAttached) {
		t.Fatalf("DeleteDatabase() error = %v, want ErrDatabaseAttached", err)
	}
	if fixture.metadata.databases[serviceDatabaseID].Status != metadata.StatusReady ||
		fixture.provisioner.deleteCalls != 0 || len(fixture.metadata.events) != 0 {

		t.Fatalf("attached deletion changed state: database=%#v calls=%d events=%v",
			fixture.metadata.databases[serviceDatabaseID], fixture.provisioner.deleteCalls, fixture.metadata.events)
	}
	if _, exists := fixture.metadata.backups[backup.ID]; !exists {
		t.Fatal("attached deletion removed backup metadata")
	}
	if _, exists := fixture.archives.files[archiveKey(serviceDatabaseID, backup.ID)]; !exists {
		t.Fatal("attached deletion removed backup archive")
	}
}

func TestDeleteDatabaseRechecksAttachmentsAfterFailClosedTransition(t *testing.T) {
	fixture := newBackupServiceFixture()
	fixture.metadata.statusHook = func(status metadata.DatabaseStatus) {
		if status == metadata.StatusError {
			fixture.metadata.attachments = []metadata.Attachment{{
				ID: "attachment_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", DatabaseID: serviceDatabaseID,
			}}
		}
	}

	if err := fixture.service.DeleteDatabase(context.Background(), serviceDatabaseID); !errors.Is(err, ErrDatabaseAttached) {
		t.Fatalf("DeleteDatabase() error = %v, want ErrDatabaseAttached", err)
	}
	if fixture.metadata.databases[serviceDatabaseID].Status != metadata.StatusReady {
		t.Fatal("attachment race did not restore the database to ready")
	}
	if fixture.provisioner.deleteCalls != 0 || contains(fixture.metadata.events, "archives") {
		t.Fatalf("attachment race triggered destructive work: %v", fixture.metadata.events)
	}
}

func TestDeleteStandaloneDatabaseRemovesMetadataLast(t *testing.T) {
	fixture := newBackupServiceFixture()
	fixture.addReadyBackup(metadata.BackupKindManual, fixture.metadata.now)
	fixture.addReadyBackup(metadata.BackupKindAutomatic, fixture.metadata.now.Add(-time.Hour))
	fixture.metadata.events = nil

	if err := fixture.service.DeleteDatabase(context.Background(), serviceDatabaseID); err != nil {
		t.Fatalf("DeleteDatabase() error = %v", err)
	}
	if _, exists := fixture.metadata.databases[serviceDatabaseID]; exists {
		t.Fatal("database metadata still exists")
	}
	if len(fixture.metadata.backups) != 0 || len(fixture.archives.files) != 0 {
		t.Fatalf("backup cleanup incomplete: metadata=%v archives=%v", fixture.metadata.backups, fixture.archives.files)
	}
	if fixture.provisioner.deleteCalls != 1 {
		t.Fatalf("resource cleanup calls = %d, want 1", fixture.provisioner.deleteCalls)
	}
	wantPrefix := []string{"status:error", "database", "role", "credential", "archives"}
	if len(fixture.metadata.events) < len(wantPrefix)+3 {
		t.Fatalf("deletion events = %v", fixture.metadata.events)
	}
	for index, want := range wantPrefix {
		if fixture.metadata.events[index] != want {
			t.Fatalf("deletion events = %v; event %d want %q", fixture.metadata.events, index, want)
		}
	}
	if fixture.metadata.events[len(fixture.metadata.events)-1] != "database_metadata" {
		t.Fatalf("database metadata was not deleted last: %v", fixture.metadata.events)
	}
	for _, event := range fixture.metadata.events[len(wantPrefix) : len(fixture.metadata.events)-1] {
		if event != "backup_metadata" {
			t.Fatalf("unexpected event before database metadata deletion: %v", fixture.metadata.events)
		}
	}
}

func TestDeleteDatabaseRetriesAfterPartialCleanup(t *testing.T) {
	fixture := newBackupServiceFixture()
	fixture.addReadyBackup(metadata.BackupKindManual, fixture.metadata.now)
	fixture.archives.deleteDatabaseErr = errors.New("injected archive cleanup failure")

	if err := fixture.service.DeleteDatabase(context.Background(), serviceDatabaseID); !errors.Is(err, ErrDeletionFailed) {
		t.Fatalf("first DeleteDatabase() error = %v, want ErrDeletionFailed", err)
	}
	if fixture.metadata.databases[serviceDatabaseID].Status != metadata.StatusError {
		t.Fatal("partial deletion did not remain fail-closed")
	}
	if len(fixture.metadata.backups) != 1 {
		t.Fatal("backup metadata changed after archive cleanup failure")
	}

	if err := fixture.service.DeleteDatabase(context.Background(), serviceDatabaseID); err != nil {
		t.Fatalf("retry DeleteDatabase() error = %v", err)
	}
	if _, exists := fixture.metadata.databases[serviceDatabaseID]; exists ||
		len(fixture.metadata.backups) != 0 || len(fixture.archives.files) != 0 {

		t.Fatal("retry did not finish partial cleanup")
	}
	if fixture.provisioner.deleteCalls != 2 {
		t.Fatalf("idempotent resource cleanup calls = %d, want 2", fixture.provisioner.deleteCalls)
	}
}

func TestDeleteDatabaseUnknownAndInvalidReturnMetadataErrors(t *testing.T) {
	fixture := newBackupServiceFixture()
	if err := fixture.service.DeleteDatabase(context.Background(), "database_ffffffffffffffffffffffffffffffff"); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("unknown database error = %v", err)
	}
	if err := fixture.service.DeleteDatabase(context.Background(), "invalid"); !errors.Is(err, metadata.ErrInvalidIdentifier) {
		t.Fatalf("invalid database error = %v", err)
	}
	if fixture.provisioner.deleteCalls != 0 {
		t.Fatal("unknown or invalid deletion touched resources")
	}
}

func TestRunDueAutomaticBackupsIsIdempotent(t *testing.T) {
	fixture := newBackupServiceFixture()
	fixture.service.now = func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) }
	first, err := fixture.service.RunDueAutomaticBackups(context.Background())
	if err != nil || first.BackupsCreated != 1 {
		t.Fatalf("first run = %#v, error=%v", first, err)
	}
	second, err := fixture.service.RunDueAutomaticBackups(context.Background())
	if err != nil || second.BackupsCreated != 0 {
		t.Fatalf("second run = %#v, error=%v", second, err)
	}
}

type serviceFixture struct {
	service     *Service
	metadata    *fakeBackupMetadata
	archives    *fakeArchiveStore
	postgres    *fakeBackupPostgres
	provisioner *fakeProvisioner
}

func newBackupServiceFixture() serviceFixture {
	meta := &fakeBackupMetadata{
		databases: map[string]metadata.Database{
			serviceDatabaseID: {
				ID: serviceDatabaseID, DisplayName: "Source", InternalName: testDatabaseName,
				RoleName: testRoleName, Status: metadata.StatusReady,
			},
		},
		backups: make(map[string]metadata.Backup),
		now:     time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
	}
	archives := &fakeArchiveStore{files: make(map[string][]byte), metadata: meta}
	postgres := &fakeBackupPostgres{}
	provisioner := &fakeProvisioner{metadata: meta}
	service := NewService(meta, archives, postgres, provisioner)
	return serviceFixture{service: service, metadata: meta, archives: archives, postgres: postgres, provisioner: provisioner}
}

func (fixture *serviceFixture) addReadyBackup(kind metadata.BackupKind, createdAt time.Time) metadata.Backup {
	fixture.metadata.now = createdAt
	backup, _ := fixture.metadata.CreateBackup(context.Background(), serviceDatabaseID, kind)
	backup, _ = fixture.metadata.UpdateBackupReady(context.Background(), backup.ID, 7)
	fixture.archives.files[archiveKey(backup.DatabaseID, backup.ID)] = []byte("archive")
	return backup
}

func (fixture *serviceFixture) hasReadyKind(kind metadata.BackupKind) bool {
	for _, backup := range fixture.metadata.backups {
		if backup.Kind == kind && backup.Status == metadata.BackupStatusReady {
			return true
		}
	}
	return false
}

type fakeBackupMetadata struct {
	databases   map[string]metadata.Database
	backups     map[string]metadata.Backup
	attachments []metadata.Attachment
	next        int
	now         time.Time
	readyErr    error
	statusHook  func(metadata.DatabaseStatus)
	events      []string
}

func (store *fakeBackupMetadata) GetDatabase(_ context.Context, id string) (metadata.Database, error) {
	database, ok := store.databases[id]
	if !ok {
		return metadata.Database{}, metadata.ErrNotFound
	}
	return database, nil
}
func (store *fakeBackupMetadata) ListDatabases(context.Context) ([]metadata.Database, error) {
	result := make([]metadata.Database, 0, len(store.databases))
	for _, database := range store.databases {
		result = append(result, database)
	}
	return result, nil
}
func (store *fakeBackupMetadata) ListAttachmentsForDatabase(_ context.Context, databaseID string) ([]metadata.Attachment, error) {
	result := make([]metadata.Attachment, 0)
	for _, attachment := range store.attachments {
		if attachment.DatabaseID == databaseID {
			result = append(result, attachment)
		}
	}
	return result, nil
}
func (store *fakeBackupMetadata) UpdateDatabaseStatus(_ context.Context, id string, status metadata.DatabaseStatus) (metadata.Database, error) {
	database, exists := store.databases[id]
	if !exists {
		return metadata.Database{}, metadata.ErrNotFound
	}
	database.Status = status
	store.databases[id] = database
	store.events = append(store.events, "status:"+string(status))
	if store.statusHook != nil {
		store.statusHook(status)
	}
	return database, nil
}
func (store *fakeBackupMetadata) CreateBackup(_ context.Context, databaseID string, kind metadata.BackupKind) (metadata.Backup, error) {
	store.next++
	id := fmt.Sprintf("backup_%032x", store.next+3)
	if store.next == 1 {
		id = serviceBackupID
	}
	backup := metadata.Backup{
		ID: id, DatabaseID: databaseID, Kind: kind, Status: metadata.BackupStatusCreating,
		CreatedAt: store.now.UTC(),
	}
	store.backups[id] = backup
	return backup, nil
}
func (store *fakeBackupMetadata) GetBackup(_ context.Context, id string) (metadata.Backup, error) {
	backup, ok := store.backups[id]
	if !ok {
		return metadata.Backup{}, metadata.ErrNotFound
	}
	return backup, nil
}
func (store *fakeBackupMetadata) ListBackups(context.Context) ([]metadata.Backup, error) {
	return store.sortedBackups(""), nil
}
func (store *fakeBackupMetadata) ListBackupsForDatabase(_ context.Context, id string) ([]metadata.Backup, error) {
	return store.sortedBackups(id), nil
}
func (store *fakeBackupMetadata) sortedBackups(databaseID string) []metadata.Backup {
	result := make([]metadata.Backup, 0)
	for _, backup := range store.backups {
		if databaseID == "" || backup.DatabaseID == databaseID {
			result = append(result, backup)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].ID > result[j].ID
		}
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})
	return result
}
func (store *fakeBackupMetadata) UpdateBackupReady(_ context.Context, id string, size int64) (metadata.Backup, error) {
	if store.readyErr != nil {
		return metadata.Backup{}, store.readyErr
	}
	backup := store.backups[id]
	backup.Status = metadata.BackupStatusReady
	backup.SizeBytes = size
	completed := store.now.UTC()
	backup.CompletedAt = &completed
	store.backups[id] = backup
	return backup, nil
}
func (store *fakeBackupMetadata) UpdateBackupError(_ context.Context, id string) (metadata.Backup, error) {
	backup := store.backups[id]
	backup.Status = metadata.BackupStatusError
	backup.SizeBytes = 0
	completed := store.now.UTC()
	backup.CompletedAt = &completed
	store.backups[id] = backup
	return backup, nil
}
func (store *fakeBackupMetadata) DeleteBackupMetadata(_ context.Context, id string) error {
	store.events = append(store.events, "backup_metadata")
	delete(store.backups, id)
	return nil
}
func (store *fakeBackupMetadata) DeleteDatabaseMetadata(_ context.Context, id string) error {
	store.events = append(store.events, "database_metadata")
	delete(store.databases, id)
	return nil
}

type fakeArchiveStore struct {
	files             map[string][]byte
	partial           bool
	createErr         error
	deleteErr         error
	deleteDatabaseErr error
	unrelated         bool
	metadata          *fakeBackupMetadata
}

func (store *fakeArchiveStore) Create(databaseID, backupID string, dump func(io.Writer) error, verify func(io.Reader) error) (int64, error) {
	store.partial = true
	if store.createErr != nil {
		return 0, store.createErr
	}
	var buffer bytes.Buffer
	if err := dump(&buffer); err != nil {
		return 0, err
	}
	if err := verify(bytes.NewReader(buffer.Bytes())); err != nil {
		return 0, err
	}
	store.files[archiveKey(databaseID, backupID)] = append([]byte(nil), buffer.Bytes()...)
	store.partial = false
	return int64(buffer.Len()), nil
}
func (store *fakeArchiveStore) Open(databaseID, backupID string) (*ArchiveReader, error) {
	data, ok := store.files[archiveKey(databaseID, backupID)]
	if !ok {
		return nil, ErrArchiveNotFound
	}
	return &ArchiveReader{ReadCloser: io.NopCloser(bytes.NewReader(data)), SizeBytes: int64(len(data))}, nil
}
func (store *fakeArchiveStore) Delete(databaseID, backupID string) error {
	if store.deleteErr != nil {
		return store.deleteErr
	}
	delete(store.files, archiveKey(databaseID, backupID))
	return nil
}
func (store *fakeArchiveStore) DeleteDatabase(databaseID string) error {
	if store.metadata != nil {
		store.metadata.events = append(store.metadata.events, "archives")
	}
	if store.deleteDatabaseErr != nil {
		err := store.deleteDatabaseErr
		store.deleteDatabaseErr = nil
		return err
	}
	for key := range store.files {
		if strings.HasPrefix(key, databaseID+"/") {
			delete(store.files, key)
		}
	}
	return nil
}
func (store *fakeArchiveStore) RemovePartial(string, string) error {
	store.partial = false
	return nil
}

type fakeBackupPostgres struct {
	dumpErr          error
	verifyErr        error
	restoreErr       error
	resetErr         error
	dumpDatabase     string
	verifyCalls      int
	restoredDatabase string
	restoredRole     string
	events           []string
}

func (postgres *fakeBackupPostgres) Dump(_ context.Context, databaseName string, output io.Writer) error {
	postgres.events = append(postgres.events, "dump")
	postgres.dumpDatabase = databaseName
	if postgres.dumpErr != nil {
		return postgres.dumpErr
	}
	_, err := output.Write([]byte("archive"))
	return err
}
func (postgres *fakeBackupPostgres) VerifyArchive(_ context.Context, archive io.Reader) error {
	postgres.verifyCalls++
	if postgres.verifyErr != nil {
		return postgres.verifyErr
	}
	_, err := io.Copy(io.Discard, archive)
	return err
}
func (postgres *fakeBackupPostgres) Restore(_ context.Context, databaseName, roleName string, archive io.Reader) error {
	postgres.events = append(postgres.events, "restore")
	postgres.restoredDatabase = databaseName
	postgres.restoredRole = roleName
	_, _ = io.Copy(io.Discard, archive)
	return postgres.restoreErr
}
func (postgres *fakeBackupPostgres) ResetDatabase(context.Context, string, string) error {
	postgres.events = append(postgres.events, "reset")
	return postgres.resetErr
}

type fakeProvisioner struct {
	metadata          *fakeBackupMetadata
	cleaned           bool
	credentialDeleted bool
	deleteCalls       int
	deleteErr         error
}

func (provisioner *fakeProvisioner) ProvisionDatabaseForRestore(_ context.Context, displayName string) (metadata.Database, error) {
	database := metadata.Database{
		ID: "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: displayName,
		InternalName: "mb_db_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RoleName:     "mb_role_cccccccccccccccccccccccccccccccc",
		Status:       metadata.StatusProvisioning,
	}
	provisioner.metadata.databases[database.ID] = database
	return database, nil
}
func (provisioner *fakeProvisioner) FinalizeRestoredDatabase(_ context.Context, database metadata.Database) (metadata.Database, error) {
	database.Status = metadata.StatusReady
	provisioner.metadata.databases[database.ID] = database
	return database, nil
}
func (provisioner *fakeProvisioner) MarkDatabaseRestoring(_ context.Context, id string) (metadata.Database, error) {
	database := provisioner.metadata.databases[id]
	database.Status = metadata.StatusProvisioning
	provisioner.metadata.databases[id] = database
	return database, nil
}
func (provisioner *fakeProvisioner) MarkDatabaseError(_ context.Context, id string) error {
	database := provisioner.metadata.databases[id]
	database.Status = metadata.StatusError
	provisioner.metadata.databases[id] = database
	return nil
}
func (provisioner *fakeProvisioner) CleanupRestoreTarget(database metadata.Database) error {
	delete(provisioner.metadata.databases, database.ID)
	provisioner.cleaned = true
	provisioner.credentialDeleted = true
	return nil
}
func (provisioner *fakeProvisioner) DeleteDatabaseResources(_ context.Context, _ metadata.Database) error {
	provisioner.deleteCalls++
	provisioner.metadata.events = append(provisioner.metadata.events, "database")
	if provisioner.deleteErr != nil {
		return provisioner.deleteErr
	}
	provisioner.metadata.events = append(provisioner.metadata.events, "role", "credential")
	return nil
}

func archiveKey(databaseID, backupID string) string {
	return databaseID + "/" + backupID
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
