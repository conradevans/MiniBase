package backups

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	testInternalDatabaseName = "mb_db_0123456789abcdef0123456789abcdef"
	testInternalRoleName     = "mb_role_0123456789abcdef0123456789abcdef"
)

type recordedCommand struct {
	timeout time.Duration
	args    []string
	input   string
}

func TestDockerPostgresDumpAndVerifyUseCustomArchiveWithoutShell(t *testing.T) {
	var commands []recordedCommand
	postgres := &DockerPostgres{run: func(_ context.Context, timeout time.Duration, args []string, input io.Reader, output io.Writer) error {
		command := recordedCommand{timeout: timeout, args: append([]string(nil), args...)}
		if input != nil {
			body, err := io.ReadAll(input)
			if err != nil {
				return err
			}
			command.input = string(body)
		}
		commands = append(commands, command)
		if output != nil && args[0] == "pg_dump" {
			_, _ = io.WriteString(output, "PGDMP")
		}
		return nil
	}}

	var dump bytes.Buffer
	if err := postgres.Dump(context.Background(), testInternalDatabaseName, &dump); err != nil {
		t.Fatalf("Dump() error = %v", err)
	}
	if dump.String() != "PGDMP" {
		t.Fatalf("dump output = %q", dump.String())
	}
	if len(commands) != 1 {
		t.Fatalf("dump command count = %d", len(commands))
	}
	assertArgumentsContain(t, commands[0].args, "pg_dump", "--format=custom", "--no-owner", "--no-acl", backupAdminUser, testInternalDatabaseName)
	assertNoShellOrCredentialArguments(t, commands[0].args)

	if err := postgres.VerifyArchive(context.Background(), strings.NewReader(dump.String())); err != nil {
		t.Fatalf("VerifyArchive() error = %v", err)
	}
	if len(commands) != 2 || !slices.Equal(commands[1].args, []string{"pg_restore", "--list"}) {
		t.Fatalf("verify command = %#v", commands)
	}
	if commands[1].input != "PGDMP" {
		t.Fatal("verification did not receive archive on stdin")
	}
}

func TestDockerPostgresRestoreUsesTargetRoleWithoutOwnerOrACL(t *testing.T) {
	var command recordedCommand
	postgres := &DockerPostgres{run: func(_ context.Context, timeout time.Duration, args []string, input io.Reader, _ io.Writer) error {
		body, err := io.ReadAll(input)
		if err != nil {
			return err
		}
		command = recordedCommand{timeout: timeout, args: append([]string(nil), args...), input: string(body)}
		return nil
	}}
	if err := postgres.Restore(context.Background(), testInternalDatabaseName, testInternalRoleName, strings.NewReader("PGDMP")); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	assertArgumentsContain(
		t,
		command.args,
		"pg_restore",
		"--exit-on-error",
		"--single-transaction",
		"--no-owner",
		"--no-acl",
		"--role",
		testInternalRoleName,
		backupAdminUser,
		testInternalDatabaseName,
	)
	assertNoShellOrCredentialArguments(t, command.args)
	if command.input != "PGDMP" {
		t.Fatal("restore did not receive archive on stdin")
	}
}

func TestDockerPostgresResetTerminatesOnlyTargetAndDropsOnlyTargetRoleObjects(t *testing.T) {
	var commands []recordedCommand
	postgres := &DockerPostgres{run: func(_ context.Context, timeout time.Duration, args []string, input io.Reader, _ io.Writer) error {
		body, err := io.ReadAll(input)
		if err != nil {
			return err
		}
		commands = append(commands, recordedCommand{timeout: timeout, args: append([]string(nil), args...), input: string(body)})
		return nil
	}}
	if err := postgres.ResetDatabase(context.Background(), testInternalDatabaseName, testInternalRoleName); err != nil {
		t.Fatalf("ResetDatabase() error = %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("reset command count = %d", len(commands))
	}
	if !strings.Contains(commands[0].input, "pg_terminate_backend") ||
		!strings.Contains(commands[0].input, testInternalDatabaseName) ||
		strings.Contains(commands[0].input, "DROP DATABASE") {
		t.Fatalf("unsafe termination SQL: %q", commands[0].input)
	}
	wantReset := `DROP OWNED BY "` + testInternalRoleName + `" CASCADE;
`
	if commands[1].input != wantReset {
		t.Fatalf("reset SQL = %q, want %q", commands[1].input, wantReset)
	}
	assertArgumentsContain(t, commands[0].args, "psql", "postgres", backupAdminUser)
	assertArgumentsContain(t, commands[1].args, "psql", testInternalDatabaseName, backupAdminUser)
	for _, command := range commands {
		assertNoShellOrCredentialArguments(t, command.args)
	}
}

func TestDockerPostgresRejectsInvalidIdentifiersBeforeExecution(t *testing.T) {
	called := false
	postgres := &DockerPostgres{run: func(context.Context, time.Duration, []string, io.Reader, io.Writer) error {
		called = true
		return nil
	}}
	if err := postgres.Dump(context.Background(), "../escape", io.Discard); !errors.Is(err, ErrPostgresTool) {
		t.Fatalf("Dump(invalid) error = %v", err)
	}
	if err := postgres.Restore(context.Background(), testInternalDatabaseName, "public", strings.NewReader("archive")); !errors.Is(err, ErrPostgresTool) {
		t.Fatalf("Restore(invalid) error = %v", err)
	}
	if err := postgres.ResetDatabase(context.Background(), "postgres", testInternalRoleName); !errors.Is(err, ErrPostgresTool) {
		t.Fatalf("ResetDatabase(invalid) error = %v", err)
	}
	if called {
		t.Fatal("PostgreSQL runner was called for invalid identifiers")
	}
}

func TestDockerPostgresErrorsAreSanitized(t *testing.T) {
	const sensitiveText = "sensitive command stderr"
	postgres := &DockerPostgres{run: func(context.Context, time.Duration, []string, io.Reader, io.Writer) error {
		return errors.New(sensitiveText)
	}}
	err := postgres.Dump(context.Background(), testInternalDatabaseName, io.Discard)
	if !errors.Is(err, ErrPostgresTool) {
		t.Fatalf("Dump() error = %v, want ErrPostgresTool", err)
	}
	if strings.Contains(err.Error(), sensitiveText) {
		t.Fatal("PostgreSQL tool error leaked runner detail")
	}
}

func assertArgumentsContain(t *testing.T, arguments []string, expected ...string) {
	t.Helper()
	for _, value := range expected {
		if !slices.Contains(arguments, value) {
			t.Fatalf("arguments %v do not contain %q", arguments, value)
		}
	}
}

func assertNoShellOrCredentialArguments(t *testing.T, arguments []string) {
	t.Helper()
	for _, argument := range arguments {
		lower := strings.ToLower(argument)
		if argument == "sh" || argument == "bash" || strings.Contains(lower, "password") || strings.Contains(lower, "pgpass") {
			t.Fatalf("unsafe process argument %q in %v", argument, arguments)
		}
	}
}
