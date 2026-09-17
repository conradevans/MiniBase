package explorer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/conradevans/MiniBase/internal/ids"
)

const (
	defaultContainerName = "minibase-postgres"
	statementTimeout     = 5 * time.Second
	commandTimeout       = 7 * time.Second
	maxCellCharacters    = 64 * 1024
	maxOutputBytes       = 8 * 1024 * 1024
)

var (
	ErrPostgresOperation = errors.New("database explorer operation failed")
	ErrObjectNotFound    = errors.New("database object not found")
	ErrQueryTimeout      = errors.New("database explorer query timed out")
	ErrResultTooLarge    = errors.New("database explorer result is too large")
)

type DockerPostgres struct {
	run func(context.Context, string, string, string) (string, error)
}

type describedColumn struct {
	Column
	Binary bool `json:"binary"`
	JSON   bool `json:"json"`
}

type descriptionWire struct {
	Name    string            `json:"name"`
	Type    ObjectType        `json:"type"`
	Columns []describedColumn `json:"columns"`
}

func NewDockerPostgres() *DockerPostgres {
	runner := &dockerSQLRunner{
		container: defaultContainerName,
		timeout:   commandTimeout,
	}
	return &DockerPostgres{run: runner.execute}
}

func (p *DockerPostgres) ListObjects(
	ctx context.Context,
	databaseName string,
	roleName string,
) (Catalog, error) {
	output, err := p.runReadOnly(ctx, databaseName, roleName, listObjectsQuery)
	if err != nil {
		return Catalog{}, err
	}
	schemas := make([]Schema, 0)
	if err := decodeJSONLines(output, func(line []byte) error {
		var schema Schema
		if err := json.Unmarshal(line, &schema); err != nil {
			return ErrPostgresOperation
		}
		if schema.Objects == nil {
			schema.Objects = make([]Object, 0)
		}
		for _, object := range schema.Objects {
			if !validObjectType(object.Type) {
				return ErrPostgresOperation
			}
		}
		schemas = append(schemas, schema)
		return nil
	}); err != nil {
		return Catalog{}, err
	}
	return Catalog{Schemas: schemas}, nil
}

func (p *DockerPostgres) DescribeObject(
	ctx context.Context,
	databaseName string,
	roleName string,
	schemaName string,
	objectName string,
) (Description, error) {
	description, _, err := p.describeObject(
		ctx,
		databaseName,
		roleName,
		schemaName,
		objectName,
	)
	return description, err
}

