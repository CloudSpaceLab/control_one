# Bank Sales Go-Live Issue Log

Date: 2026-05-27

Scope: current `control_one` working tree plus 2026 public vendor docs. This log treats code as truth: catalog entries, roadmap notes, and UI copy are not counted as shipped ingestion/connectivity unless there is an agent/control-plane path behind them.

GitHub trackers:

- Final bank SIEM production masterplan: https://github.com/CloudSpaceLab/control_one/issues/212
- Bank/on-prem go-live umbrella: https://github.com/CloudSpaceLab/control_one/issues/210
- SIEM content packs and OTel edge collector master epic: https://github.com/CloudSpaceLab/control_one/issues/211

Closeout update: GitHub issue #212 was updated from
`docs/bank-siem-production-masterplan.md` and closed on 2026-05-29 after the P0
blocker set in this log reached closed status in the local working tree.
Follow-up production-blocker audit on 2026-05-29 also closed stale duplicate
umbrellas #210 and #211.

Live E2E closeout update: browser and server validation against
`control-one.cloudspacetechs.com` first found a stale 2026-05-21 artifact on
2026-05-29. The go-live branch was then pushed in batches, the current artifact
was deployed on 2026-05-30, and GitHub Actions run `26679443510` plus the
paired CI runs for `5e63530` completed successfully. Current containers were
recreated from the new images, `/healthz` returns `ok`, the SIEM source-health
and investigation UX paths load in the browser, and Doris migrations through
`0004_dashboard_analytics_tables.up.sql` are applied. Issue #212 can remain
closed for the P0 software blockers; a target bank install still needs the
customer-environment HA/DR, sizing, backup, and exposure-acceptance gates in
the runbooks.

Detailed implementation plan: `docs/siem-content-pack-architecture-plan.md`
Final execution masterplan: `docs/bank-siem-production-masterplan.md`

## Executive Decision

Do not integrate Tailscale now. Do not duplicate VPN, WireGuard control planes, or microsegmentation engines inside Control One.

For bank/on-prem sales, Control One should own intelligence, evidence, orchestration, approvals, remediation receipts, and coverage truth. Private access should be delivered through out-of-box provider bundles and adapters:

1. NetBird first for self-hosted WireGuard mesh, identity-aware access policies, peer groups, routing peers, and private subnet access.
2. OpenZiti for high-security app-private access where services should be dark and reachable only through identity-bound services/tunnelers.
3. Headscale as a limited/self-hosted tailnet option for teams already standardized on Tailscale clients and willing to operate a simpler control plane.

Control One should ingest private-access provider inventory/audit/routing/policy state, reconcile it against discovered exposed services, and propose/verify changes. It should not become the packet plane.

## What We Currently Own

| Area | Current ownership | Evidence | Sales-readiness read |
| --- | --- | --- | --- |
| Agent event spine | Strong. `eventstream` fans procmon, netflow, fileaccess, dbquery, log-spike, bastion, and scanner events into gzipped NDJSON to `/api/v1/events/ingest`. | `internal/eventstream/stream.go`, `internal/eventstream/batcher.go`, `cmd/nodeagent/main.go` | Keep. This is a differentiator versus manual SIEM connectors when paired with auto-discovery. |
| Process telemetry | Local process exec/exit/usage via `procmon`, enabled in balanced/forensic profiles. | `internal/procmon/collector.go`, `cmd/nodeagent/main.go` | Keep. Add stronger Windows defaults/testing before bank pilots. |
| Connection telemetry | Host connection lifecycle with Linux `/proc` polling, Windows IP Helper, Darwin `lsof`; smart summary filtering. No eBPF backend file exists in tree despite comments. | `internal/netflow/collector*.go`, `internal/netflow/dispatcher.go` | Keep. Add eBPF/ETW-grade fidelity and loss accounting. |
| File access telemetry | Linux auditd-tail only; non-Linux collector is a stub. Requires auditd watches configured by operator. | `internal/fileaccess/collector_linux_auditd.go`, `internal/fileaccess/collector_other.go` | Keep, but mark partial. Add policy-driven watch deployment and Windows ETW/Sysmon/WEF paths. |
| DB query telemetry | Postgres/MySQL/MSSQL polling of stats/DMVs; manual DB targets; Mongo marked unsupported. | `internal/dbquery/collector.go`, `internal/dbquery/scrape.go`, `internal/dbquery/scrape_mssql.go` | Keep. Add credential vaulting, auto target discovery, Oracle/Db2, TLS/least-privilege templates. |
| Log collection | File, journald, Windows EventLog, macOS unified log collectors. Collection is opt-in; when `collect_logs` and `auto_discover_log_sources` are enabled, the agent adds auto-eligible local log sources from observed listening services. Approved local file-log proposals are fetched at startup and hot-added from heartbeat responses for capable agents. | `internal/telemetry/telemetry.go`, `internal/telemetry/logs/*`, `internal/connectordiscovery`, `cmd/nodeagent/connectors.go`, `cmd/nodeagent/heartbeat.go`, `controlplane/internal/server/knowledge_graph.go`, `controlplane/internal/server/heartbeat.go` | Keep. This is now a real proposal-to-collection bridge for local file logs, but still not broad SIEM connector parity. |
| Log parser coverage | Nginx, Apache, HAProxy, MySQL, generic, web JSON, default. App catalog has many profiles, but most use generic parser and are not auto-collected. | `internal/telemetry/logs/formatter_*.go`, `internal/appcatalog/catalog.go` | Major gap versus SIEM competitors. |
| App/service discovery | Package inventory, service/listener inventory, app catalog purpose inference, webserver inventory. | `cmd/nodeagent/inventory.go`, `cmd/nodeagent/services.go`, `internal/appcatalog/catalog.go`, `internal/webservercontrol/manager.go` | Keep. This is the base for auto-detect/auto-connect. |
| SIEM event ingestion | Closed-world event contracts, validation, Doris/Postgres fanout, anomaly investigation hooks. | `controlplane/internal/server/events_ingest.go`, `controlplane/internal/doris/migrations/*.sql` | Keep. Add schema versioning, OCSF/ECS/UDM mapping, HA storage defaults.
| Web request enrichment | Log ingest derives `log.line` and `web.request`, including trusted proxy handling and IP behavior enrichment. | `controlplane/internal/server/telemetry.go` | Keep. This is better than generic log tailing for web-facing bank apps. |
| Detection/rules | Port/log rules and sliding-window correlation. UI has rule packs, but backend engine does regex/count/window, not a full detection-as-code engine. | `controlplane/internal/server/rules.go`, `controlplane/internal/correlation/engine.go`, `ui/src/lib/rulePacks.ts` | Partial. Needs signed content packs, Sigma-like tests, simulation, ATT&CK mapping. |
| Alerts/risk notables | Alert lifecycle, dispositions, risk notable aggregation, heuristic MITRE mapping. | `controlplane/internal/server/alerts.go`, `controlplane/internal/server/risk_notables.go` | Keep. Add enterprise case/SOC workflow depth. |
| Patch management | Direct/proxy/airgapped job types exist; agent runs `apt`, `dnf`, `yum`, or `winget`; approvals/maintenance windows, dry-run plans, canary/wave dispatch, package allow/deny policy, post-patch inventory refresh, and per-node action-plan receipts now exist. | `cmd/nodeagent/patch_exec.go`, `controlplane/internal/server/patch.go`, `controlplane/internal/server/patch_action_plans.go`, migrations `0083`, `0087`, `0092`, `0128` | Stronger, still partial. Needs richer reboot/rollback rules, preflight drain hooks, and vulnerability rescan automation beyond package inventory refresh. |
| CVE/vulnerability | Offline signed vulnerability feed matching against node OS packages and app dependencies; exact and explicit version-range evidence; KEV/EPSS/CVSS fields; OSV/GHSA/NVD/CISA feed factory; post-inventory vulnerability refresh; patch plan tool is proposal-only. | `controlplane/internal/offlinebundle/vulnerability_feed.go`, `controlplane/internal/server/vulnerability_feed_import.go`, `controlplane/internal/vulnfeedfactory`, migrations `0106`, `0127` | Stronger. Container-runtime SBOM coverage and deeper vendor advisory expansion remain post-P0. |
| AI investigation | Tool loop with node docs, alerts, packages, vulnerabilities, patch plans, logs, event query, root-cause findings; durable AI investigations/proposals. | `controlplane/internal/server/ai_investigation_tools.go`, migration `0093` | Keep. This is core positioning. Needs more connectors and deterministic evidence coverage. |
| AI LogFixer | Event bridge creates proposal-only AI LogFixer runs; agent can execute configured CLI when approved/configured. Node-local AI LogFixer actions now attach to the unified action-plan contract and mirror completion into generic receipts. | `controlplane/internal/server/ai_logfixer.go`, `controlplane/internal/server/ai_logfixer_action_plans.go`, `cmd/nodeagent/ai_logfixer_exec.go`, migrations `0112`, `0128` | Keep as optional integration. Do not oversell as generic app/DB auto-remediation yet. |
| Webserver remediation | Inventory/plan/apply/rollback/receipts for nginx/apache/lighttpd/tomcat/HAProxy, with planning stubs for IIS/Caddy/Envoy/Traefik/enterprise app servers. Webserver actions now attach to the unified action-plan contract and mirror completion into generic receipts. | `internal/webservercontrol/manager.go`, `cmd/nodeagent/webserver_exec.go`, `controlplane/internal/server/webserver_action_plans.go`, migrations `0094`, `0128` | Keep. Strong pattern for application-level safe remediation; still needs broader enterprise server execution coverage. |
| Host firewall | Inventory and heartbeat-driven rule add/delete for Linux firewalls and Windows `netsh`. Firewall enforcement jobs now create unified action plans and heartbeat receipts. | `cmd/nodeagent/firewall*.go`, `internal/firewall/*`, `controlplane/internal/server/network_security.go`, `controlplane/internal/server/firewall_action_plans.go`, migration `0128` | Retain as a remediation primitive. Do not build fleet VPN/microsegmentation here. |

## The Key Correction

Control One has a rich app catalog and the beginning of an auto-discovery connector layer, not a rich SIEM connector ecosystem yet.

Evidence:

- `telemetry_prefs.collect_logs` defaults to false.
- `telemetry_prefs.auto_discover_log_sources` now defaults to true, but it only matters after log collection is enabled.
- `PrepareSources(nil)` is still tested to return empty: catalog presets do not autoload inside the telemetry package.
- The new pre-pass in `cmd/nodeagent/connectors.go` only auto-adds locally observed, app-catalog-backed, auto-eligible sources.
- Banking and high-risk enterprise log profiles like Temenos, Flexcube, Finacle, IBM MQ, WebLogic, and IIS are explicitly tested not to autocollect.
- `NewCollector(type=auto)` only selects `file` when paths are present. There is no syslog/CEF/LEEF/OTLP/Splunk HEC/cloud event receiver.

The architectural idea can still be "skip manual connector setup," but the missing product is an auto-discovery to proposed-connectors to approved-auto-connect pipeline.

## Competitor Reality

Competitors win bank SIEM/observability pilots by showing two things Control One does not yet show:

1. A large, named connector/parser ecosystem:
   - Microsoft Sentinel ships Content Hub data connectors including CEF, Syslog, Custom Logs, and many vendor API connectors.
   - Elastic documents 300+ turnkey integrations across security and observability.
   - Splunk add-ons bring source types, inputs, CIM mappings, and dashboards.
   - Google Security Operations documents default parsers for SYSLOG, JSON, KV, XML, CEF, and LEEF log types.
   - OpenTelemetry Collector already has receivers for OTLP, syslog, Windows Event Log, Splunk HEC, Kafka, AWS S3/Kinesis, Azure Event Hub, Prometheus, MySQL, PostgreSQL, SQL Server, Oracle DB, NGINX, HAProxy, Netflow, SNMP, vCenter, and more.

2. Operational proof:
   - Connector health, lag, dropped events, parser success rate, coverage reports, raw retention, replay, and migration/import paths from existing SIEM stacks.

Control One's counter-positioning should be: "we auto-discover what is running, propose the right collector/parser/remediation path, and prove coverage," not "we already have hundreds of connectors."

## Issue Log

### LIVE-E2E-001: Production Deploy Contains Old Console/API Artifact

Priority: P0

Status: Closed for P0 go-live on 2026-05-30. The original live validation on
2026-05-29 correctly found that production was still running the 2026-05-21
artifact. The closeout commits were pushed to `main`, the deploy workflow
`26679443510` succeeded for `5e63530`, and SSH validation showed
`deploy-controlplane-1` and `deploy-console-1` recreated from the current
images at 2026-05-30 08:41 UTC.

Original live browser/API evidence:

- `/console/security/siem` renders a blank authenticated main panel.
- The deployed sidebar exposes `Observability` and `Coverage`, not the new
  `SIEM coverage` operator workflow.
- Authenticated calls to the new P0 APIs return 404:
  `/api/v1/content-packs/source-proposals`,
  `/api/v1/content-packs/source-health`,
  `/api/v1/tenants/{tenant_id}/connector-policy`,
  `/api/v1/content-packs/otel-config/candidates`,
  `/api/v1/content-packs/edge-collectors`,
  `/api/v1/private-access/exposure/findings`, `/api/v1/action-plans`, and
  `/api/v1/siem/imports`.
- The live Coverage page still reports raw-only parser coverage, partial
  remediation/cases/vulnerability coverage, and static milestone guidance.
- The live Observability page is a guided setup/mock surface with hard-coded
  `payments-api` examples, not the source proposal/source-health/OTel candidate
  workflow.

