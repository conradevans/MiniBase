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

The Go control-plane process uses the standard library HTTP
server. It listens on `127.0.0.1:9100` by default and accepts only loopback
addresses. There is no Caddy route, Cloudflare route, public listener, or systemd
service in Phase 4.

Control-plane metadata is stored in SQLite at
`/srv/minibase/data/minibase.db` by default. SQLite is not an application
database and does not store PostgreSQL passwords, `DATABASE_URL` values, JWT
secrets, API tokens, or other application credentials.

SQLite uses ordered, transactional schema migrations recorded in a
`schema_migrations` table. Phase 2 schema version 1 contains only the `databases`
metadata table. Foreign keys are enabled, WAL journal mode is required, writes
use a busy timeout, and the database and its parent directory are
owner-restricted. Phase 3 schema version 2 adds a nullable generated role name;
nullable storage preserves any metadata-only rows created under version 1.

Database records use immutable cryptographically random resource IDs and
PostgreSQL-safe internal names. Friendly display names remain independent from
those internal identifiers.

The HTTP API exposes health, status, safe database metadata, and one mutation:

```text
POST /api/v1/databases
```

The POST body accepts only `displayName`. Responses never include PostgreSQL role
names, passwords, credential paths, administrator information, or connection
strings. No update or delete endpoint is exposed.

## React dashboard and HTTP boundary

Phase 4 adds a React/Vite browser application under `frontend/`. Vite emits the
production build to `frontend/dist`; generated dependencies, build output, and
coverage are not committed. The Go process serves that build from the directory
selected by `-frontend-dir`, which defaults to
`/srv/minibase/frontend/dist`. Go builds do not depend on an existing frontend
build and no generated files are embedded in the binary.

API dispatch takes precedence over frontend routing. Unknown `/api/` paths
remain JSON 404 responses. The frontend handler serves only regular files from
the configured root, only under `/assets/`, and only the explicit application
routes `/`, `/guest`, `/admin`, `/admin/databases`, and
`/admin/databases/{id}`. Rooted filesystem access, path validation, dotfile
rejection, and the lack of a directory-listing fallback prevent the dashboard
handler from exposing files outside the build directory. A missing build yields
a safe service-unavailable response for dashboard routes while `/health` and API
behavior remain independent.

The dashboard contains an Overview, database list, database detail, and database
creation workflow. Backups and Activity appear only as unavailable future
navigation. Database detail describes future Connections, Backups, Activity,
and Settings capabilities without exposing credentials or presenting deletion
controls.

Guest mode is a server-side response boundary, not merely a React filter. Its
read-only endpoints are:

```text
GET /api/v1/guest/status
GET /api/v1/guest/databases
```

Guest database objects contain exactly `id`, `displayName`, and `status`.
Administrative routes continue to use the Phase 3 APIs, including the real
database provisioning endpoint. Browser response adapters also allowlist known
fields and centralized error handling does not render raw backend details.

MiniBase remains loopback-only. The `/guest` and `/admin` route names do not
provide authentication or authorization. MiniBase must not be publicly routed
until a later Cloudflare Access and backend authorization phase is complete.

## PostgreSQL provisioning

Phase 3 separates provisioning into four responsibilities:

- the HTTP layer validates a bounded JSON request and serializes safe metadata;
- the service coordinates lifecycle, verification, compensation, and startup
  reconciliation;
- the SQLite store persists non-secret control-plane metadata;
- the PostgreSQL and filesystem adapters create database resources and
  credentials.

Each provisioned database receives generated identifiers of the form
`database_<32 lowercase hex>`, `mb_db_<32 lowercase hex>`, and
`mb_role_<32 lowercase hex>`. Display names never become SQL identifiers. The
dedicated role has LOGIN but is explicitly denied superuser, database creation,
role creation, replication, and row-level-security bypass capabilities.

The dedicated role owns its UTF-8 database. MiniBase revokes database privileges
from PUBLIC, grants the owner only the database connection and temporary access
it needs, revokes PUBLIC access to create in the database's `public` schema, and
allows the owner to use and create objects in that schema. Unrelated application
roles therefore cannot rely on default PUBLIC access to connect or create
objects.

PostgreSQL has no published host port. Administrative SQL is sent over stdin to
`psql` through a direct `docker exec -i minibase-postgres ...` process invocation;
no shell is involved and the PostgreSQL administrator password is not read by
the Go process. Host access to Docker is an administrative trust boundary.

## Credential lifecycle

Application passwords contain 32 bytes of cryptographic entropy encoded as 64
lowercase hexadecimal characters. They are never stored in SQLite. The default
credential path is:

