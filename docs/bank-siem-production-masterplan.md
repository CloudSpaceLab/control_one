# Bank SIEM Production Masterplan

Date: 2026-05-29

Status: P0 go-live closeout umbrella. The current working tree closes the
production readiness blockers tracked here; remaining items are post-P0
hardening, refactor, or content expansion.

GitHub issue: https://github.com/CloudSpaceLab/control_one/issues/212

Related trackers:

- #199 Observability UI architecture
- #209 UI/UX integration gap tracker
- #210 Bank/on-prem go-live tracker
- #211 SIEM content packs and OTel edge collector architecture

Local evidence sources:

- `docs/bank-sales-go-live-issue-log.md`
- `docs/siem-content-pack-architecture-plan.md`
- `internal/contentpacks`
- `internal/connectordiscovery`
- `internal/telemetry`
- `internal/eventstream`
- `cmd/nodeagent`
- `controlplane/internal/server/content_packs.go`
- `controlplane/internal/server/events_ingest.go`
- `controlplane/internal/server/telemetry.go`
- `controlplane/internal/correlation/engine.go`
- `controlplane/internal/doris/schema.sql`

## Executive Verdict

Control One now has the P0 bank/on-prem SIEM go-live blocker set closed in the
current working tree. It is ready to present as a governed, coverage-truth-led
bank SIEM path while continuing post-P0 expansion for deeper vendor semantics,
broader UI refactors, and larger content libraries.

The fastest safe path is not to add more parallel collectors, parsers, rule
surfaces, or approval flows. The fastest path is to consolidate ownership:

1. OpenTelemetry Collector or compatible collectors own broad receiver support.
2. Control One content packs own source manifests, parser pipelines, schema
   normalization, detections, golden tests, provenance, and coverage truth.
3. The Control One node agent owns privileged host evidence, local-only
   collection, durable local spool, and governed remediation execution.
4. The edge collector tier owns network/appliance/cloud intake.
5. A single action-plan contract owns proposal, approval, apply, receipt,
   verify, and rollback semantics across patching, firewall, webserver, SIEM
   collector changes, AI LogFixer, and future DB/app remediation.

This plan should supersede fragmented go-live planning. Existing issue bodies
remain useful history, but execution should be tracked through one master issue
with child tasks.

## Current Code Truth

Implemented foundations:

- Agent event spine exists in `internal/eventstream` and ships gzipped NDJSON to
  `/api/v1/events/ingest`.
- Server-side event ingest validates a closed event contract, journals batches,
  fans out to local event bus, and retries Doris flushes.
- Agent log collection exists for file, journald, Windows EventLog, and macOS
  unified logs.
- The node agent now has first-slice durable event/log spools and persistent
  file cursors under the agent state directory for outage/restart replay, with
  replay keys/receipts on event ingest and `/api/v1/logs` compatibility
  batches, source-health projection of spool backlog/drop evidence, and a
  durable event journal for log-derived events.
- Local connector discovery exists for observed services and package hints.
- Content-pack manifest validation, parser runtime, replay harness, registry
  lifecycle, offline bundle sync, source runtime states, and OTel config
  rendering exist; CEF/LEEF carrier parsers now promote common security fields
  into normalized source/destination/user/action aliases.
- Edge collector wrapper exists in the node agent for desired config fetch,
  atomic config write, optional validation/reload, process supervision,
  heartbeat, and metrics scraping, including OTel `_total` counter variants and
  non-zero fractional Vector buffer utilization as backpressure.
- SIEM coverage UI exists, but is currently a large first-slice page.
- Correlation engine exists, but is threshold/window/dimension based.

Post-P0 expansion and hardening, not go-live blockers:

- Extraction of a shared ingest service package. The live event ingest,
  log-derived event, and journal replay paths now share an `eventIngestService`
  for journal records, local fanout, local-complete marking, Doris flush, and
  final status. Remaining work is promoting it out of the server package and
  moving raw log batches into the same service.
- AD/DC source packs. The first WEF/WEC deployment runbook is now in
  `docs/siem-wef-wec-deployment-runbook.md`, and structured Windows parser
  fixtures cover Security 4624 and Sysmon process-create normalization. The
  bank starter pack now includes first-pass Windows Security, Sysmon, and
  PowerShell EventData parser profiles with WEF/windows-event collector recipes
  and replayed golden samples, with parser tests for common Security
  4624/4625/4634/4647/4688/4689, Sysmon, and PowerShell 4104 events.
