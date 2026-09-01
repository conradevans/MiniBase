package metadata

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/conradevans/MiniBase/internal/ids"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	databasePath := filepath.Join(t.TempDir(), "nested", "minibase.db")
	store, err := Open(context.Background(), databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store, databasePath
}

func TestFreshInitializationAndReopenAreIdempotent(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "nested", "minibase.db")
	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	initialStoreClosed := false
	defer func() {
		if !initialStoreClosed {
			_ = store.Close()
		}
	}()

	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, CurrentSchemaVersion)
	}

	var migrationCount int
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationCount != CurrentSchemaVersion {
		t.Fatalf("migration count = %d, want %d", migrationCount, CurrentSchemaVersion)
	}

	var foreignKeys int
	if err := store.db.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	var journalMode string
	if err := store.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var busyTimeout int
	if err := store.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != sqliteBusyTimeoutMilliseconds {
		t.Fatalf("busy_timeout = %d, want %d", busyTimeout, sqliteBusyTimeoutMilliseconds)
	}

	var synchronous int
	if err := store.db.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatalf("query synchronous: %v", err)
	}
	if synchronous != 2 {
		t.Fatalf("synchronous = %d, want 2 (FULL)", synchronous)
	}

	dataDirectoryInfo, err := os.Stat(filepath.Dir(databasePath))
	if err != nil {
		t.Fatalf("stat data directory: %v", err)
	}
	if dataDirectoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("data directory mode = %o, want 700", dataDirectoryInfo.Mode().Perm())
	}
	databaseInfo, err := os.Stat(databasePath)
	if err != nil {
		t.Fatalf("stat metadata database: %v", err)
	}
	if databaseInfo.Mode().Perm() != 0o600 {
		t.Fatalf("database mode = %o, want 600", databaseInfo.Mode().Perm())
	}

	if err := store.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	initialStoreClosed = true
	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	defer func() {
		_ = reopened.Close()
	}()
	store = reopened

	version, err = store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() after reopen error = %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version after reopen = %d, want %d", version, CurrentSchemaVersion)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT count(*) FROM schema_migrations").Scan(&migrationCount); err != nil {
		t.Fatalf("count migrations after reopen: %v", err)
	}
	if migrationCount != CurrentSchemaVersion {
		t.Fatalf("migration count after reopen = %d, want %d", migrationCount, CurrentSchemaVersion)
	}
}

func TestDatabaseMetadataLifecycle(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	created, err := store.CreateDatabaseMetadata(ctx, "  Café Schedule  ")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	if created.DisplayName != "Café Schedule" {
		t.Fatalf("DisplayName = %q", created.DisplayName)
	}
	if created.Status != StatusMetadataOnly {
		t.Fatalf("Status = %q, want %q", created.Status, StatusMetadataOnly)
	}

	listed, err := store.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("ListDatabases() = %#v", listed)
	}

	got, err := store.GetDatabase(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDatabase() error = %v", err)
	}
	if got != created {
		t.Fatalf("GetDatabase() = %#v, want %#v", got, created)
	}

	renamed, err := store.UpdateDisplayName(ctx, created.ID, "  Updated Name ")
	if err != nil {
		t.Fatalf("UpdateDisplayName() error = %v", err)
	}
	if renamed.DisplayName != "Updated Name" {
		t.Fatalf("renamed DisplayName = %q", renamed.DisplayName)
	}

	updated, err := store.UpdateDatabaseStatus(ctx, created.ID, StatusProvisioning)
	if err != nil {
		t.Fatalf("UpdateDatabaseStatus() error = %v", err)
	}
	if updated.Status != StatusProvisioning {
		t.Fatalf("updated Status = %q", updated.Status)
	}
	if updated.InternalName != created.InternalName {
		t.Fatalf("internal name changed from %q to %q", created.InternalName, updated.InternalName)
	}
}

func TestDisplayNameValidation(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	for _, displayName := range []string{"", "   ", strings.Repeat("a", MaxDisplayNameRunes+1)} {
		if _, err := store.CreateDatabaseMetadata(ctx, displayName); !errors.Is(err, ErrInvalidDisplayName) {
			t.Fatalf("CreateDatabaseMetadata(%q) error = %v, want ErrInvalidDisplayName", displayName, err)
		}
	}

	created, err := store.CreateDatabaseMetadata(ctx, "Valid")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	if _, err := store.UpdateDisplayName(ctx, created.ID, " "); !errors.Is(err, ErrInvalidDisplayName) {
		t.Fatalf("UpdateDisplayName() error = %v, want ErrInvalidDisplayName", err)
	}
}

