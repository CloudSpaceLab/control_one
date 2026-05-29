# Control One SIEM Content Pack and Edge Collector Architecture Plan

Date: 2026-05-27

Status: Phase 0 started. The first code slices are `internal/contentpacks` manifest validation, source-level coverage state, an in-memory registry contract, a registry snapshot/restore contract, a parser compiler/runtime contract, a golden replay harness, signed-offline content-pack loading, active-pack registry sync, Postgres snapshot persistence, server-side offline import sync, read-only API/coverage exposure, OTel collector config rendering, rendered config candidate persistence, admin candidate approval, edge collector registration/heartbeat, approved-candidate queueing, collector desired-config/apply-result APIs, rollback queueing, tenant edge-collector coverage projection, collector-scoped token auth, source-level health coverage projection, durable source runtime-state persistence, durable local connector proposal persistence, admin source proposal approval/rejection, source proposal coverage projection, proposal-to-runtime-state projection, approved-proposal-to-node-agent local log-source apply, and the first `/security/siem` coverage/policy/proposal/deployment UI. They define the signed content-pack manifest contract; validate pack identity/versioning, source profiles, collector modes, parser stages, schema mappings, sample/golden coverage, detection references, high-risk approval gates, and duplicate/reference integrity; add runtime source coverage states/transitions for the future coverage truth API/UI; freeze pack install/enable/quarantine/compatibility/source-resolution behavior before storage/API wiring; provide deterministic snapshot/restore state for enabled/quarantined packs; provide executable carrier-format parser stages for common SIEM content packs; replay manifest-declared samples against expected normalized JSON/JSONL outputs; allow signed offline bundles to carry active `siem_content_pack` artifacts with receipt provenance; attach replay-passing/failing active packs to the registry enable/quarantine lifecycle; persist active registry snapshots per tenant; trigger sync after offline bundle import; expose `/api/v1/content-packs` and `/api/v1/content-packs/sources`; project content-pack registry state into tenant parser coverage truth; render deterministic OTel Collector configs with config versions; persist exact rendered config candidates; expose tenant-scoped candidate detail with exact rendered YAML and source plan for operator/admin review; require candidate approval to acknowledge the exact reviewed `sha256:` config version and persist the reviewed version/YAML digest; audit admin approval of rendered candidates; record tenant-scoped collector identity, status, running config version, and heartbeat health evidence; queue approved candidates only when the caller supplies a matching `expected_config_version` and the candidate still carries a matching reviewed config version; let collectors fetch/apply/report the exact queued config version; re-queue superseded exact config versions for rollback; expose collector freshness/config drift as tenant telemetry coverage; let collectors authenticate only their own heartbeat/config/apply calls with scoped tokens; project collector heartbeat health evidence into parser-domain source health truth; persist latest per-source runtime truth for API/coverage reads and expose source instance IDs, approval refs, runtime labels such as collect mode/raw-retention evidence, and recommended investigation actions; persist node-reported connector proposals for operator review; preserve explicit approval/rejection/privacy-block decisions before any collection rollout; persist source approval collect-mode intent; support node-agent local `collect_parsed` transport without raw message retention; support first-pass OTel `collect_parsed` rendering with a transform redaction processor; project agent log ingest batches into `collecting` source runtime state; keep metadata-only/observe-only/disabled approvals non-collecting while projecting proposal-observed runtime proof labels; show proposal status in tenant coverage truth without claiming runtime health; project proposal decisions into source runtime states as pre-collection coverage truth; let node agents fetch and hot-add approved local file-log proposals; and give operators a first UI path to review connector policy, proposals, source health, edge-collector config candidate rollout, source-health-to-SOC-case investigation handoff, structured runtime-state case evidence refs, persisted cited analyst notes, and recent source-health SOC case visibility with note hydration.

Related:

- Bank go-live tracker: GitHub issue #210
- Master implementation epic: GitHub issue #211
- Current go-live evidence log: `docs/bank-sales-go-live-issue-log.md`

Implemented so far:

- `internal/contentpacks/types.go`
- `internal/contentpacks/manifest.go`
- `internal/contentpacks/manifest_test.go`
- `internal/contentpacks/state.go`
- `internal/contentpacks/state_test.go`
- `internal/contentpacks/registry.go`
- `internal/contentpacks/registry_test.go`
- `internal/contentpacks/clone.go`
- `internal/contentpacks/parser.go`
- `internal/contentpacks/parser_formats.go`
- `internal/contentpacks/parser_test.go`
- `internal/contentpacks/archive.go`
- `internal/contentpacks/archive_test.go`
- `internal/contentpacks/replay.go`
- `internal/contentpacks/replay_test.go`
- `internal/contentpacks/otel_collector.go`
- `internal/contentpacks/otel_collector_test.go`
- `controlplane/internal/offlinebundle/content_pack.go`
- `controlplane/internal/offlinebundle/content_pack_test.go`
- `controlplane/internal/migrate/sql/0113_content_pack_registry_snapshots.up.sql`
- `controlplane/internal/migrate/sql/0113_content_pack_registry_snapshots.down.sql`
- `controlplane/internal/migrate/sql/0114_content_pack_collector_config_candidates.up.sql`
- `controlplane/internal/migrate/sql/0114_content_pack_collector_config_candidates.down.sql`
- `controlplane/internal/migrate/sql/0115_content_pack_edge_collectors.up.sql`
- `controlplane/internal/migrate/sql/0115_content_pack_edge_collectors.down.sql`
- `controlplane/internal/migrate/sql/0116_content_pack_source_runtime_states.up.sql`
- `controlplane/internal/migrate/sql/0116_content_pack_source_runtime_states.down.sql`
- `controlplane/internal/migrate/sql/0117_content_pack_source_proposals.up.sql`
- `controlplane/internal/migrate/sql/0117_content_pack_source_proposals.down.sql`
- `controlplane/internal/storage/content_pack_registry.go`
- `controlplane/internal/storage/content_pack_collector_config.go`
- `controlplane/internal/storage/content_pack_edge_collector.go`
- `controlplane/internal/storage/content_pack_source_runtime_state.go`
- `controlplane/internal/storage/content_pack_source_proposal.go`
- `controlplane/internal/auth/middleware.go`
- `controlplane/internal/storage/content_pack_registry_test.go`
- `controlplane/internal/server/content_packs.go`
- `controlplane/internal/server/content_packs_test.go`

