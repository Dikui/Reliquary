# Worker Pool Governance

Reliquary uses guaranteed per-stage worker pools plus an elastic pool for the
document ingestion pipeline. Worker concurrency is a scheduling budget, not a
replacement for model quotas, parser capacity or
database connection limits.

## Topology

| Pool | Default per instance | Queues | Purpose |
| --- | ---: | --- | --- |
| Core | 8 | `default` | Parsing, one article processing call and indexing |
| Enrichment | 12 | `entity` | Full content embedding rebuilds |
| Maintenance | 4 | `low` | Batch reprocessing and deletion |
| Shared | 6 | Core and enrichment queues | Elastic capacity borrowed by the side with backlog |

The total is 30 workers per service instance by default.

The article processing call runs inside the document task, before body chunking
and indexing. Maintenance is intentionally excluded from the
shared pool because its long-running tasks could pin elastic capacity needed by
the user-facing pipeline.

PostgreSQL claiming uses row locks with SKIP LOCKED. Dedicated and shared pools consume
the same core/enrichment queues; one task is still processed by one worker.

## Configuration

All settings are available under System settings and require a service restart:

- `queue.core_concurrency` / `RELIQUARY_QUEUE_CORE_CONCURRENCY`
- `queue.enrichment_concurrency` / `RELIQUARY_QUEUE_ENRICHMENT_CONCURRENCY`
- `queue.maintenance_concurrency` / `RELIQUARY_QUEUE_MAINTENANCE_CONCURRENCY`
- `queue.shared_concurrency` / `RELIQUARY_QUEUE_SHARED_CONCURRENCY`

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
call for its content type, summary and topics; manual entity links are retained.
Body images are read within that call when the selected model supports vision.
Queue count is not a capacity signal.

The runtime dashboard reports:

- configured concurrency per instance;
- live server instance count;
- cluster capacity aggregated from the single PostgreSQL queue worker;
- active workers and utilization;
- queue backlog, retries, dead letters, and oldest pending age;
- model concurrency and limiter waiting.

Increase a pool only when backlog age grows while its downstream dependency has
headroom. Reallocate to enrichment when core stays underutilized and content
embedding rebuild queues grow. Reduce core admission when parser CPU, memory,
or latency is the bottleneck.