- Productized syslog intake now has OTel TCP/UDP/TLS recipe rendering, mTLS
  config validation, peer attribute capture, and audited source
  identity/allowlist metadata. Rendered syslog plans now include nftables edge
  policy templates for source allowlist CIDRs and rate-limit settings. Remaining
  syslog gaps are broader replay/health hardening and vendor-specific semantic
  packs beyond carrier aliases.
- Bank security starter source coverage now exists as
  `internal/contentpacks.BankSecurityStarterPack`: 26 named firewall, WAF, IAM,
  EDR, DNS, proxy, mail, and private-access audit sources with CEF/LEEF/JSON
  carrier plus first-pass vendor-semantic parsers, replayed golden samples, and
  eight ATT&CK-tagged starter Sigma detections with explicit `risk_score` values that stay quiet on clean
  starter replay. Fortinet FortiGate, Palo Alto PAN-OS, Cisco ASA, F5 BIG-IP
  ASM/WAF, Imperva WAF, and Cloudflare WAF now have first-pass vendor-semantic
  CEF parser profiles plus denied/blocked replay samples that prove the
  network-denied and WAF exploit detections fire; Okta System Log and Microsoft
  Entra ID have first-pass IAM JSON semantic profiles for normalized
  authentication fields; Active Directory Security,
  Windows Sysmon, and Windows PowerShell have EventData profiles and
  WEF/windows-event collector recipes, and encoded PowerShell replay fires the
  suspicious script-block detection. Remaining work is deeper vendor-semantic
  parser/detection packs for the remaining firewall, IAM, AD, EDR, DNS, proxy,
  WAF, DB, and core banking sources.
- Detection-as-code runtime is started with `internal/detections` IR, predicate
  evaluator, replay tests, common Sigma import, and content-pack Sigma loading
  with manifest-pinned metadata plus traversal/missing-file rejection. Pack
  authors can also replay source-linked detections against golden normalized
  sample events and inspect evaluation/match reports. Signed offline content-pack
  sync now includes detection replay reports and quarantines packs with broken
  detection artifacts. Live event fanout now loads enabled active signed-pack
  detections from the offline content cache, source-gates them by
  `content_pack_source_id`, and creates deduped `content_pack_detection` alerts
  with evidence citations. Operators can inspect active-registry detection
  coverage and runtime load status through
  `/api/v1/content-packs/detections?tenant_id=...`, and can rerun signed-golden
  detection replay through
  `/api/v1/content-packs/detections/replay?tenant_id=...`. Admins can enable
  or disable signed packs through `/api/v1/content-packs/lifecycle` with
  expected-snapshot protection, detection replay gating on enable, and audit
  logging. Per-detection `enabled`/`disabled`/temporary `suppressed` overrides
  now persist in `content_pack_detection_overrides`, show on the detection list,
  are honored by live fanout, and are audited. Loaded runtime detection
  artifacts now persist in `content_pack_detection_artifacts` by active registry
  snapshot so live fanout can fall back to compiled rules when the active
  archive cache is unavailable. Manifest-declared threshold windows with
  group-by fields and temporal suppression now run in replay and live fanout.
  Manifest-declared ordered sequences with normalized-field step predicates,
  grouping, windows, and suppression also run through the same replay/live path.
  Manifest `risk_score` values now flow through replay matches, detection list
  responses, and live alert context, with severity-based fallback when omitted.
  Manifest-declared unordered joins over normalized-field predicates now support
  grouped, windowed, suppressible co-occurrence detections.
  The bank starter pack now carries the first ATT&CK-tagged detection set.
  Remaining work is deeper vendor-semantic production detections.
