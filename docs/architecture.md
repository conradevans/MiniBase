# MiniBase Architecture

## Current Phase 1 architecture

MiniBase is ReactorLab's database control plane. PostgreSQL is its database
engine. MiniDeploy and MiniBase remain separate products and services.

Phase 1 runs one shared PostgreSQL 17 server on the Dell. The container is
attached to the internal `reactorlab-data` bridge network and has no published
host port. ReactorLab application containers will eventually connect over that
private network rather than through a public database endpoint.

PostgreSQL data is stored in the external `minibase-postgres-data` Docker
volume. Container recreation is therefore distinct from data deletion: removing
or replacing the container must not remove the volume.

## Future architecture

The following capabilities are planned and are not implemented in Phase 1:

- Each application database will receive an isolated PostgreSQL role and
  credentials with access limited to that database.
- MiniBase control-plane metadata will use SQLite, separately from application
  data stored in PostgreSQL.
- MiniDeploy will privately request project database attachments and inject the
  resulting `DATABASE_URL` into application runtime environments.
- Backups will remain on the Dell and will be managed independently from
  application containers.
- Deleting an application will not implicitly delete its database. Database
  deletion will require a separate, explicit operation.
- A project may have multiple databases, with one database offered as the
  default workflow.
- Standalone databases may be created first and attached to projects later.
- Friendly display names will map to validated, collision-safe internal IDs,
  PostgreSQL database names, and role names.
