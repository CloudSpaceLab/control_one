# Execution Timeline — Loop-Driven Batches for PR #51 Backlog

**Status:** plan (v2 — restructured for `/loop` workflow)
**Date:** 2026-05-08
**Companion (scope):** PR #51 — [`gaps-vs-probo-holmesgpt.md`](./gaps-vs-probo-holmesgpt.md) + [`incomplete-features-and-bugs.md`](./incomplete-features-and-bugs.md)
**Executor:** Claude Opus 4.7 + parallel sub-agents inside `/loop`. Reviewer: Chibueze.

---

## 0. The execution model

The reviewer drives the cadence with `/loop` in Claude Code. Each loop iteration:

1. I check the **batch tracker** (live status board, see §6).
2. I pick the next eligible batch (`NotStarted` + all dependencies `Merged`).
3. Dispatch sub-agents to author the PR for that batch.
4. Push + open PR; update tracker to `InReview`.
5. Report back.

The reviewer reviews when convenient. On merge, the next `/loop` tick picks up the next eligible batch.

This shape replaces the "1 fix = 1 PR" approach (cumbersome, ~34 PRs) with **cohesive batches** (~17 PRs). A batch is *one PR*, *one logical unit*, *one revert if needed*. Within a batch, multiple sub-agents may author different files in parallel; the assembled PR ships as a unit.

### Batching principles

- **One area per batch** — UI fixes for one page, telemetry pipeline for one signal, etc.
- **Cap ~1500 LOC** per batch. Cohesive scope makes a 1500-LOC PR easier than five disconnected 300-LOC PRs.
- **Most batches are independent** — different files, no merge conflicts, can ship in any order.
- **Sequential constraints are explicit** — only where the next batch literally cannot exist without the previous (e.g., `tool_use` loop needs the MCP server first).
- **Each batch carries its own tests + verification block + rollback note.** Same template as before, just one per batch.

---

## 1. Scope

Same as PR #51:

- All P0 security, P0 visible bugs, P1 reliability, P1 KG, P2 telemetry rough edges from the bugs doc.
- Investigation MCP + `tool_use` loop, surge detector, CVE/KEV, SMART/PSI/HSM collectors, operator-mode, findings overlay, auto-doc endpoint from the gap doc.
- **Out of scope:** UI refactor, risk register, RoPA/DPIA/TIA, governance docs, e-sign, training programs.

---

## 2. The 17 batches

Listed in **dependency order** (read top-down). The `Dep` column lists batches that must be merged first. Anything with no dep is parallel-eligible from day one.

### Phase 1 — P0 fixes (5 batches, all parallel)

| # | Batch | Files / Scope | Dep | Size |
|---|---|---|---|---:|
| **A** | `fix/p0-security` | AML route auth + sanctions HTTPS + sanctions DOB fallback removal + OpenReplay no-op (recommend: remove for v1, document re-add for v1.1) | — | M |
| **B** | `fix/single-node-bugs` | Shared `<NodeLink>` component + apply on Compliance + Alerts + Audit log + Security events tables · `node_services` → `port_observations` bridge · UI "External only" toggle + Doris `ended_at IS NULL` filter loosen | — | L |
| **C** | `fix/calibration-metric-contract` | `internal/util/sysinfo.go` adds `host.iowait_pct`, `host.swap_used_pct`, `host.oom_events_count`, `host.load_avg_ratio`, `net.icmp_latency_p99`, `net.packet_loss_pct` collectors · `node_predictive.go:493-508` skips absent signals from `min()` calibration · `telemetry.go:59-69` adds units · per-signal sample counts in `components` JSONB | — | L |
| **D** | `fix/patch-management-completeness` | Approval-gate quick flag (`patch_requires_approval`, default false) · node-selector modal · `/api/v1/nodes/{id}/packages` endpoint + node-detail packages tab · heartbeat action-prefix lookup · daily `patch.inventory_scan` cron | — | L |
| **E** | `feat/knowledge-graph-enrich` | `knowledge_graph.go:235-323` appends per-node firewall, health score, top-5 alerts, top-5 connections, baseline delta. Fix lying intro paragraph. Fix invalidation hooks. Markdown shape preserved. | — | M |