func TestInvalidStatusRejectedByStoreAndSchema(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	created, err := store.CreateDatabaseMetadata(ctx, "Status Test")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	if _, err := store.UpdateDatabaseStatus(ctx, created.ID, DatabaseStatus("unknown")); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("UpdateDatabaseStatus() error = %v, want ErrInvalidStatus", err)
	}

	id, err := ids.DatabaseID()
	if err != nil {
		t.Fatalf("DatabaseID() error = %v", err)
	}
	internalName, err := ids.DatabaseInternalName()
	if err != nil {
		t.Fatalf("DatabaseInternalName() error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := store.db.ExecContext(
		ctx,
		"INSERT INTO databases (id, display_name, internal_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		id,
		"Invalid Status",
		internalName,
		"not_valid",
		now,
		now,
	); err == nil {
		t.Fatal("SQLite accepted an invalid status")
	}
}

func TestDuplicateInternalNameReturnsConflict(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	store.newDatabaseInternalName = func() (string, error) {
		return "mb_db_0123456789abcdef0123456789abcdef", nil
	}

	if _, err := store.CreateDatabaseMetadata(ctx, "First"); err != nil {
		t.Fatalf("first CreateDatabaseMetadata() error = %v", err)
	}
	if _, err := store.CreateDatabaseMetadata(ctx, "Second"); !errors.Is(err, ErrConflict) {
		t.Fatalf("second CreateDatabaseMetadata() error = %v, want ErrConflict", err)
	}
}

func TestMissingRecordBehavior(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	if _, err := store.GetDatabase(ctx, "database_00000000000000000000000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDatabase() error = %v, want ErrNotFound", err)
	}
	if _, err := store.UpdateDisplayName(ctx, "database_00000000000000000000000000000000", "Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateDisplayName() error = %v, want ErrNotFound", err)
	}
	if _, err := store.UpdateDatabaseStatus(ctx, "database_00000000000000000000000000000000", StatusReady); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateDatabaseStatus() error = %v, want ErrNotFound", err)
	}
}

func TestMigrationFromV1PreservesExistingMetadata(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "minibase.db")
	if _, err := prepareDatabasePath(databasePath); err != nil {
		t.Fatalf("prepareDatabasePath() error = %v", err)
	}

	db, err := sql.Open("sqlite", sqliteDSN(databasePath))
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		t.Fatalf("create migration table: %v", err)
	}
	for _, statement := range migrations[0].statements {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("apply v1 statement: %v", err)
		}
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?)", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("record v1 migration: %v", err)
	}
	const (
		databaseID   = "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		internalName = "mb_db_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		"INSERT INTO databases (id, display_name, internal_name, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		databaseID, "Existing Metadata", internalName, StatusMetadataOnly, now, now,
	); err != nil {
		t.Fatalf("insert v1 metadata: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v1 database: %v", err)
	}

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() migrated database error = %v", err)
	}
	defer store.Close()

	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatalf("SchemaVersion() error = %v", err)
	}
	if version != CurrentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, CurrentSchemaVersion)
	}
	database, err := store.GetDatabase(ctx, databaseID)
	if err != nil {
		t.Fatalf("GetDatabase() error = %v", err)
	}
	if database.DisplayName != "Existing Metadata" || database.InternalName != internalName || database.RoleName != "" {
		t.Fatalf("migrated database = %#v", database)
	}
}

func TestInitialSchemaContainsNoCredentialColumns(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info(databases)")
	if err != nil {
		t.Fatalf("table_info(databases): %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var (
			cid          int
			name         string
			columnType   string
			notNull      int
			defaultValue any
			primaryKey   int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table info: %v", err)
	}

	want := []string{"id", "display_name", "internal_name", "status", "created_at", "updated_at", "role_name"}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("columns = %v, want %v", columns, want)
	}
}

func TestConcurrentMetadataCreation(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	const count = 32
	errs := make(chan error, count)
	var waitGroup sync.WaitGroup
	for index := range count {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			_, err := store.CreateDatabaseMetadata(ctx, "Database "+string(rune('A'+index)))
			errs <- err
		}(index)
	}
	waitGroup.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent CreateDatabaseMetadata() error = %v", err)
		}
	}
	databases, err := store.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("ListDatabases() error = %v", err)
	}
	if len(databases) != count {
		t.Fatalf("database count = %d, want %d", len(databases), count)
	}
}
