package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testBackupDatabaseID   = "database_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBackupInternalName = "mb_db_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testBackupRoleName     = "mb_role_cccccccccccccccccccccccccccccccc"
)

func TestBackupMetadataLifecycleAndOrdering(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	database, err := store.CreateDatabaseMetadata(ctx, "Backup Source")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}

	backupIDs := []string{
		"backup_11111111111111111111111111111111",
		"backup_22222222222222222222222222222222",
		"backup_33333333333333333333333333333333",
	}
	index := 0
	store.newBackupID = func() (string, error) {
		value := backupIDs[index]
		index++
		return value, nil
	}
	base := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	now := base
	store.now = func() time.Time { return now }

	manual, err := store.CreateBackup(ctx, database.ID, BackupKindManual)
	if err != nil {
		t.Fatalf("CreateBackup(manual) error = %v", err)
	}
	if manual.Status != BackupStatusCreating || manual.SizeBytes != 0 || manual.CompletedAt != nil {
		t.Fatalf("creating backup = %#v", manual)
	}

	now = base.Add(time.Hour)
	automatic, err := store.CreateBackup(ctx, database.ID, BackupKindAutomatic)
	if err != nil {
		t.Fatalf("CreateBackup(automatic) error = %v", err)
	}
	now = base.Add(2 * time.Hour)
	preRestore, err := store.CreateBackup(ctx, database.ID, BackupKindPreRestore)
	if err != nil {
		t.Fatalf("CreateBackup(pre_restore) error = %v", err)
	}

	now = base.Add(3 * time.Hour)
	ready, err := store.UpdateBackupReady(ctx, manual.ID, 4096)
	if err != nil {
		t.Fatalf("UpdateBackupReady() error = %v", err)
	}
	if ready.Status != BackupStatusReady || ready.SizeBytes != 4096 || ready.CompletedAt == nil || !ready.CompletedAt.Equal(now) {
		t.Fatalf("ready backup = %#v", ready)
	}
	now = base.Add(4 * time.Hour)
	failed, err := store.UpdateBackupError(ctx, automatic.ID)
	if err != nil {
		t.Fatalf("UpdateBackupError() error = %v", err)
	}
	if failed.Status != BackupStatusError || failed.SizeBytes != 0 || failed.CompletedAt == nil {
		t.Fatalf("error backup = %#v", failed)
	}

	got, err := store.GetBackup(ctx, ready.ID)
	if err != nil || got.ID != ready.ID || got.SizeBytes != ready.SizeBytes {
		t.Fatalf("GetBackup() = %#v, %v", got, err)
	}
	listed, err := store.ListBackups(ctx)
	if err != nil {
		t.Fatalf("ListBackups() error = %v", err)
	}
	wantOrder := []string{preRestore.ID, automatic.ID, manual.ID}
	if len(listed) != len(wantOrder) {
		t.Fatalf("ListBackups() length = %d", len(listed))
	}
	for position, wantID := range wantOrder {
		if listed[position].ID != wantID {
			t.Fatalf("ListBackups()[%d].ID = %q, want %q", position, listed[position].ID, wantID)
		}
	}
	forDatabase, err := store.ListBackupsForDatabase(ctx, database.ID)
	if err != nil || len(forDatabase) != 3 {
		t.Fatalf("ListBackupsForDatabase() = %#v, %v", forDatabase, err)
	}

	if _, err := store.UpdateBackupError(ctx, ready.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("second completion update error = %v, want ErrConflict", err)
	}
	if err := store.DeleteBackupMetadata(ctx, preRestore.ID); err != nil {
		t.Fatalf("DeleteBackupMetadata() error = %v", err)
	}
	if _, err := store.GetBackup(ctx, preRestore.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetBackup(deleted) error = %v, want ErrNotFound", err)
	}
}

