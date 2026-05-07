# Gap Analysis — Probo + HolmesGPT vs. Control One

**Status:** discovery / proposal
**Author:** generated from solution-angle review
**Date:** 2026-05-07

## Why this doc exists

Probo (open-source compliance management) and HolmesGPT (open-source AI incident
investigator) are the two reference points for the experience Control One must
deliver in its category. Neither competes with us at the control-plane layer —
both are *consumers* of the kind of telemetry we already produce. The question
is whether the data we collect is **shaped** correctly to power their use cases
inside one product.

We already collect a lot. This doc only identifies what is **missing or
mis-shaped** if we want one system to deliver:

- Continuous compliance documentation (Probo's job)
- Deep incident investigation with auditable trail (HolmesGPT's job)
- Closed-loop remediation through signed runbooks (our existing job)

---

## TL;DR — the seven gaps that matter

1. **No vendor / third-party / sub-processor register.**
2. **No risk register with treatment plans linked to controls.**
3. **No vulnerability surface (CVE / CVSS / KEV) tied to our existing patch state.**
4. **Evidence store is file-pointer only — no structured "evidence object" with collector, period, and freshness SLO.**
5. **No investigation API / DSL — Doris and Postgres are reachable only by direct SQL, not by an LLM-friendly tool surface.**
6. **No DPIA / processing-activity / data-classification surface — required for GDPR/DORA buyers.**
7. **No people / training / acknowledgement surface — required for SOC 2 CC1 controls.**

Everything else is incremental. These seven are category-defining for a regulated buyer.

---

## What we already have (good news)

| Domain | Status in Control One | Evidence |
|---|---|---|
| Multi-framework control catalogue | ✅ SOC 2, ISO 27001, HIPAA, PCI-DSS v4, GDPR seeded | `framework_control_mappings` |
| Compliance evaluation engine | ✅ Per-rule pass/fail + remediation | `compliance_results` |
| Audit-ready report generation | ✅ Per framework, per period | `audit_reports` |
| Behavioral baseline / anomaly detection (5 vectors) | ✅ Destinations, durations, exes, exfil bytes, DB queries | `*_baselines`, `tenant_known_*` |
| Threat-intel enrichment at ingest | ✅ Spamhaus, Firehol, Tor, AbuseIPDB, OTX | `threat_feeds`, `events.threat_*` |
| Hot OLAP event store | ✅ Doris with 73-field events, process_connections, db_queries, file_accesses | Doris schema |
| Asset & service inventory | ✅ Packages, services, listening ports | `node_packages`, `node_services` |
| Patch lifecycle | ✅ Deployment + per-node state | `patch_deployments`, `node_patch_state` |
| Trust center primitives | ✅ Sub-processors, certifications, incident reports, FAQ | `subprocessors`, `certifications` |
| Misconduct / insider-risk module | ✅ Cases + append-only signal stream | `misconduct_cases`, `risk_signals` |
| Investigate UI backend | ✅ Saved searches, entity timeline | `saved_searches`, `LifecycleItem` view |
| Signed, versioned policy bundles | ✅ With assignment + checksum | `policies`, `policy_versions` |

**Verdict:** the *operational* layer is more complete than either Probo or HolmesGPT.
The gaps are in the *governance* layer (Probo) and the *agent-friendly query
surface* (HolmesGPT).

---

## Probo parity — what we'd need to absorb its surface

Probo organizes 131 MCP tools across 20 domains. Mapping each domain to our
schema:

| Probo domain | Tools | Control One status | Gap |
|---|---:|---|---|
| Organizations | 1 | ✅ tenants | none |
| Users / People | 7 | ⚠️ users exist; **no role acknowledgements, training records, onboarding tasks** | **GAP** |
| Vendors | 6 | ⚠️ `subprocessors` is a flat trust-page table | **GAP — no vendor lifecycle, no security-review workflow** |
| Risks | 8 | ❌ no risk register, no treatment plans, no risk→control linkage | **GAP (P0)** |
| Measures | 11 | ⚠️ policy rules exist, but not "measure" objects with owners + status | **GAP** |
| Frameworks | 4 | ✅ catalogue + mappings | minor: no per-tenant scope toggle |
| Controls | 11 | ✅ control catalogue | no control-owner / due-date model |
| Assets | 4 | ✅ packages, services, nodes | minor: no business-asset abstraction (apps, repos, S3 buckets) |
| Audits | 4 | ⚠️ audit reports yes, audit *engagements* no | **GAP — no auditor portal, no evidence request workflow** |
| Tasks | 7 | ❌ no task/ticket primitive | **GAP** |
| Documents | 17 | ⚠️ policy versions yes, generic document store no | **GAP — no policy approval workflow, no e-sign** |
| Meetings | 6 | ❌ | low priority |
| Snapshots | 3 | ⚠️ `tenant_known_*` are de-facto snapshots | **GAP — no point-in-time compliance snapshot for an audit period** |
| States of Applicability | 11 | ❌ no SoA generation | **GAP (ISO 27001 mandatory)** |
| Findings | 8 | ⚠️ `compliance_results` are findings without lifecycle (open/accepted/closed) | **GAP** |
| Obligations | 4 | ❌ no obligation tracking (legal/contractual) | **GAP** |
| Data Classification | 4 | ❌ | **GAP (P0 for GDPR/DORA)** |
| Processing Activities | 5 | ❌ no RoPA | **GAP (GDPR Art. 30 mandatory)** |
| DPIAs | 5 | ❌ | **GAP** |
| TIAs | 5 | ❌ | **GAP (post-Schrems II requirement)** |

### What this looks like as new tables

Minimum viable additions, all in `controlplane/internal/migrate/sql`:

```text
0XXX_governance.up.sql
  vendors             (id, tenant_id, name, category, criticality, dpa_url, soc2_url,
                       last_review_at, next_review_at, owner_user_id, status)
  vendor_reviews      (vendor_id, period, reviewer_id, findings_json, decision)
  risks               (id, tenant_id, title, category, inherent_score, residual_score,
                       owner_user_id, treatment, status, framework_refs[])
  risk_controls       (risk_id, control_ref)            -- M:N
  measures            (id, tenant_id, name, control_ref, owner_user_id,
                       status, evidence_id, due_at)
  findings            (id, tenant_id, source, source_id, severity, status,
                       opened_at, closed_at, owner_user_id, accepted_reason)
  tasks               (id, tenant_id, title, type, status, due_at, owner_user_id,
                       linked_entity_type, linked_entity_id)
  obligations         (id, tenant_id, source_doc, clause, due_at, owner_user_id, status)
  documents           (id, tenant_id, kind, version, content_uri, signed_by[], status)

0XXX_data_governance.up.sql
  data_classifications  (id, tenant_id, name, sensitivity, retention_days)
  data_assets           (id, tenant_id, name, classification_id, system_ref, owner_user_id)
  processing_activities (id, tenant_id, name, purpose, lawful_basis, data_assets[],
                         vendors[], retention, transfers_outside_eea)
  dpias / tias          (linked to processing_activities)

0XXX_people.up.sql
  user_acknowledgements (user_id, document_id, version, signed_at, ip, user_agent)
  trainings             (id, tenant_id, name, frequency_days)
  training_completions  (user_id, training_id, completed_at, expires_at)

0XXX_audits.up.sql
  audit_engagements     (id, tenant_id, framework, auditor_org, period_start, period_end, status)
  evidence_requests     (engagement_id, control_ref, requested_at, fulfilled_at,
                         evidence_id, reviewer_decision)
```

### Evidence object — the structural fix

Today: `compliance_evidence(file_path, checksum, expires_at)`.
Probo's model treats evidence as a **typed, refreshable object**. We should add:

```sql
ALTER TABLE compliance_evidence ADD COLUMN
  collector       text,           -- 'agent.fim' | 'agent.process' | 'controlplane.policy' | 'manual'
  collection_args jsonb,          -- params used; reproducible
  period_start    timestamptz,
  period_end      timestamptz,
  freshness_sla   interval,       -- 'every 90d', 'every audit period', etc.
  control_refs    text[],         -- multi-control reuse
  status          text;           -- 'fresh' | 'stale' | 'failed' | 'pending_review'
```

This single change converts the existing telemetry stream into auditor-ready
evidence without new collectors — every Doris row already carries the data.

---

## HolmesGPT parity — what an LLM agent needs from us

HolmesGPT ships ~30 toolsets. We don't need to ship 30 — we need to expose the
data we already have through *one* well-shaped MCP/HTTP surface that an
investigator agent can call. Mapping toolsets to our existing data:

| Holmes toolset | Equivalent data in CO | Exposed today? |
|---|---|---|
| prometheus | `telemetry_metrics` | ⚠️ DB only — **GAP: no PromQL endpoint** |
| kubernetes_logs / loki | `telemetry_logs` + Doris events | ⚠️ DB only — **GAP: no LogQL-shaped endpoint** |
| kubernetes (state) | `node_*` tables | ⚠️ REST list endpoints exist; no entity-graph traversal |
| datadog / newrelic | n/a (we are the source) | n/a |
| kafka / rabbitmq | none | low priority |
| postgres / mysql / mongo | `db_queries` in Doris | ✅ partial — **GAP: no per-query plan or slow-query view** |
| elasticsearch / coralogix | Doris full-text on `events.message` | ⚠️ no public search endpoint |
| argocd / kubevela | none | not applicable to our model |
| servicenow / confluence / slab | `incident_reports`, policy docs | ⚠️ readable, not searchable |
| bash / kubectl_run | runbooks (proposed elsewhere) | **GAP: no signed-runbook execution surface** |

### The single fix that unlocks 80% of this

Ship one **`/api/v1/investigate`** endpoint with three verbs:

```
POST /investigate/query     { entity, time_range, signals[], filters }
POST /investigate/timeline  { entity, time_range }
POST /investigate/explain   { event_id }   -- returns enriched + correlated context
```

Plus an MCP server that wraps it. Every Holmes-style toolset becomes a
parameterization of those three verbs against our existing tables. No new
collectors needed — only a query-shaping layer over Doris + Postgres.

---

## The seven gaps, prioritized

| # | Gap | Why it matters | Effort | Priority |
|---|---|---|---|---|
| 1 | **Risk register + treatment plan** | Required for every framework; today we have findings without risk context | M | P0 |
| 2 | **Investigation API + MCP server** | Unlocks Holmes-style agents on top of existing telemetry without any new collection | M | P0 |
| 3 | **Evidence object refactor** | Converts existing telemetry into auditor-ready artifacts; tiny migration, huge leverage | S | P0 |
| 4 | **Vendor / sub-processor lifecycle** | Today flat list; needs review cadence + criticality + DPA tracking | M | P1 |
| 5 | **CVE/KEV layer over `node_packages`** | We have the inventory; we don't enrich with vulnerability data | M | P1 |
| 6 | **Data governance (classifications, RoPA, DPIA, TIA)** | Required for GDPR/DORA buyers; differentiates from US-centric tools | L | P1 |
| 7 | **People surface (acknowledgements, training, SoA)** | SOC 2 CC1 + ISO Statement of Applicability | M | P2 |

---

## What we **don't** need to copy

- Probo's e-sign module → use DocuSign/HelloSign integration, not our own.
- Probo's HTML→PDF stack → keep `audit_reports` simple; outsource rendering.
- Holmes's 30-tool sprawl → one good MCP query surface beats 30 thin wrappers.
- Probo's "compliance officer in Slack" model → that is a *service*, not a product capability. Out of scope.

---

## Recommended sequencing

1. **Week 1–2:** evidence object refactor (gap 3) — unblocks everything.
2. **Week 2–4:** investigation API + MCP server (gap 2) — biggest demo win.
3. **Week 4–6:** risk register (gap 1) — closes the governance loop.
4. **Q3:** vendor lifecycle + CVE layer (gaps 4, 5).
5. **Q4:** data governance + people surface (gaps 6, 7) — unlocks EU/regulated buyers.

---

## Open questions

- Do we want to ship our own "trust page" UI, or expose data and let Probo (or a
  fork) render it? Argues for keeping our trust-center surface read-API only.
- Is the investigation MCP server multi-tenant from day one, or per-tenant
  sidecar? RBAC story is easier per-tenant; cost is easier multi-tenant.
- Where does signed-runbook execution live in this stack? Likely its own doc.
