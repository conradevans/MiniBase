package explorer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDockerPostgresListsSchemasAndObjectTypes(t *testing.T) {
	var script string
	postgres := &DockerPostgres{run: func(_ context.Context, databaseName, roleName, sql string) (string, error) {
		if databaseName != testDatabaseName {
			t.Fatalf("database = %q", databaseName)
		}
		if roleName != testRoleName {
			t.Fatalf("role = %q", roleName)
		}
		script = sql
		return strings.Join([]string{
			`{"name":"analytics","objects":[{"name":"monthly","type":"materialized_view"}]}`,
			`{"name":"public","objects":[{"name":"active_users","type":"view"},{"name":"users","type":"table"}]}`,
			`{"name":"unused","objects":[]}`,
		}, "\n"), nil
	}}

	catalog, err := postgres.ListObjects(context.Background(), testDatabaseName, testRoleName)
	if err != nil {
		t.Fatalf("ListObjects() error = %v", err)
	}
	if len(catalog.Schemas) != 3 || catalog.Schemas[0].Name != "analytics" {
		t.Fatalf("catalog = %#v", catalog)
	}
	if catalog.Schemas[0].Objects[0].Type != ObjectTypeMaterializedView ||
		catalog.Schemas[1].Objects[0].Type != ObjectTypeView ||
		catalog.Schemas[1].Objects[1].Type != ObjectTypeTable {
		t.Fatalf("object types = %#v", catalog)
	}
	assertReadOnlyScript(t, script)
	if !strings.Contains(script, "pg_catalog.json_build_object(") ||
		!strings.Contains(script, "pg_catalog.json_agg(") {
		t.Fatalf("catalog built-ins are not schema-qualified: %s", script)
	}
	if !strings.Contains(script, "namespace.nspname <> 'information_schema'") ||
		!strings.Contains(script, "namespace.nspname !~ '^pg_'") {
		t.Fatalf("system-schema filters are missing: %s", script)
	}
}

func TestDockerPostgresDescribesColumnsAndPrimaryKeys(t *testing.T) {
	var script string
	postgres := &DockerPostgres{run: func(_ context.Context, _, _ string, sql string) (string, error) {
		script = sql
		return `{"name":"users","type":"table","columns":[` +
			`{"name":"id","dataType":"bigint","nullable":false,"primaryKey":true,"ordinalPosition":1,"binary":false,"json":false},` +
			`{"name":"payload","dataType":"jsonb","nullable":true,"primaryKey":false,"ordinalPosition":2,"binary":false,"json":true}]}`, nil
	}}
	description, err := postgres.DescribeObject(
		context.Background(), testDatabaseName, testRoleName, "public", "users",
	)
	if err != nil {
		t.Fatalf("DescribeObject() error = %v", err)
	}
	if description.Object.Type != ObjectTypeTable || len(description.Columns) != 2 {
		t.Fatalf("description = %#v", description)
	}
	if !description.Columns[0].PrimaryKey || description.Columns[0].Nullable ||
		description.Columns[1].DataType != "jsonb" || !description.Columns[1].Nullable {
		t.Fatalf("columns = %#v", description.Columns)
	}
	if !strings.Contains(script, "pg_catalog.json_build_object(") ||
		!strings.Contains(script, "pg_catalog.json_agg(") {
		t.Fatalf("description built-ins are not schema-qualified: %s", script)
	}
}

func TestDockerPostgresRejectsUnknownSchemasAndObjects(t *testing.T) {
	postgres := &DockerPostgres{run: func(context.Context, string, string, string) (string, error) {
		return "\n", nil
	}}
	for _, missing := range []struct{ schema, object string }{
		{"missing", "users"},
		{"public", "missing"},
	} {
		if _, err := postgres.DescribeObject(
			context.Background(), testDatabaseName, testRoleName, missing.schema, missing.object,
		); !errors.Is(err, ErrObjectNotFound) {
			t.Fatalf("missing %s.%s error = %v", missing.schema, missing.object, err)
		}
	}
}

