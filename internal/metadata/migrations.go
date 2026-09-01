package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const CurrentSchemaVersion = 2

type migration struct {
	version    int
	statements []string
}

var migrations = []migration{
	{
		version: 1,
		statements: []string{
			`CREATE TABLE databases (
				id TEXT PRIMARY KEY
					CHECK (
						length(id) = 41
						AND substr(id, 1, 9) = 'database_'
						AND id = lower(id)
						AND id NOT GLOB '*[^a-z0-9_]*'
					),
				display_name TEXT NOT NULL
					CHECK (
						length(display_name) BETWEEN 1 AND 200
						AND display_name = trim(display_name)
					),
				internal_name TEXT NOT NULL UNIQUE
					CHECK (
						length(internal_name) = 38
						AND substr(internal_name, 1, 6) = 'mb_db_'
						AND internal_name = lower(internal_name)
						AND internal_name NOT GLOB '*[^a-z0-9_]*'
					),
				status TEXT NOT NULL
					CHECK (status IN ('metadata_only', 'provisioning', 'ready', 'error')),
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			)`,
		},
	},
	{
		version: 2,
		statements: []string{
			`ALTER TABLE databases ADD COLUMN role_name TEXT
				CHECK (
					role_name IS NULL
					OR (
						length(role_name) = 40
						AND substr(role_name, 1, 8) = 'mb_role_'
						AND role_name = lower(role_name)
						AND role_name NOT GLOB '*[^a-z0-9_]*'
					)
				)`,
			`CREATE UNIQUE INDEX databases_role_name_unique
				ON databases(role_name)
				WHERE role_name IS NOT NULL`,
		},
	},
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin metadata migration transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("initialize schema migration table: %w", err)
	}

	applied := make(map[int]struct{})
	rows, err := tx.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("read applied schema migrations: %w", err)
	}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan applied schema migration: %w", err)
		}
		if version > CurrentSchemaVersion {
			_ = rows.Close()
			return fmt.Errorf("metadata schema version %d is newer than this binary supports", version)
		}
		applied[version] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close schema migration rows: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied schema migrations: %w", err)
	}

	for _, migration := range migrations {
		if _, exists := applied[migration.version]; exists {
			continue
		}
		for _, statement := range migration.statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply metadata schema migration %d: %w", migration.version, err)
			}
		}
		if _, err := tx.ExecContext(
			ctx,
			"INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)",
			migration.version,
			time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("record metadata schema migration %d: %w", migration.version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit metadata schema migrations: %w", err)
	}
	return nil
}