```text
/srv/minibase/secrets/databases/<database_id>/password
```

The root and resource directories are mode 0700 and the password file is mode
0600. Creation uses an owner-restricted temporary file, file and directory sync,
and a no-replace atomic rename. Existing credentials are never overwritten.

The creation workflow persists `status=provisioning`, creates the credential,
role, database, and restricted privileges, verifies the resulting PostgreSQL
state, and only then persists `status=ready`. Failures trigger compensation only
for exact generated identifiers belonging to that workflow. If the database,
role, credential, and metadata can all be removed safely, no failed record is
left. If cleanup is incomplete, diagnostic metadata remains with
`status=error`.

Startup reconciliation is deliberately non-destructive. For every record still
in `provisioning`, it checks the generated role, database, ownership and
privileges, and credential-file presence. A complete match becomes `ready`; an
absent, partial, or inconsistent state becomes `error`. Reconciliation does not
recreate, repair, or delete PostgreSQL resources.

## Backup and restore architecture

Schema version 3 adds a `backups` table related to `databases` with a
restricting foreign key. Records contain only generated IDs, `manual`,
`automatic`, or `pre_restore` kind, `creating`, `ready`, or `error`
status, byte size, and timestamps. Archive content and paths remain outside
SQLite; credentials and connection strings never enter backup metadata.

The filesystem adapter derives every archive path solely from validated
MiniBase database and backup IDs. It uses rooted filesystem operations,
owner-restricted directories, a mode-0600 partial archive, custom-format
verification, filesystem sync, and a no-overwrite atomic hard-link install.
Deletion targets exactly one validated metadata-backed archive and never uses a
recursive removal. Retention leaves metadata intact if archive deletion cannot
be confirmed.

The PostgreSQL adapter directly executes `docker exec -i minibase-postgres`
with argument arrays and archive streams on stdin/stdout; it does not invoke a
shell or put passwords in process arguments. Dumps use custom format without
ownership or ACL restoration data. Verification uses `pg_restore --list`.
Restore uses `--single-transaction --no-owner --no-acl --role` to make the
existing MiniBase application role own restored objects without creating roles
or importing archived grants.

Backup and restore workflows are serialized in-process. Manual and automatic
backup creation writes `creating` metadata, dumps and verifies a partial
archive, installs it atomically, then marks metadata `ready`. Restore-as-new
verifies before provisioning, restores into a newly generated isolated
resource, reapplies privilege restrictions, and compensates only that new
resource on failure.

Replace-current always creates and verifies a retained pre-restore safety
backup before destructive work. It marks the target `error` before resetting
objects owned by the exact target role; this intentionally fails closed across
a process crash because Phase 3 startup reconciliation only examines
`provisioning` records. A successful restore reapplies and verifies database,
schema, role, ownership, and PUBLIC restrictions before returning the same
resource to `ready`. Target IDs, internal names, roles, and credentials remain
unchanged.

Automatic eligibility uses UTC calendar days and treats any automatic attempt
on the current day as already handled, making repeated invocations idempotent.
Retention keeps the newest archive for seven distinct daily windows and four
older distinct ISO-week windows. It prunes ready automatic backups only;
manual, pre-restore, creating, and error records are retained. The
`-run-due-backups` CLI mode is suitable for a future timer, but Phase 5 does
not install or claim a persistent scheduler.

The React dashboard exposes a real backup inventory and per-database backup
section. Restore-as-new is selected by default. Replacement requires selecting
the exact ready database and typing its display name, while explaining the
mandatory retained safety backup. Guest endpoints remain database-status only
and expose no backup IDs, sizes, or mutations.

Archives live only under `/srv/minibase/backups` on the Dell. This protects
against logical/application failure, not loss of the Dell SSD or other hardware.
Off-Dell backup is future work.

## Future architecture

The following capabilities are planned and are not implemented in Phase 5:

- MiniDeploy will later request project database attachments privately and
  inject the resulting `DATABASE_URL` into application runtime environments.
- Persistent scheduler activation and off-Dell backup remain future
  operational work.
- Database deletion is intentionally absent until explicit backup and deletion
  safety are designed. Deleting an application will not implicitly delete its
  database.
- A project may have multiple databases, with one database offered as the
  default workflow.
- Standalone databases may be created first and attached to projects later.
- Friendly display names will map to validated, collision-safe internal IDs,
  PostgreSQL database names, and role names.
- Any public management surface requires Cloudflare Access and an explicit
  backend authorization design in a later phase.