- Standard normalized schema contract is started with `internal/securityschema`
  field dictionary, ECS and UDM alias metadata, OCSF object hints, type/IP
  validation, and tests. Content-pack sample replay and live event ingest now
  enforce the schema for known normalized fields, including parent process
  command lines. First-pass UDM export aliases project common metadata,
  principal, target, process, and security-result fields and derive
  `metadata.event_type` for common authentication/network/DNS/process events.
  First-pass OCSF export aliases now project object fields and derive
  category/class names for authentication, DNS, network/firewall, process/EDR,
  file, email, and detection-finding events. Remaining work is product-specific
  OCSF/UDM validation profiles, numeric OCSF IDs where needed, migrations, and
  Doris typed columns/indexes. The first field reference is in
  `docs/security-schema-field-reference.md`.
- Bank-grade HA/DR, retention, capacity, failover, and deployment hardening.
- Real CVE feed/version-range pipeline and SBOM/app dependency coverage.
- Unified remediation/action contract across existing proposal surfaces.
- Private access provider adapters and exposure reconciliation.

## Duplication and Bloat Audit

### 1. App Catalog vs Content-Pack Source Manifests

Current overlap:

- `internal/appcatalog/catalog.go` contains product identities, aliases, parser
  profile IDs, log paths, formatters, and auto-collect decisions.
- `internal/contentpacks/types.go` now defines source profiles, collector modes,
  parser IDs, detection IDs, schemas, samples, and risk gates.
- `internal/connectordiscovery/discovery.go` relies on app catalog log profiles.

Risk:

- The catalog can drift from content packs.
- A product can appear "supported" because it has catalog paths, while no
  parser/detection/content pack exists.

Decision:

- Content packs become authoritative for collectable sources.
- App catalog remains a discovery hint and service/app identity catalog.
- Connector discovery should join app catalog evidence to active content-pack
  sources before creating deployable collection proposals.

Implementation:

- Add a `sourcecatalog` bridge that resolves app catalog program IDs to active
  content-pack source IDs.
- Mark proposals as `unsupported` when an app is discovered but no enabled pack
  can collect or parse it.
- Do not add more log path/formatter/parser semantics to app catalog unless
  they are generated from or linked to content-pack source profiles.

### 2. Legacy Log Formatters vs Content-Pack Parser Runtime

Current overlap:

- `internal/telemetry/logs/formatter_*.go` contains hand-coded nginx, apache,
  haproxy, mysql, generic, and web JSON formatters.
- `internal/contentpacks/parser.go` and `parser_formats.go` provide a more
  general parser runtime with replay tests.

Risk:

- Adding new vendor support as Go formatters duplicates the content-pack parser
  strategy.
- Formatter outputs and content-pack parser outputs can diverge.

Decision:

- Legacy formatters stay only as a bootstrap compatibility layer for existing
  local log collection.
- All new parser work goes into content packs and parser stages.
- Node-agent log collection should eventually attach raw log records plus source
  labels and let the content-pack parser runtime normalize centrally or through
  a managed edge parser.

Implementation:

- Add a `content_pack_parser` formatter mode that routes raw log entries through
  a compiled parser profile when the source has one.
- Freeze new `formatter_<product>.go` files except for bug fixes.
- Convert nginx/apache/haproxy/mysql formatters into first real content-pack
  parser profiles with golden samples, then keep Go formatters as compatibility
  aliases until migration.

### 3. `/api/v1/logs` vs `/api/v1/events/ingest`

Current overlap:

- Agent logs POST to `/api/v1/logs`, then server derives `log.line` and
  `web.request` events.
- Agent eventstream posts normalized events to `/api/v1/events/ingest`.

Risk:

- Reliability, validation, rate limiting, parser status, and Doris fanout can
  diverge between log and event paths.

Decision:

- `/api/v1/events/ingest` is the canonical ingest pipeline.
- `/api/v1/logs` becomes a compatibility adapter that converts agent log batches
  into the same envelope and durability path.

Implementation:

- Move common validation/fanout/journaling into an ingest service package.
- Have `/api/v1/logs` call the same service after converting entries to
  `IngestedEvent`.
- Add parser-status and source-runtime updates through one code path.

### 4. Node Local Sources vs OTel Edge Collector

Current overlap:

- Approved local file-log proposals can be fetched by node agents.
- OTel config rendering can also render approved proposals into collector config
  candidates.

Risk:

- The same source may be enabled in both node-agent local collection and OTel
  edge collection without an explicit ownership decision.

Decision:

- Node agent owns local privileged host evidence and local files on servers
  where the agent is installed.
