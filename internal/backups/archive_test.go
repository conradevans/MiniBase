package backups

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testDatabaseID = "database_0123456789abcdef0123456789abcdef"
	testBackupID   = "backup_0123456789abcdef0123456789abcdef"
)

func TestFileStoreCreatesVerifiesOpensAndDeletesRestrictedArchive(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "backups")
	store, err := NewFileStore(rootPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	unrelatedPath := filepath.Join(rootPath, "unrelated.txt")
	if err := os.WriteFile(unrelatedPath, []byte("preserve me"), 0o600); err != nil {
		t.Fatalf("write unrelated file: %v", err)
	}

	archiveBody := "PGDMP-test-archive"
	finalPath := filepath.Join(rootPath, testDatabaseID, testBackupID+".dump")
	size, err := store.Create(
		testDatabaseID,
		testBackupID,
		func(writer io.Writer) error {
			if _, statErr := os.Stat(finalPath); !os.IsNotExist(statErr) {
				t.Fatalf("final archive existed before verification: %v", statErr)
			}
			_, writeErr := io.WriteString(writer, archiveBody)
			return writeErr
		},
		func(reader io.Reader) error {
			body, readErr := io.ReadAll(reader)
			if readErr != nil {
				return readErr
			}
			if string(body) != archiveBody {
				return errors.New("unexpected archive body")
			}
			if _, statErr := os.Stat(finalPath); !os.IsNotExist(statErr) {
				t.Fatalf("final archive existed during verification: %v", statErr)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if size != int64(len(archiveBody)) {
		t.Fatalf("size = %d, want %d", size, len(archiveBody))
	}
	assertFileMode(t, rootPath, 0o700)
	assertFileMode(t, filepath.Join(rootPath, testDatabaseID), 0o700)
	assertFileMode(t, finalPath, 0o600)
	if _, err := os.Stat(filepath.Join(rootPath, testDatabaseID, "."+testBackupID+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("partial archive remains: %v", err)
	}

	archive, err := store.Open(testDatabaseID, testBackupID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	body, err := io.ReadAll(archive)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("archive Close() error = %v", err)
	}
	if string(body) != archiveBody || archive.SizeBytes != size {
		t.Fatalf("opened archive size/body mismatch")
	}

	if err := store.Delete(testDatabaseID, testBackupID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := os.Stat(finalPath); !os.IsNotExist(err) {
		t.Fatalf("archive still exists after Delete(): %v", err)
	}
	unrelated, err := os.ReadFile(unrelatedPath)
	if err != nil || string(unrelated) != "preserve me" {
		t.Fatalf("unrelated file changed: %q, %v", unrelated, err)
	}
}

func TestFileStoreDeletesOnlyOneDatabaseArchiveDirectoryIdempotently(t *testing.T) {
	rootPath := t.TempDir()
	store, err := NewFileStore(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	otherDatabaseID := "database_ffffffffffffffffffffffffffffffff"
	otherBackupID := "backup_ffffffffffffffffffffffffffffffff"
	for _, fixture := range []struct {
		databaseID string
		backupID   string
	}{
		{databaseID: testDatabaseID, backupID: testBackupID},
		{databaseID: otherDatabaseID, backupID: otherBackupID},
	} {
		if _, err := store.Create(fixture.databaseID, fixture.backupID, writeArchive, verifyArchive); err != nil {
			t.Fatal(err)
		}
	}
	partial := filepath.Join(rootPath, testDatabaseID, ".backup_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.tmp")
	if err := os.WriteFile(partial, []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteDatabase(testDatabaseID); err != nil {
		t.Fatalf("DeleteDatabase() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, testDatabaseID)); !os.IsNotExist(err) {
		t.Fatalf("target archive directory remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rootPath, otherDatabaseID, otherBackupID+".dump")); err != nil {
		t.Fatalf("other database archive changed: %v", err)
	}
	if err := store.DeleteDatabase(testDatabaseID); err != nil {
		t.Fatalf("idempotent DeleteDatabase() error = %v", err)
	}
	if err := store.DeleteDatabase("../escape"); !errors.Is(err, ErrArchiveInvalid) {
		t.Fatalf("invalid DeleteDatabase() error = %v", err)
	}
}

func TestFileStoreRefusesOverwriteAndPreservesFirstArchive(t *testing.T) {
	store, err := NewFileStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	create := func(body string) error {
		_, err := store.Create(
			testDatabaseID,
			testBackupID,
			func(writer io.Writer) error {
				_, writeErr := io.WriteString(writer, body)
				return writeErr
			},
			func(io.Reader) error { return nil },
		)
		return err
	}
	if err := create("first"); err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	if err := create("second"); !errors.Is(err, ErrArchiveExists) {
		t.Fatalf("second Create() error = %v, want ErrArchiveExists", err)
	}
	archive, err := store.Open(testDatabaseID, testBackupID)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer archive.Close()
	body, err := io.ReadAll(archive)
	if err != nil || string(body) != "first" {
		t.Fatalf("preserved archive = %q, %v", body, err)
	}
}

func TestFileStoreCleansPartialArchiveOnDumpAndVerifyFailures(t *testing.T) {
	tests := []struct {
		name   string
		dump   func(io.Writer) error
		verify func(io.Reader) error
	}{
		{
			name: "dump failure",
			dump: func(writer io.Writer) error {
				_, _ = io.WriteString(writer, "partial")
				return errors.New("dump failed")
			},
			verify: func(io.Reader) error { return nil },
		},
		{
			name: "verify failure",
			dump: func(writer io.Writer) error {
				_, err := io.WriteString(writer, "invalid archive")
				return err
			},
			verify: func(io.Reader) error { return errors.New("verify failed") },
		},
		{
			name:   "empty archive",
			dump:   func(io.Writer) error { return nil },
			verify: func(io.Reader) error { return nil },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rootPath := t.TempDir()
			store, err := NewFileStore(rootPath)
			if err != nil {
				t.Fatalf("NewFileStore() error = %v", err)
			}
			if _, err := store.Create(testDatabaseID, testBackupID, test.dump, test.verify); err == nil {
				t.Fatal("Create() succeeded")
			}
			databaseDirectory := filepath.Join(rootPath, testDatabaseID)
			entries, err := os.ReadDir(databaseDirectory)
			if err != nil {
				t.Fatalf("ReadDir() error = %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("partial archive artifacts remain: %v", entries)
			}
		})
	}
}

func TestFileStoreRejectsTraversalSymlinksAndInvalidArchiveFiles(t *testing.T) {
	rootPath := t.TempDir()
	store, err := NewFileStore(rootPath)
	if err != nil {
		t.Fatalf("NewFileStore() error = %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	for _, invalidDatabaseID := range []string{"", ".", "..", "../escape", "database_not-hex"} {
		if _, err := store.Create(invalidDatabaseID, testBackupID, writeArchive, verifyArchive); !errors.Is(err, ErrArchiveInvalid) {
			t.Fatalf("Create(%q) error = %v", invalidDatabaseID, err)
		}
		if err := store.Delete(invalidDatabaseID, testBackupID); !errors.Is(err, ErrArchiveInvalid) {
			t.Fatalf("Delete(%q) error = %v", invalidDatabaseID, err)
		}
	}
	for _, invalidBackupID := range []string{"", ".", "..", "../escape", "backup_not-hex"} {
		if _, err := store.Open(testDatabaseID, invalidBackupID); !errors.Is(err, ErrArchiveInvalid) {
			t.Fatalf("Open(%q) error = %v", invalidBackupID, err)
		}
	}

	if err := os.Symlink(filepath.Dir(outside), filepath.Join(rootPath, testDatabaseID)); err != nil {
		t.Fatalf("create database-directory symlink: %v", err)
	}
	if _, err := store.Create(testDatabaseID, testBackupID, writeArchive, verifyArchive); err == nil {
		t.Fatal("Create() followed database-directory symlink")
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "outside" {
		t.Fatalf("outside file changed: %q, %v", body, err)
	}

	if err := os.Remove(filepath.Join(rootPath, testDatabaseID)); err != nil {
		t.Fatalf("remove directory symlink: %v", err)
	}
	if err := os.Mkdir(filepath.Join(rootPath, testDatabaseID), 0o700); err != nil {
		t.Fatalf("create database directory: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(rootPath, testDatabaseID, testBackupID+".dump")); err != nil {
		t.Fatalf("create archive symlink: %v", err)
	}
	if _, err := store.Open(testDatabaseID, testBackupID); !errors.Is(err, ErrArchiveInvalid) {
		t.Fatalf("Open(symlink) error = %v", err)
	}
}

func TestNewFileStoreRejectsSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	link := filepath.Join(base, "backups")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	if _, err := NewFileStore(link); err == nil {
		t.Fatal("NewFileStore() accepted a symlink root")
	}
}

func writeArchive(writer io.Writer) error {
	_, err := io.WriteString(writer, "PGDMP")
	return err
}

func verifyArchive(reader io.Reader) error {
	body, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(string(body), "PGDMP") {
		return ErrArchiveInvalid
	}
	return nil
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}
