package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteBusyTimeoutMilliseconds = 5000

type Store struct {
	db                      *sql.DB
	newBackupID             func() (string, error)
	newDatabaseID           func() (string, error)
	newDatabaseInternalName func() (string, error)
	now                     func() time.Time
}

func Open(ctx context.Context, path string) (*Store, error) {
	databasePath, err := prepareDatabasePath(path)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		return nil, fmt.Errorf("open metadata database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	closeOnError := func(openErr error) (*Store, error) {
		_ = db.Close()
		return nil, openErr
	}

	if err := db.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("reach metadata database: %w", err))
	}
	if err := verifySQLiteConfiguration(ctx, db); err != nil {
		return closeOnError(err)
	}
	if err := applyMigrations(ctx, db); err != nil {
		return closeOnError(err)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		return closeOnError(fmt.Errorf("restrict metadata database permissions: %w", err))
	}

	return &Store{
		db:                      db,
		newBackupID:             newBackupID,
		newDatabaseID:           newDatabaseID,
		newDatabaseInternalName: newDatabaseInternalName,
		now:                     time.Now,
	}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping metadata database: %w", err)
	}
	return nil
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version int
	if err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

func prepareDatabasePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("metadata database path must not be empty")
	}

	absolutePath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve metadata database path: %w", err)
	}
	parent := filepath.Dir(absolutePath)

	if info, err := os.Lstat(parent); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", fmt.Errorf("metadata data path is not a regular directory")
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect metadata data directory: %w", err)
	}

	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", fmt.Errorf("create metadata data directory: %w", err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		return "", fmt.Errorf("restrict metadata data directory permissions: %w", err)
	}

	if info, err := os.Lstat(absolutePath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("metadata database path is not a regular file")
		}
		if err := os.Chmod(absolutePath, 0o600); err != nil {
			return "", fmt.Errorf("restrict metadata database permissions: %w", err)
		}
	} else if os.IsNotExist(err) {
		file, createErr := os.OpenFile(absolutePath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if createErr != nil {
			return "", fmt.Errorf("create metadata database: %w", createErr)
		}
		if closeErr := file.Close(); closeErr != nil {
			return "", fmt.Errorf("close new metadata database: %w", closeErr)
		}
	} else {
		return "", fmt.Errorf("inspect metadata database: %w", err)
	}

	return absolutePath, nil
}

func sqliteDSN(path string) string {
	dsn := &url.URL{Scheme: "file", Path: path}
	query := url.Values{}
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", sqliteBusyTimeoutMilliseconds))
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "synchronous(FULL)")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

func verifySQLiteConfiguration(ctx context.Context, db *sql.DB) error {
	var foreignKeys int
	if err := db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fmt.Errorf("verify SQLite foreign keys: %w", err)
	}
	if foreignKeys != 1 {
		return fmt.Errorf("SQLite foreign keys are disabled")
	}

	var journalMode string
	if err := db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("verify SQLite journal mode: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("SQLite WAL mode is not enabled")
	}

	return nil
}
