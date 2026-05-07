# Control One — Deep Gap Analysis vs Probo + HolmesGPT

**Status:** discovery / proposal
**Date:** 2026-05-07
**Method:** 7 parallel Opus 4.7 deep-research agents over actual code (not summaries). Each row verified against `controlplane/internal/migrate/sql/`, `controlplane/internal/server/`, and the upstream repos.
**Goal:** identify *gaps* between Control One and the two reference products. Not a redesign.

---

## Executive summary

| | Probo (compliance management) | HolmesGPT (AI investigation) |
|---|---|---|
| **Already covered or beats them** | Frameworks catalogue, framework→control mapping (5 frameworks seeded), data classification + DLP (`0074`), audit_reports w/ PDF, evidence object with checksum + expiry + recurrence reviews, Trust Center primitives, organizations/tenants | Half of Holmes's relevant toolsets — metrics, logs, entity pivot, fleet health, connections, alerts, threat feed — already exist as REST handlers; just unexposed to an LLM |
| **Contested (we lose today, can win in weeks)** | Vendors (Create/Delete only, no UPDATE), Controls (no owner-assignment), Audits (PDF only, no engagement workflow), Findings (operational not lifecycle) | Investigation depth — `/ai/ask` is single-shot RAG with no `tool_use`. Holmes wins until we ship MCP + tool loop |
| **Hard gaps (net-new domain)** | Risks (no GRC register), SoA, Obligations, RoPA/DPIA/TIA, governance documents + e-sign + acknowledgements, training programs, generic tasks, snapshots, meetings | Operator mode (auto-investigate alerts), bidirectional ticket integration (PagerDuty / OpsGenie / ServiceNow / Jira / Teams / Slack), GitHub PR creation, ChatOps |
| **Our moat (neither has it)** | Real provisioning fleet, auto-remediation engine, mTLS+WireGuard mesh, behavioral anomaly detectors, threat-intel enrichment, auto-block firewall fan-out, patch lifecycle, asset inventory, SSH CA + command ACL, wizard / air-gapped installer | Same — Holmes has no data plane |

**Headline corrections to PR #49:**
- The investigation API is **80% built** (10+ REST endpoints already shipped). The "build investigation API" gap is actually **wrap as MCP + add `tool_use` to `/ai/ask` ≈ 3–5 days**, not weeks.
- The compliance evidence object **already has structured metadata** (framework, control_ref, checksum, expires_at, recurrence). The gap is automated collectors, not schema.
- **Critical correction**: Control One's `policies` table is **technical enforcement** (firewall rules, OS hardening) — there is **no governance-document store**. PR #49 implied parity with Probo's policy lifecycle; that is wrong. Document versioning + e-sign + acknowledgements are full gaps.

---

## Part A — Probo gap (governance / compliance management)

### A.1 Probo MCP tool category coverage (20 categories, 131 tools)

Verified row-by-row against actual migrations and handlers.