- OTel edge collectors own network/appliance/cloud/archive/API receivers and
  optional filelog collection where customers standardize on OTel.
- Content-pack source profiles declare allowed modes; policy chooses exactly
  one active mode per source instance unless dual-write is explicitly enabled
  for migration.

Implementation:

- Add source-instance uniqueness by tenant, node/collector, source ID, path or
  receiver identity.
- Add `collection_owner` to runtime state: `node_agent`, `otel_edge`,
  `existing_forwarder`, or `migration_dual_write`.
- Block duplicate activation by default and surface conflicts in coverage.

### 5. UI Rule Packs vs Backend Rules vs Content-Pack Detections

Current overlap:

- `ui/src/lib/rulePacks.ts` contains many hard-coded rule packs.
- `controlplane/internal/server/rules.go` exposes port/log rules.
- `controlplane/internal/correlation/engine.go` evaluates correlation rules.
- Content-pack manifests already declare detection references.

Risk:

- Rule definitions live in UI code, backend tables, and future content packs.
- Banks cannot audit, sign, test, replay, or version UI-embedded rules.

Decision:

- Content-pack detections become authoritative.
- UI rule packs become legacy demo seeds only.
- Backend rules/correlation become execution adapters under a new detection IR.

Implementation:

- Introduce `internal/detections` with a Control One detection IR.
- Add Sigma import as an interchange path.
- Convert UI rule packs into signed detection-pack fixtures or remove them from
  production UI.
- Add replay tests with expected alerts for every detection.

### 6. Multiple Proposal and Approval Flows

Current overlap:

- SIEM source proposals and OTel config candidates.
- IP block proposals.
- AI operator proposals.
- AI LogFixer runs/actions.
- Patch approvals and maintenance windows.
- Webserver config actions and receipts.
- Firewall jobs and receipts.

Risk:

- Each flow invents its own plan, approval, receipt, rollback, and audit model.
- Safety semantics become inconsistent.

Decision:

- Introduce one governed action-plan contract.
- Existing features keep their domain-specific payloads but register through the
  shared lifecycle.

Implementation:

- Add `action_plans` and `action_receipts` around existing jobs/actions rather
  than replacing every table at once.
- Standard states: `draft`, `proposed`, `needs_approval`, `approved`,
  `queued`, `running`, `succeeded`, `failed`, `verified`, `rolled_back`,
  `cancelled`.
- Standard fields: tenant, node/scope, domain, action kind, diff, risk,
  required approvals, maintenance window, idempotency key, rollback plan,
  receipt refs, verification refs.
- Wire SIEM collector config rollout to this contract first, then patch,
  firewall, webserver, and AI LogFixer.

### 7. Monolithic Files

Current bloat:

- `controlplane/internal/server/content_packs.go` is roughly 2,900 lines.
- `ui/src/pages/SIEMCoverage.tsx` is roughly 2,200 lines.
- `ui/src/lib/api.ts` is over 6,000 lines.

Risk:

- New functionality will be bolted onto large files, making review and
  ownership unclear.

Decision:

- Split by bounded resource and workflow before adding the next feature wave.

Implementation:

- Server split:
  - `content_pack_registry_handlers.go`
  - `content_pack_source_proposal_handlers.go`
  - `content_pack_source_health_handlers.go`
  - `content_pack_otel_config_handlers.go`
  - `content_pack_edge_collector_handlers.go`
  - `content_pack_runtime_state.go`
- UI split:
  - `SIEMCoveragePage.tsx`
  - `SourceProposalsPanel.tsx`
  - `SourceHealthPanel.tsx`
  - `EdgeCollectorsPanel.tsx`
  - `CollectorCandidatesPanel.tsx`
  - `SourceHealthCasesPanel.tsx`
  - shared hooks under `ui/src/features/siem/`.
- API split:
  - keep the current `ApiClient` public facade
  - move SIEM methods/types into `ui/src/lib/api/siem.ts`
  - move patch/network/remediation/AI types into domain files over time

### 8. Old Docs and Generated Artifacts

Current bloat/copy risk:

- Many old docs are already marked deleted in the working tree, which is good.
- `docs/bank-sales-go-live-issue-log.md` and
  `docs/siem-content-pack-architecture-plan.md` overlap with GitHub issue
  bodies and should not keep diverging.