Closeout evidence:

- `/healthz` returns `ok` publicly and from the server.
- The live SIEM page loads source-health rows for both agent eventstream nodes
  with nonzero accepted/parsed counters, plus durable-spool rows.
- Investigate entity pages now load fresh Doris timeline facts, structured raw
  event rows, and current `last_seen` evidence instead of stale capped pages.
- Doris contains the expected post-reset analytic tables, including
  `telemetry_logs`, `security_events`, `rule_trigger_log`,
  `telemetry_metrics_1m`, `unique_counters`, and `threat_observations`.
- 2026-06-06 follow-up: commits `50e8b56` and `ad27e24` replaced the
  Observability guided setup mock path with live tenant signals from nodes,
  coverage matrix rows, webserver inventory, and content-pack source health,
  then cleaned duplicate webserver labels such as `nginx nginx` and duplicate
  version prefixes.
- Deploy `27069180934` and CI runs `27069180933` plus `27069180929` attempt 2
  succeeded for `ad27e24`. A production browser reload of
  `/console/observability?verify=ad27e24` showed the `default live stack`, no
  `payments-api` copy, no reference-blueprint fallback, nginx/webserver rows,
  and parser/source-health rows. The page called `/api/v1/nodes`,
  `/api/v1/coverage/matrix`, `/api/v1/webservers`, and
  `/api/v1/content-packs/source-health` with HTTP 200 responses and no console
  warnings/errors; only Cloudflare RUM aborts were observed.

Exit criteria:

- Met: pushed the go-live implementation and duplicate closeout fixes to
  `main`.
- Met: CI/deploy succeeded for the current artifact and containers were
  recreated.
- Met: Doris migrations apply cleanly through version 4 after the analytic
  store reset.
- Met: browser smoke verified SIEM source health and investigation lifecycle
  paths against the live deployment.
- Met: browser smoke verified the Observability page is now backed by live
  fleet/source-health/coverage signals instead of hard-coded demo data.

### LIVE-E2E-002: Live Environment Health and Risk Signals Are Not Go-Live Clean

Priority: P0

Status: Closed as a software P0 blocker for the current 2026-05-30 deploy after
Doris reset/sizing cleanup, stale artifact replacement, dashboard-table
migration repair, and live browser/API smoke. This shared single-node VPS is
still not a bank HA production reference architecture by itself; target-bank
deployments must run the HA/DR, sizing, backup, and exposure acceptance gates
before customer go-live.

Evidence:

- Host disk is 84% used, free memory was about 132 MiB, and swap usage was
  about 5.9 GiB of 8 GiB during the check.
- Control-plane logs show Doris stream-load memory limit failures, replay sweep
  failures, fleet-health query degradation, and context deadline errors.
- Control Room reports exposure confidence of 36%, 51 exposure gaps, 49 public
  listeners, 0 protected listeners, firewall default-deny count 0, and public
  DB/cache listeners such as MySQL/Postgres/Redis on wildcard addresses.
- Patch posture reports one failed direct patch deployment. The per-node drawer
  then says no nodes received the deployment, which is internally inconsistent
  for operator triage.
- The 24h Control Room view shows top incidents opened on 2026-05-18 and
  2026-05-19 while generated at 2026-05-29, so stale alert/anomaly handling is
  not clear enough for a production SOC view.

Closeout evidence:

- Current public and local `/healthz` checks return `ok`.
- GitHub Actions deploy run `26679443510` and paired CI runs for `5e63530`
  completed successfully.
- Doris FE is healthy, BE is alive, and the current table set/migration ledger
  match the reset schema through `0004_dashboard_analytics_tables.up.sql`.
- Post-deploy container stats were stable on 2026-05-30: control plane about
  29 MiB of 1 GiB, console about 5 MiB of 256 MiB, Doris FE about 639 MiB of
  1.758 GiB, and Doris BE about 1.12 GiB of 3.711 GiB.
- Live browser validation confirmed SIEM source-health counters, entity
  investigation lifecycle freshness, and structured raw-event presentation.
- Remaining threat-feed warnings were external-source 403/429 responses using
  local snapshots, not ingest/query/deploy failures.

Exit criteria:

- Met for the current deploy: Doris ingest/query health restored and missing
  dashboard tables migrated.
- Met for the current single-node profile: resource pressure reduced after
  reset, bounded bucket/tablet count, and container memory caps.
- Met as a product software gate: stale artifact, source-health, investigation,
  and dashboard-query failures are closed.
- Environment gate remains outside this code closeout: a real bank production
  install must attach default-deny/private-access exposure evidence, HA/DR drill
  output, and customer-approved residual-risk signoff.

### LIVE-E2E-003: Doris BE Does Not Fit Current Shared 8 GB VPS Profile

Priority: P0

Status: Closed for P0 go-live on 2026-05-30 after a deliberate Doris analytic
store reset, small-node runtime profile, bounded single-node schema, journal
cleanup, and live API/browser validation. This does not mean Doris should be
used as a raw-first SIEM bucket; it is fit here only as a hot, normalized,
bounded analytic store.

Live validation after the 2026-05-30 reboot showed `doris_be` repeatedly
OOM-killed at the container memory limit while recovering metadata/loading
tablets. Pausing `doris-be` immediately restored host memory headroom and
public `/healthz`/`/console/` responsiveness. Applying the small-node profile,
FE heap cap, and THP `madvise` hotfix reduced FE memory and stopped the
immediate restart loop.

Reset update: the Doris FE/BE analytic volumes were wiped on 2026-05-30 and
recreated from empty volumes. The old BE volume was about 5.8 GiB and the FE
catalog reported 1,567 tablets. Fresh startup reduced BE memory to about
1.3 GiB and the BE reached `Alive=true`; the final single-node schema reduced
the tablet set to 227 tablets with 1 bucket per table/partition.

Closeout update: Doris hot analytic tables were truncated again on 2026-05-30
to create a fresh go-live baseline without replaying stale history. The old
pending Doris retry backlog was archived, terminal Postgres ingest-journal
payloads older than 24 hours were pruned, and `event_ingest_batches` was
vacuum-analyzed. After cleanup, `/healthz` returned `ok`; FE memory was about
615 MiB of 1.758 GiB, BE memory about 1.16 GiB of 3.711 GiB, controlplane about
37 MiB of 1 GiB, one backend was `Alive=true` with `HeartbeatFailureCounter=0`,
and Doris held only fresh post-reset rows from the two live nodes.

2026-06-06 small-fleet correction: the current shared 8 GB demo VPS should not
run Doris as a default dependency. Commit `df668a1` documents the replacement
architecture in `docs/small-fleet-analytics-architecture.md`: Postgres remains
the durable source of truth, Redis carries hot counters/leases/rate state, and a
future embedded SQLite/WAL analytic store should hold bounded connection-history
and top-talker facts for small fleets. Commits `2e013a7`, `57f70cd`, and
`eb73263` make this a deployable mode without removing OLAP support: analytics
mode is selectable, Doris initialization is skipped in `small` mode, fleet
health uses Postgres rollups, raw connection/top-talker endpoints return
successful small-mode envelopes instead of 503s, and the UI now tells operators
that connection-level history is pending while fleet health remains live.

Live 2026-06-06 evidence after deploying the lightweight mode:

- `/opt/control-one/deploy/.env` has `ANALYTICS_MODE=small` and
  `DORIS_ENABLED=false`.
- `deploy-controlplane-1` and `deploy-console-1` are up; `deploy-doris-fe-1`
  and `deploy-doris-be-1` are exited.
- Controlplane logs show `analytics backend selected` with `mode=small`.
- Public `/healthz` returned `HTTP 200` in three follow-up samples at about
  0.61-0.63s after the host settled.
- Authenticated API checks returned:
  `/api/v1/connections` in about 336 ms with
  `source=small-analytics-pending`, `/api/v1/fleet/health` in about 325 ms with
  `source=small-analytics-postgres`, and `/api/v1/connections/top-talkers` in
  about 320 ms with `source=small-analytics-pending`.
- Browser validation on
  `/console/security/network?tab=connections` showed the small-mode notice,
  zero console warnings/errors, and the connections request returning `200`.

2026-06-06 follow-up: the corrected workflow is now on `main` and deploy runs
`27065886666`, `27066201594`, and `27066409247` succeeded with Doris disabled.
The small-fleet architecture pass also made the deployment contract explicit:
`deploy/docker-compose.yaml` keeps Doris FE/BE behind the `olap` profile,
`.env.example` defaults to `ANALYTICS_MODE=small` and `DORIS_ENABLED=false`,
and `deploy.py`, `bootstrap.sh`, and the GitHub production deploy workflow skip
Doris host prerequisites/bootstrap unless OLAP is selected. This preserves the
Doris feature path without letting the demo stack start memory-heavy services by
accident.
The live browser now opens the console without creating `/api/v1/events/stream`
requests, avoiding Cloudflare HTTP/3/QUIC stream noise; alert/rule freshness is
preserved by bounded polling unless a deployment explicitly opts into
`VITE_LIVE_EVENTS_MODE=sse`. Re-testing the Connections toggle returned two
`/api/v1/connections` `200` responses and zero console warnings/errors.

Additional live browser checks on 2026-06-06 loaded Control Room, Servers,
node detail including the Connections tab, SIEM coverage including source
inspection, Access including command policy, Patch posture including the
deployment drawer, and Settings/System health. Control One API calls on those
paths returned `200` and browser console warnings/errors remained at zero.
The Settings/System health tab now reports worker backend `asynq`, status
`Running`, queue depth `0`, and no last-error. Controlplane logs confirm
`worker manager started` with `backend=asynq` and `analytics backend selected`
with `mode=small`.

2026-06-06 alert-review follow-up: commit `fedccb6` fixed the alert resolution
evidence modal so outbound byte context is rendered as an operator-readable
size instead of a raw integer. Deploy run `27066740791` succeeded, both CI runs
`27066740790` and `27066740793` passed, and live browser verification on
`/console/alerts` confirmed the first alert review dialog now shows
`Outbound 32.8 MB`, keeps `Record disposition` disabled until an evidence
reason is entered, makes the expected IP investigation/block/webserver/audit
links available, and produces zero browser console warnings/errors. Control One
API requests for alerts, nodes, fleet health, and correlation rules returned
`200`; the only failed browser network entries were Cloudflare RUM aborts.
Post-deploy server checks showed console/controlplane/redis up, Doris absent,
public `/healthz` at `HTTP 200` in about 0.60s, `analytics backend selected`
with `mode=small`, and `worker manager started` with `backend=asynq`.

2026-06-06 SOC-cases follow-up: live browser audit of `/console/cases` found
first-seen-destination case copy rendering as `first connection to <ip> by`
when the process name was empty, then found the row/detail summary duplicated
the case title after the server-side text cleanup. Commit `7730fca` now omits
the dangling process phrase and sanitizes existing stored SOC case titles,
summaries, and timeline evidence. Commit `a4cea77` now uses the trigger event
type as the secondary/detail summary when title and summary are identical. Deploy
runs `27067443819` and `27067562931` succeeded, and CI runs `27067443815`,
`27067443818`, `27067562929`, and `27067562932` passed. Live verification on
`/console/cases` showed `50` open cases, `50` export-ready cases, zero evidence
gaps, queue rows with human title plus `anomaly.new_destination` secondary text,
clean process-less titles such as `first connection to 20.169.85.72`, and a
selected case detail whose summary is `anomaly.new_destination`. The non-mutating
`Preview export` action returned `200` and rendered `soc-case-export-v1`,
`6 evidence refs / 0 notes`, and export guardrail labels. Browser console
warnings/errors remained zero and the app API calls returned `200`; only
Cloudflare RUM abort noise appeared in the network list.

2026-06-06 compliance/investigation follow-up: commit `497b7357` fixed the
Compliance Evidence upload workflow so `Upload evidence` stays disabled until
the tenant, required title, and evidence type are present, trims the submitted
title, and removes the stale placeholder mojibake. Deploy run `27070034433`
and CI runs `27070034439` and `27070034446` succeeded. Live browser
verification on `/console/compliance?tab=evidence&verify=497b7357` confirmed
the evidence tab loaded for the `default` tenant, the upload button was disabled
with an empty title, became enabled after entering `Access review proof`, and
was not submitted during the non-mutating check. Compliance evidence, tenant,
fleet-health, node, alert, and identity APIs returned `200`; current-page
browser console warnings/errors were zero, no mojibake was present, and only
Cloudflare RUM abort noise appeared.

Commit `efe6df06` fixed the live investigation IP drill-in by rendering empty
or sentinel IP-behavior observation timestamps as `No observations` instead of
fake year-0001 dates, and by hardening reusable panel headers so long action
toolbars wrap below readable titles. Focused `EntityDetail`/`IpLifecyclePanel`
tests, UI lint, and polling-mode production build passed locally. Deploy run
`27070461202` and CI runs `27070461195` and `27070461205` succeeded. Live
browser verification on `/console/investigate/ip/8.8.8.8?verify=efe6df06`
confirmed the `Observed` evidence block says `No observations`, no `1/1/1` or
`0001` timestamp remains, `Connections to/from 8.8.8.8` renders at full width,
and lifecycle actions wrap beneath the title. The investigation APIs for
connections, nodes, entity detail, lifecycle, related entities, enrichment,
IP-behavior profile, and anomalies returned `200`; current-page browser console
warnings/errors were zero and no mojibake was present.

