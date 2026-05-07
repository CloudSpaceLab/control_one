# Control One — Three Pillars + Chat Redesign

**Status:** proposal
**Date:** 2026-05-07
**Supersedes:** none. Independent of #49 (Probo/HolmesGPT gap doc).

## Vision

> "In a bank, daily ops are mostly boring. The only time anything changes is
> traffic surge, incoming attacks, or server health depreciation. Build a
> system that intelligently monitors those three things and does each very
> well — and when investigation is needed, give complete detail to any depth."
> — owner

Translated into product:

- **Three pillars** as the entire primary surface: traffic surge, incoming attacks, server health depreciation.
- **One chat** as the universal investigation depth tool.
- **Everything else** earns its place by being relevant to one of those four surfaces *right now*. Otherwise it lives in `/settings`, the command bar, or chat-summonable views.

The bet is that this is **the only autonomic control plane regulated banks can actually deploy** — three surfaces an operator already has in their head, one verb (ask), an immutable audit trail under every action.

---

## 1. What this means for the current repo

Five structural truths surfaced by deep code review (`/Users/astra/Engineering/CloudSpace/controlone/`):

1. **Surge is a UX problem, not a data problem.** Doris already has `events.bytes_in/out`, `process_connections.packets_*`, `events_per_hour_mv`, `connection_bytes_baselines`. What's missing is a single ranked-deltas endpoint and a card. **Budget: 2–3 days, not weeks.**

2. **Attacks is the strongest pillar today.** Five inline detectors run at ingest (`controlplane/internal/server/events_anomaly.go:22-100+`), backed by `0058_anomaly_baselines.up.sql`, threat-feed enrichment, and a correlation engine (`controlplane/internal/correlation/engine.go:34-80`). The pillar mostly needs *presentation*, plus optional sigma-rule expansion.

3. **Health is split across three systems that don't know about each other.** `health_incidents` (`0038`) is the canonical incident store, `node_health_scores` (`0084`) is the predictive snapshot, `behavioral_baselines` (`0049`) is the EWMA store, and `securityfacts/collector.go` runs a fourth flat-string lane fed only into compliance. The pillar is mostly a *consolidation* job.

4. **The agent's `eventstream` is the real product surface.** Every collector funnels through `internal/eventstream/stream.go:55` into one ingest endpoint with a closed-world type registry (`events_ingest.go:58-100`). Adding `surge.detected`, `health.smart_failing`, `health.psi_pressure` is a one-liner to the allowed-types map plus a small collector. New pillars get teeth here.

5. **Chat-first must replace `ai_ask.go`, not extend it.** Today's "Ask" (`controlplane/internal/server/ai_ask.go:181-264`, UI `ui/src/pages/Ask.tsx:27`) shoves a hand-rolled Markdown knowledge graph (`knowledge_graph.go:230-300`) into Claude with no tool calling, no streaming, no Doris awareness. It cannot answer "what surged in the last hour" because surge data isn't in the KG. **The right move is to delete the KG cache and ship a tool-shaped MCP-style surface that wraps the handlers we already have** (`connections_query.go`, `investigate.go`, `dashboard.go`, `node_predictive.go`).

---

## 2. The three pillars — what to build

### 2.1 Pillar 1 — Traffic surge

**State today.** Only existing surge-style detector is a 30-min byte-rate window for log volume (`internal/telemetry/spike.go:18-98`). Top-talkers endpoint exists (`controlplane/internal/server/connections_query.go:23`), `events_per_hour_mv` and `connection_bytes_baselines` exist. **No first-class connection/RPS surge detector.**

