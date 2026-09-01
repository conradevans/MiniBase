package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/conradevans/MiniBase/internal/ids"
)

type BackupKind string

const (
	BackupKindManual     BackupKind = "manual"
	BackupKindAutomatic  BackupKind = "automatic"
	BackupKindPreRestore BackupKind = "pre_restore"
)

type BackupStatus string

const (
	BackupStatusCreating BackupStatus = "creating"
	BackupStatusReady    BackupStatus = "ready"
	BackupStatusError    BackupStatus = "error"
)

var (
	ErrInvalidBackupKind   = errors.New("invalid backup kind")
	ErrInvalidBackupStatus = errors.New("invalid backup status")
	ErrInvalidBackupSize   = errors.New("invalid backup size")
)

type Backup struct {
	ID          string       `json:"id"`
	DatabaseID  string       `json:"databaseId"`
	Kind        BackupKind   `json:"kind"`
	Status      BackupStatus `json:"status"`
	SizeBytes   int64        `json:"sizeBytes"`
	CreatedAt   time.Time    `json:"createdAt"`
	CompletedAt *time.Time   `json:"completedAt"`
}

func newBackupID() (string, error) {
	return ids.BackupID()
}

func (kind BackupKind) Valid() bool {
	switch kind {
	case BackupKindManual, BackupKindAutomatic, BackupKindPreRestore:
		return true
	default:
		return false
	}
}

func (status BackupStatus) Valid() bool {
	switch status {
	case BackupStatusCreating, BackupStatusReady, BackupStatusError:
		return true
	default:
		return false
	}
}

func (s *Store) CreateBackup(ctx context.Context, databaseID string, kind BackupKind) (Backup, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return Backup{}, ErrInvalidIdentifier
	}
	if !kind.Valid() {
		return Backup{}, ErrInvalidBackupKind
	}
	backupID, err := s.newBackupID()
	if err != nil {
		return Backup{}, fmt.Errorf("generate backup metadata ID: %w", err)
	}
	if !ids.ValidBackupID(backupID) {
		return Backup{}, ErrInvalidIdentifier
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Backup{}, fmt.Errorf("begin backup metadata creation: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	var exists int
	if err := tx.QueryRowContext(ctx, "SELECT 1 FROM databases WHERE id = ?", databaseID).Scan(&exists); errors.Is(err, sql.ErrNoRows) {
		return Backup{}, fmt.Errorf("%w: database", ErrNotFound)
	} else if err != nil {
		return Backup{}, fmt.Errorf("verify backup database metadata: %w", err)
	}

	createdAt := s.now().UTC()
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO backups (
			id, database_id, kind, status, size_bytes, created_at, completed_at
		) VALUES (?, ?, ?, ?, 0, ?, NULL)`,
		backupID,
		databaseID,
		kind,
		BackupStatusCreating,
		createdAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Backup{}, classifyWriteError("create backup metadata", err)
	}
	if err := tx.Commit(); err != nil {
		return Backup{}, fmt.Errorf("commit backup metadata creation: %w", err)
	}
	return Backup{
		ID:         backupID,
		DatabaseID: databaseID,
		Kind:       kind,
		Status:     BackupStatusCreating,
		CreatedAt:  createdAt,
	}, nil
}

func (s *Store) GetBackup(ctx context.Context, id string) (Backup, error) {
	if !ids.ValidBackupID(id) {
		return Backup{}, ErrInvalidIdentifier
	}
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, database_id, kind, status, size_bytes, created_at, completed_at
		FROM backups
		WHERE id = ?`,
		id,
	)
	backup, err := scanBackup(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Backup{}, fmt.Errorf("%w: backup", ErrNotFound)
	}
	if err != nil {
		return Backup{}, fmt.Errorf("get backup metadata: %w", err)
	}
	return backup, nil
}

func (s *Store) ListBackups(ctx context.Context) ([]Backup, error) {
	return s.listBackups(ctx, "", nil)
}

func (s *Store) ListBackupsForDatabase(ctx context.Context, databaseID string) ([]Backup, error) {
	if !ids.ValidDatabaseID(databaseID) {
		return nil, ErrInvalidIdentifier
	}
	return s.listBackups(ctx, " WHERE database_id = ?", []any{databaseID})
}

