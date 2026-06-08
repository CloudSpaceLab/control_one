# Small-Fleet Analytics Architecture

Status: recommended demo and small-fleet architecture

Date: 2026-06-08

## Decision

Control One should run a hyper-light analytic profile for demos and small
fleets:

```text
Postgres = durable ingest acceptance, replay journal, audit, workflow truth
SQLite   = embedded recent evidence projection and bounded analytic reads
Redis    = capped hot state, queues, freshness, live counters, short streams
Doris    = explicit OLAP upgrade only; 0 MB in the default demo profile
```

The practical replacement for Doris is not Redis plus SQLite alone. Redis is
evictable and must never be the evidence store. SQLite is a local projection and
must be rebuildable. Postgres remains the durable source of truth so the small
profile can stay memory-light without becoming non-replayable.

The product promise does not change. Dashboard, network security,
investigation, timeline, search, citation, export, AI tooling, and admin health
workflows stay present. The selected analytics backend changes under
`analytics.mode`; useful features are not deleted for the demo.

## Design Principles

1. Durable acceptance happens before acceleration. An event is accepted only
   after the Postgres journal/idempotency boundary is safe.
2. Redis is speed, not truth. It may hold counters, leases, queues, freshness,
   and short live streams, but every Redis analytic value must be TTL-bound or
   rebuildable from Postgres/SQLite.
3. SQLite is a local read model. WAL, bounded cache, short transactions,
   tenant/time/limit predicates, and replay cursors make it predictable on
   small hosts.
4. Feature parity beats feature removal. If OLAP mode has a workflow, small
   mode should either answer from its projection or return analytics-neutral
   guardrails that explain bounded retention or missing projection coverage.
5. Doris remains an upgrade path. The small profile keeps Doris at 0 MB by
   default, but the code path, migrations, and API envelopes stay compatible
   with dedicated OLAP deployments.

## Operating Modes

### Demo / Small

```yaml
analytics:
  mode: small
  sqlite_dir: /var/lib/control-one/analytics
  sqlite_cache_mb: 16
doris:
  enabled: false
redis:
  maxmemory: 128mb
  maxmemory_policy: volatile-lru
```

Expected runtime:

| Component | Role | Default Budget |
| --- | --- | ---: |
| controlplane | API, ingest fan-out, SQLite writer, query facade | 512 MB target, 1 GB ceiling |
| Postgres | canonical product DB, journal, audit, cases, replay truth | existing deployment |
| Redis | queues, live freshness, hot counters, short streams | 128 MB maxmemory, 192 MB container limit |
| SQLite/WAL | embedded recent analytic read model | 16 MB cache default |
| Doris FE/BE | disabled unless OLAP is explicitly selected | 0 MB |

### Small-Fleet Sizing Tiers

These tiers are planning defaults, not hard license gates. Validate them with a
tenant replay fixture before each serious demo or customer pilot.

| Tier | Fleet Shape | Redis Hot Budget | SQLite Cache | Recent Analytic Window | Decision |
| --- | --- | ---: | ---: | --- | --- |
| demo-light | up to 50 nodes, bursty demo ingest | 64 to 128 MB | 16 MB | 7 day events, 14 day connections | default profile |
| branch / SMB | 50 to 250 nodes, a few operators | 128 to 256 MB | 32 to 64 MB | 14 to 30 day events/connections | still small mode if replay tests pass |
| edge appliance | constrained host, local evidence first | 64 to 128 MB | 16 to 32 MB | short hot window plus compressed archives | small mode with stricter retention |
| OLAP transition | sustained high event volume, many tenants, long ad hoc queries | Redis only for hot coordination | N/A | warehouse-managed | select `analytics.mode=olap` |

Move a deployment to OLAP when single-writer projection lag cannot catch up
inside the accepted recovery objective, when recent SQLite data grows beyond
the host's checkpoint/backup comfort zone, or when many users need long-window
ad hoc queries at the same time. The upgrade trigger is observed behavior,
not fear that small mode is less legitimate.