func TestDockerPostgresBrowsesBoundedRowsAndQuotesIdentifiers(t *testing.T) {
	var scripts []string
	postgres := &DockerPostgres{run: func(_ context.Context, _, _ string, script string) (string, error) {
		scripts = append(scripts, script)
		if len(scripts) == 1 {
			return `{"name":"select\" table","type":"view","columns":[` +
				`{"name":"id","dataType":"integer","nullable":false,"primaryKey":false,"ordinalPosition":1,"binary":false,"json":false},` +
				`{"name":"payload","dataType":"jsonb","nullable":true,"primaryKey":false,"ordinalPosition":2,"binary":false,"json":true},` +
				`{"name":"blob","dataType":"bytea","nullable":true,"primaryKey":false,"ordinalPosition":3,"binary":true,"json":false}]}`, nil
		}
		rows := make([]string, 0, 26)
		for index := 0; index < 26; index++ {
			rows = append(rows, fmt.Sprintf(
				`[{"kind":"value","value":"%d","truncated":false},{"kind":"json","value":"{\"ok\":true}","truncated":%t},{"kind":"binary","value":"<binary: 12 bytes>","truncated":false}]`,
				index,
				index == 0,
			))
		}
		return strings.Join(rows, "\n"), nil
	}}

	page, err := postgres.BrowseRows(
		context.Background(),
		testDatabaseName,
		testRoleName,
		"Odd Schema",
		`select" table`,
		25,
		25,
	)
	if err != nil {
		t.Fatalf("BrowseRows() error = %v", err)
	}
	if len(page.Rows) != 25 || !page.HasMore || page.Offset != 25 || page.Limit != 25 {
		t.Fatalf("page = %#v", page)
	}
	if page.Rows[0][1].Kind != CellKindJSON || !page.Rows[0][1].Truncated ||
		page.Rows[0][2].Value != "<binary: 12 bytes>" {
		t.Fatalf("cells = %#v", page.Rows[0])
	}
	if !strings.Contains(scripts[1], `FROM "Odd Schema"."select"" table" LIMIT 26 OFFSET 25`) {
		t.Fatalf("quoted row query = %s", scripts[1])
	}
	if strings.Contains(scripts[1], "encode(") || strings.Contains(scripts[1], "base64") {
		t.Fatalf("binary query serializes data: %s", scripts[1])
	}
	for _, qualifiedBuiltin := range []string{
		"pg_catalog.json_build_array(",
		"pg_catalog.json_build_object(",
		"pg_catalog.length(",
		"pg_catalog.left(",
		"pg_catalog.octet_length(",
	} {
		if !strings.Contains(scripts[1], qualifiedBuiltin) {
			t.Fatalf("row query lacks %s: %s", qualifiedBuiltin, scripts[1])
		}
	}
	assertReadOnlyScript(t, scripts[0])
	assertReadOnlyScript(t, scripts[1])
}

func TestIdentifierQuotingContainsInjectionLikeNames(t *testing.T) {
	quoted, err := quoteIdentifier(`users"; DROP TABLE public.safe; --`)
	if err != nil {
		t.Fatalf("quoteIdentifier() error = %v", err)
	}
	if quoted != `"users""; DROP TABLE public.safe; --"` {
		t.Fatalf("quoted identifier = %q", quoted)
	}
	literal, err := quoteSQLLiteral(`name'; DROP SCHEMA public; --`)
	if err != nil {
		t.Fatalf("quoteSQLLiteral() error = %v", err)
	}
	if literal != `E'name''; DROP SCHEMA public; --'` {
		t.Fatalf("quoted literal = %q", literal)
	}
}

func TestDockerPostgresPreservesSafeFailures(t *testing.T) {
	for _, expected := range []error{ErrQueryTimeout, ErrResultTooLarge, ErrPostgresOperation} {
		postgres := &DockerPostgres{run: func(context.Context, string, string, string) (string, error) {
			return "", expected
		}}
		_, err := postgres.ListObjects(context.Background(), testDatabaseName, testRoleName)
		if !errors.Is(err, expected) && !(errors.Is(expected, ErrPostgresOperation) && errors.Is(err, ErrPostgresOperation)) {
			t.Fatalf("error = %v, want %v", err, expected)
		}
	}
}

func TestLimitedBufferBoundsCommandOutput(t *testing.T) {
	buffer := &limitedBuffer{limit: 4}
	if written, err := buffer.Write([]byte("123456")); err != nil || written != 6 {
		t.Fatalf("Write() = %d, %v", written, err)
	}
	if buffer.String() != "1234" || !buffer.overflow {
		t.Fatalf("buffer = %q overflow=%t", buffer.String(), buffer.overflow)
	}
}

func assertReadOnlyScript(t *testing.T, script string) {
	t.Helper()
	position := -1
	for _, statement := range []string{
		"BEGIN TRANSACTION READ ONLY;",
		"SET LOCAL search_path = pg_catalog;",
		"SET LOCAL statement_timeout = '5000ms';",
		"COMMIT;",
	} {
		next := strings.Index(script, statement)
		if next <= position {
			t.Fatalf("query lacks ordered read-only search-path and timeout envelope at %q: %s", statement, script)
		}
		position = next
	}
}
