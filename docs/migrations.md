# Database migrations

Use the official **golang-migrate CLI**, independently of the application.
There is no migration library, embedded SQL or migration subcommand in `api`.

## Run

Install the pinned CLI and ensure the Go bin directory is on PATH:

```sh
go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
```

From the repository root:

```sh
# Copy backend/.env.example to backend/.env and configure DATABASE_URL.
make -C backend migrate
# Equivalent, with DATABASE_URL set explicitly:
migrate -path backend/db/migrations -database "$DATABASE_URL" up
migrate -path backend/db/migrations -database "$DATABASE_URL" version
```

Compose runs `migrate/migrate:v4.20.1` with the SQL directory mounted read-only;
API and workers start after it succeeds. No CLI installation is required on the
host for Compose. CI installs the same version. Run `make -C backend test-integration` with JFE_TEST_DATABASE_URL set and the CLI
on PATH. The shell script creates disposable schemas and invokes migrate before
Go tests; Go code does not launch or wrap the migration CLI. Each test suite
receives JFE_TEST_SCHEMA_URL from the script, and schemas are removed on exit.

Files live in `backend/db/migrations`, with matching numbered `.up.sql` and
`.down.sql` files. Add a new version instead of editing an applied migration.
Run `make -C backend generate` followed by `make -C frontend generate` after schema changes. sqlc reads the up migrations.

## Existing Goose databases

Versions 1 and 2 preserve the former Goose SQL. The tracking table changes from
`goose_db_version` to `schema_migrations`. Before the first CLI `up`, stop services,
back up the database and verify the applied version and actual schema:

```sql
SELECT max(version_id) AS current_version
FROM (
  SELECT DISTINCT ON (version_id) version_id, is_applied
  FROM goose_db_version
  ORDER BY version_id, id DESC
) AS latest
WHERE is_applied;
```

For a verified version **2** database:

```sh
migrate -path backend/db/migrations -database "$DATABASE_URL" force 2
make -C backend migrate
```

Use `force 1` only if the actual schema is version 1. Force records the version
without executing SQL; retain the Goose table as historical data. The CLI does
not recognize Goose automatically: running up without a baseline attempts to
recreate existing tables and leaves the migration dirty. Databases already
using golang-migrate need no conversion for this CLI change.

## Failed migrations and rollback

The CLI owns version tracking, locking and dirty-state handling. A repeated up
with no pending migrations succeeds. Diagnose and repair or restore a failed
migration before forcing the verified version and retrying; no automatic force
is performed.

For deliberate rollback, use `migrate -path backend/db/migrations -database
"$DATABASE_URL" down 1`. Review the SQL first: version 1 drops application tables;
version 2 cannot roll back if different libraries share a media path. Run rollback
with services stopped and a backup available. Startup only applies up migrations.
