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
}

func TestParseOverrides(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "metadata.db")
	secretRoot := filepath.Join(t.TempDir(), "database-secrets")
	cfg, err := Parse([]string{"-listen", "127.0.0.1:0", "-metadata-db", dbPath, "-database-secrets", secretRoot})
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
