# Worker Pool Governance

Reliquary uses guaranteed per-stage worker pools plus an elastic pool for the
document ingestion pipeline. Worker concurrency is a scheduling budget, not a
replacement for model quotas, parser capacity or
database connection limits.

## Topology

| Pool | Default per instance | Queues | Purpose |
| --- | ---: | --- | --- |
| Core | 8 | `default` | Parsing, one article processing call and indexing |
| Enrichment | 12 | `entity` | Entity and article embedding rebuilds, topic backfill |
| Maintenance | 4 | `low` | Batch reprocessing and deletion |
| Shared | 6 | Core and enrichment queues | Elastic capacity borrowed by the side with backlog |

The total is 30 workers per service instance by default.

The article processing call runs inside the document task, before body chunking
and indexing. Maintenance is intentionally excluded from the
shared pool because its long-running tasks could pin elastic capacity needed by
the user-facing pipeline.

Dedicated and shared pools consume Redis wake messages for the same
core/enrichment queues. A worker locks the PostgreSQL operation row and checks
its generation, delivery number and state before admitting the handler.

## Configuration

The following worker settings are available under System settings and take
effect on service restart.

- `queue.core_concurrency` / `RELIQUARY_QUEUE_CORE_CONCURRENCY`
- `queue.enrichment_concurrency` / `RELIQUARY_QUEUE_ENRICHMENT_CONCURRENCY`
- `queue.maintenance_concurrency` / `RELIQUARY_QUEUE_MAINTENANCE_CONCURRENCY`
- `queue.shared_concurrency` / `RELIQUARY_QUEUE_SHARED_CONCURRENCY`

## PostgreSQL connection budget

| Connection source | Default maximum | Configuration |
| --- | ---: | --- |
| Application GORM / pgx `database/sql` pool | 40 per server | `DB_MAX_OPEN_CONNS` |
| Maintenance GORM pool, including `maintenance`, `vault-rebuild` and `reembed` on the live or cloned database | 4 per CLI process | `DB_MAINTENANCE_MAX_OPEN_CONNS` |
| golang-migrate | 1 pinned session per active migration | Released when the migration closes |
| Backup clone pipeline | 2 per clone | One serial `pg_dump` on the source and one serial `pg_restore` on the clone |

HTTP requests, dispatch, all 30 default workers and session-level advisory locks
share the application pool. The worker singleton pins one of its 40 connections.
Topic backfill, shelf parsing and vault file rebuilds also take their lock
sessions from the caller's pool. PostgreSQL LISTEN/NOTIFY sessions and
standalone pgx pools contribute zero connections. Redis handles wake delivery.

A single-host installation with one server and one concurrent maintenance CLI
budgets **48 connections** even if both processes run migrations and a backup
streams to a clone at the same time (`40 + 4 + 2 + 2`). Compose uses PostgreSQL's
default `max_connections=100`, leaving 52 slots including PostgreSQL's reserved
slots. At startup each process checks that the combined budget fits strictly
below the server's non-reserved connection count. The Compose health check
uses one transient `pg_isready` connection. Clone creation and cleanup each run
a single `psql` session outside the streaming step. Additional CLI processes,
parallel backups and unrelated database clients share the remaining headroom.

Set the two limits in the deployment's `.env`. The server reads them on startup
and each CLI reads them when invoked. Both accept integers of at least 3. Offline
replay can hold a singleton session and a transaction while dispatch needs a
third connection. When tuning worker concurrency, allow room above the number
of simultaneous lock holders for their queries, HTTP requests and dispatch.

Each queue client batches transaction-visibility checks in one observer, up to
500 operation IDs per query. It checks the root database so only committed
operations trigger a wake. The dispatcher publishes up to 500 due operations per
pass. When the SQL pool is full, callers wait for a connection within their
context deadline. The periodic sweep picks up long transactions and missed wakes.

## Capacity layers

Tune these layers independently:

1. Worker concurrency controls the number of task handlers admitted per
   service instance.
2. Model quota governance controls provider concurrency and RPM within
   the backend process and its quota groups.
3. Parsers, Postgres, and local CPU/RAM retain
   their own resource limits.

If model limiter waiting grows while worker queues are busy, adding workers
only creates more waiters. Increase the provider quota or reduce worker
admission instead.

## Sizing

For each pool, estimate peak required workers using:

```text
required workers = ceil(peak task arrival rate * mean task runtime / 0.70)
```

Use task arrival rate for each queue. One approved article uses one processing
call for its content type, summary, entities and topics; body images are read
within that call when the selected model supports vision.
Queue count is not a capacity signal.

The runtime dashboard reports:

- configured concurrency per instance;
- live server instance count;
- cluster capacity aggregated from the single PostgreSQL queue worker;
- active workers and utilization;
- queue backlog, retries, dead letters, and oldest pending age;
- model concurrency and limiter waiting.

Increase a pool only when backlog age grows while its downstream dependency has
headroom. Reallocate to enrichment when core stays underutilized and entity
rebuild queues grow. Reduce core admission when parser CPU, memory, or latency is the
bottleneck.
