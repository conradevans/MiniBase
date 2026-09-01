package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/conradevans/MiniBase/internal/ids"
	sqliteDriver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const MaxDisplayNameRunes = 200

type DatabaseStatus string

const (
	StatusMetadataOnly DatabaseStatus = "metadata_only"
	StatusProvisioning DatabaseStatus = "provisioning"
	StatusReady        DatabaseStatus = "ready"
	StatusError        DatabaseStatus = "error"
)

var (
	ErrNotFound           = errors.New("metadata record not found")
	ErrConflict           = errors.New("metadata conflict")
	ErrInvalidDisplayName = errors.New("invalid display name")
	ErrInvalidStatus      = errors.New("invalid database status")
)

type Database struct {
	ID           string         `json:"id"`
	DisplayName  string         `json:"displayName"`
	InternalName string         `json:"internalName"`
	Status       DatabaseStatus `json:"status"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
}

type rowScanner interface {
	Scan(dest ...any) error
}

func newDatabaseID() (string, error) {
	return ids.DatabaseID()
}

func newDatabaseInternalName() (string, error) {
	return ids.DatabaseInternalName()
}

func (status DatabaseStatus) Valid() bool {
	switch status {
	case StatusMetadataOnly, StatusProvisioning, StatusReady, StatusError:
		return true
	default:
		return false
	}
}

func (s *Store) CreateDatabaseMetadata(ctx context.Context, displayName string) (Database, error) {
	normalizedName, err := normalizeDisplayName(displayName)
	if err != nil {
		return Database{}, err
	}

	id, err := s.newDatabaseID()
	if err != nil {
		return Database{}, fmt.Errorf("generate database metadata ID: %w", err)
	}
	internalName, err := s.newDatabaseInternalName()
	if err != nil {
		return Database{}, fmt.Errorf("generate database internal name: %w", err)
	}

	now := s.now().UTC()
	database := Database{
		ID:           id,
		DisplayName:  normalizedName,
		InternalName: internalName,
		Status:       StatusMetadataOnly,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Database{}, fmt.Errorf("begin database metadata creation: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO databases (
			id, display_name, internal_name, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		database.ID,
		database.DisplayName,
		database.InternalName,
		database.Status,
		database.CreatedAt.Format(time.RFC3339Nano),
		database.UpdatedAt.Format(time.RFC3339Nano),
	)
	if err != nil {
		return Database{}, classifyWriteError("create database metadata", err)
	}

	if err := tx.Commit(); err != nil {
		return Database{}, fmt.Errorf("commit database metadata creation: %w", err)
	}
	return database, nil
}

func (s *Store) GetDatabase(ctx context.Context, id string) (Database, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, display_name, internal_name, status, created_at, updated_at
		FROM databases
		WHERE id = ?`,
		id,
	)
	database, err := scanDatabase(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Database{}, fmt.Errorf("%w: database", ErrNotFound)
	}
	if err != nil {
		return Database{}, fmt.Errorf("get database metadata: %w", err)
	}
	return database, nil
}

func (s *Store) ListDatabases(ctx context.Context) ([]Database, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, display_name, internal_name, status, created_at, updated_at
		FROM databases
		ORDER BY created_at, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list database metadata: %w", err)
	}
	defer rows.Close()

	databases := make([]Database, 0)
	for rows.Next() {
		database, err := scanDatabase(rows)
		if err != nil {
			return nil, fmt.Errorf("scan database metadata: %w", err)
		}
		databases = append(databases, database)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database metadata: %w", err)
	}
	return databases, nil
}

func (s *Store) UpdateDatabaseStatus(ctx context.Context, id string, status DatabaseStatus) (Database, error) {
	if !status.Valid() {
		return Database{}, ErrInvalidStatus
	}
	return s.updateDatabase(
		ctx,
		id,
		"UPDATE databases SET status = ?, updated_at = ? WHERE id = ?",
		status,
	)
}

func (s *Store) UpdateDisplayName(ctx context.Context, id, displayName string) (Database, error) {
	normalizedName, err := normalizeDisplayName(displayName)
	if err != nil {
		return Database{}, err
	}
	return s.updateDatabase(
		ctx,
		id,
		"UPDATE databases SET display_name = ?, updated_at = ? WHERE id = ?",
		normalizedName,
	)
}

func (s *Store) updateDatabase(ctx context.Context, id, statement string, value any) (Database, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Database{}, fmt.Errorf("begin database metadata update: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	updatedAt := s.now().UTC()
	result, err := tx.ExecContext(ctx, statement, value, updatedAt.Format(time.RFC3339Nano), id)
	if err != nil {
		return Database{}, classifyWriteError("update database metadata", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return Database{}, fmt.Errorf("read database metadata update result: %w", err)
	}
	if rowsAffected == 0 {
		return Database{}, fmt.Errorf("%w: database", ErrNotFound)
	}

	row := tx.QueryRowContext(
		ctx,
		`SELECT id, display_name, internal_name, status, created_at, updated_at
		FROM databases
		WHERE id = ?`,
		id,
	)
	database, err := scanDatabase(row)
	if err != nil {
		return Database{}, fmt.Errorf("read updated database metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Database{}, fmt.Errorf("commit database metadata update: %w", err)
	}
	return database, nil
}

func scanDatabase(scanner rowScanner) (Database, error) {
	var (
		database             Database
		createdAt, updatedAt string
	)
	if err := scanner.Scan(
		&database.ID,
		&database.DisplayName,
		&database.InternalName,
		&database.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		return Database{}, err
	}

	var err error
	database.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return Database{}, fmt.Errorf("parse created timestamp: %w", err)
	}
	database.UpdatedAt, err = time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return Database{}, fmt.Errorf("parse updated timestamp: %w", err)
	}
	return database, nil
}

func normalizeDisplayName(displayName string) (string, error) {
	normalizedName := strings.TrimSpace(displayName)
	if normalizedName == "" || utf8.RuneCountInString(normalizedName) > MaxDisplayNameRunes {
		return "", ErrInvalidDisplayName
	}
	return normalizedName, nil
}

func classifyWriteError(operation string, err error) error {
	var sqliteErr *sqliteDriver.Error
	if errors.As(err, &sqliteErr) {
		switch sqliteErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE:
			return fmt.Errorf("%w: %s", ErrConflict, operation)
		}
	}
	return fmt.Errorf("%s: %w", operation, err)
}