func (s *Store) listBackups(ctx context.Context, whereClause string, arguments []any) ([]Backup, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, database_id, kind, status, size_bytes, created_at, completed_at
		FROM backups`+whereClause+`
		ORDER BY created_at DESC, id DESC`,
		arguments...,
	)
	if err != nil {
		return nil, fmt.Errorf("list backup metadata: %w", err)
	}
	defer rows.Close()

	backups := make([]Backup, 0)
	for rows.Next() {
		backup, err := scanBackup(rows)
		if err != nil {
			return nil, fmt.Errorf("scan backup metadata: %w", err)
		}
		backups = append(backups, backup)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate backup metadata: %w", err)
	}
	return backups, nil
}

func (s *Store) UpdateBackupReady(ctx context.Context, id string, sizeBytes int64) (Backup, error) {
	if sizeBytes <= 0 {
		return Backup{}, ErrInvalidBackupSize
	}
	return s.updateBackupCompletion(ctx, id, BackupStatusReady, sizeBytes)
}

func (s *Store) UpdateBackupError(ctx context.Context, id string) (Backup, error) {
	return s.updateBackupCompletion(ctx, id, BackupStatusError, 0)
}

func (s *Store) updateBackupCompletion(ctx context.Context, id string, status BackupStatus, sizeBytes int64) (Backup, error) {
	if !ids.ValidBackupID(id) {
		return Backup{}, ErrInvalidIdentifier
	}
	if status != BackupStatusReady && status != BackupStatusError {
		return Backup{}, ErrInvalidBackupStatus
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Backup{}, fmt.Errorf("begin backup metadata update: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	completedAt := s.now().UTC()
	result, err := tx.ExecContext(
		ctx,
		`UPDATE backups
		SET status = ?, size_bytes = ?, completed_at = ?
		WHERE id = ? AND status = 'creating'`,
		status,
		sizeBytes,
		completedAt.Format(time.RFC3339Nano),
		id,
	)
	if err != nil {
		return Backup{}, classifyWriteError("update backup metadata", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Backup{}, fmt.Errorf("read backup metadata update result: %w", err)
	}
	if rowsAffected == 0 {
		var exists int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM backups WHERE id = ?", id).Scan(&exists)
		if errors.Is(err, sql.ErrNoRows) {
			return Backup{}, fmt.Errorf("%w: backup", ErrNotFound)
		}
		if err != nil {
			return Backup{}, fmt.Errorf("verify backup metadata update target: %w", err)
		}
		return Backup{}, ErrConflict
	}

	row := tx.QueryRowContext(
		ctx,
		`SELECT id, database_id, kind, status, size_bytes, created_at, completed_at
		FROM backups
		WHERE id = ?`,
		id,
	)
	backup, err := scanBackup(row)
	if err != nil {
		return Backup{}, fmt.Errorf("read updated backup metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Backup{}, fmt.Errorf("commit backup metadata update: %w", err)
	}
	return backup, nil
}

func (s *Store) DeleteBackupMetadata(ctx context.Context, id string) error {
	if !ids.ValidBackupID(id) {
		return ErrInvalidIdentifier
	}
	result, err := s.db.ExecContext(ctx, "DELETE FROM backups WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete backup metadata: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read backup metadata deletion result: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("%w: backup", ErrNotFound)
	}
	return nil
}

func scanBackup(scanner rowScanner) (Backup, error) {
	var (
		backup      Backup
		createdAt   string
		completedAt sql.NullString
	)
	if err := scanner.Scan(
		&backup.ID,
		&backup.DatabaseID,
		&backup.Kind,
		&backup.Status,
		&backup.SizeBytes,
		&createdAt,
		&completedAt,
	); err != nil {
		return Backup{}, err
	}
	if !backup.Kind.Valid() || !backup.Status.Valid() {
		return Backup{}, fmt.Errorf("invalid backup metadata enum")
	}
	var err error
	backup.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Backup{}, fmt.Errorf("parse backup created timestamp: %w", err)
	}
	if completedAt.Valid {
		value, err := time.Parse(time.RFC3339Nano, completedAt.String)
		if err != nil {
			return Backup{}, fmt.Errorf("parse backup completed timestamp: %w", err)
		}
		backup.CompletedAt = &value
	}
	return backup, nil
}
