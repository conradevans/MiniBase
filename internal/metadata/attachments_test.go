package metadata

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func readyAttachmentDatabase(t *testing.T, store *Store, name string) Database {
	t.Helper()
	database, err := store.CreateDatabaseMetadata(context.Background(), name)
	if err != nil {
		t.Fatalf("CreateDatabaseMetadata() error = %v", err)
	}
	database, err = store.UpdateDatabaseStatus(context.Background(), database.ID, StatusReady)
	if err != nil {
		t.Fatalf("UpdateDatabaseStatus() error = %v", err)
	}
	return database
}

func TestAttachmentLifecycleUniquenessAndIsolation(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	firstDB := readyAttachmentDatabase(t, store, "First")
	secondDB := readyAttachmentDatabase(t, store, "Second")
	ids := []string{
		"attachment_00000000000000000000000000000001",
		"attachment_00000000000000000000000000000002",
		"attachment_00000000000000000000000000000003",
	}
	store.newAttachmentID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}
	created, err := store.CreateAttachment(ctx, firstDB.ID, ConsumerTypeMiniDeploy, "scheduler", BindingNamePrimary)
	if err != nil {
		t.Fatalf("CreateAttachment() error = %v", err)
	}
	if created.ID != "attachment_00000000000000000000000000000001" {
		t.Fatalf("attachment ID = %q", created.ID)
	}
	if _, err := store.CreateAttachment(ctx, secondDB.ID, ConsumerTypeMiniDeploy, "scheduler", BindingNamePrimary); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate consumer binding error = %v, want ErrConflict", err)
	}
	if _, err := store.CreateAttachment(ctx, firstDB.ID, ConsumerTypeMiniDeploy, "other-app", BindingNamePrimary); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate database binding error = %v, want ErrConflict", err)
	}
	got, err := store.GetAttachmentForConsumer(ctx, ConsumerTypeMiniDeploy, "scheduler", BindingNamePrimary)
	if err != nil || got != created {
		t.Fatalf("GetAttachmentForConsumer() = %#v, %v", got, err)
	}
	listed, err := store.ListAttachmentsForDatabase(ctx, firstDB.ID)
	if err != nil || len(listed) != 1 || listed[0] != created {
		t.Fatalf("ListAttachmentsForDatabase() = %#v, %v", listed, err)
	}
	if err := store.DeleteDatabaseMetadata(ctx, firstDB.ID); err == nil {
		t.Fatal("database deletion succeeded while an attachment exists")
	}
	if err := store.DeleteAttachment(ctx, created.ID); err != nil {
		t.Fatalf("DeleteAttachment() error = %v", err)
	}
	if _, err := store.GetDatabase(ctx, firstDB.ID); err != nil {
		t.Fatalf("database was deleted with attachment: %v", err)
	}
}

func TestAttachmentValidationAndReadyRequirement(t *testing.T) {
	ctx := context.Background()
	store, _ := openTestStore(t)
	database, err := store.CreateDatabaseMetadata(ctx, "Not Ready")
	if err != nil {
		t.Fatal(err)
	}
	for _, consumer := range []string{"", "UPPER", "../escape", "-leading", "trailing-"} {
		if _, err := store.CreateAttachment(ctx, database.ID, ConsumerTypeMiniDeploy, consumer, BindingNamePrimary); err == nil {
			t.Fatalf("invalid consumer %q accepted", consumer)
		}
	}
	if _, err := store.CreateAttachment(ctx, database.ID, ConsumerTypeMiniDeploy, "valid-app", BindingNamePrimary); !errors.Is(err, ErrConflict) {
		t.Fatalf("non-ready database error = %v, want ErrConflict", err)
	}
	if _, err := store.GetAttachment(ctx, "attachment_invalid"); !errors.Is(err, ErrInvalidIdentifier) {
		t.Fatalf("invalid attachment ID error = %v", err)
	}
}

func TestSchemaV3ToV4PreservesDatabaseAndBackupMetadata(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "minibase.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	database, err := store.CreateDatabaseMetadata(ctx, "Preserved")
	if err != nil {
		t.Fatal(err)
	}
	backup, err := store.CreateBackup(ctx, database.ID, BackupKindManual)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE attachments"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version=4"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("v3 to v4 Open() error = %v", err)
	}
	defer reopened.Close()
	if version, err := reopened.SchemaVersion(ctx); err != nil || version != 4 {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	if got, err := reopened.GetDatabase(ctx, database.ID); err != nil || got.ID != database.ID {
		t.Fatalf("preserved database = %#v, %v", got, err)
	}
	if got, err := reopened.GetBackup(ctx, backup.ID); err != nil || got.ID != backup.ID {
		t.Fatalf("preserved backup = %#v, %v", got, err)
	}
}
