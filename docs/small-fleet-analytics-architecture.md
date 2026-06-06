# Small-Fleet Analytics Architecture

Status: Proposal

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
- Events query: SQLite `events` with B-tree filters and FTS5 search.
- Timeline build: SQLite `timeline_entities` plus typed events, merged with
  existing Postgres lifecycle items.
- Investigation enrichment: SQLite for recent facts, Postgres for durable case,
  alert, audit, and compliance facts.

Responses should expose `source: "small-analytics"` or more granular
`source: "redis+sqlite"` so audits can prove the active backend.

## Ingest Flow

1. Validate and normalize incoming telemetry/events.
2. Write the replay journal in Postgres.
3. Fan out in-memory events/webhooks exactly as today.
4. Update Redis hot counters and streams with TTLs.
5. Append typed facts to the SQLite writer queue.
6. The SQLite writer commits the batch in one transaction.
7. Mark the journal batch `analytics_status = local-complete`.
8. Optional: mirror to Doris when `analytics.mode = olap` or `mirror_doris =
   true`.

If SQLite is unavailable, ingest still writes the Postgres journal, marks
analytics pending, and the UI reports degraded analytic freshness rather than
losing events or consuming unbounded memory.

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
  hot_retention_days: 7
  rollup_retention_days: 90
  redis_hot_ttl: 24h
  mirror_doris: false
```

For the current demo VPS:

- set `CONTROLPLANE_DORIS_ENABLED=false`;
- stop/remove Doris FE and BE from the default compose profile;
- keep Redis;
- mount `/opt/control-one/deploy/analytics` into the controlplane container;
- expose analytics health in `/healthz` detail/admin health endpoints.

For larger installs:

- set `analytics.mode=olap`;
- run Doris/OpenSearch/warehouse on dedicated hosts;
- optionally dual-write from small analytics to OLAP during migration.

## Migration Plan

1. Introduce `internal/analytics` interfaces and adapt existing Doris types into
   backend-neutral structs.
2. Build `smallanalytics` SQLite schema, migrations, writer, and query methods.
3. Route fleet health and top talkers first. These have the highest dashboard
   impact and can use Redis/rollups immediately.
4. Route connection list/detail to SQLite.
5. Route events query and timeline build to SQLite with FTS5 and
   `timeline_entities`.
6. Add dual-read comparison tests: Doris vs SQLite fixtures return equivalent
   normalized rows and citations.
7. Add deploy profile `small` that disables Doris and enables the SQLite volume.
8. Run live audit against `source: small-analytics`, then remove Doris from the
   demo host.

## Non-Goals

- Do not remove Doris from the codebase.
- Do not remove investigation, timeline, search, top-talkers, or connection
  drilldown features.
- Do not make Redis the evidence store.
- Do not call the small-fleet mode a replacement for a high-volume bank SIEM
  warehouse. It is the correct default for demo, branch, and low-EPS fleets.

## Recommended First Implementation Slice

Build the small analytics backend behind a feature flag and migrate only these
paths first:

1. `/api/v1/fleet/health`
2. `/api/v1/connections/top-talkers`
3. `/api/v1/connections?tenant_id=...`

That slice removes the current Doris dashboard pressure, proves the Redis/SQLite
model under live browser use, and leaves the deeper investigation paths on Doris
until the SQLite timeline/query implementation is ready.
