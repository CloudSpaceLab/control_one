# Small-Fleet Analytics Architecture

Status: Recommended small-fleet design

Date: 2026-06-06

## Purpose

Control One needs a demo and small-fleet analytic mode that keeps the existing
SIEM, investigation, connection, timeline, and dashboard features without
requiring Apache Doris on a shared 8 GB VPS.

The live audit showed that Doris can return correct data after tuning, but it is
still too memory-sensitive for the current demo host. A small-fleet default
should be boring, bounded, and recoverable: Redis for hot state, SQLite for
indexed recent facts, Postgres for canonical product state and replay journals,
and Doris only as an optional large-fleet OLAP backend.

## Decision Summary

Use a local analytic backend for demo, branch, and small-fleet deployments:

- Postgres remains the source of truth for product state, durable ingest
  journals, audit records, cases, and operator workflows.
- Redis keeps only bounded hot state: live counters, short streams, leases,
  writer lag, and dashboard acceleration.
- SQLite stores recent indexed analytic facts in WAL-mode tenant databases:
  events, process connections, timeline links, and hourly rollups.
- Doris remains supported behind `analytics.mode=olap`, but it becomes opt-in
  and should not start in the default demo compose profile.

This is not a feature reduction. The API contract and UI surfaces stay intact;
only the backing analytic store changes for small fleets.

## 2026-06-07 Demo Architecture Decision

For the current demo and any small fleet that fits this envelope, make
`analytics.mode=small` the default operating model and keep Doris stopped. The
runtime should have only four data services in the hot path:

- Postgres for canonical product state, ingest journal, rollups, audit, cases,
  and replay.
- Redis for bounded hot state: worker/asynq queues, live counters, dashboard
  cache, writer lag, and short streams.
- SQLite/WAL inside the controlplane container for recent indexed analytic read
  models.
- Optional object/archive storage for long-retention exports when a customer
  asks for more evidence history than the local SQLite window should hold.

Doris remains a supported `analytics.mode=olap` backend for larger fleets, but
it should be treated as a dedicated-capacity warehouse, not a dependency of the
demo host. The small-fleet path must be additive: replace backend adapters and
query sources, do not hide or delete connection, investigation, timeline,
search, dashboard, or export features.

The practical integration shape is:

1. Ingest succeeds only after Postgres writes the replay journal.
2. Local fanout updates Postgres rollups, live event subscribers, detections,
   and the SQLite analytic read model.
3. Redis receives TTL-bound counters/streams for low-latency UI freshness.
4. API handlers read Redis first where freshness matters, then SQLite for
   evidence-grade recent facts, then Postgres rollups as the durable fallback.
5. Doris adapters remain behind the same analytic interface for OLAP mode and
   migration/dual-read tests.

Current code is already past the first slice: `controlplane/internal/smallanalytics`
uses the pure-Go SQLite driver in WAL mode, writes `process_connections`, and
serves connection list, connection detail, top talkers, investigation event
query, and timeline build in small mode. On 2026-06-07 the live demo host was
also hardened after a `SQLITE_BUSY` fanout warning: SQLite pragmas now apply
through the driver DSN to every pooled connection, transactions start with
immediate locking, and in-process writers are serialized while reads remain
available through a small bounded pool. The event/timeline implementation is a
demo-grade connection-fact projection today: it emits cited `conn.open` and
`conn.close` rows from SQLite and keeps Doris as the opt-in OLAP backend for
full generic/file/db/web event timelines. The next demo-hardening work should
focus on entity enrichment, log-volume buckets, Redis hot-counter acceleration,
and admin health copy that still talks about `doris_status` even when the active
backend is local analytics.

## Current Implementation State

The repo is already partially aligned with this decision:

- `ANALYTICS_MODE=small` is the deploy default.
- Docker Compose keeps Doris FE/BE behind the explicit `olap` profile.
- The controlplane does not initialize Doris while small mode is selected, even
  if Doris credentials are present.
