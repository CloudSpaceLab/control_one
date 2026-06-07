# Small-Fleet Analytics Architecture

Status: recommended small-fleet and demo architecture

Date: 2026-06-07

## Decision

Control One should use a hyper-light analytic profile for demos and small
fleets. The working name is **Control One Lite Analytics**:

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
routes and API contracts. The backend adapter changes under `analytics.mode`;
the product surface should not ask demo users to know whether the answer came
from Redis, SQLite, Postgres, or Doris.

For the demo host and other branch-size installations, the recommended shape is
not "smaller Doris." It is a different operating mode:

```text
one controlplane process
  - owns the SQLite writer and query pool
  - exposes the same analytics APIs as OLAP mode
one Redis container
  - bounded hot counters, queues, short streams, freshness
existing Postgres
  - ingest journal, replay truth, audit, workflow state
zero Doris containers
  - unless analytics.mode=olap is explicitly selected
```

Doris remains supported for larger installations, but it should consume zero
memory in the default demo and small-fleet profile. It starts only through the
explicit Compose `olap` profile and `analytics.mode=olap`.

The design rule is simple:

- Redis accelerates hot, disposable questions.
- SQLite stores recent, cited evidence and timelines.
- Postgres accepts, journals, audits, and rebuilds.
- Doris is an explicit OLAP upgrade path, not the demo default.

The product rule is equally important: small mode must preserve useful features.
If a projection is not implemented yet, that is backlog for the projection
layer, not a reason to remove the UI route, API contract, or operator workflow.

## Executive Recommendation

For the demo and small-fleet product tier, Control One should run a
**Postgres + Redis + SQLite/WAL analytic stack** and keep Doris completely off
the hot path:

```text
Postgres = durable acceptance, replay, audit, workflow truth
Redis    = bounded hot counters, queues, live freshness, short streams
SQLite   = recent local evidence, timelines, search, rollups
Doris    = explicit OLAP tier for high-volume customers only
```

This is the right trade for small fleets because it keeps the operational
memory profile predictable while preserving bank-grade accuracy. The system can
answer the demo's recent investigation and dashboard questions from an embedded
read model, and the same accepted events remain replayable into Doris later if a
customer outgrows the small profile.

The practical rule for implementation is: **replace Doris-specific plumbing,
not product capability.** Console routes, API contracts, AI tools, exports,
network drilldowns, timelines, and citations should stay present. If small mode
does not yet have a typed projection for a particular fact family, it should
return source/guardrail metadata and track that as a projection gap.

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
- `controlplane/internal/server/analytics_connections.go` now centralizes
  connection-history reads for small and OLAP modes. IP-scoped network
  targeting, node documentation top connections, and event-capture flow deltas
  use that selected backend instead of calling Doris directly.

This means the design is not speculative. The immediate task is to finish the
small profile as a complete product path, not to remove the Doris path.

## Integration Status And Gaps

The repo already starts and queries a small SQLite read model, but several
server paths still bind capability to a Doris client or Doris-specific status
wording. These should be treated as integration work, not as justification to
remove UI workflows.

| Area | Current Shape | Small-Fleet Target |
| --- | --- | --- |
| event ingest status | `eventIngestService` still reports `DorisStatus` and `pending_doris` even when the active backend is small | introduce backend-neutral `analytics_status` semantics while keeping the existing database field compatible during migration |
| network block targeting | `resolveAffectedNodesForIP` queries the selected analytics backend for recent IP connection facts | keep the same behavior as more fact families move into SQLite |
| node documentation | `buildNodeDocumentation` fills top connections from the selected analytics backend | add broader recent event/process facts as SQLite projections grow |
| event-capture flow deltas | `event_capture.go` computes connection deltas through the selected analytics backend | keep file/db/web deltas on their best available small-mode projections as they land |
| AI investigation tool naming | `doris_ingest_health` and related copy are still platform-Doris specific | keep the tool capability but expose it as analytics ingest health, with source labels for small vs OLAP |
| health and copy | logs/errors mention "small analytics sqlite store unavailable" and "Doris writer" separately | expose one analytics health envelope with mode, source, lag, queue depth, quick-check, and replay status |

A first connection-reader facade is now in place. The next safe implementation
move is to grow that into a fuller internal analytics facade and migrate each
route to backend-neutral capabilities:

```go
type AnalyticsReader interface {
    ListConnectionsForIP(ctx context.Context, tenantID, ip string, since, until time.Time, limit int) ([]doris.ConnectionRow, error)
    ListConnectionsForNode(ctx context.Context, tenantID, nodeID string, since, until time.Time, limit int, openOnly, externalOnly bool) ([]doris.ConnectionRow, error)
    ListConnectionsForTenant(ctx context.Context, tenantID string, since, until time.Time, limit int, externalOnly bool) ([]doris.ConnectionRow, error)
    ConnectionLifetime(ctx context.Context, tenantID, connID string) (*doris.ConnectionRow, error)
    TopTalkers(ctx context.Context, tenantID string, since time.Time, limit int) ([]doris.TopTalker, error)
    QueryEvents(ctx context.Context, p doris.EventQueryParams) ([]doris.EventRow, int, error)
    BuildTimeline(ctx context.Context, p doris.TimelineBuildParams) ([]doris.TimelineItem, error)
    Health(ctx context.Context) AnalyticsHealth
}
```

This can initially reuse the Doris row types to keep the patch small. A later
cleanup can move common analytic contracts out of the `doris` package once the
small path is complete. That sequencing protects useful features and avoids a
large cross-repo rename in the middle of demo stabilization.

## Small-Mode Contract

Small mode has to be boring in the right places. It should have fewer moving
parts than Doris, but it must keep the same correctness boundaries:

| Boundary | Contract |
| --- | --- |
| feature surface | routes, buttons, filters, exports, AI tools, and drilldowns remain available when their underlying facts exist |
| write acceptance | Postgres journal commit is the durable acceptance point |
| evidence | citations come from SQLite or Postgres records, never Redis-only state |
| hot state | Redis may be restarted or evicted without losing evidence or auditability |
| read limits | every query is tenant-scoped, time-bounded, limit-bounded, and timeout-bounded |
| degradation | small-mode gaps return source/guardrail metadata, not Doris-specific failure copy |
| rebuild | deleting the SQLite projection is recoverable from the Postgres journal |
| upgrade | OLAP mode uses the same API contracts and adds scale, not a different product |

The most important implementation habit is to route all analytic reads through
backend-neutral capability methods. Handlers should not branch directly on
"Doris page" versus "SQLite page"; they should ask for connections, events,
timelines, top talkers, or health and let the selected backend answer.

## Feature Preservation Checklist

Small mode is acceptable only if the operator experience remains complete. Each
feature should be reviewed with this checklist before any route, button, API, or
tool is hidden:

- Can the workflow be answered from Postgres canonical state, Redis hot state,
  SQLite recent evidence, or a combination of those sources?
- If a fact family is not projected yet, can the workflow still show a bounded
  empty state with source and guardrail metadata?
- Does the response preserve tenant scoping, RBAC, citations, redaction policy,
  and export shape?
- Is the limitation about retention, scale, or projection maturity rather than
  about the user's entitlement to the feature?
- Would a later OLAP upgrade use the same API contract and UI workflow?

Only the underlying analytic source should change between small and OLAP mode.
The console should not become a smaller product merely because Doris is absent.

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

Use Redis data structures deliberately:

| Need | Redis Shape | Example |
| --- | --- | --- |
| live fleet freshness | hash with TTL | `HSET co:hot:fleet:{tenant}:nodes {node} {json}` |
| top talkers | sorted set per time bucket | `ZINCRBY co:hot:toptalkers:{tenant}:{hour} bytes ip` |
| short live event feed | stream with max length | `XADD co:stream:events:{tenant} MAXLEN ~ 5000 * ...` |
| writer lag / health | string/hash with TTL | `co:analytics:writer:{tenant}:lag_ms` |
| rate and cardinality hints | counters or HyperLogLog | `PFADD co:hot:seen_ips:{tenant}:{day} ip` |

All keys must expire or be safe under Redis eviction. A Redis restart may make
the UI momentarily less warm, but it must not lose evidence, citations,
compliance state, audit rows, or replayability.

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

SQLite should be treated as a product read model, not as a hidden scratch file.
The store needs explicit health, retention, and rebuild semantics:

- `PRAGMA quick_check` is part of analytics health.
- WAL files are checkpointed on a schedule and before backups.
- Backups use SQLite backup APIs or `VACUUM INTO`; do not copy only the `.db`
  file while WAL mode is active.
- Corruption or schema mismatch quarantines the projection, starts a rebuild
  from the journal, and exposes degraded analytics health rather than taking
  down `/healthz`.
