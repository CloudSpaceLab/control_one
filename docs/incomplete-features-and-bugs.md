# Control One — Incomplete Features & Bugs Audit

**Status:** discovery / triage list
**Date:** 2026-05-08
**Companion doc:** [`gaps-vs-probo-holmesgpt.md`](./gaps-vs-probo-holmesgpt.md)
**Method:** parallel Opus 4.7 deep-research agents tracing each issue from UI → API → DB. All findings verified against actual code with file:line citations.

> Goal: a single source of truth for everything in Control One that **doesn't work today**, so the team knows what to fix before any bank pilot demo. Issues are sorted by user-visible impact and severity, not by area.

---

## TL;DR — what's broken right now

| Area | Symptom | Root cause | Severity |
|---|---|---|---|
| Single-node view: predictive health | Permanent **"Calibrating (0/24 samples)"** | Agent emits 9 metric names; predictive engine reads 7 **disjoint** names. Calibration counter is `min()` across all signals, so any missing signal pins it at 0 | **P0** |
| Single-node view: open connections | Tab shows empty even on active node | Server filter `ended_at IS NULL` excludes summary aggregates; UI client-side filter strips RFC1918 peers | **P0** |
| Single-node view: recommendations | Tab shows empty for every node | `port_observations` table has **zero writers anywhere in the codebase**; the generator's input is permanently empty | **P0** |
| Patch management | Every deploy fails with "approval required" by default | `runPatchSafetyGates` hard-codes severity=high; default `MinApprovalSeverity` is also high; no approval-then-dispatch loop | **P0** |
| Knowledge graph | Comment claims firewall posture; LLM can't answer "what's surging" | KG is a hand-rolled markdown blob over 2 tables; firewall/baselines/Doris/alerts/health all unused | **P1** |
| AML gateway | PII (BVN, NIN, DOB, address) readable without auth | No `s.authorize()` on AML routes | **P0 security** |
| Sanctions client | Hardcoded `http://178.79.176.19/moov-watchman-aml` (plain HTTP) | Sanctions data unencrypted in transit | **P0 security** |
| Sanctions DOB fallback | Hardcoded `birthDate=1962-11-23` | Wrong-person matches when DOB null | **P0 correctness** |
| Session recording | OpenReplay upload silently logs "placeholder" and returns nil | `uploadToOpenReplay()` is a no-op stub | **P0 silent compliance failure** |
| Agent reliability | 15+ `panic()` / `log.Fatal` calls; any config issue crashes | Unbounded fatal exits in `cmd/nodeagent/` | **P1** |
| Process tree handler | Returns single node | Stub implementation | **P2** |
| Test coverage | 164 of 296 Go modules have no tests | Including `ai_ask`, `compliance_evidence`, `dlp_scan`, `anomaly_baselines` | **P2** |

The first four rows are all reachable from the production deployment at `https://control-one.cloudspacetechs.com/console/nodes/0d4893c0-867a-4bf1-8aa9-e247680280ab` and represent **the entire visible state** of the single-node view + patch page right now.

---

## 1. The Single-Node View is broken in three independent places

Three separate failures, all at the **agent ↔ controlplane contract boundary**, not a single ingestion outage.

### 1.1 "Calibrating (0/24 samples)" sticks forever

**Trace:**
- UI: `ui/src/pages/NodeDetail.tsx:116` — `Calibrating (${calibratingSamples ?? 0}/24 samples)`, sourced from `health.components.calibrating_samples`.
- Calibration counter: `controlplane/internal/server/node_predictive.go:494-539` — `scorePredictForTenant` queries `ListTelemetryMetrics` per signal, takes `min(len(samples))` across **all** signals, gates on `< 24`. When any metric has 0 rows, `minSamples = 0` → permanent `risk_level='calibrating'`.
- Signals queried (`node_predictive.go:63-111`): `host.iowait_pct`, `host.swap_used_pct`, `host.load_avg_ratio`, `host.oom_events_count`, `smart.reallocated_sector_count`, `smart.uncorrectable_errors`, `net.packet_loss_pct`, `net.icmp_latency_p99`.
- Agent emitter (`internal/util/sysinfo.go:53-85` `CollectHostMetrics`): emits `cpu_usage_percent`, `cpu_count`, `memory_total_bytes`, `memory_used_percent`, `disk_usage_percent`, `disk_total_bytes`, `load1`, `load5`, `load15`. **Nine names.**

