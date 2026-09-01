package backups_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conradevans/MiniBase/internal/backups"
	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/provisioning"
	"github.com/conradevans/MiniBase/internal/secrets"
)

func TestRealPostgresBackupRestoreAcceptance(t *testing.T) {
	if os.Getenv("MINIBASE_INTEGRATION") != "1" {
		t.Skip("set MINIBASE_INTEGRATION=1 to run real PostgreSQL backup acceptance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root := t.TempDir()

	metadataStore, err := metadata.Open(ctx, filepath.Join(root, "metadata", "minibase.db"))
	if err != nil {
		t.Fatalf("open temporary metadata: %v", err)
	}
	defer metadataStore.Close()

	secretRoot := filepath.Join(root, "secrets", "databases")
	credentialStore, err := secrets.New(secretRoot)
	if err != nil {
		t.Fatalf("create temporary credential store: %v", err)
	}
	archiveStore, err := backups.NewFileStore(filepath.Join(root, "backups"))
	if err != nil {
		t.Fatalf("create temporary archive store: %v", err)
	}
	provisioningPostgres := provisioning.NewDockerPostgres()
	provisioningService := provisioning.NewService(metadataStore, credentialStore, provisioningPostgres)
	backupService := backups.NewService(
		metadataStore,
		archiveStore,
		backups.NewDockerPostgres(),
		provisioningService,
	)

	cleaned := false
	defer func() {
		if !cleaned {
			cleanupAcceptanceResources(t, archiveStore, metadataStore, credentialStore, provisioningPostgres)
		}
	}()

	source, err := provisioningService.ProvisionDatabase(ctx, "Phase 5 Backup Source")
	if err != nil {
		t.Fatal("provision backup source failed")
	}
	peer, err := provisioningService.ProvisionDatabase(ctx, "Phase 5 Isolation Peer")
	if err != nil {
		t.Fatal("provision isolation peer failed")
	}
	sourceCredential := readAcceptanceCredential(t, secretRoot, source.ID)
	peerCredential := readAcceptanceCredential(t, secretRoot, peer.ID)

	if _, err := runAcceptancePSQL(
		ctx,
		root,
		source.InternalName,
		source.RoleName,
		sourceCredential,
		"CREATE TABLE backup_probe (value text NOT NULL); INSERT INTO backup_probe (value) VALUES ('original');",
	); err != nil {
		t.Fatal("create source sample data failed")
	}

	backup, err := backupService.CreateBackup(ctx, source.ID)
	if err != nil {
		t.Fatal("create real custom-format backup failed")
	}
	if backup.Status != metadata.BackupStatusReady || backup.SizeBytes <= 0 {
		t.Fatalf("real backup is not ready: status=%q size=%d", backup.Status, backup.SizeBytes)
	}

	if _, err := runAcceptancePSQL(
		ctx,
		root,
		source.InternalName,
		source.RoleName,
		sourceCredential,
		"UPDATE backup_probe SET value = 'mutated-after-backup';",
	); err != nil {
		t.Fatal("mutate source data failed")
	}

	restored, err := backupService.RestoreAsNewDatabase(ctx, backup.ID, "Phase 5 Recovered Copy")
	if err != nil {
		t.Fatal("restore as new database failed")
	}
	restoredCredential := readAcceptanceCredential(t, secretRoot, restored.ID)
	restoredValue := queryAcceptanceValue(t, ctx, root, restored, restoredCredential)
	if restoredValue != "original" {
		t.Fatalf("restored new database value = %q, want original", restoredValue)
	}
	sourceValue := queryAcceptanceValue(t, ctx, root, source, sourceCredential)
	if sourceValue != "mutated-after-backup" {
		t.Fatalf("source changed during restore-as-new: %q", sourceValue)
	}

	if _, err := runAcceptancePSQL(ctx, root, peer.InternalName, source.RoleName, sourceCredential, "SELECT 1;"); err == nil {
		t.Fatal("source application role connected to isolation peer")
	}
	if _, err := runAcceptancePSQL(ctx, root, source.InternalName, peer.RoleName, peerCredential, "SELECT 1;"); err == nil {
		t.Fatal("peer application role connected to source database")
	}

	if _, err := runAcceptancePSQL(
		ctx,
		root,
		source.InternalName,
		source.RoleName,
		sourceCredential,
		"UPDATE backup_probe SET value = 'mutated-before-replace';",
	); err != nil {
		t.Fatal("mutate source before in-place restore failed")
	}
	credentialBeforeReplace := readAcceptanceCredential(t, secretRoot, source.ID)
	replaced, err := backupService.RestoreInPlace(ctx, backup.ID, source.ID)
	if err != nil {
		t.Fatal("in-place restore failed")
	}
	if replaced.ID != source.ID || replaced.InternalName != source.InternalName || replaced.RoleName != source.RoleName {
		t.Fatal("in-place restore changed target resource identity")
	}
	credentialAfterReplace := readAcceptanceCredential(t, secretRoot, source.ID)
	if credentialBeforeReplace != credentialAfterReplace {
		t.Fatal("in-place restore changed application credential")
	}
	if value := queryAcceptanceValue(t, ctx, root, replaced, credentialAfterReplace); value != "original" {
		t.Fatalf("in-place restored value = %q, want original", value)
	}
	assertAcceptancePostgresState(t, ctx, provisioningPostgres, replaced)

	databaseBackups, err := metadataStore.ListBackupsForDatabase(ctx, source.ID)
	if err != nil {
		t.Fatal("list source backups after replacement failed")
	}
	preRestoreReady := 0
	for _, candidate := range databaseBackups {
		if candidate.Kind == metadata.BackupKindPreRestore && candidate.Status == metadata.BackupStatusReady {
			preRestoreReady++
		}
	}
	if preRestoreReady != 1 {
		t.Fatalf("ready pre-restore backup count = %d, want 1", preRestoreReady)
	}

	if _, err := runAcceptancePSQL(ctx, root, source.InternalName, peer.RoleName, peerCredential, "SELECT 1;"); err == nil {
		t.Fatal("isolation failed after in-place restore")
	}
	if _, err := runAcceptancePSQL(ctx, root, peer.InternalName, peer.RoleName, peerCredential, "SELECT 1;"); err != nil {
		t.Fatal("unrelated peer lost own-database access")
	}

	cleanupAcceptanceResources(t, archiveStore, metadataStore, credentialStore, provisioningPostgres)
	assertAcceptanceCleanup(t, ctx, metadataStore, credentialStore, provisioningPostgres, []metadata.Database{source, peer, restored})
	cleaned = true
}

func queryAcceptanceValue(
	t *testing.T,
	ctx context.Context,
	root string,
	database metadata.Database,
	credential string,
) string {
	t.Helper()
	output, err := runAcceptancePSQL(
		ctx,
		root,
		database.InternalName,
		database.RoleName,
		credential,
		"SELECT value FROM backup_probe;",
	)
	if err != nil {
		t.Fatal("application-role restored query failed")
	}
	return strings.TrimSpace(output)
}

func readAcceptanceCredential(t *testing.T, root, databaseID string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, databaseID, "password"))
	if err != nil {
		t.Fatal("read temporary application credential failed")
	}
	value := strings.TrimSpace(string(content))
	if len(value) != 64 {
		t.Fatal("temporary application credential has unexpected length")
	}
	return value
}