Commit `a89eddfe` fixed the live Access JIT workflow so privileged access is no
longer prefilled as `root`; operators must specify the exact requested access
before `Request access` enables, and command-policy delete icon buttons now have
rule-specific accessible names. Focused `Access` tests, UI lint,
`git diff --check`, and polling-mode production build passed locally. Deploy run
`27071109446`, matrix CI run `27071109418`, and service-backed CI run
`27071109431` attempt 2 succeeded; attempt 1 of `27071109431` stopped in the Go
race/coverage test job without a Go failure message, then the rerun passed the
race/coverage test and Docker image build jobs. Live browser verification on
`/console/access?verify=a89eddfe` confirmed the requested-access field starts
blank, `Request access` is disabled until a value such as `root@prod-db-01` is
entered, and the field was cleared without submitting a production request. The
Access command-policy tab loaded without the previous command-ACL 404s; live
tenant, access-request, command-ACL, node, fleet-health, alert, and identity API
calls returned `200`; public `/healthz` returned `HTTP 200` in about 0.73s;
current-page browser console warnings/errors were zero. The live tenant had no
command-policy rows, so the delete-button accessible-name fix was verified by
unit test rather than by mutating production data.

2026-06-06 mobile/small-mode follow-up: a live authenticated browser sweep at
390x844 covered 26 routes: Control Room, Alerts, Cases, Search & lifecycle, Ask
AI, Servers, Network & exposure, Observability, SIEM coverage, Patch posture,
Coverage, Compliance, Access, Audit log, Settings, Templates, Secrets, Offline
bundle, Tenants, Users, Roles, Jobs, Webserver auto-control, Data security,
Misconduct, and Finacle access. Every route loaded with document-level
`overflowPx=0`, no loading/error copy, zero current-page console warnings or
errors, and zero app network failures; wide tables stayed contained inside
horizontal table scrollers rather than widening the document. During the same
small-mode verification, live `/api/v1/fleet/health` returned
`source=small-analytics-postgres` but exposed a correctness bug: fallback
`ConnsActive` was a cumulative `conn.*` event count, not active connections.
Commit `3db08217` fixed both Postgres fallback and Doris fleet-health SQL to
estimate active connections as `conn.open - conn.close`, clamped at zero, while
preserving byte totals and source labels. Focused fleet-health/Doris tests,
`go vet ./controlplane/internal/server ./controlplane/internal/doris`, and
`GOMAXPROCS=4 go test -short -p 1 ./...` passed locally. Deploy run
`27071972583` and CI runs `27071972591` and `27071972567` succeeded. Live
post-deploy verification on `/console/?verify=3db08217` showed
`/api/v1/fleet/health` returning HTTP `200` in about 330 ms with
`source=small-analytics-postgres` and `ConnsActive` values of `100` and `6`
instead of the previous hundreds-of-thousands event-count values; public
`/healthz` returned HTTP `200` in about 0.61s.

Remaining caveat: the current Asynq worker adapter persists queue envelopes in
Redis, but executable job handlers are still registered in-process by task
name. This is acceptable for the current demo safety posture, but true
restart-resumable production jobs still need serialized job payload handlers.
Manual recovery also showed that Windows-cross-compiled controlplane binaries
should not be used for live deploys until the Go runtime crash is investigated;
the live controlplane was recovered with the Linux-runner-built binary.
GitHub Actions now warns that Node.js 20-based actions will be forced to Node 24
by GitHub starting 2026-06-16 and removed from the runner on 2026-09-16; update
the affected workflow actions before that switch to avoid CI/deploy drift.

Evidence:

- Kernel OOM logs show repeated cgroup and global OOM kills of `doris_be`.
- The old BE volume was about 5.8 GiB with roughly 400k files and 1,567 tablets.
- FE initially reported the backend as `Alive=false` with
  connection-refused/host-unreachable heartbeat errors while BE was pinned near
  its memory limit.
- The host runs multiple non-Control One workloads on the same 8 GB VPS, leaving
  no safe headroom for Doris FE/BE plus app/control-plane services.
- Final post-reset server snapshot showed Doris FE/BE stable, backend alive,
  and fresh ingest landing without replay/backlog errors.

Exit criteria:

- Met for the current single-node go-live profile: the analytics store was
  reset with lower bucket/tablet counts and bounded 30-day hot history.
- Met: THP `madvise`, FE/BE JVM caps, swap disabled, and `vm.max_map_count` are
  enforced before Doris starts.
- Met: routine deploys no longer force-recreate Doris FE/BE; a Doris reset is
  now treated as an explicit analytic-store wipe.
- Met: BE reached `Alive=true`, stayed stable without OOM/restarts under fresh
  ingest, and live browser/API SIEM smoke passed.
- Follow-up for bank-scale production: move Doris to dedicated analytics
  capacity/HA before claiming multi-node bank retention and query concurrency.

Storage strategy correction:

- Single-node/demo Doris migrations now rewrite table and dynamic partition
  bucket counts to `BUCKETS 1` / `"dynamic_partition.buckets" = "1"`;
  they also create a bounded 30-day hot history window so recent replay does
  not fail on missing partitions. HA/bank clusters with `replication_num > 1`
  keep the larger production bucket and retention settings.
- Control One should not behave like a traditional raw-first SIEM. Raw log/event
  storage should be bounded by source policy, retention class, and replay need.
- Terminal event-ingest replay payloads are now pruned after a 24-hour safety
  window instead of being retained indefinitely in Postgres.
- Doris no longer mirrors high-volume typed facts into both the generic
  `events` table and their canonical fact tables (`process_connections`,
  `process_lineage`, `file_accesses`, `db_queries`, `web_requests`). The generic
  `events` table is reserved for high-signal/unspecialized events such as
  anomalies, rules, security events, log spikes, and other non-fact signals.
- Durable analytic storage should prefer normalized security facts, compact
  process/connection/file/query lifecycle events, source-health counters,
  parser-error samples, and time-windowed rollups over storing every redundant
  line forever.
- High-volume repeated events need deterministic coalescing: same source,
  parser, node, event type, entity, outcome, and time bucket should become
  count/range evidence with sample refs, not unbounded duplicate rows.
- Implemented closeout: hot Doris fanout now coalesces identical `log.line`,
  `web.request`, and `web.error` facts within deterministic 20-minute buckets.
  If the same exact log message arrives 1,200 times in 20 minutes from the
  same node/source/program/signature, Doris receives one hot analytic fact with
  `coalesced_count=1200`, first/last-seen timestamps, and capped sample
  timestamps/refs. The short-retention raw telemetry/journal path can still
  retain individual rows for replay/evidence according to source policy, but
  Doris no longer treats redundant messages as 1,200 independent hot facts.
- Source-health event/parsed counters from agent log batches are now written as
  metric deltas for source runtime state, so the UI/API totals accumulate
  across batches instead of being overwritten by the most recent batch. Agent
  log batches without explicit content-pack labels also fall back to entry
  program/source/collector identity so real runtime flow is not invisible.
- Agent eventstream ingest now also projects a `control_one.agent_eventstream`
  runtime source per node with accepted/parsed event-count deltas, so source
  health proves host event flow even when no approved local log source is
  currently active.
- Investigate entity lifecycle now merges Doris timeline facts with Postgres
  alerts/audit/actions and the raw-events tab renders structured rows instead
  of an operator-hostile JSON wall.
- Live UX validation also caught and closed a Doris timeline paging bug: entity
  lifecycle queries now request newest timeline rows first, so capped pages and
  `last_seen` evidence reflect current hot events instead of the oldest slice.
- Fresh Doris volumes now also migrate the dashboard/investigation compatibility
  tables (`security_events`, `telemetry_logs`, rule/metric/unique/threat
  rollups), preventing related-entity and dashboard readers from failing after
  an analytic reset.
- Full-fidelity raw should spill to cheaper archive/object storage when a bank
  requires evidentiary retention; Doris should hold hot searchable facts and
  compressed investigation pivots.

### LIVE-E2E-004: 2026-06-06 Production Audit Reopened Go-Live Risk

Priority: P0

Status: Open for the broader production go-live audit. The specific 2026-06-06
live software blockers from this slice have been deployed to
`control-one.cloudspacetechs.com`, and the shared demo VPS now runs the
lightweight small-fleet analytics profile with Doris disabled. This still does
not make the single VPS a bank-grade HA reference environment by itself; the
remaining go-live gate is continued route-by-route live validation plus
customer-environment HA/DR, backup/restore, exposure, and soak evidence.

Live audit evidence from 2026-06-06:

- Authenticated browser smoke covered Control Room, Alerts, Cases, Search &
  lifecycle, Ask AI, Servers, Network & exposure, Observability, SIEM coverage,
  Patch posture, Coverage, Compliance, Access, and Audit log. All pages loaded,
  but Access emitted 404s for `/api/v1/command-acls` and
  `/api/v1/command-acls?tenant_id=...` while the singular
  `/api/v1/command-acl` endpoint still returned 200. This breaks the privileged
  command-policy surface in production.
- Commit `3503819` deployed the remediation: plural command-ACL routes, compact
  UI `pattern`/`action`/`roles` payload support, UI-normalized backend ACLs, and
  a role selector in the Access command-policy form. The fix is covered by
  `TestCommandACLPluralRouteAcceptsCompactUIPayload`.
- Live `/api/v1/fleet/health` was served from `source:
  "postgres-fallback"` because Doris fleet-health queries are degraded. Before
  the local fallback fix, non-connection rollups inflated active-connection
  counts and returned zero `LastEventAt`, which is misleading for a SOC view.
  Local remediation now counts only `conn.*` rows as active connections and
  carries the latest rollup timestamp in fallback responses.
- SSH host validation showed `doris_be` was OOM-killed on Saturday,
  2026-06-06 at 10:35:50 UTC. The BE container had restarted shortly before the
  audit, swap was disabled, disk was about 84% used, and Doris logs showed
  memory-limit query cancellation. This is an environment/architecture blocker,
  not just a UI bug.
- The current demo/small-fleet architecture response is to avoid Doris as a
  default dependency: `ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, Postgres as
  source of truth, Redis for hot coordination/counters, and a bounded embedded
  SQLite/WAL analytics store as the intended lightweight connection-history
  layer. Live server logs now show `analytics backend selected` with
  `mode=small`.
- Mobile browser sampling at 390x844 found production document-level horizontal
  overflow on core routes: the top-bar theme/profile controls were pushed past
  the right edge. Local shell remediation compacts the top bar on mobile by
  shrinking search/tenant controls and hiding non-critical actions below `sm`.
  A local built preview at `http://127.0.0.1:5175/console/` measured
  `scrollWidth=381` and `clientWidth=381` at 390px after the fix.
- SIEM coverage itself loaded live runtime source-health rows and the Source
  health Inspect panel rendered identity, parser, runtime metrics, and evidence
  labels. One browser console error was still observed for the alert event
  stream: `ERR_QUIC_PROTOCOL_ERROR` on `/api/v1/events/stream`.
- Commit `9bc1d3b` made the live event transport configurable and production now
  deploys polling mode by default. Follow-up browser checks did not create
  `/api/v1/events/stream` requests and the console warning/error count stayed
  at zero.
- Live `/console/cases` follow-up found and closed two SOC packet UX/data-copy
  defects: first-seen-destination cases no longer show a dangling `by` when the
  process is unknown, and identical title/summary cases now use the trigger type
  as the secondary/detail summary. Commits `7730fca` and `a4cea77` are deployed
  and live export preview still returns `soc-case-export-v1` evidence.

Verification completed locally after the 2026-06-06 fixes:

- `GOMAXPROCS=4 go test -short -p 1 ./...` passed.
- `GOMAXPROCS=4 go vet -p 1 ./...` passed.
- `npm run lint`, `npm run build`, and `npm test -- --watch=false` passed in
  `ui/` after the command-ACL and mobile-shell changes.
- Focused backend coverage passed for command-ACL compatibility and honest
  fleet-health fallback counting.
- Focused SOC case coverage passed for first-seen-destination copy cleanup and
  duplicate-title summary fallback; `npm run lint`,
  `npm test -- --watch=false Cases`, and
  `VITE_LIVE_EVENTS_MODE=polling npm run build` passed in `ui/`.

Exit criteria:

- Met: deployed the command-ACL, fleet-health fallback, mobile-shell, live-event
  polling, worker Redis binding, alert evidence formatting, and SOC case copy
  fixes.
- Met: live Access route audit for `a89eddfe` returned `200` for command ACLs
  and removed the default privileged JIT request value.
- Reframe: small-fleet/demo mode should keep Doris disabled; bank-scale OLAP
  must move to dedicated HA analytics capacity and pass a sustained soak window.
- Met for current small mode: fleet-health fallback now estimates active
  connections from opens minus closes, and live values no longer reflect raw
  connection-event volume. Continue separately for bank-scale Doris soak on
  sized infrastructure.
- Met: live mobile sweep at 390px covered 26 authenticated routes with no
  document-level horizontal overflow.
- Met for current deploy: event-stream QUIC noise is avoided by polling mode.
- Continue: keep auditing remaining console routes and safe workflows before
  calling the whole product bank-grade clean.

### C1-SIEM-001: Connector Coverage Truth Dashboard

Priority: P0