## Executive Decision

Control One should not fork or deeply extend `otelcol-contrib` into a SIEM product, and it should not copy the full parser libraries of NetBird/Tailscale/headscale/OpenZiti-adjacent systems or SIEM competitors.

The reliable architecture is:

1. Use OpenTelemetry Collector/contrib as the optional edge receiver and transport substrate for broad input coverage.
2. Keep the Control One node agent as the privileged local evidence and remediation agent.
3. Build a Control One content-pack runtime above collectors for source manifests, parser pipelines, normalization, detection rules, tests, provenance, offline distribution, and coverage truth.
4. Use OCSF as the primary security event model, with ECS-compatible export fields where useful for existing SOC/SIEM coexistence.
5. Treat Sigma as a detection interchange format and authoring bridge, not as the parser model.

This gives banks a single intelligence platform without forcing Control One to become a collector zoo, VPN product, or manual connector marketplace.

## Why This Architecture

The current code now owns useful host evidence, service discovery, packages, events, patch jobs, webserver remediation, AI investigations, vulnerability feeds, firewall actions, connector proposal generation, durable agent replay, syslog/CEF/LEEF and WEF-compatible edge paths, signed content-pack parsing/detection replay, and first-pass security schema normalization. Post-P0 work should deepen vendor semantics and refactor ownership boundaries without reintroducing parallel collector/parser/rule surfaces.

OpenTelemetry Collector contrib already supplies a large receiver ecosystem, including file logs, syslog, Windows Event Log, Splunk HEC, Kafka, cloud queues/storage, Prometheus, databases, NGINX, HAProxy, Netflow, SNMP, vCenter, and more. The receiver docs define receivers as the components that collect telemetry from sources and formats. That is the right layer to reuse.

What OTel does not give us is bank-ready SIEM semantics:

- Product-specific parser ownership.
- Security schema mapping.
- Parser golden tests.
- Detection content.
- SOC coverage states.
- Signed offline content packs.
- Approval gates for sensitive systems.
- Remediation links and receipts.

That layer is where Control One should differentiate.

## Non-Goals

- Do not build a Control One VPN, WireGuard control plane, or microsegmentation engine.
- Do not integrate Tailscale natively in this go-live wave.
- Do not make `otelcol-contrib` the Control One business logic runtime.
- Do not silently collect high-risk bank, IAM, database, or core-banking logs.
- Do not claim a connector is healthy because a product appears in the app catalog.
- Do not embed GPL content such as Wazuh rules/decoders unless the business explicitly accepts the licensing consequences.

## Target Topology

Control One should support three deployment shapes, all on prem:

1. Single-site bank pilot:
   - Control plane, Postgres, Doris, object storage, worker, UI.
   - Node agents on representative servers.
   - One edge collector VM for syslog/OTLP/WEF/vendor API sources.

2. Multi-site institution:
   - Central control plane or regional control planes.
   - Regional edge collectors close to firewalls, domain controllers, apps, and network devices.
   - Node agents on servers where host-level evidence/remediation is needed.

3. Airgapped bank:
   - Same topology, but content packs, CVE feeds, detections, and collector profiles are imported through signed offline bundles.
   - No external pull dependency at runtime.

## Data Flow

1. Discover:
   - Node agent observes packages, processes, listening services, webserver inventory, DB hints, local log candidates, firewall state, and host facts.
   - Edge collector observes configured network/API sources.

2. Propose:
   - `internal/connectordiscovery` turns local evidence into connector proposals.
   - Edge collectors report receiver health, source identity, parser candidate, and observed event shape.
   - Control plane stores proposal state per node/source/app.

3. Decide:
   - Bank policy evaluates data sensitivity, source class, environment, volume budget, path allowlists, and approval requirements.
   - Low-risk infra logs can auto-connect.
   - High-risk sources such as core banking, IAM, DB audit/query logs, and customer-data-bearing app logs require explicit approval.
   - Approval records carry collection intent: `observe_only`, `metadata_only`, `collect_parsed`, `collect_raw`, or `disabled`.

4. Collect:
   - Node agent tails approved local sources and sends Control One events.
   - OTel edge collectors receive syslog, CEF, LEEF, OTLP, WEF-adjacent exports, Splunk HEC, Kafka, cloud archive streams, and appliance feeds.
   - Collector configs are generated from Control One content packs and policy.

5. Buffer:
   - Agents and edge collectors use durable local queues/spools for critical logs.
   - File cursors persist across restarts.
   - Backpressure, lag, drop counts, retry state, and volume throttling are reported to the control plane.

6. Parse:
   - Raw records are classified by source profile and event shape.
   - Parser stages decode JSON, syslog, CEF, LEEF, key/value, logfmt, XML, Windows EventData, regex/grok, vendor fields, and product-specific variants.
   - Failed parses preserve raw references and generate parser-health events.

7. Normalize:
   - Normalize to Control One canonical fields backed by OCSF class/category mapping.
   - Preserve raw body/ref and vendor fields.
   - Add ECS-compatible aliases for exports and SIEM coexistence.

8. Detect:
   - Detection rules run over normalized fields.
   - Sigma imports compile into Control One detection IR where possible.
   - Stateful correlation, sequences, suppressions, risk scoring, and ATT&CK mapping run in Control One.

9. Investigate and Act:
   - AI investigations consume normalized events, alerts, CVEs, package inventory, patch state, service exposure, private-access state, and remediation receipts.
   - Remediation stays proposal-first with typed plans, approvals, verification, rollback, and receipts.

## Component Design

### 1. Content Pack Registry

The registry stores signed, versioned packs. Packs may ship with the product or be imported offline.