**Root cause (confirmed):** the agent never emits any of the eight metric names the engine reads. There is no SMART collector, no OOM collector, no `iowait`, no ICMP/packet-loss anywhere in `internal/`. Repo-wide grep for `iowait|swap_used|oom_events|reallocated_sector|uncorrectable_errors|packet_loss|icmp_latency|psi_|pressure` returns zero hits outside one comment in `procmon/collector.go:10`.

**Fix sketch (two-front, both required):**
1. **Agent-side** (`internal/util/sysinfo.go:53`): add Linux PSI-derived `host.iowait_pct` (read `/proc/pressure/io` or compute from `cpu.Times.Iowait/Total`); `host.swap_used_pct` from `mem.SwapMemory()`; `host.oom_events_count` from `/proc/vmstat` `oom_kill` delta; `host.load_avg_ratio` = `Load1/cpuCount`; `net.packet_loss_pct` and `net.icmp_latency_p99` from a periodic ICMP probe to the controlplane gateway; SMART (`smart.*`) via `smartctl -A -j` per discovered block device. Linux-only is fine — fail-safe to omitting on macOS/Windows so the predictive gate still resolves once iowait/swap/oom/load/icmp are present.
2. **Server-side** (`node_predictive.go:493-508`): change calibration semantics to skip *missing* signals from the `min()` calculation. Otherwise rolling out one signal at a time keeps every node calibrating until the slowest-deployed signal catches up. Treat absent signals as "no penalty, no calibration vote." Expose per-signal sample counts in `components` so operators see which signal is the laggard.
3. Update `controlplane/internal/server/telemetry.go:59-69` `metricUnits` to add units for the new names (`pct` / `count` / `ms`).

**Faster compromise (1 day):** rename `healthSignalsCatalog()` to query the keys the agent already sends (`cpu_usage_percent`, `memory_used_percent`, `load1`-derived) and downgrade SMART/OOM/ICMP signals to "optional." Calibration completes ~24 minutes after onboarding at the default 60 s telemetry interval.

**Diagnostic recipe (operator on live deployment):**
```sql
-- 1. Confirm zero rows for predictive signals
SELECT metric_name, COUNT(*) FROM telemetry_metrics
WHERE node_id='<uuid>' AND timestamp > now() - interval '2 hours'
GROUP BY metric_name ORDER BY 1;
-- Expected: rows for cpu_usage_percent/memory_used_percent/load1; ZERO rows for smart.*/host.iowait_pct/etc.

-- 2. Confirm calibrating gate hit
SELECT score, risk_level, components, computed_at
FROM node_health_scores WHERE node_id='<uuid>';
-- Expected: risk_level='calibrating', components->>'calibrating_samples'='0'.

-- 3. Confirm agent ticks succeed
journalctl -u controlone-agent | grep telemetry.metrics.sent
-- Expected: components: 9 (or fewer on macOS).
```

### 1.2 Open connections tab empty

**Trace:**
- UI: `ui/src/pages/NodeDetail.tsx:484-640` — `ConnectionsTab` calls `api.listConnections({tenantId, nodeId, openOnly: true, since: -24h, limit: 250})`.
- Endpoint: `controlplane/internal/server/connections_query.go:23-67` — requires `s.dorisClient` (returns 503 if nil); calls `dorisClient.ListConnectionsForNode`.
- Doris query: `controlplane/internal/doris/reader_events.go:78-118` — `WHERE … AND ended_at IS NULL ORDER BY threat_match DESC, started_at DESC`.
- Agent emitter: `internal/netflow/collector.go:165-237` with smart filter at `internal/netflow/filter.go:25-54`. Default `CaptureExternal=true, CaptureInternalSummary=true`. **Critically: summary events take the `FilterSummary` branch and return without publishing** (`collector.go:173-176`); only `FilterEmit` publishes `conn.open` / `conn.close`.