- Fleet health can already fall back to Postgres rollups with
  `source=small-analytics-postgres`.
- A first SQLite/WAL slice now persists process-connection facts from the
  existing ingest fanout and serves connection list, connection detail, and top
  talker APIs with `source=small-analytics` when `analytics.sqlite_dir` is
  configured.
- The same local connection facts now project into `/api/v1/events/query` and
  `/api/v1/timelines/build` as cited `conn.open`/`conn.close` rows, while OLAP
  mode keeps the existing Doris-backed full timeline/search path.
- The live demo host has verified this first slice post-deploy: connection
  list, top talkers, connection detail, event query, and timeline build returned
  `source=small-analytics` with Doris disabled, while recent control-plane logs
  showed no SQLite busy/lock warnings after the concurrency hardening deploy.
- Redis-backed hot counters remain the next acceleration layer; SQLite is the
  evidence-grade local read model for this first slice.

The next implementation step should therefore be additive: add Redis
acceleration for live counters and dashboard cache, then expand SQLite beyond
connection facts into full normalized event/FTS/timeline tables. Do not delete
the Doris code path; keep it as the opt-in OLAP backend for larger deployments.

## Small-Fleet Fit Envelope

The Redis+SQLite mode is the right default for:

- demos and proofs of value;
- one to roughly 50 monitored nodes;
- low to moderate telemetry rates, roughly up to 250 sustained EPS before
  tuning and up to 500 EPS with short retention and a fast disk;
- recent investigation windows measured in days, not multi-year warehouse
  searches;
- one controlplane instance with Postgres already available.

Expected memory profile on the shared 8 GB VPS:

- controlplane: 512 MB to 1 GB;
- Redis: 128 MB for demo, 256-512 MB for branch deployments;
- SQLite: no daemon, bounded in-process page cache;
- no Doris FE JVM and no Doris BE process.

Move to Doris or another dedicated analytic warehouse when sustained ingestion,
retention, tenant concurrency, or ad hoc search volume exceed that envelope.

## Current Doris Responsibilities

The current controlplane uses Doris for several user-facing read paths:

- `/api/v1/fleet/health`: node health, connection counts, traffic, severity.
- `/api/v1/connections/top-talkers`: busiest external peers.
- `/api/v1/connections` and `/api/v1/connections/{conn_id}`: connection
  drilldowns and correlated events.
- `/api/v1/events/query`: normalized event query with citations.
- `/api/v1/timelines/build`: investigation timeline builder.
- Investigation entity enrichment and related-entity lookups.
- Log-volume buckets and log search in selected investigation flows.

Postgres already carries the ingest journal, dashboard rollups, alerts,
security events, telemetry APIs, and product state. Redis already exists in the
deployment for worker/runtime coordination.

## Design Principles

1. Do not remove useful features. Replace the storage implementation behind the
   same analytic capabilities.
2. Make the small-fleet path the default for demos and branch-scale installs.
3. Keep Redis non-canonical. It accelerates dashboards and live state, but any
   evidence-quality result must be replayable from Postgres journal plus SQLite.
4. Keep SQLite bounded. Time-window caps, retention, WAL checkpoints, and
   per-tenant files prevent one noisy tenant from wedging the whole controlplane.
5. Keep Doris optional. It remains the right answer for large, multi-node,
   high-EPS analytic clusters, not for the smallest production footprint.

## Why This Shape

Redis alone is too volatile to be an evidence store. It is excellent for hot
counters and low-latency dashboards, but eviction, TTLs, and approximate
structures must never decide what a bank can later prove.

Postgres alone can carry the replay journal and rollups, and the current small
mode already uses that path for fleet health. However, pushing high-cardinality
connection drilldowns, full-text event search, and timeline facts into the same
transactional database risks turning investigation traffic into product-state
contention.

SQLite gives the small deployment a local indexed read model without a separate
memory-heavy service. Because the Postgres journal is canonical, SQLite can be
treated as durable but reconstructable: if a tenant database is missing or
corrupt, replay the journal and rebuild the analytic file.