Responsibilities:

- Validate pack manifests.
- Enforce semantic versioning and compatibility.
- Track provenance, license, author, signature, and hash.
- Support rollback to a previous known-good pack.
- Publish pack inventory and health to the UI/API.

Pack lifecycle states:

- `available`
- `installed`
- `enabled`
- `disabled`
- `quarantined`
- `deprecated`
- `rollback_available`

### 2. Source Profile Manifest

Each pack declares one or more source profiles. A source profile represents a collectable product/service/log family, not just a parser.

Required manifest fields:

- `pack_id`
- `pack_version`
- `source_id`
- `display_name`
- `vendor`
- `product`
- `versions`
- `source_class`
- `risk_class`
- `data_sensitivity`
- `collector_modes`
- `approval_required`
- `required_privileges`
- `expected_volume`
- `raw_retention_default`
- `schemas`
- `parsers`
- `sample_cases`
- `detections`
- `license`
- `provenance`

Example shape:

```yaml
pack_id: controlone.nginx
pack_version: 1.0.0
sources:
  - source_id: nginx.access
    display_name: NGINX Access Log
    vendor: nginx
    product: nginx
    source_class: webserver
    risk_class: low
    data_sensitivity: moderate
    approval_required: false
    collector_modes:
      - node_filelog
      - otel_filelog
      - syslog
    schemas:
      primary: ocsf
      export_aliases:
        - ecs
    parsers:
      - parser_id: nginx.access.combined
        entrypoint: parsers/access-combined.yaml
    sample_cases:
      - samples/nginx-access-combined.jsonl
    detections:
      - detections/web-scanner-burst.yaml
```

### 3. Collector Recipe Generator

The generator converts source profiles and policy into concrete collector configuration.

Outputs:

- Node agent log source configs.
- OTel Collector YAML for filelog, syslog, Windows Event Log, Splunk HEC, Kafka, OTLP, cloud archive receivers, and Prometheus-style metrics.
- Receiver identity and TLS/mTLS settings.
- Queue/retry/exporter settings.
- Redaction and drop processors where safe to perform at the edge.

Design rules:

- Generated configs are deterministic and diffable.
- Config changes are applied through a plan/approve/apply path for sensitive sources.
- Every generated collector pipeline emits health metrics.
- Config render errors block enablement and surface in coverage truth.

### 4. Parser Runtime

Parser runtime should be a Control One-owned Go package, not an OTel fork.

Initial parser stages:

- `json`
- `syslog_rfc3164`
- `syslog_rfc5424`
- `cef`
- `leef`
- `regex`
- `grok`
- `kv`
- `logfmt`
- `xml`
- `windows_eventdata`
- `timestamp`
- `field_map`
- `redact`
- `drop`
- `enrich`
- `ocsf_map`
- `ecs_alias`

Runtime properties:

- Compiles parser pipelines once and caches them.
- Uses bounded memory per event and per batch.
- Records per-stage latency and failures.
- Never drops raw input silently.
- Supports parser version pinning per source.
- Supports replay of raw events through a new parser version before rollout.

### 5. Normalization Model

Control One should keep its current event contracts but add a security normalization layer.

Primary normalized envelope:

- `event.id`
- `event.time`
- `event.ingested_at`
- `event.kind`
- `event.category`
- `event.class`
- `event.action`
- `event.outcome`
- `event.severity`
- `source.profile`
- `source.version`
- `source.node_id`
- `source.collector_id`
- `source.raw_ref`
- `observer.type`
- `actor`
- `process`
- `file`
- `network`
- `http`
- `dns`
- `tls`
- `auth`
- `database`
- `cloud`
- `vulnerability`
- `labels`
- `vendor`

Schema policy:

- OCSF mapping is first-class because it is a security event normalization framework with categories, classes, dictionary, objects, observables, and base event concepts.
- ECS aliases are emitted where useful because many banks already have Elastic/SIEM/SOC tooling using ECS field names.
- Original vendor fields are preserved under a controlled namespace.
- Raw payload storage can be disabled or shortened by policy, but raw refs and parser receipts remain.

### 6. Detection Content

Detection content should use a Control One detection IR with Sigma compatibility.

Why:

- Sigma is strong as a portable rule format and defines detection, logsource, and metadata concepts.
- Sigma rules target logs by `category`, `product`, and `service`, which maps cleanly to Control One source profiles.
- Control One still needs its own runtime for stateful correlation, joins, patch/CVE context, private-access context, suppressions, and governed remediation links.

Detection pack assets:

- Sigma-imported rules.
- Native Control One rules.
- Correlation graphs.
- ATT&CK tags.
- Test events.
- Expected alerts.
- False-positive notes.
- Tuning knobs.
- Suppression defaults.

### 7. Coverage Truth Model

Coverage truth is the sales-critical UI/API contract.

Per source, show:

- `discovered`
- `proposed`
- `approval_required`
- `approved`
- `config_rendered`
- `deployed`
- `collecting`
- `parser_healthy`
- `parser_failed`
- `silent`
- `backpressured`
- `unsupported`
- `privacy_blocked`
- `stale`

Metrics:

- Events received.
- Events parsed.
- Parse failure rate.
- Dropped events.
- Lag.
- Cursor age.
- Last event time.
- Last successful parse time.
- Queue depth.
- Retry count.
- Raw retention state.
- Content-pack version.
- Collector config version.

### 8. Storage and Query

Storage should separate concerns:

- Raw payload/ref store:
  - Object storage or compressed local/central spool.
  - Retention controlled by sensitivity policy.

- Normalized hot event store:
  - Doris for high-volume analytics and correlation.
  - Partition by time, tenant, event family/source class where practical.
  - Retain key dimensions as typed columns for performance.

- Control metadata:
  - Postgres for source state, pack state, approvals, config versions, parser health, and receipts.

