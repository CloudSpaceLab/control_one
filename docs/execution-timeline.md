# Execution Timeline — Closing the PR #51 Gap & Bug List

**Status:** plan
**Date:** 2026-05-08
**Companion (scope):** PR #51 — [`gaps-vs-probo-holmesgpt.md`](./gaps-vs-probo-holmesgpt.md) + [`incomplete-features-and-bugs.md`](./incomplete-features-and-bugs.md)
**Executor:** Claude Opus 4.7 + parallel sub-agents. Reviewer: Chibueze.

---

## 0. Premise — the execution model is AI-agent, not human team

A sane human-team estimate for the PR #51 backlog is ~9 weeks (gap doc) + ~3–4 weeks of fixes (bugs doc) = roughly a quarter. That's not the right yardstick here. The fixer is me + parallel sub-agents, which changes three things:

- **Authoring is parallel.** Independent fixes ship as separate small PRs in the same batch. The bottleneck isn't engineer-hours — it's review throughput, CI run time, and deploy cycles.
- **Reading is fast.** I already have the file:line trace for every issue from the prior research swarm. I don't need to learn the codebase per fix.
- **The slow parts are real-world signals.** "Did calibration actually complete after deploy?" needs ~30 minutes of live data; "did three days of soak surface no regressions?" needs three days. Those are unavoidable and are baked in.

What this gives us:

| Cadence input | Value | Implication |
|---|---|---:|
| Per-PR agent authoring | 30 min – 1 day | Most fixes are sub-day |
| CI run on push (`go test -short` + ui-test, 3 OS matrix) | ~10–15 min | Negligible |
| Auto-deploy on merge to `main` (per `deploy.yml`) | ~30 min | Negligible |
| Review (Chibueze) | assume 3–5 PRs/day cleared | **The real bottleneck** |
| Live-signal validation (calibration completion, soak) | hours–days | Unavoidable, runs in background |

**Realistic calendar target: 3 weeks** to close P0 + P1 + the bank-pillar amplifiers from PR #51, with sub-agent-driven parallel authoring against a serial review queue. That's roughly 3× faster than the human estimate, gated by review throughput, not by engineering capacity.

---

## 1. Scope (everything in PR #51, nothing else)

### From `incomplete-features-and-bugs.md`

P0 security: §4 #1–#4 (AML auth, sanctions HTTPS, DOB fallback, OpenReplay no-op).
P0 visible bugs: §1.1 calibration · §1.2 connections · §1.3 recommendations · §1.5 compliance→node link · §3.1 patch approval gate.
P1 reliability: §5 (agent Fatal calls, process tree stub, dashboard scalability, Trivy CVE detail, vendor UPDATE, untested modules, evidence S3, metadata JSONB, test_hooks shim).
P1 KG: §2 (option B tool-shaped, after MCP).
P2 telemetry rough edges: §6.

### From `gaps-vs-probo-holmesgpt.md`

P0 investigation: §6 — 5-day MCP wrap + `tool_use` loop in `/ai/ask`.
Pillar amplifiers (P1): §5 — Surge detector + Prometheus ingest, CVE/KEV (Attacks), SMART/PSI/HSM collectors (Health).
P1 patterns: streaming, citations, tool RBAC, refusal, operator-mode.
P2 deferrals: ServiceNow/Jira/Teams writeback, Snapshots, Findings overlay, Asset criticality.

### Explicitly out of scope

- UI refactor (deferred per owner).
- Risk register, RoPA/DPIA/TIA, governance docs, e-sign, training programs, SoA — paperwork that doesn't serve the three pillars (gap doc §7).
- Brand-new ML platforms.

---

## 2. Conventions & guardrails

