# Raw Log Dump Requests Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let analysts request a raw log dump from a node, pulled either from logs the control plane already ingested (`source=control_plane`) or directly from the node agent's spooled bytes (`source=node_agent`), and get a bounded preview + downloadable JSONL artifact with a 7-day retention.

**Architecture:** Two dispatch paths share one `agent_log_dumps` table and one artifact directory. The control-plane path reads from the already-ingested telemetry/log readers and writes the JSONL artifact immediately (`status=captured`). The node-agent path reuses the heartbeat job pipeline (`PendingActions` / `completed_actions[].metadata`) exactly like connectivity tests: a `log_dump` job is created, dispatched via heartbeat, the agent returns `{lines:[...], truncated}` in the completed action metadata, and a heartbeat completed-action processor persists the artifact and flips the row to `captured`. Both paths share tenant-scoped list/preview/download endpoints, window clamping (5m..7d), entity-filter validation, a preview endpoint (first N lines), and a retention sweeper over `expires_at`.

**Tech Stack:** Go (net/http, lib/pq with `decimal` JSONB args, google/uuid), React (existing tailwind + PageHeader/Panel/DataTable kit + Popover/Command), vitest + @testing-library/react, embedded golang-migrate migrations.

**Spec:** `docs/superpowers/specs/2026-09-16-raw-log-dumps-design.md` (includes the `job_id` amendment). **Case-collab Plan A** is in `docs/superpowers/plans/2026-09-16-case-collaboration.md` and is committed separately (`fd206550`); this Plan B is an independent feature on the same `ws3/team-activity` branch but does not depend on Plan A's schema.

**Working directory:** `C:\dev\control_one_ws3` (worktree, branch `ws3/team-activity`). Go module root is the repo root (run Go commands from there); UI commands run from `ui/`. Follow existing gofmt/eslint. Commit frequently on `ws3/team-activity`.

---

### Task 1: Migration 0150 (agent_log_dumps)

**Files:**
- Create: `controlplane/internal/migrate/sql/0150_log_dumps.up.sql`
- Create: `controlplane/internal/migrate/sql/0150_log_dumps.down.sql`

(`job_id` is part of this schema; migration numbering next free is 0150, after the 0149 team-collab used by Plan A and the 0148 team-activity migration already merged.)

- [ ] **Step 1: Write the up migration**

```sql
-- Raw log dump requests (control-plane capture + agent dump via heartbeat).
CREATE TABLE IF NOT EXISTS agent_log_dumps (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    node_id       UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    job_id        UUID REFERENCES jobs(id) ON DELETE SET NULL,
    source        TEXT NOT NULL CHECK (source IN ('control_plane', 'node_agent')),
    entity_filter JSONB NOT NULL DEFAULT '{}'::jsonb,
    window_start  TIMESTAMPTZ NOT NULL,
    window_end    TIMESTAMPTZ NOT NULL,
    status        TEXT NOT NULL DEFAULT 'requested'
                  CHECK (status IN ('requested', 'captured', 'failed')),
    artifact_path TEXT,
    row_count     INTEGER NOT NULL DEFAULT 0,
    size_bytes    BIGINT NOT NULL DEFAULT 0,
    truncated     BOOLEAN NOT NULL DEFAULT FALSE,
    error         TEXT,
    requested_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_log_dumps_tenant_created
    ON agent_log_dumps (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_log_dumps_node
    ON agent_log_dumps (node_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_log_dumps_job
    ON agent_log_dumps (job_id)
    WHERE status = 'requested';
```

- [ ] **Step 2: Write the down migration**

```sql
DROP TABLE IF EXISTS agent_log_dumps;
```

- [ ] **Step 3: Build**

Run: `go build ./...`
Expected: exit 0 (migrations are glob-embedded in `migrate.go`, no manifest to update).

- [ ] **Step 4: Commit**

```bash
git add controlplane/internal/migrate/sql/0150_log_dumps.up.sql controlplane/internal/migrate/sql/0150_log_dumps.down.sql
git commit -m "feat: add agent_log_dumps schema (raw log dump requests)"
```

---

### Task 2: Storage — log dump CRUD

**Files:**
- Create: `controlplane/internal/storage/log_dumps.go`
- Create: `controlplane/internal/storage/log_dumps_test.go`

Mirror the column-list/scan/helper style used by `controlplane/internal/storage/telemetry.go` (see `telemetryLogSelect` / `scanTelemetryLogAround` at telemetry.go:33/103 and the `nullableUUID` arg helper at store.go:473).