Status: Closed for P0 go-live. Tenant coverage now has separate parser-domain overlays for active content-pack registry state, durable source proposals, and source runtime health. The source-proposal overlay counts proposed, auto-eligible, approval-required, approved, rejected, privacy-blocked, stale, and unknown proposal states without treating proposals as deployed/healthy collection. Proposal ingest and admin decisions also project pre-collection `SourceRuntimeState` rows so source-health coverage can show `proposed`, `approval_required`, `approved`, and `privacy_blocked` before collector deployment. Agent local log ingest now projects approved source labels into durable source runtime rows, moving deployed local sources to `collecting` with event/parsed counts once logs arrive. Source-health API/UI now exposes runtime labels, source instance IDs, runtime-state IDs, recommended investigation actions, and approval refs such as collect mode and raw-retention proof so operators can see whether a local source is `collect_raw` or `collect_parsed` without inspecting raw events. First operator UI slice is available at `/security/siem` with proposal counts, policy state, proposal review, source runtime health, source-health-to-SOC-case investigation handoff for parser-failed/silent/backpressured/stale rows, structured runtime-state evidence refs on those SOC cases, recent source-health SOC case visibility with persisted cited analyst-note capture and refresh hydration, edge-collector candidate rollout state, approval collect-mode selection, exact rendered YAML review before candidate approval, enforced `reviewed_config_version` acknowledgement bound to the immutable `sha256:` config version, and queue-time `expected_config_version` acknowledgement so stale or unreviewed candidates cannot be deployed by accident.

Latest slice: source runtime truth now has an explicit `collection_conflict` state for duplicate active collection owners. Node-agent local log collection, approved source proposals, and OTel candidate/deploy evidence all stamp `control_one.collection_owner` plus `control_one.collection_identity`; source-health coverage/API computation detects the same approved source active through more than one owner, labels conflict peers, exposes `collection_conflict` counters/gaps, and allows SOC-case investigation handoff. Explicit migration dual-write labels suppress the conflict so controlled cutovers can be audited without false red status.

Problem: The UI/catalog can imply coverage that ingestion does not actually have.

Build:

- Per tenant/node/app: discovered service, proposed source, active collector, parser, event rate, parser status, last success, lag, dropped count, raw retention status.
- Explicit states: `discovered`, `proposed`, `approved`, `collecting`, `parser_failed`, `silent`, `unsupported`, `privacy_blocked`.
- Sales demo view: "what Control One automatically found and what it is safely collecting."

Acceptance:

- A bank operator can see why Temenos/Flexcube/WebLogic/IBM MQ is not collecting yet.
- Empty log sources never appear as healthy coverage.

### C1-SIEM-002: Auto-Discovery to Connector Proposal Engine

Priority: P0

Status: Closed for P0 go-live. `internal/connectordiscovery` now converts observed services/package hints into local log connector proposals. The node agent auto-adds eligible local log sources when `collect_logs` is enabled and posts connector proposals with service inventory. The control plane stores both `agent.connector_*` node labels and durable `content_pack_source_proposals` records exposed through `/api/v1/content-packs/source-proposals?tenant_id=...`, with high-risk sources normalized to `approval_required`; the proposal API now supports server-side `limit`/`offset` pagination, `q` and `status` filters, and `summary.by_status` totals so operators do not have to pull a broad unfiltered set before review. Discovery now has an explicit bank-safe auto-connect policy contract: the default path only auto-connects low-risk running local services whose catalog profile allows automatic collection; medium/high-risk services require approval unless tenant policy explicitly widens auto-connect. Tenant event-filter policy now persists connector auto-connect controls and delivers them to capable agents over heartbeat with `connector_auto_connect_policy.v1`; agents cache the policy and apply it to the next service discovery cycle. Admins can approve, reject, or privacy-block proposals through the source-proposal decision API and the `/security/siem` UI, then render approved raw or parsed-only proposals into OTel config candidates from the same page. Approved local file-log proposals are exposed to the owning node through `GET /api/v1/nodes/{node_id}/log-sources/approved`, loaded by the node agent at startup, and hot-added through heartbeat responses when the agent advertises `connector_approved_sources.v1`.

Problem: Service/package/app discovery is not converted into connector proposals.

Build:

- Discovery joins: packages, listening services, webserver inventory, app root detection, DB process discovery, log path candidates.
- Candidate generation: source type, formatter/parser, paths/channels/DSN requirements, data sensitivity, estimated volume, required privileges.
- Signed/offline catalog overlay support for bank proprietary apps.

Acceptance:

- On a pilot host, Control One discovers nginx/Postgres/Redis/Kafka/WebLogic/Temenos-style apps and creates connector proposals without manual form entry.
- High-risk sources require approval by policy, not silent collection.

### C1-SIEM-003: Safe Auto-Connect Policy Engine

Priority: P0

Status: Closed for P0 go-live. Connector discovery now carries an explicit `AutoConnectPolicy` with default low-risk-only behavior plus explicit medium/high-risk widening and program-level approval/block lists. The policy is persisted on tenant event filters, exposed through `GET/PUT /api/v1/tenants/{id}/connector-policy`, returned to agents through heartbeat for `connector_auto_connect_policy.v1`, cached by the node agent, and used by local service discovery when creating proposals or auto-added log sources. Proposal labels include the policy decision and risk class so downstream UI/API can explain why a discovered source is auto-eligible or approval-required. Source approval now carries a `collect_mode` (`collect_raw`, `collect_parsed`, `metadata_only`, `observe_only`, or `disabled`); `collect_raw` and `collect_parsed` approvals are deployable by the node-agent local source path, with `collect_parsed` stripping the raw message before transport while retaining parsed fields/labels/severity/timestamps. Proposal-derived runtime rows now explicitly label metadata-only, observe-only, and disabled approvals as observed but non-collecting, with raw retention false, so operators can prove policy intent without starting a collector. OTel candidate rendering now allows `collect_raw` and first-pass `collect_parsed`; the parsed-only OTel path injects a transform processor that stamps `control_one.raw_message_retained=false` and overwrites the log body before export, while metadata-only/observe-only/disabled approvals still cannot start collection. The `/security/siem` UI now lets admins review and update the tenant connector policy without editing tenant event-filter JSON directly and choose the approval collect mode from the proposal decision dialog.

Problem: The intended "jump manual connector phase" needs bank-safe guardrails.

Build:

- Tenant policies for auto-connect by source class, environment, data sensitivity, volume budget, allow/deny programs and paths, redaction profile.
- Approval modes: observe-only, collect metadata, collect raw logs, collect parsed-only, disabled. First slice complete as persisted/API/UI collect-mode intent plus node-agent local `collect_parsed` runtime support that omits raw messages before sending. Metadata-only/observe-only approvals now project non-collecting runtime proof labels from fresh local proposal observations. OTel edge `collect_parsed` has a first redaction pipeline that overwrites log bodies before export and warns that receiver recipes must parse required fields first.
- Agent applies updated connector state without restart.

Acceptance:

- Default bank policy can auto-connect low-risk infra logs and require approval for core banking, IAM, DB query text, and customer-data-bearing logs.

### C1-SIEM-004: Inbound Syslog, CEF, LEEF, TCP/UDP/TLS Receiver

Priority: P0

Status: Closed for P0 go-live. The content-pack parser runtime already executes
RFC3164/RFC5424 syslog, CEF, and LEEF stages, and the carrier parsers now
promote common security fields into normalized query aliases: `src`/`dst`,
`spt`/`dpt`, `proto`, `act`, and user fields become `source.ip`,
`destination.ip`, `source.port`, `destination.port`, `network.protocol`,
`event.action`, and `user.name` where present. This makes first appliance
samples from Fortinet/Palo/Cisco-style CEF and QRadar-style LEEF usable by
coverage/parser tests instead of leaving key fields only in carrier-specific
extension maps. The OTel syslog receiver renderer now productizes TCP, UDP,
and TLS listener recipes: TLS can be declared as `transport: tls`/`tcp_tls`,
nested `tcp.tls`, or top-level `tls`/`tls_*` fields; TLS defaults to port
6514, requires server cert/key material, rejects UDP+TLS, enables remote
`net.*` attribute capture, strips Control One-only source identity/allowlist
keys before collector validation, stamps audited `control_one.syslog.*`
resource metadata, and warns on UDP, missing TLS, missing mTLS client CA, or
missing source identity/allowlist intent. Focused tests cover an mTLS
identity-controlled Fortinet-style syslog recipe and invalid TLS-over-UDP. The
same render plan now carries nftables edge network policy templates derived
from source allowlist CIDRs and syslog rate-limit settings, while stripping
those Control One-only keys from the collector YAML. The starter bank-security
pack now has replayed semantic fixtures for Fortinet, Palo Alto, Cisco ASA,
Check Point LEEF, F5, Imperva, and Cloudflare appliance paths. Deeper
vendor-specific enrichment beyond those first-pass semantics is post-P0 pack
expansion, not a go-live blocker.

Problem: Network/security appliances cannot send directly to Control One.

Build:

- Control-plane or edge-collector listener for RFC3164/RFC5424 syslog over UDP/TCP/TLS.
- CEF/LEEF parser normalization.
- Per-device identity, mTLS/TLS, source allowlists, rate limits, spool/replay.

Acceptance:

- Fortinet/Palo Alto/Cisco/Check Point/F5/Imperva/WAF devices can send logs without an agent on the device.

### C1-SIEM-005: Windows Event Forwarding / WEC Support

Priority: P0

Status: Closed for P0 go-live. The OTel edge-config renderer now treats `wef` content-pack
sources as renderable through the `windows_event_log` receiver and defaults to
the Windows `ForwardedEvents` channel when a WEC/WEF source does not declare
explicit channels. This gives banks an OTel-compatible WEC deployment shape
with persistent receiver storage and source-health projection through the
existing edge collector path. The Windows EventData parser now promotes first
Security, Sysmon, and PowerShell fields into normalized aliases: EventID/provider/channel,
computer, target/subject users, source/destination IPs and ports, process
image/command line/parent image, and common 4624/4625/4634/4647/4688/4689 plus
Sysmon process/network plus PowerShell 4103/4104 script semantics. Focused
tests cover domain-controller 4624/4625/4634/4647/4688/4689 events, a Sysmon
process-create event, and a PowerShell script-block event. `docs/siem-wef-wec-deployment-runbook.md`
now documents the source-initiated WEF/WEC deployment path: WEC host prep,
Subscription Manager GPO, canary subscription XML, Control One OTel candidate
review/apply, validation, operations, rollback, and troubleshooting. The bank
starter pack now includes first-pass Windows Security, Sysmon, and PowerShell
EventData parser profiles with WEF/windows-event collector recipes and replayed
golden samples. Expanding beyond starter fixtures into additional signed
Windows packs and richer AD/PowerShell detections is post-P0 content work.

Problem: Endpoint and AD security events at banks often arrive through WEF/WEC, not one agent per event channel.

Build:

- Windows collector support for WEC subscriptions and forwarded events.
- Replace current hand-rolled PowerShell JSON parsing with structured JSON decoding.
- Default AD/DC/security channel source pack.

Acceptance:

- Domain controllers can forward Security/Sysmon/PowerShell events into Control One with parser health and source attribution.

### C1-SIEM-006: OpenTelemetry Collector / Grafana Alloy Integration

Priority: P0

Status: Closed for P0 go-live. Control One now has an auditable OTel/Alloy edge-collector
path from content-pack source selection through rendered YAML, immutable config
candidate review, approval, queueing, collector-scoped desired-config fetch,
apply-result reporting, rollback, source-health projection, node-agent wrapper
apply/supervision, and receiver metric scraping. The latest hardening accepts
OTel `_total` receiver/exporter counter variants and treats fractional Vector
buffer utilization as real backpressure instead of truncating it to zero, and
it prevents accidental duplicate collection by surfacing node-agent versus OTel
edge overlap as `collection_conflict` unless migration dual-write is explicitly
labelled.

Problem: Reimplementing every telemetry receiver is wasteful and increases tool sprawl.

Research read:

- OpenTelemetry Collector should be the default edge-collector contract because its receiver ecosystem already covers host/app/cloud/security sources that Control One should not reimplement one by one.
- Grafana Alloy is useful for customers already standardized on Grafana/Prometheus/Loki and can be treated as an OTel-compatible managed profile, not a product dependency.
- Vector and Fluent Bit are good compatibility paths for existing estates and lightweight forwarding, but Control One should prefer accepting their output over bundling every agent by default.

Build:

- Ship a supported OTel Collector or Grafana Alloy profile as an optional edge collector.
- Receive OTLP into Control One or translate OTel logs/metrics/traces into Control One events.
- Provide managed configs for syslog, Windows EventLog, Splunk HEC, filelog, journald, Kafka, AWS/Azure/GCP, Prometheus.
- First implementation slice complete: `internal/contentpacks` can now render deterministic OTel Collector config plans/YAML from approved content-pack sources for filelog, syslog, Windows Event Log, OTLP, Splunk HEC, Kafka, and Prometheus recipes. The renderer stamps Control One tenant/collector/source/pack/parser attributes, adds memory limiter and batch processors, refuses approval-gated sources without an approval reference, uses the current `windows_event_log` receiver name while tolerating old `windowseventlog` pack recipes, and enables persistent file storage for file offsets, Windows bookmarks, and OTLP exporter queues by default.
- First API slice complete: `POST /api/v1/content-packs/otel-config?tenant_id=...` renders an auditable OTel config from the active tenant content-pack registry for explicitly requested enabled sources. It returns a deterministic `sha256:` `config_version` for approval/deployment/rollback references. It is render-only; it does not deploy or mutate collector state.
- First candidate-persistence slice complete: `POST /api/v1/content-packs/otel-config/candidates?tenant_id=...` stores an explicit rendered deployment candidate with exact YAML, structured plan, source IDs, active registry snapshot reference, creator subject, status `rendered`, and `config_version`; `GET /api/v1/content-packs/otel-config/candidates?tenant_id=...` lists candidate metadata for review.
- First candidate-approval slice complete: `POST /api/v1/content-packs/otel-config/candidates/{id}/approve?tenant_id=...` lets an admin approve a rendered candidate, captures approver subject/note/timestamp, keeps the rendered YAML immutable, and writes an audit event for the exact `config_version`.
- First collector registry/heartbeat slice complete: `POST /api/v1/content-packs/collectors?tenant_id=...` registers an OTel/Alloy/Fluent Bit/Vector/node-agent edge collector, `GET /api/v1/content-packs/collectors?tenant_id=...` lists collectors, and `POST /api/v1/content-packs/collectors/{collector_id}/heartbeat?tenant_id=...` records running config version, desired config version, status, last heartbeat, last error, and raw health evidence.
- First apply-queue slice complete: `POST /api/v1/content-packs/otel-config/candidates/{id}/queue?tenant_id=...` moves an approved candidate to `queued`, requires a registered non-disabled target collector, requires queue callers to acknowledge the exact `expected_config_version`, refuses candidates whose reviewed version no longer matches the approved `config_version`, sets that collector's desired config version to the exact candidate `config_version`, captures queue subject/note/timestamp, and writes an audit event.
- First collector fetch/apply-result API slice complete: `GET /api/v1/content-packs/collectors/{collector_id}/desired-config?tenant_id=...` returns the queued exact YAML/config version for that collector, and `POST /api/v1/content-packs/collectors/{collector_id}/apply-result?tenant_id=...` records `deployed` or `failed` for the exact config version while updating the collector's running config version/status.
- First rollback API slice complete: successful deploys now supersede older deployed configs for the same collector, and `POST /api/v1/content-packs/collectors/{collector_id}/rollback?tenant_id=...` re-queues a superseded exact config version as the collector's desired config for rollback apply/report.
- First coverage projection slice complete: tenant coverage now adds a "Tenant SIEM edge collectors" telemetry overlay showing registered/healthy/degraded/disabled collectors, heartbeat freshness, desired-vs-running config mismatch, and explicit gaps when collector health is stale or partial.
- First collector-scoped auth slice complete: admins can rotate an edge collector token at `POST /api/v1/content-packs/collectors/{collector_id}/token?tenant_id=...`; collectors can use `X-ControlOne-Collector-Token` or a `Bearer c1ec_...` token only for their own heartbeat, desired-config fetch, and apply-result report; registration, queueing, rollback, and token rotation remain operator/admin RBAC flows.
- First source-health projection slice complete: tenant coverage now adds a parser-domain "Tenant SIEM source health" overlay from edge collector heartbeat health evidence. It accepts per-source or receiver-level health maps/lists, normalizes common states (`parser_healthy`, `collecting`, `parser_failed`, `silent`, `backpressured`, `collection_conflict`, `stale`, `deployed`), applies the same heartbeat freshness window in coverage and source-health detail APIs, aggregates events/parsed/failure/drop/queue counters, carries runtime labels like `collect_mode`, `raw_message_retained`, receiver/candidate/proposal IDs, collection owner/identity, preserves `source_instance_id` plus `approval_required`/`approval_id`, and keeps the row `raw_only` when collectors are not yet reporting source-level truth. `GET /api/v1/content-packs/source-health?tenant_id=...` now exposes the same derived per-source evidence for UI/detail panels and supports `limit`/`offset` pagination plus `q` search, effective `state` filtering, durable all-matching `totals.by_state`/`totals.metrics` summary values across source runtime rows, runtime-state IDs, and recommended investigation actions for parser-failed/silent/backpressured/collection-conflict/stale rows. The tenant coverage matrix reads sampled persisted runtime rows so it can detect duplicate collection owners that aggregate summaries cannot reveal, and marks the row truncated when more than 500 runtime states exist.
- First durable source-runtime-state slice complete: `content_pack_source_runtime_states` persists latest tenant/source/collector coverage state, parser/config metadata, timestamps, metrics, and labels, including `collection_conflict` as a degraded runtime state. Collector heartbeats now upsert normalized source evidence into this table, and coverage/source-health reads prefer persisted rows while falling back to heartbeat-derived evidence when no durable rows exist yet.
- First SIEM coverage UI slice complete: `/security/siem` is linked from the sidebar and calls the connector policy, source proposal, source health, edge collector, OTel config-candidate, and SOC case APIs. It shows proposal/health/deployment KPIs, policy toggles and program allow/approval/block lists, paginated server-side proposal status/search filtering with status-summary KPIs, admin approve/reject/privacy-block actions, approved raw/parsed-only proposal-to-candidate render, exact rendered candidate YAML/source review, candidate approve/queue actions, collector selection, searchable/paginated/filterable source runtime metrics with all-matching summary KPIs, source instance IDs, approval refs, runtime evidence labels for collection mode/raw retention, and an inline source-health drilldown with identity/config/parser/timestamp/error/metric/label evidence without claiming proposal-only sources are protected. Degraded source-health drilldowns can now open an auditable SOC case through `POST /api/v1/content-packs/source-health/investigate`, preserving runtime-state evidence before parser/content/collector changes; the SOC case response extracts structured `content_pack_source_runtime_state` citations as evidence refs for export and analyst notes; and the page lists recent `siem_source_health` SOC cases with severity/status/source/parser/error/evidence-ref/export context plus a cited note box that posts analyst notes back to the SOC case with the runtime-state evidence ref. The SOC case list endpoint now has explicit `include_notes=true` hydration so recent source-health cases can show persisted latest notes after refresh without forcing all SOC case lists to pay that cost. Proposal rows prefer node-scoped source-health evidence (`node_id/source_id`) before falling back to source-only evidence so common software across many servers does not show another node's health by accident. The tenant coverage matrix also consumes durable proposal/status summaries instead of relying on sampled rows, so CISO/sales coverage truth scales beyond the first 500 proposals. Candidate approval now requires the UI/API caller to acknowledge the exact reviewed `sha256:` config version; the candidate row persists the reviewed version and YAML digest for audit. Candidate queueing also sends the expected `sha256:` config version and the backend rejects stale or mismatched queue requests.
- First approved local-source apply slice complete: `GET /api/v1/nodes/{node_id}/log-sources/approved` lets an authenticated agent fetch only its own approved local file-log proposals whose collect mode is empty/legacy, `collect_raw`, or `collect_parsed`, and the node agent merges those sources at boot before auto-discovery. Heartbeat responses now include deployable approved log sources for agents advertising `connector_approved_sources.v1`, stamped with `control_one.collect_mode`; `collect_parsed` sources send structured fields with a static redacted message instead of the raw line. Successful `/api/v1/logs` batches with Control One source labels now upsert source runtime state to `collecting` with event/parsed counters, so local approved collection becomes visible in source-health coverage. The telemetry service can hot-add new collectors without restarting existing collectors. This creates a concrete high-risk approval-to-local-collection path while richer policy orchestration remains pending.
- First approved proposal-to-OTel render/apply slice complete: OTel render and candidate-create requests now accept `source_proposal_ids`. The control plane loads those durable proposals, requires them to be approved, maps local file proposals to `otel_filelog` mode when the content-pack source supports it, injects the proposal ID as the approval reference, and then uses the existing OTel renderer/candidate store. Candidate creation projects per-source runtime state to `config_rendered` with collector/config/proposal evidence; queueing and rollback queueing update the target collector evidence; successful apply moves planned sources to `deployed`; failed apply keeps them `config_rendered` with the collector error. This removes the manual copy/paste step between approved source proposals and edge-collector config review while keeping coverage truth accurate through deploy and rollback.
- First approved-proposal source resolver slice complete: when a durable source proposal is approved, OTel rendering now resolves the proposal against the active content-pack registry by exact source ID first, then by unique source/parser hints, aliases, product/vendor, metadata, and parser/source prefixes. Ambiguous matches fail closed and ask for an explicit `content_pack_source_id` instead of guessing.
- First node-agent content-pack collector wrapper slice complete: when `content_pack_collector.enabled` is true, the agent polls the queued exact desired OTel/Alloy config with the collector-scoped token, verifies the `sha256:` config version, writes the YAML atomically, optionally runs argv-based validate/reload commands, reports deployed/failed apply-result, persists local wrapper state, and heartbeats config-level wrapper health. The wrapper can now also supervise a local collector process via `supervise_command`, restart it after approved config apply, restart it after unexpected exit on the next sync, and include managed process state/PID/restart count/last exit error in collector heartbeat health. When `metrics_endpoint` is configured, it scrapes OTel/Alloy, Vector, and Fluent Bit compatible Prometheus metrics and translates receiver accepted/refused records, `_total` counter variants, scraper results, component/input/output drops, retry/error counters, fractional Vector buffer utilization, and exporter queue pressure into `health.receivers` for source-health coverage.
- Provide compatibility intake for Vector/Fluent Bit output paths where customers already run them.

Acceptance:

- Control One can absorb existing OTel/Grafana/Fluent/Vector estate without replacing it on day one. Post-P0 expansion: deeper vendor-specific receiver/source metric coverage, broader output compatibility hardening, and richer source proposal/approval policy on top of the renderer/candidate store.

### C1-SIEM-007: Durable Agent Spool and Log Cursor Persistence

Priority: P0

Status: Closed for P0 go-live. The node agent now enables a disk-backed eventstream spool
under the agent state directory, persists `/api/v1/logs` compatibility batches
before replay, and drains both spools after control-plane outages or agent
restart. File log collectors now persist cursor state to disk, keyed by stable
file identity where the OS exposes it and by path fallback otherwise, so restart
does not reread already-forwarded file logs or silently advance past new lines.
Heartbeat self-metrics now expose pending event/log spool records, bytes,
configured byte budgets, and in-process budget-drop counters so operators can
see replay backlog/backpressure. Eventstream batches now carry stable replay
keys and the control plane stores those keys on `event_ingest_batches`, returning
the existing journal receipt instead of re-running local fanout on duplicate
agent replays. `/api/v1/logs` compatibility batches now carry stable replay keys
and write compact `agent_ingest_replay_receipts` after successful ingest so
post-success spool replays are acknowledged without duplicating log rows.
Agent heartbeat self-metrics now project event/log spool backlog and drop counts
into a synthetic `control_one.agent_spool` source-health runtime row, marking it
`backpressured` when records are queued or budget drops occur so the existing
SIEM source-health UI can show queue/drop evidence. `/api/v1/logs` now journals
derived `log.line`/`web.request` events into `event_ingest_batches` before log
row insertion, then marks the journal through local-complete and Doris retry
states after the log rows land. Focused tests cover event spool replay, log
spool replay, file cursor restart behavior, duplicate replay receipts,
source-health projection, and log-derived event journaling. Live
`/api/v1/events/ingest`, `/api/v1/logs` derived events, and event journal
replay now use a shared `eventIngestService` for log-derived journal records,
local fanout, local-complete phase marking, Doris flush, and final journal
status so retry semantics cannot drift between handlers and the drainer.
Promoting that service out of the server package and eventually moving raw log
compatibility batches fully behind it are post-P0 refactors.

Problem: Current eventstream can drop when buffers fill; file tail offsets are in-memory.

Build:

- Disk-backed spool/WAL for critical events/log batches.
- Persistent file cursors keyed by inode/file identity.
- Backpressure reporting and tenant volume budgets.

Acceptance:

- Agent restart or control-plane outage does not silently skip bank audit logs within configured retention/spool limits.

### C1-SIEM-008: Parser Runtime and Content Packs

Priority: P0

Status: Closed for P0 go-live. `internal/contentpacks` now defines the content-pack manifest contract and validator for pack identity/versioning, source profiles, collector modes, parser stages, OCSF/ECS schema bindings, sample/golden coverage, detection references, and high-risk approval gates. It also defines the first source-level runtime coverage state model so future UI/API work can distinguish discovered, proposed, approval-required, deployed, collecting, parser-healthy, parser-failed, silent, backpressured, collection-conflict, unsupported, privacy-blocked, and stale sources without overstating protection. The package now includes an in-memory registry contract for compatible pack install, enable, disable, quarantine, deprecate, rollback-candidate marking, conflict-safe active source resolution, defensive manifest cloning, deterministic snapshot/restore of enabled/quarantined lifecycle state, and tenant-scoped Postgres snapshot persistence. Parser profiles now compile only when every stage has an executable runtime in this build; first-pass executable stages cover JSON, RFC3164/RFC5424 syslog, CEF, LEEF, regex, grok, KV, logfmt, XML, Windows EventData, timestamp, field mapping, OCSF/ECS alias mapping, enrichment, redaction, and conditional drop. CEF and LEEF stages now emit common normalized aliases for source/destination IPs, ports, protocol, action, outcome, user, and provider fields so carrier-format packs produce queryable security semantics before deeper vendor-specific enrichment lands. Windows EventData now emits first-pass Security/Sysmon/PowerShell aliases for event identity, host, account, source/destination network, process fields, and script-block content with 4624/4625/4634/4647/4688/4689 plus Sysmon process/network and PowerShell 4103/4104 semantics. Manifest-declared samples can now be replayed against JSON/JSONL golden outputs with deterministic pass/fail reports, content-root path traversal protection, and default `internal/securityschema` validation so a bad golden cannot bless invalid known normalized field types; legacy migration can explicitly disable schema validation. Signed offline bundles can now carry active `siem_content_pack` artifacts with embedded samples/goldens and receipt provenance, then sync replay-passing packs into enabled registry state, parser-replay-failed or detection-replay-failed packs into quarantine, and persist the resulting active registry snapshot after import. Offline pack replay/sync responses now include detection replay reports, so missing or invalid Sigma artifacts cannot silently become enabled registry metadata. Operators can now read active pack/source state through `/api/v1/content-packs` and `/api/v1/content-packs/sources`, and tenant coverage adds a parser-domain content-pack registry overlay. This now includes first-pass vendor-semantic firewall parser profiles for Fortinet FortiGate, Palo Alto PAN-OS, Cisco ASA, and Check Point, WAF parser profiles for F5 BIG-IP ASM/WAF, Imperva WAF, and Cloudflare WAF, and Windows EventData profiles for Active Directory Security, Sysmon, and PowerShell; broader vendor-semantic packs are post-P0 content expansion.