| Rule | Why |
|---|---|
| One fix per PR; target < 500 LOC | Reviewer fatigue is the bottleneck |
| Each PR carries unit tests for the change | We're closing untested-modules debt at the same time |
| Each PR has `## What this changes` / `## Verification` / `## Rollback` in the body | Self-contained for the reviewer |
| Branch: `fix/<area>-<short>` for bugs, `feat/<area>-<short>` for new capabilities, `chore/<short>` for cleanup | Consistent prefix for `gh pr list` filtering |
| Never merge a red CI; never bypass hooks; never `--force-push` to `main` | Deploy auto-fires on merge — broken main = broken prod |
| Wiki update appended at the end of each phase, not per-PR | Avoids Wiki churn during active work |
| If a fix needs > 2 sub-agents to deliver, split into multiple PRs | Keeps each PR atomic and revertable |
| Per-PR sub-agent dispatched with: scope + file paths + acceptance criteria + test plan | Same shape as PR #51's research dispatches, but for code authoring |

---

## 3. Phases

Each phase is a calendar week-shaped batch. Within a phase, PRs are mostly parallel — sub-agents work on independent files. Across phases, there are real dependencies (MCP must precede tool_use; tool_use precedes operator-mode).

### Phase 0 — Kickoff (Day 1, half-day)

Pre-flight before authoring anything.

| Task | Owner | Output |
|---|---|---|
| Verify CI green on `main` | me | confirmed |
| Snapshot current production state at `cloudspacetechs.com` (run the §9 diagnostic SQL from the bugs doc) | me + reviewer | "before" data point |
| Confirm review cadence with reviewer (target: clear 5 PRs/day) | reviewer | Slack/PR thread agreement |
| Create `chore/timeline-tracker.md` issue or pinned PR comment | me | live status board |

### Phase 1 — P0 security + P0 visible bugs (Days 1–3)

**Goal:** every ship-blocker resolved or explicitly downgraded by EOD3.

#### Day 1 — Security batch (4 parallel PRs)

| # | PR | Files | Time | Notes |
|---|---|---|---:|---|
| P1-S1 | `fix/aml-route-auth` | AML handler files (search `178.79.176.19/moov-watchman-aml`) | 30m | Add `s.authorize(roleAdmin)` + tests |
| P1-S2 | `fix/sanctions-https` | sanctions client | 45m | Move URL to env var; force HTTPS; pin cert if available |
| P1-S3 | `fix/sanctions-dob-fallback` | `SanctionsScanner` | 20m | Refuse scan when DOB null; add error path |
| P1-S4 | `fix/openreplay-implement-or-remove` | `uploadToOpenReplay` callsite | 1–2h | Decision: implement (~2h) or remove + feature-flag (~30m). Recommend: **remove** for v1, document, re-add behind real impl in v1.1 |

All four dispatched as parallel sub-agents in a single message. CI runs in parallel. Review queue holds 4 small PRs by EOD1.

#### Day 2 — Visible-bug batch A (4 parallel PRs)

| # | PR | Files | Time |
|---|---|---|---:|
| P1-B1 | `fix/compliance-row-node-link` + introduce shared `<NodeLink>` component | `Compliance.tsx:267-268`, new `ui/src/components/NodeLink.tsx`, sweep alerts/audit log/security_events tables for the same pattern | 1h |
| P1-B2 | `fix/recommendations-bridge` | `knowledge_graph.go:148` (write port_observations from `node_services` ingest) + `recommendations.go:88-95` (stamp evidence.node_id) + `?node_id=` filter | 2h |
| P1-B3 | `fix/connections-filter` | UI external-only toggle + Doris query loosen `(ended_at IS NULL OR last_data_at >= NOW() - INTERVAL 5 MINUTE)` | 1.5h |
| P1-B4 | `fix/patch-approval-gate` (quick path) | `patch.go:341` flag `patch_requires_approval` (default false) + Day-3 follow-up for the proper loop | 1h |

#### Day 3 — Visible-bug batch B + KG minimal enrichment (3 PRs)

