# Design: Raw log dump requests (control plane + node agents)

Date: 2026-09-16
Scope: Control plane + web UI (worktree `control_one_ws3`, branch `ws3/team-activity`).
Status: Design (pending approval → implementation plan).

## Problem

Analysts need raw log bytes for an alert/IP/service around a time window.
Today `GET /api/v1/nodes/{id}/log-tail` only replays **already-ingested** logs
(tail of Postgres `telemetry_logs` / Doris, hard `limit=20`). Logs still
sitting on a node agent are unreachable.

This spec adds a "request raw log dump" flow that produces a single JSONL
artifact from either source:
- **Control plane source** — reads already-ingested central logs at full
  depth (beyond the tail limit).
- **Node agent source** — dispatches a job over the existing heartbeat
  channel (`PendingActions` / `completed_actions[].metadata`); the agent
  returns raw lines it has not (yet) shipped.

## Non-goals

- No agent re-architecture (agents remain pollers; no outbound push API).
- No live streaming/tailing of agent logs (poll of a completed dump).
- No change to existing `log-tail` behavior.
- Dumps are text/JSONL only — no pcaps/network capture in this slice.

## Key existing mechanisms (reused, not invented)

- Job/action channel: `POST /api/v1/nodes/{id}/heartbeat` returns
  `PendingActions`; agents return `completed_actions` `{action, job_id, status,
  error, metadata}` — metadata is the documented result payload channel
  (`heartbeat.go:97-106`, consumed in `processHeartbeatCompletedActions`,
  switch keyed by job type).
- Canonical new-job template: `connectivity.go` (`POST /api/v1/nodes/{id}/connectivity-test`
  → create `Job` → append pending action → `{job_id, node_id, status}`).
- Capability gating: `updateNodeAgentCapabilities`/`normalizeAgentCapabilities`
  (heartbeat labels `agent.capabilities`); same pattern as `ai_logfixer`
  (`ai_logfixer_remediation.v1`).
- Online gating: `nodes.last_seen_at` (`TouchNodeHeartbeat` updates it).
- Artifact persistence: `audit_report_artifacts.go` writes a file to a local
  artifact dir (tmp → publish, validated path inside dir), DB stores the path,
  attachment endpoint serves it. Feature B mirrors this exactly.

## Schema (migration `0150_log_dumps`)

```sql
CREATE TABLE IF NOT EXISTS agent_log_dumps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id         UUID REFERENCES nodes(id) ON DELETE CASCADE,
    source          TEXT NOT NULL CHECK (source IN ('control_plane', 'node_agent')),
    entity_filter   JSONB NOT NULL DEFAULT '{}'::jsonb,
    window_start    TIMESTAMPTZ NOT NULL,
    window_end      TIMESTAMPTZ NOT NULL,
    status          TEXT NOT NULL DEFAULT 'requested'
                    CHECK (status IN ('requested', 'captured', 'failed')),
    artifact_path   TEXT,          -- validated path inside dumps artifact dir
    row_count       INTEGER NOT NULL DEFAULT 0,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    truncated       BOOLEAN NOT NULL DEFAULT false,
    error           TEXT,
    requested_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_log_dumps_tenant_created
    ON agent_log_dumps (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_log_dumps_node
    ON agent_log_dumps (node_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_log_dumps_expires
    ON agent_log_dumps (expires_at)
    WHERE status = 'captured';
```

`entity_filter` JSONB: `{type: "ip"|"host"|"domain"|"process"|"file"|"hash"|"user"|"service", value: "…"}` — "service" also accepts `{service:"http"}` for agent service-level filtering. Window clamped server-side to min 5m, max 7d.

## Backend

All endpoints: `authorize(roleInvestigator, roleOperator, roleAdmin)` +
`requireTenantAccessFromQuery`; node verified in tenant via `ensureNodeInTenant`.

### Request (POST /api/v1/nodes/{id}/log-dump)

Body: `{source: "control_plane"|"node_agent", window_start, window_end, entity: {type, value}, limit_lines?}`.
- Validate window (5m..7d), entity type/value (allowed set), node.
- **node_agent**: reject with 422 + human copy if node offline (`last_seen_at`
  older than 5 minutes) or capability `log_dump` absent. Otherwise mirror
  `connectivity.go`: insert `Job{type:"log_dump"}` → append pending action
  `log_dump:<job_id>` → insert `agent_log_dumps` row `status=requested`,
  `expires_at = now()+7d`.