- Search/export:
  - Query normalized fields first.
  - Export ECS aliases, raw refs, and original vendor fields.
  - Forward to Splunk/Sentinel/Elastic/Loki when customer wants coexistence.
  - Current outbound compatibility includes tested Loki, Elasticsearch, Splunk
    HEC, and Azure Monitor/Sentinel sink construction/serialization in
    `controlplane/internal/logforward`; persisted tenant forwarding
    destinations/checkpoints/delivery attempts and an audited redacting
    destination API are present. The `siem_forwarding` worker can now drain
    enabled destinations with checkpointed batches and delivery-attempt
    evidence using pluggable env/Vault credential resolution. Inbound import
    paths remain go-live work.

## Performance Design

Performance goal: the common path should be streaming, bounded, batch-oriented, and observable.

Rules:

- Compile parser pipelines at pack install/enable time.
- Cache compiled regex/grok patterns.
- Avoid repeated schema reflection per event.
- Batch decode/normalize/export where possible.
- Use backpressure instead of unbounded queues.
- Keep per-source budgets for CPU, memory, event rate, and bytes/sec.
- Support sampling only for non-audit observability data; never silently sample security audit logs.
- Make parser health cheap to compute from counters, not expensive ad hoc queries.
- Store high-cardinality vendor fields carefully; promote only useful fields into indexed columns.

Initial sizing targets for pilot:

- Node agent local logs: thousands of events/sec per busy host within configured CPU/memory caps.
- Edge syslog collector: tens of thousands of events/sec per collector VM with disk queue enabled.
- Parser latency: p95 under 25 ms per event batch for common source packs.
- Collector config rollout: deterministic diff and rollback in under one minute per collector group.

These are targets, not promises; benchmark harnesses must be added before sales claims.

## Reliability Design

Reliability requirements:

- At-least-once delivery within configured spool limits.
- Persistent file cursors.
- Disk-backed queue for critical audit logs.
- Idempotent ingest by event/source/cursor hash where feasible.
- Crash-safe pack install and rollback.
- Parser quarantine for bad pack versions.
- Replay raw events through parser versions.
- Explicit data-loss receipts when configured limits are exceeded.
- Health probes for edge collectors and node agents.

Failure behavior:

- Control plane outage: agents and edge collectors spool until budget is exhausted.
- Parser failure: raw is preserved, parser health turns red, normalized event may be partial.
- Content-pack regression: pack is quarantined or rolled back; affected sources show degraded coverage.
- Volume burst: lower-priority sources throttle before P0 audit/security sources.

## Security and Governance

Security requirements:

- mTLS between agents, edge collectors, and control plane.
- Signed collector configs.
- Signed content packs and offline bundles.
- RBAC and dual approval for high-risk connector enablement and remediation actions.
- Immutable audit log for pack install, source approval, config rollout, detection enablement, and remediation.
- Redaction profiles with tests.
- Secret references through vault/KMS/HSM integration, never inline in pack manifests.
- Tenant/environment scoping for every source and collector.
- License metadata and third-party content attribution for every imported parser/rule/pattern.

## Implementation Plan

### Phase 0: Contract Freeze

Goal: define contracts before widening source coverage.

Status: Closed for P0 go-live. The remaining work in this section is tracked as
post-P0 content and refactor expansion, not a blocker for bank sales go-live.

Build:

- `internal/contentpacks` manifest structs and validation. First slice complete.
- Content-pack compatibility/version model. First slice complete with `Registry`, semantic version comparison, `min_control_one_version` checks, duplicate pack rejection, enable/disable/quarantine/deprecate status transitions, rollback-candidate marking, and active source resolution.
- Registry snapshot/restore model. First slice complete with JSON-shaped snapshot records, lifecycle state preservation, compatibility recomputation on restore, defensive manifest validation, duplicate rejection, enabled-source conflict rejection, tenant-scoped Postgres snapshot history, active snapshot retrieval, server-side persistence after offline bundle content-pack sync, and read-only tenant API projection.
- Source profile state model. First slice complete with `SourceRuntimeState`, metrics, approval reference rules, and source instance validation.
- Parser stage interface. First slice complete with `ParserRuntimeRegistry`, parser compilation, compiled stage execution, parser statuses, stage errors, and initial executable stages for JSON, RFC3164/RFC5424 syslog, CEF, LEEF, regex, grok, KV, logfmt, XML, Windows EventData, timestamp, field mapping, OCSF/ECS alias mapping, enrichment, redaction, and conditional drop.
- Golden replay harness. First slice complete with manifest-level sample replay from an `fs.FS`, JSON/JSONL/text/XML input support, JSON/JSONL golden output support, exact or subset field comparison, per-case failure reporting, parser-error expectations, sample size limits, and content-root path traversal rejection.
- Signed offline content-pack artifact support. First slice complete with direct manifest and tar/tar.gz pack parsing, in-memory pack filesystem support for samples/goldens, active `siem_content_pack` discovery from the existing offline bundle active-content directory, receipt provenance, replay of active pack goldens, and registry sync that installs/enables passing packs while quarantining replay-failed packs.
- Coverage truth state machine. First slice complete with source-level states, legal transitions, healthy/degraded classification, collector/parser evidence requirements, `collection_conflict` duplicate-owner truth, and tenant parser-domain overlays for active content-pack registry, source proposals, edge collectors, and source runtime health.
- GitHub issue breakdown from this plan.

Acceptance:

- A pack with source profile, parser, sample input, golden normalized output, and detection metadata can be validated in tests. First slice complete.
- Invalid packs fail with operator-readable errors. First slice complete.
- Runtime source state cannot claim deployed/collecting/parser-healthy without the required collector/parser evidence, and high-risk/high-sensitivity active collection requires an approval reference. First slice complete.
- Parser profiles can now compile only when every stage has an executable runtime in the current build; common SIEM carrier-format stages have first-pass executable runtimes, and Windows EventData now emits initial Security/Sysmon/PowerShell normalized aliases. The bank starter pack now includes first-pass Windows Security, Sysmon, and PowerShell EventData parser profiles with WEF/windows-event collector recipes and replayed golden samples.
- Manifest-declared samples can now be replayed against golden normalized outputs with deterministic pass/fail reports. First slice complete.
- Signed offline bundles can now carry active SIEM content-pack artifacts, replay their embedded sample/golden cases with bundle receipt provenance, and sync passing/failing packs into the registry enabled/quarantined lifecycle. First slice complete.
- Registry lifecycle state can now be snapshotted/restored deterministically, persisted per tenant, refreshed by server-side signed offline bundle import when `siem_content_pack` artifacts are present, listed through read-only APIs, and reflected in tenant parser coverage. First slice complete.
- OTel collector recipe rendering. First slice complete with deterministic config plans/YAML for approved content-pack sources, stable receiver/pipeline/resource processor IDs, OTLP exporter configuration, memory limiter/batch processors, source/pack/parser attribution, approval gating for sensitive sources, current `windows_event_log` receiver output with old `windowseventlog` recipe tolerance, persistent file storage for filelog offsets and Windows Event Log bookmarks, persistent exporter queue defaults, receiver support for filelog, syslog, Windows Event Log, OTLP, Splunk HEC, Kafka, and Prometheus, plus syslog TCP/UDP/TLS normalization with cert/key validation, mTLS client CA support, peer attribute capture, audited source identity/allowlist metadata, and nftables edge policy templates for syslog source allowlists and rate limits.
- Render-only tenant API. First slice complete with `POST /api/v1/content-packs/otel-config?tenant_id=...`, active-registry source resolution, operator/admin authorization, explicit source selection, approval-ref enforcement, deterministic `sha256:` `config_version`, JSON plan output, and YAML output for change review.
- Collector config candidate persistence. First slice complete with `content_pack_collector_config_candidates`, exact rendered YAML storage, structured plan JSON, active registry snapshot reference, source IDs, creator subject, status `rendered`, list API, and create API at `POST /api/v1/content-packs/otel-config/candidates?tenant_id=...`.
- Collector config candidate approval. First slice complete with admin-only approval at `POST /api/v1/content-packs/otel-config/candidates/{id}/approve?tenant_id=...`, rendered-only state transition, approver subject/note/timestamp persistence, immutable rendered YAML/config version, and audit logging.
- Edge collector registration/heartbeat. First slice complete with `content_pack_edge_collectors`, collector kind/status/config-version/health metadata, `POST /api/v1/content-packs/collectors?tenant_id=...`, `GET /api/v1/content-packs/collectors?tenant_id=...`, `POST /api/v1/content-packs/collectors/{collector_id}/heartbeat?tenant_id=...`, and registration audit logging.
- Approved-candidate queueing. First slice complete with `POST /api/v1/content-packs/otel-config/candidates/{id}/queue?tenant_id=...`, approved-only transition to `queued`, required caller acknowledgement of the exact `expected_config_version`, reviewed-version-to-approved-version match enforcement, registered/non-disabled target collector enforcement, target collector desired config version update, queue subject/note/timestamp persistence, and audit logging.
- Collector desired-config/apply-result API. First slice complete with `GET /api/v1/content-packs/collectors/{collector_id}/desired-config?tenant_id=...`, `POST /api/v1/content-packs/collectors/{collector_id}/apply-result?tenant_id=...`, queued config YAML fetch, exact `config_version` deployed/failed reporting, candidate deployed/failed timestamps/errors, and collector running config/status updates.
- Rollback queueing. First slice complete with deploy-time superseding of previous deployed configs and `POST /api/v1/content-packs/collectors/{collector_id}/rollback?tenant_id=...` to re-queue a superseded candidate by candidate ID, config version, or latest superseded config.
- Edge collector coverage projection. First slice complete with a tenant telemetry coverage overlay for edge collector count/status, heartbeat freshness, desired-vs-running config mismatch, disabled collectors, and explicit stale/partial gaps.
- Collector-scoped auth. First slice complete with admin token rotation at `POST /api/v1/content-packs/collectors/{collector_id}/token?tenant_id=...`, hashed token storage, token-last-four/issued-at metadata, middleware bypass only for collector self-service paths with collector credentials, and scoped validation for heartbeat, desired-config, and apply-result calls.
- Source-level health coverage projection. First slice complete with a parser-domain "Tenant SIEM source health" overlay that reads edge collector heartbeat health evidence, accepts per-source and receiver health shapes, normalizes common source states, applies one heartbeat freshness window in both coverage and detail APIs, aggregates received/parsed/failure/drop/queue counters, preserves source instance IDs, approval refs, runtime labels such as collect mode/raw retention, and receiver/candidate/proposal IDs, supports paginated/searchable/state-filterable durable runtime-state reads through `GET /api/v1/content-packs/source-health?tenant_id=...&limit=...&offset=...&q=...&state=...`, returns all-matching `totals.by_state` and `totals.metrics` summaries for paginated views, feeds those durable summaries into the tenant coverage matrix when present, includes runtime-state IDs and recommended investigation actions for parser-failed/silent/backpressured/stale states, and refuses to claim health when collectors do not report source-level evidence.
- Durable source runtime-state persistence. First slice complete with `content_pack_source_runtime_states`, source runtime storage methods, heartbeat-time upsert of normalized source evidence, persisted-state preference in `GET /api/v1/content-packs/source-health?tenant_id=...`, and coverage overlay preference for durable source rows before falling back to raw collector heartbeat evidence.
- Durable local connector proposal persistence. First slice complete with `content_pack_source_proposals`, node service-ingest upsert from agent connector proposals, bank-safe status normalization (`auto_eligible` vs `approval_required`), preserved manual decision states, and `GET /api/v1/content-packs/source-proposals?tenant_id=...&q=...&status=...` for searchable/status-filterable UI/API consumers.
- Bank-safe auto-connect policy. First slice complete in `internal/connectordiscovery` and tenant event filters with default low-risk-only auto-connect, explicit medium/high-risk widening, program-level approval/block lists, dedicated `GET/PUT /api/v1/tenants/{id}/connector-policy`, heartbeat delivery to `connector_auto_connect_policy.v1` agents, node-agent policy caching, and proposal labels that explain policy decision and risk class before anything is collected.
- Source proposal decision API. First slice complete with admin-only `POST /api/v1/content-packs/source-proposals/{id}/approve?tenant_id=...` and `POST /api/v1/content-packs/source-proposals/{id}/reject?tenant_id=...`, audit logging, approval note persistence, rejection/privacy-block persistence, and guarded transitions that do not overwrite already approved/rejected decisions on rediscovery.
- Source proposal coverage projection. First slice complete with a parser-domain "Tenant SIEM source proposals" overlay that counts proposed, auto-eligible, approval-required, approved, rejected, privacy-blocked, stale, and unknown proposal states while explicitly keeping proposal truth separate from deployed source health.
- Proposal-to-runtime-state projection. First slice complete with service-ingest proposal upserts and source-proposal approve/reject decisions writing `SourceRuntimeState` records in pre-collection states (`proposed`, `approval_required`, `approved`, `privacy_blocked`, `unsupported`, `stale`) when the runtime-state store is available; source-health coverage now counts these states separately from collector/parser health.
- Approved local proposal-to-agent-source apply. First slice complete with `GET /api/v1/nodes/{node_id}/log-sources/approved`, agent-only node-CN authorization, approved/local/file/source filtering, collect-mode filtering so empty/legacy, `collect_raw`, and `collect_parsed` approvals are deployable, proposal trace labels, node-agent startup merge/dedupe before auto-discovered sources, heartbeat delivery for `connector_approved_sources.v1` agents, and additive telemetry collector start without disrupting existing collectors. Node-agent `collect_parsed` strips raw messages before `/api/v1/logs` transport while retaining parsed fields, labels, severity, source, and timestamps. Control-plane log ingest now uses approved-source labels to upsert durable source runtime state to `collecting` with event/parsed counters, so local collection is reflected in source-health coverage.
- Approved proposal-to-OTel render/apply workflow. First slice complete with `source_proposal_ids` on render/candidate-create requests, tenant-scoped proposal lookup, approved-only enforcement, collect-mode enforcement that allows `collect_raw` and first-pass `collect_parsed` while rejecting metadata-only/observe-only/disabled approvals before config rendering, proposal `source_id` resolution against the active content-pack registry, proposal ID injection as the renderer approval ref, local file proposal mapping to `otel_filelog` mode when the pack supports it, candidate-create/queue/rollback-queue projection of source runtime state to `config_rendered` with collector/config/proposal evidence, apply-success projection to `deployed`, and apply-failure preservation of `config_rendered` with the collector error.
- Approved proposal source-hint resolution. First slice complete with exact-source resolution first, then unique active-registry matching by source/parser labels, aliases, product/vendor, metadata, and parser/source prefixes; ambiguous matches fail closed and require an explicit `content_pack_source_id`.
- WEF/WEC deployment runbook. First slice complete in `docs/siem-wef-wec-deployment-runbook.md` with WEC host prep, source-initiated Subscription Manager GPO, canary subscription XML, Control One OTel candidate review/apply, validation, operations, rollback, and troubleshooting.
- Detection-as-code IR. First slice complete in `internal/detections` with nested boolean expressions, exact/contains/prefix/suffix/existence/set/numeric predicates, replayable match results, and common Sigma import for field selections, keyword selections, modifiers, field mapping, and boolean conditions. Content-pack manifests can now load referenced Sigma files through `LoadManifestDetections`, pin rule identity/severity/tags/risk score to manifest metadata, reject traversal/missing files, evaluate loaded rules against normalized events, and simulate source-linked detections against golden normalized sample events with evaluation/match reports. Signed offline content-pack sync includes detection replay reports and quarantines packs with missing or invalid detection files. Live event fanout now evaluates enabled active signed-pack detections by `content_pack_source_id` and creates deduped `content_pack_detection` alerts with evidence citations and risk score context. `/api/v1/content-packs/detections?tenant_id=...` now lists active-registry detections with source linkage, runtime load status, effective override state, risk score, and temporal metadata for operator review; `/api/v1/content-packs/detections/replay?tenant_id=...` reruns signed-golden detection replay on demand; `/api/v1/content-packs/lifecycle` enables/disables signed packs with expected-snapshot protection, replay gating on enable, and audit logging; `/api/v1/content-packs/detections/overrides` persists audited per-detection `enabled`/`disabled`/temporary `suppressed` overrides honored by live fanout; `content_pack_detection_artifacts` persists compiled detection artifacts by active registry snapshot for runtime fallback when the active archive cache is unavailable; manifest-declared threshold windows with group-by fields plus temporal suppression now run in both replay and live fanout; manifest-declared ordered sequences with normalized-field step predicates, grouping, windows, and suppression use the same replay/live evaluator path; and manifest-declared unordered joins now support grouped, windowed, suppressible co-occurrence detections over normalized-field predicates.
- Bank security starter pack. First slice complete in `internal/contentpacks.BankSecurityStarterPack` with 26 named firewall, WAF, IAM, EDR, DNS, proxy, mail, and private-access audit sources over CEF, LEEF, JSON carrier, and first-pass vendor-semantic parsers, each with approval-required metadata, replayed golden samples, and linked starter detections. The pack includes eight ATT&CK-tagged Sigma detections with explicit risk scores for denied network traffic, WAF exploit blocks, IAM authentication failure bursts, EDR malware alerts, DNS query bursts, mail threats, suspicious PowerShell script blocks, and private-access policy changes. Fortinet FortiGate, Palo Alto PAN-OS, Cisco ASA, F5 BIG-IP ASM/WAF, Imperva WAF, and Cloudflare WAF now have first-pass vendor-semantic CEF parser profiles with denied/blocked replay samples that fire the network-denied and WAF exploit detections while clean samples stay quiet; Active Directory Security, Windows Sysmon, and Windows PowerShell have first-pass EventData profiles with WEF/windows-event collector recipes and replayed golden samples, and encoded PowerShell replay fires the script-block detection.
- IAM starter parser update. Okta System Log and Microsoft Entra ID now have
  first-pass JSON semantic profiles that normalize authentication event code,
  action, outcome, user, and source IP fields with replayed samples.
