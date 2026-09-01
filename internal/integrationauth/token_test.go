package integrationauth

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureIsSecureAndIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets", "token")
	first, err := Ensure(path)
	if err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}
	if len(first) != 64 {
		t.Fatalf("generated token length = %d, want 64 hex characters", len(first))
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %v, %v", info.Mode().Perm(), err)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || parent.Mode().Perm() != 0o700 {
		t.Fatalf("token parent mode = %v, %v", parent.Mode().Perm(), err)
	}
	second, err := Ensure(path)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("Ensure() overwrote or changed an existing token")
	}
	if !Authorized("Bearer "+string(first), first) {
		t.Fatal("valid bearer token rejected")
	}
	for _, header := range []string{"", "Basic abc", "Bearer", "Bearer wrong", "Bearer " + string(first) + " extra"} {
		if Authorized(header, first) {
			t.Fatalf("invalid authorization header accepted: %q", header)
		}
	}
}

func TestLoadRejectsUnsafeTokenFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "token")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 32)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted group/world-readable token")
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("too-short\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() accepted short token")
	}
}
