# Node Process Inventory and Response Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Provide a current node process and listener inventory plus approval-gated process termination and port blocking that is executed only by an enrolled agent.

**Architecture:** Agents submit bounded process snapshots during heartbeat and the control plane atomically replaces that node’s inventory. A requested node response creates a high-risk action plan in `needs_approval`; an independent operator approval releases a single pending agent action, and the node agent executes it only after validating its current process identity or port request. Receipts provide the authoritative terminal result.

**Tech Stack:** Go, PostgreSQL migrations, agent heartbeat protocol, React/TypeScript, Vitest.

---

## File map

- `controlplane/internal/migrate/sql/0152_node_processes_and_response_actions.{up,down}.sql` — process snapshot and response-action records.
- `controlplane/internal/storage/node_processes.go` — atomically replace and list a node’s process snapshot.
- `controlplane/internal/storage/node_response_actions.go` — pending/approved/dispatched/completed response-action storage.
- `controlplane/internal/storage/node_processes_test.go` and `node_response_actions_test.go` — storage tests.
- `controlplane/internal/server/heartbeat.go` — heartbeat contract, inventory persistence, receipt intake, and approved-action dispatch.
- `controlplane/internal/server/node_response_actions.go` — authorization, validation, action-plan creation and release logic.
- `controlplane/internal/server/connectivity.go` — node-scoped response routes.
- `controlplane/internal/server/*_test.go` — route, scope, approval and protocol tests.
- `cmd/nodeagent/process_inventory*.go` — cross-platform safe process snapshot collector.
- `cmd/nodeagent/node_response_exec*.go` — process termination and firewall action executor.
- `cmd/nodeagent/heartbeat.go` — agent protocol fields and capability advertisement.
- `ui/src/lib/api.ts` and `ui/src/pages/NodeDetail.tsx` — inventory, action state and request UI.
- `ui/src/pages/NodeDetail.test.tsx` — operations UI behavior.

### Task 1: Add durable process snapshots and response-action state

**Files:**
- Create: `controlplane/internal/migrate/sql/0152_node_processes_and_response_actions.up.sql`
- Create: `controlplane/internal/migrate/sql/0152_node_processes_and_response_actions.down.sql`
- Create: `controlplane/internal/storage/node_processes.go`
- Create: `controlplane/internal/storage/node_response_actions.go`
- Create: `controlplane/internal/storage/node_processes_test.go`
- Create: `controlplane/internal/storage/node_response_actions_test.go`

- [ ] **Step 1: Write storage tests for atomic replacement and action transitions**

Test `ReplaceNodeProcesses` inserts two rows, then replaces them with one row and leaves no stale process. Test `CreateNodeResponseAction`, `MarkNodeResponseActionApproved`, `ClaimApprovedNodeResponseActions`, and `CompleteNodeResponseAction` cannot cross tenant or node scope and can claim an action only once.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./controlplane/internal/storage -run 'Test(NodeProcesses|NodeResponseActions)' -count=1 -timeout=20s`

Expected: FAIL because the tables and storage methods do not exist.

- [ ] **Step 3: Add reversible schema**

Create `node_processes` with `(node_id, pid, started_at)` as its primary key and columns `tenant_id`, `ppid`, `executable`, `command_line`, `run_as`, `state`, `listen_addresses JSONB`, `observed_at`. Create `node_response_actions` with UUID ID, tenant/node IDs, nullable action-plan ID, `action_type`, `request JSONB`, `state`, requester, approved-by, receipt JSONB, timestamps, and `dispatch_attempts`. Add tenant/node/state indexes. Down migration drops `node_response_actions` then `node_processes`.

- [ ] **Step 4: Implement bounded storage contracts**

Define these exact action states: `needs_approval`, `approved`, `dispatched`, `succeeded`, `failed`, `refused`, `expired`, `cancelled`. Implement `ReplaceNodeProcesses` as delete-and-insert in one transaction scoped to the target node. `ClaimApprovedNodeResponseActions` must atomically update at most 20 matching `approved` actions to `dispatched` using `FOR UPDATE SKIP LOCKED` and return only actions for the requesting node.

- [ ] **Step 5: Run storage checks and commit**

Run: `gofmt -w controlplane/internal/storage/node_processes.go controlplane/internal/storage/node_response_actions.go controlplane/internal/storage/node_processes_test.go controlplane/internal/storage/node_response_actions_test.go; go test ./controlplane/internal/storage -run 'Test(NodeProcesses|NodeResponseActions)' -count=1 -timeout=20s`

Expected: PASS or documented no-database skip.

```bash
git add controlplane/internal/migrate/sql/0152_node_processes_and_response_actions.* controlplane/internal/storage/node_processes.go controlplane/internal/storage/node_response_actions.go controlplane/internal/storage/node_processes_test.go controlplane/internal/storage/node_response_actions_test.go
git commit -m "feat: store node process and response action state"
```

### Task 2: Send a bounded process inventory from supported agents

**Files:**
- Create: `cmd/nodeagent/process_inventory.go`
- Create: `cmd/nodeagent/process_inventory_linux.go`
- Create: `cmd/nodeagent/process_inventory_windows.go`
- Create: `cmd/nodeagent/process_inventory_darwin.go`
- Create: `cmd/nodeagent/process_inventory_test.go`
- Modify: `cmd/nodeagent/heartbeat.go`
- Modify: `cmd/nodeagent/heartbeat_test.go`
- Modify: `controlplane/internal/server/heartbeat.go`
- Modify: `controlplane/internal/server/heartbeat_test.go`

- [ ] **Step 1: Write collector and heartbeat contract tests**

Test that process collection caps at 2,000 entries, redacts no fields locally, produces a non-zero PID and RFC3339 start time when available, and that heartbeat serializes `process_inventory` only when `process_inventory.v1` is advertised. Add a server test that 2,001 submitted entries are truncated to 2,000 and are persisted only for the authenticated node.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./cmd/nodeagent ./controlplane/internal/server -run 'Test(ProcessInventory|Heartbeat.*Process)' -count=1 -timeout=30s`

