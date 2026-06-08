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
- local quick-check, WAL checkpointing, retention, and rebuild state.

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

## Capability Matrix

| Capability | Small-Fleet Source | OLAP Source | Required Behavior |
| --- | --- | --- | --- |
| fleet health | Redis freshness plus Postgres/SQLite rollups | Doris plus Postgres fallback | same cards, optional source metadata |
| connections list/detail | SQLite `process_connections` | Doris `process_connections` | same filters, drilldowns, citations |
| top talkers | Redis sorted sets, fallback SQLite | Doris aggregation | same response envelope |
| event query | SQLite `events`/FTS as projected; connection facts today | Doris `events` | same citations, guardrails for gaps |
| timeline build | SQLite `timeline_entities` as projected; connection facts today | Doris timeline views | same timeline and raw tabs |
| exports | SQLite/Postgres recent evidence | Doris long-window evidence | same export flow with source metadata |
| analytics health | journal, local lag, quick-check, Redis evictions | journal and warehouse writer health | analytics-neutral copy |

This matrix is the anti-regression contract. If a workflow works in OLAP mode,
small mode should either answer from its read model or explain the bounded
limitation without removing the workflow.

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
- `PRAGMA quick_check` health.

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
- health exposes mode, source, lag, backlog, quick-check status, DB/WAL size,
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

Known gaps to close before calling the small profile fully bank-grade:

| Gap | Required Work |
| --- | --- |
| direct Doris naming in code and copy | introduce backend-neutral names while preserving compatibility fields |
| Redis hot-counter acceleration | add sorted-set/hash update path with SQLite fallback |
| normalized non-connection events | project log, web, process, file, DNS, DB audit, policy, and security events into SQLite |
| FTS search | add `events_fts` and bounded query paths |
| projection cursors/rebuild | add admin job or command to rebuild tenant projections from Postgres |
| health surfaces | expose local lag, quick-check, DB/WAL size, Redis evictions, and source metadata |
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
   analytics-neutral health language.
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
- `/healthz` is healthy with `analytics.mode=small` and SQLite quick-check OK;
- Redis remains within its configured memory cap under noisy ingest;
- dashboard, network security, investigation, timelines, exports, and admin
  health load without console errors or Doris-only copy;
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