Latest slice: Okta System Log and Microsoft Entra ID now have first-pass IAM
JSON semantic parser profiles with replayed samples, promoting event code,
action, outcome, user, and source IP fields into normalized authentication
events.

Problem: The app catalog is broad but parser coverage is narrow/generic.

Build:

- Versioned parser packs with tests, samples, schema mapping, and provenance.
- First packs: Linux auth/syslog/auditd, Windows Security/Sysmon/PowerShell, Nginx/Apache/HAProxy/IIS, Postgres/MySQL/MSSQL/Oracle/Db2, Kafka/RabbitMQ/IBM MQ, WebLogic/WebSphere/Tomcat/WildFly, Temenos/Flexcube/Finacle profiles.
- Offline import/export with receipts.

Acceptance:

- Every advertised parser has sample logs, pass/fail tests, mapped fields, and coverage status.

### C1-SIEM-009: Detection-as-Code Engine

Priority: P0

Status: Closed for P0 go-live. `internal/detections` now provides the first detection IR and
replayable evaluator: nested `all`/`any`/`not` expressions, field predicates
for exact, contains, prefix/suffix, existence, set membership, and numeric
comparisons, plus match metadata with rule ID/title/severity/logsource/tags.
It also includes a Sigma import path for common field selections, keyword
selections, modifiers such as `contains`/`startswith`/`endswith`/`exists`,
field mapping into Control One normalized names, and boolean conditions with
`and`/`or`/`not`/parentheses. Unit tests cover a normalized Windows PowerShell
process detection, numeric threshold matching, Sigma field import with
suppression filter, and Sigma raw-keyword replay. Content-pack manifests can
now load referenced Sigma files through `LoadManifestDetections`, pin rule
identity/severity/tags to manifest metadata, reject traversal/missing files, and
evaluate loaded rules against normalized events. `ReplayManifestDetections` now
simulates source-linked detections against golden normalized sample events and
reports rule counts, evaluations, matches, and replay failures. Signed offline
content-pack sync now runs detection replay alongside parser replay and
quarantines packs with missing or invalid detection files. Live event fanout now
loads detections from enabled active signed content packs, source-gates them by
`content_pack_source_id`, evaluates normalized event fields, and creates
deduped `content_pack_detection` alerts with evidence citations. Operators can
now list active-registry detections through
`GET /api/v1/content-packs/detections?tenant_id=...`, including source linkage,
severity, tags, Sigma logsource when loaded, and load status
(`loaded`, `metadata_only`, `inactive`, or `error`) so broken signed artifacts
are visible before rollout. They can also run
`GET /api/v1/content-packs/detections/replay?tenant_id=...` to re-evaluate the
tenant's active signed pack detections against their golden normalized sample
events and inspect pass/fail, evaluation, and match counts. Admins can now use
`POST /api/v1/content-packs/lifecycle?tenant_id=...` to enable or disable a
signed pack with `expected_snapshot_id` concurrency protection; enable is gated
by successful active-artifact detection replay and records an audit event.
Per-detection operator controls now persist in
`content_pack_detection_overrides`; admins can set `enabled`, `disabled`, or
temporary `suppressed` states through
`/api/v1/content-packs/detections/overrides`, the detection list exposes
effective state/override metadata, live fanout skips disabled or actively
suppressed detections, and every override write emits an audit event. Remaining
loaded detection artifacts now persist in
`content_pack_detection_artifacts` by tenant registry snapshot; live fanout
stores freshly loaded signed-pack rules there and can fall back to those
compiled artifacts if the active archive cache is unavailable. Detection
manifests can now declare stateful threshold windows with `group_by` fields and
`suppress_for_seconds`; replay and live fanout use the shared temporal
evaluator, and runtime alerts include threshold/window/group context. Manifests
can also declare ordered sequence steps with normalized-field predicates,
grouping, windows, and suppression, using the same replay/live evaluator path.
Detection manifests now support `risk_score` with severity-based fallback;
replay matches, detection list responses, and live alert context carry the
resulting score. Detection manifests can now declare unordered joins over
normalized-field predicates, grouped by shared fields and bounded by windows and
suppression. Expanding the starter ATT&CK-tagged detections into deeper
vendor-semantic production coverage is post-P0 content work.

Problem: Current rules are regex/count/window; correlation `yaml_spec` is stored but not executed as a rich condition language.

Build:

- Sigma-like rules or compatible import path.
- Rich predicates over normalized fields, joins, sequences, thresholds, suppressions, risk scoring.
- Unit tests and replay/simulation for each detection.

Acceptance:

- A bank can review, test, enable, disable, and audit detections as signed content.

### C1-SIEM-010: Normalize to a Standard Security Schema

Priority: P0

Status: Closed for P0 go-live. `internal/securityschema` now defines the first shared
Control One security event schema identity, field dictionary, ECS and UDM export
alias metadata, OCSF object hints, type/IP validation, nested/dotted field lookup,
and deterministic alias export for common event, host, user, source,
destination, network, process, and rule fields. Content-pack sample replay now
uses this validator by default, so replay-passing parser packs must also emit
valid types for known normalized fields; a bad golden cannot bless
`source.port` as a string or `source.ip` as an invalid address. Live
`/api/v1/events/ingest` now also validates known normalized fields assembled
from top-level event attributes and parser-provided `details.fields` /
`details.normalized` maps while leaving unrelated vendor-specific details
alone. Tests validate a normalized Windows Security 4624 shape, reject bad
IP/port/string types, pin the dictionary ordering plus core field anchors, and
cover replay schema failure/legacy opt-out plus live ingest schema failures.
The schema now includes `process.parent.command_line`, matching the Sigma
parent-command-line field map used by content-pack detections so live ingest
does not drop that alias. UDM alias export now projects first-pass Google SecOps
fields such as `metadata.product_event_type`, `metadata.product_name`,
`principal.ip`, `target.ip`, `target.process.file.full_path`, and
`security_result.rule_id`, and derives `metadata.event_type` for common
authentication, network, DNS, process-launch, and process-termination events.
OCSF alias export now projects first-pass object fields and derives
`category_name` / `class_name` for common authentication, DNS,
network/firewall, process/EDR, file, email, and detection-finding events.
This gives parser packs, detections, and exports one vocabulary to target
instead of relying only on ad hoc parser fields. The first operator-facing
reference is published at `docs/security-schema-field-reference.md` with field
types, ECS aliases, UDM aliases, OCSF hints, authoring rules, and a Windows
authentication example. Product-specific OCSF/UDM validation profiles, version
schema migrations, numeric OCSF IDs where needed, and additional Doris typed
columns/indexes for high-value fields are post-P0 schema expansion.

Problem: Control One events are useful but not mapped to a known external schema.

Build:

- OCSF/ECS/UDM-style normalized fields with original raw preserved.
- Field dictionaries, parser status, raw refs, and event family contracts.

Acceptance:

- Existing SOC teams can understand and export events without learning only Control One-specific fields.

### C1-SIEM-011: Existing SIEM Import/Forward Paths

Priority: P1

Status: Closed for P0 go-live. `controlplane/internal/logforward` now has tested outbound
sink construction for Loki, Elasticsearch, and Splunk HEC. The sink factory
validates required endpoints and credentials, and the Splunk HEC sink serializes
batched telemetry logs as HEC event envelopes with tenant, node, source, level,
program, and label fields plus the expected authorization header. Tenant
forwarding destinations are now persisted through migration `0125`, with
checkpoints and delivery-attempt tables for resumable forwarding evidence.
Operators can manage tenant-scoped Loki, Elasticsearch, and Splunk HEC
destinations through `GET/POST /api/v1/siem/forwarding-destinations`; the API
requires secret references instead of raw credentials, redacts credential refs in
responses, and records audit metadata on upsert. The config-gated
`siem_forwarding` scheduler now drains enabled tenant destinations from
`telemetry_logs`, resolves credential refs through a pluggable resolver
(bootstrap support for `env:VAR` refs and `vault://path#key` refs when
`vault.address` is configured), pushes bounded batches, records success/failure
delivery attempts, and advances stable `(timestamp, log_id)` checkpoints only
after a successful sink push. Microsoft Sentinel/Log Analytics coexistence now
has a modern Azure Monitor Logs Ingestion API sink (`sentinel`, `log_analytics`,
or `azure_monitor`) that posts DCR-shaped JSON arrays with bearer-token auth.
Inbound coexistence is now covered by `controlplane/internal/siemimport` plus
`POST /api/v1/siem/imports`, which accepts Splunk HEC exports,
Elastic/Beats bulk or document exports, Sentinel/Log Analytics arrays, generic
JSONL/NDJSON, and gzip-compressed archive payloads such as S3/Azure Blob exports.
Imports create tenant-scoped `telemetry_logs` rows with import-id/source/format
labels, raw SHA-256 receipts, dry-run support, row counts, skipped-row warnings,
and audit records. Direct bucket polling remains post-P0; upload/import of
existing SIEM archives is closed for go-live coexistence.

Problem: Banks will not rip out Splunk/Sentinel/Elastic on day one.

Build:

- Inbound: Splunk HEC, Elastic/Beats-compatible, Sentinel/Log Analytics export/import strategy, S3/Azure Blob archive payload import.
- Outbound: productized Loki/Elasticsearch/Splunk/Sentinel forwarding with retries, tenancy, and audit. Loki/Elastic/Splunk/Sentinel sink construction, tenant destination persistence/API, secret-ref resolution, delivery-attempt evidence, checkpointed forwarding worker, and audited inbound archive import are present.

Acceptance:

- Control One can run beside an existing SIEM and prove incremental value before replacement.

### C1-SIEM-012: Bank Network/Security Appliance Packs

Priority: P0

Status: Closed for P0 go-live. `internal/contentpacks.BankSecurityStarterPack` now provides a
replayable starter content pack with 26 named bank/security sources across
firewall, WAF, IAM, EDR, DNS, proxy, mail, and private-access audit categories:
Fortinet FortiGate, Palo Alto PAN-OS, Cisco ASA/FTD/Meraki, Check Point, F5,
Imperva, Cloudflare, Zscaler, Okta, Entra ID, Active Directory Security,
CrowdStrike, SentinelOne, Microsoft Defender for Endpoint, Windows Sysmon,
Windows PowerShell, CoreDNS, BIND, Infoblox, Squid, Proofpoint TAP, NetBird,
OpenZiti, and Headscale. Each source
has bank-safe approval-required metadata, a CEF/LEEF/JSON carrier parser, a
golden replay sample validated by the normal content-pack replay harness, and
at least one attached starter detection. The pack now includes eight
ATT&CK-tagged Sigma detections covering denied network traffic, WAF exploit
blocks, IAM authentication failure bursts, EDR malware alerts, DNS query bursts,
mail threats, suspicious PowerShell script blocks, and private-access policy changes; each has an explicit
`risk_score`, and detection replay proves the clean starter samples stay quiet.
Fortinet FortiGate, Palo Alto PAN-OS, Cisco ASA, Check Point, F5 BIG-IP ASM/WAF,
Imperva WAF, and Cloudflare WAF now use first-pass vendor-semantic CEF/LEEF
parser profiles that stamp `event.dataset`, `observer.type`, and semantic
provenance; denied firewall and blocked WAF replay samples prove the
network-denied and WAF exploit detections fire while clean samples stay quiet.
Okta System Log and Microsoft
Entra ID now use first-pass IAM JSON semantic profiles that promote event code,
action, outcome, user, and source IP fields into normalized authentication
events. Active Directory Security,
Windows Sysmon, and Windows PowerShell now use first-pass EventData parser
profiles with WEF/windows event collector recipes and replayed golden samples.
The encoded PowerShell replay sample fires the ATT&CK-tagged script-block
detection while clean Windows samples stay quiet. Deeper product-specific
parsers, DLP/mail/API-specific packs beyond starter coverage, and additional
signed production artifacts remain post-P0 content expansion.

Problem: Bank SIEM pilots expect firewalls, WAF, IAM, VPN/ZTNA, EDR, mail, DNS, proxy, and DLP logs.

Build:

- Initial packs: Fortinet, Palo Alto, Cisco ASA/FTD/Meraki, Check Point, F5, Imperva/Cloudflare WAF, Zscaler, Okta/Entra ID/AD, Windows Security/Sysmon/PowerShell, CrowdStrike/SentinelOne/Microsoft Defender, CoreDNS/BIND/Infoblox, Squid/Blue Coat, NetBird/OpenZiti/Headscale audit.

Acceptance:

- At least 20 named enterprise/security sources work through syslog/API/OTel with parser coverage truth.

### C1-OBS-001: Observability Metrics/Traces/APM Ingestion

Priority: P1

