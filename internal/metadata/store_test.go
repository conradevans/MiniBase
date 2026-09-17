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
	if created.GuestVisible {
		t.Fatal("new database is guest-visible, want hidden by default")
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

	visible, err := store.UpdateGuestVisibility(ctx, created.ID, true)
	if err != nil {
		t.Fatalf("UpdateGuestVisibility() error = %v", err)
	}
	if !visible.GuestVisible || visible.Status != StatusProvisioning || visible.DisplayName != "Updated Name" {
		t.Fatalf("visibility update changed unrelated metadata: %#v", visible)
	}
	reloaded, err := store.GetDatabase(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDatabase() after visibility update error = %v", err)
	}
	if !reloaded.GuestVisible {
		t.Fatal("guest visibility was not persisted")
	}
}

func TestProvisioningDatabaseIsHiddenByDefault(t *testing.T) {
	store, _ := openTestStore(t)
	database, err := store.CreateProvisioningDatabase(context.Background(), ProvisioningDatabase{
		ID:           "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DisplayName:  "Provisioned",
		InternalName: "mb_db_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RoleName:     "mb_role_cccccccccccccccccccccccccccccccc",
	})
	if err != nil {
		t.Fatalf("CreateProvisioningDatabase() error = %v", err)
	}
	if database.GuestVisible {
		t.Fatal("provisioning database is guest-visible, want hidden by default")
	}
	reloaded, err := store.GetDatabase(context.Background(), database.ID)
	if err != nil || reloaded.GuestVisible {
		t.Fatalf("reloaded provisioning database = %#v, error = %v", reloaded, err)
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
	if _, err := store.UpdateGuestVisibility(ctx, "database_00000000000000000000000000000000", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateGuestVisibility() error = %v, want ErrNotFound", err)
	}
}

func TestGuestVisibilityUpdateDoesNotResurrectDeletedDatabase(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	database, err := store.CreateDatabaseMetadata(ctx, "Delete Me")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	if err := store.DeleteDatabaseMetadata(ctx, database.ID); err != nil {
		t.Fatalf("DeleteDatabaseMetadata() error = %v", err)
	}
	if _, err := store.UpdateGuestVisibility(ctx, database.ID, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateGuestVisibility() after delete error = %v, want ErrNotFound", err)
	}
	if _, err := store.GetDatabase(ctx, database.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDatabase() after delete error = %v, want ErrNotFound", err)
	}
}

func TestConcurrentGuestVisibilityAndLifecycleUpdatesPreserveBothFields(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	database, err := store.CreateDatabaseMetadata(ctx, "Concurrent Updates")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}

	errorsChannel := make(chan error, 2)
	go func() {
		_, err := store.UpdateGuestVisibility(ctx, database.ID, true)
		errorsChannel <- err
	}()
	go func() {
		_, err := store.UpdateDatabaseStatus(ctx, database.ID, StatusReady)
		errorsChannel <- err
	}()
	for range 2 {
		if err := <-errorsChannel; err != nil {
			t.Fatalf("concurrent update error = %v", err)
		}
	}

	reloaded, err := store.GetDatabase(ctx, database.ID)
	if err != nil {
		t.Fatalf("GetDatabase() error = %v", err)
	}
	if !reloaded.GuestVisible || reloaded.Status != StatusReady {
		t.Fatalf("concurrent updates lost a field: %#v", reloaded)
	}
}

func TestConcurrentGuestVisibilityUpdateAndDeleteCannotResurrectDatabase(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)

	for attempt := range 16 {
		database, err := store.CreateDatabaseMetadata(ctx, "Concurrent Delete")
		if err != nil {
			t.Fatalf("attempt %d CreateDatabaseMetadata() error = %v", attempt, err)
		}

		deleteErrors := make(chan error, 1)
		visibilityErrors := make(chan error, 1)
		go func() {
			deleteErrors <- store.DeleteDatabaseMetadata(ctx, database.ID)
		}()
		go func() {
			_, err := store.UpdateGuestVisibility(ctx, database.ID, true)
			visibilityErrors <- err
		}()

		if err := <-deleteErrors; err != nil {
			t.Fatalf("attempt %d DeleteDatabaseMetadata() error = %v", attempt, err)
		}
		if err := <-visibilityErrors; err != nil && !errors.Is(err, ErrNotFound) {
			t.Fatalf("attempt %d UpdateGuestVisibility() error = %v", attempt, err)
		}
		if _, err := store.GetDatabase(ctx, database.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("attempt %d database was resurrected: error = %v", attempt, err)
		}
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
	if database.DisplayName != "Existing Metadata" || database.InternalName != internalName || database.RoleName != "" || !database.GuestVisible {
		t.Fatalf("migrated database = %#v", database)
	}
}

func TestMigrationFromV5AddsGuestVisibilityAndPreservesHiddenValue(t *testing.T) {
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
	for _, migration := range migrations[:5] {
		for _, statement := range migration.statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatalf("apply v%d statement: %v", migration.version, err)
			}
		}
		if _, err := db.ExecContext(
			ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			migration.version,
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			t.Fatalf("record v%d migration: %v", migration.version, err)
		}
	}

	const (
		databaseID   = "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		internalName = "mb_db_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		roleName     = "mb_role_cccccccccccccccccccccccccccccccc"
	)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO databases (
			id, display_name, internal_name, status, created_at, updated_at, role_name
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		databaseID,
		"Existing V5 Database",
		internalName,
		StatusReady,
		now,
		now,
		roleName,
	); err != nil {
		t.Fatalf("insert v5 database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v5 database: %v", err)
	}

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() migrated v5 database error = %v", err)
	}
	storeOpen := true
	defer func() {
		if storeOpen {
			_ = store.Close()
		}
	}()

	version, err := store.SchemaVersion(ctx)
	if err != nil || version != CurrentSchemaVersion {
		t.Fatalf("SchemaVersion() = %d, %v; want %d", version, err, CurrentSchemaVersion)
	}
	var guestVisible int
	if err := store.db.QueryRowContext(
		ctx,
		"SELECT guest_visible FROM databases WHERE id = ?",
		databaseID,
	).Scan(&guestVisible); err != nil {
		t.Fatalf("read migrated guest visibility: %v", err)
	}
	if guestVisible != 1 {
		t.Fatalf("migrated guest_visible = %d, want 1", guestVisible)
	}
	if _, err := store.db.ExecContext(
		ctx,
		"UPDATE databases SET guest_visible = 2 WHERE id = ?",
		databaseID,
	); err == nil {
		t.Fatal("guest_visible CHECK constraint accepted 2")
	}

	hidden, err := store.UpdateGuestVisibility(ctx, databaseID, false)
	if err != nil {
		t.Fatalf("UpdateGuestVisibility(false) error = %v", err)
	}
	if hidden.GuestVisible {
		t.Fatal("UpdateGuestVisibility(false) returned a visible database")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close migrated store: %v", err)
	}
	storeOpen = false

	reopened, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen migrated database error = %v", err)
	}
	defer func() { _ = reopened.Close() }()

	database, err := reopened.GetDatabase(ctx, databaseID)
	if err != nil {
		t.Fatalf("GetDatabase() after reopen error = %v", err)
	}
	if database.GuestVisible {
		t.Fatal("hidden guest visibility was reset after reopen")
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

	want := []string{"id", "display_name", "internal_name", "status", "created_at", "updated_at", "role_name", "guest_visible"}
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