- [ ] **Step 1: Write the failing test**

```go
package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLogDumpLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	// tenant T, node N, user U.
	// 1. CreateLogDump(T, N, {source:"node_agent", window:last 1h,
	//    entity:{type:"ip",value:"10.0.0.5"}})
	//    -> dumps[?].Source=="node_agent", Status=="requested".
	// 2. ListLogDumps(T, LogDumpFilter{TenantID:T}) -> contains the row.
	// 3. MarkLogDumpCaptured(dump.ID, "log-dump-<id>.jsonl", 120, 4096, false)
	//    -> Status=="captured", ArtifactPath set, RowCount==120.
	// 4. GetLogDump(T, dump.ID) -> captured path matches; GetLogDump with a
	//    different tenant -> sql.ErrNoRows.
	// 5. CreateLogDump with window spanning >7d -> decodes fine (server
	//    clamps), and a requested row older than its expires_at is returned
	//    by ListExpiredLogDumps; DeleteExpiredLogDumps removes it.
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./controlplane/internal/storage/ -run TestLogDumpLifecycle -short`
Expected: FAIL — `CreateLogDump` undefined.

- [ ] **Step 3: Implement**

```go
package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LogDump is one raw log dump request / captured artifact row.
type LogDump struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	NodeID       uuid.UUID      `json:"node_id"`
	JobID        uuid.NullUUID  `json:"job_id,omitempty"`
	Source       string         `json:"source"`
	EntityFilter map[string]any `json:"entity_filter"`
	WindowStart  time.Time      `json:"window_start"`
	WindowEnd    time.Time      `json:"window_end"`
	Status       string         `json:"status"`
	ArtifactPath string         `json:"artifact_path,omitempty"`
	RowCount     int            `json:"row_count"`
	SizeBytes    int64          `json:"size_bytes"`
	Truncated    bool           `json:"truncated"`
	Error        string         `json:"error,omitempty"`
	RequestedBy  uuid.NullUUID  `json:"requested_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	ExpiresAt    time.Time      `json:"expires_at"`
}

const logDumpSelect = `id, tenant_id, node_id, job_id, source, entity_filter,
	window_start, window_end, status, artifact_path, row_count, size_bytes,
	truncated, error, requested_by, created_at, expires_at`

func scanLogDump(row interface{ Scan(...any) error }) (LogDump, error) {
	var d LogDump
	var nodeID, jobID, reqBy sql.NullString
	var efRaw []byte
	if err := row.Scan(
		&d.ID, &d.TenantID, &nodeID, &jobID, &d.Source, &efRaw,
		&d.WindowStart, &d.WindowEnd, &d.Status, &d.ArtifactPath,
		&d.RowCount, &d.SizeBytes, &d.Truncated, &d.Error,
		&reqBy, &d.CreatedAt, &d.ExpiresAt); err != nil {
		return d, err
	}
	if nodeID.Valid {
		if id, err := uuid.Parse(nodeID.String); err == nil {
			d.NodeID = id
		}
	}
	if jobID.Valid {
		d.JobID = uuid.NullUUID{UUID: uuid.MustParse(jobID.String), Valid: true}
	}
	if reqBy.Valid {
		d.RequestedBy = uuid.NullUUID{UUID: uuid.MustParse(reqBy.String), Valid: true}
	}
	if len(efRaw) > 0 && string(efRaw) != "null" {
		_ = json.Unmarshal(efRaw, &d.EntityFilter)
	}
	return d, nil
}

// CreateLogDump inserts a dump request row (job_id is optional/nullable).
func (s *Store) CreateLogDump(ctx context.Context, p LogDump) (*LogDump, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx,
		`INSERT INTO agent_log_dumps (id, tenant_id, node_id, job_id, source,
			entity_filter, window_start, window_end, status, requested_by, expires_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING `+logDumpSelect,
		p.ID, p.TenantID, p.NodeID, nullableUUID(p.JobID.UUID), p.Source,
		jsonMarshalArgLogic(p.EntityFilter), p.WindowStart, p.WindowEnd, p.Status,
		nullableUUID(p.RequestedBy.UUID), p.ExpiresAt)
	d, err := scanLogDump(row)
	if err != nil {
		return nil, fmt.Errorf("create log dump: %w", err)
	}
	return &d, nil
}