**All five are file-disjoint and ship in any order.** Verification gate after Phase 1 (§5).

### Phase 2 — Investigation MCP + chat-first (3 batches, mostly sequential)

| # | Batch | Files / Scope | Dep | Size |
|---|---|---|---|---:|
| **F** | `feat/mcp-server` | New `controlplane/internal/mcp/` package using `github.com/mark3labs/mcp-go`. 10 tools wrapping existing investigate handlers (`c1_search`, `c1_entity_get`, `c1_lifecycle`, `c1_related`, `c1_enrich`, `c1_events_query`, `c1_processes_query`, `c1_connections_query`, `c1_compliance_status`, `c1_node_health`). Schema validation + happy-path tests per tool. **Does not touch `ai_ask.go`.** | — | L |
| **G** | `feat/ai-ask-tool-use` | Refactor `ai_ask.go:256` to use `Tools []anthropicTool`; iterate up to 12 turns / 60s; switch to official `github.com/anthropics/anthropic-sdk-go`; add SSE streaming via existing `events_stream.go` pattern; thread `result_id` into `[c1_<tool>:<id>]` citation chips; tool RBAC (writes admin-only); refusal on zero-grounding. **Drops `buildKnowledgeGraph` from the `/ai/ask` path.** UI swaps to streaming render. | F | XL |
| **H** | `feat/operator-mode` | New worker subscribes to `events_anomaly.go` severity≥high; auto-runs `/ai/ask` with templated investigation prompt; persists to new `investigations` table (migration); fires existing webhook outbox. Includes `chore/delete-knowledge-graph` once `/ai/ask` no longer references it. Rate limit per-tenant + dedup key per-anomaly. | G | L |

### Phase 3 — Pillar amplifiers (3 batches, all parallel after Phase 2)

| # | Batch | Files / Scope | Dep | Size |
|---|---|---|---|---:|
| **I** | `feat/surge-pillar` | New `detectTrafficSurge` (z-score on rolling 1m/5m/1h windows over `process_connections` + `unique_counters`) emitting `events.surge.detected`; Prometheus Alertmanager webhook receiver routing into the same anomaly stream. Library: `gonum.org/v1/gonum/stat`. | H (operator-mode picks up the new event type) | L |
| **J** | `feat/attacks-pillar-cve-kev` | New `controlplane/internal/cveintel/` package · OSV.dev API client (no auth) · CISA KEV catalog feed (24h refresh) · `node_vulnerabilities` table migration · cron joining `node_packages` × OSV → `node_vulnerabilities` · Trivy adapter parse-detail (CVE ID + CVSS + fixed version) · node-detail vuln tab UI sorted by KEV-presence + EPSS. | — | XL |
| **K** | `feat/health-pillar-collectors` | Three agent-side collectors. SMART via `github.com/anatol/smart.go` (Linux); PSI from `/proc/pressure/{cpu,memory,io}`; HSM PKCS#11 prober via `github.com/miekg/pkcs11` (feature-flagged default off). Server-side: surface them on the node detail page health card. | C (calibration-metric contract must accept the new names) | L |

### Phase 4 — Reliability + UX polish (3 batches, parallel)

| # | Batch | Files / Scope | Dep | Size |
|---|---|---|---|---:|
| **L** | `chore/agent-reliability` | Replace 15+ `panic()`/`log.Fatal` calls in `cmd/nodeagent/` with structured-error returns + exit codes · process-tree handler hydration (real `process_lineage` traversal, no longer single-node stub) · scanner adapters use `os.MkdirTemp` per run | — | M |
| **M** | `feat/ux-polish` | `GET /api/v1/nodes/{id}/documentation(.md)` aggregating node + firewall + health + services + packages + alerts + connections + surge + compliance · Findings overlay (status/owner/sla on `security_events` + `compliance_results`) · Vendor `PATCH /vendors/{id}` endpoint + UI form · Patch management approval-gate **proper loop** (replaces the Phase 1 quick flag) | E (auto-doc joins KG-enriched data) | L |
| **N** | `chore/cleanup` | Delete `handleLegacyTelemetry` + `handleLegacyHeartbeat` (unregistered dead code) · loosen `DisallowUnknownFields` on telemetry envelope · bump `MaxBytesReader` 64 KiB → 256 KiB · remove `cluster_rollouts_test_hooks.go` shim · fix `largestPenalty` alpha-sort tie-break in `node_predictive.go:635-650` | — | S |