## Proposed Components

### Postgres: System Of Record

Postgres remains the canonical database for:

- tenants, users, RBAC, MFA, policies, jobs, alerts, cases, evidence metadata;
- `event_ingest_batches` replay journal;
- `event_rollups_hourly` fallback/summary data;
- security events and audit/event records already modeled in Postgres.

Ingest is accepted only after the replay journal write succeeds. A batch is
marked analytics-complete only after the small analytic store commits it.

### Redis: Hot State And Fast Dashboard Cache

Redis keeps short-lived, memory-bounded operational state:

- live event stream fanout for UI/SSE;
- fleet last-seen and node status counters;
- per-hour top talker sorted sets;
- queue-depth and writer-lag gauges;
- small recent-event rings for "what just happened" UI affordances.

Redis keys must use TTLs and bounded structures. Example key families:

- `co:hot:fleet:{tenant}:nodes`
- `co:hot:fleet:{tenant}:node:{node}:counters`
- `co:hot:toptalkers:{tenant}:{yyyyMMddHH}`
- `co:stream:events:{tenant}`
- `co:analytics:writer:{tenant}:lag`

Redis is allowed to be approximate for dashboard speed. Investigation evidence
and citations must come from SQLite/Postgres.

### SQLite: Local Analytic Read Store

SQLite becomes the small-fleet analytic store. Use WAL mode, a single writer
goroutine per tenant database, many read connections, bounded query windows, and
explicit retention jobs.

Recommended layout:

- one SQLite file per tenant under `/var/lib/control-one/analytics/{tenant}.db`;
- a tiny metadata database for schema versions and shard registry;
- WAL files on the same persistent volume;
- periodic `PRAGMA quick_check`, checkpoint, and backup snapshots.

The per-tenant file choice keeps lock contention and retention isolated. For a
single-tenant demo it behaves like one tiny embedded analytics database.

Recommended runtime settings:

```sql
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA temp_store = FILE;
PRAGMA cache_size = -32768; -- 32 MiB per active tenant connection pool
PRAGMA wal_autocheckpoint = 1000;
```

Use `synchronous=NORMAL` for the demo because Postgres keeps the replay journal.
For bank production small-fleet installs that treat SQLite as evidence storage
between backups, allow `synchronous=FULL` as an explicit config option.

Cap the writer queue, batch size, and query window:

- writer queue: default 5,000 events per tenant, reject or mark degraded after
  the cap instead of growing memory unbounded;
- writer batch size: 250-1,000 rows per transaction;
- default query window: 24 hours;
- maximum unprivileged query window: 7 days;
- admin override window: bounded by configured retention.

Candidate tables:

```sql
CREATE TABLE events (
  event_id TEXT NOT NULL,
  raw_ref TEXT,
  ts INTEGER NOT NULL,
  node_id TEXT,
  event_type TEXT NOT NULL,
  severity TEXT,
  correlation_id TEXT,
  conn_id TEXT,
  collector TEXT,
  parser TEXT,
  parser_status TEXT,
  src_ip TEXT,
  src_port INTEGER,
  dst_ip TEXT,
  dst_port INTEGER,
  protocol TEXT,
  pid INTEGER,
  process_name TEXT,
  user_name TEXT,
  bytes_in INTEGER DEFAULT 0,
  bytes_out INTEGER DEFAULT 0,
  duration_ms INTEGER DEFAULT 0,
  rule_id TEXT,
  threat_feed TEXT,
  threat_score INTEGER DEFAULT 0,
  message TEXT,
  details_json TEXT,
  dedup_key TEXT,
  PRIMARY KEY (event_id)
);

CREATE INDEX events_ts_idx ON events(ts DESC);
CREATE INDEX events_node_ts_idx ON events(node_id, ts DESC);
CREATE INDEX events_type_ts_idx ON events(event_type, ts DESC);
CREATE INDEX events_correlation_idx ON events(correlation_id, ts DESC);
CREATE INDEX events_conn_idx ON events(conn_id, ts DESC);
CREATE INDEX events_ip_ts_idx ON events(src_ip, dst_ip, ts DESC);

CREATE VIRTUAL TABLE events_fts USING fts5(
  message,
  details_json,
  content='events',
  content_rowid='rowid'
);

CREATE TABLE process_connections (
  conn_id TEXT PRIMARY KEY,
  correlation_id TEXT,
  started_at INTEGER NOT NULL,
  ended_at INTEGER,
  node_id TEXT,
  direction TEXT,
  pid INTEGER,
  process_name TEXT,
  cmdline TEXT,
  user_name TEXT,
  src_ip TEXT,
  src_port INTEGER,
  dst_ip TEXT,
  dst_port INTEGER,
  protocol TEXT,
  bytes_in INTEGER DEFAULT 0,
  bytes_out INTEGER DEFAULT 0,
  packets_in INTEGER DEFAULT 0,
  packets_out INTEGER DEFAULT 0,
  threat_match INTEGER DEFAULT 0,
  threat_feed TEXT,
  closed_reason TEXT,
  bastion_session_id TEXT
);

CREATE INDEX connections_node_time_idx
  ON process_connections(node_id, started_at DESC);
CREATE INDEX connections_src_time_idx
  ON process_connections(src_ip, started_at DESC);
CREATE INDEX connections_dst_time_idx
  ON process_connections(dst_ip, started_at DESC);

CREATE TABLE timeline_entities (
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  source_table TEXT NOT NULL,
  PRIMARY KEY (entity_type, entity_id, ts, event_id)
);

CREATE INDEX timeline_event_idx ON timeline_entities(event_id);

CREATE TABLE rollups_hourly (
  hour_ts INTEGER NOT NULL,
  node_id TEXT,
  event_type TEXT NOT NULL,
  cnt INTEGER NOT NULL DEFAULT 0,
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  sev_max TEXT,
  PRIMARY KEY (hour_ts, node_id, event_type)
);
```

SQLite should use a pure-Go driver or a build profile that preserves the current
cross-compile deployment path. The current deploy builds with `CGO_ENABLED=0`,
so a cgo-only driver would be a deployment regression.

### Derived-Store Crash Semantics

The small analytic database is durable, but the recovery model is replay-first:

1. Accept ingest only after the Postgres journal write succeeds.
2. Normalize events and write local fanout metadata as the code does today.
3. Write Redis hot counters with TTLs.
4. Append SQLite facts in a bounded writer transaction.
5. Mark the journal batch complete for the active analytic backend only after
   SQLite commits.

The existing `doris_status` column can remain for compatibility during the
transition, but new code should treat it as backend status and expose it as
`analytics_status` in API/admin copy. Status values should distinguish
`local_completed`, `pending_local`, `pending_olap`, `failed`, and `archived`
without requiring an immediate destructive migration.

If the process crashes after the journal write but before SQLite commit, the
drainer replays the batch. If SQLite fails health checks, the server keeps
accepting journaled ingest only up to configured backlog limits and reports
analytic freshness as degraded.

## API Source Mapping

The server should depend on a new internal interface, not directly on
`doris.Client`.

```go
type AnalyticsStore interface {
  AppendBatch(ctx context.Context, batch AnalyticsBatch) error
  QueryEvents(ctx context.Context, p EventQueryParams) ([]EventRow, int, error)
  BuildTimeline(ctx context.Context, p TimelineBuildParams) ([]TimelineItem, error)
  ListConnectionsForNode(ctx context.Context, p ConnectionQuery) ([]ConnectionRow, error)
  ListConnectionsForIP(ctx context.Context, p ConnectionQuery) ([]ConnectionRow, error)
  ListConnectionsForTenant(ctx context.Context, p ConnectionQuery) ([]ConnectionRow, error)
  TopTalkers(ctx context.Context, tenantID string, since time.Time, limit int) ([]TopTalker, error)
  FleetHealthSnapshot(ctx context.Context, tenantID string, since time.Time) ([]FleetSnapshotRow, error)
  LogVolumeBucketed(ctx context.Context, p LogVolumeParams) (map[time.Time]int64, error)
  Health(ctx context.Context) AnalyticsHealth
}
```