| # | Category | Probo tools | Control One state | Severity | Effort | Bank relevance | Defensibility |
|---|---|---:|---|---|---|---|---:|
| 1 | Organizations | 1 | `tenants` | none | — | mandatory | 5 |
| 2 | Users | 7 | `0003_auth`, `0050_mfa`, `0061/62_user_roles`, no invite-flow | partial | S | mandatory | 4 |
| 3 | Vendors | 6 | `subprocessors` Create+Delete only — **no UPDATE** | partial | M | mandatory | 5 |
| 4 | **Risks** | 8 | `risk_signals` is insider-threat scoring, NOT GRC. **No `risk_register` / `risk_treatment` / `risk_controls`** | **full** | **M** | **mandatory** | **5** |
| 5 | Measures | 11 | none — controls + remediation exist but no "measure" object with owner+status | full | M | desirable | 3 |
| 6 | Frameworks | 4 | SOC2/ISO27001/HIPAA/PCI-DSSv4/GDPR seeded; `0078_framework_control_mappings` | none | — | mandatory | 5 |
| 7 | Controls | 11 | CIS seeded (`0077`); `compliance.go`, `GetControlCoverage`. No owner-assignment / status-attestation flow | partial | M | mandatory | 5 |
| 8 | Assets | 4 | `nodes` + `asset_cidrs` + `entity_tags` — infra assets present; missing data/app/process classes + owner→control linkage | partial | M | mandatory | 4 |
| 9 | Audits | 4 | `audit_reports` table + `compliance_reporting.go`. No external-auditor workspace / publish/archive lifecycle | partial | S | mandatory | 4 |
| 10 | Tasks | 7 | `compliance_remediation.go` is task-shaped but tied to remediation runs; no generic GRC task | partial | S | desirable | 3 |
| 11 | **Documents** | 17 | `compliance_evidence` (flat blob store) + technical `policies`. **No governance-doc store, no versioning, no e-sign, no acks** | **partial→full** | **L** | **mandatory** | **5** |
| 12 | Meetings | 6 | none | full | S | desirable | 2 |
| 13 | Snapshots | 3 | none — no point-in-time GRC capture | full | M | desirable | 4 |
| 14 | **States of Applicability (SoA)** | 11 | none — `framework_control_mappings` is global, not per-tenant applicability | **full** | **M** | **mandatory (ISO 27001)** | **5** |
| 15 | Findings | 8 | `alerts` + `security_events` + `health_incidents` + `compliance_results` — operational, not audit-finding lifecycle | partial | M | mandatory | 4 |
| 16 | **Obligations** | 4 | none — no regulatory obligations register | **full** | **M** | **mandatory (CBN/PCI/GDPR)** | **5** |
| 17 | Data Classification | 4 | `0074_data_classification`, `pii_findings`, `dlp.go`, `entity_classify.go` | none | — | mandatory | 4 |
| 18 | **Processing Activities (RoPA)** | 5 | none | **full** | **M** | **mandatory (GDPR Art.30)** | **5** |
| 19 | DPIAs | 5 | none | full | M | mandatory (GDPR Art.35) | 4 |
| 20 | TIAs | 5 | none | full | L | desirable | 3 |

**Totals:** none 4 / partial 8 / full 8.

