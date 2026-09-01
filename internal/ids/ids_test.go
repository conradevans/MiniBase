package ids

import (
	"regexp"
	"strings"
	"testing"
)

func TestGeneratedIdentifiers(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		generator func() (string, error)
	}{
		{name: "database ID", prefix: "database_", generator: DatabaseID},
		{name: "backup ID", prefix: "backup_", generator: BackupID},
		{name: "database internal name", prefix: "mb_db_", generator: DatabaseInternalName},
		{name: "role internal name", prefix: "mb_role_", generator: RoleInternalName},
	}

	safePattern := regexp.MustCompile("^[a-z0-9_]+$")
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := test.generator()
			if err != nil {
				t.Fatalf("generator() error = %v", err)
			}
			if !strings.HasPrefix(value, test.prefix) {
				t.Fatalf("value %q does not use prefix %q", value, test.prefix)
			}
			if !safePattern.MatchString(value) {
				t.Fatalf("value %q contains unsafe characters", value)
			}
			suffix := strings.TrimPrefix(value, test.prefix)
			if len(suffix) != 32 {
				t.Fatalf("random suffix length = %d, want 32 hexadecimal characters", len(suffix))
			}
			if len(value) >= 63 {
				t.Fatalf("identifier length = %d, must be below PostgreSQL's limit", len(value))
			}
		})
	}
}

func TestDatabaseIDUniqueness(t *testing.T) {
	const sampleSize = 4096
	seen := make(map[string]struct{}, sampleSize)
	for range sampleSize {
		value, err := DatabaseID()
		if err != nil {
			t.Fatalf("DatabaseID() error = %v", err)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate ID generated: %q", value)
		}
		seen[value] = struct{}{}
	}
}

func TestBackupIDValidationAndUniqueness(t *testing.T) {
	const sampleSize = 4096
	seen := make(map[string]struct{}, sampleSize)
	for range sampleSize {
		value, err := BackupID()
		if err != nil {
			t.Fatalf("BackupID() error = %v", err)
		}
		if !ValidBackupID(value) {
			t.Fatalf("ValidBackupID(%q) = false", value)
		}
		if _, exists := seen[value]; exists {
			t.Fatalf("duplicate backup ID generated: %q", value)
		}
		seen[value] = struct{}{}
	}

	for _, invalid := range []string{"", ".", "..", "backup_", "backup_ABCDEF0123456789abcdef0123456789", "backup_0123456789abcdef0123456789abcdeg", "../backup_0123456789abcdef0123456789abcdef"} {
		if ValidBackupID(invalid) {
			t.Fatalf("ValidBackupID(%q) = true", invalid)
		}
	}
}
