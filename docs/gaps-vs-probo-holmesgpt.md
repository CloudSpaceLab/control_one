# Control One vs Probo + HolmesGPT — Gap Analysis (3-Pillar Lens)

**Status:** proposal
**Date:** 2026-05-08
**Wiki context:** [`Wiki/wiki/entities/control-one.md`](../../Wiki/wiki/entities/control-one.md), [`Wiki/wiki/synthesis/control-one-deep-gap-analysis.md`](../../Wiki/wiki/synthesis/control-one-deep-gap-analysis.md), [`Wiki/wiki/tasks/task-control-one-completion.md`](../../Wiki/wiki/tasks/task-control-one-completion.md)
**Companion doc:** [`incomplete-features-and-bugs.md`](./incomplete-features-and-bugs.md)
**Method:** 8 parallel Opus 4.7 deep-research agents over actual code, anchored by Wiki review.

---

## 0. The lens

> "In a bank, daily ops are mostly boring. Only three things ever change — **traffic surge, incoming attacks, server health depreciation**. Build a system that monitors those three pillars intelligently, and when investigation is needed, give complete detail to any depth via a chat-first interface. UI refactor is separate."
> — owner, verbatim

Translation:

| Pillar | What changes | What "good" looks like |
|---|---|---|
| 🚦 Surge | Traffic delta beyond baseline | One headline metric per surface, 60-min spark, drill to top-N talkers |
| 🛡️ Attacks | Threat-feed hits, exploit attempts, anomalous behavior | Detection breadth + CVE-context + auto-block + auto-investigate |
| 💚 Health | Disk wear, OOM precursors, link drift, HSM latency | Calibration-free predictive scoring with concrete next-action |
| 🔬 Investigation | "Tell me everything you know about X" | LLM-driven `tool_use` loop with citations, audit, RBAC |
| 🏛️ GRC paperwork | Once-a-year auditor artifacts | Out of scope for this lens |

Every gap below carries a pillar tag. The three real questions are:
1. What's the smallest change to deliver each pillar credibly?
2. What's the single P0 unlock for chat-first investigation?
3. What in Probo / Holmes is **paperwork that doesn't serve the pillars** and should be skipped?

---

## 1. Identity check (Wiki-anchored)

Per `Wiki/wiki/entities/control-one.md:12`:

> "Unified infrastructure control plane for hybrid cloud environments (VMware, Azure, AWS, LibVirt) designed for regulated industries (banks, telcos). A single dashboard where you can control access to clusters of servers, provision nodes, assign permissions, create sandboxes, and enforce compliance — all from one place."

The chat-first reframe is documented in `Wiki/wiki/synthesis/control-one-deep-gap-analysis.md:12` and **is one day old**. It is not yet reflected in the entity page's "Next planned work" or in `backlog.md`. This PR's gap analysis assumes the synthesis-page reframe wins — but the contradiction is open and the Wiki should be updated to settle it.

Sprints 0–3 are closed (`v0.1.0-mvp`, `v0.2.0-foundations` tags pushed, `seigha` integration branch retired 2026-05-07). Work is targeting `main`. The deferred / known-incomplete list in `task-control-one-completion.md:174-181` plus the synthesis-page "Hidden Gaps NOT in PR #49" (lines 218–230) is the canonical bug carryforward — folded into the companion doc.

---

## 2. Verified state of the investigation surface

Code anchors verified row-by-row by parallel research:

| Claim | File:Line |
|---|---|
| Single-shot LLM, **no `tool_use` loop** | `controlplane/internal/server/ai_ask.go:256` (one `anthropicMessage` call, MaxTokens 1024) |
| 5–7 inline anomaly detectors | `events_anomaly.go:22, 56, 99, 138, 212, 260, 300` |
| Investigation REST surface | `investigate.go:79, 738` (`/search`, `/entities/{type}/{id}/{lifecycle\|related\|enrich}`) |
| Generic webhook only, **zero vendor adapters** | `compliance_remediation.go:454-518` (raw HTTP POST, no PagerDuty/Jira/ServiceNow shape) |
| **Zero CVE/KEV/NVD/OSV references** | confirmed grep across `controlplane/` |
| **Zero risk register / SoA / RoPA / DPIA / TIA** | confirmed grep |

The implication: most of the Probo/Holmes "gap" is a presentation/protocol problem, not a data problem. The 80% of investigation already shipped is reachable from the LLM in **5 days** of MCP wrapping.

---

## 3. Probo MCP categories — Chibueze-tagged

