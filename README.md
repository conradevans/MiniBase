# MiniBase

MiniBase is ReactorLab's database control plane. MiniBase and MiniDeploy are
separate products and services.

## Phase 1: PostgreSQL foundation

Phase 1 provides one shared PostgreSQL 17 server in Docker:

- `minibase-postgres` is attached only to the private, internal
  `reactorlab-data` Docker network.
- PostgreSQL does not publish port 5432, or any other port, on the host.
- PostgreSQL data lives in the external `minibase-postgres-data` volume.
- Recreating the container does not delete the external data volume or its data.
- The administrator password is stored in an ignored, mode-0600 file under
  `secrets/` and is mounted read-only into the container.

Create or verify that foundation with:

```bash
scripts/bootstrap-postgres.sh
scripts/verify-postgres.sh
```

## Phase 2: local control plane metadata

Phase 2 adds a Go control-plane service and a SQLite metadata database. SQLite
contains resource metadata only; it is separate from application data in
PostgreSQL and stores no PostgreSQL credentials, connection strings, API tokens,
or other application secrets.

The API listens on `127.0.0.1:9100` by default and is intentionally read-only and
loopback-only:

```text
GET /health
GET /api/v1/status
GET /api/v1/databases
GET /api/v1/databases/{id}
```

Build and run it locally with:

```bash
go build ./...
go run ./cmd/minibase
```

Command-line overrides are available for development and tests:

```text
-listen 127.0.0.1:9100
-metadata-db /srv/minibase/data/minibase.db
-database-secrets /srv/minibase/secrets/databases
```

Non-loopback listen addresses are rejected. The default SQLite file and its WAL
and shared-memory companions are ignored by Git.

## Phase 3: PostgreSQL provisioning

Phase 3 adds real PostgreSQL provisioning to the loopback control plane:

```text
POST /api/v1/databases
```

The request accepts only a display name:

```json
{"displayName":"MyScheduler Production"}
```

MiniBase generates an opaque resource ID, one PostgreSQL database, one dedicated
LOGIN role, and a random application password. The safe response contains only
the resource ID, display name, internal database name, lifecycle status, and
timestamps. Role names, credential locations, passwords, and connection strings
are not returned by the API or stored in SQLite.

Application passwords are filesystem secrets at:

```text
/srv/minibase/secrets/databases/<database_id>/password
```

The secret root and per-database directories are mode 0700; password files are
mode 0600 and are created atomically without overwrite. MiniBase provisions
through direct `docker exec -i minibase-postgres psql ...` argument arrays and
SQL on stdin. PostgreSQL remains unexposed, MiniBase does not read the PostgreSQL
administrator password, and access to Docker is therefore an administrative
trust boundary.

Creation records `provisioning` metadata first and marks the resource `ready`
only after the role, database, privileges, ownership, and credential state have
been verified. A failed creation attempts narrowly scoped compensation. If full
cleanup cannot be confirmed, the record is retained with `status=error`.

At startup, MiniBase conservatively reconciles records left in `provisioning`.
Complete and matching resources become `ready`; absent or inconsistent resources
become `error`. Reconciliation never creates, drops, or repairs PostgreSQL
resources.

The create API remains loopback-only. Database deletion is intentionally not
exposed in Phase 3. Backups and delete safety, MiniDeploy integration and
`DATABASE_URL` injection, public/admin routing, systemd deployment, and a React
dashboard remain future work.