Expected: FAIL because the heartbeat contract does not contain process inventory.

- [ ] **Step 3: Implement the protocol and collector**

Use this shared wire shape in agent and server:

```go
type processInventoryEntry struct {
    PID int `json:"pid"`; PPID int `json:"ppid,omitempty"`
    Executable string `json:"executable,omitempty"`; CommandLine string `json:"command_line,omitempty"`
    RunAs string `json:"run_as,omitempty"`; State string `json:"state,omitempty"`
    StartedAt string `json:"started_at,omitempty"`; ListenAddresses []string `json:"listen_addresses,omitempty"`
}
```

Linux collector must read `/proc` without invoking a shell. Windows and macOS collectors must return an empty inventory plus an `unsupported` capability status until an equivalent native collector exists. Advertise `process_inventory.v1` only on platforms where collection succeeds. Server parsing rejects negative PIDs, overlong strings, invalid timestamps, more than 50 addresses per process, and non-owned node IDs; it logs validation failures and persists valid entries.

- [ ] **Step 4: Run focused agent and server tests**

Run: `gofmt -w cmd/nodeagent/process_inventory*.go cmd/nodeagent/heartbeat.go cmd/nodeagent/heartbeat_test.go controlplane/internal/server/heartbeat.go controlplane/internal/server/heartbeat_test.go; go test ./cmd/nodeagent ./controlplane/internal/server -run 'Test(ProcessInventory|Heartbeat.*Process)' -count=1 -timeout=30s`

Expected: PASS.

- [ ] **Step 5: Commit heartbeat inventory**

```bash
git add cmd/nodeagent/process_inventory*.go cmd/nodeagent/heartbeat.go cmd/nodeagent/heartbeat_test.go controlplane/internal/server/heartbeat.go controlplane/internal/server/heartbeat_test.go
git commit -m "feat: collect node process inventory"
```

### Task 3: Create and approve safe node-response actions

**Files:**
- Create: `controlplane/internal/server/node_response_actions.go`
- Create: `controlplane/internal/server/node_response_actions_test.go`
- Modify: `controlplane/internal/server/connectivity.go`
- Modify: `controlplane/internal/server/action_plans.go`
- Modify: `controlplane/internal/server/action_plans_test.go`

- [ ] **Step 1: Write authorization and approval tests**