Implementations:

- `smallanalytics.Store`: Redis plus SQLite.
- `doris.Store`: adapter around the existing Doris client/writer.
- `noop.Store`: ingest journal and Postgres rollups only, for degraded mode.

Endpoint behavior:

- Fleet health: read Redis hot counters first, repair/fallback from SQLite
  `rollups_hourly`, then Postgres `event_rollups_hourly`.
- Top talkers: read Redis sorted sets for the selected hours; fallback to
  SQLite `process_connections`.
- Connection drilldown: SQLite `process_connections` plus `events` by
  `correlation_id`.
- Events query, current demo slice: SQLite `process_connections` projected into
  cited `conn.open` and `conn.close` normalized rows with bounded filters.
- Events query, full small-fleet slice: SQLite `events` with B-tree filters and
  FTS5 search.
- Timeline build, current demo slice: SQLite `process_connections` projected
  into connection timelines for `ip`, `node`, `host`, `process`, `user`,
  `connection`, `event`, and `raw_ref` pivots where the facts exist.
- Timeline build, full small-fleet slice: SQLite `timeline_entities` plus typed
  events, merged with existing Postgres lifecycle items.
- Investigation enrichment: SQLite for recent facts, Postgres for durable case,
  alert, audit, and compliance facts.

Responses should expose `source: "small-analytics"` or more granular
`source: "redis+sqlite"` so audits can prove the active backend.

Endpoint compatibility rules:

- Do not remove UI panels, routes, filters, exports, or investigation affordances
  just because the backend changes.
- Small mode should return successful envelopes with data whenever the SQLite
  store is healthy. Guardrail text is reserved for true degraded or not-yet-built
  paths.
- The same tenant, RBAC, redaction, capture-policy, and citation checks must run
  in both `small` and `olap` modes.
- The same fixture events should produce equivalent normalized rows from Doris
  and SQLite in dual-read tests.

## Query Budget

Default query targets for the demo host:

- fleet health: p95 under 500 ms;
- top talkers: p95 under 500 ms for a 24-hour window;
- connection list: p95 under 700 ms for 1,000 returned rows or fewer;
- connection detail and correlated events: p95 under 700 ms;
- timeline build: p95 under 1.5 s for the configured recent window.

Every small-mode query must have a bounded time window, limit, and context
timeout. Expensive searches should return a typed partial/degraded response
instead of blocking the controlplane.

## Ingest Flow

1. Validate and normalize incoming telemetry/events.
2. Write the replay journal in Postgres.
3. Fan out in-memory events/webhooks exactly as today.
4. Update Redis hot counters and streams with TTLs.
5. Append typed facts to the SQLite writer queue.
6. The SQLite writer commits the batch in one transaction.
7. Mark the journal batch `analytics_status = local_completed`.
8. Optional: mirror to Doris when `analytics.mode = olap` or `mirror_doris =
   true`.

If SQLite is unavailable, ingest still writes the Postgres journal, marks
analytics pending, and the UI reports degraded analytic freshness rather than
losing events or consuming unbounded memory.

## Redis Data Model

Redis key families should be tenant-scoped, TTL-bound, and safe under eviction:

```text
co:hot:fleet:{tenant}:nodes                       hash, ttl 24h
co:hot:fleet:{tenant}:node:{node}:counters        hash, ttl 24h
co:hot:toptalkers:{tenant}:{yyyyMMddHH}           zset, ttl 48h
co:stream:events:{tenant}                         stream, maxlen approximate
co:analytics:writer:{tenant}:lag                  string/gauge, ttl 5m
co:analytics:writer:{tenant}:degraded             string, ttl 5m
```