func TestBackupMetadataValidationAndForeignKeyConstraints(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	database, err := store.CreateDatabaseMetadata(ctx, "Constraint Source")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	store.newBackupID = func() (string, error) {
		return "backup_44444444444444444444444444444444", nil
	}

	for _, test := range []struct {
		name       string
		databaseID string
		kind       BackupKind
	}{
		{name: "invalid database ID", databaseID: "../escape", kind: BackupKindManual},
		{name: "invalid kind", databaseID: database.ID, kind: BackupKind("external")},
		{name: "missing database", databaseID: "database_ffffffffffffffffffffffffffffffff", kind: BackupKindManual},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.CreateBackup(ctx, test.databaseID, test.kind); err == nil {
				t.Fatal("CreateBackup() succeeded")
			}
		})
	}

	backup, err := store.CreateBackup(ctx, database.ID, BackupKindManual)
	if err != nil {
		t.Fatalf("CreateBackup() error = %v", err)
	}
	if _, err := store.UpdateBackupReady(ctx, backup.ID, 0); !errors.Is(err, ErrInvalidBackupSize) {
		t.Fatalf("UpdateBackupReady(0) error = %v", err)
	}
	if _, err := store.GetBackup(ctx, "../escape"); !errors.Is(err, ErrInvalidIdentifier) {
		t.Fatalf("GetBackup(invalid) error = %v", err)
	}
	if err := store.DeleteDatabaseMetadata(ctx, database.ID); err == nil {
		t.Fatal("database metadata deletion succeeded while backup metadata exists")
	}
	if err := store.DeleteBackupMetadata(ctx, backup.ID); err != nil {
		t.Fatalf("DeleteBackupMetadata() error = %v", err)
	}
	if err := store.DeleteDatabaseMetadata(ctx, database.ID); err != nil {
		t.Fatalf("DeleteDatabaseMetadata() after backup removal error = %v", err)
	}
}

func TestBackupSchemaConstraintsAndSafeColumns(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	database, err := store.CreateDatabaseMetadata(ctx, "Schema Source")
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	for _, statement := range []string{
		`INSERT INTO backups (id, database_id, kind, status, size_bytes, created_at) VALUES
			('backup_55555555555555555555555555555555', ?, 'external', 'creating', 0, ?)`,
		`INSERT INTO backups (id, database_id, kind, status, size_bytes, created_at) VALUES
			('backup_66666666666666666666666666666666', ?, 'manual', 'unknown', 0, ?)`,
		`INSERT INTO backups (id, database_id, kind, status, size_bytes, created_at, completed_at) VALUES
			('backup_77777777777777777777777777777777', ?, 'manual', 'ready', 0, ?, ?)`,
	} {
		arguments := []any{database.ID, now}
		if strings.Contains(statement, "completed_at)") {
			arguments = append(arguments, now)
		}
		if _, err := store.db.ExecContext(ctx, statement, arguments...); err == nil {
			t.Fatalf("SQLite accepted invalid backup row: %s", statement)
		}
	}

	rows, err := store.db.QueryContext(ctx, "PRAGMA table_info(backups)")
	if err != nil {
		t.Fatalf("table_info(backups): %v", err)
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
			t.Fatalf("scan backup column: %v", err)
		}
		columns = append(columns, name)
	}
	want := []string{"id", "database_id", "kind", "status", "size_bytes", "created_at", "completed_at"}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("backup columns = %v, want %v", columns, want)
	}
	for _, column := range columns {
		lower := strings.ToLower(column)
		if strings.Contains(lower, "password") || strings.Contains(lower, "credential") || strings.Contains(lower, "path") || strings.Contains(lower, "url") {
			t.Fatalf("unsafe backup column %q", column)
		}
	}
}

func TestMigrationFromV2PreservesDatabaseRowsAndAddsEmptyBackups(t *testing.T) {
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
	for _, migration := range migrations[:2] {
		for _, statement := range migration.statements {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatalf("apply v%d statement: %v", migration.version, err)
			}
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)", migration.version, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("record v%d migration: %v", migration.version, err)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO databases (
			id, display_name, internal_name, status, created_at, updated_at, role_name
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		testBackupDatabaseID,
		"Existing V2 Database",
		testBackupInternalName,
		StatusReady,
		now,
		now,
		testBackupRoleName,
	); err != nil {
		t.Fatalf("insert v2 database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v2 database: %v", err)
	}

	store, err := Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("Open() migrated v2 database error = %v", err)
	}
	defer store.Close()
	version, err := store.SchemaVersion(ctx)
	if err != nil || version != CurrentSchemaVersion {
		t.Fatalf("SchemaVersion() = %d, %v", version, err)
	}
	database, err := store.GetDatabase(ctx, testBackupDatabaseID)
	if err != nil || database.DisplayName != "Existing V2 Database" || database.RoleName != testBackupRoleName {
		t.Fatalf("migrated database = %#v, %v", database, err)
	}
	backups, err := store.ListBackups(ctx)
	if err != nil || len(backups) != 0 {
		t.Fatalf("migrated backups = %#v, %v", backups, err)
	}
}