func runAcceptancePSQL(
	ctx context.Context,
	root, databaseName, roleName, password, sqlInput string,
) (string, error) {
	pgpass, err := os.CreateTemp(root, ".pgpass-")
	if err != nil {
		return "", errAcceptanceCommand
	}
	pgpassPath := pgpass.Name()
	defer os.Remove(pgpassPath)
	if err := pgpass.Chmod(0o600); err != nil {
		pgpass.Close()
		return "", errAcceptanceCommand
	}
	if _, err := fmt.Fprintf(pgpass, "minibase-postgres:5432:%s:%s:%s\n", databaseName, roleName, password); err != nil {
		pgpass.Close()
		return "", errAcceptanceCommand
	}
	if err := pgpass.Sync(); err != nil {
		pgpass.Close()
		return "", errAcceptanceCommand
	}
	if err := pgpass.Close(); err != nil {
		return "", errAcceptanceCommand
	}

	command := exec.CommandContext(
		ctx,
		"docker", "run", "--rm", "--interactive", "--pull=never",
		"--network", "reactorlab-data",
		"--mount", "type=bind,src="+pgpassPath+",dst=/run/minibase/pgpass,readonly",
		"--env", "PGPASSFILE=/run/minibase/pgpass",
		"postgres:17",
		"psql", "-X", "--no-psqlrc", "-v", "ON_ERROR_STOP=1",
		"-h", "minibase-postgres", "-U", roleName, "-d", databaseName,
		"-A", "-t", "-q", "-f", "-",
	)
	command.Stdin = strings.NewReader(sqlInput)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", errAcceptanceCommand
	}
	return output.String(), nil
}