### OLAP

```yaml
analytics:
  mode: olap
doris:
  enabled: true
```

Expected runtime:

- Doris, or another warehouse, runs on dedicated analytic capacity.
- OLAP migrations and writer health checks are required.
- The UI/API contract remains the same; OLAP adds retention, concurrency, and
  ad hoc analytic depth, not a different product surface.

## Reference Topologies

### Demo Host

Use one controlplane, one Redis, the existing Postgres service, and an embedded
SQLite projection directory mounted into the controlplane container:

```text
console -> controlplane -> Postgres journal
                       -> SQLite/WAL recent projection
                       -> Redis hot state and queues
```

This is the target for the sales/demo VPS. It should boot with no Doris
containers, no Doris host prerequisites, and no route removal. Its success
metric is that the console can demonstrate the product end to end from
`source=small-analytics` with bounded latency and clear health metadata.

### Small Production / Pilot

Keep Postgres as the HA/backup boundary. Run one active projection writer per
tenant or deployment, protected by a lease. Standby controlplane instances may
either warm their own SQLite projection from the Postgres journal or rebuild on
promotion. Do not put the same SQLite database file on a shared network
filesystem with concurrent writers.

```text
active controlplane   -> local SQLite/WAL projection
standby controlplane  -> warm projection or rebuild-on-promote
Postgres HA/backup    -> canonical journal and replay truth
Redis                 -> bounded queues, leases, freshness, counters
```

For active-active APIs, route analytic reads to the instance that owns the
fresh projection, or make each instance run a read projection from the shared
Postgres journal with independent cursors. The simple, safe small-fleet default
is active/passive for the projection layer.

### OLAP Upgrade

When the observed workload needs long-window ad hoc search, high concurrency,
or storage beyond the local projection comfort zone, switch to:

```text
analytics.mode=olap
DORIS_ENABLED=true
docker compose --profile olap ...
```

That should be a capacity upgrade, not a UI rewrite. The same routes should
keep working with `source=doris` or another future warehouse source.

## Why Not Doris By Default

The demo host is a shared, memory-constrained environment. Even with tuned heap
and BE limits, Doris introduces a frontend JVM, backend process, cluster
bootstrap, host sysctl requirements, storage compaction behavior, and memory
variance that are out of proportion for small fleets.

Small deployments need deterministic correctness more than warehouse depth:

- accepted ingest must commit durably;
- recent evidence must be searchable and cited;
- live UI heat must stay responsive;
- projection failures must be replayable;
- operational memory must be predictable;
- missing projection coverage must be visible as backlog, not hidden by
  removed routes.

## External Design Anchors

The small profile is intentionally built around the documented behavior of each
component:

- Redis officially treats `maxmemory` as a cache-data limit and evicts according
  to `maxmemory-policy`; with AOF or replication, extra buffers are outside
  that eviction comparison. Therefore Redis must hold only reconstructable hot
  state, and container memory must leave headroom above `maxmemory`.
- SQLite WAL lets readers and a writer run concurrently, but there is still
  only one writer at a time and checkpoints are part of normal operation.
  Therefore Control One should serialize projection writes, keep transactions
  short, and expose checkpoint/lag health.
- SQLite negative `cache_size` values are expressed in kibibytes and are an
  upper bound, not eagerly allocated memory. This is why `sqlite_cache_mb=16`
  is a useful cap rather than a guaranteed 16 MB allocation.
- Doris BE memory limits are process-level and the Doris docs call out OOM risk
  when BE is mixed with FE or other services on the same host. That is the exact
  demo shape we are avoiding by keeping Doris behind the explicit OLAP profile.