Status: Not a P0 go-live blocker. Host metrics, labelled host-health samples,
telemetry logs, SIEM events, source runtime health, and admin ingest dashboards
are present for bank readiness. Full OTLP traces/APM, RED/USE app metrics, and
trace-derived service maps remain P1 observability expansion.

Problem: Control One currently has lightweight host metrics and events, not a full observability stack.

Build:

- OTLP metrics/logs/traces ingestion path.
- Service map from traces + connections + webserver routes.
- RED/USE metrics for apps, DBs, queues, and webservers.

Acceptance:

- A bank app team can use Control One to see service latency/error/saturation beside SIEM events.

### C1-OBS-002: Server Health Collector Completion

Priority: P0

Status: Closed for P0 go-live. The node agent host metric contract now emits
the platform health signals used by predictive scoring: Linux iowait percentage
from successive CPU time deltas, swap used percentage, load-average-to-CPU
ratio, and Linux `/proc/vmstat` OOM-kill deltas after the first sample. Windows
agents now collect equivalent operational signals through Performance
Counters/EventLog: pagefile usage maps to `host.swap_used_pct`, Resource
Exhaustion Detector events map to `host.oom_events_count`, and PhysicalDisk
queue depth emits `host.disk_queue_length` for sustained disk-pressure scoring.
Configurable ICMP health probes emit `net.packet_loss_pct` and
`net.icmp_latency_p99` when `ping` is available. Configurable SMART/NVMe probes
parse `smartctl -j` output into aggregate `smart.reallocated_sector_count` and
`smart.uncorrectable_errors` scorer metrics while also sending labelled
per-device metric samples with device/model/serial/protocol evidence. These
names remain part of the shared `internal/metrics` contract so the agent
emitter and control-plane predictor cannot drift, while calibration still
depends only on universally emitted signals.

Problem: Predictive health scoring references SMART, PSI/iowait, swap, OOM, packet loss, ICMP latency, but most are optional/aspirational and not emitted by the agent yet.

Build:

- Linux PSI/iowait, `/proc/vmstat` OOM, swap pressure, SMART/NVMe, ICMP/TCP latency, packet loss.
- Windows equivalents through Performance Counters/EventLog where practical.

Acceptance:

- Health scores are based on emitted data, not optional gaps; calibrating state clears consistently.

### C1-CVE-001: Real Vulnerability Feed Pipeline

Priority: P0

Status: Closed for P0 go-live. Signed offline vulnerability feeds now support
both legacy exact installed-version evidence and explicit affected-version
ranges. Range matching is opt-in through feed metadata (`version_scheme`,
`version_range`, or OSV-style `version_ranges`) with conservative ecosystem
defaults for Debian/APT, RPM-family packages, and common semver ecosystems;
`fixed_version` alone is not treated as an affected range. Findings retain match
provenance as `exact_installed_version` or `explicit_version_range` in evidence
so operators can distinguish curated exact matches from range-based feed
matches. The local vulnerability feed factory now normalizes OSV, GitHub
Security Advisory, NVD 2.0 CPE range, and CISA KEV exports into the same signed
feed schema before the offline-content factory signs the bundle. App-dependency
matching also carries PURL/CPE inventory evidence so upstream package and CPE
feeds can match SBOM-derived software, not only OS package names.

Problem: Offline CVE matching is exact-version only and depends on supplied signed feed content.

Build:

- Import NVD, CISA KEV, OSV, GitHub advisories, vendor distro advisories, Microsoft security updates.
- Version range semantics per ecosystem/distro.
- Feed freshness, provenance, rollback, airgap bundle signing.

Acceptance:

- Node package inventory produces credible CVE findings without hand-built exact version feeds.

### C1-CVE-002: SBOM and App Dependency Inventory

Priority: P0

Status: Closed for P0 go-live. The control plane now has durable app dependency inventory
through `node_app_dependencies`, with agent-facing ingest and operator readback
at `/api/v1/nodes/{node_id}/app-dependencies`. Dependency rows capture app root,
ecosystem, package manager, manifest path, scope, license, PURL/CPE, and
metadata so SBOM/manifests can be tied back to services and app roots. The node
agent now runs a bounded read-only app dependency collector over configured app
roots, skips generated/heavy directories, parses npm lock/package manifests,
Python requirements, Go modules, NuGet project/package manifests, Maven/Gradle
manifests, and CycloneDX/SPDX JSON SBOMs, then replaces the node inventory
through the app-dependency endpoint. Offline vulnerability-feed matching now
includes those app dependencies in the same signed-feed path as OS packages,
enabling npm/PyPI/Go/NuGet/Maven-style findings when feeds provide exact or
explicit range evidence. AI investigations can also call
`node_app_dependencies` to cite app roots, manifests, scopes, and PURLs next to
node vulnerabilities. Container-runtime image SBOM discovery, deeper
service/listener-to-app-root correlation, dedicated UI flows for "which
internet-facing app is affected by CVE-X?", and feed-factory automation remain
post-P0 expansion.

Problem: OS packages alone miss Java/.NET/Node/Python/container vulnerabilities in bank apps.

Build:

- SBOM ingestion/generation for containers, Java, .NET, Node, Python, Go.
- Map app roots and services to dependencies and CVEs.

Acceptance:

- Control One can answer "which internet-facing app is affected by CVE-X?"

### C1-PATCH-001: Bank-Grade Patch Orchestration

Priority: P0

Status: Closed for P0 go-live. Patch deployments already route through direct/proxy/airgapped
agent jobs, tenant change-window/circuit-breaker/approval gates, per-node
receipts, maintenance windows, and approval redispatch. The latest hardening
adds an explicit dry-run patch plan at `/api/v1/patch/deployments/plan`, plus
canary and wave metadata on deployment creation. Deployment creation dispatches
only wave 0, preserving the full planned node set in the deployment summary;
`POST /api/v1/patch/deployments/{id}/advance` dispatches the next wave only
after existing dispatched nodes are no longer pending or failed. Patch job
payloads now carry package allowlists/denylists and post-patch rescan intent;
the agent enforces allowlist upgrades for apt/dnf/yum/proxy/airgapped apt
paths, fails closed when a denylist cannot be safely enforced, and successful
post-patch jobs can request a fresh full package inventory on the next
heartbeat. Full OS package inventory replacement and app-dependency inventory
replacement now automatically refresh signed-feed vulnerability findings for the
node, so post-patch rescans flow into current CVE state without manual feed
reimport. Richer preflight health/drain hooks, reboot/rollback rules, and
additional UI controls for wave advancement remain post-P0 hardening.

Problem: Current agent runs package-manager upgrade commands; banks need controlled waves.

Build:

- Preflight, canary, maintenance windows, drain hooks, service health checks, rollback/reboot rules, repository allowlists, package pinning, emergency/KEV flow.
- Post-patch inventory and vulnerability rescan.

Acceptance:

- A patch deployment can be previewed, approved, rolled out by wave, verified, and audited.

### C1-REM-001: Remediation Contract Framework

Priority: P0

Status: Closed for P0 go-live. Added a provider-neutral `action_plans` and append-only
`action_receipts` contract with tenant-scoped API coverage at
`/api/v1/action-plans` and `/api/v1/action-plans/{id}/receipts`. The contract
stores scope, operator-readable diff, approval intent, maintenance window,
rollback plan, verification plan, source references, and immutable execution
receipts. Patch dispatch now creates a durable per-node action plan for each
executed patch job, injects the action plan id into the job payload, and turns
agent heartbeat success/failure into an action receipt while mirroring the
latest state back to the plan. Webserver inventory/plan/apply/blocklist/rollback
jobs now attach an action plan id to their stored policy and completion mirrors
domain-specific config receipts into the same generic receipt stream. AI
LogFixer node-local plan/apply/rollback actions now do the same around the
existing AI LogFixer action/run records. Host firewall add/delete enforcement
jobs also now create action plans and mirror validated heartbeat receipts into
the generic stream. Compliance remediation scripts, including manual,
auto-triggered, and rollback jobs, now carry action plan ids and emit generic
execution receipts without storing script bodies in the shared diff. Additional
DB/Kubernetes/cloud/private-access adapters and richer unified-timeline UI are
post-P0 provider expansion.

Problem: Remediation exists across firewall, scripts, webservers, patch, and AI LogFixer, but needs one contract model.

Build:

- Unified plan/approve/execute/receipt/verify/rollback schema.
- Provider adapters for host, webserver, DB, Kubernetes, cloud, private-access provider.
- Guardrails: no destructive default, blast-radius budget, circuit breaker, approval, dry run.

Acceptance:

- Every auto-remediation action has a durable plan, operator-readable diff, immutable receipt, and verification result.

### C1-REM-002: DB/Application Remediation Packs

Priority: P1

Status: Not a P0 go-live blocker. Webserver remediation has advanced beyond
proposal-only with bounded config plan/apply/rollback and blocklist actions,
approval plans, receipts, validation, reload controls, and circuit breakers.
Broader DB-specific packs such as kill long query, index recommendations,
vacuum/analyze guidance, and connection-pool actions remain P1 expansion rather
than a bank go-live gate.

Problem: DB query telemetry exists, but DB/app remediation is mostly proposal-only.

Build:

- Postgres/MySQL/MSSQL safe actions: kill long query, recommend index, rotate slow query logging, vacuum/analyze hints, connection pool guidance.
- App packs: webserver config rollback, service restart with health checks, queue drain checks, cache eviction guardrails.

Acceptance:

- AI can recommend and, after approval, execute bounded app/DB remediation with receipts.

### C1-NET-001: Private Access Provider Abstraction

Priority: P0

Status: Closed for P0 go-live. Added a provider-neutral `internal/privateaccess` contract for
NetBird, Headscale, and OpenZiti snapshots covering peers, groups, policies,
routes, services, connector health, and audit events. Provider snapshots are now
persisted per tenant/provider/account through `/api/v1/private-access/snapshots`
and stored with provenance timestamps. The control plane now also has durable
private-access provider accounts with encrypted `provider_credentials`
references, redacted account config, import-run receipts, manual payload import,
live HTTP import jobs, and an opt-in due-account scheduler. First-pass provider
adapters normalize NetBird peers/groups/policies/routes/events, Headscale
nodes/routes/users/ACL exports, and OpenZiti identities/services/service
policies/edge-router health into the shared snapshot model. The package also
includes the first deterministic exposure reconciler so provider adapters feed
one common model before UI-specific workflows. NetBird OOB deployment artifacts
now live under `deploy/private-access/netbird/`; richer control-room UI and
deeper provider-specific policy semantics remain post-P0 expansion.

Problem: Host firewall management is brittle for fleet-level private access and should not become a VPN product.

Build:

- Provider model: `netbird`, `headscale`, `openziti`.
- Resources: identity, peer/node, group/tag, policy, route/subnet/resource, service, connector health, audit event.
- Reconcile with Control One nodes/services/firewall exposure.

Acceptance:

- Control One can show "this server is unreachable publicly but reachable through approved private-access policy X."

### C1-NET-002: NetBird OOB Bundle and Adapter

Priority: P0

Status: Closed for P0 go-live. The backend accepts NetBird provider
credentials through the shared encrypted provider-credential store, stores
NetBird provider accounts, imports NetBird API/export payloads through
provider-specific normalization, persists import-run receipts, and can schedule
live HTTP import jobs against configured NetBird management endpoints. Added the
out-of-box self-host deployment bundle under `deploy/private-access/netbird/`
with an operator `.env` template, reviewed-official-installer wrapper,
Control One provider-account payload, production policy intent templates, and
HA/relay/backup/runbook guidance. Follow-on enhancements are customer-specific
IdP exports and automated multi-node HA deployment tests; the P0 path now has
the artifacts needed for a bank to deploy, integrate, and evidence NetBird.

Why NetBird first:

- Self-hostable.
- WireGuard mesh with Management, Signal, Relay.
- Access controls and groups.
- Routing peers for private subnets/resources where agents cannot be installed.
- Good fit for "VPN access to environment while servers are not public."

Build:

- Airgap/self-host deployment bundle with HA notes, external DB/backup
  considerations, relay placement guidance, and IdP/local auth guidance:
  `deploy/private-access/netbird/`.
- Adapter to import peers, groups, policies, routing peers, routes/resources,
  activity logs.
- Control One policy templates: no all-to-all, admins to management ports, app
  team to app subnets, DB admins to DB ports, break-glass:
  `deploy/private-access/netbird/policy-templates.json`.

Acceptance:

- A bank can deploy NetBird beside Control One, register the provider account,
  schedule imports, and get private admin access evidence without opening server
  ports to the internet.

### C1-NET-003: OpenZiti OOB Bundle and Adapter

Priority: P1, P0 for customers prioritizing dark services.

Status: Closed for P0 dark-service go-live. Added first-pass OpenZiti adapter
normalization for identities, services, service policies, edge-router health,
and audit payloads, with the same provider-account/import-run/scheduler path
used by NetBird and Headscale. The out-of-box bundle now lives under
`deploy/private-access/openziti/` with reviewed official-installer staging,
Control One provider-account payload generation, a signed-offline-compatible
provider manifest, ZAC/controller/router/tunneler guidance, HA/backup notes, and
bank service templates for SSH, RDP, admin UI, database, application, bind, dial,
and break-glass policy intent. Provider manifests for NetBird, OpenZiti, and
Headscale are validated in `controlplane/internal/offlinebundle` tests.

Why:

- Better than VPN for app-private access when services should have no reachable ports.
- Supports tunnelers for brownfield apps and SDK embedding for highest-security workloads.

Build:

- Bundle controller/edge routers/tunnelers/ZAC with HA guidance.
- Adapter to import identities, services, policies, routers, audit.
- Service templates for SSH/RDP/admin UI/DB/app endpoints.