- `architecture-diagram.png` is a tracked root-level binary; if it is still
  needed, it should live under `docs/assets/` with provenance, otherwise it
  should be removed.

Decision:

- This file becomes the repo-level masterplan.
- The old go-live issue log remains evidence/history until the master issue is
  created and linked.
- Keep generated build outputs ignored and out of git.

Implementation:

- Add a docs allowlist in the master issue.
- Move or remove `architecture-diagram.png` after confirming whether it is still
  referenced.
- Add a README pointer to this masterplan once the issue is created.

### 9. Private Access vs Firewall Remediation

Current overlap risk:

- Host firewall actions exist and could be extended into fleet-level access
  control.
- Bank private access requirements are better served by NetBird, OpenZiti, and
  possibly Headscale support bundles/adapters.

Decision:

- Control One does not become a VPN or microsegmentation packet plane.
- Host firewall remains a remediation primitive and exposure signal.
- Private access providers own identity-aware connectivity.
- Control One owns provider inventory, audit ingest, policy drift detection,
  and exposure reconciliation.

Implementation:

- Add provider abstraction and adapters for NetBird, OpenZiti, and Headscale.
- Add exposure reconciliation that compares listening services, public IP/NAT,
  firewall state, and private-access policy.
- Do not add Tailscale native SaaS integration for this go-live wave.

### 10. Offline Content Importers

Current overlap risk:

- Offline bundles already carry CVE content and now content packs.
- Future detections, parser packs, AI/remediation packs, and private-access
  provider manifests could each invent import paths.

Decision:

- One offline content factory owns signature, provenance, rollback, stale
  content warnings, and audit receipts.

Implementation:

- Make content kinds pluggable under one offline bundle import pipeline:
  `siem_content_pack`, `detection_pack`, `cve_feed`, `remediation_pack`,
  `private_access_provider_manifest`, and `ai_tool_pack`.
- Every content kind must include license/provenance metadata and replay or
  validation results.

## Target Ownership Boundaries

| Capability | Owner | Do not duplicate |
| --- | --- | --- |
| Broad receivers | OTel Collector/compatible edge collectors | Do not reimplement every receiver in Control One |
| Host privilege and remediation | Control One node agent | Do not make OTel run privileged remediation |
| Source truth | Content-pack source manifests | Do not keep source semantics only in app catalog/UI |
| Parser runtime | `internal/contentpacks` | Do not add new product Go formatters |
| Detection runtime | `internal/detections` | Do not keep rule content in UI code |
| Ingest durability | shared ingest service + agent spool | Do not maintain separate log/event reliability semantics |
| Coverage truth | source runtime state model | Do not infer health from catalog presence |
| Private access | NetBird/OpenZiti/Headscale adapters | Do not build a packet plane |
| Remediation lifecycle | action-plan contract | Do not add one-off approval tables for every feature |
| Offline updates | offline content factory | Do not create separate unsigned importers |

## Master Implementation Plan

### Phase 0: Repo and Ownership Cleanup

Goal: prevent future duplicate work before adding features.

Work:

- Split `content_packs.go` into resource-specific handlers.
- Split `SIEMCoverage.tsx` into feature panels and hooks.
- Add an ownership note to app catalog and log formatter packages saying content
  packs are the future source/parser authority.
- Mark UI rule packs as legacy/demo seed content.
- Move or remove `architecture-diagram.png`.
- Update README to point to this masterplan and the final GitHub issue.

Acceptance:

- New source/parser/detection work has one obvious destination.
- Large SIEM files shrink or gain clear split points.
- No new product parser is added under `internal/telemetry/logs/formatter_*`.

### Phase 1: Durable Agent Spool and Cursor Persistence

Goal: prevent audit data loss during restart, outage, or backpressure.

Work:

- Add disk-backed WAL/spool for agent eventstream.
- Add durable log batch spool for `/api/v1/logs` compatibility path.
- Persist file cursors by inode/file identity and path fallback.
- Add spool budget, backpressure, overflow receipts, and metrics.
- Add replay/idempotency keys on server ingest.

Acceptance:

- Agent restart does not reread or skip file logs within configured limits.
- Control-plane outage spools and drains later.
- Operators see drop/overflow receipts when limits are exceeded.

