package explorer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/conradevans/MiniBase/internal/ids"
	"github.com/conradevans/MiniBase/internal/provisioning"
)

func TestRealPostgresExplorerReadOnlyAcceptance(t *testing.T) {
	if os.Getenv("MINIBASE_INTEGRATION") != "1" {
		t.Skip("set MINIBASE_INTEGRATION=1 to run real PostgreSQL Explorer acceptance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	databaseName, err := ids.DatabaseInternalName()
	if err != nil {
		t.Fatalf("generate database name: %v", err)
	}
	roleName, err := ids.RoleInternalName()
	if err != nil {
		t.Fatalf("generate role name: %v", err)
	}

	admin := provisioning.NewDockerPostgres()
	if err := admin.CreateRole(ctx, roleName, randomExplorerPassword(t)); err != nil {
		t.Fatalf("create integration role: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := admin.DropDatabase(cleanupContext, databaseName)
		cleanupCancel()
		if err != nil {
			t.Errorf("drop integration database %q: %v", databaseName, err)
		}
		cleanupContext, cleanupCancel = context.WithTimeout(context.Background(), 30*time.Second)
		err = admin.DropRole(cleanupContext, roleName)
		cleanupCancel()
		if err != nil {
			t.Errorf("drop integration role %q: %v", roleName, err)
		}
	})

	roleState, err := admin.InspectRole(ctx, roleName)
	if err != nil {
		t.Fatalf("inspect integration role: %v", err)
	}
	if !roleState.Exists || !roleState.Login || roleState.Superuser || roleState.CreateDB ||
		roleState.CreateRole || roleState.Replication || roleState.BypassRLS {
		t.Fatalf("integration role has unsafe attributes: %#v", roleState)
	}
	if err := admin.CreateDatabase(ctx, databaseName, roleName); err != nil {
		t.Fatalf("create integration database: %v", err)
	}
	if err := admin.ConfigureDatabasePrivileges(ctx, databaseName, roleName); err != nil {
		t.Fatalf("configure integration database: %v", err)
	}

	quotedDatabaseName, err := quoteIdentifier(databaseName)
	if err != nil {
		t.Fatalf("quote integration database name: %v", err)
	}
	setup := fmt.Sprintf(`
CREATE SCHEMA analytics;
CREATE SCHEMA "Odd Schema";
CREATE SCHEMA empty_schema;
CREATE SCHEMA attacker;
CREATE FUNCTION attacker.length(pg_catalog.text)
RETURNS pg_catalog.int4
LANGUAGE plpgsql
AS $function$
BEGIN
  RAISE EXCEPTION 'hostile length invoked';
END
$function$;
CREATE TABLE public.people (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  email text NOT NULL,
  payload jsonb,
  optional text,
  binary_data bytea,
  created_at timestamp with time zone,
  external_id uuid,
  large_text text
);
INSERT INTO public.people (
  email, payload, optional, binary_data, created_at, external_id, large_text
) VALUES (
  'person@example.com',
  '{"active":true,"count":3}',
  NULL,
  pg_catalog.decode(pg_catalog.repeat('ab', 1024), 'hex'),
  '2026-09-17T12:34:56Z',
  '01234567-89ab-cdef-0123-456789abcdef',
  pg_catalog.repeat('é', 70000)
);
CREATE TABLE analytics.numbers (value integer NOT NULL);
INSERT INTO analytics.numbers SELECT pg_catalog.generate_series(1, 60);
CREATE TABLE analytics.partitioned_events (
  id bigint NOT NULL,
  occurred_on date NOT NULL,
  payload text,
  PRIMARY KEY (id, occurred_on)
) PARTITION BY RANGE (occurred_on);
CREATE TABLE analytics.partitioned_events_2026
  PARTITION OF analytics.partitioned_events
  FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');
INSERT INTO analytics.partitioned_events VALUES (7, '2026-09-17', 'partition row');
CREATE VIEW public.active_people AS SELECT id, email, payload FROM public.people;
CREATE MATERIALIZED VIEW "Odd Schema"."Monthly Summary" AS
  SELECT pg_catalog.count(*)::bigint AS total FROM public.people;
CREATE TABLE "Odd Schema"."select table" ("mixed Case" text);
INSERT INTO "Odd Schema"."select table" VALUES ('quoted identifier works');
CREATE FUNCTION public.can_read_privileged_catalog()
RETURNS boolean
LANGUAGE plpgsql
SECURITY INVOKER
SET search_path = pg_catalog
AS $function$
DECLARE
  password_hash text;
BEGIN
  SELECT rolpassword
  INTO password_hash
  FROM pg_catalog.pg_authid
  WHERE rolname = 'minibase_admin';
  RETURN password_hash IS NOT NULL;
EXCEPTION
  WHEN insufficient_privilege THEN
    RETURN false;
END
$function$;
CREATE VIEW public.privilege_probe AS
  SELECT
    current_user::text AS current_role,
    session_user::text AS session_role,
    public.can_read_privileged_catalog() AS can_read_pg_authid;
CREATE VIEW public.slow_view AS
  SELECT value
  FROM analytics.numbers
  WHERE value = 1 AND pg_catalog.pg_sleep(6) IS NULL;
ALTER DATABASE %s SET search_path = attacker, pg_catalog;
`, quotedDatabaseName)
	if _, err := runExplorerPSQL(ctx, databaseName, roleName, setup); err != nil {
		t.Fatalf("create Explorer fixtures: %v", err)
	}

	searchPath, err := runExplorerPSQL(ctx, databaseName, roleName, "SHOW search_path;")
	if err != nil {
		t.Fatalf("read hostile search_path: %v", err)
	}
	if strings.TrimSpace(searchPath) != "attacker, pg_catalog" {
		t.Fatalf("database search_path = %q", strings.TrimSpace(searchPath))
	}

	postgres := NewDockerPostgres()
	catalog, err := postgres.ListObjects(ctx, databaseName, roleName)
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
	for _, systemSchema := range []string{"pg_catalog", "information_schema", "pg_toast"} {
		if findSchema(catalog, systemSchema) != nil {
			t.Fatalf("catalog exposed system schema %q", systemSchema)
		}
	}
	for _, applicationSchema := range []string{"analytics", "attacker", "empty_schema", "Odd Schema", "public"} {
		if findSchema(catalog, applicationSchema) == nil {
			t.Fatalf("catalog omitted schema %q: %#v", applicationSchema, catalog)
		}
	}
	assertCatalogObject(t, catalog, "public", "people", ObjectTypeTable)
	assertCatalogObject(t, catalog, "public", "active_people", ObjectTypeView)
	assertCatalogObject(t, catalog, "public", "privilege_probe", ObjectTypeView)
	assertCatalogObject(t, catalog, "Odd Schema", "Monthly Summary", ObjectTypeMaterializedView)
	assertCatalogObject(t, catalog, "analytics", "partitioned_events", ObjectTypeTable)

	description, err := postgres.DescribeObject(ctx, databaseName, roleName, "public", "people")
	if err != nil {
		t.Fatalf("DescribeObject() error = %v", err)
	}
	if len(description.Columns) != 8 || description.Columns[0].Name != "id" ||
		!description.Columns[0].PrimaryKey || description.Columns[0].Nullable ||
		description.Columns[1].DataType != "text" || description.Columns[2].DataType != "jsonb" {
		t.Fatalf("description = %#v", description)
	}

	page, err := postgres.BrowseRows(ctx, databaseName, roleName, "public", "people", 25, 0)
	if err != nil {
		t.Fatalf("BrowseRows() error = %v", err)
	}
	if len(page.Rows) != 1 || page.HasMore {
		t.Fatalf("page = %#v", page)
	}
	cells := page.Rows[0]
	if cells[2].Kind != CellKindJSON || cells[3].Kind != CellKindNull ||
		cells[4].Kind != CellKindBinary || cells[4].Value != "<binary: 1024 bytes>" ||
		cells[5].Kind != CellKindValue || !strings.Contains(cells[5].Value, "2026-09-17 12:34:56") ||
		cells[6].Kind != CellKindValue || cells[6].Value != "01234567-89ab-cdef-0123-456789abcdef" ||
		!cells[7].Truncated || utf8.RuneCountInString(cells[7].Value) != maxCellCharacters ||
		len(cells[7].Value) <= maxCellCharacters {
		t.Fatalf("serialized cells = %#v", cells)
	}

	privilegePage, err := postgres.BrowseRows(
		ctx,
		databaseName,
		roleName,
		"public",
		"privilege_probe",
		25,
		0,
	)
	if err != nil {
		t.Fatalf("browse privilege probe: %v", err)
	}
	if len(privilegePage.Rows) != 1 || len(privilegePage.Rows[0]) != 3 {
		t.Fatalf("privilege page = %#v", privilegePage)
	}
	privilegeCells := privilegePage.Rows[0]
	if privilegeCells[0].Value != roleName || privilegeCells[1].Value != roleName ||
		privilegeCells[2].Value != "false" {
		t.Fatalf("privilege boundary failed: %#v", privilegeCells)
	}

	partitionDescription, err := postgres.DescribeObject(
		ctx,
		databaseName,
		roleName,
		"analytics",
		"partitioned_events",
	)
	if err != nil {
		t.Fatalf("describe partitioned parent: %v", err)
	}
	if partitionDescription.Object.Type != ObjectTypeTable || len(partitionDescription.Columns) != 3 ||
		partitionDescription.Columns[0].Name != "id" || !partitionDescription.Columns[0].PrimaryKey ||
		partitionDescription.Columns[1].Name != "occurred_on" || !partitionDescription.Columns[1].PrimaryKey {
		t.Fatalf("partitioned parent description = %#v", partitionDescription)
	}
	partitionPage, err := postgres.BrowseRows(
		ctx,
		databaseName,
		roleName,
		"analytics",
		"partitioned_events",
		25,
		0,
	)
	if err != nil {
		t.Fatalf("browse partitioned parent: %v", err)
	}
	if len(partitionPage.Rows) != 1 || partitionPage.Rows[0][0].Value != "7" ||
		partitionPage.Rows[0][1].Value != "2026-09-17" ||
		partitionPage.Rows[0][2].Value != "partition row" {
		t.Fatalf("partitioned parent page = %#v", partitionPage)
	}

	for _, missing := range []struct{ schema, object string }{{"missing", "people"}, {"public", "missing"}} {
		if _, err := postgres.DescribeObject(
			ctx,
			databaseName,
			roleName,
			missing.schema,
			missing.object,
		); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("missing object %s.%s error = %v", missing.schema, missing.object, err)
		}
	}

	for _, object := range []struct {
		schema string
		name   string
		typeID ObjectType
	}{
		{"public", "active_people", ObjectTypeView},
		{"Odd Schema", "Monthly Summary", ObjectTypeMaterializedView},
		{"Odd Schema", "select table", ObjectTypeTable},
	} {
		objectPage, err := postgres.BrowseRows(
			ctx,
			databaseName,
			roleName,
			object.schema,
			object.name,
			25,
			0,
		)
		if err != nil {
			t.Fatalf("browse %s.%s: %v", object.schema, object.name, err)
		}
		if objectPage.Object.Type != object.typeID || len(objectPage.Rows) != 1 {
			t.Fatalf("object page = %#v", objectPage)
		}
	}

	numbers, err := postgres.BrowseRows(
		ctx,
		databaseName,
		roleName,
		"analytics",
		"numbers",
		25,
		25,
	)
	if err != nil {
		t.Fatalf("browse second page: %v", err)
	}
	if len(numbers.Rows) != 25 || !numbers.HasMore || numbers.Offset != 25 {
		t.Fatalf("second page = %#v", numbers)
	}

	if _, err := postgres.runReadOnly(
		ctx,
		databaseName,
		roleName,
		"INSERT INTO public.people (email) VALUES ('must-not-write');",
	); !errors.Is(err, ErrPostgresOperation) {
		t.Fatalf("write in read-only transaction error = %v", err)
	}
	count, err := runExplorerPSQL(
		ctx,
		databaseName,
		roleName,
		"SELECT pg_catalog.count(*) FROM public.people;",
	)
	if err != nil || strings.TrimSpace(count) != "1" {
		t.Fatalf("read-only probe changed rows: count=%q err=%v", count, err)
	}

	if _, err := postgres.BrowseRows(
		ctx,
		databaseName,
		roleName,
		"public",
		"slow_view",
		25,
		0,
	); !errors.Is(err, ErrQueryTimeout) {
		t.Fatalf("slow view error = %v", err)
	}
}

func randomExplorerPassword(t *testing.T) string {
	t.Helper()
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		t.Fatalf("generate integration credential: %v", err)
	}
	return hex.EncodeToString(value)
}

