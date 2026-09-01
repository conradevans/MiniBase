package provisioning

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const (
	testDatabaseName = "mb_db_0123456789abcdef0123456789abcdef"
	testRoleName     = "mb_role_0123456789abcdef0123456789abcdef"
	testPassword     = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestCreateRoleUsesRestrictedRoleAndPasswordOnlyInSQLInput(t *testing.T) {
	var database, sqlInput string
	postgres := &DockerPostgres{run: func(_ context.Context, gotDatabase, gotSQL string) (string, error) {
		database, sqlInput = gotDatabase, gotSQL
		return "", nil
	}}
	if err := postgres.CreateRole(context.Background(), testRoleName, testPassword); err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if database != "postgres" {
		t.Fatalf("database = %q, want postgres", database)
	}
	for _, clause := range []string{"LOGIN", "NOSUPERUSER", "NOCREATEDB", "NOCREATEROLE", "NOREPLICATION", "NOBYPASSRLS"} {
		if !strings.Contains(sqlInput, clause) {
			t.Fatalf("role SQL is missing %s", clause)
		}
	}
	if !strings.Contains(sqlInput, testPassword) {
		t.Fatal("password was not supplied through SQL stdin")
	}
}

func TestPostgresErrorsAreSanitized(t *testing.T) {
	postgres := &DockerPostgres{run: func(context.Context, string, string) (string, error) {
		return "", errors.New(testPassword)
	}}
	err := postgres.CreateRole(context.Background(), testRoleName, testPassword)
	if !errors.Is(err, ErrPostgresOperation) {
		t.Fatalf("CreateRole() error = %v, want ErrPostgresOperation", err)
	}
	if strings.Contains(err.Error(), testPassword) {
		t.Fatal("PostgreSQL error leaked the application password")
	}
}

func TestInvalidIdentifiersNeverReachRunner(t *testing.T) {
	called := false
	postgres := &DockerPostgres{run: func(context.Context, string, string) (string, error) {
		called = true
		return "", nil
	}}
	if err := postgres.CreateDatabase(context.Background(), "../escape", testRoleName); err == nil {
		t.Fatal("CreateDatabase() accepted invalid name")
	}
	if err := postgres.DropRole(context.Background(), "public"); err == nil {
		t.Fatal("DropRole() accepted invalid name")
	}
	if called {
		t.Fatal("runner was called for invalid identifiers")
	}
}

func TestConfigurePrivileges(t *testing.T) {
	var inputs []string
	postgres := &DockerPostgres{run: func(_ context.Context, _ string, sqlInput string) (string, error) {
		inputs = append(inputs, sqlInput)
		return "", nil
	}}
	if err := postgres.ConfigureDatabasePrivileges(context.Background(), testDatabaseName, testRoleName); err != nil {
		t.Fatalf("ConfigureDatabasePrivileges() error = %v", err)
	}
	combined := strings.Join(inputs, "\n")
	for _, expected := range []string{
		"REVOKE ALL ON DATABASE",
		"FROM PUBLIC",
		"GRANT CONNECT, TEMPORARY",
		"REVOKE ALL ON SCHEMA public FROM PUBLIC",
		"GRANT USAGE, CREATE ON SCHEMA public",
	} {
		if !strings.Contains(combined, expected) {
			t.Fatalf("privilege SQL is missing %q", expected)
		}
	}
}

func TestInspectRoleAndDatabase(t *testing.T) {
	responses := []string{
		"t|f|f|f|f|f\n",
		testRoleName + "|f\n",
		"f|t\n",
	}
	postgres := &DockerPostgres{run: func(context.Context, string, string) (string, error) {
		response := responses[0]
		responses = responses[1:]
		return response, nil
	}}
	role, err := postgres.InspectRole(context.Background(), testRoleName)
	if err != nil {
		t.Fatalf("InspectRole() error = %v", err)
	}
	if !role.Exists || !role.Login || role.Superuser || role.CreateDB || role.CreateRole || role.Replication || role.BypassRLS {
		t.Fatalf("unexpected role state: %#v", role)
	}
	database, err := postgres.InspectDatabase(context.Background(), testDatabaseName, testRoleName)
	if err != nil {
		t.Fatalf("InspectDatabase() error = %v", err)
	}
	if !database.Exists || database.Owner != testRoleName || database.PublicConnect || database.PublicSchemaCreate || !database.OwnerSchemaCreate {
		t.Fatalf("unexpected database state: %#v", database)
	}
}
