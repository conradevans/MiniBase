package provisioning

import (
	"bytes"
	"context"
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
	postgresAdminUser    = "minibase_admin"
	postgresTimeout      = 30 * time.Second
)

var ErrPostgresOperation = errors.New("PostgreSQL operation failed")

type RoleState struct {
	Exists      bool
	Login       bool
	Superuser   bool
	CreateDB    bool
	CreateRole  bool
	Replication bool
	BypassRLS   bool
}

type DatabaseState struct {
	Exists             bool
	Owner              string
	PublicConnect      bool
	PublicSchemaCreate bool
	OwnerSchemaCreate  bool
}

type Postgres interface {
	CreateRole(context.Context, string, string) error
	CreateDatabase(context.Context, string, string) error
	ConfigureDatabasePrivileges(context.Context, string, string) error
	InspectRole(context.Context, string) (RoleState, error)
	InspectDatabase(context.Context, string, string) (DatabaseState, error)
	DropDatabase(context.Context, string) error
	DropRole(context.Context, string) error
}

type DockerPostgres struct {
	run func(context.Context, string, string) (string, error)
}

func NewDockerPostgres() *DockerPostgres {
	runner := &dockerSQLRunner{
		container: defaultContainerName,
		timeout:   postgresTimeout,
	}
	return &DockerPostgres{run: runner.execute}
}

func (p *DockerPostgres) CreateRole(ctx context.Context, roleName, password string) error {
	if !ids.ValidRoleInternalName(roleName) || !validPassword(password) {
		return fmt.Errorf("%w: invalid generated role or credential", ErrPostgresOperation)
	}
	sql := fmt.Sprintf(
		"CREATE ROLE %s WITH LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;\n",
		quoteIdentifier(roleName),
		quoteSQLLiteral(password),
	)
	if _, err := p.run(ctx, "postgres", sql); err != nil {
		return sanitizedPostgresError(err)
	}
	return nil
}

func (p *DockerPostgres) CreateDatabase(ctx context.Context, databaseName, roleName string) error {
	if !ids.ValidDatabaseInternalName(databaseName) || !ids.ValidRoleInternalName(roleName) {
		return fmt.Errorf("%w: invalid generated identifier", ErrPostgresOperation)
	}
	sql := fmt.Sprintf(
		"CREATE DATABASE %s WITH OWNER %s ENCODING 'UTF8' TEMPLATE template0;\n",
		quoteIdentifier(databaseName),
		quoteIdentifier(roleName),
	)
	if _, err := p.run(ctx, "postgres", sql); err != nil {
		return sanitizedPostgresError(err)
	}
	return nil
}

func (p *DockerPostgres) ConfigureDatabasePrivileges(ctx context.Context, databaseName, roleName string) error {
	if !ids.ValidDatabaseInternalName(databaseName) || !ids.ValidRoleInternalName(roleName) {
		return fmt.Errorf("%w: invalid generated identifier", ErrPostgresOperation)
	}
	databaseSQL := fmt.Sprintf(
		"REVOKE ALL ON DATABASE %s FROM PUBLIC;\nGRANT CONNECT, TEMPORARY ON DATABASE %s TO %s;\n",
		quoteIdentifier(databaseName),
		quoteIdentifier(databaseName),
		quoteIdentifier(roleName),
	)
	if _, err := p.run(ctx, "postgres", databaseSQL); err != nil {
		return sanitizedPostgresError(err)
	}
	schemaSQL := fmt.Sprintf(
		"REVOKE ALL ON SCHEMA public FROM PUBLIC;\nGRANT USAGE, CREATE ON SCHEMA public TO %s;\n",
		quoteIdentifier(roleName),
	)
	if _, err := p.run(ctx, databaseName, schemaSQL); err != nil {
		return sanitizedPostgresError(err)
	}
	return nil
}

