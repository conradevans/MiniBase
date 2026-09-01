package provisioning_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/conradevans/MiniBase/internal/api"
	"github.com/conradevans/MiniBase/internal/metadata"
	"github.com/conradevans/MiniBase/internal/provisioning"
	"github.com/conradevans/MiniBase/internal/secrets"
)

func TestRealPostgresProvisioningAndHTTPAcceptance(t *testing.T) {
	if os.Getenv("MINIBASE_INTEGRATION") != "1" {
		t.Skip("set MINIBASE_INTEGRATION=1 to run real PostgreSQL acceptance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	root := t.TempDir()
	metadataStore, err := metadata.Open(ctx, filepath.Join(root, "metadata", "minibase.db"))
	if err != nil {
		t.Fatalf("open temporary metadata store: %v", err)
	}
	defer metadataStore.Close()

	secretRoot := filepath.Join(root, "secrets", "databases")
	credentialStore, err := secrets.New(secretRoot)
	if err != nil {
		t.Fatalf("create temporary credential store: %v", err)
	}
	postgres := provisioning.NewDockerPostgres()
	service := provisioning.NewService(metadataStore, credentialStore, postgres)
	handler := api.New(metadataStore, service, nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)))

	first := createDatabaseThroughHTTP(t, handler, "Phase 3 HTTP Acceptance")
	firstInternal, err := metadataStore.GetDatabase(ctx, first.ID)
	if err != nil {
		t.Fatalf("read first internal metadata: %v", err)
	}
	second, err := service.ProvisionDatabase(ctx, "Phase 3 Isolation Peer")
	if err != nil {
		cleanupDatabase(t, ctx, postgres, credentialStore, metadataStore, firstInternal)
		t.Fatalf("provision isolation peer: %v", err)
	}

	cleaned := false
	defer func() {
		if !cleaned {
			cleanupDatabase(t, context.Background(), postgres, credentialStore, metadataStore, second)
			cleanupDatabase(t, context.Background(), postgres, credentialStore, metadataStore, firstInternal)
		}
	}()

	assertPostgresState(t, ctx, postgres, firstInternal)
	assertPostgresState(t, ctx, postgres, second)

	firstPassword := readCredential(t, secretRoot, firstInternal.ID)
	secondPassword := readCredential(t, secretRoot, second.ID)
	if _, err := runApplicationPSQL(
		ctx,
		root,
		firstInternal.InternalName,
		firstInternal.RoleName,
		firstPassword,
		"CREATE TABLE integration_probe (value text NOT NULL); INSERT INTO integration_probe (value) VALUES ('ok');",
	); err != nil {
		t.Fatalf("application-role own-database CREATE/INSERT failed")
	}
	ownOutput, err := runApplicationPSQL(
		ctx,
		root,
		firstInternal.InternalName,
		firstInternal.RoleName,
		firstPassword,
		"SELECT value FROM integration_probe;",
	)
	if err != nil {
		t.Fatalf("application-role own-database SELECT failed")
	}
	if strings.TrimSpace(ownOutput) != "ok" {
		t.Fatalf("application-role query returned unexpected non-secret output")
	}

	if _, err := runApplicationPSQL(
		ctx,
		root,
		second.InternalName,
		firstInternal.RoleName,
		firstPassword,
		"SELECT 1;",
	); err == nil {
		t.Fatal("first application role connected to unrelated database")
	}
	if _, err := runApplicationPSQL(
		ctx,
		root,
		second.InternalName,
		second.RoleName,
		secondPassword,
		"SELECT 1;",
	); err != nil {
		t.Fatal("second application role could not connect to its own database")
	}

	cleanupDatabase(t, ctx, postgres, credentialStore, metadataStore, second)
	cleanupDatabase(t, ctx, postgres, credentialStore, metadataStore, firstInternal)

	for _, database := range []metadata.Database{firstInternal, second} {
		databaseState, err := postgres.InspectDatabase(ctx, database.InternalName, database.RoleName)
		if err != nil {
			t.Fatalf("inspect cleaned database: %v", err)
		}
		if databaseState.Exists {
			t.Fatal("acceptance database remains after cleanup")
		}
		roleState, err := postgres.InspectRole(ctx, database.RoleName)
		if err != nil {
			t.Fatalf("inspect cleaned role: %v", err)
		}
		if roleState.Exists {
			t.Fatal("acceptance role remains after cleanup")
		}
		exists, err := credentialStore.Exists(database.ID)
		if err != nil {
			t.Fatalf("inspect cleaned credential: %v", err)
		}
		if exists {
			t.Fatal("acceptance credential remains after cleanup")
		}
	}
	databases, err := metadataStore.ListDatabases(ctx)
	if err != nil {
		t.Fatalf("list temporary metadata after cleanup: %v", err)
	}
	if len(databases) != 0 {
		t.Fatalf("temporary metadata remains after cleanup: %d records", len(databases))
	}
	cleaned = true
}