### Phase 5 — Tests, soak, Wiki (2 batches + soak window)

| # | Batch | Files / Scope | Dep | Size |
|---|---|---|---|---:|
| **O** | `chore/test-coverage` | Unit + integration tests for `ai_ask`, `compliance_evidence`, `anomaly_baselines`, scanner adapters, `dlp_scan`, `dashboard_metrics`. Target each module > 50% line coverage. Parallel sub-agents author tests per module. | G, H, J, K (test against the new code) | L |
| **P** | `feat/evidence-s3` *(optional v1)* | Replace local-temp file storage with S3-compatible blob (Minio in dev, AWS S3 in prod) · use `compliance_evidence.metadata` JSONB for collector args · presigned-URL download. Recommend defer to v1.1 unless multi-replica is on the roadmap. | — | M |
| **soak** | (no PR) Production soak | 48–72h on `cloudspacetechs.com`. Re-run §5 verification SQL hourly. Watch operator-mode metrics + CVE/KEV refresh cadence. | All prior | — |
| **wiki** | (small commits) Wiki update | Append `Wiki/wiki/entities/control-one-production.md` (topology + diagnostic recipes + broken-area history) · update `Wiki/wiki/entities/control-one.md` to reflect chat-first reframe · close out `task-control-one-completion.md` with v1.0 readiness · session log. | All prior | — |

### Dependency graph (visual)

```
A   B   C   D   E
                 \
F ──► G ──► H    M
              ├─ I
              ├─ J
              └─ K (also depends on C)

L (parallel anytime)
N (parallel anytime)
O (after G, H, J, K)
P (parallel anytime, optional)

soak → wiki
```

**Top of queue at kickoff** (no dependencies): **A, B, C, D, E, F, J, L, N**. Nine batches eligible from day one.

---

## 3. Batch sizing & review pace

| Size | Approx LOC | Review time | Notes |
|---|---|---|---|
| S | < 300 | 10–20 min | Single-area cleanup |
| M | 300–800 | 20–45 min | Cohesive feature batch |
| L | 800–1500 | 45–90 min | Multi-file but one logical unit |
| XL | 1500–2500 | 1.5–3 h | Reserved for the chat-first refactor (G); split if grows beyond |

If reviewer clears 2–3 batches/day, the calendar is **~10–14 working days**. If 1 batch/day, ~17 days. Slower than that, the calendar slides linearly.

---

## 4. Loop pickup protocol

When `/loop` fires (self-paced or interval), I run this:

1. **Read the tracker** at `Wiki/wiki/tasks/task-pr51-execution.md` (created in Phase 0). Status columns: `NotStarted` / `InProgress` / `InReview` / `Merged` / `Blocked`.
2. **Identify eligible batches** — `NotStarted` AND every `Dep` is `Merged`.
3. **Pick one** (default: leftmost in queue order). If the user stated a preference in the loop prompt, honor it.
4. **Dispatch sub-agents** for that batch. Sub-agents work in parallel where files don't conflict.
5. **Push + open PR** with body following the template in §7.
6. **Update tracker**: batch → `InReview`. Note PR URL.
7. **Report** in chat: which batch, PR URL, what's in it, what to look at first in review.
8. **Optionally schedule next loop** if working autonomously and there are still eligible batches.

If no eligible batches exist (all are blocked on review), I **do not author** — I report the queue state and exit. Author churn while review is the bottleneck wastes reviewer time.

### Loop prompt patterns the reviewer might use

```
/loop                                  # self-paced, I pick next eligible
/loop /implement-next                  # same, explicit
/loop /implement A                     # force batch A next (override)
/loop 30m                              # every 30 min, autonomously continue
/loop /soak                            # during Phase 5, run the diagnostic SQL on a schedule
```

---

## 5. Phase verification gates

After each phase, before unlocking the next, run on `cloudspacetechs.com`:

### Phase 1 gate

