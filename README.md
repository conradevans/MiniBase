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
`DATABASE_URL` injection, public/admin authorization, and systemd deployment
remain future work.

## Phase 4: local React dashboard

Phase 4 adds a React/Vite dashboard served by the existing Go control plane.
Build the browser application before starting MiniBase:

```bash
cd frontend
npm ci
npm test -- --run
npm run lint
npm run build
cd ..
go run ./cmd/minibase
```

The default frontend directory is `/srv/minibase/frontend/dist`. Development or
test builds can be selected with the `-frontend-dir` flag. A missing build is
reported safely without affecting the health or API endpoints.

The dashboard routes are:

```text
/                         product landing
/guest                    read-only Guest view
/admin                    administrative overview
/admin/databases          database list and creation
/admin/databases/{id}     safe database metadata and backups
/admin/backups            backup inventory and restore controls
```

The Guest view uses dedicated read-only endpoints with an allowlisted response:

```text
GET /api/v1/guest/status
GET /api/v1/guest/databases
```

Guest database responses contain only `id`, `displayName`, and `status`. The
administrative UI uses the existing Go API and can provision a database from a
display name. Neither UI displays generated roles, passwords, secret paths,
administrator details, or connection strings.

MiniBase remains bound to `127.0.0.1:9100`. The `/guest` and `/admin` names are
navigation only and are **not an authentication boundary** in Phase 4. Access
the dashboard through localhost or SSH port forwarding. Do not route MiniBase
publicly until the future Cloudflare Access and backend authorization phase is
implemented.

Phase 4 intentionally did not implement backups or restores. Phase 5 adds
those workflows while the following remain unavailable:

- database deletion;
- credential display or rotation;
- MiniDeploy database attachment or `DATABASE_URL` injection;
- public routing or authentication.

Generated frontend dependencies, build output, and coverage are ignored by Git.

## Phase 5: PostgreSQL backups and restore

Phase 5 stores PostgreSQL custom-format archives only on the Dell under:

```text
/srv/minibase/backups/<database_id>/<backup_id>.dump
```

Backup IDs are cryptographically random `backup_<32 lowercase hex>` resource
identifiers. The root and per-database directories are mode 0700, final and
temporary archives are mode 0600, creation is verified before an atomic
no-overwrite install, and failed partial archives are removed. SQLite schema
version 3 stores only the safe backup resource ID, database ID, kind, status,
size, and timestamps. It never stores an archive path or credential.

The loopback administrative API adds:

```text
GET  /api/v1/backups
GET  /api/v1/backups/{id}
GET  /api/v1/databases/{id}/backups
POST /api/v1/databases/{id}/backups
POST /api/v1/backups/{id}/restore
```

Manual backups use `pg_dump --format=custom --no-owner --no-acl` inside the
existing `minibase-postgres` container. MiniBase requires a nonempty archive
that `pg_restore --list` can inspect before marking it ready. Restore uses
`--no-owner --no-acl --role <target-role>`, so archived ownership and ACLs
cannot create roles or override MiniBase's isolated per-database owner model.

Restore as new is the safe default: MiniBase provisions a fresh database, role,
and credential, restores the selected archive, reapplies and verifies security
properties, and leaves the source untouched. Replace-current is destructive:
MiniBase first creates and verifies a retained `pre_restore` safety backup,
marks the target unavailable, resets only that target's owned objects, restores
into the same database and role, and preserves its resource ID and credential.
An interrupted or failed replacement remains `error`; startup reconciliation
cannot incorrectly promote it to ready.

Daily automatic eligibility and retention are implemented but no persistent
scheduler is installed. Run the idempotent one-shot operation with:

```bash
go run ./cmd/minibase -run-due-backups
```

It creates no more than one automatic attempt per database per UTC day and
retains seven daily plus four older weekly automatic archives. Manual and
pre-restore backups are never automatically pruned in Phase 5. Configure a
different archive root for development or acceptance with `-backup-root`.

These backups protect against logical and application errors only. Because they
remain on the same Dell, they do **not** protect against SSD or other hardware
failure. Off-Dell/external-drive backup and persistent scheduler activation are
future operational work. Database deletion, credential rotation, MiniDeploy
attachment, `DATABASE_URL` injection, public routing, and authentication also
remain out of scope.