**Root cause (multi-factor):**
1. **Server-side filter `ended_at IS NULL` excludes summary aggregates.** `conn.summary` rows have `ended_at = bucket_close`, not NULL. For a fresh node, all short-lived flows roll up into summaries → none satisfy the filter.
2. **UI client-side filter strips RFC1918 peers** (`NodeDetail.tsx:520-523` rejects 10/8, 172.16/12, 192.168/16, 169.254/16, loopback) AND requires `bytes>0 || threat_match || direction==='inbound'`. On dev/internal nodes where most traffic is RFC1918, every row gets stripped client-side — API may return rows but UI shows the empty-state copy.

**Fix sketch:**
- `ui/src/pages/NodeDetail.tsx:520-523`: drop the `externalPeerIP` filter behind a "External only" toggle (default off). Operators *do* want to see internal flows on a single-node view.
- `controlplane/internal/doris/reader_events.go:88`: when `openOnly=true`, change to `(ended_at IS NULL OR last_data_at >= NOW() - INTERVAL 5 MINUTE)` so recent summary buckets count as "open enough."
- Add a fallback to `event_rollups_hourly` / netflow data so the view degrades gracefully when Doris is empty.

### 1.3 Recommendations tab empty

**Trace:**
- UI: `ui/src/pages/NodeDetail.tsx:884-908` — `RecommendationsTab` calls `api.listRecommendations(tenantId)` (tenant-scoped only — no `node_id`), filters client-side by `evidence.node_id === nodeId || !evidence.node_id`.
- Endpoint: `controlplane/internal/server/recommendations.go:33-108` — calls `s.store.AggregatePortObservations(ctx, tenantID, since=NOW()-30d)`. Requires ≥ 50 samples per `(port, protocol)` AND ≥ 95% dominant-state ratio.
- Source table: `port_observations` (migration `0046_port_observations.up.sql`).
- Writer: `controlplane/internal/storage/correlation.go:229-242` `Store.CreatePortObservation` — **defined but has zero non-test callers anywhere in the codebase.** Verified: agent never POSTs port observations; service collector posts to `node_services` only (`cmd/nodeagent/services.go:339`, `controlplane/internal/server/knowledge_graph.go:94-159`).

**Root cause (confirmed):** the recommendations generator's input table is **never written**. Permanently empty → `AggregatePortObservations` returns `[]` → endpoint returns `{"data": []}` for every tenant. Even rows the engine *could* produce never carry `evidence.node_id` (`recommendations.go:88-95` only stores `samples / state_counts / window_days`), so the UI's per-node filter would still hide them.

**Fix sketch:**
1. **Bridge `node_services` → `port_observations`** in `handleNodeServicesIngest` (`knowledge_graph.go:148`): for each service in the payload call `store.CreatePortObservation({TenantID, NodeID, Port, Protocol, State: probeStatus(svc)})`. This generates one observation per service-collector tick (5–15 min default) and feeds the existing aggregator.
2. **Stamp `evidence.node_id`** in `recommendations.go:88-95`.
3. **Add a `?node_id=` filter** to `handleRecommendations` — push per-node scoping into SQL.

### 1.4 100-word verdict

Three independent failures, all rooted in **incomplete agent ↔ controlplane wiring**, not a single ingestion outage. Predictive scoring queries names the agent has never emitted (pure naming/coverage gap). Connections rows likely *do* exist in Doris but are doubly filtered (server-side `ended_at IS NULL` excludes summaries, UI strips RFC1918 peers). Recommendations input table `port_observations` has no writers; the generator is a dead loop. Fix order: 1.3 (smallest, highest-value), then 1.1 (rename metrics), then 1.2 (Doris filter + UI toggle).

---

## 2. Knowledge graph & auto-documentation are ~15% delivered

`controlplane/internal/server/knowledge_graph.go:235-323` builds a per-tenant Markdown blob from **two** data sources only: `nodes` and `node_services`. Then `ai_ask.go:228-250` stuffs the entire blob into the system prompt with `cache_control: ephemeral` and ships the user's question. Single-shot, no `tool_use`, no streaming.

**The intro paragraph at `knowledge_graph.go:263-267` lies.** It promises "OS, agent version, listening services and their detected URLs, and **firewall posture**." There is no firewall code path — `NodeFirewallState` exists in storage (`storage/node_firewall_state.go:34`) but `buildKnowledgeGraphCtx` never calls it. Same for `NodePackages`, `NodeHealthScore`, `Alerts`, `AnomalyBaselines`, all Doris event tables. The UI page description (`KnowledgeGraph.tsx:154`, `Ask.tsx:66`) repeats the false promise.