Probo (https://github.com/getprobo/probo) ships 131 MCP tools across 20 categories. Tagged through the pillar lens:

| Probo category | Pillar | Effort | Priority | Notes |
|---|---|---|---|---|
| Organizations / Users | 🚫 | S | Skip | Multi-tenant + RBAC already shipped |
| **Vendors (lifecycle + UPDATE)** | 🛡️ + 🏛️ | M | **P2** | Vendor *health* (cert expiry, breach feeds) helps Attacks; UPDATE endpoint is missing today (Create+Delete only) |
| Risks (register + scoring) | 🏛️ | M | **P3** | Pure paperwork unless wired to live signals — defer |
| Measures / Controls | 🏛️ | M | Skip-mostly | `framework_control_mappings` exists; don't replicate Probo depth |
| Frameworks | 🏛️ | XS | **Done** | SOC2/ISO/HIPAA/PCI/GDPR seeded |
| **Assets (criticality + owner)** | 💚 | S | **P1** | Cheapest amplifier for the Health pillar — node table exists, just needs `criticality + owner` overlay |
| Audits | 🏛️ | M | Skip | `audit_reports` PDF exists; don't expand |
| Tasks | 🏛️ | S | Skip | Use Jira adapter (Holmes-side P2) instead |
| Documents (governance + e-sign) | 🏛️ | L | **Skip** | Banks have Word/SharePoint/DocuSign. Don't compete. |
| Meetings | 🚫 | — | Skip | Calendar exists |
| **Snapshots** | 🔬 | S | **P2** | Point-in-time entity snapshots — directly amplifies "what changed since last week" |
| Statement of Applicability | 🏛️ | M | Skip | Auditor-only artifact |
| **Findings** | 🛡️ | S | **P1** | Probo's `findings` shape = our `security_events` + `threat_observations`. Adopt the `(status, owner, sla)` verbs as a thin overlay |
| Obligations / Data Class. / RoPA / DPIA / TIA | 🏛️ | L each | **Skip** | Genuine paperwork. Build only when a paying customer asks. |

**Verdict:** ~70% of Probo is paperwork that doesn't touch the three pillars. Cherry-pick **Findings, Snapshots, Asset criticality**; ignore the rest. Compliance evidence object stays in our hands (it's already richer than Probo's wire shape). Automated evidence collectors are P2 — useful, not pillar-load-bearing.

---

## 4. HolmesGPT toolsets / patterns — Chibueze-tagged

HolmesGPT (https://github.com/HolmesGPT/holmesgpt) is the inverse of Probo: pure investigation. Most of its DNA maps directly to the 🔬 pillar.

| Holmes pattern | Pillar | Effort | Priority | Notes |
|---|---|---|---|---|
| **Iterative `tool_use` loop** | 🔬 | M (5d) | **P0** | The single biggest unlock — see §6 below |
| **Citations** (per-tool source refs) | 🔬 | S | **P0** | Tool outputs already have IDs — just thread them through |
| Streaming (SSE) | 🔬 | S | P1 | Anthropic SDK supports it; UI just needs delta render |
| Tool RBAC (per-role tool allowlist) | 🔬 | S | **P1** | We have `roleOperator`/`roleAdmin` already — add tool-level gate |
| Refusal on missing data | 🔬 | XS | **P1** | Prompt-level; current system half-does this |
| **Operator-mode** (auto-investigate alerts) | 🛡️ + 🚦 + 💚 | L | **P1** | Fires on anomaly emit → runs investigation loop → posts summary. Touches all three pillars. |
| Ticket writeback (Jira / ServiceNow) | 🏛️-adjacent | M | P2 | Useful, not pillar-critical |
| ChatOps (Slack / Teams slash-cmd) | 🔬 | M | P2 | Distribution; not foundation |
| **Alert sources** (Prometheus / Alertmanager / Grafana) | 💚 + 🚦 | M | **P1** | Direct ingest of health + surge signals; today we rely solely on agent telemetry |
| Toolset framework (declarative YAML catalog) | 🔬 | M | **P1** | The shape Holmes uses to scale to 50+ tools — steal it |
| Runbooks (declarative investigation playbooks) | 🛡️🚦💚 | M | P2 | Bridges anomaly → loop |

**Verdict:** 90%+ of Holmes maps directly to 🔬 + Operator-mode. This is the right north-star repo.

---

## 5. The 3-pillar gap matrix — single biggest closure each

| Pillar | State today | Single biggest closure | Source | Why |
|---|---|---|---|---|
| 🚦 **Surge** | `telemetry_metrics_1m` + `unique_counters` collected; **no surge-specific detector** | **Surge detector** (z-score on rolling 1m/5m/1h windows) + **Prometheus/Alertmanager ingest** | Holmes alert sources + `gonum.org/v1/gonum/stat` | Surge is statistical; we already store the data, we just don't compute deltas |
| 🛡️ **Attacks** | 7 inline detectors + 7 TI feeds + auto-block; **zero CVE / KEV / EPSS** | **CVE/KEV enrichment** of installed packages, KEV-and-EPSS-prioritized | [`google/osv-scanner`](https://github.com/google/osv-scanner) + [CISA KEV catalog](https://www.cisa.gov/known-exploited-vulnerabilities-catalog) | Detection without vuln context is half-blind. KEV+EPSS makes "boring" vs "patch now" obvious. |
| 💚 **Health** | Heartbeat + disk + node_repair; **agent emits 9 names, predictive engine reads 7 disjoint names** (see bugs doc §1) | **Fix metric-name contract + add SMART/PSI/HSM collectors** + simple regression on disk/CPU/memory trend | [`anatol/smart.go`](https://github.com/anatol/smart.go), [`prometheus/node_exporter`](https://github.com/prometheus/node_exporter) `pressure` collector, [`miekg/pkcs11`](https://github.com/miekg/pkcs11) (HSM bank moat), `gonum/stat` | "Server depreciation" = trend, not threshold. We have the score table, the calibration loop is permanently stuck because the agent never emits what the engine reads. |
| 🔬 **Investigation** | 10+ REST endpoints, single-shot LLM, hand-rolled markdown KG | **MCP wrapper over `investigate.go` + `tool_use` loop in `/ai/ask`** | [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go), [`anthropics/anthropic-sdk-go`](https://github.com/anthropics/anthropic-sdk-go) | See §6 — this is P0 |

---

## 6. Investigation as P0 — concrete 5-day plan

The bridge from "we have data" to "ask anything to any depth" is **not** a UI; it's converting `investigate.go`'s 10 REST endpoints into MCP tools and replacing the single-shot call in `ai_ask.go:256` with a `tool_use` loop. **Library swaps (no new infra):**

**Day 1 — MCP server skeleton.** New package `controlplane/internal/mcp/` using `mark3labs/mcp-go` (stdio + SSE transports). Ten tools, each a thin adapter over an existing handler:
`c1_search`, `c1_entity_get`, `c1_lifecycle`, `c1_related`, `c1_enrich`, `c1_events_query`, `c1_processes_query`, `c1_connections_query`, `c1_compliance_status`, `c1_node_health`. Zero new business logic — just JSON schema + adapter.

**Day 2 — `tool_use` loop in `ai_ask.go`.** Refactor `anthropicMessage` to accept `Tools []anthropicTool` and parse `stop_reason` + content blocks. Loop: send → receive `tool_use` → execute via MCP → append `tool_result` → resend. Cap 12 iterations / 60 s budget. **Drop `buildKnowledgeGraph` entirely** — its 5-min cached blob is a workaround for not having tools.

**Day 3 — Streaming + citations.** Switch to SSE (`stream: true`); thread Anthropic deltas through the existing `events_stream.go` pattern. Every `tool_result` carries `{tool, args, result_id}`; surface as `[c1_entity_get:node-abc]` inline citations replacing the regex `[node:hostname]` hack.

**Day 4 — Tool RBAC + refusal.** Per-tool role gate (`c1_node_repair` → admin only; reads → operator). Mirror Holmes's `roles_allowed`. System-prompt rule: "If a tool returns no rows, say 'no data' — never fabricate."

**Day 5 — Operator-mode wiring.** New worker subscribes to `events_anomaly.go` severity≥high emits; calls `/ai/ask` with templated question ("investigate event {id} — root cause + blast radius"); writes verdict to a new `investigations` table; fires existing webhook outbox.

### Architectural test for "chat-first"

If you delete the React dashboard, does the security operator still get value via `curl /ai/ask`?

- **Today:** "barely — single-shot, no tools, fabricates citations."
- **After this 5-day:** "yes — operator can investigate to arbitrary depth from cURL." That's chat-first.

The UI refactor (deferred per owner) is then trivial — the dashboards become *cached, opinionated tool-call results* rather than the source of truth.

### Library pick-list

```
github.com/mark3labs/mcp-go                  # MCP server
github.com/anthropics/anthropic-sdk-go       # official SDK (drop hand-rolled HTTP)
github.com/google/osv-scanner/v2             # CVE enrichment for Attacks pillar
github.com/prometheus/client_golang          # alert ingest for Surge + Health
gonum.org/v1/gonum/stat                      # z-scores + regression
github.com/anatol/smart.go                   # SMART (Health pillar collector)
github.com/miekg/pkcs11                      # HSM probe (bank-specific moat)
```

---

## 7. What we explicitly **do not** chase

These appear in Probo, look "complete," and add nothing to the three pillars for a bank running boring daily ops:

- **Governance documents + e-signature + acknowledgements** — banks have Word + SharePoint + DocuSign. Don't compete.
- **Statement of Applicability** — auditor-only, generated annually. Not a pillar.
- **RoPA / DPIA / TIA** — privacy paperwork. Skip until a regulator forces it (DORA + NDPR conversations may flip this to P2 later).
- **Vendor lifecycle UPDATE flow with questionnaires + renewal calendar** — Excel + Outlook. Not pillar.
- **Training programs + acknowledgements** — LMS territory.
- **Probo's Measures CRUD editor** — we have control mappings; don't replicate.
- **Meetings** — out of scope; Calendar exists.
- **Generic ChatOps slash-commands** — output, not core. Build the core tool_use loop first.

If a paying bank asks for any of the above, build the thinnest possible version. **Never lead with paperwork.**

---

## 8. Honest contested-moats list

| | Status | Path to win |
|---|---|---|
| **Investigation depth** | Holmes wins today | 5-day MCP + tool_use upgrade closes the gap |
| **GRC paperwork** | Probo wins | Don't try to win this. Concede; cherry-pick Findings + Snapshots + Asset criticality. |
| Trust Center | Parity with Probo | Not a moat |
| Auto-documentation | Marketing claim, ~15% delivered | See bugs doc §2 — needs structural rework |

---

## 9. The moats neither has

(verified row-by-row in the moat inventory; full evidence in PR #50's gap doc)

1. **Real provisioning fleet** — agents on Linux/Win/macOS + provider adapters that *create* infra (`internal/provisioning/adapter_*.go`, `cmd/nodeagent/`).
2. **Auto-remediation engine** — signed scripts, leases, rollback, safety gates (`internal/remediation/`).
3. **mTLS + WireGuard mesh + key rotation** between nodes (`internal/mesh/manager.go`).
4. **Auto-block firewall fan-out** to every node (`internal/autoblock/`).
5. **5 behavioral anomaly detectors + 7 threat-intel feeds + correlation engine** (process↔connection 2 s window).
6. **Wizard installer + air-gapped offline bundle** (`scripts/wizard/`, `internal/wizard/`).
7. **SSH CA + command ACL + session recording** (privileged-access plumbing).
8. **HSM PKCS#11 prober** (proposed; bank-specific moat with no OSS competitor — see Health pillar §5).

**Deck-line positioning:** *"Probo manages records, Holmes reads telemetry, Control One **runs the data plane** — over an mTLS+WireGuard fabric that ships in an air-gapped installer."*

---

## 10. Sequencing summary

| Tier | Items | Days |
|---|---|---:|
| **P0 (~3 wks)** | Investigation MCP + tool_use (5d), CVE/KEV (~13d), Operator-mode (~1 wk) | ~21 |
| **P1 (~6 wks)** | Surge detector + Prometheus ingest, Health metric-name fix + SMART/PSI/HSM collectors, Findings overlay, Asset criticality, Tool RBAC + citations + streaming | ~30 |
| **P2 (~6 wks)** | ServiceNow + Jira + Teams writeback, Snapshots, Vendor UPDATE, Probo evidence collectors (top-5) | ~30 |
| **P3** | Risk register, Audit engagements, RoPA/DPIA/TIA, more Holmes integrations | as-asked |

**Critical path to a defensible bank pilot: ~9 weeks (P0 + P1).** Investigation parity with Holmes for relevant scope: 5 days inside that.

---

## 11. Appendix — file pointers

| Subject | Path | Action |
|---|---|---|
| LLM single-shot path | `controlplane/internal/server/ai_ask.go:256` | Replace with `tool_use` loop |
| Hand-rolled KG | `controlplane/internal/server/knowledge_graph.go:235-323` | Delete after MCP ships |
| Investigation handlers | `controlplane/internal/server/investigate.go` | Wrap as MCP tools |
| Anomaly detectors | `controlplane/internal/server/events_anomaly.go:22-300+` | Hook operator-mode trigger |
| Health scoring (read side) | `controlplane/internal/server/node_predictive.go:63-110, 494-539` | Fix metric-name contract; see bugs doc §1 |
| Agent host metrics (write side) | `internal/util/sysinfo.go:53-85` | Add iowait/swap/oom/load_ratio/icmp |
| Generic webhook | `controlplane/internal/server/webhooks.go:557-605` | Adapter targets (PagerDuty, ServiceNow, Jira, Teams) |
| Wiki canonical id | `Wiki/wiki/entities/control-one.md:12` | Update post-decision |
| Wiki synthesis | `Wiki/wiki/synthesis/control-one-deep-gap-analysis.md:12, 244, 257-264` | Reconcile with entity page |