func createDatabaseThroughHTTP(t *testing.T, handler http.Handler, displayName string) metadata.Database {
	t.Helper()
	body := fmt.Sprintf(`{"displayName":%q}`, displayName)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/databases", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("HTTP provisioning status = %d", response.Code)
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "password") ||
		strings.Contains(response.Body.String(), "roleName") {
		t.Fatal("HTTP provisioning response exposed credential-related metadata")
	}
	var database metadata.Database
	if err := json.Unmarshal(response.Body.Bytes(), &database); err != nil {
		t.Fatalf("decode HTTP provisioning response: %v", err)
	}
	if database.Status != metadata.StatusReady {
		t.Fatalf("HTTP provisioning status field = %q", database.Status)
	}
	return database
}

func assertPostgresState(t *testing.T, ctx context.Context, postgres provisioning.Postgres, database metadata.Database) {
	t.Helper()
	role, err := postgres.InspectRole(ctx, database.RoleName)
	if err != nil {
		t.Fatalf("inspect application role: %v", err)
	}
	if !role.Exists || !role.Login || role.Superuser || role.CreateDB || role.CreateRole || role.Replication || role.BypassRLS {
		t.Fatalf("application role security state is incorrect: %#v", role)
	}
	state, err := postgres.InspectDatabase(ctx, database.InternalName, database.RoleName)
	if err != nil {
		t.Fatalf("inspect application database: %v", err)
	}
	if !state.Exists || state.Owner != database.RoleName || state.PublicConnect || state.PublicSchemaCreate || !state.OwnerSchemaCreate {
		t.Fatalf("application database security state is incorrect: %#v", state)
	}
}

func readCredential(t *testing.T, secretRoot, databaseID string) string {
	t.Helper()
	credential, err := os.ReadFile(filepath.Join(secretRoot, databaseID, "password"))
	if err != nil {
		t.Fatalf("read temporary application credential: %v", err)
	}
	value := strings.TrimSpace(string(credential))
	if len(value) != 64 {
		t.Fatal("temporary application credential has unexpected length")
	}
	return value
}

func runApplicationPSQL(ctx context.Context, temporaryRoot, databaseName, roleName, password, sqlInput string) (string, error) {
	pgpass, err := os.CreateTemp(temporaryRoot, ".pgpass-")
	if err != nil {
		return "", errorsWithoutSecrets()
	}
	pgpassPath := pgpass.Name()
	defer os.Remove(pgpassPath)
	if err := pgpass.Chmod(0o600); err != nil {
		_ = pgpass.Close()
		return "", errorsWithoutSecrets()
	}
	if _, err := fmt.Fprintf(pgpass, "minibase-postgres:5432:%s:%s:%s\n", databaseName, roleName, password); err != nil {
		_ = pgpass.Close()
		return "", errorsWithoutSecrets()
	}
	if err := pgpass.Sync(); err != nil {
		_ = pgpass.Close()
		return "", errorsWithoutSecrets()
	}
	if err := pgpass.Close(); err != nil {
		return "", errorsWithoutSecrets()
	}

	command := exec.CommandContext(
		ctx,
		"docker",
		"run",
		"--rm",
		"--interactive",
		"--pull=never",
		"--network",
		"reactorlab-data",
		"--mount",
		"type=bind,src="+pgpassPath+",dst=/run/minibase/pgpass,readonly",
		"--env",
		"PGPASSFILE=/run/minibase/pgpass",
		"postgres:17",
		"psql",
		"-X",
		"--no-psqlrc",
		"-v",
		"ON_ERROR_STOP=1",
		"-h",
		"minibase-postgres",
		"-U",
		roleName,
		"-d",
		databaseName,
		"-A",
		"-t",
		"-q",
		"-f",
		"-",
	)
	command.Stdin = strings.NewReader(sqlInput)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", errorsWithoutSecrets()
	}
	return stdout.String(), nil
}

func cleanupDatabase(
	t *testing.T,
	ctx context.Context,
	postgres provisioning.Postgres,
	credentialStore *secrets.Store,
	metadataStore *metadata.Store,
	database metadata.Database,
) {
	t.Helper()
	cleanupContext, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := postgres.DropDatabase(cleanupContext, database.InternalName); err != nil {
		t.Errorf("drop acceptance database failed")
		return
	}
	if err := postgres.DropRole(cleanupContext, database.RoleName); err != nil {
		t.Errorf("drop acceptance role failed")
		return
	}
	if err := credentialStore.Delete(database.ID); err != nil {
		t.Errorf("delete acceptance credential failed")
		return
	}
	if err := metadataStore.DeleteDatabaseMetadata(cleanupContext, database.ID); err != nil {
		t.Errorf("delete acceptance metadata failed")
	}
}

func errorsWithoutSecrets() error {
	return fmt.Errorf("application PostgreSQL command failed")
}
