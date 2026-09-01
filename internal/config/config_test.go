package config

import (
	"path/filepath"
	"testing"
)

func TestParseDefaults(t *testing.T) {
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ListenAddress != DefaultListenAddress {
		t.Fatalf("ListenAddress = %q, want %q", cfg.ListenAddress, DefaultListenAddress)
	}
	if cfg.MetadataDBPath != DefaultMetadataDBPath {
		t.Fatalf("MetadataDBPath = %q, want %q", cfg.MetadataDBPath, DefaultMetadataDBPath)
	}
	if cfg.DatabaseSecretRoot != DefaultDatabaseSecretRoot {
		t.Fatalf("DatabaseSecretRoot = %q, want %q", cfg.DatabaseSecretRoot, DefaultDatabaseSecretRoot)
	}
	if cfg.BackupRoot != DefaultBackupRoot {
		t.Fatalf("BackupRoot = %q, want %q", cfg.BackupRoot, DefaultBackupRoot)
	}
	if cfg.RunDueBackups {
		t.Fatal("RunDueBackups = true, want false")
	}
	if cfg.FrontendDir != DefaultFrontendDir {
		t.Fatalf("FrontendDir = %q, want %q", cfg.FrontendDir, DefaultFrontendDir)
	}
}

func TestParseOverrides(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metadata.db")
	secretRoot := filepath.Join(t.TempDir(), "database-secrets")
	backupRoot := filepath.Join(t.TempDir(), "backups")
	frontendDir := filepath.Join(t.TempDir(), "frontend")
	cfg, err := Parse([]string{"-listen", "127.0.0.1:0", "-metadata-db", dbPath, "-database-secrets", secretRoot, "-backup-root", backupRoot, "-frontend-dir", frontendDir, "-run-due-backups"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.ListenAddress != "127.0.0.1:0" {
		t.Fatalf("ListenAddress = %q", cfg.ListenAddress)
	}
	if cfg.MetadataDBPath != dbPath {
		t.Fatalf("MetadataDBPath = %q, want %q", cfg.MetadataDBPath, dbPath)
	}
	if cfg.DatabaseSecretRoot != secretRoot {
		t.Fatalf("DatabaseSecretRoot = %q, want %q", cfg.DatabaseSecretRoot, secretRoot)
	}
	if cfg.BackupRoot != backupRoot {
		t.Fatalf("BackupRoot = %q, want %q", cfg.BackupRoot, backupRoot)
	}
	if !cfg.RunDueBackups {
		t.Fatal("RunDueBackups = false, want true")
	}
	if cfg.FrontendDir != frontendDir {
		t.Fatalf("FrontendDir = %q, want %q", cfg.FrontendDir, frontendDir)
	}
}

func TestParseRejectsNonLoopbackAddresses(t *testing.T) {
	for _, address := range []string{"0.0.0.0:9100", "[::]:9100", "192.0.2.10:9100", "localhost:9100"} {
		t.Run(address, func(t *testing.T) {
			if _, err := Parse([]string{"-listen", address}); err == nil {
				t.Fatalf("Parse() accepted non-loopback address %q", address)
			}
		})
	}
}

func TestParseAcceptsIPv6Loopback(t *testing.T) {
	if _, err := Parse([]string{"-listen", "[::1]:9100"}); err != nil {
		t.Fatalf("Parse() rejected IPv6 loopback: %v", err)
	}
}