**Auto-documentation parity:**

| Field a bank operator expects | In KG? | Storage source available? |
|---|---|---|
| OS / arch / agent | ✅ | `nodes` |
| Listening services / ports | ✅ | `node_services` |
| Firewall posture | ❌ (claimed but absent) | `node_firewall_state` |
| Hardware (CPU/RAM/disk) | ❌ | partial — `nodes` |
| Recent changes | ❌ | `dashboard_events`, audit log |
| Compliance state | ❌ | `compliance_aggregation`, `framework_control_mappings` |
| Recent alerts | ❌ | `storage/alerts.go` |
| Recent connections | ❌ | Doris `process_connections` |
| Vulnerabilities | ❌ | `node_packages` + CVE feed (CVE feed not landed) |
| Health score | ❌ | `node_health_scores` |
| Patch status | ❌ | `patch.go` |
| Ownership / runbooks | ❌ | not modeled |

**Cache invalidation is incorrect.** Invalidated only on `POST /nodes/{id}/services` upload (`knowledge_graph.go:156`). Not on node enroll/decommission/hostname change, firewall upsert, public-IP rotation, agent-version bump, OS reinstall — all flow through `nodes` updates with no cache touch. Process-local cache also means in a multi-replica controlplane each pod has its own; one upload invalidates one replica only.

**Closure plan (3 options, ordered by effort):**

- **A — Minimal enrichment (~2 days):** extend `buildKnowledgeGraphCtx` to fetch and append per-node firewall, health score, top-5 alerts, top-5 connections from Doris, anomaly baseline delta. Keep Markdown shape, just enrich. Fix invalidation hooks. **Highest ROI.**
- **B — Tool-shaped (~1 week):** replace the inlined KG with a `tool_use` turn over thin wrapper tools (`get_node_summary`, `top_connections(node, window)`, `recent_alerts(node, since, severity)`, `surge_signal(node)`, `compliance_state(node)`). This is the chat-first P0 from the gap doc and supersedes A.
- **C — Real KG store (~3-6 weeks):** Apache AGE on existing Postgres or Dgraph sidecar. Don't build until B is in production and you can point at queries B can't answer.

**Auto-documentation as a separate surface.** Split it. Propose:
```
GET /api/v1/nodes/{id}/documentation        → application/json
GET /api/v1/nodes/{id}/documentation.md     → text/markdown
```
JSON shape composes `node + firewall + health + services + packages + recent_alerts + recent_changes + top_connections + surge + compliance + ownership`. Single `s.store.GetNodeDocumentation()` aggregator fanning out existing storage methods + one `doris.Reader` call. Cache 60 s per node. Then the per-tenant KG becomes a `for each node, render that template, concat`.

This makes the KG a **derived view** of node-level documentation — adding a field shows up in both places. Fixes the lying-comment problem and closes the auto-documentation gap simultaneously.

---

## 3. Patch management — works on paper, blocked by default

Audit traced agent collection → ingest → deploy model → execution → CVE linkage → UI. The full pipeline:

| Stage | Status | Notes |
|---|---|---|
| Agent package collection (apt/rpm/winget) | ✅ Working | `cmd/nodeagent/inventory.go:30`, `heartbeat.go:240-291`. macOS returns `(nil, nil)` — no brew/macports collector. |
| Ingest to `node_packages` | ✅ Working | `controlplane/internal/server/heartbeat.go:440-510`, full replace via `ReplaceNodePackages`. |
| Deployment schema | ✅ Working | `0083_patch_deployments.up.sql`, `0087_patch_proxy_squid.up.sql`. Fields coherent. |
| Direct-mode deploy flow | ⚠️ **Default-blocked** (see below) | API → `node_patch_state` row → heartbeat PendingAction → agent runs apt/yum/winget → reports back → rollup. Works *if* config is tuned. |
| Proxy / airgapped modes | ❌ Wired but unreachable from UI | Heartbeat hard-codes `patch.deploy_direct` (`heartbeat.go:259`); proxy/airgapped action prefixes are dead |
| `patch.inventory_scan` | ❌ Dead code | Job type registered, agent path exists, **no scheduler ever creates the job** |
| CVE / KEV linkage | ❌ Confirmed absent | Zero refs to `cve\|cvss\|kev\|nvd\|osv` in `controlplane/`. Pure bulk-upgrade model. |
| UI page | ⚠️ Functional but shallow | See breakdown below |