func (p *DockerPostgres) BrowseRows(
	ctx context.Context,
	databaseName string,
	roleName string,
	schemaName string,
	objectName string,
	limit int,
	offset int,
) (RowsPage, error) {
	description, columns, err := p.describeObject(
		ctx,
		databaseName,
		roleName,
		schemaName,
		objectName,
	)
	if err != nil {
		return RowsPage{}, err
	}

	quotedSchema, err := quoteIdentifier(description.Schema)
	if err != nil {
		return RowsPage{}, err
	}
	quotedObject, err := quoteIdentifier(description.Object.Name)
	if err != nil {
		return RowsPage{}, err
	}
	expressions := make([]string, 0, len(columns))
	columnNames := make([]string, 0, len(columns))
	for _, column := range columns {
		expression, err := cellExpression(column)
		if err != nil {
			return RowsPage{}, err
		}
		expressions = append(expressions, expression)
		columnNames = append(columnNames, column.Name)
	}

	query := fmt.Sprintf(
		"SELECT pg_catalog.json_build_array(%s)::text FROM %s.%s LIMIT %d OFFSET %d;\n",
		strings.Join(expressions, ", "),
		quotedSchema,
		quotedObject,
		limit+1,
		offset,
	)
	output, err := p.runReadOnly(ctx, databaseName, roleName, query)
	if err != nil {
		return RowsPage{}, err
	}

	rows := make([][]Cell, 0, limit)
	if err := decodeJSONLines(output, func(line []byte) error {
		var row []Cell
		if err := json.Unmarshal(line, &row); err != nil || len(row) != len(columns) {
			return ErrPostgresOperation
		}
		for _, cell := range row {
			if !validCellKind(cell.Kind) {
				return ErrPostgresOperation
			}
		}
		rows = append(rows, row)
		return nil
	}); err != nil {
		return RowsPage{}, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return RowsPage{
		Schema:  description.Schema,
		Object:  description.Object,
		Columns: columnNames,
		Rows:    rows,
		Limit:   limit,
		Offset:  offset,
		HasMore: hasMore,
	}, nil
}

func (p *DockerPostgres) describeObject(
	ctx context.Context,
	databaseName string,
	roleName string,
	schemaName string,
	objectName string,
) (Description, []describedColumn, error) {
	schemaLiteral, err := quoteSQLLiteral(schemaName)
	if err != nil {
		return Description{}, nil, err
	}
	objectLiteral, err := quoteSQLLiteral(objectName)
	if err != nil {
		return Description{}, nil, err
	}
	query := fmt.Sprintf(describeObjectQuery, schemaLiteral, objectLiteral)
	output, err := p.runReadOnly(ctx, databaseName, roleName, query)
	if err != nil {
		return Description{}, nil, err
	}
	lines := nonEmptyLines(output)
	if len(lines) == 0 {
		return Description{}, nil, ErrObjectNotFound
	}
	if len(lines) != 1 {
		return Description{}, nil, ErrPostgresOperation
	}
	var wire descriptionWire
	if err := json.Unmarshal([]byte(lines[0]), &wire); err != nil || !validObjectType(wire.Type) {
		return Description{}, nil, ErrPostgresOperation
	}
	columns := make([]Column, 0, len(wire.Columns))
	for _, column := range wire.Columns {
		columns = append(columns, column.Column)
	}
	return Description{
		Schema:  schemaName,
		Object:  Object{Name: wire.Name, Type: wire.Type},
		Columns: columns,
	}, wire.Columns, nil
}

func (p *DockerPostgres) runReadOnly(
	ctx context.Context,
	databaseName string,
	roleName string,
	query string,
) (string, error) {
	if !ids.ValidDatabaseInternalName(databaseName) || !ids.ValidRoleInternalName(roleName) {
		return "", ErrPostgresOperation
	}
	script := fmt.Sprintf(
		"BEGIN TRANSACTION READ ONLY;\nSET LOCAL search_path = pg_catalog;\nSET LOCAL statement_timeout = '%dms';\n%s\nCOMMIT;\n",
		statementTimeout.Milliseconds(),
		query,
	)
	output, err := p.run(ctx, databaseName, roleName, script)
	if err != nil {
		switch {
		case errors.Is(err, ErrQueryTimeout):
			return "", ErrQueryTimeout
		case errors.Is(err, ErrResultTooLarge):
			return "", ErrResultTooLarge
		default:
			return "", ErrPostgresOperation
		}
	}
	return output, nil
}

func cellExpression(column describedColumn) (string, error) {
	identifier, err := quoteIdentifier(column.Name)
	if err != nil {
		return "", err
	}
	if column.Binary {
		return fmt.Sprintf(
			"CASE WHEN %[1]s IS NULL THEN pg_catalog.json_build_object('kind', 'null', 'truncated', false) ELSE pg_catalog.json_build_object('kind', 'binary', 'value', '<binary: ' || pg_catalog.octet_length(%[1]s)::text || ' bytes>', 'truncated', false) END",
			identifier,
		), nil
	}
	kind := CellKindValue
	if column.JSON {
		kind = CellKindJSON
	}
	return fmt.Sprintf(
		"CASE WHEN %[1]s IS NULL THEN pg_catalog.json_build_object('kind', 'null', 'truncated', false) ELSE pg_catalog.json_build_object('kind', '%[2]s', 'value', CASE WHEN pg_catalog.length(%[1]s::text) > %[3]d THEN pg_catalog.left(%[1]s::text, %[3]d) ELSE %[1]s::text END, 'truncated', pg_catalog.length(%[1]s::text) > %[3]d) END",
		identifier,
		kind,
		maxCellCharacters,
	), nil
}

func quoteIdentifier(value string) (string, error) {
	if !validObjectIdentifier(value) {
		return "", ErrPostgresOperation
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`, nil
}

func quoteSQLLiteral(value string) (string, error) {
	if !validObjectIdentifier(value) {
		return "", ErrPostgresOperation
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `'`, `''`)
	return `E'` + escaped + `'`, nil
}

func decodeJSONLines(output string, decode func([]byte) error) error {
	for _, line := range nonEmptyLines(output) {
		if err := decode([]byte(line)); err != nil {
			return err
		}
	}
	return nil
}

func nonEmptyLines(output string) []string {
	rawLines := strings.Split(output, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func validObjectType(value ObjectType) bool {
	switch value {
	case ObjectTypeTable, ObjectTypeView, ObjectTypeMaterializedView:
		return true
	default:
		return false
	}
}

func validCellKind(value CellKind) bool {
	switch value {
	case CellKindValue, CellKindJSON, CellKindNull, CellKindBinary:
		return true
	default:
		return false
	}
}

type dockerSQLRunner struct {
	container string
	timeout   time.Duration
}

func (runner *dockerSQLRunner) execute(
	ctx context.Context,
	databaseName string,
	roleName string,
	sqlInput string,
) (string, error) {
	operationContext, cancel := context.WithTimeout(ctx, runner.timeout)
	defer cancel()

	command := exec.CommandContext(
		operationContext,
		"docker",
		"exec",
		"-i",
		runner.container,
		"psql",
		"-X",
		"--no-psqlrc",
		"-v",
		"ON_ERROR_STOP=1",
		"-v",
		"VERBOSITY=sqlstate",
		"-U",
		roleName,
		"-d",
		databaseName,
		"-A",
		"-t",
		"-q",
	)
	command.Stdin = strings.NewReader(sqlInput)
	stdout := &limitedBuffer{limit: maxOutputBytes}
	stderr := &limitedBuffer{limit: 4 * 1024}
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Run(); err != nil {
		if operationContext.Err() != nil || strings.Contains(stderr.String(), "57014") {
			return "", ErrQueryTimeout
		}
		return "", ErrPostgresOperation
	}
	if stdout.overflow {
		return "", ErrResultTooLarge
	}
	return stdout.String(), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	overflow bool
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.overflow = true
		return originalLength, nil
	}
	if len(value) > remaining {
		value = value[:remaining]
		buffer.overflow = true
	}
	_, _ = buffer.buffer.Write(value)
	return originalLength, nil
}

func (buffer *limitedBuffer) String() string {
	return buffer.buffer.String()
}

var _ io.Writer = (*limitedBuffer)(nil)

const listObjectsQuery = `
SELECT pg_catalog.json_build_object(
    'name', namespace.nspname,
    'objects', COALESCE((
        SELECT pg_catalog.json_agg(
            pg_catalog.json_build_object(
                'name', relation.relname,
                'type', CASE relation.relkind
                    WHEN 'r' THEN 'table'
                    WHEN 'p' THEN 'table'
                    WHEN 'v' THEN 'view'
                    WHEN 'm' THEN 'materialized_view'
                END
            )
            ORDER BY relation.relname
        )
        FROM pg_catalog.pg_class relation
        WHERE relation.relnamespace = namespace.oid
          AND relation.relkind IN ('r', 'p', 'v', 'm')
    ), '[]'::json)
)::text
FROM pg_catalog.pg_namespace namespace
WHERE namespace.nspname <> 'information_schema'
  AND namespace.nspname !~ '^pg_'
ORDER BY namespace.nspname;
`

const describeObjectQuery = `
SELECT pg_catalog.json_build_object(
    'name', relation.relname,
    'type', CASE relation.relkind
        WHEN 'r' THEN 'table'
        WHEN 'p' THEN 'table'
        WHEN 'v' THEN 'view'
        WHEN 'm' THEN 'materialized_view'
    END,
    'columns', COALESCE((
        SELECT pg_catalog.json_agg(
            pg_catalog.json_build_object(
                'name', attribute.attname,
                'dataType', pg_catalog.format_type(attribute.atttypid, attribute.atttypmod),
                'nullable', NOT attribute.attnotnull,
                'primaryKey', EXISTS (
                    SELECT 1
                    FROM pg_catalog.pg_index index_record
                    WHERE index_record.indrelid = relation.oid
                      AND index_record.indisprimary
                      AND attribute.attnum = ANY(index_record.indkey)
                ),
                'ordinalPosition', attribute.attnum,
                'binary', attribute.atttypid = 'pg_catalog.bytea'::regtype,
                'json', attribute.atttypid IN (
                    'pg_catalog.json'::regtype,
                    'pg_catalog.jsonb'::regtype
                )
            )
            ORDER BY attribute.attnum
        )
        FROM pg_catalog.pg_attribute attribute
        WHERE attribute.attrelid = relation.oid
          AND attribute.attnum > 0
          AND NOT attribute.attisdropped
    ), '[]'::json)
)::text
FROM pg_catalog.pg_class relation
JOIN pg_catalog.pg_namespace namespace
  ON namespace.oid = relation.relnamespace
WHERE namespace.nspname = %s
  AND relation.relname = %s
  AND relation.relkind IN ('r', 'p', 'v', 'm')
  AND namespace.nspname <> 'information_schema'
  AND namespace.nspname !~ '^pg_';
`