Add request tests proving: only operator/admin may request; tenant A cannot target tenant B’s node; `process.terminate` needs a known PID and exact RFC3339 process start time; `firewall.port_block` accepts TCP/UDP, port 1–65535, inbound/outbound, optional valid CIDR, TTL 60–86400 seconds; the requester cannot approve; a distinct operator approval changes exactly one action to `approved`; and denied/cancelled plans can never be claimed.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./controlplane/internal/server -run 'Test(NodeResponse|ActionPlan.*NodeResponse)' -count=1 -timeout=30s`

Expected: FAIL because routes and request validation do not exist.

- [ ] **Step 3: Add API routes, scoped validation, and action-plan creation**

Add `GET /api/v1/nodes/{node_id}/processes`, `GET /api/v1/nodes/{node_id}/response-actions`, and `POST /api/v1/nodes/{node_id}/response-actions`. The POST creates both a `node_response_action` and an `action_plan` with `risk: high`, `state: needs_approval`, `min_approvers: 1`, `separation_of_duties: true`, and allowed roles `operator`/`admin`. Never create a job at request time. Reuse existing action-plan role checks and creator-exclusion guard.

- [ ] **Step 4: Release exactly one action only after independent approval**

In the successful branch of `handleCreateActionPlanApproval`, call `MarkNodeResponseActionApproved` using the plan ID, only after the plan becomes `approved`. If no node response action is associated, it is a no-op. Use a transaction/conditional update so concurrent approvals cannot release the same action twice.

- [ ] **Step 5: Run focused server tests and commit**

Run: `gofmt -w controlplane/internal/server/node_response_actions.go controlplane/internal/server/node_response_actions_test.go controlplane/internal/server/connectivity.go controlplane/internal/server/action_plans.go controlplane/internal/server/action_plans_test.go; go test ./controlplane/internal/server -run 'Test(NodeResponse|ActionPlan.*NodeResponse)' -count=1 -timeout=30s`

Expected: PASS.

```bash
git add controlplane/internal/server/node_response_actions.go controlplane/internal/server/node_response_actions_test.go controlplane/internal/server/connectivity.go controlplane/internal/server/action_plans.go controlplane/internal/server/action_plans_test.go
git commit -m "feat: require approval for node response actions"
```

### Task 4: Dispatch and execute approved actions with target validation

**Files:**
- Create: `cmd/nodeagent/node_response_exec.go`
- Create: `cmd/nodeagent/node_response_exec_linux.go`
- Create: `cmd/nodeagent/node_response_exec_test.go`
- Modify: `cmd/nodeagent/heartbeat.go`
- Modify: `controlplane/internal/server/heartbeat.go`
- Modify: `controlplane/internal/server/heartbeat_test.go`

- [ ] **Step 1: Write execution and receipt tests**

Test a process terminate request refuses when the PID is absent or the recorded start time differs from the current process; test a matching short-lived test process receives SIGTERM and returns a succeeded receipt; test a port-block request validates its fields before firewall invocation; test an agent can receive an action exactly once; test the server maps receipts to only the matching node and action ID.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./cmd/nodeagent ./controlplane/internal/server -run 'Test(NodeResponseExecution|Heartbeat.*NodeResponse)' -count=1 -timeout=30s`

Expected: FAIL because action fields and executor do not exist.

- [ ] **Step 3: Implement heartbeat dispatch and agent executor**

Add `pending_node_response_actions` and `completed_node_response_actions` heartbeat fields. The server claims approved actions for the heartbeat’s node and provides only action ID, type, request, and an expiry timestamp. The agent accepts only `process.terminate` and `firewall.port_block`; unsupported types produce a `refused` receipt. For process termination, compare PID plus process start time immediately before signaling; send SIGTERM first and never escalate to SIGKILL. For port blocking, use the existing platform firewall executor and include its rule ID in the receipt. Receipt errors are bounded to 512 characters.

- [ ] **Step 4: Make server receipt handling idempotent**

Complete an action only from `dispatched`; duplicate receipts must return no error and retain the original terminal receipt. Expire `approved` and `dispatched` records past their expiry before dispatch and expose `expired` state to the API.

- [ ] **Step 5: Run focused tests and commit**

Run: `gofmt -w cmd/nodeagent/node_response_exec*.go cmd/nodeagent/heartbeat.go controlplane/internal/server/heartbeat.go controlplane/internal/server/heartbeat_test.go; go test ./cmd/nodeagent ./controlplane/internal/server -run 'Test(NodeResponseExecution|Heartbeat.*NodeResponse)' -count=1 -timeout=30s`

Expected: PASS.

```bash
git add cmd/nodeagent/node_response_exec*.go cmd/nodeagent/heartbeat.go controlplane/internal/server/heartbeat.go controlplane/internal/server/heartbeat_test.go
git commit -m "feat: execute approved node response actions"
```

### Task 5: Surface agent health, inventory and approval state in Node Detail

**Files:**
- Modify: `ui/src/lib/api.ts`
- Modify: `ui/src/pages/NodeDetail.tsx`
- Modify: `ui/src/pages/NodeDetail.test.tsx`