### 3.1 The blocker — every deploy fails by default

`runPatchSafetyGates` at `controlplane/internal/server/patch.go:341` **hard-codes severity = "high"**. `DefaultTenantRemediationConfig` at `tenant_remediation_config.go:47` sets `MinApprovalSeverity = "high"`. With no operator config the gate fires `EventRemediationApprovalRequested` and returns `false` → response shows every node `gate_blocked: "approval required"` and **nothing dispatches**. There is **no approval-then-redispatch loop**, so the deploy is dead unless tenant config is hand-edited.

**Fix options:**
- **Quick (4–6 h):** lower the synthesised severity to "medium" or add a tenant flag `patch_requires_approval=false` (default).
- **Proper (2–3 d):** wire an actual approval-then-dispatch loop — approver receives the event, signs off, the gate re-evaluates and dispatches.

### 3.2 UI gaps

`ui/src/pages/PatchManagement.tsx` (933 lines): three-tab shell (Deployments / Proxies / Windows). KPI tiles render. Deployments table works. But:
- **No node-selector** — the only deploy button is fleet-wide; cannot pick a subset even though the API supports `node_ids`.
- **No installed-package view** — `node_packages` has data; no API or UI surfaces it. A bank pilot will ask "show me what's installed on host X" and there's no answer.
- **No upgradable-packages preview** (because `patch.inventory_scan` is unscheduled).
- **No CVE / vendor-advisory column anywhere.**
- **NodeConfigEditor cannot pick `proxy_id` or `window_id`** — both fields exist on `patchConfigUpsertRequest` (`patch.go:704-709`) but the UI never sends them. Proxy mode is effectively unselectable.

### 3.3 Top-5 fixes for a bank pilot demo

1. **Fix the approval-gate default** (`patch.go:341`) — quick flag or proper loop. **4–6 h or 2–3 d.**
2. **Add a node-selector to the Deploy form** — replace `confirm()` with a proper modal sending `node_ids`. **4–6 h.**
3. **Ship a "Packages installed on this node" tab** at `/api/v1/nodes/{id}/packages` against `node_packages` + node-detail UI tab. Data is already there. **6–8 h.**
4. **Schedule `patch.inventory_scan` daily** and surface available-upgrades count per node + a fleet KPI tile. **1 d** (cron + agent code already exists; store count column on `node_inventory_sync`).
5. **Fix the heartbeat action prefix** to look up the job's actual type rather than hard-coding `JobTypePatchDeployDirect` (`heartbeat.go:259`); add the matching cases to the completion switch. Otherwise proxy/airgapped silently misroutes. **2–3 h.**

### 3.4 v1 verdict

Don't hide the page entirely — but **rename to "OS Updates"** (banks recoil at "patch management" because they expect CVE-driven workflows; what this product does is fleet `apt upgrade`). **Hide the Proxies and Windows tabs** until proxy_id/window_id pickers exist on the deploy form — they let you create plumbing nothing uses. After the five fixes above (~3–4 dev days), the page has a believable narrative: see installed packages → see upgradable count → push upgrade to selected hosts → watch results.

---

## 4. P0 security gaps (Wiki-flagged, code-confirmed)

From `Wiki/wiki/synthesis/control-one-deep-gap-analysis.md:218-230` — re-confirmed:

| # | Issue | Evidence | Fix |
|---|---|---|---|
| 1 | **AML Gateway has NO auth on API routes** — anyone can trigger AML scans, read PII (`full_name`, BVN, NIN, DOB, address) | synthesis line 220 | Add `s.authorize(roleAdmin)` to AML routes |
| 2 | **Hardcoded Moov Watchman URL with plain HTTP** (`http://178.79.176.19/moov-watchman-aml`) — sanctions data unencrypted in transit | synthesis line 221 | Move to env var, force HTTPS, pin cert |
| 3 | **SanctionsScanner hardcodes fallback `birthDate=1962-11-23`** when DOB is null | synthesis line 222 | Refuse the scan instead of silently returning a wrong-person match |
| 4 | **OpenReplay session recording is a no-op** — `uploadToOpenReplay()` logs "placeholder" and returns nil | synthesis line 223 | Either implement properly or remove the feature flag and document |