References: [Redis key eviction](https://redis.io/docs/latest/develop/reference/eviction/),
[SQLite WAL](https://www.sqlite.org/wal.html),
[SQLite PRAGMA cache_size](https://www.sqlite.org/pragma.html#pragma_cache_size),
[Doris BE configuration](https://doris.apache.org/docs/3.x/admin-manual/config/be-config/),
and [Doris spill/memory behavior](https://doris.apache.org/docs/dev/admin-manual/workload-management/spill-disk/).

## Component Responsibilities

### Postgres: System Of Record

Postgres owns durable product state:

- tenants, users, roles, audit, cases, jobs, policies, and workflow state;
- accepted event ingest batches and idempotency state;
- replay source for local SQLite rebuilds and future OLAP backfill;
- terminal evidence references when SQLite has aged out hot projection data.

An ingest request is accepted only after the Postgres journal boundary is safe.
This keeps bank-grade replayability even when Redis or SQLite is unavailable.

### SQLite/WAL: Recent Evidence Projection

SQLite runs embedded inside the controlplane process. It provides recent,
tenant-scoped analytic reads without another daemon:

- connection list/detail and IP/node/connection pivots;
- cited event query rows for projected fact families;
- timelines and raw-event tabs for recent investigations;
- small dashboard rollups and export slices;
- local read checks, WAL checkpointing, retention, and rebuild state.

SQLite is not a canonical database. It is a bounded read model that can be
deleted and rebuilt from Postgres.

### Redis: Bounded Hot State

Redis accelerates reconstructable state only:

- Asynq queues and worker coordination;
- node freshness and live status;
- top-talkers sorted sets and short dashboard heat;
- short event streams for live UI affordances;
- writer lag, projection lag, and health counters.

Redis keys used for analytics heat must be TTL-bound or rebuildable. Redis-only
data is never used as the sole source for citations, audit, compliance, or
customer evidence.

Suggested hot key families:

| Key Family | Structure | TTL / Bound | Rebuild Source | Allowed Uses |
| --- | --- | --- | --- | --- |
| `tenant:{tid}:node:freshness` | hash or string per node | 1 to 15 minutes | heartbeat/audit rows in Postgres | live online/stale badges |
| `tenant:{tid}:talkers:{window}` | sorted set | 1 to 24 hours | SQLite `process_connections` and events | dashboard heat, top talkers |
| `tenant:{tid}:eventstream:{window}` | stream with `MAXLEN` | minutes to hours | Postgres journal / SQLite projection | live tail affordances only |
| `tenant:{tid}:projection:lag` | hash | overwrite, no evidence value | Postgres journal + SQLite cursor | health cards and alerts |
| `tenant:{tid}:dashboard:heat` | hash/sorted set | minutes | SQLite rollups | fast cards with fallback |

Forbidden Redis-only state:

- citations, evidence exports, audit trails, compliance findings, case
  attachments, durable job state, and tenant/RBAC decisions;
- anything that would make an investigation false or incomplete after eviction;
- keys without TTL unless they are queue/control-plane primitives that are
  already durable or reconciled elsewhere.

### Doris: Optional OLAP Upgrade

Doris remains supported for deployments that need:

- sustained high event volume;
- long hot retention;
- many tenants with concurrent analytic queries;
- warehouse-style aggregation and text search at larger scale.

Doris must stay behind the explicit Compose `olap` profile and
`analytics.mode=olap`. It is not part of the default demo memory budget.

## Data Flow

```text
agents / collectors
        |
        v
controlplane ingest API
        |
        +--> validate tenant, node, schema, capture policy, RBAC, limits
        +--> commit Postgres journal and idempotency state
        +--> fan out detectors, audit, alerts, cases, subscriptions
        +--> project recent analytic facts into SQLite/WAL
        +--> update Redis hot state after durable acceptance
        +--> enqueue or stream to Doris only in OLAP mode
```

Projection failures do not make accepted ingest disappear:

- SQLite failure marks local projection lag and retries from Postgres.
- Redis failure degrades freshness/hot counters but does not block evidence.
- Doris failure in OLAP mode marks warehouse writer lag and retries.

The journal is the replay boundary in every mode.

## Read Path Contract

Product handlers should call backend-neutral capability methods instead of
deciding UI behavior based on whether Doris exists:

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
    LogVolumeBucketed(ctx context.Context, p LogVolumeParams) ([]Bucket, error)
    Health(ctx context.Context) AnalyticsHealth
}
```

Small mode may return source and guardrail metadata when a projection is
bounded or incomplete. It should not hide routes, remove buttons, or show
Doris-specific failure copy. Preferred copy is analytics-neutral, such as
"recent evidence projection is rebuilding" or "older history requires OLAP
mode."

Small-mode event pagination should avoid exact warehouse-style counts on hot
operator paths. When a query can match more rows than the current page, fetch
`limit + 1`, return the requested page, and expose a minimal total that proves
whether `next_offset` exists. Exact totals are acceptable only when the page
exhausts the result set or a precomputed rollup can answer cheaply. This keeps
the Redis+SQLite demo profile responsive on large local projections without
removing event search, citations, or timeline pivots.

Every analytic response should preserve the same product envelope across modes:

```json
{
  "source": "small-analytics",
  "mode": "small",
  "as_of": "2026-06-08T00:00:00Z",
  "retention": {"from": "2026-06-01T00:00:00Z", "to": "2026-06-08T00:00:00Z"},
  "lag": {"journal_backlog": 0, "projection_ms": 240},
  "guardrails": [],
  "data": []
}
```

Existing compatibility fields such as `doris_status` can remain during the
transition, but new handlers should also emit analytics-neutral state such as
`warehouse_status`, `projection_status`, or `analytics_status`. UI copy should
read from the neutral fields first and treat Doris naming as backwards
compatibility only.

Admin health must also distinguish a disabled warehouse from a failed
warehouse. In `analytics.mode=small`, pending journal replay means the local
projection is degraded until replay drains; it must not be reported as a
missing-Doris outage. In `analytics.mode=olap`, pending replay with no
configured warehouse is still a loud `down` condition because the selected
analytic backend cannot accept the work.

## Capability Matrix

| Capability | Small-Fleet Source | OLAP Source | Required Behavior |
| --- | --- | --- | --- |
| fleet health | Redis freshness plus Postgres/SQLite rollups | Doris plus Postgres fallback | same cards, optional source metadata |
| connections list/detail | SQLite `process_connections` | Doris `process_connections` | same filters, drilldowns, citations |
| top talkers | Redis sorted sets, fallback SQLite | Doris aggregation | same response envelope |
| event query | SQLite `events`/FTS as projected; connection facts today | Doris `events` | same citations, guardrails for gaps |
| timeline build | SQLite `timeline_entities` as projected; connection facts today | Doris timeline views | same timeline and raw tabs |
| exports | SQLite/Postgres recent evidence | Doris long-window evidence | same export flow with source metadata |
| analytics health | journal, local read/deep-check status, Redis evictions | journal and warehouse writer health | analytics-neutral copy |

This matrix is the anti-regression contract. If a workflow works in OLAP mode,
small mode should either answer from its read model or explain the bounded
limitation without removing the workflow.

## Feature-Preservation Contract

Small mode is an implementation choice, not a reduced edition. The UI and API
should follow these rules:

- keep navigation, buttons, exports, timeline pivots, AI tools, and drilldowns
  visible when the user is allowed to use them;
- show source, retention, lag, and guardrail metadata when the small projection
  is bounded;
- use copy such as "recent evidence projection is rebuilding" or "older
  history requires OLAP mode" instead of Doris-specific outage copy;
- keep OLAP-only depth behind explicit guardrails, not hidden route removal;
- preserve compatibility fields for existing clients while preferring neutral
  fields such as `analytics_mode`, `analytics_status`, `projection_status`,
  `warehouse_status`, and `source`;
- test each high-value route with Doris absent so missing projections become
  backlog items with clear operator behavior.

This rule is especially important for demos: a light backend is acceptable;
a light product surface is not.

## SQLite Projection Model

The current implementation has the first slice in
`controlplane/internal/smallanalytics`:

- WAL mode and `busy_timeout`;
- small configurable cache;
- serialized writes;
- indexed `process_connections`;
- connection list/detail, IP, node, tenant, connection, and correlation pivots;
- top-talkers fallback;
- event query and timeline projection from connection facts;
- one-row lookahead pagination for small-mode event queries instead of exact
  OLAP-style counts on every request;
- lightweight read health; deep `PRAGMA quick_check` should run from a
  scheduled or explicit admin job, not from ordinary page-load health checks.

The next target schema should add normalized events, FTS, timelines, rollups,
and replay cursors:

```sql
CREATE TABLE events (
  event_id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  raw_ref TEXT,
  ts_ms INTEGER NOT NULL,
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
  threat_feed TEXT,
  threat_score INTEGER DEFAULT 0,
  message TEXT,
  details_json TEXT,
  dedup_key TEXT
);

CREATE INDEX events_tenant_ts_idx ON events(tenant_id, ts_ms DESC);
CREATE INDEX events_tenant_node_ts_idx ON events(tenant_id, node_id, ts_ms DESC);
CREATE INDEX events_tenant_type_ts_idx ON events(tenant_id, event_type, ts_ms DESC);
CREATE INDEX events_tenant_corr_ts_idx ON events(tenant_id, correlation_id, ts_ms DESC);
CREATE INDEX events_tenant_conn_ts_idx ON events(tenant_id, conn_id, ts_ms DESC);

CREATE VIRTUAL TABLE events_fts USING fts5(
  message,
  details_json,
  content='events',
  content_rowid='rowid'
);

CREATE TABLE timeline_entities (
  tenant_id TEXT NOT NULL,
  entity_type TEXT NOT NULL,
  entity_id TEXT NOT NULL,
  ts_ms INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  source_table TEXT NOT NULL,
  PRIMARY KEY (tenant_id, entity_type, entity_id, ts_ms, event_id)
);

CREATE INDEX timeline_event_idx ON timeline_entities(tenant_id, event_id);

CREATE TABLE rollups_hourly (
  tenant_id TEXT NOT NULL,
  hour_ts_ms INTEGER NOT NULL,
  node_id TEXT,
  event_type TEXT NOT NULL,
  cnt INTEGER NOT NULL DEFAULT 0,
  bytes_in INTEGER NOT NULL DEFAULT 0,
  bytes_out INTEGER NOT NULL DEFAULT 0,
  severity_max TEXT,
  PRIMARY KEY (tenant_id, hour_ts_ms, node_id, event_type)
);

CREATE TABLE projection_cursors (
  tenant_id TEXT NOT NULL,
  projector TEXT NOT NULL,
  source_batch_id TEXT,
  source_ts_ms INTEGER NOT NULL DEFAULT 0,
  last_success_ms INTEGER NOT NULL DEFAULT 0,
  last_error TEXT,
  rebuild_state TEXT NOT NULL DEFAULT 'ready',
  PRIMARY KEY (tenant_id, projector)
);
```

### SQLite Write Discipline

To keep SQLite lightweight and predictable:

- use one projection writer per controlplane instance, guarded by a process
  mutex or queue, and keep upsert batches small enough to finish under the
  ingest timeout budget;
- set a bounded `busy_timeout`, short query contexts, and limit/tenant/time
  predicates on every read path;
- use bounded lookahead or rollups for pagination metadata on hot paths; avoid
  request-time exact counts over broad unions unless the result is already
  trivially exhausted;
- checkpoint WAL during idle moments and after retention sweeps, and report
  database size, WAL size, checkpoint age, and failed checkpoint attempts;
- store projector cursors in SQLite and checkpoint durable replay state in
  Postgres, so deleting the SQLite file is a recovery drill rather than data
  loss;
- keep FTS optional per event family and never let broad text search skip the
  tenant/time/limit guardrails;
- back up with SQLite-aware mechanisms such as the online backup API or
  `VACUUM INTO`, not by copying only the main `.db` file while WAL is active.

## Failure Modes

| Failure | Small-Mode Behavior |
| --- | --- |
| Redis unavailable | queues/live heat degrade; evidence queries continue from SQLite/Postgres where possible |
| Redis eviction | hot counters may rebuild; citations/audit are unaffected |
| SQLite locked or slow | bounded timeout, visible local projection lag, retry from journal |
| SQLite corrupt | quarantine file, create fresh projection, rebuild from Postgres journal |
| SQLite deleted | rebuild from Postgres journal; recent analytics unavailable until replay catches up |
| controlplane restart | WAL-backed facts remain; writer resumes from journal cursor |
| Postgres unavailable | no new accepted ingest; existing SQLite reads may continue with stale-source metadata |
| OLAP selected but Doris unavailable | fail loudly as OLAP health failure; do not silently behave like warehouse mode |

## Security And Accuracy

Small mode is bank-grade when it is deterministic and replayable:

- tenant and RBAC checks are identical in small and OLAP modes;
- capture-policy redaction happens before sensitive fields enter SQLite;
- every projected fact has a stable event ID, connection ID, dedup key, or raw
  reference;
- citations point to SQLite/Postgres records, never Redis-only state;
- reads are tenant-scoped, time-bounded, limit-bounded, and timeout-bounded;
- retention deletion is explicit and auditable;
- health exposes mode, source, lag, backlog, read/deep-check status, DB/WAL size,
  Redis eviction count, and last successful projection time.

For encryption at rest, prefer encrypted volumes or field-level encryption for
sensitive event bodies. Avoid making SQLCipher a default dependency unless the
build and deployment model deliberately accepts cgo.

## Retention Defaults

Recommended small-fleet defaults:

| Data | Default Retention |
| --- | --- |
| Redis streams/counters | 1 to 24 hours, depending on key family |
| SQLite normalized events | 7 days by default, configurable to 30 days |
| SQLite connection and timeline facts | 14 to 30 days |
| SQLite hourly rollups | 90 days |
| Postgres ingest journal | pending/failed until repaired; terminal rows archived after replay window |
| optional object storage | compressed daily evidence archives beyond SQLite hot window |

Retention jobs should checkpoint WAL files and report DB/WAL sizes.

## Current Repo State

The repository already points toward this architecture:

- `deploy/.env.example` defaults to `ANALYTICS_MODE=small`,
  `ANALYTICS_SQLITE_CACHE_MB=16`, `REDIS_MAXMEMORY=128mb`,
  `REDIS_MAXMEMORY_POLICY=volatile-lru`, and `DORIS_ENABLED=false`.
- `deploy/docker-compose.yaml` caps Redis hot memory, mounts
  `/var/lib/control-one/analytics`, and keeps Doris FE/BE behind the `olap`
  profile.
- `deploy/bootstrap.sh` and `deploy/deploy.py` skip Doris unless OLAP is
  explicitly selected.
- `controlplane/internal/smallanalytics` implements the embedded SQLite store
  for connection facts and bounded timeline/event reads from those facts.
- Several server paths already prefer `localAnalytics` in small mode before
  Doris.
- Admin ingest backlog, capacity, and AI ingest-health responses now include
  backend-neutral `analytics_mode`, `analytics_status`, `warehouse_status`, and
  `warehouse_configured` fields while retaining legacy `doris_status` and
  `doris_configured` compatibility fields. The AI tool is exposed as
  `ingest_health`, with `doris_ingest_health` kept as a compatibility alias.
- The embedded SQLite projection exposes admin capacity health stats for
  lightweight read-check status, DB/WAL/SHM bytes, total projection bytes, configured
  cache cap, checked-at time, and last health error. The Settings System health
  panel renders those stats without Doris-specific copy.

Known gaps to close before calling the small profile fully bank-grade:

| Gap | Required Work |
| --- | --- |
| remaining direct Doris naming in code and copy | continue moving operator contracts to backend-neutral names while preserving compatibility fields |
| Redis hot-counter acceleration | add sorted-set/hash update path with SQLite fallback |
| normalized non-connection events | project log, web, process, file, DNS, DB audit, policy, and security events into SQLite |
| FTS search | add `events_fts` and bounded query paths |
| projection cursors/rebuild | add admin job or command to rebuild tenant projections from Postgres |
| health surfaces | expose projection lag, checkpoint age, rebuild state, deep quick-check results, Redis evictions, and source metadata beyond the current read-check and DB/WAL size stats |
| restart/replay tests | ingest fixture, restart, delete SQLite, rebuild, compare counts/citations/timelines |
| UI copy | remove Doris-only empty/error language from small-mode routes |

## Implementation Roadmap

### P0: Demo-Safe

1. Keep the default deploy at `analytics.mode=small` with Doris stopped.
2. Keep current SQLite connection facts powering network, investigation, event
   query, and timeline flows.
3. Add Redis acceleration for top talkers, node freshness, dashboard heat, and
   writer lag with SQLite/Postgres fallback.
4. Replace user-facing `doris_status` and Doris-specific empty states with
   analytics-neutral health language, keeping compatibility fields and aliases
   for existing clients.
5. Live-test dashboard, network security, investigation, timelines, exports,
   and admin health with Doris absent.

### P1: Bank-Grade Local Projection

1. Add SQLite `events`, `events_fts`, `timeline_entities`, `rollups_hourly`,
   and `projection_cursors`.
2. Project log/web/process/file/DNS/DB/policy/security facts from the existing
   ingest journal.
3. Add tenant-scoped rebuild, quick-check, checkpoint, retention, and lag jobs.
4. Add source/guardrail metadata wherever a small-mode query is bounded by
   projection coverage or retention.
5. Add restart and rebuild acceptance tests with fixture counts, citations, and
   timeline pivots.

### P2: OLAP Compatibility

1. Complete a backend-neutral `AnalyticsStore` facade for all analytic reads.
2. Keep Doris stream loading, migrations, and HA runbooks available only in the
   dedicated OLAP profile.
3. Add dual-read fixture tests comparing small and OLAP contracts.
4. Preserve the same UI copy and response envelopes across modes.

## Demo Acceptance Criteria

The architecture is demo-ready when:

- `docker compose up -d` starts no Doris containers unless `--profile olap` is
  passed;
- `/healthz` is healthy with `analytics.mode=small` and local projection read checks OK;
- Redis remains within its configured memory cap under noisy ingest;
- dashboard, network security, investigation, timelines, exports, and admin
  health load without console errors or Doris-only copy;
- admin ingest backlog and capacity report small-mode replay lag through
  `analytics_status` / `warehouse_status`, with disabled Doris treated as
  normal for small mode and as failure only when OLAP is explicitly selected;
- `/api/v1/fleet/health`, `/api/v1/connections`,
  `/api/v1/connections/top-talkers`, `/api/v1/events/query`, and
  `/api/v1/timelines/build` return successful small-mode envelopes wherever
  projected facts exist;
- restarting controlplane does not lose recent analytic results;
- deleting and rebuilding SQLite from Postgres reproduces fixture counts,
  citations, connection facts, and timeline pivots;
- OLAP remains available and explicit for larger deployments.

## Non-Goals

- Do not remove Doris from the codebase.
- Do not remove investigation, timeline, search, top-talkers, connection
  drilldown, dashboard, export, or AI workflows.
- Do not make Redis the evidence store.
- Do not position small mode as a replacement for high-volume bank SIEM
  warehousing.
- Do not make the UI depend on knowing whether the selected backend is SQLite
  or Doris.
