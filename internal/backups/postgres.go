package backups

import (
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
	backupContainerName = "minibase-postgres"
	backupAdminUser     = "minibase_admin"
	dumpTimeout         = 10 * time.Minute
	verifyTimeout       = 2 * time.Minute
	restoreTimeout      = 10 * time.Minute
	resetTimeout        = 30 * time.Second
)

var ErrPostgresTool = errors.New("PostgreSQL backup operation failed")

type Postgres interface {
	Dump(context.Context, string, io.Writer) error
	VerifyArchive(context.Context, io.Reader) error
	Restore(context.Context, string, string, io.Reader) error
	ResetDatabase(context.Context, string, string) error
}

type postgresCommandRunner func(context.Context, time.Duration, []string, io.Reader, io.Writer) error

type DockerPostgres struct {
	run postgresCommandRunner
}

func NewDockerPostgres() *DockerPostgres {
	return &DockerPostgres{run: runDockerPostgresCommand}
}

func (postgres *DockerPostgres) Dump(ctx context.Context, databaseName string, output io.Writer) error {
	if !ids.ValidDatabaseInternalName(databaseName) || output == nil {
		return ErrPostgresTool
	}
	arguments := []string{
		"pg_dump",
		"--format=custom",
		"--no-owner",
		"--no-acl",
		"-U",
		backupAdminUser,
		"-d",
		databaseName,
	}
	if err := postgres.run(ctx, dumpTimeout, arguments, nil, output); err != nil {
		return ErrPostgresTool
	}
	return nil
}

func (postgres *DockerPostgres) VerifyArchive(ctx context.Context, archive io.Reader) error {
	if archive == nil {
		return ErrPostgresTool
	}
	arguments := []string{"pg_restore", "--list"}
	if err := postgres.run(ctx, verifyTimeout, arguments, archive, io.Discard); err != nil {
		return ErrPostgresTool
	}
	return nil
}

func (postgres *DockerPostgres) Restore(ctx context.Context, databaseName, roleName string, archive io.Reader) error {
	if !ids.ValidDatabaseInternalName(databaseName) || !ids.ValidRoleInternalName(roleName) || archive == nil {
		return ErrPostgresTool
	}
	arguments := []string{
		"pg_restore",
		"--exit-on-error",
		"--single-transaction",
		"--no-owner",
		"--no-acl",
		"--role",
		roleName,
		"-U",
		backupAdminUser,
		"-d",
		databaseName,
	}
	if err := postgres.run(ctx, restoreTimeout, arguments, archive, io.Discard); err != nil {
		return ErrPostgresTool
	}
	return nil
}

func (postgres *DockerPostgres) ResetDatabase(ctx context.Context, databaseName, roleName string) error {
	if !ids.ValidDatabaseInternalName(databaseName) || !ids.ValidRoleInternalName(roleName) {
		return ErrPostgresTool
	}
	terminateSQL := fmt.Sprintf(
		"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = %s AND pid <> pg_backend_pid();\n",
		quoteLiteral(databaseName),
	)
	terminateArguments := psqlArguments("postgres")
	if err := postgres.run(ctx, resetTimeout, terminateArguments, strings.NewReader(terminateSQL), io.Discard); err != nil {
		return ErrPostgresTool
	}

	resetSQL := fmt.Sprintf("DROP OWNED BY %s CASCADE;\n", quoteIdentifier(roleName))
	if err := postgres.run(ctx, resetTimeout, psqlArguments(databaseName), strings.NewReader(resetSQL), io.Discard); err != nil {
		return ErrPostgresTool
	}
	return nil
}

func runDockerPostgresCommand(
	ctx context.Context,
	timeout time.Duration,
	arguments []string,
	input io.Reader,
	output io.Writer,
) error {
	operationContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	commandArguments := append([]string{"exec", "-i", backupContainerName}, arguments...)
	command := exec.CommandContext(operationContext, "docker", commandArguments...)
	command.Stdin = input
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return ErrPostgresTool
	}
	return nil
}

func psqlArguments(databaseName string) []string {
	return []string{
		"psql",
		"-X",
		"--no-psqlrc",
		"-v",
		"ON_ERROR_STOP=1",
		"-U",
		backupAdminUser,
		"-d",
		databaseName,
		"-q",
		"-f",
		"-",
	}
}

func quoteIdentifier(value string) string {
	return `"` + value + `"`
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}