```sql
-- (1.1) Calibration completing? (≥ 30 min after Batch C deploy)
SELECT score, risk_level, components FROM node_health_scores
WHERE node_id='0d4893c0-867a-4bf1-8aa9-e247680280ab';

-- (1.3) Recommendations populating? (≥ 30 min after Batch B deploy)
SELECT count(*) FROM port_observations WHERE tenant_id='<tenant>';

-- (1.2) Connections visible?
-- Open node detail; toggle "External only" off; rows appear.

-- (1.5) Compliance row → node nav?
-- Trigger a known failing rule; click the hostname; lands on /nodes/{id}.

-- (P0 sec) AML routes:
-- curl without auth → 401; curl with operator role → 403; curl with admin → 200.
```

### Phase 2 gate

```bash
curl -N /api/v1/ai/ask -d '{"question":"what surged on node 0d48… in the last hour?"}'
# expect streaming SSE with citation chips referencing c1_connections_query / c1_baseline_compare
```

Trigger a synthetic anomaly; confirm operator-mode produces a row in `investigations` within 60s.

### Phase 3 gate

- Synthetic 10× connection burst → `events.surge.detected` → operator-mode investigation appears.
- Install a known-CVE package on a test node → `node_vulnerabilities` row with KEV/EPSS within 1h.
- Test node with real SSD → SMART metrics non-zero in `node_health_scores.components`.

### Phase 5 gate (overall v1.0 readiness)

All four user-reported production bugs fixed. All four P0 security gaps closed. Chat-first investigation answers "what's surging on node X" with citations end-to-end. CVE/KEV column populating. **48–72h soak with no Sev-1 regression.** Wiki updated.

---

## 6. Live tracker shape (`Wiki/wiki/tasks/task-pr51-execution.md`)

```markdown
---
title: "PR #51 Execution Tracker"
type: task
status: in-progress
priority: critical
project: "[[Control One]]"
created: 2026-05-08
---

## Batches

| Batch | Title | Status | Dep | PR | Merged | Notes |
|---|---|---|---|---|---|---|
| A | fix/p0-security | NotStarted | — | — | — | |
| B | fix/single-node-bugs | NotStarted | — | — | — | |
| C | fix/calibration-metric-contract | NotStarted | — | — | — | |
| D | fix/patch-management-completeness | NotStarted | — | — | — | |
| E | feat/knowledge-graph-enrich | NotStarted | — | — | — | |
| F | feat/mcp-server | NotStarted | — | — | — | |
| G | feat/ai-ask-tool-use | NotStarted | F | — | — | |
| H | feat/operator-mode | NotStarted | G | — | — | |
| I | feat/surge-pillar | NotStarted | H | — | — | |
| J | feat/attacks-pillar-cve-kev | NotStarted | — | — | — | |
| K | feat/health-pillar-collectors | NotStarted | C | — | — | |
| L | chore/agent-reliability | NotStarted | — | — | — | |
| M | feat/ux-polish | NotStarted | E | — | — | |
| N | chore/cleanup | NotStarted | — | — | — | |
| O | chore/test-coverage | NotStarted | G,H,J,K | — | — | |
| P | feat/evidence-s3 | Optional | — | — | — | defer to v1.1? |

## Verification gates

- [ ] Phase 1 gate
- [ ] Phase 2 gate
- [ ] Phase 3 gate
- [ ] Phase 5 gate (v1.0 readiness)
```

I update this every loop tick. The reviewer can read it directly to know "what's open / what's mine to review next."

---

## 7. Per-batch PR template

Every PR opens with:

```markdown
## What this batch ships
<bullet list of file-level changes>

## Acceptance criteria
- [ ] criterion 1 (with verification step)
- [ ] criterion 2 (test added at <path>)
- [ ] CI green
- [ ] Phase verification (if this batch closes a gate item)

## Verification
<exact commands or SQL the reviewer runs to confirm>

## Rollback
<git revert <sha> + auto-redeploy + any data implications>

## Out of scope (deferred to <future batch>)
<bullet list of things not in this batch>
```

---

## 8. Risk register