func cleanupAcceptanceResources(
	t *testing.T,
	archiveStore *backups.FileStore,
	metadataStore *metadata.Store,
	credentialStore *secrets.Store,
	postgres provisioning.Postgres,
) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	backupRecords, err := metadataStore.ListBackups(ctx)
	if err == nil {
		for _, backup := range backupRecords {
			if backup.Status == metadata.BackupStatusReady {
				if err := archiveStore.Delete(backup.DatabaseID, backup.ID); err != nil && !errors.Is(err, backups.ErrArchiveNotFound) {
					t.Errorf("delete acceptance archive failed")
					continue
				}
			} else {
				_ = archiveStore.RemovePartial(backup.DatabaseID, backup.ID)
			}
			if err := metadataStore.DeleteBackupMetadata(ctx, backup.ID); err != nil {
				t.Errorf("delete acceptance backup metadata failed")
			}
		}
	} else {
		t.Errorf("list acceptance backups for cleanup failed")
	}

	databases, err := metadataStore.ListDatabases(ctx)
	if err != nil {
		t.Errorf("list acceptance databases for cleanup failed")
		return
	}
	for _, database := range databases {
		if database.RoleName == "" {
			continue
		}
		if err := postgres.DropDatabase(ctx, database.InternalName); err != nil {
			t.Errorf("drop acceptance database failed")
			continue
		}
		if err := postgres.DropRole(ctx, database.RoleName); err != nil {
			t.Errorf("drop acceptance role failed")
			continue
		}
		if err := credentialStore.Delete(database.ID); err != nil {
			t.Errorf("delete acceptance credential failed")
			continue
		}
		if err := metadataStore.DeleteDatabaseMetadata(ctx, database.ID); err != nil {
			t.Errorf("delete acceptance database metadata failed")
		}
	}
}

func assertAcceptancePostgresState(
	t *testing.T,
	ctx context.Context,
	postgres provisioning.Postgres,
	database metadata.Database,
) {
	t.Helper()
	role, err := postgres.InspectRole(ctx, database.RoleName)
	if err != nil || !role.Exists || !role.Login || role.Superuser || role.CreateDB ||
		role.CreateRole || role.Replication || role.BypassRLS {
		t.Fatal("restored application role security state is invalid")
	}
	state, err := postgres.InspectDatabase(ctx, database.InternalName, database.RoleName)
	if err != nil || !state.Exists || state.Owner != database.RoleName ||
		state.PublicConnect || state.PublicSchemaCreate || !state.OwnerSchemaCreate {
		t.Fatal("restored database security state is invalid")
	}
}

func assertAcceptanceCleanup(
	t *testing.T,
	ctx context.Context,
	metadataStore *metadata.Store,
	credentialStore *secrets.Store,
	postgres provisioning.Postgres,
	databases []metadata.Database,
) {
	t.Helper()
	records, err := metadataStore.ListDatabases(ctx)
	if err != nil || len(records) != 0 {
		t.Fatal("temporary database metadata remains after cleanup")
	}
	backupsAfter, err := metadataStore.ListBackups(ctx)
	if err != nil || len(backupsAfter) != 0 {
		t.Fatal("temporary backup metadata remains after cleanup")
	}
	for _, database := range databases {
		databaseState, err := postgres.InspectDatabase(ctx, database.InternalName, database.RoleName)
		if err != nil || databaseState.Exists {
			t.Fatal("acceptance database remains after cleanup")
		}
		roleState, err := postgres.InspectRole(ctx, database.RoleName)
		if err != nil || roleState.Exists {
			t.Fatal("acceptance role remains after cleanup")
		}
		exists, err := credentialStore.Exists(database.ID)
		if err != nil || exists {
			t.Fatal("acceptance credential remains after cleanup")
		}
	}
}

var errAcceptanceCommand = errors.New("acceptance PostgreSQL command failed")