| # | PR | Files | Time |
|---|---|---|---:|
| P1-B5 | `fix/calibration-metric-contract` | `internal/util/sysinfo.go:53-85` (add `host.iowait_pct`, `host.swap_used_pct`, `host.oom_events_count`, `host.load_avg_ratio`, `net.icmp_latency_p99`, `net.packet_loss_pct`); `node_predictive.go:493-508` (skip absent signals from min-calibration); `telemetry.go:59-69` (units) | 4h |
| P1-B6 | `feat/knowledge-graph-enrich` | `knowledge_graph.go:235-323` adds firewall, health score, top-5 alerts, top-5 connections, baseline delta. Fix the lying intro paragraph. Fix invalidation hooks. | 3h |
| P1-B7 | `feat/patch-management-completeness` | Node-selector modal + `/api/v1/nodes/{id}/packages` endpoint + node-detail packages tab + heartbeat action-prefix fix + schedule `patch.inventory_scan` daily | 4h (split into 2 PRs if > 500 LOC) |

#### Phase 1 verification gate (end of Day 3 / Day 4 morning)

After all P1 PRs deploy, run on `cloudspacetechs.com`:

```sql
-- 1. Calibration completing? (wait 30 min after deploy)
SELECT score, risk_level, components FROM node_health_scores
WHERE node_id='0d4893c0-867a-4bf1-8aa9-e247680280ab';
-- Expected: risk_level NOT 'calibrating' (or calibrating with samples > 0)

-- 2. Recommendations populating?
SELECT count(*) FROM port_observations
WHERE tenant_id='<tenant>'; -- expect > 0 within ~30 min

-- 3. Connections visible?
-- Open the node detail page; toggle "External only" off; rows should appear

-- 4. Compliance row → node nav?
-- Trigger a known failing rule; click the node hostname; lands on /nodes/{id}
```