**Severity rationale:** all four are **silent compliance failures** in a regulated-industry product. Gap 4 is especially nasty — operators believe sessions are recorded for forensics but nothing is captured.

---

## 5. P1 reliability & code-quality gaps

| # | Issue | Evidence | Severity |
|---|---|---|---|
| 5 | Agent uses `panic()` and `log.Fatal` extensively (15+ Fatal calls); any config issue crashes | synthesis line 230 | Replace with structured-error returns + exit code |
| 6 | Process tree handler is a stub (returns single node) | synthesis line 229; `process_lineage` table exists, handler is stub | P2 |
| 7 | Scanner adapters write to predictable/fixed temp paths → race conditions on concurrent scans | synthesis line 227 | P2 |
| 8 | Dashboard metrics computed on-the-fly with scalability TODOs | synthesis line 228 | P2 — fine for pilot, breaks at scale |
| 9 | **164 of 296 Go modules have no tests** — including `ai_ask`, `compliance_evidence`, `dlp_scan`, `anomaly_baselines`, scanner adapters | synthesis line 226 | P2 |
| 10 | Trivy adapter discards all CVE detail — only aggregate counts surface | synthesis line 113 | P1 — see attacks pillar in gap doc |
| 11 | Vendor table has no UPDATE endpoint (Create / List / Delete only) | synthesis line 187 | P2 |
| 12 | Evidence file storage is local temp dir (not S3 / blob) | synthesis line 174 | P2 |
| 13 | `compliance_evidence.metadata` JSONB column never read or written by Go | synthesis line 175 | P2 |
| 14 | `cluster_rollouts_test_hooks.go` shim still on `main` post-Sprint-2 merge | `Wiki/wiki/tasks/sessions/2026-04-20-control-one-sprint-2-safety-coordination.md:77` | P3 cleanup |

---

## 6. Telemetry pipeline — additional issues found

While tracing the calibration bug (§1.1), the pipeline audit surfaced these:

| # | Issue | File:Line | Severity |
|---|---|---|---|
| 15 | Dead handler `handleLegacyTelemetry` — never registered, swallows the body, doc-comment says "live handler" | `controlplane/internal/server/telemetry.go:480-490` | P3 cleanup; misleading |
| 16 | Dead handler `handleLegacyHeartbeat` — same situation | `telemetry.go:465` | P3 |
| 17 | Strict JSON decoding on ingest endpoint (`DisallowUnknownFields`); once the agent adds new envelope fields, older controlplanes silently drop the entire batch with HTTP 400 | `telemetry.go:104` | P2 — version-bump landmine |
| 18 | `MaxBytesReader = 64 KiB` on telemetry ingest — fine today; SMART per-disk + per-NIC will blow this on hosts with many disks/NICs | `telemetry.go:99` | P3 — bump to ~256 KiB or stream |
| 19 | Hourly rollups duplicated: Postgres `IncrementHourlyRollup` and Doris `events_per_hour_mv` — two sources of truth, no reconciliation | `events_ingest.go:389-394` + Doris MV | P2 — divergence-bomb |
| 20 | `largestPenalty` tie-break is alpha-sort by key — `icmp_latency_spike` always loses to `iowait_sustained` on ties | `node_predictive.go:635-650` | P3 — wrong-cause root-cause attribution on equal penalties |
| 21 | 2 h lookback + 256 sample limit on predictive engine — fine at 60 s intervals; if `MetricsInterval` drops below ~28 s, oldest samples silently truncate | `node_predictive.go:495-500` | P3 |

---

## 7. Wiki gap acknowledgement

The Wiki has **zero coverage of the live deployment** at `control-one.cloudspacetechs.com`. Greps for `cloudspacetechs`, `0d4893c0`, `Calibrating`, `samples`, `console/nodes` across `/Users/astra/Engineering/Wiki/` return no hits. No production runbook, no incident notes, no live-environment caveats.