**Detection method.** Layered ensemble — fire if 2-of-3 agree:
- **EWMA** (primary fast filter, O(1) per update).
- **Robust z-score** via MAD on a sliding window (immune to the very spikes we're hunting).
- **Holt-Winters** seasonal layer for HTTP RPS / txn throughput / DB conns (handles business hours, EoM, FX opens). Port the ~200-LOC Prometheus implementation.

**Bank-specific seasoning.** Never train one global model. Always partition baselines by `(tenant, dimension, hour_of_week, is_business_day, is_payday)`. Existing per-tenant baselines table supports this; just expand the key.

**Library picks.**
- [`VividCortex/ewma`](https://github.com/VividCortex/ewma) — battle-tested EWMA.
- [`influxdata/tdigest`](https://github.com/influxdata/tdigest) — streaming p95/p99; replaces the Postgres baseline math.
- [`gonum/gonum`](https://github.com/gonum/gonum) — MAD, robust z, quantiles.
- [`cilium/ebpf`](https://github.com/cilium/ebpf) — pure-Go eBPF loader for agent-side traffic measurement (kprobe `tcp_sendmsg`/`tcp_recvmsg` → per-CPU hash → 10 s drain). ~1–2% CPU on busy hosts.
- Custom Holt-Winters port (~200 LOC) from Prometheus' `holt_winters.go`.

**Defer.** RRCF, Prophet, S-H-ESD, MERLIN — until v1 false-positive rate is measured. Don't buy Anodot/Chronosphere; ~600 LOC of detection beats a vendor contract.

**MVP shape.**
1. Agent ships per-(conn-class, dest-class) 10 s windows via `eventstream` as `surge.window`.
2. Asynq worker every 30 s pulls last 60 s from Doris, computes EWMA + robust-z + HW band per `(tenant, dimension)`, fires `surge.detected` if 2/3 trigger.
3. Single `/api/v1/surge/ranked` endpoint returns `(port, process, dst_ip)` deltas vs p95.
4. Pillar card consumes that endpoint.

### 2.2 Pillar 2 — Incoming attacks

**State today.** Strongest pillar. Five inline detectors (`events_anomaly.go`), threat-feed enrichment (`schema.sql:140-161`), correlation engine producing alerts (`correlation/engine.go`), `anomaly.*` event family closed-world enumerated, auto-block via firewall jobs (`network_security.go:79-110`).

**What to add (sensors).**
- [`cilium/tetragon`](https://github.com/cilium/tetragon) — Go-native eBPF runtime security, in-kernel filtering, lowest overhead. Embeds cleanly alongside the existing agent.
- [`OISF/suricata`](https://github.com/OISF/suricata) sidecar — multi-threaded IDS, EVE JSON, JA3/JA4, ET Open + ET Pro rules. Tail `eve.json` from Go.
- Selective [`zeek/zeek`](https://github.com/zeek/zeek) scripts for SWIFT/SMB/Kerberos protocol introspection.
- [`osquery/osquery`](https://github.com/osquery/osquery) as scheduled co-process driven by the existing agent.

**What to add (detection engine).**
- [`bradleyjkemp/sigma-go`](https://github.com/bradleyjkemp/sigma-go) — pure-Go Sigma rule evaluator over Doris events + Tetragon/Suricata JSON. Ship top 200 [SigmaHQ](https://github.com/SigmaHQ/sigma) rules; tag alerts with MITRE ATT&CK technique IDs.
- [`dreadl0ck/ja3`](https://github.com/dreadl0ck/ja3) + [`FoxIO-LLC/ja4`](https://github.com/FoxIO-LLC/ja4) — TLS fingerprinting.
- [`hillu/go-yara`](https://github.com/hillu/go-yara) — file/memory scanning.

**Bank-specific moat.** Embed Moov's pure-Go financial parsers, hook them into the existing baseline anomaly detector:
- [`moov-io/iso8583`](https://github.com/moov-io/iso8583) — ATM/POS message anomalies.
- [`moov-io/iso20022`](https://github.com/moov-io/iso20022) — payment-rail tampering.
- [`moov-io/ach`](https://github.com/moov-io/ach) — NACHA fraud anomalies.

**Validation.** [`redcanaryco/atomic-red-team`](https://github.com/redcanaryco/atomic-red-team) in CI; [`rabobank-cdc/DeTTECT`](https://github.com/rabobank-cdc/DeTTECT) for ATT&CK coverage reporting to risk committee — Rabobank built it, banks accept it.

**MVP cut (4–6 weeks).** Tetragon + Suricata sidecars, sigma-go engine over Doris, ATT&CK tags into events, Atomic Red Team smoke tests. Ships a defensible attack pillar without a second platform.

### 2.3 Pillar 3 — Server health depreciation

**State today.** Predictive scoring engine exists (`controlplane/internal/server/node_predictive.go:22-100+`) using EWMA over telemetry signals into `node_health_scores`. Health incidents have dedup keys (`0038`). **But the agent has no dedicated health collector** — it depends on whatever telemetry the host's metrics path happens to emit. SMART metrics are referenced by name in `node_predictive.go:65` with no Linux/Darwin/Win collector behind them.

**Missing collectors (small, additive).**
- SMART: [`anatol/smart.go`](https://github.com/anatol/smart.go) — pure-Go ATA/NVMe/SCSI, no smartctl shell-out. Watch reallocated sectors, pending sectors, NVMe `percentage_used`, media errors. Backblaze's analysis: five attributes (5, 187, 188, 197, 198) explain ~95% of failures — threshold + rate-of-change beats ML.
- IPMI/BMC: [`u-root/u-root/pkg/ipmi`](https://github.com/u-root/u-root) (kernel `/dev/ipmi0`); [`bougou/go-ipmi`](https://github.com/bougou/go-ipmi) over RMCP+ for OS-less BMCs.
- Redfish: [`stmcginnis/gofish`](https://github.com/stmcginnis/gofish) for any 2018+ server (Dell iDRAC, HPE iLO, Supermicro).
- ECC: parse `/sys/devices/system/edac/mc/mc*/ce_count` directly. Climbing CE rate is the best DRAM pre-failure signal.
- PSI (kernel pressure): just enable [`prometheus/node_exporter`](https://github.com/prometheus/node_exporter)'s `pressure` and `tainted` collectors. PSI sustained > 10% for 10 min is a top-tier capacity-decay signal.
- eBPF perf: [`cloudflare/ebpf_exporter`](https://github.com/cloudflare/ebpf_exporter) — biolatency, runqlat, oomkill. ~3% CPU.
- Synthetic probes: [`prometheus/blackbox_exporter`](https://github.com/prometheus/blackbox_exporter) per region for branch-link health.

**Bank-specific moat (no OSS competitor ships these).**
- HSM PKCS#11 prober: [`miekg/pkcs11`](https://github.com/miekg/pkcs11) — open session, `C_GenerateRandom(32)` every 30 s, record latency + error code. Thales/Utimaco/SafeNet all expose this. **HSM session-handle exhaustion is a top-3 bank outage cause.**
- SWIFT/Alliance Gateway: [`gosnmp/gosnmp`](https://github.com/gosnmp/gosnmp) polls Link MIB counters.
- IBM MQ/CICS gateway: [`ibm-messaging/mq-golang`](https://github.com/ibm-messaging/mq-golang).

**Consolidation.** Unify `health_incidents` + `node_health_scores` + `behavioral_baselines` + `securityfacts` into one health surface. Predict at one layer, surface at one place. UX win without schema churn.

**Defer ML 6 months.** Once 90 days of fleet data in Doris, run [`yzhao062/pyod`](https://github.com/yzhao062/pyod) IsolationForest offline, backfill labels. Then consider [`online-ml/river`](https://github.com/online-ml/river) for online scoring.

---

## 3. Chat-first interface

### 3.1 The frontend stack

**Pick** [`assistant-ui/assistant-ui`](https://github.com/assistant-ui/assistant-ui) over CopilotKit, deep-chat, Mendable. Radix-style headless primitives, ChatGPT/Claude parity, MIT, integrates Vercel AI SDK directly. Pair with the [`vercel/ai`](https://github.com/vercel/ai) SDK + [`vercel/ai-chatbot`](https://github.com/vercel/ai-chatbot) template patterns.

**Topology.** `Browser ⇄ Next.js BFF (AI SDK) ⇄ Go control plane (SSE)`. The BFF translates between AI SDK UIMessage and Go's stream; Go remains the source of truth for auth, RBAC, audit. Alternative pure-Go path: emit [`ag-ui-protocol/ag-ui`](https://github.com/ag-ui-protocol/ag-ui) events (16 standard events over SSE, MIT) and skip the BFF.

**Rendering.**
- Citations as `[n]` markers + side rail of source cards (the Perplexity / Honeycomb NLQ pattern). Custom remark plugin → `<CitationChip />`. Memoize markdown to avoid streaming re-render storms.
- Charts as **tool results**, not parsed markdown. Tool returns `{type:'chart', spec:VegaLiteSpec}` → `react-vega`. [`tremor`](https://github.com/tremorlabs/tremor) for fixed dashboards inside chat.
- Action approval via AI SDK 6's `needsApproval: true` + `addToolApprovalResponse`. Mirrors Devin / Cursor agent / Codex Suggest Mode.

**Pick-list (npm):**
```
@assistant-ui/react  @assistant-ui/react-ai-sdk
ai  @ai-sdk/react  @ai-sdk/anthropic
react-markdown  remark-gfm
react-vega  vega-lite
@tremor/react
zod
```

**Backend libs (Go):** [`anthropics/anthropic-sdk-go`](https://github.com/anthropics/anthropic-sdk-go) (first-class streaming + tool-use), [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go) for the MCP server, [`pganalyze/pg_query_go`](https://github.com/pganalyze/pg_query_go) for SQL allowlisting. Skip langchaingo (heavy, leaky abstractions).

### 3.2 Investigation surface — narrow tools + one gated SQL hatch

The 2026 consensus from HolmesGPT, Honeycomb NLQ, Wren AI, k8sgpt: **mostly-narrow typed tools, one broad SQL tool gated by RBAC + approval.** Broad surfaces burn context and hallucinate parameters; narrow tools degrade gracefully and audit cleanly.

**Toolset.**
- `search_events(filters, time_range, limit)` — typed filters over the 73-field `events` table.
- `get_entity_timeline(entity_id, kind, window)` — joins `process_lineage` / `process_connections`.
- `explain_event(event_id)` — single-row evidence pull.
- `list_baselines(entity_id)` / `compare_to_baseline(event_id)`.
- `get_compliance_findings(scope, framework)`, `get_policy(policy_id)`, `list_audit(actor, window)`.
- `top_talkers(window, top_k)`, `connection_lifetime(conn_id)`.
- `get_agent_health(node_id)`, `tail_agent_logs(node_id, since)`.
- `query_doris_sql(sql)` — escape hatch (see safeguards).
- Mutation tools (`propose_*`) emit approval records; never auto-execute.

**Semantic layer.** Adopt the [`Canner/WrenAI`](https://github.com/Canner/WrenAI) MDL pattern: encode entities, relationships, governed access patterns in a manifest. The LLM emits *semantic SQL* against the model rather than raw Doris columns. Snowflake Cortex Analyst and Databricks Genie use the same shape. This is the auditability story.

**SQL sandbox safeguards (quadruple-locked).**
1. Dedicated read-only Postgres role + Doris user. `GRANT SELECT` only. Statement timeout 30 s.
2. Postgres RLS keyed to tenant + RBAC claims. Doris row policies mirror.
3. Parser allowlist via `pg_query_go` / Doris parser. `SELECT` only. Reject `pg_*` / `information_schema` writes. Cap `LIMIT`.
4. Approval queue for any write/mutation tool. Writes never go through SQL — they go through narrow `propose_*` tools.

**Citations.** Every tool returns `{result, evidence: {query_id, source: "doris|pg|agent", rows: [pkey...], snapshot_url}}`. Chat UI hyperlinks every claim to a row-level drill-in. Save every `query_id` to `saved_searches` so a regulator can replay.

**API.**
```
POST /v1/investigate/sessions                  -> {session_id}
POST /v1/investigate/sessions/{id}/messages    -> SSE stream
GET  /v1/investigate/queries/{query_id}        -> replay row-level results
POST /v1/investigate/tools/{name}              -> direct tool invocation (UI drill-in)
POST /v1/investigate/approvals/{action_id}     -> approve/deny queued mutation
```

**Cost / context.** Three-layer schema embedding: static MDL (~2–4k tokens, prompt-cached) + retrieved per-table descriptions (Vanna pattern) + lazy `describe_table(name)` tool. Prompt-cache the static layer; ~90% cost reduction on follow-ups.

**Repos to fork or study.** [`robusta-dev/holmesgpt`](https://github.com/robusta-dev/holmesgpt) (closest analog — fork the toolset YAML loader and investigator loop), [`Canner/WrenAI`](https://github.com/Canner/WrenAI) (semantic layer wholesale), [`k8sgpt-ai/k8sgpt`](https://github.com/k8sgpt-ai/k8sgpt) (Go architectural template), [`vanna-ai/vanna`](https://github.com/vanna-ai/vanna) (RAG-over-schema reference).

---

## 4. Information architecture — five routes, calm by default

The current sidebar registers ~28 nav items across five groups before role filtering (`ui/src/components/shell/Sidebar.tsx:50-128`); `App.tsx:1-168` registers ~50 routes lazy-loaded. This collapses into:

**Routes to keep (5).**

1. **`/`** — three pillar cards stacked + chat input. Equal weight when calm. Earned color (slate → amber → red). Each card: name, one-line state, 60-min sparkline, single numeric anchor. When a pillar escalates, it expands inline; siblings collapse to one-line summaries. No left nav.

2. **`/chat/:threadId`** — persistent investigations. Primary action: pin to landing. Replaces `Ask.tsx`.

3. **`/changes`** — active and recent rollouts/templates. Active rollouts surface on `/` only when correlated with a pillar.

4. **`/incidents/:id`** — post-escalation incident view (PagerDuty-shaped). Carries the DORA classification fields. Primary actions: ack, assign, link runbook execution.

5. **`/settings`** — policies, access, secrets, compliance, vendors, integrations, threat-feeds, frameworks, trust center config. Sectioned, not nested routes.

**Deprecate as routes (reachable via Cmd-K or chat only):** tenants, nodes, jobs, telemetry raw, audit raw, runbooks, templates list, secrets list, access list, vendor list, compliance dashboards, fleet enroll, hypervisors, finacle profiles, misconduct, sessions list. All are filters or settings, not destinations.

**Drill-down rule.** Two clicks to context, three clicks to evidence. If the answer needs a fourth click, it needs chat instead — chat preserves the question, clicks lose it.

**Density principle.** Linear-grade calm, not Tufte. The 2 a.m. operator needs one number, one verb, one color. Density belongs in click 3, not click 1.

**Mobile.** Three cards stacked, nothing else above the fold. Tap → expanded pillar with top-3 slices and a "Chat about this" button that pre-fills context. No settings, no audit, no vendors.

**Cmd-K.** Linear-style command bar carries the long tail. Every deprecated route is a command. Steal the pattern wholesale.

---

## 5. Bank regulatory P0s — ship-blockers for any pilot

We already have signed policies, audit logs, RBAC, mTLS. Specific items that must be added:

| # | Requirement | Driver |
|---|---|---|
| 1 | **Model & prompt registry** — versioned, validated, monitored | SR 11-7, EU AI Act Art. 12, ISO 42001 |
| 2 | **Hash-chained chat audit log** — 6-year retention, SIEM export within 5 min | NYDFS §500.06, DORA Art. 12 |
| 3 | **Dual-control approval** — approver ≠ operator, enforced in policy bundle | FFIEC SoD, DORA Art. 5 |
| 4 | **DORA incident object** — 7 RTS classification fields + 4h/24h/1m reporting webhook | DORA Art. 17–23, Reg. 2024/1772 |
| 5 | **Third-party register API** — sub-processors, exit plan, region | DORA Art. 28–30 |
| 6 | **Human-in-the-loop kill-switch** + auto-remediation off-by-default per tenant | EU AI Act Art. 14 |
| 7 | **Signed RFC integration** — every chat-driven action carries `change_id` to ServiceNow / Jira CR | ITIL, NYDFS §500.16 |

**Each chat turn must persist (immutable, hash-chained):** `session_id`, `user_id` (OIDC sub), tenant, RBAC role, full prompt, system-prompt hash, retrieved context refs, `model_id` + version + provider, params, `tool_calls[]` with `policy_bundle_sig`, `action_executed` before/after, `approver_id`, `approval_method` (step-up MFA), latency, response, citations, `query_id`s.

**Three-role separation, cryptographically distinct:** Operator (drafts, no execute), Approver (signed RFC + step-up auth), Auditor (read-only on the immutable log). Enforce at policy-bundle level, not UI.

P1/P2 (model risk doc, TLPT-ready test harness, BYOK runbook signing, SOC 2 + ISO 27001 + ISO 42001 certs, regulator templates for CBN/MAS/HKMA/RBI) come post-pilot.

---

## 6. Sequenced rollout

| Phase | Weeks | Deliverable |
|---|---|---|
| **0 — Foundations** | 1–2 | New event types in `eventstream`: `surge.window`, `surge.detected`, `health.smart_failing`, `health.psi_pressure`, `health.hsm_latency`. Schema for unified `health_state`. |
| **1 — Surge pillar MVP** | 2–4 | EWMA/MAD/HW detector worker, `/api/v1/surge/ranked`, surge card. Library wiring: `ewma`, `tdigest`, `gonum`, custom HW. |
| **2 — Health collectors** | 3–5 | Embed `smart.go`, `gofish`, EDAC parser, PSI via node_exporter, ebpf_exporter, blackbox_exporter. **HSM `pkcs11` prober.** Consolidate three health systems. |
| **3 — Investigation MCP server** | 4–7 | Narrow typed tools wrapping existing handlers. WrenAI-style MDL semantic layer. SQL sandbox with `pg_query_go` allowlist. Replace `ai_ask.go`. |
| **4 — Chat UI** | 5–8 | assistant-ui + AI SDK + Vega-Lite + Tremor. AG-UI events from Go. AI SDK 6 `needsApproval` flow. Replace `Ask.tsx`. |
| **5 — Three-pillar landing** | 6–9 | Collapse 28 nav items into 5 routes. Cmd-K palette. Earned-severity coloring. Mobile pillar stack. |
| **6 — Attacks pillar deepening** | 7–10 | Tetragon + Suricata + OSQuery sidecars. sigma-go engine. ATT&CK tagging. Atomic Red Team in CI. moov-io parsers wired into baselines. |
| **7 — Bank-defensibility** | 9–12 | Hash-chained audit, DORA incident object, third-party register, dual-control enforcement, kill-switch, ServiceNow/Jira CR integration. |

Phases can run mostly in parallel. Critical path: Foundations → Investigation MCP → Chat UI → Three-pillar landing.

---

## 7. What we explicitly reject

- **A new ML platform.** Threshold + EWMA + Holt-Winters covers 90% of bank surge/health detection. Defer ML 6 months.
- **A second SIEM.** Wazuh, Anodot, Chronosphere all overlap with what we already have. Embed sensors, not platforms.
- **Sidebar utility nav.** Every item taxes the 2 a.m. operator. Cmd-K + chat replace it.
- **A pure-SQL agent.** Hallucinates joins on 73-field schemas. Narrow tools + WrenAI-style semantic layer instead.
- **A "do anything" MCP tool.** Unauditable. Many narrow typed tools, one gated SQL hatch.
- **Vendor lock-in messaging.** DORA Art. 28–30 will require explicit model-portability disclosures regardless.

---

## 8. Open questions

1. Next.js BFF for chat or pure-Go AG-UI? BFF gets free AI SDK features; pure Go keeps the deployment story simple. Lean BFF for v1, plan migration later.
2. Where does runbook execution live? Likely a dedicated `runbooks` package with a signed-bundle format mirroring `policies`. Out of scope for this doc.
3. Multi-tenant vs per-tenant MCP server? Per-tenant has cleaner RBAC at the cost of footprint. Recommend per-tenant with shared schema cache.
4. Do we ship our own Trust Center page, or expose data and let Probo (or a fork) render it? Argues for read-API only.
5. Embedded (in-process) sigma-go vs sidecar? Embedded is one less deploy artifact; sidecar isolates blast radius on rule-eval bugs. Lean embedded with circuit-breaker.

---

## Appendix — concrete file pointers

| Concern | Existing file | Action |
|---|---|---|
| Event registry | `controlplane/internal/server/events_ingest.go:58-100` | Add `surge.*`, `health.*` types |
| Anomaly detectors | `controlplane/internal/server/events_anomaly.go:22-100+` | Keep; add sigma-go evaluator alongside |
| Predictive scoring | `controlplane/internal/server/node_predictive.go:22-100+` | Wire to new SMART/PSI/HSM events |
| Knowledge graph | `controlplane/internal/server/knowledge_graph.go:230-300` | **Delete** — replaced by tool-shaped MCP surface |
| Ask UI | `ui/src/pages/Ask.tsx:27` | Replace with assistant-ui |
| Sidebar | `ui/src/components/shell/Sidebar.tsx:50-128` | Replace with three-pillar shell + Cmd-K |
| Routes | `ui/src/App.tsx:1-168` | Collapse to 5 routes |
| Doris schema | `controlplane/internal/doris/migrations/0001_events_pipeline.up.sql:196-207` | No change; add MV for surge ranked deltas |
| Health tables | `0038_health_incidents`, `0049_behavioral_baselines`, `0058_anomaly_baselines`, `0084_node_health_scores` | Unify under one `health_state` view |
| Agent collectors | `internal/securityfacts/collector.go:55`, `internal/telemetry/telemetry.go:97` | Add SMART, PSI, HSM, IPMI/Redfish |
| Eventstream fan-in | `internal/eventstream/{stream,correlator,batcher}.go` | No change; new event types route through here |