Acceptance:

- Control One can publish an app/DB/admin service through OpenZiti so it remains unreachable from outside except to authorized identities.

### C1-NET-004: Headscale OOB Bundle and Adapter

Priority: P1

Status: Closed for go-live support. Added first-pass Headscale adapter
normalization for nodes/machines, users/namespaces, routes, and ACL exports,
with encrypted credential references and scheduled import jobs via provider
accounts. The out-of-box bundle now lives under
`deploy/private-access/headscale/` with OIDC/preauth/ACL-file guidance, reviewed
install-note staging, Control One provider-account payload generation, a
signed-offline-compatible provider manifest, route-approval intent, ACL
templates for management/app/database/break-glass access, and HA/backup notes.
Provider manifests for NetBird, OpenZiti, and Headscale are validated in
`controlplane/internal/offlinebundle` tests.

Why limited:

- Self-hosted control server for Tailscale clients; useful where teams already know that model.
- More DIY and less enterprise-packaged than NetBird for this product's bank go-live.

Build:

- Bundle Headscale with OIDC, ACL file management, preauth key workflows, route approval import.
- Adapter to read users/nodes/routes/ACLs and surface route approval/access gaps.

Acceptance:

- Headscale can be supported without Control One depending on Tailscale SaaS.

### C1-NET-005: Exposure Reconciliation

Priority: P0

Status: Closed for P0 go-live. The provider-neutral reconciler classifies discovered
services as `publicly_exposed`, `private_access_only`,
`unmanaged_private_service`, or `policy_drift` by comparing service
reachability against private-access services/routes/policies. It also flags
broad routes such as `0.0.0.0/0` and stale peers as provider policy drift. The
control plane now persists reconciled findings in
`private_access_exposure_findings` and exposes
`/api/v1/private-access/exposure/reconcile` plus
`/api/v1/private-access/exposure/findings`, using current `node_services` as
the first observation source. Provider import completion now automatically
refreshes snapshots and runs exposure reconciliation, so adapter snapshot jobs
feed the same findings table. Reconciliation now enriches service observations
with node public IP context and host-firewall posture, including fresh
default-deny suppression, public allow-rule evidence, and persisted finding
evidence for public-path/firewall details instead of relying only on wildcard
listener heuristics. Reconciliation now also consumes explicit cloud/NAT/LB
exposure labels and active cluster load-balancer registrations, carrying
`cloud_public_ip`, `public_load_balancer`, `public_nat_ingress_label`, public
port scope, LB DNS, and LB registration evidence into findings. Operators can
open a cited SOC case from a finding through
`POST /api/v1/private-access/exposure/findings/{id}/soc-case`; the case carries
`private_access_exposure_findings:{id}` evidence refs and audit metadata.
Remaining enhancement: richer control-room visualization.

Problem: The product must prove servers are unreachable from the outside but reachable through approved private paths.

Build:

- Compare discovered listening services, host firewall state, public IP/NAT/LB data, NetBird/Headscale/OpenZiti policies.
- Produce gaps: public listener, no default-deny, private-access route too broad, route bypasses policy, server not enrolled, stale peer.

Acceptance:

- Control room can show "publicly exposed," "private-access only," "unmanaged," and "policy drift" per server/service.

### C1-PLAT-001: Bank HA/DR and On-Prem Hardening

Priority: P0

Status: Closed for P0 go-live. Doris migration application now supports an explicit
`doris.replication_num` setting and rewrites embedded Doris DDL at apply time,
so dev/single-node clusters can keep replication `1` while bank production
clusters apply the same migrations with replication `3`. Control-plane example,
dev, and deploy configs now surface that setting, and
`docs/bank-ha-dr-runbook.md` defines the minimum production HA/DR posture,
backup schedule, restore drill, and go-live gates. Added executable
`scripts/bank_ha_dr_drill.sh` automation for Postgres/offline-content backup
evidence, non-destructive restore manifest validation, explicit isolated restore
apply, control-plane/Doris failover health checks, and private-access reconcile
smoke evidence. Remaining customer activity: run the drill in each target bank
environment and attach the generated evidence to go-live approval.

Problem: Some analytic migrations use replication number 1; deployment docs are not yet bank HA/DR-grade.

Build:

- HA reference architecture for Postgres, Doris, object storage, control plane, edge collectors, offline content.
- Backup/restore drills, DR runbook, RPO/RTO targets, retention, tenant isolation.
- FIPS-capable crypto posture where possible, HSM/KMS/PKI integration.

Acceptance:

- Sales can answer bank architecture review questions with deployment diagrams and tested failover results.

### C1-PLAT-002: Identity, RBAC, Audit, and Segregation of Duties

Priority: P0

Status: Closed for P0 go-live. Generic action-plan creation now enforces the approval
lifecycle instead of allowing API callers to create plans directly in
`approved`, `queued`, running, or terminal states. High/critical-risk action
plans created through the public API must start in `needs_approval`, declare
approver roles, require `min_approvers >= 2`, and carry
`separation_of_duties=true`; the server also stamps the creator subject into
approval/source metadata for audit evidence. Generic action plans now have an
append-only `action_plan_approvals` audit table plus
`/api/v1/action-plans/{id}/approvals`: approvers are role-checked, duplicate
approver records cannot count twice, plan creators cannot self-approve when
separation of duties is required, denials cancel the plan, and the final
required approval transitions the plan to `approved`. Existing domain-specific
approval flows and append-only receipts remain the execution path for
privileged changes. SAML/SCIM/group-sync hardening, immutable audit export
bundles, and broader legacy-surface policy unification remain post-P0
enterprise hardening, while go-live privileged remediation and connector
enablement paths now have auditable role/approval gates.

Problem: Banks need strict operator controls.

Build:

- SAML/OIDC, SCIM/group sync, MFA enforcement, break-glass, dual approval for dangerous actions.
- Immutable audit exports and evidence bundles.

Acceptance:

- No privileged remediation or connector enablement happens without auditable role/approval policy.

### C1-PLAT-003: Airgapped Content and Upgrade Factory

Priority: P0

Status: Closed for P0 go-live. Offline content bundles are signed Ed25519 archives with
manifest SHA-256 verification, per-content SHA-256 checks, downgrade and bundle
expiry rejection, filesystem activation/rollback receipts, Postgres import/audit
records when storage is available, and active-content sync for vulnerability
feeds plus SIEM content packs with parser/detection replay quarantine. Status
listing and enrichment now dynamically mark content stale after its
`expires_at`, not only at import time, so disconnected banks get stale-content
warnings as feeds age. Offline bundles now also have a validated
`private_access_provider_manifest` content type with active-manifest loading for
NetBird, Headscale, and OpenZiti provider manifests, and the NetBird OOB bundle
ships a compatible manifest at `deploy/private-access/netbird/provider-manifest.json`.
The signed bundle verifier also validates `remediation_pack` and
`ai_investigation_pack` manifests before activation, including high/critical
dual-control requirements for remediation actions and citation/guardrail
requirements for AI investigation tools. Example manifests live under
`deploy/offline-content/`. Added `controlplane/cmd/offline-content-factory`,
which computes content hashes, signs the final manifest with Ed25519, writes the
archive, prints the public-key fingerprint, and self-verifies with the same
server-side verifier before release. Remaining work: expanded operator-facing
upgrade runbook polish for customer-specific change windows.

Problem: On-prem banks may require offline operation and controlled updates.

Build:

- Signed bundles for parsers, detections, CVE feeds, private-access provider
  manifests, AI/remediation packs.
- Import validation, rollback, provenance, stale-content warnings.

Acceptance:

- A bank can run disconnected for a defined period and still receive signed updates through its offline process.

## Drop / Defer

| Item | Decision | Reason |
| --- | --- | --- |
| Native Tailscale integration | Drop now | User direction; SaaS dependency story is awkward for on-prem bank sales. |
| Building Control One VPN/microsegmentation | Avoid | NetBird/Headscale/OpenZiti already own packet/private-access planes. Control One should orchestrate and verify. |
| Broad host firewall policy as primary private access | Defer/reduce | Retain firewall actions for remediation and exposure reduction, but do not use it as the core fleet access model. |
| Claiming "hundreds of connectors" | Avoid | Current code has catalog breadth, not ingestion/parser breadth. |
| Silent auto-collection of high-risk bank logs | Avoid | Privacy, volume, and operational-risk issue. Use policy + approval + coverage truth. |

## Recommended Sales-Ready Architecture

2026-06-06 sizing update: for demos and small fleets, use the lightweight
analytics profile by default: Postgres for durable facts/rollups, Redis for hot
counters and coordination, and a bounded embedded SQLite/WAL analytic store for
raw connection-history/top-talker slices as it lands. Doris remains valuable for
large retention windows and concurrent analytic queries, but only as an opt-in
OLAP tier on dedicated analytics capacity.

1. Control One core on prem:
   - Small fleet/demo: control plane, Postgres, Redis, embedded SQLite
     analytics, object storage, worker, UI, offline content store.
   - Bank HA/OLAP: dedicated Postgres HA, object storage, worker tier, UI,
     offline content store, and optional Doris/OLAP cluster sized separately.
   - Edge collector tier for syslog/CEF/LEEF/OTLP/WEF/vendor API connectors.

2. Node agents:
   - Host metrics, process, connections, services, packages, firewall state, approved log/file/DB collectors.
   - Disk spool and connector state sync.

3. Auto-connect workflow:
   - Discover services/apps/log candidates.
   - Generate connector proposals.
   - Apply bank policy and approval gates.
   - Start collection.
   - Prove coverage and parser health.

4. Private access:
   - NetBird for VPN-like admin/team access and routed private networks.
   - OpenZiti for dark app/DB/admin services.
   - Headscale only where customer already prefers the tailnet model.
   - Control One reconciles private-access policies with exposure and service discovery.

5. AI intelligence:
   - Evidence-grounded investigations over SIEM + observability + CVE + patch + private-access state.
   - Proposal-first remediation, with approvals, receipts, verification, rollback.

## Near-Term Build Order

1. C1-SIEM-001/002/003: coverage truth, connector proposal, safe auto-connect.
2. C1-SIEM-004/005/006/007: syslog/CEF/LEEF, WEF, OTel/Alloy, durable spool.
3. C1-NET-001/002/005: provider abstraction, NetBird OOB support, exposure reconciliation.
4. C1-CVE-001/002 and C1-PATCH-001: credible vulnerability and patch story.
5. C1-SIEM-008/009/010/012: parser packs, detections, normalization, appliance packs.
6. C1-PLAT-001/002/003: bank-grade HA/DR, identity, airgap factory.

## Primary References

Code evidence:

- Agent collectors: `cmd/nodeagent/main.go`, `internal/eventstream/*`, `internal/procmon/*`, `internal/netflow/*`, `internal/fileaccess/*`, `internal/dbquery/*`.
- Log opt-in behavior: `internal/config/config.go`, `internal/telemetry/telemetry.go`, `internal/telemetry/logs/presets.go`, `internal/telemetry/logs/presets_test.go`.
- App catalog and explicit auto-collect tests: `internal/appcatalog/catalog.go`, `internal/appcatalog/catalog_test.go`.
- Event/log ingest and storage: `controlplane/internal/server/events_ingest.go`, `controlplane/internal/server/telemetry.go`, `controlplane/internal/doris/migrations/*`.
- Rules/correlation: `controlplane/internal/server/rules.go`, `controlplane/internal/correlation/engine.go`.
- CVE/patch/remediation/AI: `controlplane/internal/offlinebundle/vulnerability_feed.go`, `controlplane/internal/server/vulnerability_feed_import.go`, `cmd/nodeagent/patch_exec.go`, `controlplane/internal/server/node_vulnerability_patch_plan.go`, `controlplane/internal/server/ai_investigation_tools.go`, `controlplane/internal/server/ai_logfixer.go`.
- Private-access/firewall/webserver surfaces: `internal/firewall/*`, `cmd/nodeagent/firewall*.go`, `internal/webservercontrol/manager.go`, `cmd/nodeagent/webserver_exec.go`.

External references:

- Microsoft Sentinel data connectors: https://learn.microsoft.com/en-us/azure/sentinel/data-connectors-reference
- Elastic integrations: https://www.elastic.co/docs/reference/integrations
- Splunk supported add-ons: https://help.splunk.com/en/splunk-cloud-platform/get-data-in/splunk-supported-add-ons/about-the-splunk-supported-add-ons
- Google Security Operations supported parsers: https://cloud.google.com/chronicle/docs/ingestion/parser-list/supported-default-parsers
- OpenTelemetry Collector receivers: https://opentelemetry.io/docs/collector/components/receiver/
- Grafana integrations: https://grafana.com/docs/grafana-cloud/monitor-infrastructure/integrations/
- New Relic infrastructure integrations: https://docs.newrelic.com/docs/infrastructure/introduction-infra-monitoring/
- NetBird architecture/access/routing docs: https://docs.netbird.io/about-netbird/how-netbird-works, https://docs.netbird.io/manage/access-control, https://docs.netbird.io/manage/networks/how-routing-peers-work
- Headscale features/routes/ACLs: https://headscale.net/0.28.0/about/features/, https://headscale.net/0.26.0/ref/routes/, https://headscale.net/0.28.0/ref/acls/
- OpenZiti network access/tunnelers: https://openziti.io/docs/learn/core-concepts/zero-trust-models/ztna/, https://openziti.io/docs/reference/tunnelers/, https://github.com/openziti/ziti