- Normalized security schema contract. First slice complete in `internal/securityschema` with a Control One schema identity, deterministic field dictionary, ECS and UDM alias metadata, first-pass OCSF object/class/category aliases, nested/dotted lookup, type/IP validation, tests for Windows authentication fields and schema anchors, parent process command-line alias support, UDM `metadata.event_type` derivation for common authentication/network/DNS/process events, default content-pack sample replay enforcement with an explicit legacy opt-out, live event-ingest validation for known normalized top-level and parser-provided fields, and an operator/content-author reference at `docs/security-schema-field-reference.md`.
- SIEM coverage UI. First slice complete with `/security/siem`, sidebar navigation, API client bindings for connector policy/proposals/source health/edge collectors/OTel config candidates/SOC cases/SOC notes, proposal/health/deployment KPIs, status-filtered proposal review, node-scoped proposal-to-health matching, approve/reject/privacy-block actions, approval collect-mode selection, deployable approved-proposal-to-candidate rendering, tenant-scoped exact rendered YAML/source-plan review before candidate approval, enforced reviewed-config-version acknowledgement, expected-config-version acknowledgement before queueing, candidate approve/queue actions, searchable/paginated source runtime metrics plus source instance/approval/collection-mode/raw-retention evidence labels, inline source-health drilldown, degraded source-health-to-SOC-case investigation handoff through `POST /api/v1/content-packs/source-health/investigate`, structured source-runtime-state citations extracted as SOC evidence refs, a recent `siem_source_health` case list with source/parser/error/evidence-ref/export context, cited analyst-note capture back into the SOC case, explicit `include_notes=true` case-list hydration for persisted latest notes, and tenant connector policy editing.
- Remaining: expand UI beyond the first coverage/proposal/policy route, add durable active-pack sync receipts beyond snapshot history, expand live receiver/source health metrics beyond the first OTel/Alloy/Vector/Fluent-compatible Prometheus scrape, expand grok pattern parity and vendor-semantic parser packs, replace starter carrier profiles with production vendor-semantic parser packs, and wire parser replay into pack upgrade/quarantine workflows.