- **control_plane**: stream rows from the same readers `log-tail` uses
  (`telemetry_logs` / Doris events), scoped to tenant + node + window + entity
  filter. Write JSONL artifact immediately (persist → attach → update), mark
  `status=captured`. Cap size (row cap; default 50MB) — `truncated=true` past cap.
- Both return `{dump_id, node_id, source, status}`; the 7d retention / row cap
  are fixed.

### Agent result (completed_actions handling)

New `case JobTypeLogDump:` in `processHeartbeatCompletedActions`:
- `status=succeeded`: `metadata` carries `{lines: [ …raw lines… ], truncated}`.
  Persist lines to the artifact file (persist → attach → update), row
  `status=captured`, `row_count`, `size_bytes`, `truncated`. Cap applied here
  too (metadata size cap, e.g. 10MB default); oversized payloads flagged
  `truncated`.
- `status=failed`: row `status=failed`, `error` from the action.
- Jobs table retries govern the agent-side transient failures (existing
  semantics, unchanged).

### Read endpoints

- `GET /api/v1/log-dumps?tenant_id&limit&offset` — paginated list (id, node,
  source, window, status, row_count, size_bytes, truncated, created_at,
  expires_at). Only captured rows advertise preview/download.
- `GET /api/v1/log-dumps/{id}/preview?tenant_id&lines=200` — first N lines
  from the artifact file + `{total_rows, truncated}`. Tenant-scoped.
- `GET /api/v1/log-dumps/{id}/download?tenant_id` — attachment
  `Content-Disposition; filename=log-dump-<id>.jsonl`. Tenant-scoped.

### Cleanup

Small sweeper (pattern of `review_reminder_scheduler`): every hour, delete
`captured` rows with `expires_at < now()` and their artifact files. Keeps the
artifact dir from growing unbounded.

## UI

### Request dialog (shared component, two entry points)
Fields: **Node** (combobox of tenant nodes; preset + locked on `log-tail`,
required on entity pages), **Window** (start/end, default last 1h, clamp 5m..7d),
**Entity** (preset from context: the IP/host/domain being investigated; optional),
**Source** radio:
- Control plane — always available, label "Already-ingested logs (instant)".
- Node agent — disabled state + reason when offline or lacking capability,
  e.g. "Node offline — use Control plane source" / "Agent can't dump logs".
Submit → POST, show requested state, then **poll `GET /log-dumps`** every 3s
(up to 2m) until `captured`/`failed`.

### Dump list + preview
- New "Log dumps" panel/section on the `log-tail` page listing recent dumps
  for the node (Pagination), with status badge, row/size counts, and
  Preview/Download for captured rows.
- Preview drawer/modal: first 200 lines + banner "showing first 200 of N ·
  download full dump" when truncated.
- Failed rows show the stored error with actionable copy; stale `requested`
  rows surface as "agent did not respond — retry".

### Copy notes
Human, standard product tone: "Requesting raw logs from node…", "Agent
captured N lines", "Node offline — use Control plane source". No emojis, no
AI-voice phrasing.

## Testing

- Storage: create/list/get dumps + artifact path validation (escape check
  against dumps dir, mirroring the report artifact test), cleanup expiry.
- Server: handler tests via `fakeStore` — request validation (window clamp,
  entity whitelist, 422 offline/no-capability), control-plane capture path,
  completed-action success/failure handling, preview/download tenant + status
  gating.
- UI: dialog (source disable reasons, submit → poll → captured), preview
  truncated banner, list pagination, error states.

## Risks / notes

- Metadata channel size: heartbeat metadata is a bounded field; oversized
  dumps are truncated + flagged, never silently dropped. Artifact cap keeps
  DB rows small (paths only, not payloads).
- Node offline with pending dump: job stays `requested` until heartbeat; UI
  gives up at 2m with an explicit retry.
- Doris fallback: entity/window filtering on Doris events may be coarse; the
  control-plane reader shares the existing `log-tail` semantics, so behavior
  is consistent with today.