// MarkLogDumpCaptured persists the artifact path + counts on a requested row.
func (s *Store) MarkLogDumpCaptured(ctx context.Context, id uuid.UUID, path string, rowCount int, sizeBytes int64, truncated bool) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_log_dumps SET status='captured', artifact_path=$2, row_count=$3,
			size_bytes=$4, truncated=$5, error=NULL
		WHERE id=$1 AND status='requested'`,
		id, path, rowCount, sizeBytes, truncated)
	if err != nil {
		return fmt.Errorf("mark log dump captured: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkLogDumpFailed records a terminal failure on a dump row.
func (s *Store) MarkLogDumpFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if errMsg == "" {
		errMsg = "log dump failed"
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_log_dumps SET status='failed', error=$2 WHERE id=$1 AND status='requested'`,
		id, errMsg)
	if err != nil {
		return fmt.Errorf("mark log dump failed: %w", err)
	}
	return nil
}

// GetLogDump fetches a tenant-scoped dump row.
func (s *Store) GetLogDump(ctx context.Context, tenantID, id uuid.UUID) (*LogDump, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+logDumpSelect+` FROM agent_log_dumps WHERE id=$1 AND tenant_id=$2`,
		id, tenantID)
	d, err := scanLogDump(row)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ListLogDumps returns paged dumps newest-first for a tenant.