### Phase 1: OTel Edge Collector Contract

Goal: reuse OTel for receivers while keeping Control One semantics.

Build:

- Control One managed OTel collector profile.
- Collector registration and heartbeat API. First slice complete with tenant-scoped collector records, OTel/Alloy/Fluent Bit/Vector/node-agent kind tracking, status, desired/running config versions, last heartbeat, last error, and raw health evidence.
- Collector scoped authentication. First slice complete with per-collector rotated tokens for self-service heartbeat, desired-config, and apply-result calls while keeping registration, queueing, rollback, and token issuance under human RBAC.
- Generated OTel configs for filelog, syslog, Windows Event Log, OTLP, Splunk HEC, and Kafka. First renderer slice complete in `internal/contentpacks`; tenant API exposure, persisted candidates, node-agent wrapper config application, and optional managed collector process supervision are complete.
- Rendered config candidate persistence for approval/apply workflows. First persistence, admin approval, queue-to-collector-desired-config, desired-config fetch, apply-result, rollback queueing, node-agent config wrapper, external reload, and managed-process supervision slices are complete; richer reload/process policy hardening remains pending.
- Health ingestion for receiver status, queue depth, drops, lag, and exporter errors. First raw health evidence capture, collector-level coverage projection, source-level coverage projection, durable source runtime persistence, wrapper config-level heartbeat, managed process heartbeat, and OTel/Alloy/Vector/Fluent-compatible Prometheus receiver metric scrape are complete, including OTel `_total` counter variants and non-zero fractional Vector buffer utilization as backpressure; broader receiver/source metric coverage remains pending.
- Local connector proposal ingestion. First slice complete with durable proposal upsert from node service inventory plus admin approve/reject/privacy-block decisions. Approval decisions now persist `collect_mode`; approved local file-log proposals now have node-agent startup fetch plus heartbeat hot-add for raw and parsed-only local collection, approved raw and parsed-only proposals can be used directly as OTel render/candidate inputs, proposal review supports server-side pagination plus search/status filters and `summary.by_status` totals, the tenant coverage matrix consumes those summary totals instead of sampled proposal rows, and discovery now defaults to low-risk-only auto-connect unless persisted tenant policy delivered through heartbeat widens it.
- Config versioning and rollback. First queue/apply/rollback API slice complete for OTel edge collectors, including reviewed-version acknowledgement at approval time and expected-version acknowledgement at queue time.

Acceptance:

- A syslog source can be proposed, approved, rendered into OTel config, deployed, rolled back, and shown as collecting or failed. Render, rendered-config approval, collector registration, collector-scoped auth, desired-config queueing/fetch, node-agent wrapper config apply, managed collector process supervision, apply-result reporting, rollback queueing, heartbeat evidence capture, collector-level coverage truth, source-level health projection, durable per-source state persistence, source proposal approval, approved proposal-to-OTel candidate rendering, approved local file-log startup/hot-add apply, and first OTel/Alloy/Vector/Fluent-compatible receiver metric scrape have implementation coverage; broader receiver/source metrics remain.

### Phase 2: Parser Runtime MVP

Goal: parse and normalize first production source packs.

Build:

- JSON, syslog, CEF, LEEF, regex/grok, kv/logfmt, timestamp, field_map, redact, OCSF map, ECS alias stages.
- Golden test harness. First slice complete in `internal/contentpacks`.
- Parser metrics.
- Raw preservation and replay path.

First packs:

- Linux auth/syslog/auditd.
- Windows Security/Sysmon/PowerShell.
- NGINX/Apache/HAProxy/IIS.
- PostgreSQL/MySQL/MSSQL.
- Fortinet/Palo Alto/Cisco firewall syslog.

Acceptance:

- Every advertised parser has sample logs and golden normalized outputs.

### Phase 3: Policy-Gated Auto-Connect

Goal: make "auto-detect/auto-connect" bank-safe.

Build:

- Source approval policy engine.
- Sensitivity and volume budgets.
- Allow/deny path rules.
- Redaction policy binding.
- UI/API for approve/deny/collect metadata/collect parsed-only/collect raw. First UI/API slice complete for approve/reject/privacy-block, persisted approval collect mode, node-agent local parsed-only collection with raw message omission, first-pass OTel parsed-only rendering with raw body overwrite, proposal-derived runtime proof labels for metadata-only/observe-only/disabled approvals, fail-closed deployment for metadata-only/observe-only/disabled approvals, approved-proposal-to-collector-candidate rendering, exact rendered candidate review, reviewed `sha256:` acknowledgement, expected `sha256:` acknowledgement before candidate queueing, candidate approve/queue, and policy editing; richer parser-aware OTel recipes and redaction certification remain pending.
- Agent and edge collector config sync.

Acceptance:

- Low-risk infra logs can auto-connect.
- Temenos/Flexcube/WebLogic/IBM MQ/DB audit logs remain proposal-only unless approved.

### Phase 4: Coverage Truth UI and API

Goal: make truth obvious to SOC, sales, and CISO users.

Build:

- Coverage API over nodes, sources, collectors, packs, parsers, and detections.
- Control Room coverage states.
- Connector proposal workflow. First `/security/siem` review/decision/render-to-candidate slice complete with paginated server-backed proposal review and fleet-level status-summary KPIs.
- Source health detail panel. First `/security/siem` source-health table complete with server-side search, state filters, pagination, runtime-summary KPIs that are not limited to the visible page, and an inline per-source drilldown for identity/config/parser/timestamp/error/metric/label evidence.
- Parser failure investigation entry point. First slice complete: degraded source-health rows carry recommended actions, `POST /api/v1/content-packs/source-health/investigate` opens a durable SOC case from the cited runtime-state evidence, structured runtime-state citations are exposed as SOC evidence refs for export and notes, and `/security/siem` lists recent `siem_source_health` cases with the cited source/parser/error context plus hydrated persisted notes and inline cited-note capture so operators can keep working the handoff after creation.

Acceptance:

- The UI never shows an unsupported or parser-failed source as protected.

### Phase 5: Detection-as-Code

Goal: make content testable, portable, and reviewable.

Build:

- Native Control One detection IR.
- Sigma import path.
- Rule tests with normalized sample events.
- Replay/simulation.
- Threshold/group-by/suppression windows, ordered sequences, unordered joins,
  and manifest risk scores are implemented.
- ATT&CK and CVE tags.

Acceptance:

- A detection can be reviewed, tested, enabled, disabled, simulated, and audited from content pack to alert.

### Phase 6: Bank Integration Packs

Goal: make sales pilots credible.

Build:

- Bank network/security appliance packs.
- IAM/AD/Entra/Okta packs.
- EDR/DNS/proxy/mail/WAF/load balancer packs.
- DB/app server packs.
- Private-access audit packs for NetBird, OpenZiti, and Headscale.

Acceptance:

- At least 20 named enterprise/security sources work through syslog/API/OTel with parser coverage truth.

### Phase 7: HA, Airgap, and Compliance Hardening

Goal: pass bank architecture review.

Build:

- Signed offline content factory.
- Pack import/export receipts.
- HA reference architecture.
- Backup/restore and failover drills.
- Capacity benchmark harness.
- Immutable audit/evidence export.
- SAML/OIDC, SCIM, MFA, break-glass, and segregation-of-duties controls.

Acceptance:

- A bank can operate disconnected, import signed updates, roll back bad content, and prove who enabled each source and rule.

## Suggested GitHub Issue Breakdown

Master epic:

- SIEM content pack and OTel edge collector architecture.

Child issues:

- Content pack manifest and validator.
- Parser runtime MVP.
- OCSF primary mapping and ECS export aliases.
- OTel collector registration/config rollout.
- Syslog/CEF/LEEF edge intake.
- Windows Event Forwarding/WEC intake plan.
- Source policy and approval workflow.
- Coverage truth API.
- Coverage truth UI.
- Detection IR and Sigma import.
- Golden test harness and parser replay.
- Signed offline content import/export.
- First Linux/Windows/webserver/DB packs.
- First firewall/IAM/EDR/DNS/WAF packs.
- Collector and parser benchmark harness.

## Acceptance for the Master Epic

- A new source can be represented as a signed content pack with source manifest, collector recipe, parser, samples, golden normalized events, detections, license metadata, and provenance.
- Control One can generate and deploy node-agent or OTel edge collector config from that pack. OTel generation, candidate persistence, candidate approval, collector registration, collector-scoped auth, desired-config queueing/fetch, node-agent wrapper config apply, managed collector process supervision, apply-result reporting, rollback queueing, heartbeat capture, OTel/Alloy/Vector/Fluent-compatible receiver metric scrape, and durable source health persistence have first implementation coverage; broader receiver/source metrics are still pending.
- The system can prove whether the source is discovered, proposed, approved, collecting, parser-healthy, parser-failed, silent, unsupported, or privacy-blocked.
- Raw events are retained or referenced according to policy.
- Normalized events map to Control One canonical fields and OCSF, with ECS aliases for export.
- Detections can be tested and simulated before enablement.
- Pack install, enablement, rollback, source approval, and config rollout are audited.
- High-risk bank systems are never silently collected.

## References

- OpenTelemetry Collector receivers: https://opentelemetry.io/docs/collector/components/receiver/
- OCSF schema browser: https://schema.ocsf.io/
- Elastic package spec: https://github.com/elastic/package-spec
- Elastic Common Schema: https://www.elastic.co/docs/reference/ecs
- Sigma rule format: https://sigmahq.io/docs/basics/rules.html
- Logstash grok patterns: https://github.com/logstash-plugins/logstash-patterns-core