- Retention deletes by tenant and time window, followed by opportunistic
  checkpointing.

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
6. Update Redis hot state after durable acceptance, never before.
7. Mark the batch complete for the active local analytics projection.
8. Optionally mirror or drain to Doris only when OLAP mode is selected.

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

The contract should include a source envelope, but the UI should only surface it
where it helps the operator trust the result. Avoid Doris-specific empty states
such as "Doris unavailable" in small mode; use analytics-neutral language like
"recent evidence projection is rebuilding" or "older history requires OLAP
mode".

Small mode may return guardrails when a projection is genuinely incomplete, but
it should not hide the route, remove the UI affordance, or convert a working
workflow into a dead end.

## Backend Selection Matrix

| Capability | Small Mode Source | OLAP Mode Source | Required UX Behavior |
| --- | --- | --- | --- |
| fleet health | Redis freshness + Postgres rollups, then SQLite as it grows | Doris with Postgres fallback | same topology card, source metadata optional |
| connections list/detail | SQLite `process_connections` | Doris `process_connections` | same filters and drilldown |
| top talkers | Redis sorted sets, fallback SQLite | Doris aggregation | same card and API envelope |
| event query | SQLite normalized events/FTS; currently connection projection | Doris events | same citations, guardrails if projection incomplete |
| timeline build | SQLite timeline links; currently connection projection | Doris timeline views | same timeline/Raw tabs |
| exports | SQLite/Postgres recent evidence | Doris long-window evidence | same export flow, explicit source in file metadata |
| ingest health | journal + local projection lag | journal + Doris writer lag | analytics-neutral health copy |

This matrix is the anti-regression contract. When a path works in Doris mode,
small mode should either answer from its read model or explain the exact
bounded limitation without removing the route.

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

## Projection Pipeline

The small-mode projection should be explicit enough to test end to end:

```text
event_ingest_batches.payload
        |
        v
analytics projector
        |
        +--> events
        +--> events_fts
        +--> process_connections
        +--> timeline_entities
        +--> rollups_hourly
        +--> redis hot counters / streams
```

The projector can run inline for small batches and through a bounded background
queue for larger batches. Either way, the journal is the retry boundary. A
batch is not considered fully projected until SQLite writes and Redis hot-state
updates complete. If Redis fails, the batch should still complete with a
warning because Redis is reconstructable. If SQLite fails, mark it
`pending_local` and retry from the journal.

Recommended projector guarantees:

- idempotent upserts keyed by tenant plus event ID, connection ID, or dedup key;
- per-tenant replay cursors so rebuilds can resume;
- maximum batch size, maximum queue depth, and visible lag;
- read queries bounded by tenant, time window, and limit;
- fixture tests that compare projected counts, citations, and timelines after
  restart and rebuild.

## Resource Budget

Default demo target:

- controlplane: 512 MB preferred, 1 GB ceiling for noisy demos;
- Redis: `REDIS_MAXMEMORY=128mb`, container limit around 192 MB;
- SQLite: no daemon, 16 MB default cache, 64 MB upper demo setting;
- console: 256 MB;
- landing and edge services: 128 MB each;
- Doris: 0 MB unless OLAP profile is explicitly selected.

The design goal is one analytic process in small mode: the controlplane itself.

### Minimum-Memory Demo Profile

For the demo host, optimize for deterministic product behavior before analytic
depth. The smallest credible runtime shape is:

- one controlplane process that owns the SQLite writer;
- one Redis container with a hard memory cap and eviction-safe key design;
- one Postgres database that journals every accepted ingest batch;
- zero Doris FE/BE processes unless the `olap` profile is explicitly selected.

The hot path should stay cheap:

```text
ingest accepted in Postgres
        |
        +--> SQLite append/upsert for cited recent evidence
        +--> Redis TTL update for live counters and freshness
        +--> UI reads through backend-neutral analytics APIs
```

Memory guardrails:

- Redis keys are always TTL-bound or reconstructable.
- SQLite cache is intentionally small; query performance comes from narrow
  tenant/time indexes, not memory-heavy buffering.
- The SQLite writer queue has a maximum depth; when saturated, the ingest
  journal remains the durable retry boundary.
- Dashboard widgets must prefer bounded summaries over unbounded scans.
- Large lookback windows should degrade with an explicit guardrail such as
  "older history requires OLAP mode" rather than trying to make SQLite behave
  like a warehouse.