Use Redis for speed and operator freshness, not for citations. Any value shown
as evidence must be traceable to SQLite/Postgres by ID.

## Retention Defaults

For demo and small-fleet mode:

- Redis streams/counters: 1-24 hours, key-dependent.
- SQLite raw normalized events: 7 days by default, configurable to 30 days.
- SQLite connection/timeline facts: 14-30 days.
- SQLite hourly rollups: 90 days.
- Postgres replay journal: short terminal retention, but pending/failed rows
  retained until repaired or archived by an operator.
- Optional object-store archive: compressed daily JSONL/Parquet export for
  longer evidence retention.

Scale-out threshold for Doris or another OLAP store:

- sustained ingestion above roughly 500-1000 EPS;
- hot searchable event data above tens of GB;
- many tenants requiring concurrent ad hoc searches;
- retention or compliance requirements that exceed the local SQLite profile.

## Resource Envelope

Expected small-fleet deployment:

- Controlplane: 512 MB to 1 GB.
- Redis: 128-512 MB with `maxmemory` and AOF enabled.
- SQLite: file-backed page cache, writer queue capped in process memory.
- No JVM, no BE/FE pair, no analytic cluster bootstrap.

This removes the largest memory consumers from the current demo stack while
keeping the investigation and dashboard features.

## Accuracy And Durability

Bank-grade behavior for this mode means deterministic, replayable, and visible,
not "infinite analytical scale":

- all event IDs/replay keys are idempotent;
- SQLite commits happen before a journal batch is marked complete;
- dashboards clearly label stale/degraded analytic freshness;
- Redis-only values are treated as dashboard acceleration, not evidence;
- citations point to SQLite/Postgres source records with stable IDs;
- retention deletion is explicit and auditable;
- backups use SQLite online backup or `VACUUM INTO`, plus WAL checkpointing;
- startup runs schema migration and `PRAGMA quick_check` on each tenant DB.

Sensitive event fields need the same capture-policy controls already used for
database query text redaction. If a bank requires encrypted-at-rest event bodies
inside SQLite, prefer encrypted volumes or application-level field encryption;
do not add a cgo-only SQLCipher dependency to the default demo path unless the
deployment build changes deliberately.

## Deployment Shape

Add an analytics mode:

```yaml
analytics:
  mode: small # small | olap | disabled
  sqlite_dir: /var/lib/control-one/analytics
  sqlite_cache_mb: 32
  sqlite_synchronous: normal # normal | full
  writer_queue_events: 5000
  writer_batch_size: 500
  hot_retention_days: 7
  rollup_retention_days: 90
  redis_hot_ttl: 24h
  mirror_doris: false
```

For the current demo VPS:

- set `CONTROLPLANE_DORIS_ENABLED=false`;
- put Doris FE and BE behind an explicit `olap` compose profile so a normal
  `docker compose up -d` cannot accidentally start them;
- keep Redis;
- mount `/opt/control-one/deploy/analytics` into the controlplane container;
- expose analytics health in `/healthz` detail/admin health endpoints.

For larger installs:

- set `analytics.mode=olap`;
- run Doris/OpenSearch/warehouse on dedicated hosts;
- optionally dual-write from small analytics to OLAP during migration.

## Observability And Operations

Expose enough status for a live demo and for bank operators:

- `analytics_mode`, `analytics_source`, and `analytics_backend_healthy`;
- SQLite DB size, WAL size, last checkpoint time, quick-check status;
- writer queue depth, writer lag seconds, failed batch count;
- Redis hot-key availability and eviction count;
- journal replay backlog by tenant and oldest pending age;
- endpoint-level source labels in the JSON responses used by the console.

Recovery runbook:

1. Keep Postgres and Redis running.
2. Stop the controlplane.
3. Move the affected tenant SQLite file aside.
4. Start the controlplane with replay enabled.
5. Rebuild from `event_ingest_batches` until backlog reaches zero.
6. Run `PRAGMA quick_check` and a browser/API smoke test.