**Top 5 to close before any bank pilot pitch:** Risks (#4), Documents/policy lifecycle (#11), SoA (#14), Obligations (#16), RoPA+DPIA (#18, #19). **Skip for v1:** Meetings (#12), Snapshots (#13), TIAs (#20).

### A.2 Probo evidence-collection automation

Probo ships **18 connector providers** (`pkg/connector/providers.go`, `pkg/coredata/connector_provider.go`, `pkg/awsconfig/`). Each produces dated, signed evidence bound to a control reference.

| Provider | Evidence | Controls |
|---|---|---|
| AWS (Config/IAM/S3) | IAM users, MFA status, root key, S3 public-access, Config snapshots | SOC2 CC6.1/CC6.6 |
| GitHub | Org members, branch protection, 2FA, Dependabot, repo visibility | CC6.1, CC8.1 |
| Google Workspace, M365 | 2FA enforcement, admin list, drive sharing, login activity | CC6.1, CC6.7 |
| Slack, Sentry, Linear, Notion, HubSpot, DocuSign, Intercom, Brex, 1Password, Supabase, Cloudflare, OpenAI, Resend, Tally | varies | varies |

**Control One coverage today: zero automated collectors.** Provisioning adapters exist for AWS/Azure/VMware/Libvirt but they only *create* infra — they never read posture for evidence. All evidence is manual upload via `compliance_evidence.go`.

**What's structurally missing:**
1. `compliance_evidence_collectors` registry (`provider`, `kind`, `schedule_cron`, `control_refs[]`, `config_encrypted`, `last_run_at`).
2. `evidence_collector` Go interface in `internal/compliance/collectors/` mirroring `internal/provisioning/adapter_*`.
3. Signed evidence objects — extend `compliance_evidence` with `collector_id`, `collected_at`, `period_start/end`, `signature` (Ed25519), `superseded_by`.
4. `metadata` JSONB schema (currently dead).
5. Cron registration at boot via `internal/scheduler/scheduler.go` (already uses robfig/cron).
6. S3/blob backend (currently local temp).
7. OAuth2/secret broker reusing `internal/secrets/`.

**Top 5 collectors for a bank pilot (post-foundation):**

| Collector | Days | Why |
|---|---:|---|
| AWS Config + IAM posture | 5–6 | SOC2 CC6.1/CC6.6, FFIEC |
| Okta / Entra ID MFA + privileged groups | 4–5 | NYDFS 500.12 — identity is auditor question #1 |
| GitHub branch protection + Dependabot + 2FA | 3–4 | SDLC controls (CC8.1), supply-chain (DORA Art.6) |
| Google Workspace / M365 MFA + admin inventory | 3 | CC6.1 attestation |
| Vuln-scan evidence (extend Trivy + Nessus/Qualys) | 4–5 | FFIEC vuln-mgmt, PCI 11.3 |

**Total**: ~6–8 days foundation + ~25 days collectors = **~33 days**.

### A.3 Probo policy/document/people workflow

**Critical correction.** Control One's `policies` table (`0008_policies.up.sql`) models *technical enforcement* — `rule_type`, `rule_definition`, `checksum`. It is firewall/OS-hardening configs nodes pull. **There is no governance-document store, no rich-text body, no acknowledgement trail, no approver workflow.** This is a much bigger gap than PR #49 captured.

| Capability | Probo | Control One | Severity | Effort | Bank |
|---|---|---|---|---|---|
| Document versioning (governance) | `pkg/coredata/document.go`, `document_version.go`, approval-quorum migration | none | **Critical** | L | SOC 2 CC2.2, ISO A.5.36 — mandated |
| E-signature (crypto-sealed) | `pkg/esign/` (TSA RFC 3161, sealing worker, completion cert) + `electronic_signatures` migration | none | **Critical** | M | SOC 2 CC1.4, ISO A.6.2, DORA Art 5 |
| Policy acknowledgement | `policy_version_signatures` UNIQUE(policy_version_id, signed_by) | only `audit_logs` row | **Critical** | S | SOC 2 CC1.4 |
| Training programs | none in Probo (we'd design from ISO A.6.3) | UI-only filter on `compliance_evidence` | High | M | ISO A.6.3, DORA Art 13 |
| Training assignments | — | none | High | S | mandated |
| Training completions | — | filter on `compliance_evidence` | High | S | mandated |
| **SoA generation** | `states_of_applicability` migration | none | **Critical** | M | ISO 27001 cl. 6.1.3 d) |
| Audit engagements | `audits` + `controls_audits` + `findings_audits` | `audit_reports` PDF only | High | M | SOC 2 audit cycle, ISO cl. 9.2 |
| Evidence-request workflow | `evidences` + `evidence_state_transitions` + `pkg/evidencedescriber/` | flat blob | High | M | SOC 2 CC4.1 |
| People profiles (rich HR) | `peoples` + `iam_identity_profiles` | `users` 5 fields | Medium | M | ISO A.6.1–A.6.6 |
| Generic tasks / kanban | `tasks` + `task_state_transitions` + `controls_tasks` | none | Medium | M | nice-to-have |
| Obligations register | `obligations` + linkages | none | High | M | ISO A.5.31, DORA Art 28 |

**Probo packages absorb-able wholesale:**
- `pkg/esign/` — port directly. TSA RFC 3161 timestamping, seal/sealing worker, completion cert. Trade Probo's `pkg/html2pdf` for Control One's `audit_reports` PDF pipeline.
- `pkg/prosemirror/` — rich text doc model + serializers. Required to host actual policy bodies.
- `pkg/coredata/document*.go` schema + state-transition helpers.
- **Skip:** `pkg/vetting/` (tied to Probo's agent runtime), `pkg/accessreview/` (large, defer until Identity Governance is on roadmap).

**Top 3 highest-leverage additions:**
1. **Governance doc store + e-sign + acknowledgements** — 10–14 days. Unlocks SOC 2 CC1.4, CC2.2, ISO A.5.36, A.6.2 in one PR.
2. **SoA generator** — 4–6 days. ISO 27001 cannot be sold without this.
3. **Training programs/assignments/completions** — 5–7 days. Pair with e-sign for "completed + acknowledged" attestation. Closes DORA Art 13 + SOC 2 CC1.4.

**Total to flip Control One from "platform" to "audit-ready governance system": ~25 days.** Adding vendor lifecycle + obligations: another ~10 days.

---

## Part B — HolmesGPT gap (AI investigation)

### B.1 Toolset coverage (33 toolsets)

| Category | Count | Notes |
|---|---:|---|
| **Already covered, just not LLM-exposed** | ~14 | prometheus (`telemetry_metrics_1m` + handler), grafana (dashboards.go), victorialogs/loki/elasticsearch-logs (`telemetry_logs`), investigator (`investigate.go`), logging_utils (regex+filters already), bash-equivalent (remediation engine), plus C1-native (fleet health, connections, top-talkers, alerts, events stream, threat_observations) |
| **Genuinely missing data domains** | ~9 | Kafka admin, RabbitMQ admin, MongoDB Atlas, Azure SQL, Mongo, Elasticsearch cluster API, ServiceNow, Confluence, Slab, plus generic web-fetch (`internet`, `http`) |
| **Out-of-scope for bank endpoint plane** | ~8 | datadog, newrelic, coralogix, kubernetes, aks, openshift, argocd, kubevela, helm |

Three Doris tables that **already have data** but no public read route: `file_accesses`, `db_queries`, `process_lineage` (~½ day each to expose).

**Verdict:** Control One isn't behind Holmes; it's missing the *protocol layer* Holmes uses. Ship MCP server + `tool_use` upgrade + 3 thin read routes → **2–3 weeks to Holmes-parity for relevant scope.**

### B.2 Investigation patterns

Verified state of `controlplane/internal/server/ai_ask.go:181-264`: single-shot LLM call, hand-rolled markdown KG, no `tool_use`, no streaming, no citations, no alert hooks, no ticket writeback.

| # | Pattern | C1 state | Effort | Bank impact |
|---|---|---|---|---:|
| 1 | Iterative `tool_use` (hypothesize→call→refine) | none | 3–4 d | 5 |
| 2 | Tool-result streaming + per-tool memory limit | none | 0.5 d | 3 |
| 3 | Server-side filtering / output_transformers | partial | 0.5 d | 4 |
| 4 | Citations: assertion → tool call → row id | none (`Citations` field exists, never populated) | 1.5 d | 5 |
| 5 | Read-only-by-default + RBAC enforcement | partial | 1 d | 5 |
| 6 | Hallucination refusal when no grounding | none | 0.5 d | 5 |
| 7 | Operator mode (24/7 background investigation) | none (eventbus exists, no consumer) | 1 wk | 5 |
| 8 | Bidirectional alert/ticket writeback | none | 3 d (Phase A) | 4 |
| 9 | Streaming SSE response to UI | none in `/ai/ask` (pattern proven elsewhere) | 2 d | 3 |
| 10 | Cost/context guards (cache, schema RAG) | partial | 1 d | 2 |
| 11 | Confidence scoring | none | 0.5 d | 3 |
| 12 | Multi-turn session state | none | 1.5 d | 3 |

**Smallest viable bank-defensible MVP — 5 days:** rows 1, 4, 5, 6, plus 2/3 as guardrails. Replace static KG dump with a `tool_use` loop wired to 5 read-only tools (`query_doris_events`, `get_node`, `list_alerts`, `lookup_ip_intel`, `get_audit_for_user`). After that: refusal on zero-grounding, citation chips, RBAC-aware tool registry. Stop here for the bank pitch.

**Full parity:** ~14 engineering days.

### B.3 Alert + ticket writeback loop

**Vendor adapters in Control One: zero.** Grep across `controlplane/` for `pagerduty|opsgenie|slack|servicenow|datadog|sentry|alertmanager|teams|jira` returns no matches.

What *does* exist: `eventbus.TopicAlertOpened` (`alerts.go:305`, `correlation/engine.go:166`); generic outbound webhook outbox with HMAC + retries (`webhooks.go`, migration `0014_webhooks`). Building blocks for the loop are present; consumer is missing.

| Integration | Holmes capability | C1 state | Gap | Effort | Bank |
|---|---|---|---|---|---|
| AlertManager | Pull alerts, post annotations | Generic POST only | High | S | Med |
| PagerDuty | Pull incidents, post via Events API v2 | None | High | S | High |
| OpsGenie | Pull/ack/comment | None | High | S | Med |
| **ServiceNow** | Create/comment Incident via Table API | None | **Critical** | M | **Very high** |
| Datadog Alerts | Pull monitors, post comments | None | High | S | Low–Med |
| Sentry | Pull issues, post comments | None | Med | S | Low |
| GitHub Issues + PRs | Comment / open PR | None | Med | M | Low (banks often on GitLab/BB) |
| **Slack ChatOps + writeback** | DM bot + channel writeback | None | **Critical** | M | Med |
| **Microsoft Teams** | Adaptive Cards + writeback | None | **Critical** | M | **Very high** |
| Jira | Comment on issues | None | High | S | High |
| **Operator mode auto-investigate** | Subscribe → tool_use → writeback | Eventbus exists, no consumer | **Critical** | M | **Very high** |

**Operator mode architecture (the load-bearing gap).** New package `controlplane/internal/autoinvestigate/`:
1. Subscribe to `TopicAlertOpened` via existing `eventbus.Subscribe`.
2. Promote `/ai/ask` to `tool_use` loop (extends `anthropicRequest` in `ai_ask.go:283`).
3. Persist verdict to new `alert_investigations` table (alert_id, summary, evidence_json, llm_metadata).
4. Fan out via existing webhook outbox + per-vendor adapters.

Effort: **~2 weeks**. The eventbus and AI plumbing are already there — this is glue + tool definitions + one table.

**Top 3 to ship by (bank-relevance × leverage):**
1. **Operator mode + tool_use loop** — unlocks every other writeback.
2. **ServiceNow + Jira adapters** — banks live in these; two REST calls each, slot into existing webhook outbox.
3. **Microsoft Teams writeback** — most regulated banks are M365-locked; Teams beats Slack for the target ICP.

---

## Part C — Control One moats (neither competitor has)

Verified row-by-row against actual files. 21 capabilities; ranked by uniqueness.

| # | Capability | Probo | Holmes | Bank impact | Evidence |
|---|---|---|---|---:|---|
| 1 | Real provisioning fleet — agents on Linux/Win/macOS + cloud + VMware/Libvirt adapters that create VMs | No | No | 5 | `internal/provisioning/adapter_{aws,azure,vmware,libvirt}.go`; migrations `0004`, `0033`, `0034` |
| 2 | Auto-remediation engine: signed scripts + rollback + leases + safety gates | No | Partial (PR creation, no exec) | 5 | `internal/remediation/engine.go`; `0012_remediation_scripts`, `0019_remediation_rollback`, `0021_remediation_leases`, `0030_remediation_safety` |
| 3 | Signed policy bundles + assignment tracking (CIS seeded) | No | No | 4 | `internal/policy/policy.go`, `policy/sync.go`; `0008_policies`, `0077_seed_cis_policies` |
| 4 | mTLS + WireGuard mesh between nodes with key rotation | No | No | 5 | `internal/mesh/manager.go` |
| 5 | 5 behavioral anomaly baselines (new dst, conn-dur p95, new exe, exfil, new DB query) | No | No | 5 | `0058_anomaly_baselines.up.sql`; `controlplane/internal/behavioral/rollup.go` |
| 6 | 7 threat-intel feeds + IP enrichment + caching + singleflight | No | Investigates external alerts; doesn't host feeds | 4 | `controlplane/internal/threatintel/{feeds,factory,custom}.go`; `internal/ipintel/service.go`; `0052_threat_feeds`, `0067_ip_enrichment_cache` |
| 7 | Auto-block firewall fan-out to fleet (entity action → all nodes) | No | No | 5 | `internal/autoblock/autoblock.go`, `internal/firewall/`; `0080_node_firewall_state`, `0081_node_firewall_rules` |
| 8 | Real-time SSE event stream + Doris OLAP (11 tables, ~100 fields) | No | Pulls external sources only | 4 | `internal/eventstream/`; `controlplane/internal/doris/schema.sql`; `0053_event_ingest` |
| 9 | Process↔connection correlation engine (2 s window) | No | No | 4 | `controlplane/internal/correlation/engine.go`; `0045_correlation_rules` |
| 10 | Compliance evidence with framework + control_ref + checksum + expires_at + recurrence + audit-report PDF | Partial (records, no live wire) | No | 4 | `0075_compliance_evidence`, `0078_framework_control_mappings` |
| 11 | DLP regex PII scanner per-column | No | No | 3 | `controlplane/internal/dlp/scanner.go`; `0074_data_classification` |
| 12 | Patch lifecycle (deployments + per-node state + Squid proxy) | No | No | 4 | `0083_patch_deployments`, `0087_patch_proxy_squid` |
| 13 | Asset inventory: packages, services, ports | No (vendor inv yes; host no) | No | 3 | `0079_node_packages`, `0088_node_services`, `0046_port_observations` |
| 14 | Trust Center (subprocessors, certs, incidents, FAQ) | Yes | No | 1 (parity) | `0076_trust_center` |
| 15 | Misconduct/insider-threat case mgmt + encrypted whistleblower + risk_signals stream | No | No | 4 | `0086_misconduct.up.sql` |
| 16 | OIDC + RBAC + audit_logs + MFA + SSH CA + command ACL | RBAC/audit yes; no SSH CA / cmd ACL | Respects external; ships none | 4 | `controlplane/internal/{auth,sshca,sshproxy}/`; `0041_ssh_ca`, `0043_command_acl`, `0050_mfa` |
| 17 | Vault + AD/LDAP sync | No | No | 3 | `controlplane/internal/vault/client.go`, `controlplane/internal/ldap/client.go` |
| 18 | Wizard installer + air-gapped offline bundles | No | No | 5 | `scripts/wizard/setup_control_one.sh`, `internal/wizard/wizard.go` |
| 19 | Session recording (SSH interceptor + parser) — caveat: OpenReplay upload is no-op stub | No | No | 3 | `internal/sessionrecording/` |
| 20 | Investigation depth (tool-loop / MCP) | Has 131-tool MCP | Holmes core strength | **contested — Holmes wins until MCP ships** | n/a |
| 21 | GRC primitives (vendors, RoPA/DPIA/TIA, e-sign, training, SoA, audits) | Probo core strength | No | **contested — Probo wins** | not present |

### Honest deck-line positioning (defensible against both)

1. *"Only platform with a real node-agent fleet — mTLS+WireGuard, signed remediation, firewall fan-out. Probo manages records, Holmes reads telemetry, we run the data plane."* (#1, #2, #4, #7)
2. *"Behavioral anomaly detection + 7 threat-intel feeds + process↔connection correlation are built in — not a SIEM plug-in."* (#5, #6, #9)
3. *"Compliance evidence is generated from live host state, not uploaded by humans — checksum, expiry, recurrence, mapped to SOC2/ISO/HIPAA/PCI/GDPR controls."* (#10)
4. *"Air-gapped wizard installer — bank-compatible from day one."* (#18)
5. *"Privileged-access plumbing (SSH CA, command ACL, session recording, vault, LDAP) ships with the box."* (#16, #17, #19)

### What we should NOT claim

- **Holmes-grade investigation** — not until MCP + `tool_use` ship. Lead with the data plane, reveal investigation as the platform deepens.
- **Probo-grade GRC** — not until governance docs + e-sign + SoA + RoPA/DPIA land. Lead with live-wired evidence, not paper workflows.
- **Trust Center as a moat** — Probo has it too.
- **Session recording as a moat in demos** — the OpenReplay upload path is a stub; demo carefully.

---

## Part D — Prioritized closure plan

Ordered by `(bank relevance × defensibility) ÷ effort`. Effort assumes existing scaffolding + agent fleet.

### P0 — ship-blockers for any bank pilot

| # | Item | Effort | Source |
|---|---|---:|---|
| 1 | Hidden security fixes — AML gateway auth, sanctions HTTPS, sanctions DOB, OpenReplay no-op, agent Fatal calls | ~1 wk | Wiki synthesis |
| 2 | MCP server wrapping existing 12 handlers + `/ai/ask` tool_use upgrade + 3 thin read routes (file_accesses, db_queries, process_lineage) + grounding refusal + citation chips + RBAC tool registry | **~5 days** | B.1 + B.2 |
| 3 | CVE/KEV enrichment over `node_packages` (OSV/NVD/KEV clients, Trivy parse-detail, vuln dashboard) | ~13 days | Wiki synthesis |
| 4 | Risk register: `risk_register` + `risk_treatment` + `risk_controls` tables + CRUD + scoring + framework linkage | M | A.1 #4 |
| 5 | Operator mode + bidirectional alert writeback for **ServiceNow + Jira + Teams** | ~3 wks | B.3 |

### P1 — required before audit walkthrough

| # | Item | Effort | Source |
|---|---|---:|---|
| 6 | Governance document store + e-signature (port `pkg/esign/`) + acknowledgements | 10–14 d | A.3 #1 |
| 7 | Statement of Applicability generator (per-tenant applicability + justifications + export) | 4–6 d | A.3 #2 |
| 8 | Obligations register (regulatory + contractual, linked to controls) | M | A.1 #16 |
| 9 | Vendor lifecycle UPDATE + review cadence + criticality + contacts + DPA tracking | M | A.1 #3 |
| 10 | Evidence-collector framework + 5 highest-value collectors (AWS, Okta/Entra, GitHub, Google/M365, Vuln) | ~33 d | A.2 |

### P2 — hardening for tier-1 banks

| # | Item | Effort | Source |
|---|---|---:|---|
| 11 | Training programs / assignments / completions schema + UI (paired with e-sign) | 5–7 d | A.3 #3 |
| 12 | Audit engagements (auditor portal, evidence-request workflow) | M | A.3 |
| 13 | RoPA / DPIA / TIA (GDPR Art. 30 / 35 / Schrems II) | L | A.1 #18, #19, #20 |
| 14 | Findings lifecycle (status, owner, due date, evidence link) on top of existing alerts/results | M | A.1 #15 |
| 15 | People profiles (rich HR metadata) | M | A.3 |
| 16 | Holmes integration sources we still want: AlertManager / PagerDuty / OpsGenie / Slack ChatOps | S each, gang in 3 d | B.3 |

### P3 — nice-to-have / EU expansion

| # | Item | Effort |
|---|---|---:|
| 17 | Generic tasks / kanban primitive | M |
| 18 | Snapshots (point-in-time GRC state capture) | M |
| 19 | Meetings | S |
| 20 | GitHub PR creation for proposed fixes | M |
| 21 | Holmes data domains we don't currently read: Kafka/RabbitMQ/Mongo Atlas/Azure SQL/ES cluster API | M each |

---

## Effort summary

| Tier | Item count | Working days |
|---|---:|---:|
| P0 | 5 | ~6 weeks (with hidden-security fixes done first) |
| P1 | 5 | ~12 weeks |
| P2 | 6 | ~10 weeks |
| P3 | 5 | ~8 weeks |

**Critical path to a defensible bank pilot: ~6 weeks** (P0 only). Investigation parity with Holmes for relevant scope: **~5 days inside that 6 weeks**. GRC parity with Probo for the bank-mandatory subset: ~8 additional weeks (P1).

---

## Appendix — repo file pointers

| Subject | Existing path | Action |
|---|---|---|
| Identity (canonical) | `Wiki/wiki/entities/control-one.md` | Reference; do not move |
| Wiki synthesis | `Wiki/wiki/synthesis/control-one-deep-gap-analysis.md` | This doc supersedes for codebase action |
| Investigation handlers | `controlplane/internal/server/{investigate,connections_query,telemetry}.go` | Wrap as MCP tools (P0 #2) |
| AI ask | `controlplane/internal/server/ai_ask.go:181-264` | Replace single-shot with `tool_use` loop |
| Knowledge graph (delete) | `controlplane/internal/server/knowledge_graph.go:230-300` | Remove after `/ai/ask` upgrade |
| Anomaly detectors | `controlplane/internal/server/events_anomaly.go:22-100+` | Keep |
| Eventbus | `eventbus.TopicAlertOpened` | New consumer for operator mode (P0 #5) |
| Webhook outbox | `controlplane/internal/server/webhooks.go:557-605` | Adapter targets (ServiceNow / Jira / Teams) |
| Evidence object | `0075_compliance_evidence`, `compliance_evidence.go` | Extend with collector_id, signature, period, supersede (P1 #10) |
| Policies (technical only) | `0008_policies.up.sql` — for OS hardening | Do NOT confuse with governance docs |
| Governance docs (NEW) | none | Build from Probo's `pkg/coredata/document.go` schema (P1 #6) |
| E-sign (NEW) | none | Port `pkg/esign/` from Probo (P1 #6) |
| AML gateway (P0 fix) | search `178.79.176.19/moov-watchman-aml` | Add auth + HTTPS |
| OpenReplay (P0 fix) | `uploadToOpenReplay()` — no-op | Implement or remove |
| Wizard | `scripts/wizard/setup_control_one.sh` | Keep — major moat |
