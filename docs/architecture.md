# MiniBase Architecture

MiniBase is ReactorLab's database control plane. MiniDeploy and MiniBase remain
separate products and services.

## PostgreSQL data plane

Phase 1 runs one shared PostgreSQL 17 server on the Dell. The container is
attached only to the internal `reactorlab-data` bridge network and has no
published host port.

PostgreSQL data is stored in the external `minibase-postgres-data` Docker volume.
Container recreation is distinct from data deletion: removing or replacing the
container must not remove the volume.

## Go control plane and SQLite metadata

Phase 2 introduces a Go control-plane process using the standard library HTTP
server. It listens on `127.0.0.1:9100` by default and accepts only loopback
addresses. There is no Caddy route, Cloudflare route, public listener, or systemd
service in Phase 2.

Control-plane metadata is stored in SQLite at
`/srv/minibase/data/minibase.db` by default. SQLite is not an application
database and does not store PostgreSQL passwords, `DATABASE_URL` values, JWT
secrets, API tokens, or other application credentials.

SQLite uses ordered, transactional schema migrations recorded in a
`schema_migrations` table. Phase 2 schema version 1 contains only the `databases`
metadata table. Foreign keys are enabled, WAL journal mode is required, writes
use a busy timeout, and the database and its parent directory are
owner-restricted.

Database records use immutable cryptographically random resource IDs and
PostgreSQL-safe internal names. Friendly display names remain independent from
those internal identifiers.

The Phase 2 HTTP API exposes health, status, and read-only database metadata.
Metadata mutation is available only through the internal store for tests and
future provisioning workflows; there are no public create, update, or delete
endpoints.

## Future architecture

The following capabilities are planned and are not implemented in Phase 2:

- Phase 3 will provision PostgreSQL application databases and isolated roles.
- MiniDeploy will later request project database attachments privately and
  inject the resulting `DATABASE_URL` into application runtime environments.
- Backups will remain on the Dell and will be managed independently from
  application containers.
- Deleting an application will not implicitly delete its database. Database
  deletion will require a separate, explicit operation.
- A project may have multiple databases, with one database offered as the
  default workflow.
- Standalone databases may be created first and attached to projects later.
- Friendly display names will map to validated, collision-safe internal IDs,
  PostgreSQL database names, and role names.
- A dashboard and any public management surface will be introduced only in a
  later phase with an explicit authorization design.
