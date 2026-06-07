# Small-Fleet Analytics Architecture

Status: recommended small-fleet and demo architecture

Date: 2026-06-07

## Decision

Control One should use a hyper-light analytic profile for demos and small
fleets:

```text
agents / collectors
        |
        v
controlplane ingest
        |
        +--> Postgres: canonical product state, ingest journal, audit, cases
        +--> Redis: bounded hot counters, queues, streams, freshness cache
        +--> SQLite/WAL: embedded recent analytic read model
        +--> Doris: optional OLAP backend only when analytics.mode=olap
```

This is not a feature reduction. Dashboard, network security, investigation,
timeline, search, citation, and export workflows should keep their existing UI
routes and API contracts. The backend adapter changes under `analytics.mode`.

Doris remains supported for larger installations, but it should consume zero
memory in the default demo and small-fleet profile. It starts only through the
explicit Compose `olap` profile and `analytics.mode=olap`.

## Why Not Doris By Default

The current demo host has limited shared memory. Even with tuned heap and BE
limits, Doris introduces a frontend JVM, backend process, cluster bootstrap
requirements, host sysctl requirements, and operational variance that are out
of proportion for a small fleet.

For demos and branch-size deployments, the goal is bank-grade correctness within
a bounded footprint:

- deterministic ingest acceptance;
- replayable analytic projections;
- visible health and freshness;
- no unbounded memory growth;
- no deleted product workflows.

Redis plus SQLite alone is not enough because Redis is disposable hot state and
SQLite is a projection. Postgres remains the acceptance source of truth and
rebuild source.

## Fit Envelope

Use this small profile for:

- demos, proofs of value, branch installs, and low-EPS small fleets;
- roughly 1 to 50 monitored nodes;
- recent investigation windows measured in days or weeks;
- one controlplane instance with an existing Postgres database;
- sustained ingest that can be handled by serialized SQLite writes, roughly
  250 EPS before tuning and up to about 500 EPS with short retention and fast
  disk.

Move to Doris or another dedicated warehouse when the deployment needs sustained
high EPS, many concurrent tenants, long hot retention, multi-GB ad hoc search,
or bank-scale OLAP concurrency.

## Current Repo State

The repository is already aligned with the first version of this architecture:

- `deploy/docker-compose.yaml` defaults to `ANALYTICS_MODE=small` and
  `DORIS_ENABLED=false`.
- Doris FE and BE live behind the explicit Compose `olap` profile.
- Redis is already required and bounded with `REDIS_MAXMEMORY`, defaulting to
  `128mb`.
- The controlplane mounts `/var/lib/control-one/analytics` for local SQLite
  files and defaults `CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB` to `16` in deploy.
- `controlplane/internal/server/analytics_mode.go` selects `small`, `olap`, or
  `disabled`; `auto` resolves to OLAP only when Doris is enabled and configured.
- `controlplane/internal/smallanalytics` already uses the pure-Go SQLite driver,
  WAL mode, busy timeouts, an immediate transaction lock, and serialized
  in-process writes.
- Small analytics currently persists `process_connections` and serves:
  `/api/v1/fleet/health` through Postgres rollups,
  `/api/v1/connections`,
  `/api/v1/connections/{conn_id}`,
  `/api/v1/connections/top-talkers`,
  `/api/v1/events/query` for cited connection-fact rows, and
  `/api/v1/timelines/build` for connection-fact timelines.

This means the design is not speculative. The immediate task is to finish the
small profile as a complete product path, not to remove the Doris path.

## Component Responsibilities

### Postgres: System Of Record

Postgres remains canonical for:

- tenants, users, RBAC, MFA, policies, jobs, alerts, cases, audit, and
  workflow state;
- ingest replay journals and idempotency keys;
- durable hourly rollups and fallback dashboard summaries;
- evidence metadata and rebuild coordination.

An ingest batch is accepted only after the Postgres journal write commits.
SQLite and Redis may lag, but they must be reconstructable from Postgres.

### Redis: Hot State

Redis should be fast, bounded, and non-evidentiary. It is appropriate for:

- worker and Asynq queues;
- live node freshness and status counters;
- short UI streams;
- top-talker acceleration;
- dashboard caches;
- writer lag and degradation gauges.

Redis keys must be tenant-scoped, TTL-bound, and safe under eviction. Suggested
key families:

```text
co:hot:fleet:{tenant}:nodes
co:hot:fleet:{tenant}:node:{node}:counters
co:hot:toptalkers:{tenant}:{yyyyMMddHH}
co:stream:events:{tenant}
co:analytics:writer:{tenant}:lag
co:analytics:writer:{tenant}:degraded
```

Redis can answer "what is happening right now?" but it must not be the only
source for a bank-grade citation.

### SQLite/WAL: Local Analytic Read Model

SQLite is the embedded analytic projection for recent evidence reads. It should
store indexed, queryable facts that the UI needs without starting another
daemon:

- connection rows and top talker facts;
- normalized events;
- timeline entity links;
- full-text-search content through FTS5;
- hourly rollups;
- enrichment snapshots that need recent investigation pivots.

Current implementation uses one SQLite file under the configured analytics
directory. The target architecture can evolve to one tenant file per active
tenant if lock isolation becomes necessary:

```text
/var/lib/control-one/analytics/controlone-small-analytics.db
/var/lib/control-one/analytics/tenants/{tenant_id}.db   # future isolation option
```

Required runtime behavior:

- WAL mode;
- `busy_timeout` at least 5 seconds;
- `synchronous=NORMAL` for demo, with `FULL` as a future production option;
- bounded read pool and one serialized writer path per database;
- capped write queue and batch size;
- query timeouts, limits, and maximum windows;
- startup migrations and `PRAGMA quick_check`;
- online backup or `VACUUM INTO` after WAL checkpoint for snapshots.

SQLite is durable on disk, but the operating model is replay-first: if a file is
lost or corrupt, rebuild it from the Postgres journal.

### Doris: Optional OLAP

Doris stays valuable for:

- high-EPS analytic ingest;
- many tenants with concurrent ad hoc investigations;
- long hot retention;
- warehouse-grade aggregation and large searchable history.

It should not be part of the default demo path. In small mode, Doris health
must not gate `/healthz`, browser UX, ingest, dashboard rendering, or
investigation flows that the local projection can answer.

## Data Flow

Ingest should behave the same whether the active analytic backend is small or
OLAP:

1. Validate tenant, node, schema, RBAC, rate limits, and capture policy.
2. Write the Postgres replay journal and idempotency state.
3. Fan out local events to detectors, audit, subscriptions, and product
   workflows.
4. Update Redis hot counters and short streams with TTLs.
5. Append normalized analytic facts to SQLite in bounded transactions.
6. Mark the batch complete for the active local analytics projection.
7. Optionally mirror or drain to Doris only when OLAP mode is selected.

The existing `doris_status` storage field can remain for compatibility during
the transition, but new code and admin copy should move toward
`analytics_status` semantics. A compatible status model is:

- `accepted`;
- `local_completed`;
- `pending_local`;
- `pending_olap`;
- `failed`;
- `disabled`.

## Read Path Contract

API handlers should select the backend behind a common analytic capability
contract:

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

Endpoint behavior:

- Fleet health reads Redis freshness first, then SQLite or Postgres rollups.
- Top talkers read Redis sorted sets first, then SQLite connection rows.
- Connection list/detail reads SQLite in small mode and Doris in OLAP mode.
- Events query reads SQLite `events` and FTS once that projection exists; until
  then, it can return cited `conn.open` and `conn.close` rows from
  `process_connections`.
- Timeline build reads SQLite `timeline_entities` once implemented; until then,
  it returns bounded connection timelines.
- Investigation enrichment combines SQLite recent facts with Postgres cases,
  alerts, audit, compliance, and entity metadata.
- Exports should preserve the same user workflow and include the active source
  label, for example `source=small-analytics` or `source=doris`.

Small mode may return guardrails when a projection is genuinely incomplete, but
it should not hide the route, remove the UI affordance, or convert a working
workflow into a dead end.

## SQLite Target Schema

