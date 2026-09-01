package secrets

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const testDatabaseID = "database_0123456789abcdef0123456789abcdef"

func TestCreateAndDeleteCredential(t *testing.T) {
	root := filepath.Join(t.TempDir(), "databases")
	store, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	password, err := store.Create(testDatabaseID)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !regexp.MustCompile("^[0-9a-f]{64}$").MatchString(password) {
		t.Fatal("credential does not have the expected 32-byte lowercase hexadecimal representation")
	}

	assertMode(t, root, 0o700)
	databaseDirectory := filepath.Join(root, testDatabaseID)
	assertMode(t, databaseDirectory, 0o700)
	passwordPath := filepath.Join(databaseDirectory, "password")
	assertMode(t, passwordPath, 0o600)

	storedPassword, err := os.ReadFile(passwordPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(storedPassword)) != password {
		t.Fatal("stored credential does not match generated credential")
	}

	exists, err := store.Exists(testDatabaseID)
	if err != nil || !exists {
		t.Fatalf("Exists() = %v, %v; want true, nil", exists, err)
	}
	readPassword, err := store.Read(testDatabaseID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if readPassword != password {
		t.Fatal("Read() did not return the stored credential")
	}
	if err := os.Chmod(passwordPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(testDatabaseID); err == nil || strings.Contains(err.Error(), password) {
		t.Fatal("Read() accepted unsafe permissions or leaked the credential")
	}
	if err := os.Chmod(passwordPath, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(testDatabaseID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	exists, err = store.Exists(testDatabaseID)
	if err != nil || exists {
		t.Fatalf("Exists() after delete = %v, %v; want false, nil", exists, err)
	}
	if _, err := os.Stat(databaseDirectory); !os.IsNotExist(err) {
		t.Fatalf("database credential directory still exists: %v", err)
	}
}

func TestCreateRefusesOverwriteWithoutLeakingCredential(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	password, err := store.Create(testDatabaseID)
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	secondPassword, err := store.Create(testDatabaseID)
	if !errors.Is(err, ErrCredentialExists) {
		t.Fatalf("second Create() error = %v, want ErrCredentialExists", err)
	}
	if secondPassword != "" {
		t.Fatal("failed Create() returned a credential")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatal("overwrite error leaked credential")
	}
}

func TestConcurrentCreateInstallsExactlyOneCredential(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	type result struct {
		password string
		err      error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			password, err := store.Create(testDatabaseID)
			results <- result{password: password, err: err}
		}()
	}
	close(start)

	var installedPassword string
	var successes, conflicts int
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			successes++
			installedPassword = result.password
		case errors.Is(result.err, ErrCredentialExists):
			conflicts++
			if result.password != "" {
				t.Fatal("conflicting Create() returned a credential")
			}
		default:
			t.Fatalf("concurrent Create() error = %v", result.err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes = %d, conflicts = %d; want 1, 1", successes, conflicts)
	}
	storedPassword, err := os.ReadFile(filepath.Join(root, testDatabaseID, "password"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.TrimSpace(string(storedPassword)) != installedPassword {
		t.Fatal("persisted credential does not match the successful create")
	}
}

func TestCredentialGenerationFailureLeavesNoPartialFile(t *testing.T) {
	root := t.TempDir()
	store, err := New(root)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	store.random = errorReader{}

	if _, err := store.Create(testDatabaseID); err == nil {
		t.Fatal("Create() succeeded with failing random source")
	}
	entries, err := os.ReadDir(filepath.Join(root, testDatabaseID))
	if err != nil {
		t.Fatalf("ReadDir() error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("partial credential artifacts remain: %v", entries)
	}
}

func TestInvalidDatabaseIDRejected(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	for _, databaseID := range []string{"", ".", "..", "../escape", "database_not-hex"} {
		if _, err := store.Create(databaseID); err == nil {
			t.Fatalf("Create(%q) succeeded", databaseID)
		}
		if err := store.Delete(databaseID); err == nil {
			t.Fatalf("Delete(%q) succeeded", databaseID)
		}
	}
}

func assertMode(t *testing.T, path string, expected os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
	if info.Mode().Perm() != expected {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), expected)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}