Backups should use SQLite online backup or `VACUUM INTO` after a WAL checkpoint.
For the demo, backing up the `/var/lib/control-one/analytics` directory is
acceptable only when the controlplane is stopped or the backup tool is
SQLite-aware.

## Migration Plan

1. Introduce `internal/analytics` interfaces and adapt existing Doris types into
   backend-neutral structs.
2. Build `smallanalytics` SQLite schema, migrations, writer, and query methods.
3. Route fleet health and top talkers first. These have the highest dashboard
   impact and can use Redis/rollups immediately.
4. Route connection list/detail to SQLite.
5. Route events query and timeline build to SQLite in two stages: first project
   cited `conn.open`/`conn.close` rows from `process_connections`, then add FTS5
   and `timeline_entities` for full generic/file/db/web coverage.
6. Add dual-read comparison tests: Doris vs SQLite fixtures return equivalent
   normalized rows and citations.
7. Add deploy profile `small` that disables Doris and enables the SQLite volume.
8. Run live audit against `source: small-analytics`, then remove Doris from the
   demo host.

## Demo-First Implementation Slice

The fastest useful implementation is narrower than the final store:

1. Add `internal/analytics` and a `smallanalytics.Store` skeleton with health,
   migrations, tenant DB open/close, WAL settings, and writer queue limits.
2. Persist connection facts and hourly rollups from the already-normalized
   `conn.open` and `conn.close` event fanout.
3. Route `/api/v1/fleet/health`, `/api/v1/connections/top-talkers`, and
   `/api/v1/connections` through the analytics interface.
4. Route `/api/v1/events/query` and `/api/v1/timelines/build` through the same
   backend choice. In small mode, return cited connection-fact events/timeline
   rows from SQLite. In OLAP mode, keep the full Doris-backed timeline/search
   implementation.
5. Add compose profiles so Doris is opt-in and the default demo stack cannot
   consume Doris memory by accident.

This first slice turns `small-analytics-pending` responses for top talkers,
connection lists, event query, and timeline build into real SQLite-backed data
where connection facts exist, while preserving the existing guardrail behavior
if the local store is degraded.

## Demo Acceptance Criteria

Before calling the demo architecture ready:

- `docker compose up -d` starts no Doris containers unless `--profile olap` is
  passed.
- `/healthz` and the admin health detail report `analytics.mode=small` and a
  healthy SQLite store.
- `/api/v1/fleet/health`, `/api/v1/connections/top-talkers`,
  `/api/v1/connections`, `/api/v1/events/query`, and
  `/api/v1/timelines/build` return `source=redis+sqlite` or
  `source=small-analytics` with non-error envelopes.
- Browser validation shows the network, investigation, and dashboard routes
  loading without console errors, horizontal overflow, or misleading empty-state
  copy.
- Restarting the controlplane does not lose recent analytic facts.
- Deleting a tenant SQLite file and replaying the journal rebuilds the same
  connection/top-talker results from fixtures.
- Redis memory remains bounded under noisy ingest and eviction does not remove
  evidence because evidence is read from SQLite/Postgres.

## Non-Goals

- Do not remove Doris from the codebase.
- Do not remove investigation, timeline, search, top-talkers, or connection
  drilldown features.
- Do not make Redis the evidence store.
- Do not call the small-fleet mode a replacement for a high-volume bank SIEM
  warehouse. It is the correct default for demo, branch, and low-EPS fleets.

## Recommended First Implementation Slice

Build the small analytics backend behind a feature flag and migrate these paths
first:

1. `/api/v1/fleet/health`
2. `/api/v1/connections/top-talkers`
3. `/api/v1/connections?tenant_id=...`
4. `/api/v1/events/query` for cited connection-fact events
5. `/api/v1/timelines/build` for cited connection-fact timelines

That slice removes the current Doris dashboard pressure, proves the Redis/SQLite
model under live browser use, and leaves only the deeper generic/file/db/web
investigation coverage on Doris until the fuller SQLite event/FTS/timeline
implementation is ready.