| Risk | Mitigation |
|---|---|
| Sub-agent introduces a regression in a multi-file batch | Each batch has unit tests + a phase verification gate; revert is per-PR (one batch = one revert). |
| Review queue stalls; agents idle | `/loop` self-detects empty eligibility and exits silently rather than authoring stale work. |
| A batch grows past XL during authoring | Sub-agent reports back; I split mid-flight into two related PRs (e.g., G could split into "tool_use loop" + "streaming + citations"). |
| Calibration fix doesn't resolve the symptom | Phase 1 gate runs the diagnostic SQL before unlocking Phase 2. If gate fails, fix-forward in Phase 1; don't proceed. |
| MCP RBAC has a hole | Batch G ships RBAC + refusal as part of the same batch; per-tool tests required at merge. |
| Operator-mode investigation storm during a real incident | Rate limit per-tenant + dedup-key per-anomaly built into Batch H. |
| HSM prober crashes in dev | Feature-flag default off in Batch K; document the enable path. |
| OSV.dev rate limits | 24h cache + KEV catalog is static JSON; Batch J handles backoff. |
| Production soak surfaces a regression late | Soak runs in parallel with Phase 4; rollback to pre-Phase-2 commit if Sev-1. |
| Loop fires when nothing is eligible | Loop tick reports queue state and exits without authoring; no spurious PRs. |

---

## 9. Rollback protocol

For any merged batch that breaks something:

1. `gh pr revert <num>` (or `git revert <sha> && git push`); auto-redeploy fans out within ~30 min.
2. Re-run the relevant phase verification SQL to confirm rollback.
3. Open a follow-up batch with the actual fix; do not silently retry.
4. Append a session log under `Wiki/wiki/tasks/sessions/`.

Schema migrations are append-only — never drop a column/table prod has populated. Rollback for a schema regression is "ignore the column" not "drop the column."

---

## 10. Definitions of done

| Level | Test |
|---|---|
| **Per batch** | CI green; tests cover the change; verification block in PR body confirmed by reviewer. |
| **Per phase** | Phase verification gate (§5) passes on `cloudspacetechs.com`. |
| **v1.0 ready** | All four user-reported production bugs fixed (calibration completing, connections visible, recommendations populating, compliance→node nav clickable). All four P0 security gaps closed. Chat-first investigation answers "what's surging on node X" with citations end-to-end. CVE/KEV populating. 48–72h soak with no Sev-1. Wiki updated. |

---

## 11. Calendar at a glance

```
Day 1–4    Phase 1 (5 parallel batches: A B C D E)
Day 4–8    Phase 2 (sequential: F → G → H)
Day 7–10   Phase 3 (parallel after H: I J K — J starts earlier since no Phase-2 dep)
Day 9–12   Phase 4 (parallel: L M N — M after E)
Day 11–14  Phase 5 (O after G/H/J/K; soak runs through to Day 17)
Day 17     v1.0 readiness gate

Total: ~14–17 calendar days at 2–3 batches/day review pace.
```

If review pace is **1 batch/day average**: ~22 days.
If review pace is **3+ batches/day average**: ~12 days.

---

## 12. Open questions (block kickoff)

1. **OpenReplay** — implement (~1 day + project) or remove for v1 (~30 min in Batch A)? **Recommend: remove**.
2. **HSM prober (Batch K)** — feature-flag default off, or strip for v1? **Recommend: feature-flag default off**.
3. **Patch approval gate** — quick flag in Batch D, proper loop in Batch M (already split this way in v2). Confirm OK.
4. **Evidence S3 (Batch P)** — v1 or v1.1? **Recommend: v1.1** unless multi-replica deploy is on the table.
5. **Production DB access for verification SQL** — can I run it myself, or does the reviewer? Affects gate latency.

---

## 13. Kickoff checklist (before first `/loop` tick)

- [ ] Reviewer answers the five open questions in §12.
- [ ] I create `Wiki/wiki/tasks/task-pr51-execution.md` mirroring §6.
- [ ] Reviewer (or I, if granted access) captures the production "before" baseline via the §5 SQL.
- [ ] Reviewer agrees on review pace target (default: 2–3 batches/day cleared).
- [ ] First `/loop` tick fires. I pick from {A, B, C, D, E, F, J, L, N} based on reviewer preference or default leftmost.

When all five are checked, the loop starts.