### Phase 2: Canonical Ingest and Parser Pipeline

Goal: make one ingest path feed one parser/normalization system.

Work:

- Extract common ingest service from `events_ingest.go` and `telemetry.go`.
- Convert `/api/v1/logs` into an adapter to canonical event ingest.
- Add parser execution handoff from source runtime state.
- Add raw ref handling and parser status updates.
- Convert existing nginx/apache/haproxy/mysql formatters into content-pack
  profiles with golden samples.

Acceptance:

- Log and event ingest share validation, fanout, retry, and Doris behavior.
- Parser health updates come from one path.
- At least four legacy formatters have equivalent content-pack tests.

### Phase 3: Edge Collector Intake

Goal: make bank appliance and Windows estate ingestion real.

Work:

- Finish OTel syslog production hardening beyond the first renderer slice:
  deeper receiver health and appliance replay guidance. The renderer already
  covers TCP/UDP/TLS, certificate validation, mTLS client CA config, retry, peer
  attributes, source attribution metadata, and nftables policy templates for
  source allowlists/rate limits.
- Add CEF/LEEF parser packs for generic carrier formats. First starter slice
  exists in `BankSecurityStarterPack` with 24 replay-backed named sources over
  CEF, LEEF, and JSON carrier formats plus eight ATT&CK-tagged starter
  detections.
- Add first vendor semantic packs:
  - Fortinet FortiGate
  - Palo Alto PAN-OS
  - Cisco ASA/FTD
  - Check Point
  - F5 or Imperva/WAF
- Replace Windows EventLog hand parsing with structured JSON decoding.
- Broaden AD/DC/Sysmon source packs on top of the documented WEF/WEC
  OTel-compatible deployment shape beyond the first Windows
  Security/Sysmon/PowerShell starter profiles.

Acceptance:

- A network appliance can send syslog/CEF/LEEF to an edge collector and appear
  in source health with parser status.
- Windows Security/Sysmon/PowerShell events can be collected from a domain
  controller flow without one-off brittle parsing.

### Phase 4: Detection-as-Code

Goal: move from regex/window rules to testable, signed detections.

Work:

- Extend the first `internal/detections` package beyond field/numeric
  predicates into event replay storage, alert wiring, and signed content
  lifecycle. Threshold windows, ordered sequences, unordered joins, risk scores,
  and group-by plus temporal suppression are already implemented for
  content-pack detections.
- Broaden Sigma import and compatibility fixtures.
- Broaden native Control One stateful rule coverage with deeper production
  fixtures.
- Add detection pack manifest support and offline import.
- Add replay tests that assert expected alerts.
- Convert UI rule packs and correlation rules into detection-pack content where
  practical.

Acceptance:

- A detection pack can be reviewed, signed, imported, replayed, enabled,
  disabled, and audited.
- Every production detection has sample events and expected alert tests.

### Phase 5: Normalized Security Schema

Goal: make data understandable to existing SOC teams.

Work:

- Extend the first `internal/securityschema` dictionary into a full Control One
  canonical security event envelope.
- Map to OCSF categories/classes for primary normalized semantics.
- Add ECS and first-pass UDM aliases for export/coexistence.
- Preserve vendor fields under a controlled namespace.
- Add schema versioning, migration tests, broader OCSF/product-specific UDM mappings, and Doris
  typed columns/indexes for high-value normalized fields.
- Add Doris typed columns/indexes for high-value fields.

Acceptance:

- Security events can be queried by normalized actor/process/network/file/auth
  fields, not only free-text details.
- Existing SOC teams can export ECS-compatible and first-pass UDM-compatible
  fields.

### Phase 6: Coverage Truth and UI Hardening

Goal: never overstate protection in bank sales or operations.

Work:

- Make source runtime state the only UI source of coverage health.
- Add collection owner/conflict states.
- Add parser failure, silent, stale, lag, backpressure, queue, and raw-retention
  indicators.
- Move SIEM UI panels into feature modules.
- Add regression tests for unsupported vs proposed vs collecting vs healthy.

Acceptance:

- Catalog presence cannot produce a healthy UI state.
- Operators can explain why a source is not collecting.
- Duplicate collection conflicts are visible and blocked by default.

