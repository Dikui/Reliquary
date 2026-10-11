# Database migration troubleshooting

This guide is linked from the system info page when Reliquary's startup database
migration fails. It covers the most common causes, how to diagnose them, and
how to recover within the current migration history.

If none of these match your situation, jump to
[Reporting an issue](#reporting-an-issue) at the bottom.

---

## Single baseline and database rebuild

On 2026-10-07, migrations `000000` through `000098` from commit `45daf07`
were compressed into `migrations/versioned/000000_init.up.sql` and its
matching down migration. A fresh database reaches version `0` after applying
this baseline. Future schema changes continue from version `000001`.

The baseline includes `uuid-ossp`, `vector`, `pg_trgm`, and `pg_search`,
application tables, and their final indexes, constraints, functions,
and trigger. Running the old migration history on an empty database leaves
every application table empty, so the baseline has no seed table rows.
The baseline is for a fresh installation with the personal tenant ID fixed at `1`.

Databases created with the old migration history **cannot be upgraded in
place**, including old databases whose recorded version is already `0`.
Stop Reliquary, provision a new empty database or recreate its PostgreSQL
data volume, point the database configuration at it, and restart with
`AUTO_MIGRATE=true`. Existing data is discarded during this rebuild. Do not
use `force 0` or delete `schema_migrations` to claim that an old schema is
the new baseline; changing the recorded version does not create the schema.

The Compose PostgreSQL service uses `paradedb/paradedb:v0.22.2-pg17`.
Both `docker-compose.yml` and `docker-compose.dev.yml` mount only their
PostgreSQL data volume. The unused upstream initialization and one-time
Chinese search demonstration scripts have been removed. Startup and
`scripts/migrate.sh` use only `migrations/versioned/`.

---

## What "migration failed" means

Reliquary auto-runs `golang-migrate` migrations on every startup. When a
migration fails, the application **still finishes starting up** (so the UI
remains reachable to help you diagnose the problem), but:

- The failing migration is rolled back, leaving the database at the previous
  version. Any tables / indexes introduced by that migration **are not
  created**.
- Downstream features depending on those tables (entity registry and
  task queues) may silently produce nothing.
- The system info page shows the partial DB version + a red "Migration failed"
  tag and the captured error.

The cached error message you see in the UI is the same one logged at startup
under `Database migration failed: ...`. Recent container logs are the
authoritative source — copy them before doing anything destructive.

---

## Common causes

### 1. Missing PostgreSQL extension

Many migrations require extensions (`pg_trgm`, `vector`, `pg_search`) created
by `CREATE EXTENSION IF NOT EXISTS`. **`IF NOT EXISTS` does not validate that
the extension is actually installed** — it only checks the catalog. If the
extension's shared library is missing or the role lacks `CREATE` privilege,
the statement may succeed in the migration that nominally creates it but a
later migration that uses the extension (e.g. building a `gin_trgm_ops` index)
will fail.

**Symptoms in the error**:

```
ERROR: operator class "gin_trgm_ops" does not exist for access method "gin"
ERROR: type "vector" does not exist
ERROR: function ... does not exist
```

**Fix**:

```sql
-- Connect as a superuser (typically `postgres`):
\c your_reliquary_database
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS vector;       -- PostgreSQL vector retrieval
CREATE EXTENSION IF NOT EXISTS pg_search;    -- only on ParadeDB

-- Verify they are actually loaded:
SELECT extname, extversion FROM pg_extension WHERE extname IN ('pg_trgm','vector','pg_search');
```

Then clear any dirty migration state as described below and restart
Reliquary to retry the failing migration.

If `CREATE EXTENSION` itself errors with **"could not open extension control
file"** or **"permission denied"**, the extension is not installed on your
PostgreSQL server — install the corresponding OS package (e.g.
`postgresql-contrib` for `pg_trgm`) or switch to an image that ships it
preinstalled, then retry.

### 2. Dirty migration state

If a migration crashed partway through (OOM, container kill, network blip)
`golang-migrate` marks the schema as "dirty" at the failing version. By
default, Reliquary's startup tries to auto-recover; if you disabled that with
`AUTO_RECOVER_DIRTY=false` you'll see:

```
database is in dirty state at version N. ...
```

**Fix** — use the bundled helpers:

```bash
# Check the recorded version
make migrate-version

# After checking and cleaning up any partially applied objects, reset a failed
# baseline at N = 0. For later migrations, use the previous available version
# shown in the startup error instead of -1.
make migrate-force version=-1

# Re-run pending migrations
make migrate-up
```

After that, restart Reliquary.

Or set `AUTO_RECOVER_DIRTY=true` (the default in recent versions) and just
restart — startup will perform the same `force` + retry automatically.

For a failure at baseline version `0`, there is no previous migration:
`make migrate-force version=-1` clears the recorded version so the baseline
can run again. Clean up partial objects first, or rebuild this fresh
database. Forcing version `0` would mark the baseline complete and skip it.
The baseline creates tables directly and is not intended to be reapplied
over an existing schema. Automatic dirty recovery resets the migration
state; it does not remove partially created objects.

### 3. Insufficient privileges on the database role

Some migrations create extensions or alter shared catalogs, which require
either superuser or `CREATEROLE` / `CREATEDB`. Errors look like:

```
ERROR: permission denied to create extension "pg_trgm"
ERROR: must be owner of database ...
```

**Fix**: grant the role used by `DB_USER` the necessary privileges, or
pre-create the extensions / objects as a superuser ahead of time, then
restart. The migration's `CREATE EXTENSION IF NOT EXISTS` will then no-op.

### 4. Out-of-disk during `CREATE INDEX`

GIN / pgvector indexes can require significant temporary space. Errors:

```
ERROR: could not extend file ...: No space left on device
ERROR: cannot create temporary tables in transaction
```

**Fix**: free disk on the volume backing `PGDATA`, then restart. The
migration will retry the index build.

### 5. Schema drift from manual edits

If you previously edited tables / columns by hand and a later migration
expects the original shape, it will fail with mismatched-type errors.
Compare the live schema with `000000_init.up.sql` and any later successfully
applied migrations, correct the drift, and retry the pending migration.
If the database belongs to the old history ending at `000098`, follow the
database rebuild procedure above.

---

## Generic diagnostic checklist

1. **Read the full error**: the cached message in the UI is truncated only by
   your browser scroll — it is the complete `golang-migrate` error. The
   container log shows the same content with stack context.
2. **Identify the failing migration**: the version number in the error (or
   `make migrate-version`) points to a file under `migrations/versioned/`.
   Version `0` points to `migrations/versioned/000000_init.up.sql`.
   For later versions, open the corresponding `*.up.sql` and find the statement
   matching the error type (extension, index, function, foreign key, …).
3. **Run the failing statement manually** against the DB using `psql`. The
   error will be far more specific than the migration wrapper's.
4. **Fix the underlying cause** (install extension, fix privileges, free
   disk, …), then either:
   - Restart Reliquary and let auto-recovery retry; **or**
   - Run `make migrate-up` from a checkout to apply migrations outside the
     server process.
5. **Verify**: the system info page should now show the DB version without
   the "Migration failed" tag, and the previously broken feature (entity registry or task queues) should start producing output.

---

## Reporting an issue

If you've worked through the checklist and the migration still fails, please
open an issue at:

<https://github.com/Tencent/WeKnora/issues/new?template=bug_report.yml>

Include:

- Reliquary version + commit ID (from the system info page).
- The full error from the system info page (or container logs).
- PostgreSQL version (`SELECT version();`) and how it was deployed (vanilla,
  ParadeDB, Aurora, Aliyun RDS, …).
- The output of:
  ```sql
  SELECT extname, extversion FROM pg_extension;
  ```
- Any non-default values of `AUTO_MIGRATE` and
  `AUTO_RECOVER_DIRTY`.

The "Report issue" link on the system info page pre-fills a body with the
captured error for you — clicking it is the fastest path.