**Suggestion:** create `Wiki/wiki/entities/control-one-production.md` with:
- Live URL + tenant
- Deployment topology
- Known-broken UI areas (this document)
- Diagnostic recipes (the SQL in §1.1)
- Recent fix history

This PR's bugs doc should also be referenced from `Wiki/wiki/tasks/backlog.md` so the carryforward is tracked.

---

## 8. Closure sequencing

**P0 — block any pilot demo (~10–14 working days):**

1. AML auth + sanctions HTTPS + DOB fallback + OpenReplay (§4 #1–#4)
2. Single-node view three fixes (§1) — recommendations (smallest), then calibration metric-name contract, then connections filter
3. Patch-management approval-gate fix (§3.1) + node-selector + packages-on-node tab + heartbeat action-prefix fix
4. Knowledge-graph minimal enrichment (§2 option A)

**P1 — required before bank pilot signoff (~3–4 weeks):**

5. Knowledge-graph tool-shaped upgrade (§2 option B) — supersedes A; ties into gap doc P0
6. Trivy adapter parse-detail + CVE/KEV from gap doc Attacks pillar
7. Agent reliability (§5 #5) — replace `Fatal` calls
8. Process-tree handler hydration (§5 #6)
9. Test coverage on critical untested modules (§5 #9) — focus `ai_ask`, `compliance_evidence`, `anomaly_baselines`

**P2 — hardening:**

10. Dashboard metrics scalability (§5 #8)
11. Dead handler cleanup + ingest-version-bump tolerance (§6 #15–#17)
12. Vendor UPDATE + evidence S3 backend + metadata JSONB usage (§5 #11–#13)

**P3 — cleanup:**

13. Telemetry pipeline rough edges (§6 #18, #20, #21)
14. Process-tree alpha-sort tie-break (§6 #20)
15. `cluster_rollouts_test_hooks.go` shim removal

---

## 9. Production diagnostic addendum

For each user-reported symptom on `https://control-one.cloudspacetechs.com/console/nodes/0d4893c0-867a-4bf1-8aa9-e247680280ab`, the recipe is:

| Symptom | First check | Confirms |
|---|---|---|
| "Calibrating (0/24 samples)" | `SELECT metric_name, COUNT(*) FROM telemetry_metrics WHERE node_id='0d4893c0-867a-4bf1-8aa9-e247680280ab' AND timestamp > now() - interval '2 hours' GROUP BY metric_name;` | If only `cpu_usage_percent`/`memory_used_percent`/`load1` appear → §1.1 confirmed |
| Empty connections | `SELECT count(*), bool_or(ended_at IS NULL) FROM process_connections WHERE node_id='0d4893c0-867a-4bf1-8aa9-e247680280ab' AND started_at > now() - interval '24 hours';` | If count > 0 but `bool_or = false` → §1.2 confirmed |
| Empty recommendations | `SELECT count(*) FROM port_observations WHERE tenant_id='<tenant>';` | If 0 → §1.3 confirmed |

Run all three; share the results in the PR thread.

---

## Appendix — file pointer index

| Issue | Path |
|---|---|
| §1.1 calibration | `node_predictive.go:63-110, 494-539` + `internal/util/sysinfo.go:53-85` |
| §1.2 connections | `connections_query.go:23-67`, `reader_events.go:78-118`, `NodeDetail.tsx:484-640` |
| §1.3 recommendations | `recommendations.go:33-108`, `correlation.go:229-242` (zero callers), `knowledge_graph.go:94-159` |
| §2 KG | `knowledge_graph.go:178-323, 368-404`, `ai_ask.go:181-264`, `Ask.tsx:66`, `KnowledgeGraph.tsx:154` |
| §3 patch | `patch.go:124-543, 341, 704-709`, `heartbeat.go:240-422`, `inventory.go`, `PatchManagement.tsx` |
| §4 AML | search `178.79.176.19/moov-watchman-aml`; `uploadToOpenReplay()` |
| §6 telemetry | `telemetry.go:59-161, 480-490`, `events_ingest.go:389-394`, `node_predictive.go:495-500, 635-650` |