The current `process_connections` table is the first slice. The next target
schema should add normalized events, timeline links, FTS, and rollups:

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
```

This keeps queryable evidence local and cheap while preserving a clean migration
path to Doris for warehouse scale.

## Resource Budget

Default demo target:

- controlplane: 512 MB to 1 GB container limit;
- Redis: `REDIS_MAXMEMORY=128mb`, container limit around 192 MB;
- SQLite: no daemon, 16 to 64 MB cache budget;
- console: 256 MB;
- landing and edge services: 128 MB each;
- Doris: 0 MB unless OLAP profile is explicitly selected.

The design goal is one analytic process in small mode: the controlplane itself.

## Security And Accuracy

Bank-grade small mode means deterministic and replayable, not infinite scale.
Required properties:

- every event has a stable ID or dedup key;
- tenant and RBAC checks are identical in small and OLAP modes;
- capture-policy redaction happens before sensitive fields enter SQLite;
- Redis-only data is never the sole evidence source;
- citations point to stable SQLite/Postgres records;
- retention deletion is explicit and auditable;
- corrupted SQLite projections are quarantined and rebuilt from Postgres;
- health APIs expose mode, source, writer lag, backlog, quick-check status, and
  last successful projection time.

For encryption at rest, prefer encrypted disks or application-level field
encryption for sensitive event bodies. Avoid making SQLCipher a default
dependency unless the build and deployment model deliberately accepts cgo.

## Retention Defaults

Recommended small-fleet defaults:

- Redis streams/counters: 1 to 24 hours, depending on key family;
- SQLite normalized events: 7 days by default, configurable to 30 days;
- SQLite connection and timeline facts: 14 to 30 days;
- SQLite hourly rollups: 90 days;
- Postgres ingest journal: retain pending and failed rows until repaired;
  archive terminal rows after the configured replay window;
- optional object storage: compressed daily evidence archives for retention
  beyond the SQLite hot window.

Retention jobs must checkpoint WAL files and report DB/WAL sizes.

## Deployment Modes

### Demo / Small

```yaml
analytics:
  mode: small
  sqlite_dir: /var/lib/control-one/analytics
  sqlite_cache_mb: 16
doris:
  enabled: false
```

Expected runtime:

- Postgres on the host or managed database;
- Redis container;
- controlplane container with embedded SQLite;
- console, landing, nginx edge, certbot, and IP enrichment;
- no Doris FE/BE containers.

### OLAP

```yaml
analytics:
  mode: olap
doris:
  enabled: true
```

Expected runtime:

- Doris or another warehouse on dedicated capacity;
- OLAP migrations and health checks are required;
- optional dual-read tests compare small fixtures with warehouse results.

## Implementation Roadmap

1. Keep the current small profile as the default demo deployment.
2. Add Redis hot-counter acceleration for fleet health, top talkers, dashboard
   freshness, and writer lag.
3. Expand SQLite from `process_connections` into `events`, FTS5,
   `timeline_entities`, `rollups_hourly`, and enrichment facts.
4. Introduce a backend-neutral `AnalyticsStore` interface so server handlers no
   longer talk directly in Doris terms.
5. Rename admin and AI health copy from `doris_status` to
   backend-neutral analytics health while preserving the database field until a
   deliberate migration.
6. Add replay/restart acceptance tests: ingest, restart controlplane, confirm
   small analytics results survive, delete a SQLite projection, rebuild from the
   Postgres journal, and compare counts/citations.
7. Add dual-read fixture tests for small vs OLAP mode so larger customers can
   move to Doris without relearning the UI.
8. Run live browser validation against the network, investigation, timeline,
   dashboard, and export flows with `source=small-analytics`.

## Demo Acceptance Criteria

The small architecture is demo-ready when all of these are true:

- `docker compose up -d` starts no Doris containers unless `--profile olap` is
  passed.
- `/healthz` is healthy with `analytics.mode=small` and a healthy SQLite store.
- Redis remains within its configured memory cap under noisy ingest.
- The console routes for dashboard, network security, investigation, timelines,
  and exports load without console errors or misleading Doris-only empty states.
- `/api/v1/fleet/health`, `/api/v1/connections`,
  `/api/v1/connections/top-talkers`, `/api/v1/events/query`, and
  `/api/v1/timelines/build` return successful small-mode envelopes wherever
  projected facts exist.
- Restarting the controlplane does not lose recent analytic results.
- Rebuilding SQLite from the Postgres journal reproduces the same fixture
  counts, citations, connection facts, and timeline pivots.
- OLAP mode remains available and explicit for larger deployments.

## Non-Goals

- Do not remove Doris from the codebase.
- Do not remove investigation, timeline, search, top-talkers, connection
  drilldown, dashboard, or export features.
- Do not make Redis the evidence store.
- Do not position small mode as a replacement for high-volume bank SIEM
  warehousing. It is the right default for demos, branch installs, and small
  fleets.