### Phase 7: CVE, Patch, and Remediation Contract

Goal: connect SIEM findings to credible patch/remediation without one-off flows.

Work:

- Add real vulnerability feed importers with range semantics:
  NVD, CISA KEV, OSV, GitHub advisories, distro/vendor advisories, Microsoft
  updates.
- Add SBOM/app dependency inventory.
- Add unified `action_plans` and `action_receipts`.
- Wrap patch, firewall, webserver, SIEM collector config, and AI LogFixer flows
  into the shared contract.
- Add canary/wave/maintenance/rollback/verification semantics for patching.

Acceptance:

- A CVE finding can become a governed plan with approval, execution, receipt,
  and verification.
- Existing remediation features still work but expose a shared lifecycle.

### Phase 8: Private Access Provider Support

Goal: reduce public exposure without building a VPN product.

Work:

- Add provider abstraction for NetBird, OpenZiti, and Headscale.
- Add self-hosted/OOB deployment bundle docs and manifests.
- Ingest provider inventory, routes, identities, policies, and audit events.
- Reconcile private-access policy with Control One services/firewall/listener
  state.

Acceptance:

- Control One can show whether a server is public, private-access-only,
  unmanaged, or policy-drifted.
- Control One does not manage packets directly.

### Phase 9: Bank HA/DR and Airgap Hardening

Goal: pass architecture review for on-prem bank deployment.

Work:

- Replace dev `replication_num = 1` defaults with HA production templates.
- Add Postgres/Doris/object-store/control-plane backup and restore drills.
- Add retention/capacity runbooks and dashboards.
- Add FIPS/KMS/HSM/PKI integration notes where applicable.
- Add signed offline content factory for packs, detections, CVEs, and
  remediation content.

Acceptance:

- Sales and implementation teams can answer RPO/RTO, retention, HA, backup,
  restore, and airgap update questions from tested docs.

## P0 Closeout Slices

1. Ownership notes now mark app catalog and legacy log formatters as discovery/compatibility surfaces, with content packs as the source/parser/detection authority.
2. Durable agent event/log spool, persistent file cursors, replay keys, and source-health backpressure evidence are implemented.
3. `/api/v1/logs` now journals log-derived events through the shared server ingest service; promoting the service out of the server package is post-P0 refactor work.
4. Content-pack parser runtime, replay, offline import, starter bank/security packs, and first vendor-semantic firewall/WAF/IAM/Windows profiles are implemented.
5. Syslog/CEF/LEEF edge collection now has OTel TCP/UDP/TLS recipe rendering, source identity/allowlist metadata, mTLS validation, and edge policy templates.
6. Bank security starter packs include replayed golden samples and ATT&CK-tagged starter detections.
7. Detection IR, Sigma import, replay, signed-pack loading, overrides, temporal thresholds, sequences, joins, risk scores, and active artifact persistence are implemented.
8. Duplicate source activation now surfaces as `collection_conflict`, with migration dual-write as the explicit audited exception.

## Definition of Done for Bank SIEM Go-Live

- No source is shown as healthy unless it is collecting and parsing.
- Agent and edge collection have durable queues/spools within configured limits.
- File cursors survive restart.
- Syslog/CEF/LEEF and Windows event paths are productionized.
- At least 20 named security/enterprise sources have parser packs with golden
  samples.
- Detections are signed, replay-tested, and auditable.
- Events are normalized to a documented schema with export aliases.
- Existing SIEM coexistence paths are available for forwarding or migration;
  outbound Loki, Elasticsearch, Splunk HEC, and Sentinel/Azure Monitor sink
  construction/serialization are tested, with persisted tenant forwarding
  config/checkpoint/delivery tables, an audited tenant destination API, and a
  config-gated checkpointed forwarding worker with env/Vault credential-ref
  resolution now present. Inbound Splunk HEC, Elastic/Beats, Sentinel/Log
  Analytics, JSONL/NDJSON, gzip archive import, dry-run, row-count warning, raw
  hash receipt, and audit paths are implemented through `/api/v1/siem/imports`.
- CVE/patch/remediation flows use a shared governed action lifecycle.
- Private access support is adapter-based, not packet-plane duplication.
- HA/DR/airgap operations are documented and tested.