func (p *DockerPostgres) InspectRole(ctx context.Context, roleName string) (RoleState, error) {
	if !ids.ValidRoleInternalName(roleName) {
		return RoleState{}, fmt.Errorf("%w: invalid generated role name", ErrPostgresOperation)
	}
	query := fmt.Sprintf(
		"SELECT rolcanlogin, rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls FROM pg_roles WHERE rolname = %s;\n",
		quoteSQLLiteral(roleName),
	)
	output, err := p.run(ctx, "postgres", query)
	if err != nil {
		return RoleState{}, sanitizedPostgresError(err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return RoleState{}, nil
	}
	fields := strings.Split(output, "|")
	if len(fields) != 6 {
		return RoleState{}, ErrPostgresOperation
	}
	values := make([]bool, len(fields))
	for index, field := range fields {
		switch field {
		case "t":
			values[index] = true
		case "f":
			values[index] = false
		default:
			return RoleState{}, ErrPostgresOperation
		}
	}
	return RoleState{
		Exists:      true,
		Login:       values[0],
		Superuser:   values[1],
		CreateDB:    values[2],
		CreateRole:  values[3],
		Replication: values[4],
		BypassRLS:   values[5],
	}, nil
}

func (p *DockerPostgres) InspectDatabase(ctx context.Context, databaseName, roleName string) (DatabaseState, error) {
	if !ids.ValidDatabaseInternalName(databaseName) || !ids.ValidRoleInternalName(roleName) {
		return DatabaseState{}, fmt.Errorf("%w: invalid generated identifier", ErrPostgresOperation)
	}
	databaseQuery := fmt.Sprintf(
		`SELECT pg_get_userbyid(d.datdba),
			EXISTS (
				SELECT 1
				FROM aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) acl
				WHERE acl.grantee = 0 AND acl.privilege_type = 'CONNECT'
			)
		FROM pg_database d
		WHERE d.datname = %s;`+"\n",
		quoteSQLLiteral(databaseName),
	)
	output, err := p.run(ctx, "postgres", databaseQuery)
	if err != nil {
		return DatabaseState{}, sanitizedPostgresError(err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return DatabaseState{}, nil
	}
	fields := strings.Split(output, "|")
	if len(fields) != 2 {
		return DatabaseState{}, ErrPostgresOperation
	}
	publicConnect, err := parsePostgresBool(fields[1])
	if err != nil {
		return DatabaseState{}, err
	}

	schemaQuery := fmt.Sprintf(
		`SELECT
			EXISTS (
				SELECT 1
				FROM pg_namespace namespace
				CROSS JOIN LATERAL aclexplode(
					COALESCE(namespace.nspacl, acldefault('n', namespace.nspowner))
				) acl
				WHERE namespace.nspname = 'public'
					AND acl.grantee = 0
					AND acl.privilege_type = 'CREATE'
			),
			has_schema_privilege(%s, 'public', 'CREATE');`+"\n",
		quoteSQLLiteral(roleName),
	)
	schemaOutput, err := p.run(ctx, databaseName, schemaQuery)
	if err != nil {
		return DatabaseState{}, sanitizedPostgresError(err)
	}
	schemaFields := strings.Split(strings.TrimSpace(schemaOutput), "|")
	if len(schemaFields) != 2 {
		return DatabaseState{}, ErrPostgresOperation
	}
	publicSchemaCreate, err := parsePostgresBool(schemaFields[0])
	if err != nil {
		return DatabaseState{}, err
	}
	ownerSchemaCreate, err := parsePostgresBool(schemaFields[1])
	if err != nil {
		return DatabaseState{}, err
	}

	return DatabaseState{
		Exists:             true,
		Owner:              fields[0],
		PublicConnect:      publicConnect,
		PublicSchemaCreate: publicSchemaCreate,
		OwnerSchemaCreate:  ownerSchemaCreate,
	}, nil
}

func (p *DockerPostgres) DropDatabase(ctx context.Context, databaseName string) error {
	if !ids.ValidDatabaseInternalName(databaseName) {
		return fmt.Errorf("%w: invalid generated database name", ErrPostgresOperation)
	}
	sql := fmt.Sprintf("DROP DATABASE IF EXISTS %s WITH (FORCE);\n", quoteIdentifier(databaseName))
	if _, err := p.run(ctx, "postgres", sql); err != nil {
		return sanitizedPostgresError(err)
	}
	return nil
}

func (p *DockerPostgres) DropRole(ctx context.Context, roleName string) error {
	if !ids.ValidRoleInternalName(roleName) {
		return fmt.Errorf("%w: invalid generated role name", ErrPostgresOperation)
	}
	sql := fmt.Sprintf("DROP ROLE IF EXISTS %s;\n", quoteIdentifier(roleName))
	if _, err := p.run(ctx, "postgres", sql); err != nil {
		return sanitizedPostgresError(err)
	}
	return nil
}

type dockerSQLRunner struct {
	container string
	timeout   time.Duration
}

func (runner *dockerSQLRunner) execute(ctx context.Context, databaseName, sqlInput string) (string, error) {
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
		"-U",
		postgresAdminUser,
		"-d",
		databaseName,
		"-A",
		"-t",
		"-q",
		"-F",
		"|",
	)
	command.Stdin = strings.NewReader(sqlInput)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = io.Discard

	if err := command.Run(); err != nil {
		if operationContext.Err() != nil {
			return "", fmt.Errorf("%w: timed out or canceled", ErrPostgresOperation)
		}
		return "", ErrPostgresOperation
	}
	return stdout.String(), nil
}

func quoteIdentifier(value string) string {
	return `"` + value + `"`
}

func quoteSQLLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func validPassword(password string) bool {
	if len(password) != 64 {
		return false
	}
	for _, character := range password {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func parsePostgresBool(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "t":
		return true, nil
	case "f":
		return false, nil
	default:
		return false, ErrPostgresOperation
	}
}

func sanitizedPostgresError(error) error {
	return ErrPostgresOperation
}
