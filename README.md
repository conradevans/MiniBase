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
```

Non-loopback listen addresses are rejected. The default SQLite file and its WAL
and shared-memory companions are ignored by Git.

Phase 2 does not create PostgreSQL application databases or roles. PostgreSQL
provisioning comes in Phase 3. The dashboard, backups, public routing, and
MiniDeploy integration also remain future work.