func runExplorerPSQL(
	ctx context.Context,
	databaseName string,
	roleName string,
	sqlInput string,
) (string, error) {
	command := exec.CommandContext(
		ctx,
		"docker",
		"exec",
		"-i",
		defaultContainerName,
		"psql",
		"-X",
		"--no-psqlrc",
		"-v",
		"ON_ERROR_STOP=1",
		"-U",
		roleName,
		"-d",
		databaseName,
		"-A",
		"-t",
		"-q",
	)
	command.Stdin = strings.NewReader(sqlInput)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return "", ErrPostgresOperation
	}
	return output.String(), nil
}

func findSchema(catalog Catalog, name string) *Schema {
	for index := range catalog.Schemas {
		if catalog.Schemas[index].Name == name {
			return &catalog.Schemas[index]
		}
	}
	return nil
}

func assertCatalogObject(t *testing.T, catalog Catalog, schemaName, objectName string, objectType ObjectType) {
	t.Helper()
	schema := findSchema(catalog, schemaName)
	if schema == nil {
		t.Fatalf("schema %q missing", schemaName)
	}
	for _, object := range schema.Objects {
		if object.Name == objectName && object.Type == objectType {
			return
		}
	}
	t.Fatalf("object %s.%s (%s) missing: %#v", schemaName, objectName, objectType, schema.Objects)
}
