# MiniBase

MiniBase is ReactorLab's database control plane.

MiniBase will manage PostgreSQL databases for ReactorLab applications while
keeping the database engine private from the public internet. MiniBase and
MiniDeploy are separate products and services.

## Phase 1: PostgreSQL foundation

Phase 1 provides one shared PostgreSQL 17 server in Docker:

- `minibase-postgres` is attached only to the private, internal
  `reactorlab-data` Docker network.
- PostgreSQL does not publish port 5432, or any other port, on the host.
- PostgreSQL data lives in the external `minibase-postgres-data` volume.
- Recreating the container does not delete the external data volume or the data
  stored in it.
- The administrator password is stored in an ignored, mode-0600 file under
  `secrets/` and is mounted read-only into the container.

Create or verify the Phase 1 foundation with:

```bash
scripts/bootstrap-postgres.sh
scripts/verify-postgres.sh
```

Run these scripts as `conradevans` from any working directory. Neither script
prints the administrator password. The bootstrap script preserves existing
networks, volumes, and credentials and stops if the existing network does not
match the required private architecture.

## Future phases

Application-specific databases and roles are not created in Phase 1. Later
phases will add SQLite control-plane metadata, the MiniBase API and dashboard,
database backups, project/database attachments, and private MiniDeploy
integration. Those features are future architecture, not current behavior.