- [ ] **Step 1: Write focused UI tests**

Mock a node with a current heartbeat, two process rows and one listener. Assert the Operations section shows agent state, PID, executable, listening address, current response action state, and a request button. Assert a process request includes PID and `started_at`; assert a pending response displays `Approval required`; assert no direct kill request is sent before confirmation.

- [ ] **Step 2: Run the focused UI test and verify failure**

Run: `node C:\dev\control_one_main_fix\ui\node_modules\vitest\vitest.mjs run --root C:\dev\control_one\ui src/pages/NodeDetail.test.tsx`

Expected: FAIL because the operations data and action controls are absent.

- [ ] **Step 3: Add Operations UI with explicit action state**

Extend the existing node detail request model with process and response-action endpoints. Render agent status from `last_seen_at`, agent version and capabilities; render processes in a sortable table with PID, executable, command, identity, start time and listeners. Add a confirmation dialog for `End process` and `Block port`, show the exact process identity or port rule, and submit only to the response-action endpoint. Render status values as `Approval required`, `Approved`, `Dispatched`, `Succeeded`, `Failed`, `Refused`, `Expired`, or `Cancelled`. Customer-facing copy must not narrate interface mechanics.

- [ ] **Step 4: Run focused UI tests and commit**

Run: `node C:\dev\control_one_main_fix\ui\node_modules\vitest\vitest.mjs run --root C:\dev\control_one\ui src/pages/NodeDetail.test.tsx`

Expected: PASS without starting a frontend server.

```bash
git add ui/src/lib/api.ts ui/src/pages/NodeDetail.tsx ui/src/pages/NodeDetail.test.tsx
git commit -m "feat: show node operations and approval state"
```

### Task 6: Move direct firewall blocks behind the same approval gate

**Files:**
- Modify: `controlplane/internal/server/firewall_action_plans.go`
- Modify: `controlplane/internal/server/network_security.go`
- Modify: `controlplane/internal/server/firewall_action_plans_test.go`
- Modify: `controlplane/internal/server/network_security_test.go`

- [ ] **Step 1: Write regression tests**

Test that an IP or port firewall request creates a `needs_approval` action plan and a node response action, creates no pending firewall job/rule before another user approves, and releases exactly one agent action after a distinct approver accepts it.

- [ ] **Step 2: Run focused tests and verify failure**

Run: `go test ./controlplane/internal/server -run 'Test(Firewall.*Approval|NetworkSecurity.*Approval)' -count=1 -timeout=30s`

Expected: FAIL because firewall plans enter `queued` and dispatch immediately.

- [ ] **Step 3: Route firewall block requests through node responses**

Replace direct high-risk queued plan creation with the Task 3 `firewall.port_block` response-action factory. Preserve existing read-only firewall state and rollback receipt behavior. Do not change low-risk read paths. Existing IP block callers must supply a target node, protocol, direction, port/CIDR and expiry so the agent has an unambiguous scope.

- [ ] **Step 4: Run focused regression tests and commit**

Run: `gofmt -w controlplane/internal/server/firewall_action_plans.go controlplane/internal/server/network_security.go controlplane/internal/server/firewall_action_plans_test.go controlplane/internal/server/network_security_test.go; go test ./controlplane/internal/server -run 'Test(Firewall.*Approval|NetworkSecurity.*Approval)' -count=1 -timeout=30s`

Expected: PASS.

```bash
git add controlplane/internal/server/firewall_action_plans.go controlplane/internal/server/network_security.go controlplane/internal/server/firewall_action_plans_test.go controlplane/internal/server/network_security_test.go
git commit -m "fix: gate firewall blocks behind approval"
```

### Task 7: Verify the releasable node-response slice

**Files:**
- Verify only.

- [ ] **Step 1: Run focused verification**

Run: `go test ./cmd/nodeagent ./controlplane/internal/storage ./controlplane/internal/server -run 'Test(ProcessInventory|NodeResponse|ActionPlan.*NodeResponse|Firewall.*Approval|Heartbeat.*NodeResponse)' -count=1 -timeout=60s`

Expected: PASS or documented database-test skip.

Run: `node C:\dev\control_one_main_fix\ui\node_modules\vitest\vitest.mjs run --root C:\dev\control_one\ui src/pages/NodeDetail.test.tsx`

Expected: PASS.

- [ ] **Step 2: Inspect release integrity**

Run: `git diff --check; git status --short`

Expected: no whitespace errors; pre-existing untracked user files remain untouched.