func (s *Store) ListLogDumps(ctx context.Context, filter LogDumpFilter, limit, offset int) ([]LogDump, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if filter.TenantID == uuid.Nil {
		return nil, 0, errors.New("tenant filter required")
	}
	conds := []string{"tenant_id = $1"}
	args := []any{filter.TenantID}
	if filter.NodeID != uuid.Nil {
		args = append(args, filter.NodeID)
		conds = append(conds, "node_id = $2")
	}
	if strings.TrimSpace(filter.Status) != "" {
		args = append(args, filter.Status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_log_dumps WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count log dumps: %w", err)
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(
		`SELECT %s FROM agent_log_dumps WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		logDumpSelect, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query log dumps: %w", err)
	}
	defer rows.Close()
	out := make([]LogDump, 0, limit)
	for rows.Next() {
		d, err := scanLogDump(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan log dump: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate log dumps: %w", err)
	}
	return out, total, nil
}
```

Note on `jsonMarshalArgLogic` + `LogDumpFilter` + helpers: the storage JSONB-arg and JSONB-scan patterns already exist in this package (`json.Marshal` + `nullableUUID` for args; the `scanLogTail`/telemetry scan style for rows). Follow the existing telemetry-log scan helper names — do **not** introduce new helper names; reuse `nullableUUID` (store.go:473), the `sql.NullString`-based nullable-UUID scan used by `scanTelemetryLog...`, and the JSON-labels map decode used by telemetry (`decodeStringMap` at telemetry.go). The exact JSONB-arg helper name must match an existing one in this package (grep `func .*JSON.*Arg(` / `func mapToJSONString`); pick whichever exists, and if none exists, marshal inline like `connectivity_test` payloads do.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./controlplane/internal/storage/ -run TestLogDumpLifecycle -short`
Expected: PASS (with a configured DB), skips otherwise.

- [ ] **Step 5: gofmt/vet + commit**

```bash
gofmt -w controlplane/internal/storage/log_dumps.go controlplane/internal/storage/log_dumps_test.go
go vet ./controlplane/internal/storage/
git add controlplane/internal/storage/log_dumps.go controlplane/internal/storage/log_dumps_test.go
git commit -m "feat: add raw log dump storage (CRUD + expiry query)"
```

---

### Task 3: Server — job registration + heartbeat dispatch

**Files:**
- Modify: `controlplane/internal/server/job_types.go` (register `JobTypeLogDump`)
- Modify: `controlplane/internal/server/heartbeat.go` (pending-action append + completed-action switch case + processor dispatch)
- Create: `controlplane/internal/server/log_dumps.go` (dispatch + completed-action processor — mirrors `connectivity.go`)
- Test: `controlplane/internal/server/heartbeat_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestHeartbeatQueuesPendingLogDumpAction(t *testing.T) {
	// one requested node_agent dump for node N; after handleHeartbeat,
	// resp.PendingActions contains "log_dump:<job_id>" and the job is running.
}

func TestProcessLogDumpCompletedActionCapturesArtifact(t *testing.T) {
	// completed success with metadata lines ["10.0.0.5 80 OK","..."] and
	// truncated=false -> store.MarkLogDumpCaptured(path, 2, bodyLen, false),
	// job marked succeeded, artifact persisted under the dumps dir.
}

func TestProcessLogDumpCompletedActionFailure(t *testing.T) {
	// status=failed + error -> dump row failed + job failed.
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./controlplane/internal/server/ -run 'TestHeartbeatQueuesPendingLogDumpAction|TestProcessLogDumpCompletedAction'`
Expected: FAIL — `JobTypeLogDump`, `appendPendingLogDumpActions`, `processLogDumpCompletedAction` undefined.

- [ ] **Step 3: Register the job definition**

In `controlplane/internal/server/job_types.go`, next to the `JobTypeConnectivityTest` registration block (job_types.go:161-164):

```go
	registerJobDefinition(JobTypeLogDump, jobDefinition{
		RequiresTenant: true,
		Validate:       nil,
	})
```

Add the constant next to `JobTypeConnectivityTest` (job_types.go:16-17 area — currently in `connectivity.go:16`; declare it consistently with that group, e.g. in `job_types.go` or alongside `JobTypeConnectivityTest`):

```go
// JobTypeLogDump requests a raw log dump from the node agent.
const JobTypeLogDump = "log_dump"
```

- [ ] **Step 4: Append the pending action in heartbeat**

Model the dispatch on `appendPendingConnectivityTestActions` (heartbeat.go:412-428) — a sibling `appendPendingLogDumpActions` that lists `requested` node_agent dumps for the node and appends `JobTypeLogDump+":"+jobID` to `resp.PendingActions`, marking each job running. Add the call into `handleHeartbeat` where the connectivity-test appends happen.

- [ ] **Step 5: Completed-action switch case**

In `processHeartbeatCompletedActions` (heartbeat.go:938), after the `case JobTypeConnectivityTest:` that calls `processErrorAwareConnectivityTest` / `processConnectivityTestCompletedAction` (connectivity.go:144):

```go
		case JobTypeLogDump:
			s.processLogDumpCompletedAction(ctx, jobID, c)
```

- [ ] **Step 6: Implement the completed-action processor**

Create `controlplane/internal/server/log_dumps.go`, mirroring `connectivity.go:144-172` (`processConnectivityTestCompletedAction`, using `metadataBool(c.Metadata, "...")` and the job-status update guards):

```go
package server

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// processLogDumpCompletedAction persists the agent-reported dump artifact.
func (s *Server) processLogDumpCompletedAction(ctx context.Context, jobID uuid.UUID, c heartbeatCompletedAction) {
	if s.store == nil {
		return
	}
	errMsg := strings.TrimSpace(c.Error)
	if c.Status == "succeeded" {
		var lines []string
		if raw, ok := c.Metadata["lines"]; ok {
			lines = stringSliceFromAny(raw)
		}
		lines = sanitizeForLogDump(lines)
		perr := s.persistLogDumpFromLines(ctx, jobID, lines, metadataBool(c.Metadata, "truncated"))
		if perr != nil {
			errMsg = perr.Error()
		} else {
			if jerr := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "agent reported log dump success", c.Metadata); jerr != nil {
				s.logger.Warn("log dump job mark succeeded",
					zap.String("job_id", jobID.String()), zap.Error(jerr))
			}
			return
		}
	} else {
		if errMsg == "" {
			errMsg = "agent reported log dump failure"
		}
	}
	if jerr := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, errMsg, c.Metadata); jerr != nil {
		s.logger.Warn("log dump job mark failed",
			zap.String("job_id", jobID.String()), zap.Error(jerr))
	}
}
```

The `persistLogDumpFromLines` helper looks up the dump row by `job_id` (a new `store.LogDumpForJobID` storage method + a `Server.logDumpArtifactDir()`), joins lines, persists via the tmp→rename `persistLogDumpArtifact` helper (reuse the audit-report pattern in `audit_report_artifacts.go:21-61`), then calls `store.MarkLogDumpCaptured`. When lookup/fail, call `store.MarkLogDumpFailed`. Use `metadataBool`/`stringSliceFromAny`/`sanitizeStringSlice` (existing server helpers: webservers.go:615, control_room.go:2084, heartbeat.go:537).

- [ ] **Step 7: Run to verify they pass**

Run: `go test ./controlplane/internal/server/ -run 'TestHeartbeatQueuesPendingLogDumpAction|TestProcessLogDumpCompletedAction'`
Expected: PASS.

- [ ] **Step 8: gofmt/vet/build + commit**

```bash
gofmt -w controlplane/internal/server/log_dumps.go controlplane/internal/server/job_types.go controlplane/internal/server/heartbeat.go
go vet ./controlplane/internal/server/
go build ./...
git add controlplane/internal/server/job_types.go controlplane/internal/server/heartbeat.go controlplane/internal/server/log_dumps.go controlplane/internal/server/heartbeat_test.go
git commit -m "feat: add heartbeat-driven raw log dump job pipeline"
```

---

### Task 4: Server — request + preview/download endpoints

**Files:**
- Create: `controlplane/internal/server/log_dumps_api.go`
- Create: `controlplane/internal/server/log_dump_artifacts.go` (artifact dir + tmp→rename persistence)
- Modify: `controlplane/internal/server/server.go` (routes + sweeper wiring)
- Test: `controlplane/internal/server/log_dumps_api_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestLogDumpRequestValidatesWindowAndEntity(t *testing.T) {
	// POST /api/v1/log-dumps with window_start/end spanning >7d or <5m, or an
	// entity type outside {ip,host,domain,process,file,hash,user,service}
	// -> 400.
}

func TestLogDumpRequestNodeAgentSourceGatedOnNode(t *testing.T) {
	// node offline (last_seen_at > 5m) + source=node_agent -> 422 with
	// "Node offline — use Control plane source" copy; online but no
	// log_dump capability -> 422 similar.
}

func TestLogDumpRequestControlPlaneCaptures(t *testing.T) {
	// source=control_plane -> job nil, dump row captured immediately,
	// artifact exists with row_count > 0.
}

func TestLogDumpRequestNodeAgentDispatches(t *testing.T) {
	// online + capability -> 202 with {job_id, status:"requested"} and a job
	// row + pending action.
}

func TestLogDumpListAndPreview(t *testing.T) {
	// GET /api/v1/log-dumps?tenant_id -> paged; GET /{id}/preview?lines=200
	// -> first N lines + {total_rows, truncated}; foreign tenant -> 404.
}

func TestLogDumpDownloadPathEscapesDir(t *testing.T) {
	// artifact_path outside the dumps dir -> no file served (mirror the
	// audit-report artifact escape test).
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./controlplane/internal/server/ -run 'TestLogDumpRequest|TestLogDumpList|TestLogDumpDownload'`
Expected: FAIL.

- [ ] **Step 3: Artifact persistence helper**

Create `controlplane/internal/server/log_dump_artifacts.go`, mirroring `audit_report_artifacts.go` (env `CONTROL_ONE_DUMPS_DIR`, default `/var/lib/control-one/dumps`):

```go
package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultLogDumpArtifactDir = "/var/lib/control-one/dumps"

func logDumpArtifactDir() string {
	if dir := strings.TrimSpace(os.Getenv("CONTROL_ONE_DUMPS_DIR")); dir != "" {
		return dir
	}
	return defaultLogDumpArtifactDir
}

// persistLogDumpArtifact writes dump bytes to a subdir of the dumps dir using
// an atomic tmp-then-rename and returns the absolute artifact path.
func persistLogDumpArtifact(dumpID uuid.UUID, body []byte, truncated bool) (string, error) {
	dir := logDumpArtifactDir()
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", fmt.Errorf("create log dump artifact dir: %w", err)
	}
	name := "log-dump-" + dumpID.String() + ".jsonl"
	path := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, "."+name+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("create log dump artifact temp file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("write log dump artifact: %w", err)
	}
	if err := tmp.Chmod(0640); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("chmod log dump artifact: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("close log dump artifact: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", fmt.Errorf("publish log dump artifact: %w", err)
	}
	cleanup = false
	return path, nil
}
```

- [ ] **Step 4: Implement the request handler**

Create `controlplane/internal/server/log_dumps_api.go`. Follow `handleConnectivityTest` (connectivity.go:32) for node lookup + role gating, plus a `persistLogDumpRequest` server helper that performs the control-plane stream (reusing `s.listLogTail`/`listLogEvents` readers + the artifact persist) or the node-agent dispatch (job create → `JobTypeLogDump` → `resp.PendingActions`). Gating helpers: `nodeAdvertisesCapability(node, "log_dump")` (webservers.go:217) and window clamping `clampLogDumpWindow(start,end)` clamping to [5m,7d].

Consolidated spec rules to honor (same as `handleConnectivityTest`):
- `authorize(s, w, r, roleInvestigator, roleOperator, roleAdmin)` + `requireTeamTenantFromQuery`, then `requireNodeAccess`.
- Window default last 1h, clamped to [5m, 7d] (server-side clamp, not reject).
- Entity type in {ip, host, domain, process, file, hash, user, service}; method must be POST.
- node_agent: require node online (last_seen_at within 5m) + capability `log_dump`, else 422 with human copy.
- control_plane: stream rows from the same readers `log-tail` uses (telemetry logs), write JSONL artifact immediately, mark `captured`, `expires_at = now+7d`.
- Response `{dump_id, node_id, source, status, job_id?}` (202 for agent path, 200 for captured path).

Routes (server.go next to the connectivity/log-tail route block):

```go
s.baseRouter.HandleFunc("/api/v1/log-dumps", s.handleLogDumps)
s.baseRouter.HandleFunc("/api/v1/log-dumps/", s.handleLogDumps)
```

- [ ] **Step 5: Preview + download endpoints**

- `GET /api/v1/log-dumps/{id}/preview?tenant_id&lines=200` → `{lines, total_rows, truncated}` (first N lines read off the artifact file; tenant-scoped via `requireLogDumpAccess`).
- `GET /api/v1/log-dumps/{id}/download?tenant_id` → attachment `log-dump-<id>.jsonl` (artifact path must resolve inside `logDumpArtifactDir()`).

- [ ] **Step 6: Sweeper**

Add a retention sweeper (pattern of `review_reminder_scheduler.go`/`server.go` sched wiring ~3026-3091): every hour, `DeleteExpiredLogDumps` + remove artifact files. Wire `.Start(...)` in the same `if store != nil {` block that starts the other schedulers.

- [ ] **Step 7: gofmt/vet/build + commit**

```bash
gofmt -w controlplane/internal/server/log_dumps_api.go controlplane/internal/server/log_dump_artifacts.go controlplane/internal/server/server.go
go vet ./controlplane/internal/server/
go build ./...
git add controlplane/internal/server/log_dumps_api.go controlplane/internal/server/log_dump_artifacts.go controlplane/internal/server/server.go controlplane/internal/server/log_dumps_api_test.go
git commit -m "feat: add raw log dump request, preview, download endpoints"
```

---

### Task 5: UI — api.ts client methods

**Files:**
- Modify: `ui/src/lib/api.ts`

- [ ] **Step 1: Add types + methods** (follow the existing `requestLogTail`/`connectivityTest` shapes ~api.ts)

```ts
export type LogDumpSource = 'control_plane' | 'node_agent';
export type LogDumpStatus = 'requested' | 'captured' | 'failed';

export interface LogDump {
  id: string;
  tenant_id: string;
  node_id: string;
  job_id?: string;
  source: LogDumpSource;
  entity_filter: Record<string, unknown>;
  window_start: string;
  window_end: string;
  status: LogDumpStatus;
  artifact_path?: string;
  row_count: number;
  size_bytes: number;
  truncated: boolean;
  error?: string;
  created_at: string;
  expires_at: string;
}

export interface LogDumpPreview {
  lines: string;
  total_rows: number;
  truncated: boolean;
}
```

```ts
  async requestLogDump(nodeId: string, tenantId: string, body: LogDumpRequestParams): Promise<LogDumpRequestResponse> {
    return this.request<LogDumpRequestResponse>(`/api/v1/log-dumps`, {
      method: 'POST',
      body: JSON.stringify(body),
      headers: { 'x-tenant-id': tenantId, 'x-node-id': nodeId },
    });
  }

  async listLogDumps(params: { tenantId: string; nodeId?: string; status?: LogDumpStatus; limit?: number; offset?: number }): Promise<PaginatedResponse<LogDump>> {
    const p = new URLSearchParams({ tenant_id: params.tenantId });
    if (params.nodeId) p.set('node_id', params.nodeId);
    if (params.status) p.set('status', params.status);
    if (params.limit) p.set('limit', String(params.limit));
    if (params.offset) p.set('offset', String(params.offset));
    return this.request<PaginatedResponse<LogDump>>(`/api/v1/log-dumps?${p}`);
  }

  async logDumpPreview(dumpId: string, tenantId: string, lines = 200): Promise<LogDumpPreview> {
    return this.request<LogDumpPreview>(`/api/v1/log-dumps/${dumpId}/preview?tenant_id=${tenantId}&lines=${lines}`);
  }

  logDumpDownloadUrl(dumpId: string, tenantId: string): string {
    return `/api/v1/log-dumps/${dumpId}/download?tenant_id=${tenantId}`;
  }
```

- [ ] **Step 2: Verify**

Run: `npx tsc --noEmit`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add ui/src/lib/api.ts
git commit -m "feat: add log dump api client methods"
```

---

### Task 6: UI — request dialog + list/preview on the log-tail and investigate pages

**Files:**
- Create: `ui/src/components/events/LogDumpDialog.tsx`
- Create: `ui/src/components/events/LogDumpList.tsx`
- Create: `ui/src/components/events/LogDumpPreviewDrawer.tsx`
- Modify: `ui/src/pages/LogTail.tsx` (confirm exact filename — the log-tail page)
- Modify: `ui/src/pages/Investigate.tsx` (confirm exact filename — entity pages)
- Test: `ui/src/components/events/LogDumpDialog.test.tsx`, `ui/src/components/events/LogDumpList.test.tsx`

- [ ] **Step 1: Write the failing tests**

```tsx
it('disables the node-agent source when the node is offline', () => {
  // render with node last_seen_at > 5m; expect the disabled radio with
  // "Node offline — use Control plane source" reason text.
});

it('requests a dump from the control plane and polls to captured', async () => {
  // submit -> requestLogDump called; advance mock timers -> status captured;
  // expect a "Captured N lines" summary.
});

it('shows the truncated preview banner for large dumps', () => {
  // preview with truncated=true -> "showing first 200 of N" banner.
});

it('shows a retryable failed row', () => {
  // status=failed + error -> actionable "retry" surfaced; requested-but-stale
  // rows show "agent did not respond".
});
```

- [ ] **Step 2: Run to verify they fail**

Run: `npx vitest run src/components/events/LogDumpDialog.test.tsx`
Expected: FAIL — components missing.

- [ ] **Step 3: Implement**

- `LogDumpDialog.tsx`: Modal with **Node** (pre-filled from current context/segment), **Window** (start/end, default last 1h, clamped 5m..7d), **Entity** (preset from investigate context or optional), **Source** radio (control_plane always enabled "Already-ingested logs (instant)"; node_agent disabled + reason when offline / no capability). Submit → POST → show requested state → poll `listLogDumps` every 3s up to 2m → captured/failed.
- `LogDumpList.tsx`: table of recent dumps for the node (DataTable, 25/page): id, source, window, row_count/size_bytes, status badge, created_at, Preview/Download for captured.
- `LogDumpPreviewDrawer.tsx`: first 200 lines + "showing first 200 of N · download full dump" banner when truncated; failed → error + retry.

Follow the existing DataTable/Panel/EmptyState/StatusTag/Pagination kit components and the PageHeader styling used across the app; reuse the Submit-button/poll spinner style from other dialogs.

- [ ] **Step 4: Run to verify they pass**

Run: `npx vitest run src/components/events/`
Expected: PASS.

- [ ] **Step 5: Verify**

Run: `npx eslint src/components/events/ src/pages/LogTail.tsx src/pages/Investigate.tsx; npx tsc --noEmit`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add ui/src/components/events/ ui/src/pages/LogTail.tsx ui/src/pages/Investigate.tsx
git commit -m "feat: add raw log dump request + preview UI"
```

---

### Task 7: Final verification

- [ ] **Step 1: Backend**

Run: `go build ./...; go vet ./controlplane/...; gofmt -l controlplane/`
Expected: clean (no output from gofmt -l).
Then: `go test ./controlplane/internal/server/ ./controlplane/internal/storage/ -short`
Expected: only the known pre-existing Postgres-integration + `IpLifecyclePanel` failures; new tests pass.

- [ ] **Step 2: Frontend**

Run: `npx tsc --noEmit; npx vitest run` (excluding the pre-existing IpLifecyclePanel suite). Expected: green.

- [ ] **Step 3: Repo hygiene**

Verify no stray artifacts staged:
```bash
git status --porcelain
```
Confirm only intended files; never stage `.opencode/`, `deploy/__pycache__/`, `docs/temp_query.sql`, PNG droppings, or `videos/`.

- [ ] **Step 4: Final commit + note**

```bash
git log --oneline -12
```
Summarize branch state; the user merges to the main repo themselves.