If any check fails → diagnose in-thread; fix-forward in Phase 1 (don't proceed). If all pass → Phase 2.

**Phase 1 outcome:** all four user-reported production bugs fixed; all four P0 security gaps closed; KG no longer lies about firewall posture; patch management round-trips end-to-end.

### Phase 2 — Investigation MCP + tool_use (Days 4–8)

The 5-day P0 from the gap doc. This is the biggest deliverable and the foundation everything else builds on.

| Day | PR | Time |
|---|---|---:|
| 4 | `feat/mcp-server-skeleton` — new package `controlplane/internal/mcp/` using `mark3labs/mcp-go`. 10 tools wrapping existing handlers (`c1_search`, `c1_entity_get`, `c1_lifecycle`, `c1_related`, `c1_enrich`, `c1_events_query`, `c1_processes_query`, `c1_connections_query`, `c1_compliance_status`, `c1_node_health`). Tests: schema validation + happy-path per tool. | 1 day |
| 5 | `feat/ai-ask-tool-use-loop` — refactor `anthropicMessage` in `ai_ask.go:256` to `Tools []anthropicTool`, parse `stop_reason` + content blocks. Loop max 12 iter / 60s. **Drop `buildKnowledgeGraph` from `ai_ask.go` path.** Switch official SDK: `github.com/anthropics/anthropic-sdk-go`. | 1 day |
| 6 | `feat/ai-ask-streaming-citations` — SSE streaming via `events_stream.go` pattern; per-tool `result_id` threaded into `[c1_entity_get:node-abc]` markers; UI renders citation chips. | 1 day |
| 7 | `feat/ai-ask-tool-rbac-refusal` — per-tool role gate (writes admin-only); system prompt rule "no tool result → say 'no data'". Adversarial eval added to `ai_ask_test.go`. | 0.5 day |
| 7–8 | `feat/operator-mode` — new worker subscribes to `events_anomaly.go` severity≥high emits; auto-runs `/ai/ask` with templated investigation question; persists to new `investigations` table; fires existing webhook outbox. | 1 day |
| 8 | `chore/delete-knowledge-graph` — remove `knowledge_graph.go` once `/ai/ask` no longer references it. UI swaps to MCP-driven path. | 0.5 day |

#### Phase 2 verification gate (Day 8)

- Ask via curl: `curl -N /api/v1/ai/ask -d '{"question":"what surged on node 0d48… in the last hour?"}'` — expect a streaming response with citations, calling `c1_connections_query` and `c1_baseline_compare`.
- Trigger a synthetic anomaly; confirm operator-mode produces an investigation row with summary + citations within 60s.
- Run the architectural test from the gap doc: delete the React dashboard locally; confirm cURL workflow still gives an operator value.

**Phase 2 outcome:** chat-first investigation works end-to-end. Hand-rolled KG deleted. Operator-mode auto-investigates anomalies.

### Phase 3 — Pillar amplifiers (Days 9–13)

All three pillars get their P1 closure. Most can run in parallel.

#### Surge pillar (Days 9–10)

| PR | Time |
|---|---:|
| `feat/surge-detector` — z-score on rolling 1m/5m/1h windows over `process_connections` + `unique_counters`; emits `events.surge.detected` for the operator-mode loop to pick up. Library: `gonum.org/v1/gonum/stat`. | 1 day |
| `feat/prometheus-alert-source` — receiver endpoint accepting Alertmanager webhooks + Prometheus metric ingest; routes into the same anomaly stream. | 1 day |

#### Attacks pillar (Days 9–11)

| PR | Time |
|---|---:|
| `feat/cve-kev-osv-client` — new package `controlplane/internal/cveintel/`; OSV.dev API client (no auth needed), CISA KEV catalog feed (~24h refresh). | 1 day |
| `feat/node-vulnerabilities-table` — migration + storage layer; cron job joining `node_packages` × OSV → `node_vulnerabilities`. | 1 day |
| `feat/trivy-parse-detail` — extend Trivy adapter to persist individual CVEs/CVSS/fixed-versions instead of aggregate counts. | 0.5 day |
| `feat/vuln-dashboard` — UI tab on node-detail showing CVEs sorted by KEV-presence + EPSS score + fixed-version availability. | 1 day |

#### Health pillar (Days 9–12)

Three collectors in parallel sub-agents:

| PR | Time |
|---|---:|
| `feat/agent-smart-collector` — embed `github.com/anatol/smart.go`; emit `smart.reallocated_sector_count`, `smart.uncorrectable_errors`, `smart.percentage_used` (NVMe). Linux-only initially. | 1 day |
| `feat/agent-psi-collector` — read `/proc/pressure/{cpu,memory,io}` `some/avg10`. | 0.5 day |
| `feat/agent-hsm-pkcs11-prober` — bank moat. `github.com/miekg/pkcs11`; periodic `C_GenerateRandom(32)`; emit `hsm.latency_ms`, `hsm.error_rate`. **Optional/feature-flagged** since most dev environments don't have an HSM. | 1 day |

#### Phase 3 verification gate (Day 13)

- Surge: synthetic 10× connection burst → operator-mode investigation appears within 60s with z-score evidence.
- Attacks: install a known-CVE package on a test node → CVE row appears with KEV/EPSS within an hour; operator-mode flags it.
- Health: a node with a real SSD shows non-zero SMART metrics; calibration uses them.

### Phase 4 — Reliability + UX polish (Days 14–17)

| PR | Files | Time |
|---|---|---:|
| `chore/agent-replace-fatal-calls` | `cmd/nodeagent/` 15+ `panic`/`Fatal` callsites → structured-error returns + exit-code | 0.5 day |
| `fix/process-tree-handler-hydration` | from stub to real `process_lineage` traversal | 0.5 day |
| `feat/auto-doc-endpoint` | `GET /api/v1/nodes/{id}/documentation(.md)` aggregating node + firewall + health + services + packages + alerts + connections + surge + compliance | 1 day |
| `feat/findings-overlay` | thin status/owner/sla overlay on `security_events` + `compliance_results` (Probo `findings` shape) | 1 day |
| `chore/dead-handler-cleanup` | delete `handleLegacyTelemetry`, `handleLegacyHeartbeat`; loosen `DisallowUnknownFields` on envelope; bump `MaxBytesReader` to 256 KiB | 0.5 day |
| `fix/scanner-temp-paths` | switch scanner adapters to per-run `os.MkdirTemp` | 0.5 day |
| `feat/vendor-update-endpoint` | add `PATCH /vendors/{id}` + UI form | 0.5 day |
| `chore/remove-test-hooks-shim` | delete `cluster_rollouts_test_hooks.go` from `main` (per `task-control-one-completion.md`) | 30m |

### Phase 5 — Tests, soak, Wiki (Days 18–21)

| Day | Task | Owner |
|---|---|---|
| 18–19 | Test coverage for `ai_ask`, `compliance_evidence`, `anomaly_baselines`, scanner adapters, `dlp_scan`, `dashboard_metrics` (target: each module > 50% line coverage). Parallel sub-agents per module. | me |
| 18 | `feat/evidence-s3-backend` — replace local-temp file storage with S3-compatible blob (Minio for dev, AWS S3 for prod). Use `compliance_evidence.metadata` JSONB for collector args. | me |
| 19–21 | **Production soak**: 48–72h on `cloudspacetechs.com`. Re-run the §9 diagnostic SQL hourly. Operator-mode metrics. CVE/KEV refresh cadence. | me + reviewer |
| 21 | Wiki updates: append `Wiki/wiki/entities/control-one-production.md` (topology, broken-area history, diagnostic recipes); update `Wiki/wiki/entities/control-one.md` to reflect chat-first reframe; close out `task-control-one-completion.md` with v1.0 readiness; append session log. | me |

---

## 4. Per-PR template (what every fix-agent dispatch includes)

When I dispatch a sub-agent to author a fix, the prompt has this shape:

```
## Scope
<one-line summary>

## Files
<file1:line range>
<file2:line range>

## Acceptance criteria
- [ ] criterion 1
- [ ] criterion 2 (test added)
- [ ] criterion 3 (CI green)

## Test plan
<unit/integration test path + what it asserts>

## Rollback
<git revert; deploy.yml rolls forward; data: ...>

## Out of scope
<things the agent must NOT touch>
```

The sub-agent writes the code, runs tests locally where possible, opens the PR with the same headings filled in, and reports the PR URL back. I then either approve and request review, or send a follow-up with corrections.

---

## 5. Risk register + mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Sub-agent introduces a regression that breaks `main` (and therefore production via auto-deploy) | Medium | CI on PR catches most; if a regression slips, `git revert` + redeploy is the path. Never bypass CI. |
| Review queue backs up; PRs stale | Medium | Cap WIP at 5 PRs/day; pause new dispatches until queue drains. |
| Calibration fix doesn't actually resolve "Calibrating (0/24 samples)" because of an unknown additional factor | Low–Medium | Phase 1 verification gate runs the SQL diagnostic before proceeding. |
| MCP server has a security flaw exposing tools without RBAC | Medium | Phase 2 explicitly ships RBAC + refusal as part of the same batch; per-tool tests required. |
| Operator-mode triggers an investigation storm during a real incident, masking root cause | Medium | Cap operator-mode invocations: rate-limit per-anomaly + per-tenant; store dedup key. |
| HSM prober crashes when no HSM is configured | High | Feature-flag default-off; detect missing PKCS#11 module gracefully. |
| OSV.dev rate-limits us | Low | Cache responses for 24h; back off; KEV catalog is a static JSON anyway. |
| Production soak surfaces a Phase-2 regression late | Medium | Soak runs in parallel with Phase 4; if a regression appears, rollback to pre-Phase-2 commit. |
| User changes priorities mid-flight | High | Plan is week-shaped, not day-locked. Each phase is independently shippable. |
| I (Claude) hit a context-limit failure mid-batch | Low | Each PR is < 500 LOC + sub-agents run in fresh contexts; resumable per-PR. |

---

## 6. Rollback protocol

For any fix that lands on `main` and breaks something:

1. **Revert the PR** (`gh pr revert <num>` or `git revert <sha> && git push`). Auto-deploy fans out within 30 min.
2. **Confirm rollback** via the production diagnostic SQL.
3. **Open a follow-up PR** with the actual fix; do not silently retry.
4. **Append a session log** to the Wiki under `wiki/tasks/sessions/` so the rollback is captured.

Special cases:

- **Schema migration revert.** Migrations are append-only; never drop columns/tables that prod has populated. Rollback is "ignore the column" not "drop the column."
- **Agent rollout regression.** Agent binaries don't auto-update on prod nodes (per the wizard installer); rollback is "don't push the new agent." If a bad agent went out, the new agent has to ship a self-disable flag the controlplane can flip.

---

## 7. Wiki update protocol

End of each phase:

- **Phase 0:** create `Wiki/wiki/tasks/task-pr51-execution.md` — kanban-shaped, links each PR.
- **Phase 1:** session log capturing the four production-bug confirmations.
- **Phase 2:** update `Wiki/wiki/synthesis/control-one-deep-gap-analysis.md` to mark Gap 2 (MCP + tool_use) closed; supersede with link to MCP design doc.
- **Phase 3:** update entity page's "Key Components" with CVE/KEV layer + surge detector + new collectors.
- **Phase 4:** session log on reliability batch.
- **Phase 5:** append `Wiki/wiki/entities/control-one-production.md` (the gap PR #51 flagged); update `wiki/log.md`; close `task-pr51-execution.md` with `completed:` date.

---

## 8. Calendar at a glance

```
Week 1   Phase 0–1   Days 1–3    P0 security + visible bugs              ~10 PRs
Week 2   Phase 2     Days 4–8    Investigation MCP + tool_use loop       ~6 PRs
Week 3   Phase 3–5   Days 9–21   Pillars + reliability + soak + Wiki     ~18 PRs

Total: ~34 PRs, ~21 calendar days, gated by review throughput.
```

If review pace is **slower** than 5 PRs/day average, the calendar slides linearly. If review is **fast**, Phases 4–5 can compress to ~5 days and the whole thing finishes in ~17 days.

---

## 9. Definitions of done

| Level | Test |
|---|---|
| **Per PR** | CI green; unit test covers the change; verification block in PR body confirmed by reviewer. |
| **Per phase** | Phase verification gate (the SQL/curl checks above) all pass on `cloudspacetechs.com`. |
| **Overall (v1.0 readiness)** | All four user-reported production bugs fixed (calibration completing, connections visible, recommendations populating, compliance→node nav clickable). All four P0 security gaps closed. Investigation chat answers "what's surging on node X" with citations end-to-end. CVE/KEV column populates on the node-detail vuln tab. Three days of production soak with no Sev-1 regressions. Wiki updated. |

---

## 10. Open questions for the reviewer

1. **OpenReplay — implement or remove?** Implementing properly costs ~1 day + an OpenReplay project to upload to. Removing for v1 is ~30 min and we re-add behind a real impl in v1.1. Recommend: **remove** for v1.
2. **HSM prober — feature-flag default off, or strip for v1?** Banks will love it in the demo, but it crashes in dev environments without a PKCS#11 module. Recommend: **feature-flag default off**; document that enabling requires a configured HSM.
3. **Patch approval gate — quick flag (4h) or proper loop (2–3d)?** Quick flag unblocks the page; proper loop is the right design but pushes Phase 1 by a day. Recommend: **quick flag for v1, proper loop in Phase 4** as a backfill.
4. **Evidence S3 backend — required for v1, or v1.1?** Local-temp works in dev/single-replica prod; falls over with multi-replica. Currently controlplane is single-replica per the deploy script. Recommend: **defer to v1.1** unless multi-replica is on the table.
5. **Production access for soak validation.** Do I have read-only DB access on `cloudspacetechs.com` to run the diagnostic SQL myself, or does the reviewer run it? Affects how tight the verification gates are.

---

## 11. Kickoff checklist (before authoring PR #1)

- [ ] Reviewer confirms the open questions in §10.
- [ ] Reviewer confirms target review pace (default: clear 5 PRs by EOD each working day).
- [ ] I run the production diagnostic SQL (or have the reviewer run it) to lock in the "before" baseline.
- [ ] I create `Wiki/wiki/tasks/task-pr51-execution.md` with this timeline mirrored as a kanban.
- [ ] First batch (Phase 1, Day 1, security PRs) dispatched as four parallel sub-agents.

When all five are checked, Day 1 starts.