Evidence guardrails:

- Redis is never the citation source.
- SQLite citations include stable `raw_ref` or event IDs that can be traced back
  to the Postgres journal.
- If SQLite is rebuilding, APIs return a successful envelope with source and
  guardrail metadata where possible; they do not show Doris-specific failures
  in small mode.

Think of the tiers as:

| Tier | Role | Memory Behavior | Evidence Role |
| --- | --- | --- | --- |
| Redis | seconds-to-hours live heat | capped, evictable | no citations |
| SQLite/WAL | recent searchable facts | embedded, bounded cache | cited recent evidence |
| Postgres | canonical journal/workflow state | existing product database | replay and audit truth |
| Doris/OLAP | high-volume warehouse | dedicated profile only | long-window/ad hoc analytics |

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
redis:
  maxmemory: 128mb
  maxmemory_policy: allkeys-lru
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

### P0: Demo-Safe Small Mode

1. Keep the current small profile as the default demo deployment.
2. Keep Doris FE/BE stopped unless `ANALYTICS_MODE=olap`,
   `DORIS_ENABLED=true`, and the Compose `olap` profile are all selected.
3. Keep fleet health, connection list/detail, top talkers, event query, and
   timeline build on backend-neutral helpers.
4. Keep IP-scoped network targeting, node documentation top connections, and
   event-capture flow deltas on the selected analytics backend instead of
   direct `dorisClient` calls.
5. Continue using SQLite `process_connections` for cited connection facts and
   bounded IP, node, connection, and correlation pivots.
6. Add Redis sorted-set/hash acceleration for dashboard freshness, top talkers,
   node freshness, and writer lag, with SQLite/Postgres fallback.
7. Rename user-facing and AI health copy from `doris_status` to
   backend-neutral analytics health while preserving the database field until a
   deliberate migration.
8. Run live browser validation against dashboard, network, investigation,
   timeline, and export flows with `source=small-analytics`.

### P1: Bank-Grade Local Projection

1. Expand SQLite from `process_connections` into `events`, FTS5,
   `timeline_entities`, `rollups_hourly`, and enrichment facts.
2. Add replay/restart acceptance tests: ingest, restart controlplane, confirm
   small analytics results survive, delete a SQLite projection, rebuild from the
   Postgres journal, and compare counts/citations.
3. Add admin rebuild and checkpoint commands or jobs:
   `analytics rebuild --tenant`, `analytics quick-check`,
   `analytics checkpoint`, and `analytics retention`.
4. Add source/guardrail metadata to every small-mode API envelope where a query
   uses a partial projection or bounded retention.
5. Track projection lag, queue depth, SQLite DB/WAL size, quick-check status,
   last successful projection time, and Redis eviction count in metrics.

### P2: OLAP Upgrade Compatibility

1. Introduce or complete a backend-neutral `AnalyticsStore` interface so server
   handlers no longer talk directly in Doris terms.
2. Add dual-read fixture tests for small vs OLAP mode so larger customers can
   move to Doris without relearning the UI.
3. Keep Doris migrations, stream loading, and HA runbooks available only for the
   dedicated OLAP profile.
4. Preserve the same UI copy and response envelopes across small and OLAP modes;
   OLAP adds longer retention and higher concurrency, not different workflows.

## Demo Cut Plan

For the near-term demo, do not wait for every warehouse-grade projection. The
small profile is credible when it can prove these flows without Doris:

1. Keep Doris containers stopped and memory at 0 MB in the default deploy.
2. Use SQLite `process_connections` for connection list/detail, IP timelines,
   Raw events, and top-talkers fallback.
3. Add Redis sorted-set acceleration for top talkers and dashboard freshness,
   but preserve SQLite fallback.
4. Add a normalized SQLite `events` table for log-derived and web request
   events that already flow through the unified ingest path.
5. Rename user-facing health/copy from Doris-specific wording to analytics
   backend wording.
6. Add rebuild command or admin job: journal to SQLite, tenant-scoped, with
   progress and lag.
7. Record live acceptance evidence in the go-live issue log: route sweeps,
   memory stats, query timings, restart survival, and no Doris containers.

This lets the demo be fast and honest: "this branch-size deployment runs the
full Control One investigation experience on Postgres, Redis, and embedded
SQLite; Doris is for the high-volume warehouse tier."

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
