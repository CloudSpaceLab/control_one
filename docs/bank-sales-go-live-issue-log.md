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

2026-06-06 small-analytics implementation follow-up: the first embedded
SQLite/WAL slice is now implemented behind small mode without removing the
Doris OLAP path. `analytics.sqlite_dir` opens a local read store, connection
ingest appends normalized `process_connections` rows from the existing fanout,
and `/api/v1/connections`, `/api/v1/connections/{conn_id}`, and
`/api/v1/connections/top-talkers` can return `source=small-analytics` instead
of the previous pending guardrail when the store is configured. Deploy config
now mounts `/var/lib/control-one/analytics` into the controlplane container.
Focused validation passed:
`go test ./controlplane/internal/smallanalytics`,
`go test ./controlplane/internal/config`,
`go test ./controlplane/internal/doris ./controlplane/internal/storage`,
`go test ./controlplane/cmd/controlplane`, and targeted server tests for
small-mode connections/top-talkers plus graceful pending fallback. Additional
`CGO_ENABLED=0` checks passed for `./controlplane/internal/smallanalytics` and
`./controlplane/cmd/controlplane`, preserving the current deploy build shape. A full
`go test ./controlplane/internal/server` still requires the repo's local
Postgres integration database at `localhost:5432/controlone_test`; in the
current workstation state those integration tests fail before exercising this
change because Postgres is not listening.

Live deployment follow-up for commit `8b29ae2f`: production deploy run
`27075192642` succeeded and cross-compiled the controlplane with the pure-Go
SQLite driver. The first live boot exposed an operations bug rather than a code
path bug: `/opt/control-one/deploy/analytics` was root-owned while the
controlplane image runs as uid `65532`, so logs showed
`small analytics sqlite store unavailable` with `unable to open database file`.
The live directory was corrected to `65532:65532`/`750`, the controlplane was
restarted, and logs then showed `small analytics sqlite store ready` for
`/var/lib/control-one/analytics`. The deploy workflow and manual `deploy.py`
path now create/chown/chmod that analytics directory before restart.

Post-fix live evidence:

- `docker compose ps` showed console/controlplane up, Redis healthy, and no
  Doris services running in the small profile.
- `/opt/control-one/deploy/analytics` contained
  `controlone-small-analytics.db`, `-shm`, and `-wal` files owned by uid 65532.
- Public `/healthz` returned `HTTP 200` in about 1.32s.
- Authenticated API checks for the default tenant returned
  `/api/v1/connections` with `source=small-analytics`, 5 rows, about 342 ms;
  `/api/v1/connections/top-talkers` with `source=small-analytics`, 5 rows,
  about 361 ms; and `/api/v1/connections/{conn_id}` with
  `source=small-analytics`, a real connection body, about 294 ms.
- Browser checks on
  `/console/security/network?tab=connections&verify=8b29ae2f` showed the
  Connections table with live rows instead of pending copy, zero current
  console warnings/errors, current app API calls returning `200`, and
  document-level horizontal overflow of `0` at both 381px and 1430px widths.
- Follow-up ownership hardening commit `c71263eb` deployed successfully in run
  `27075444975`; CI runs `27075444960` and `27075444961` also succeeded.
  Post-deploy logs still showed `small analytics sqlite store ready`, the
  analytics directory stayed owned by uid 65532, and authenticated
  connections/top-talkers checks continued returning `source=small-analytics`.

2026-06-06 follow-up: the corrected workflow is now on `main` and deploy runs
`27065886666`, `27066201594`, and `27066409247` succeeded with Doris disabled.
The small-fleet architecture pass also made the deployment contract explicit:
`deploy/docker-compose.yaml` keeps Doris FE/BE behind the `olap` profile,
`.env.example` defaults to `ANALYTICS_MODE=small` and `DORIS_ENABLED=false`,
and `deploy.py`, `bootstrap.sh`, and the GitHub production deploy workflow skip
Doris host prerequisites/bootstrap unless OLAP is selected. This preserves the
Doris feature path without letting the demo stack start memory-heavy services by
accident.

2026-06-06 deploy-contract correction: commits `c90298d0` and `41aca30e`
made the Doris opt-in profile executable from the full deploy path and reduced
CI race-test runner pressure. Production deploy runs `27072903352` and
`27073047055` succeeded. The latest follow-up CI runs `27073047058` and
`27073047060` also succeeded. Final live verification showed
`ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, Compose profile `olap`, console
and controlplane up, Redis healthy, Doris FE/BE stopped, direct control-plane
health returning `ok`, and public `/healthz` returning `HTTP 200` in about
0.64s.

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

2026-06-06 Observability responsive follow-up: an authenticated desktop sweep
across 41 live console routes found no app HTTP 4xx/5xx responses, no browser
console warnings/errors, no page crashes, and no document-level horizontal
overflow, but it caught the Observability Stack Map table forcing the `Next
action` and `Open` columns behind an internal horizontal scroll at 1440px.
Commits `2d0c4c9f` and `a9a84464` keep the same Stack Map evidence/actions
while wrapping dense evidence text, showing all columns at desktop width, and
preserving an internal table scroller on mobile. Local `npm run lint`,
`npx vitest run src/pages/Observability.test.tsx --coverage=false`, and
`npm run build` passed. Deploy runs `27073845351` and `27074130774` succeeded;
CI runs `27073845345`, `27073845353`, `27074130772`, and `27074130786`
succeeded. Final live browser verification on
`/console/observability?verify=a9a84464-*` showed desktop `docOverflowX=0`,
all Stack Map columns visible, mobile `docOverflowX=0` with table scroller
`306/646`, zero HTTP failures, zero console warnings/errors, and no lingering
loading text.

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

2026-06-06 worker durability implementation follow-up: the Asynq caveat is now
closed in the local tree for persisted `jobs` rows. Worker tasks can carry a
serialized durable job reference (`job_id` plus `job_type`) instead of relying
only on an in-memory closure name. The server registers a wildcard durable job
handler that reloads the persisted job row, validates the type, and runs the
existing audited job lifecycle. Attempts now derive from persisted
`jobs.retries`, so restart/retry accounting survives process replacement.
Generic API-created jobs, private-access import jobs, scheduled compliance
scans, and scheduled health jobs now enqueue durable references while retaining
the in-process function for the memory worker backend. Local verification
passed for `go test ./controlplane/internal/worker`, focused server
job/scheduler tests, Go vet on the touched worker/server/controlplane packages,
and the full short sweep `$env:GOMAXPROCS='4'; go test -short -p 1 ./...`.
Commit `127ede46` deployed successfully in run `27076037726`; CI runs
`27076037732` and `27076037727` also succeeded. Post-deploy SSH validation
showed the new control plane and console containers recreated, Redis healthy,
`worker manager started` with `backend=asynq`, `analytics backend selected`
with `mode=small`, and `small analytics sqlite store ready`. Authenticated API
checks returned worker backend `asynq`, `started=true`, queue depth `0`, no
last error, fleet health from `source=small-analytics-postgres`, connection
history from `source=small-analytics`, and jobs API rows. Browser verification
on `/console/jobs?verify=127ede46` and `/console/settings?verify=127ede46`
showed the Jobs page and System Health worker pool card rendering Asynq/Running
with queue depth `0`, document-level horizontal overflow `0`, current app API
requests returning `200`, and zero current console warnings/errors.
Manual recovery also showed that Windows-cross-compiled controlplane binaries
should not be used for live deploys until the Go runtime crash is investigated;
the live controlplane was recovered with the Linux-runner-built binary.
2026-06-07 CI/deploy correction: the Node.js 20 action-runtime warning from
the latest GitHub runs was reproduced through check-run annotations. Commits
`11aa1933` and `293a7661` opt the workflows into
`FORCE_JAVASCRIPT_ACTIONS_TO_NODE24=true`, upgrade the
GitHub/Docker/Azure/GoReleaser/golangci actions that have Node 24 majors, move
SARIF upload to `github/codeql-action/upload-sarif@v4`, replace
`actions/create-release@v1` with `softprops/action-gh-release@v3`, pin Trivy to
`aquasecurity/trivy-action@v0.36.0` instead of mutable `master`, and pin the
Windows matrix to `windows-2025-vs2026` after GitHub warned that
`windows-latest` will redirect there on 2026-06-15. Local `actionlint` and
`git diff --check` passed. GitHub deploy run `27076776221` and CI runs
`27076776220`/`27076776217` succeeded, and every check run on those three runs
reported zero annotations. Post-deploy SSH/browser-edge checks showed
`/healthz` returning `ok`, Redis healthy, no Doris FE/BE services in the small
profile, logs with `worker manager started`, `analytics backend selected`
`mode=small`, and `small analytics sqlite store ready`, and public
`https://control-one.cloudspacetechs.com/healthz` returning `ok`.

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
  SQLite/WAL analytics store for lightweight connection-history reads. The
  first local slice now covers connection list/detail and top talkers when
  `analytics.sqlite_dir` is configured. Live server logs from the earlier
  deployed small-mode pass show `analytics backend selected` with `mode=small`;
  this new SQLite-backed slice still needs post-deploy live API/browser
  verification before it is counted as live evidence.
- 2026-06-07 architecture decision: keep the demo host on the hyper-light
  Redis+SQLite small profile and treat Doris as opt-in OLAP only. The intended
  integration is additive: Postgres remains canonical, Redis stays
  TTL-bounded/non-evidentiary, SQLite/WAL serves recent evidence-grade analytic
  reads, and Doris adapters are preserved behind the same API contracts for
  larger fleets. Remaining small-mode work is to broaden event/timeline coverage
  beyond connection facts and route entity enrichment, log-volume, Redis
  acceleration, and admin health copy away from Doris-specific assumptions
  without removing UI features.
- 2026-06-07 small-fleet architecture refresh: `docs/small-fleet-analytics-architecture.md`
  now records the compact Redis+SQLite blueprint explicitly. The demo target is
  one lightweight analytic process inside the controlplane, Postgres as the
  replayable source of truth, Redis for bounded hot state, SQLite/WAL for recent
  cited evidence reads, and Doris at 0 MB in the default profile. The go/no-go
  standard is not "can Doris be tuned enough for a demo"; it is whether the
  console can show dashboards, connections, investigation timelines, recent
  search, and exports from `source=small-analytics` with bounded latency,
  restart survival, and journal replay.
- 2026-06-07 small-analytics hardening follow-up: live control-plane logs
  exposed `SQLITE_BUSY` / `database is locked` during small-mode connection
  fanout. Commit `c4921f86` keeps the Redis+SQLite feature path and hardens the
  embedded SQLite store by applying WAL/busy-timeout/synchronous/temp/cache
  pragmas through the driver DSN for every pooled connection, using immediate
  transaction locking, reducing the pool to a small bounded set, and
  serializing in-process writes. Local checks passed:
  `go test ./controlplane/internal/smallanalytics -count=1`, focused server
  coverage for `TestSmallAnalyticsSQLiteServesConnectionsAndTopTalkers`,
  `go vet ./controlplane/internal/smallanalytics ./controlplane/internal/server`,
  and `go test -short -p 1 ./...`; `go test -race` could not run on this
  Windows workstation because `gcc` is not installed for CGO. Deploy run
  `27078299083` and CI runs `27078299078`/`27078299097` succeeded. Post-deploy
  live checks showed `ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, Redis
  healthy, controlplane about 70 MiB, `/healthz=ok`, no recent
  `SQLITE_BUSY`/lock warnings, and an authenticated browser/API pass through
  `/console/security/network?verify=c4921f86-smallanalytics` where connection
  list, top talkers, and connection detail all returned HTTP 200 with
  `source=small-analytics`, zero app API failures, zero console warnings/errors,
  zero page errors, and no document horizontal overflow.
- 2026-06-07 small-mode investigation architecture follow-up: the Redis+SQLite
  path should also keep investigation workflows alive instead of returning a
  Doris-only 503. The local implementation now projects SQLite
  `process_connections` into cited `conn.open`/`conn.close` rows for
  `/api/v1/events/query`, `/api/v1/timelines/build`, and the matching AI
  investigation tools when `analytics.mode=small`; explicit OLAP mode still
  fails loudly if Doris is configured but unavailable, preserving the large
  fleet backend contract. This is deliberately additive: Doris remains the
  opt-in full generic/file/db/web timeline backend, while the demo gets
  memory-light connection-fact investigations from SQLite. Local checks passed:
  `go test ./controlplane/internal/smallanalytics -count=1`, focused
  investigation server tests for small-mode HTTP/tool events and timelines,
  `go vet ./controlplane/internal/smallanalytics ./controlplane/internal/server`,
  and `go test -short -p 1 ./...`. A non-short package test run for
  `./controlplane/internal/smallanalytics ./controlplane/internal/server`
  reached unrelated integration tests that require a local Postgres test
  database on `localhost:5432`; the smallanalytics package passed, while the
  server integration subset was blocked by the missing local DB.
- 2026-06-07 live closeout for commit `a2ed0672`: deploy run
  `27078952592` succeeded and CI runs `27078952584`/`27078952587` succeeded.
  Post-deploy host checks showed `ANALYTICS_MODE=small`,
  `DORIS_ENABLED=false`, Redis healthy, no Doris containers in the running
  stack, `/healthz=ok`, and controlplane memory around 92 MiB. Recent
  control-plane logs showed the small SQLite store ready and no
  `SQLITE_BUSY`, `database is locked`, panic, fatal, or analytic-store
  unavailable errors; remaining warnings were the known mock provisioning/
  compliance clients and an external TOR feed 403. Authenticated browser/API
  validation on the live tenant showed `/api/v1/connections` returning HTTP 200
  with `source=small-analytics`, `/api/v1/events/query` returning HTTP 200 with
  `source=small-analytics`, one cited `conn.open` row in 490 ms, and
  `/api/v1/timelines/build` returning HTTP 200 with `source=small-analytics`,
  ten cited connection timeline rows in 662 ms. Browser validation on
  `/console/investigate?verify=a2ed0672-ui-clean` and
  `/console/investigate/ip/158.94.211.49?verify=a2ed0672-entity` showed all
  Control One app API requests at HTTP 200, zero console warnings/errors, no
  document horizontal overflow, and no Doris/analytic-store unavailable copy.
  The only failed network entries on the entity page were Cloudflare RUM aborts,
  not Control One API failures.
- 2026-06-07 mobile/small-mode console sweep follow-up: an authenticated live
  desktop sweep across 23 high-risk console routes found all Control One app
  API requests at HTTP 200, zero console warnings/errors, zero page errors, no
  document horizontal overflow, and no visible Doris, analytic-store
  unavailable, or `small-analytics-pending` copy. The paired 390x844 mobile
  sweep found real clipped controls in long tab strips and the patch deployment
  table: Settings, Network, Compliance, and Patch Management could keep
  useful dense operational data, but needed internal horizontal scroll rather
  than being clipped by the shell. Commit `70d21172` preserves the existing
  features and makes shared tab lists and patch-operation tables scroll inside
  their own controls. Local checks passed `npm --prefix ui run build`,
  `npm --prefix ui run lint`, `npm --prefix ui test`, and `git diff --check`;
  the attempted `npm --prefix ui test -- PatchManagement --runInBand` was not
  useful because there is no PatchManagement test file and npm warns
  `--runInBand` is not a recognized config. Deploy run `27079554690` and CI
  runs `27079554696` and `27079554691` succeeded after rerunning the initially
  flaky frontend-test job. Post-deploy host checks showed `/healthz=ok`,
  `ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, Redis healthy, no Doris
  containers in the hot path, controlplane memory about 91 MiB, console about
  4 MiB, and Redis about 11 MiB. Live browser validation on
  `/console/settings`, `/console/infrastructure/patch`,
  `/console/security/network`, and `/console/compliance` at 390x844 showed no
  document horizontal overflow, no unscrollable controls extending beyond the
  viewport, no app API failures, zero console warnings/errors, zero page
  errors, and no Doris/analytic-store unavailable copy; intentionally wide
  tables and tab buttons are now reachable through their local horizontal
  scrollers.
- 2026-06-07 Users/RBAC follow-up: live browser validation on
  `/console/users?verify=2773897d` found duplicated effective role labels for
  default local users (`viewer viewer`, `operator operator`, `ciso ciso`,
  `admin admin`). Commit `2773897d` keeps all useful grants but dedupes
  effective role names in storage/API serialization. Local focused checks,
  `go vet` on the touched packages, `GOMAXPROCS=4 go test -short -p 1 ./...`,
  and `npm --prefix ui run build` passed. Deploy run `27077266406` and CI runs
  `27077266403`/`27077266404` succeeded. Post-deploy checks showed
  `/healthz` returning `ok`, app/console containers recreated, Redis healthy,
  Doris absent in the small profile, authenticated `/api/v1/users?limit=20`
  returning 6 users with `duplicate_role_count=0`, and the live Users page
  rendering single role chips with zero console warnings/errors and no document
  horizontal overflow at the checked desktop viewport.
- 2026-06-07 Roles/RBAC follow-up: live validation on
  `/console/roles?verify=a120fb03` found the canonical roles rendered as custom
  because the UI and delete guard still depended on older fixed role UUIDs.
  Commit `a120fb03` preserves custom-role creation/update/delete, but now marks
  and protects built-in roles by canonical role name (`admin`, `ciso`,
  `investigator`, `operator`, `viewer`) in storage, API serialization, and UI
  fallback logic. Local checks passed:
  `npm --prefix ui test -- Roles.test.tsx`, focused server/storage RBAC tests,
  `go vet ./controlplane/internal/server ./controlplane/internal/storage`,
  `npm --prefix ui run build`, and `go test -short -p 1 ./...`. Deploy run
  `27077802393` and CI runs `27077802386`/`27077802384` succeeded. Post-deploy
  live API verification showed 5 roles, every canonical role returning
  `built_in=true` despite non-old IDs, no custom roles, and a safe attempted
  `DELETE /api/v1/roles/{admin_id}` rejection with `400 cannot delete built-in
  role`; the admin role remained present afterward. A fresh browser reload of
  `/console/roles?verify=a120fb03-live-ui` showed `BUILT-IN 5`, `CUSTOM 0`,
  built-in labels on all role headers, zero delete buttons for built-ins, seven
  app API responses at HTTP 200, zero console warnings/errors, zero page
  errors, and no document horizontal overflow.
- 2026-06-07 Jobs execution-mode transparency follow-up: live small-mode logs
  exposed mock provisioning/compliance clients while the Jobs UI still presented
  `provision.apply` and `compliance.scan` as ordinary dispatches. Commit
  `a32ea0bd` preserves the existing job catalog but adds explicit integration
  status to `/api/v1/worker/status`, queue evidence, and the Jobs submit UI:
  provisioning now reports simulated/non-mutating execution when no external
  provisioning API is configured, and compliance scans report the local policy
  evaluator when no external scanner is configured. The same pass fixed
  tenant-scoped built-in job forms so they require/default the selected tenant,
  move provisioning `template_version` into metadata, generate missing per-node
  `scan_id` values, route blank-node compliance scans through the batch scan
  endpoint, and preserve optional rule-set/policy facts in generated scan jobs.
  Local checks passed focused server RBAC/job/compliance tests,
  `npm --prefix ui test -- Jobs.test.tsx`, `npm --prefix ui run lint`,
  `npm --prefix ui run build`, and `git diff --check`; the full local
  `go test ./controlplane/internal/server` sweep still requires the local
  Postgres test database on `localhost:5432`, while GitHub service-backed CI
  covered the broader server suite. Deploy run `27080356963` and CI runs
  `27080356966`/`27080356962` succeeded. Post-deploy host checks showed
  controlplane around 92 MiB, console around 4 MiB, Redis around 11 MiB,
  `/healthz=ok`, Redis healthy, Doris absent in the small profile, and normal
  control-plane logs. Authenticated browser verification on
  `/console/jobs?verify=jobs-execution-mode-a32ea0bd` showed worker backend
  `ASYNQ`, queue depth `0`, visible badges for `Local policy evaluator` and
  `Simulated provisioning`, required tenant selection, correct compliance
  fields (`Scan ID`, `Node ID`, `Rule set`), all Control One app API requests
  at HTTP 200, and zero console warnings/errors. No state-changing live job was
  submitted during the final UI check.
- 2026-06-07 Secrets vault follow-up: code review and live read-only
  validation on `/console/secrets?verify=secrets-pre-delete-fix` found the
  Secrets UI already exposed a `Delete` action, but the server only allowed
  `GET /api/v1/secrets/groups/{id}`, so the existing feature would fail with
  HTTP 405 when a live group existed. The same surface used backend
  `synced`/`error` statuses while the UI counted only `success`/`failed`, so a
  healthy synced group could appear unknown and keep the synced KPI at zero.
  Commit `8e63da17` preserves the feature by adding tenant/admin-checked
  `DELETE /api/v1/secrets/groups/{id}`, storage deletion with existing
  `secret_syncs` cascade, and `secret_group.deleted` audit evidence; the UI now
  treats `synced` as healthy and `error` as failed, and the touched Secrets copy
  uses ASCII-safe separators/fallbacks. Local checks passed focused server
  delete/audit tests, Go vet on the touched server/storage packages,
  `npm --prefix ui test -- Secrets.test.tsx`, `npm --prefix ui run lint`,
  `npm --prefix ui run build`, and
  `git diff --check` aside from the repository's normal LF/CRLF notice. Deploy
  run `27080898644` and CI runs `27080898659`/`27080898634` succeeded.
  Post-deploy host checks showed console/controlplane recreated, Redis healthy,
  controlplane about 60 MiB, console about 4.5 MiB, `/healthz=ok`, and no Doris
  containers in the small profile. Authenticated browser verification on
  `/console/secrets?verify=8e63da17` showed ASCII-safe Secrets headings, the
  create modal with the ASCII `x` close control and expected fields,
  `/api/v1/secrets/groups` returning HTTP 200, and zero console
  warnings/errors. No live secret group was created or deleted during final
  verification.
- 2026-06-07 Settings/Webhooks follow-up: code review found that the backend
  webhook response could expose configured outbound custom headers, including
  values commonly used for `Authorization`, API key, token, secret, signature,
  or auth headers. The Settings UI also did not expose the already-supported
  signing secret or custom header features, forcing operators away from the
  console for a useful production integration. Commit `f13d3991` preserves the
  webhook feature path while redacting sensitive response headers, adding
  `secret_configured`/`headers_configured` response flags, surfacing signed and
  custom-header badges, and adding edit-safe signing-secret/custom-header JSON
  controls. Blank edit fields keep existing secret/header settings, while
  explicit clear checkboxes remove them. Local checks passed
  `go test ./controlplane/internal/server -run TestWebhookToResponseRedactsSensitiveHeaders -count=1`,
  focused server RBAC/job coverage, `go vet ./controlplane/internal/server`,
  `npm --prefix ui test -- Settings.test.tsx`, `npm --prefix ui test -- Settings.test.tsx Roles.test.tsx`,
  full `npm --prefix ui test`, `npm --prefix ui run lint`,
  `npm --prefix ui run build`, and `git diff --check` aside from the normal
  LF/CRLF notices. Deploy run `27081585322` succeeded. CI run `27081585318`
  succeeded; CI run `27081585319` initially failed in an unrelated Roles UI
  test after rendering zero mocked roles, while the new Settings tests passed,
  then passed on rerun with Ubuntu/macOS/Windows jobs green. Post-deploy live
  browser verification on `/console/settings?verify=f13d3991` showed the
  Webhooks tab loading, `GET /api/v1/webhooks?...` returning HTTP 200, the
  `Signing secret` and `Custom headers JSON` controls visible, zero console
  warnings/errors, no mojibake/ellipsis/middle-dot copy in the rendered Settings
  text, and no document horizontal overflow. The form was opened and cancelled;
  no live webhook was created, tested, edited, or deleted. Host checks showed
  `ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, Redis healthy, public
  `/healthz=ok`, no Doris services running, controlplane about 75 MiB, console
  about 4.5 MiB, and Redis about 11 MiB.
- 2026-06-07 hyper-light analytics design follow-up: the demo/small-fleet
  architecture is now explicitly Redis+SQLite+Postgres, with Doris preserved as
  an opt-in OLAP backend rather than a default dependency. The deploy defaults
  now cap Redis hot state at `REDIS_MAXMEMORY=128mb`, set the demo SQLite cache
  to `ANALYTICS_SQLITE_CACHE_MB=16`, and keep `DORIS_ENABLED=false`. The design
  keeps all Control One dashboard, connection, investigation, timeline, search,
  and export surfaces intact: backend adapters change behind
  `analytics.mode`, while missing small-mode projections become backlog work
  instead of deleted UI. Next small-fleet implementation work is Redis
  hot-counter acceleration, broader SQLite event/FTS/timeline projections,
  backend-neutral analytics health copy, and restart/replay acceptance tests.
  Commit `92792ce8` deployed successfully via run `27082159708`; CI runs
  `27082159681` and `27082159700` also succeeded. Production Compose now
  renders Redis with command `--maxmemory 128mb` and a 192 MiB container limit,
  while the controlplane env includes `CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`.
  Because the existing Redis container predated the compose hash, it was
  recreated manually after deploy; live verification then showed Redis healthy
  with `maxmemory=134217728`, `appendonly=yes`, and `allkeys-lru`, Redis memory
  about 6.4 MiB of 192 MiB, controlplane about 60.7 MiB of 1 GiB, console about
  4.3 MiB of 256 MiB, public and local `/healthz=ok`, no fresh Redis/SQLite/
  analytic-store error logs after the restart window, and Doris FE/BE stopped.
- 2026-06-07 Settings/MFA enrollment follow-up: code review found that TOTP
  enrollment rendered its QR code through a third-party `api.qrserver.com`
  image URL, leaking the provisioning URI/secret off-origin, and that WebAuthn
  enrollment only logged the challenge rather than completing the browser
  credential ceremony and finish API call. Commit `1713fe22` keeps both MFA
  enrollment features, but moves TOTP QR generation fully local with the
  `qrcode` package, removes the challenge console log, converts WebAuthn
  creation options to browser-native `ArrayBuffer` fields, serializes the
  attestation response back to base64url, and calls
  `/api/v1/mfa/webauthn/finish` with `challenge_id`, label, and attestation.
  Focused and full local checks passed: `npm --prefix ui test -- Settings.test.tsx`,
  full `npm --prefix ui test`, `npm --prefix ui run lint`,
  `npm --prefix ui run build`, `npm --prefix ui audit --omit=dev`, and
  `git diff --check` aside from the normal LF/CRLF warnings. Production audit
  now reports zero omitted-dev vulnerabilities; the separate dev audit still has
  existing transitive findings for future cleanup. Deploy run `27082755716` and
  CI runs `27082755718`/`27082755715` succeeded. Live host validation after
  deploy showed console/controlplane/redis running, Redis healthy with
  `maxmemory=134217728`, `appendonly=yes`, and `allkeys-lru`, controlplane about
  60.8 MiB of 1 GiB, console about 4.3 MiB of 256 MiB, Redis about 6.5 MiB of
  192 MiB, `/healthz=ok`, and Doris FE/BE stopped. Authenticated browser
  validation on `/console/settings?verify=1713fe22` loaded Security/MFA at
  mobile 390x844 and desktop 1440x900, showed zero console warnings/errors,
  only HTTP 200 app API responses for Settings/MFA/fleet/tenant calls, no
  `api.qrserver.com` request, clean desktop layout, and a deliberate scrollable
  Settings tab strip on mobile. No live TOTP, WebAuthn, or backup-code factor
  was created during final verification.
- 2026-06-07 Access/RBAC command-policy follow-up: live validation on
  `/console/access?verify=access-rbac-role-source-before` found that the
  Command policy `New rule` form hardcoded only `operator`, `admin`, and
  `investigator`, so `ciso`, `viewer`, and custom roles from the real RBAC
  catalog could not be selected even though the backend command ACL model
  accepts role names. Commit `7d583dc0` preserves command policy creation and
  deletion, but now sources the selector from the existing `/api/v1/roles` hook,
  keeps canonical built-in ordering, dedupes role names, includes custom roles,
  and falls back to built-in defaults only if the roles API is unavailable.
  Local checks passed `npm --prefix ui test -- Access.test.tsx`, full
  `npm --prefix ui test` with 27 files and 100 tests passing,
  `npm --prefix ui run lint`, `npm --prefix ui run build`, and
  `git diff --check` aside from normal LF/CRLF warnings. Deploy run
  `27083180470` succeeded. CI run `27083180475` succeeded; CI run
  `27083180473` initially failed only because Docker Hub timed out pulling
  `postgres:16-alpine` for a storage integration test, then passed on failed-job
  rerun with Ubuntu/macOS/Windows jobs green. Post-deploy host checks showed
  console/controlplane recreated, Redis healthy, controlplane about 50.7 MiB,
  console about 4.3 MiB, Redis about 6.6 MiB, `/healthz=ok`, and no panic,
  fatal, SQLite lock, or analytic-store unavailable logs. Authenticated browser
  validation on `/console/access?verify=7d583dc0-access-role-options` showed
  `/api/v1/roles`, `/api/v1/command-acls`, and `/api/v1/access-requests`
  returning HTTP 200, the command-policy role selector containing
  `admin,ciso,investigator,operator,viewer` from the live role API, zero console
  warnings/errors, and no document-level horizontal overflow at 390x844
  (`scrollWidth=clientWidth=381`). No live command policy rule was created or
  deleted during final verification.
- 2026-06-07 public workflow routing and Trust Center follow-up: live browser
  verification found that root public workflow links were reaching the landing
  site instead of the React public pages, and that `/console/trust/default`
  could call the public trust API but crashed when empty trust-center tables
  serialized as `null` collections. Commits `09bdeb02`, `5f77c990`, and
  `dd208ea7` preserve the public features and fix the integration: system nginx
  now redirects `/trust/*`, `/intake`, and `/intake-status` into the
  `/console` app, the unauthenticated `GET /api/v1/trust/{tenant}` route allows
  only the public single-tenant lookup while keeping trust admin collections
  authenticated, Settings links the Trust Center by tenant name, and the public
  trust API/UI now tolerate empty subprocessors, certifications, FAQ, and
  incidents as empty arrays. Local checks passed focused server auth/trust
  coverage, `go test ./controlplane/internal/server -run
  'TestTrustCenterPublicEmptyCollectionsEncodeAsArrays|TestPublicTrustCenterPathBypassesAuth|TestAdminTrustCenterCollectionRequiresAuth'
  -count=1`, `npm --prefix ui test -- TrustCenter.test.tsx`,
  `npm --prefix ui test -- TrustCenter.test.tsx Settings.test.tsx`,
  `npm --prefix ui run lint`, `npm --prefix ui run build`, and
  `git diff --check` aside from normal LF/CRLF warnings. Deploy runs
  `27083933801`, `27084212964`, and `27084735628` succeeded; CI runs
  `27083933804`, `27083933809`, `27084212958`, `27084212969`,
  `27084735620`, and `27084735622` succeeded. Post-deploy checks showed
  `nginx -t` successful with the public redirect locations installed,
  `/healthz=200`, root `/` and `/console/` still at HTTP 200, and
  `/api/v1/trust/default` returning HTTP 200 with `[]` collections. Browser
  verification showed `/intake`, `/intake-status`, and `/trust/default`
  redirecting to their `/console/...` routes, rendering expected content at
  desktop and 390x844 mobile widths, zero app console warnings/errors, and no
  document-level horizontal overflow. Host checks after the final deploy showed
  Redis healthy, no Doris FE/BE services running, controlplane about 42 MiB of
  1 GiB, console about 5.4 MiB of 256 MiB, and Redis about 7.9 MiB of 192 MiB.
- 2026-06-07 authenticated Control Room first-load follow-up: live browser
  verification found an initial tenant-loading race after a cold login. The UI
  briefly called `/api/v1/control-room/overview?period=24h` before
  `TenantProvider` selected the default tenant, producing HTTP 400 and leaving
  an `Overview data unavailable` panel even after the tenant-scoped overview
  request succeeded. Commit `806ca2af` preserves Control Room and drilldown
  behavior but waits for a tenant id before requesting overview data, clearing
  stale first-load errors while tenant selection is still loading. Local checks
  passed focused Control Room tests, full `npm --prefix ui test` with 28 files
  and 105 tests passing, `npm --prefix ui run lint`,
  `npm --prefix ui run build`, and `git diff --check` aside from normal
  LF/CRLF warnings. Deploy run `27085214597` succeeded. CI runs `27085214593`
  and `27085214595` succeeded, including tests, lint, security scan,
  cross-platform builds, and image pushes. Live cold-login validation at
  desktop 1440x1000 and mobile 390x844 showed `/api/v1/auth/login`,
  `/api/v1/tenants`, `/api/v1/alerts`, `/api/v1/fleet/health`,
  `/api/v1/control-room/overview?tenant_id=...&period=24h`, and
  `/api/v1/nodes` all returning HTTP 200, with no tenantless overview request,
  zero console warnings/errors, no visible unavailable/request-failed copy, and
  no document-level horizontal overflow. A live authenticated sweep then loaded
  `/console`, `/console/alerts`, `/console/cases`, `/console/investigate`,
  `/console/ask`, `/console/nodes`, `/console/security/network`,
  `/console/observability`, `/console/security/siem`,
  `/console/infrastructure/patch`, `/console/coverage`, `/console/compliance`,
  `/console/access`, `/console/audit`, `/console/onboard`,
  `/console/settings`, and `/console/security/webservers` at both desktop and
  mobile widths; every route stayed authenticated, rendered its main heading,
  had no captured app HTTP failures, no page crashes, no warning/error console
  entries, and no document-level horizontal overflow.
- 2026-06-07 authenticated deep-route/mobile shell follow-up: a broader live
  authenticated link sweep collected 51 unique `/console` routes from the
  current application surface and loaded all 51 at desktop 1440x1000 and mobile
  390x844 with no anomalies: every route stayed authenticated, rendered, and
  showed no warning/error console entries or document-level horizontal
  overflow. The sweep then found a real mobile navigation accessibility and
  clickability defect: the Radix sheet opened without a `DialogTitle`/
  description, and the first close-control size fix (`463c1222`) still left the
  close button behind the sheet sidebar's `z-40` content in the live browser.
  Commits `3255d0a8`, `463c1222`, and `ebde2992` preserve the mobile shell
  feature set while adding the hidden `Primary navigation` title/description,
  a 32px close target, and `z-50` stacking above sheet contents. Local checks
  passed focused `npm --prefix ui test -- NavigationScope.test.tsx`, full
  `npm --prefix ui test` with 28 files and 106 tests passing,
  `npm --prefix ui run lint`, `npm --prefix ui run build`, and
  `git diff --check` aside from normal LF/CRLF warnings. Deploy runs
  `27085813732`, `27086062791`, and `27086254930` succeeded; CI runs
  `27085813731`, `27085813734`, `27086062779`, `27086062794`, `27086254925`,
  `27086254934`, and artifact cleanup run `27086389342` succeeded. Final live
  verification on
  `/console/telemetry?verify=sheet-close-ebde2992` at 390x844 showed the mobile
  navigation dialog named `Primary navigation`, described as `Main console
  navigation links.`, a close button carrying `z-50 h-8 w-8`, successful close
  click with the dialog removed, zero console warnings/errors, and no horizontal
  overflow. Additional safe shell interactions opened, filtered, and closed
  global search; opened and dismissed the tenant selector; and opened and
  dismissed the profile menu, all with zero console warnings/errors, no leftover
  expanded dialogs/menus, and no document-level horizontal overflow.
- 2026-06-07 small-analytics investigation follow-up: live small-mode API
  timing found `/api/v1/events/query` returning correct SQLite-backed
  connection evidence but taking several seconds on one run because close-event
  reads had no `ended_at_ms` indexes. Commit `7b0a81cf` keeps the Redis+
  SQLite+Postgres demo architecture and the Doris OLAP path, but adds partial
  SQLite indexes for closed connection pivots by tenant, node, source IP,
  destination IP, and correlation. Targeted local checks passed
  `go test ./controlplane/internal/smallanalytics`,
  `go test ./controlplane/internal/server -run
  'TestEventsAndTimelineHandlersUseSmallAnalyticsSQLite|TestEventAndTimelineRowsExposeStableCitations|TestSmallAnalytics'`,
  and `git diff --check`; full `go test ./controlplane/...` was blocked only
  by the local `controlone_test` Postgres password/auth environment in four
  existing server integration tests. Deploy run `27086787045` and CI runs
  `27086787034`/`27086787056` succeeded. Post-deploy host verification showed
  `ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, Redis healthy, `/healthz=ok`,
  and the new partial indexes present on the live SQLite DB. Read-only live
  SQLite timing improved close-event count from about 530 ms before the fix to
  11.65 ms after, and the projected open+close count from about 643 ms to
  83.98 ms. Authenticated browser API sampling after deploy returned eight
  consecutive `/api/v1/events/query` responses at about 710-828 ms with exact
  totals over roughly 548k projected connection events, zero browser console
  warnings/errors, and no SQLite lock or analytic-store unavailable logs.
- The same follow-up found a UX integration gap: the IP investigation
  Connections tab showed live small-analytics lifecycles, but Timeline and Raw
  events still rendered empty legacy lifecycle states, making the demo read as
  if connection evidence did not exist. Commit `28493272` preserves the
  existing entity lifecycle API and Connections table, adds a typed
  `/api/v1/timelines/build` client method, and merges small-analytics timeline
  rows into the IP Timeline and Raw events tabs with stable dedupe. Local checks
  passed `npm --prefix ui test -- api.normalize.test.ts EntityDetail.test.tsx`,
  full `npm --prefix ui test` with 28 files and 109 tests passing,
  `npm --prefix ui run lint`, `npm --prefix ui run build`, and
  `git diff --check` aside from normal LF/CRLF notices. Deploy run
  `27087172137` succeeded, and CI runs `27087172138`/`27087172143`
  succeeded. Live browser verification on
  `/console/investigate/ip/149.154.166.110?verify=timeline-ui-28493272` at
  390x844 showed the Connections tab still rendering 250 lifecycle rows, the
  header event count updated to 100, Timeline rendering connection open/close
  events instead of the empty state, Raw events rendering 100 rows with
  `source_table=process_connections`, `event_type=conn.*`, connection id, node,
  and correlation evidence, and the timeline detail sheet showing JSON metadata
  with stable `smallanalytics://...` raw refs. No console warnings/errors,
  no `small-analytics-pending` or unavailable copy, no supplemental timeline
  warning, and no document-level horizontal overflow were observed. Live
  control-plane logs showed the new `/api/v1/timelines/build` request returning
  HTTP 200 in about 45 ms server-side; only the known threat-feed 403/429 local
  snapshot fallback warnings appeared.
- Commits `c90298d0` and `41aca30e` hardened the small-fleet deploy contract:
  Doris FE/BE are behind the Compose `olap` profile, deploy/bootstrap/CI paths
  skip Doris unless OLAP is selected, `.env.example` defaults to small mode, and
  the race test runner is serialized for the CI environment. Deploy runs
  `27072903352` and `27073047055` succeeded; latest CI runs `27073047058` and
  `27073047060` succeeded; live verification showed only app services plus
  healthy Redis running, with Doris FE/BE stopped and `/healthz` at `HTTP 200`.
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
- An authenticated desktop sweep across 41 live console routes found no app
  HTTP failures, browser console warnings/errors, page crashes, lingering
  loading copy, or document-level horizontal overflow. The sweep did uncover a
  real Observability layout defect where the Stack Map hid the `Next action`
  and `Open` columns behind an internal scroll at 1440px. Commits `2d0c4c9f`
  and `a9a84464` are deployed: desktop now shows all Stack Map columns with
  wrapped evidence/action text, while mobile keeps the dense table inside its
  own scroller. Final live verification on 2026-06-06 showed desktop
  `docOverflowX=0`, mobile `docOverflowX=0`, zero app HTTP failures, and zero
  console warnings/errors on `/console/observability?verify=a9a84464-*`.
- 2026-06-07 Observability/Compliance mobile polish follow-up: live desktop
  validation found the Observability Knowledge Tree detail pane could extend
  outside its fixed side column at 1440px when citation/vault chunk evidence
  was selected. The UI now stacks that evidence detail inside the side column
  with `min-w-0` constraints instead of widening the page. A paired 390x844
  mobile sweep found two additional real clipping risks: Observability DBMS
  onboarding step cards and Compliance control-posture evidence counters. Both
  were fixed without removing fields or workflows by wrapping content inside
  the existing controls. Local checks passed
  `npm run test -- src/pages/Observability.test.tsx src/components/coverage/CoverageTruth.test.tsx`
  and `npm run build` in `ui/`; the console-only deploy then completed. Final
  production retest covered 36 authenticated console routes at 390x844 with
  zero document-level horizontal overflow, zero unscrollable overflow
  candidates, zero visible error states, zero failed Control One app API
  responses, zero browser console/page errors, zero Doris/analytic-store copy,
  and zero `/api/v1/events/stream` requests. Intentionally wide operational
  tables remained available through their local horizontal scrollers. Host
  checks after deploy showed `/healthz=ok`, Redis healthy, Doris FE/BE absent
  in the small profile, controlplane about 147 MiB of 1 GiB, console about
  4.6 MiB of 256 MiB, Redis about 7.8 MiB of 192 MiB, landing about 5.7 MiB of
  128 MiB, and ipq about 4.8 MiB of 128 MiB.
- 2026-06-07 safe workflow interaction follow-up: a whitelist-only production
  browser pass at 390x844 exercised non-mutating interactions across 18 routes:
  Alerts tabs/review panel, Network Security tabs, SIEM Inspect/search/clear,
  Observability debug/detail/evidence selection, Patch tabs/deploy dialog open,
  Compliance tabs, Access command-policy/new-rule open, Users role edit open,
  Jobs submit panel open, Settings tabs, Secrets group dialog open, Webserver
  inventory/plan, and Data Security tabs. The sweep avoided destructive
  buttons such as ack, approve, reject, apply, deploy, delete, save, and submit.
  It found one real mobile polish defect: a long Observability evidence path
  chip (`controlplane/internal/server/db_audit_discovery.go`) could extend
  roughly 7 px outside the viewport after selecting Knowledge Tree evidence.
  The shared `StatusTag` now constrains badge width and allows long evidence
  values to wrap inside the chip, preserving the citation instead of truncating
  or removing it. Local checks passed
  `npm run test -- src/pages/Observability.test.tsx src/components/coverage/CoverageTruth.test.tsx`
  and `npm run build`; the console-only deploy completed. Live retest on
  Observability initial, Healthy evidence, Coverage gap, Compliance evidence,
  SIEM coverage, and Settings security at mobile width showed
  `scrollWidth=clientWidth`, zero overflow offenders, zero failed app API
  responses, zero browser console/page errors, zero Doris/analytic-store copy,
  and zero `/api/v1/events/stream` requests. Host checks after deploy showed
  public `/healthz=ok`, only the small-profile services running, no Doris FE/BE
  under the `olap` profile, console about 6 MiB of 256 MiB, controlplane about
  194 MiB of 1 GiB, Redis about 7.5 MiB of 192 MiB, landing about 5.7 MiB of
  128 MiB, and ipq about 4.8 MiB of 128 MiB.
- 2026-06-07 live performance/public-workflow follow-up: a chunked production
  browser timing pass covered 53 authenticated console routes across Control
  Room, Alerts, Cases, Search, Investigation, Ask AI, Nodes, Network Security,
  SIEM, Webservers, Observability, Patch, Coverage, Compliance, Access, Audit,
  Users/Roles, Telemetry, Secrets, Offline Bundle, Settings, Onboard, Data
  Security, Misconduct, Finacle, Fleet Enroll, Hypervisors, Jobs, Templates,
  Sessions, Tenants, Rules, and tenant detail. The sweep used real live tenant,
  node, and IP data. Aside from browser-cancelled `ERR_ABORTED` requests caused
  by intentionally navigating away quickly, the authenticated pass showed zero
  app HTTP failures, zero console/page errors, zero document-level horizontal
  overflow, zero misleading small-mode/Doris error copy, and zero
  `/api/v1/events/stream` requests. The slowest normal app API response across
  the chunks was about 657 ms; the one `/sessions` chunk navigation timeout was
  isolated immediately afterward and loaded to `document.readyState=complete`
  in about 327 ms with `GET /api/v1/sessions` returning 200 and no errors.
  A separate unauthenticated/public mobile pass covered `/`, `/intake`,
  `/intake-status`, `/trust/default`, their `/console/...` redirected routes,
  and `/console/login`. It exposed two real landing-page defects: stale
  default-Doris sales copy on the public root and a mobile overflow in the
  dashboard metric mock caused by inline grid columns overriding responsive CSS.
  The landing page now describes the default small-fleet stack as Postgres,
  Redis, and embedded SQLite analytics, keeps the larger-estate OLAP warehouse
  path as optional, and replaces inline dashboard metric grids with responsive
  metric classes. The landing-only deploy completed, and post-deploy public
  mobile retest across those 8 routes showed zero stale Doris-first copy, zero
  overflow offenders, zero response failures, zero console/page errors, and
  login still presenting one password field. Final host checks showed
  `/healthz=ok`, small-profile services running, no Doris FE/BE under the
  `olap` profile, no recent panic/fatal/SQLite/analytic-store/stream log
  matches, console about 6.3 MiB of 256 MiB, controlplane about 234 MiB of
  1 GiB, Redis about 7.5 MiB of 192 MiB, landing about 4.4 MiB of 128 MiB, and
  ipq about 4.8 MiB of 128 MiB.
- 2026-06-07 console route-fallback/operations follow-up: another live browser
  pass found that invalid nested console paths such as `/console/settings/security`
  and `/console/audit/reports` could render the authenticated shell with an
  empty main panel because the inner console router had no wildcard fallback.
  `ui/src/App.tsx` now renders an authenticated in-app not-found state with the
  unmatched path plus Control Room and Search navigation instead of a blank
  workspace. Local `npm run build` passed, the console-only deploy completed,
  and mobile production retest at 390x844 confirmed `/console/settings/security`,
  `/console/audit/reports`, and `/console/no-such-demo-route` all showed the
  not-found panel with no document overflow. A desktop production sweep at
  1440x900 then covered 16 valid operational routes across Fleet Enroll,
  Hypervisors, Tenants, Telemetry, Data Security, Misconduct, Finacle, Coverage,
  Observability, Compliance evidence/reports, Audit reports, SIEM coverage,
  Webservers, Patch, and Network Security IP behavior with zero failed app API
  responses, zero browser console/page errors, zero overflow offenders, zero
  `/api/v1/events/stream` requests, and zero Doris/analytic-store copy. The
  mobile admin/security route pass immediately before the fix covered 12 routes
  with the same clean API/error/overflow result and exposed the invalid-route
  blank-state risk. Post-deploy host checks showed `/healthz=ok`, Redis healthy,
  no Doris FE/BE containers under the `olap` profile, no recent panic/fatal/
  SQLite-lock/analytic-store/status-5/stream log matches, and memory still light:
  controlplane about 118 MiB of 1 GiB, console about 5.4 MiB of 256 MiB, Redis
  about 4 MiB of 192 MiB, landing about 4.5 MiB of 128 MiB, and ipq about
  4.8 MiB of 128 MiB.
- 2026-06-07 live mobile interaction follow-up: a corrected non-mutating
  browser pass at 390x844 exercised 12 authenticated workflows through safe
  operator interactions instead of only page loads. The pass covered Control
  Room, Nodes, Network Connections, Alerts, Cases, Rules, Access, Compliance,
  Settings, SIEM coverage, Observability, and Fleet Enroll. It opened the
  command palette with `Ctrl+K`, searched for roles, clicked safe detail/
  refresh/refine controls, switched visible tab strips across Network Security,
  Rules, Access, Compliance, and Settings, and touched/cleared local text
  inputs while explicitly rejecting any unexpected API write method as a
  failure. The corrected run produced zero `POST`/`PUT`/`PATCH`/`DELETE`
  requests, zero failed app API responses, zero browser console/page errors,
  zero request failures, zero document overflow or unscrollable overflow
  offenders, zero `/api/v1/events/stream` requests, and zero Doris/
  analytic-store error copy. Host checks after the pass showed `/healthz=ok`,
  Redis healthy, no Doris FE/BE containers under the `olap` profile, no recent
  panic/fatal/SQLite-lock/analytic-store/status-5/stream log matches, and
  memory still light: controlplane about 121 MiB of 1 GiB, console about
  5.4 MiB of 256 MiB, Redis about 4 MiB of 192 MiB, landing about 4.5 MiB of
  128 MiB, and ipq about 4.8 MiB of 128 MiB.

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
- Focused Observability coverage passed after the Stack Map responsive fix:
  `npm run lint`, `npx vitest run src/pages/Observability.test.tsx
  --coverage=false`, and `npm run build` passed in `ui/`.

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
- Met: later live mobile sweep at 390px covered 36 authenticated routes with no
  document-level horizontal overflow or unscrollable overflow candidates.
- Met: live safe-workflow interaction sweep covered 18 authenticated routes
  with non-mutating clicks/filters/dialog opens and no app API, console,
  stream, or page-error failures after the evidence-chip polish fix.
- Met: live timing/API sweep covered 53 authenticated routes plus 8 public
  routes, with the public landing copy/layout corrections deployed and retested.
- Met: invalid nested console paths now render an explicit in-app not-found
  fallback instead of a blank authenticated workspace, and 16 valid operational
  desktop routes were retested clean after deploy.
- Met: non-mutating mobile interaction audit covered 12 authenticated workflows
  with zero unexpected write requests, app failures, console errors, stream
  traffic, or overflow findings.
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

2026-06-07 demo architecture update: formalized this as Control One Lite
Analytics in `docs/small-fleet-analytics-architecture.md`. The demo/small-fleet
path keeps the full product surface while moving recent investigation reads,
connection timelines, top-talkers, and future normalized event/FTS projections
onto Postgres + bounded Redis + embedded SQLite/WAL. Doris is preserved as an
explicit OLAP upgrade path and must not consume memory in the default demo
deployment.

2026-06-07 minimum-memory refinement: the Lite Analytics design now documents
the smallest credible demo runtime as one controlplane-owned SQLite writer, one
hard-capped Redis container, the existing Postgres journal, and zero Doris FE/BE
processes unless the `olap` profile is explicitly selected. Redis is explicitly
non-evidentiary and eviction-safe; SQLite/WAL is the recent cited evidence read
model; Postgres remains the audit/replay truth; and Doris remains the dedicated
warehouse tier for larger fleets rather than a default demo dependency.

2026-06-07 small-analytics integration follow-up: the Redis+SQLite small-fleet
path now has a server-side connection reader in
`controlplane/internal/server/analytics_connections.go`. IP-scoped network
targeting, node documentation top connections, and event-capture flow deltas
now read through the selected analytics backend, so small mode can preserve
those workflows from SQLite connection facts while OLAP mode keeps using Doris.
This is additive and feature-preserving: Doris remains the warehouse upgrade
path, but the demo profile keeps Doris at 0 MB. A fresh production footprint
check showed `/healthz=ok`, `ANALYTICS_MODE=small`, `DORIS_ENABLED=false`,
`SQLITE_CACHE_MB=16`, no Doris FE/BE services under the `olap` profile,
controlplane about 85.8 MiB / 1 GiB, Redis about 4.84 MiB / 192 MiB, and
console about 4.28 MiB / 256 MiB.

Post-deploy verification update: the controlplane was rebuilt from a prebuilt
local binary and recreated successfully, then authenticated production API
checks confirmed the selected small analytics backend on live data:
`/api/v1/connections?...&node_id=0d4893c0-867a-4bf1-8aa9-e247680280ab`
returned `source=small-analytics` with 5 sampled rows, node documentation for
that node returned 10 top connections, and `/flow-delta` returned 16 rows.
Host checks stayed clean: `/healthz=ok`, Redis healthy, no Doris FE/BE profile
containers, no recent controlplane panic/fatal/SQLite/analytic-store errors,
no edge 5xx entries, controlplane about 60.86 MiB / 1 GiB, Redis about
4.83 MiB / 192 MiB, and console about 4.48 MiB / 256 MiB.

Saved-search duplicate prevention was also deployed and retested in a real
browser. Query `codex-duplicate-guard-1780843512220` produced exactly one
`POST /api/v1/saved-searches` with 201, the button changed from `Save search`
to disabled `Saved`, the saved-search list contained exactly one matching row,
cleanup deleted that row with 204, and a follow-up list showed zero remaining
rows. The same production browser check reported zero console errors, zero page
errors, zero failed API responses, and zero document horizontal overflow on the
search page.

A 390x844 mobile browser smoke across Search, Saved Searches, Network
Connections, and the live node detail route also showed zero console/page/API
errors, zero Doris or analytic-store unavailable copy, and zero document-level
horizontal overflow. The Saved and Network tables remain wider than the
viewport inside their table containers, but the document itself does not
overflow.

2026-06-07 live footprint check: the demo host reports
`CONTROLPLANE_ANALYTICS_MODE=small`, `CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`; `docker compose --profile olap ps
doris-fe doris-be` shows no Doris containers. Current post-deploy container
memory is roughly controlplane 63.7 MiB / 1 GiB, Redis 7.9 MiB / 192 MiB,
console 4.5 MiB / 256 MiB, landing 5.9 MiB / 128 MiB, and ipq 4.8 MiB /
128 MiB.

2026-06-07 production follow-up: deploy `27088445229` succeeded for
`a06ef72d`, closing the live agent contract failures without removing policy,
compliance, or mesh features. Post-deploy logs show
`POST /api/v1/compliance/report` returning 202, `GET /api/v1/mesh/peers`
returning 200, `POST /api/v1/mesh/rotate` returning 200, and agent
`GET /api/v1/policies` returning 200. The remaining threat-feed warnings are
external-source responses (`tor-exit` 403 and AbuseIPDB 429) with local snapshot
fallback, not ingest/deploy failures. A real-browser route smoke across 15
console routes returned HTTP 200 navigations, no failing app API responses, and
no browser console errors. Deploy `27088747860` then succeeded for `86196ab8`;
a 390x844 production browser check confirmed the Compliance tab strip now wraps
all five tabs visibly on mobile, with Compliance API calls returning 200 and
zero browser console errors.

2026-06-07 mobile onboarding follow-up: a deeper 390x844 browser sweep across
Access, Audit, Data Security, Finacle, Misconduct, Onboard, Investigation IP
detail, Search, Sessions, Webservers, Patch, SIEM, and Offline Bundle returned
HTTP 200 navigations, no failed app API responses, and no browser console
errors. The sweep found one real mobile UX defect: `/console/onboard` allowed
the `Hypervisor / cloud account` mode tab to extend past the viewport. Commit
`8f72e15a` wraps the onboarding mode tabs using the same responsive grid pattern
as the other corrected tab strips. Local production build passed, and a
console-only live deploy was completed after fixing `deploy/deploy_console.py`
so it no longer excludes source paths such as `ui/src/components/coverage` while
skipping generated `ui/coverage` output. Production retest at
`/console/onboard?verify=8f72e15a-live` returned HTTP 200 with all three tab
strips in bounds, no document horizontal overflow, no failed app API responses,
no browser console errors, no Doris/analytic-store unavailable copy, and live
post-deploy `/healthz=ok`.

2026-06-07 node/detail live-data follow-up: authenticated production API probes
confirmed the default tenant has two active nodes with fresh heartbeats and the
small analytics fleet source remains `small-analytics-postgres`. A 390x844
browser sweep then loaded real detail and investigation paths:
`/nodes/0d4893c0-867a-4bf1-8aa9-e247680280ab`,
`/nodes/1ab45ccc-3984-4315-bc17-641ad43f02c8`,
`/investigate/ip/158.220.87.109`, `/investigate/ip/172.67.163.40`,
`/security/network?tab=connections`, and Control Room drilldowns for exposure,
app/db health, patch posture, and the overview. All navigations returned HTTP
200 with no browser console errors, no failed app API responses, and no
Doris/analytic-store unavailable copy. The sweep found one real polish issue on
the long-hostname node detail page: the header/breadcrumb content rendered
wider than the mobile viewport. Commit `8dcea4b7` fixes this by adding
mobile-safe wrapping/breaking to Node Detail and the shared section header. The
console-only deploy completed, and production retest at both 390x844 and
1440x900 showed the real node detail page with header right edge inside the
viewport, no overflow candidates, no horizontal document overflow, all seven
tabs in bounds, no failed app API responses, and no browser console errors.
Post-deploy `/healthz=ok`, Redis remained healthy, Doris FE/BE remained absent,
and container memory stayed light.

2026-06-07 live interaction/transport follow-up: a post-deploy browser check
found one production regression from the console-only build path: the live UI
bundle had fallen back to the default SSE transport and Chromium reported
`ERR_QUIC_PROTOCOL_ERROR` for `/api/v1/events/stream`. The fix keeps the SSE
feature available for private/direct deployments but makes the small-fleet demo
default polling mode in `ui/src/config/live.ts`; `useLiveSubscribe` now also
honors the same transport switch before opening `streamEvents`. Local
`npm run build` passed, the console-only deploy completed, and fresh production
browser pages for Control Room, Alerts, and Rules made zero
`/api/v1/events/stream` requests, produced zero stream failures, zero browser
console/page errors, zero failed app API responses, no document-level
horizontal overflow, and no Doris/analytic-store unavailable copy.

The same live slice exercised read-only demo flows end to end: Ctrl+K command
palette IP pivot opened `/console/investigate/ip/172.67.163.40`; Network
Connections showed 500 live rows and opened the connection detail sheet; the
real node detail page for `0d4893c0-867a-4bf1-8aa9-e247680280ab` showed 250
connection rows, opened the same detail sheet, and loaded Packages plus
Recommendations tabs. A 390x844 mobile pass across Control Room, Alerts,
Rules, Network Connections, and Node Detail had zero app failures and zero
stream traffic. The pass found one real mobile polish defect: the Node Detail
Connections action row shifted the "Listening only" control partly off-screen.
`ui/src/pages/NodeDetail.tsx` now wraps that action group; production retest
showed all three controls inside the 390px viewport, the mobile connection
detail sheet visible, no document overflow, no browser errors, no failed app
responses, and no fresh `/events/stream` log entries. Post-deploy
`/healthz=ok`; Doris FE/BE remained stopped/absent; memory stayed light
(controlplane about 88 MiB, console about 4.5 MiB, Redis about 7 MiB).

2026-06-07 Control Room freshness/auth follow-up: a live auth and security
matrix passed 30/30 checks. Invalid login returned 401, protected API reads
returned 401 without a token and with an invalid bearer token, public
misconduct/trust/health endpoints stayed reachable, authenticated reads
succeeded, logout invalidated the session, HTTPS responses carried the expected
security headers, HTTP requests redirected to HTTPS, and the slowest observed
check was 914 ms.

The same real-browser Control Room retest found one material freshness/UX issue:
the 24h header said "8 incidents, 12 pending actions, 6 IP findings in 24h" even
though the visible IP behavior findings were stale May 18 unresolved findings.
The fix scopes unresolved IP behavior findings by the selected overview window
in the backend, and changes the UI copy to distinguish persistent open
incidents from recent IP findings. This preserves useful open incidents instead
of deleting signal for the demo, while no longer implying stale findings landed
inside the selected 24h window. After deploy, the live API returned
`ip_findings=0`, `ip_requests=0`, `pending_actions=2`, `top_incidents=8`, and
682 ms for the 24h overview; the browser header now reads "8 open incidents, 2
pending actions, 0 recent IP findings (24h)", the IP behavior lane shows zero
recent requests/findings, and browser console errors/warnings remained at zero.

Post-deploy host evidence still matches the hyper-light design:
`/healthz=ok`, controlplane/console/redis are up, Doris FE/BE are absent under
the `olap` profile, `CONTROLPLANE_ANALYTICS_MODE=small`,
`CONTROLPLANE_DORIS_ENABLED=false`, Redis data memory is about 1.78 MiB with a
128 MiB cap, controlplane is about 65.6 MiB / 1 GiB, console about 4.6 MiB /
256 MiB, Redis about 4.8 MiB / 192 MiB, and a 20-minute severe-log scan found no
panic/fatal/SQLite lock/analytics unavailable/stream transport signatures.

2026-06-07 Search/investigation UX follow-up: a fresh production browser sweep
covered 100 current-state route loads across desktop and 390px mobile. The
authenticated chunks covered Control Room and drilldowns, Search, Investigation,
Cases, Tenants, Nodes, Fleet Enroll, Hypervisors, Jobs, Observability,
Templates, Coverage, Compliance, Rules, Alerts, Access, Network Security tabs,
SIEM, Webservers, Patch, Sessions, Roles, Audit, Users, Telemetry, Secrets,
Offline Bundle, Settings, Data Security, Misconduct, and Finacle. The public and
redirect chunk covered `/`, intake, intake status, Trust Center, route aliases,
and the console 404 path. Clean chunks showed zero app API 4xx/5xx failures,
zero browser console/page errors, zero document-level horizontal overflow, and
no Doris/analytic-store unavailable copy.

The sweep found one real search-workflow polish issue: `/console/search` rendered
the primary heading as a leading chevron plus `(empty query)` or the raw query
(`› nginx`), and `Save search` stayed enabled with no query. The Search page now
uses explicit headings (`Search` and `Search results`), keeps the query context
in the description, disables Save Search until there is a real query, and lets a
cleared refine box return to the empty-query state. Focused
`SearchResults.test.tsx` coverage and `npm run build` passed. The console-only
deploy completed, and live desktop/mobile retest confirmed the fixed headings,
`0 matches for "nginx"` description, disabled empty Save Search button, zero
document overflow, zero app failures, and zero console/page errors. Post-deploy
host checks remained clean: `/healthz=ok`, no Doris FE/BE, console about
4.8 MiB / 256 MiB, controlplane about 115 MiB / 1 GiB, Redis about 4.8 MiB /
192 MiB, and no severe controlplane or edge 5xx logs.

2026-06-07 Saved Search workflow follow-up: continuing from the Search audit,
live browser testing found that the now-polished `Save search` button was still
only visual on production. On `/console/search?q=nginx`, the button was enabled,
but clicking it emitted zero `/api/v1/saved-searches` requests, created no saved
row, and showed no product feedback. The UI now integrates the existing
role-gated saved-search API instead of adding a parallel feature: it creates a
private saved search named from the current query, preserves any active entity
type tab as metadata, disables while saving or without a tenant/query, invalidates
saved-search consumers, and reports success/error via toast.

Focused `SearchResults.test.tsx` coverage now includes the empty-query guard,
query clearing, and the `createSavedSearch` payload. `npm run test --
SearchResults.test.tsx --runInBand` and `npm run build` passed, with the same
known npm/React Router future warnings only. The console-only production deploy
completed. Live browser round trip then created a temporary query
`codex-save-roundtrip-*` from Search, observed `POST /api/v1/saved-searches`
returning 201, verified the saved row on `/console/investigate/saved`, deleted
that exact temporary row with `DELETE /api/v1/saved-searches/{id}` returning 204,
and confirmed the row was gone after reload. The live run had zero browser
console/page errors, zero app request failures, and zero document overflow.
Post-deploy host checks remained clean: `/healthz=ok`, console/controlplane/Redis
up, no Doris FE/BE, console about 4.7 MiB / 256 MiB, controlplane about
140.6 MiB / 1 GiB, Redis about 4.8 MiB / 192 MiB, and no severe controlplane or
edge 5xx logs.

2026-06-07 post-`e490bbe5` broad live sweep: with the Browser Use Node bridge
unavailable, the audit used the Playwright MCP fallback for a chunked
authenticated browser pass. The pass covered 23 additional console routes at
desktop 1440x1000 and mobile 390x844, for 46 route loads total:
`/console/access`, `/console/sessions`, `/console/cases`, `/console/jobs`,
`/console/templates`, `/console/fleet-enroll`, `/console/hypervisors`,
`/console/compliance`, `/console/security/siem`,
`/console/security/webservers`, `/console/infrastructure/patch`,
`/console/roles`, `/console/users`, `/console/audit`, `/console/telemetry`,
`/console/secrets`, `/console/offline-bundle`, `/console/settings`,
`/console/data-security`, `/console/misconduct`, `/console/access/finacle`,
`/console/investigate/knowledge-graph`, and `/console/observability`. Every
navigation returned HTTP 200, and the sweep found zero browser console/page
errors, zero app API failures, zero document-level horizontal overflow, zero
uncontained off-viewport elements, and zero Doris/analytic-store unavailable
copy. No new UI defect was found in this slice.

The same live validation rechecked RBAC/list integrity after the saved-search
and small-analytics hardening: `/api/v1/users?limit=100&offset=0` returned 6
users with zero duplicate user-role rows, and `/api/v1/roles` returned 5 roles
with 5 unique names and no duplicate role names. GitHub Actions did not expose a
visible run for commit `e490bbe5` in the queried branch/run list, so this slice
is recorded as local test plus manual live deployment/browser/API evidence.

Fresh host evidence still matches the Redis+SQLite+Postgres small-fleet design:
`/healthz=ok`, Compose and container env report `CONTROLPLANE_ANALYTICS_MODE=small`,
`CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`, Redis is healthy, and
`docker compose --profile olap ps doris-fe doris-be` shows no Doris containers.
Memory stayed light at about console 4.55 MiB / 256 MiB, controlplane
64.05 MiB / 1 GiB, Redis 4.84 MiB / 192 MiB, landing 4.48 MiB / 128 MiB, and
ipq 4.81 MiB / 128 MiB. A 30-minute log scan found no controlplane panic,
fatal, SQLite lock, analytic-store unavailable, stream transport, or edge 5xx
matches.

2026-06-07 evidence/export workflow follow-up: the next production audit slice
focused on bank-facing export paths rather than route loads. Direct authenticated
API probes showed all static CSV report exports returning HTTP 200 with
`text/csv` attachment headers: compliance (4,523 non-empty lines,
`rule_id,node_id,passed,severity,checked_at`), audit (2,519 lines,
`occurred_at,actor_id,action,resource_type,resource_id`), alerts (21 lines,
`opened_at,severity,state,source,title,node_id`), and access (header-only,
`requested_at,user_id,resource_type,requested_access,status,decided_at,decided_by,ttl_seconds`,
which is correct for no access requests in the window). A SOC case export
returned HTTP 200 JSON with `export_version=soc-case-export-v1`, 6 evidence
references, 5 guardrails, and no `raw_evidence` field in the packet body.

Real-browser export checks then validated the operator controls. On
`/console/cases?verify=export-packet-browser`, clicking `Preview export`
triggered `GET /api/v1/soc/cases/{id}/export` with HTTP 200, rendered packet
evidence and guardrail copy, and did not expose raw evidence text. On
`/console/audit?verify=client-csv-audit` and
`/console/compliance?verify=client-csv-compliance`, the single enabled
`Export CSV` button on each page produced real downloads named
`audit-logs-2026-06-07.csv` and `compliance-results-2026-06-07.csv`. On
`/console/investigate/knowledge-graph?verify=kg-md-export`, the `Download .md`
button produced `knowledge_graph_00000000-0000-0000-0000-000000000001.md`.
Those browser checks observed zero console warnings/errors, zero page errors,
zero failed app API responses, zero request failures, no document-level
horizontal overflow, no broken/mojibake rendered copy, and no Doris/
analytic-store unavailable copy. The generated audit-report artifact list
currently returned `data:null` with `total=0`, so there was no live generated
report file to download in this slice; the UI's list normalizer treats that as
an empty report list.

Post-slice host checks remained clean: `/healthz=ok`,
`CONTROLPLANE_ANALYTICS_MODE=small`, `CONTROLPLANE_DORIS_ENABLED=false`,
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`, Redis healthy, no Doris FE/BE under
the `olap` profile, console about 4.56 MiB / 256 MiB, controlplane about
63.86 MiB / 1 GiB, Redis about 4.84 MiB / 192 MiB, landing about 4.48 MiB /
128 MiB, and ipq about 4.81 MiB / 128 MiB. A 30-minute log scan found no
controlplane panic, fatal, SQLite lock, analytic-store unavailable, stream
transport, or edge 5xx matches.

2026-06-07 small-fleet analytics architecture decision: for the demo host and
branch-size deployments, Control One should run the full operator experience on
Postgres + Redis + embedded SQLite/WAL and keep Doris at 0 MB unless
`ANALYTICS_MODE=olap`, `DORIS_ENABLED=true`, and the Compose `olap` profile are
explicitly selected. This is a product-preserving architecture, not a feature
cut: Postgres remains the durable ingest journal and audit/workflow truth,
SQLite supplies recent cited analytic evidence, Redis supplies bounded hot
state and queues, and Doris stays as the explicit high-volume OLAP upgrade
path. The standalone design record is
`docs/small-fleet-analytics-architecture.md`; it now calls out the remaining
Doris-coupled server paths that need backend-neutral migration rather than UI
removal. Focused validation for this decision passed with
`go test ./controlplane/internal/smallanalytics -count=1` and targeted server
tests covering small-mode fleet health, top talkers, and SQLite-backed
connections.

2026-06-07 compliance report artifact hardening and live browser fix:
production checks found that empty generated-report/review API responses could
serialize `data:null`, report/review rows leaked Go-style field names, generated
report artifacts needed a writable nonroot reports mount, and the Reports UI
opened protected artifact URLs in a new tab without the bearer token. The fix
keeps the feature surface intact: report/review APIs now return arrays and
snake_case fields, deploy mounts/chowns `/var/lib/control-one/reports`, and the
Reports/Evidence buttons fetch protected artifacts with the authenticated API
client before saving a browser download.

Local validation passed: focused compliance/report server tests, storage tests,
smallanalytics tests, `go vet ./controlplane/internal/server ./controlplane/internal/storage ./controlplane/internal/smallanalytics`,
`go test -short ./controlplane/internal/server -count=1`, `npm run build`, and
`npm run lint` (lint emitted only the existing ESLintRC deprecation warning).
The live controlplane and console were redeployed. Direct live API checks showed
`/api/v1/compliance/reports` and `/api/v1/compliance/reviews` returning array
`data`, created report `b811e3a3-7d03-47d5-8a28-d88c749d0341`, downloaded it
with HTTP 200, and then listed it as `status=ready` with `pdf_path` present and
no Go field names. Real-browser verification on
`/console/compliance?tab=reports&verify=auth-download-fix-20260607` showed the
Reports table with ready SOC2 rows; clicking a ready row's download control made
an authenticated `/api/v1/compliance/reports/{id}/download` request with HTTP
200 and saved `compliance-report-SOC2-2026-06-07.html` (8,156 bytes). The page
had zero current console warnings/errors and no document-level horizontal
overflow. Post-deploy host evidence remained healthy: `/healthz=ok`,
`CONTROLPLANE_ANALYTICS_MODE=small`, `CONTROLPLANE_DORIS_ENABLED=false`,
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`, no Doris FE/BE containers under
the OLAP profile, memory around controlplane 65.89 MiB / 1 GiB, Redis
4.79 MiB / 192 MiB, console 7.03 MiB / 256 MiB, landing 4.49 MiB / 128 MiB,
and ipq 4.81 MiB / 128 MiB. A 20-minute post-deploy log scan found no panic,
fatal, permission denied, audit report artifact, SQLite lock, analytics
unavailable, or edge 5xx matches.

2026-06-07 broad post-architecture live sweep: with the Browser Use Node bridge
still unavailable, the audit used the Playwright MCP fallback for another
authenticated production browser pass against the deployed small-mode console.
The desktop pass covered 48 route loads at 1440x900 across core operations,
investigation, fleet, compliance, detection rules, privileged access, network
security, SIEM/webserver controls, patching, sessions, RBAC/users, audit,
telemetry, secrets, offline bundle, settings, data-security, misconduct,
Finacle, and compatibility aliases such as `/console/reports`,
`/console/connections`, `/console/behavioral`, `/console/recommendations`,
`/console/compliance-evidence`, `/console/audit-reports`, and
`/console/frameworks`. The route batches found zero browser console errors,
zero page errors, zero app API HTTP 4xx/5xx responses, and zero
document-level horizontal overflow.

Candidate findings were checked and classified: `/console/rules/builder` is an
intentional alias to `/console/rules`; compliance `Failed` strings are policy
result/status labels, not load failures; the webserver "approval required"
message is the expected safety gate before applying a config plan; and isolated
`net::ERR_ABORTED` entries were stale in-flight requests during route
transition or Cloudflare RUM beacons, with no UI or HTTP failure. A 390x844
mobile smoke pass covered 12 high-risk routes and found no body overflow, no
runtime/API failures, and no bad Doris/analytics-unavailable copy. The mobile
controls that initially appeared offscreen were verified to live inside
`overflow-x:auto` table containers, with explicit scroll ancestors on Alerts,
Users, Roles, Network Connections, and Compliance Reports.

Fresh host evidence continues to match the minimum-memory small-fleet
architecture: public `/healthz=ok`; the deployed controlplane environment shows
`CONTROLPLANE_ANALYTICS_MODE=small`, `CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`; `docker ps` shows no Doris
containers; and the current memory profile is approximately controlplane
79.11 MiB / 1 GiB, console 7.10 MiB / 256 MiB, Redis 6.15 MiB / 192 MiB,
landing 4.49 MiB / 128 MiB, and ipq 4.81 MiB / 128 MiB. A 30-minute
controlplane/console/edge log scan found no panic, fatal, permission denied,
audit report artifact, SQLite lock, database locked, analytics unavailable, or
edge 5xx matches. No new code defect was confirmed in this sweep.

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

2026-06-07 small-fleet analytics contract/performance follow-up: the live
demo direction remains Redis+SQLite/WAL+Postgres, with Doris preserved as the
explicit OLAP upgrade path rather than deleted. This pass found three real
defects in the small-mode implementation: direct analytics API responses leaked
Go struct keys such as `ConnID`, `ThreatHits`, and `NodeID`; unscoped
small-mode `/api/v1/events/query` and connection-pivot
`/api/v1/timelines/build` could exceed the 5s SQLite query timeout; and the
network connection drawer still depended on legacy `detail.events`, so small
mode could show "No correlated activity" even when the backend-neutral timeline
API had cited connection facts.

Implemented fixes are additive and feature-preserving. Shared connection,
top-talker, and fleet structs now emit snake_case JSON. The UI top-talker
normalizer accepts both snake_case and legacy Go-shaped rows. Small analytics
event/timeline SQL now pushes tenant/time/node/correlation/connection filters
inside the open/close union branches, and IP timelines split source-IP and
destination-IP branches so existing SQLite indexes are usable. The connection
detail drawer now fetches `/api/v1/timelines/build` in parallel with the
connection metadata call, maps cited timeline rows into the existing
`EventTimeline`, and falls back to legacy detail events when needed.

Local validation passed: `go test ./controlplane/internal/smallanalytics -count=1`, focused server tests for small analytics events/timelines and
connections, `go test ./controlplane/internal/doris -count=1`, `go vet ./controlplane/internal/server ./controlplane/internal/doris ./controlplane/internal/smallanalytics`, `npx vitest run src/lib/api.normalize.test.ts`, `npm run build`, `npm run lint`, and
`git diff --check`. A broader `go test ./controlplane/internal/server` run
was blocked by local Postgres test authentication for `controlone_test`, not
by this change.

The fixed controlplane and console were deployed to production. Live API
evidence on tenant `00000000-0000-0000-0000-000000000001` showed
`fleet_health`, `connections_external`, `top_talkers`, `connection_detail`,
`connections_by_ip`, `timeline_by_ip_entity`, `events_query`, and
`timeline_by_conn` all returning HTTP 200 with `go_style_keys=false`. Final
server-side timings after the deploy settled were approximately: fleet health
2-7 ms, connections list 3-15 ms, top talkers 19 ms, IP-filtered connections
25 ms, event query 643 ms, IP timeline 906 ms, connection detail 2 ms, and
connection timeline 780 ms. The SQLite projection held about 608k connection
rows during the test, so this was not an empty-fixture check.

Real-browser verification on
`/console/security/network?tab=connections&verify=small-analytics-contract-20260607c`
showed the Connections table populated with live rows and threat labels,
opening a row made `/api/v1/connections/{id}` and `/api/v1/timelines/build`
requests with HTTP 200, and the drawer rendered a cited `conn.open` forensic
timeline row instead of the empty-state message. Browser console warnings/errors
were zero on the final clean pass. Host evidence stayed aligned with the
minimum-memory design: public `/healthz=ok`, no Doris FE/BE containers under
the OLAP profile, controlplane about 63 MiB / 1 GiB, console about 6.5 MiB /
256 MiB, Redis about 6.2 MiB / 192 MiB, and ipq about 4.8 MiB / 128 MiB.

2026-06-07 tenant timeline follow-up: a broader live API smoke of the main
console contracts found one real small-mode backend defect after the route
sweep: `POST /api/v1/timelines/build` returned HTTP 500 for
`entity_type=tenant` / `entity_id=<tenant_id>`, even though the tenant scope was
already authorized and the request should mean "recent tenant-scoped timeline".
This was a contract gap, not a resource issue: the failing request returned in
about 761 ms before the fix, while the server log showed a fast 500.

The fix is additive. Tenant timeline pivots are normalized at the HTTP and AI
tool boundary, `tenant_id` is treated as the canonical `tenant` pivot alias, a
mismatched tenant entity ID now returns HTTP 400, and both small analytics and
Doris timeline predicate builders treat tenant pivots as the already-required
tenant scope instead of unsupported entity filters. Regression coverage now
includes small analytics tenant timelines, HTTP tenant timeline success and
mismatch rejection, and the Doris timeline SQL tenant-pivot path.

Local validation passed: `go test ./controlplane/internal/smallanalytics -count=1`,
focused Doris timeline SQL tests, focused server investigation timeline tests,
`go vet ./controlplane/internal/server ./controlplane/internal/doris ./controlplane/internal/smallanalytics`,
and `git diff --check`. The new Linux/amd64 controlplane binary
`eecf4a4688be2b3583b2c9ed07442f917b8e479930f8e56c9ac9172f0211d7f9` was deployed
to production and the controlplane container was recreated.

Post-deploy live evidence: public `/healthz=ok`; tenant timeline returned
HTTP 200 in about 520 ms with 10 items, 10 citations, `source=small-analytics`,
and scope `tenant/00000000-0000-0000-0000-000000000001`; a mismatched tenant
timeline returned HTTP 400; and a corrected 29-endpoint authenticated API smoke
returned zero failures. A post-deploy browser sweep of Control Room, Network,
SIEM, Compliance, Reports, Alerts, Cases, Observability, Nodes, Coverage, Audit,
and Telemetry showed no visible error states, no same-origin failed requests,
and no console warnings/errors. Recent controlplane logs showed no 5xx, panic,
database lock, analytics-unavailable, or Doris-unavailable matches; only the
known AbuseIPDB 429 local-snapshot warning appeared. Host memory remained in the
small-fleet envelope: controlplane about 62.6 MiB / 1 GiB, console about
6.6 MiB / 256 MiB, Redis about 6.2 MiB / 192 MiB, ipq about 4.8 MiB / 128 MiB,
and no Doris FE/BE containers under the OLAP profile.

2026-06-07 fresh-login deep-link follow-up: a fresh isolated browser context
opened
`/console/security/network?tab=connections&verify=fresh-login-return-20260607`
without a session. The console correctly redirected to `/console/login`, but
after a successful `admin@local` password login it landed on `/console` instead
of returning to the requested Network Connections page. This is a real demo and
operator UX defect because alert, case, report, and investigation links must
survive session establishment.

The fix is small and preserves existing auth behavior: protected-route redirects
now pass `{from: pathname + search + hash}` to `/login`, and the existing login
return logic uses that state after password, token, or SSO sign-in. A focused
React Router regression test proves an unauthenticated
`/security/network?tab=connections#row-7` route carries the full return target
into the login route.

Local validation passed: `npx vitest run src/App.test.tsx`, `npm run build`,
and `npm run lint` (only the existing ESLintRC deprecation warning). The console
was rebuilt and redeployed. Live retest in a fresh isolated browser context
showed the same protected Network deep link redirecting to `/console/login`, then
returning after sign-in to
`/console/security/network?tab=connections&verify=fresh-login-return-fixed-20260607`.
The Network page rendered the expected tab surface with no visible error state,
no same-origin failed requests, and zero browser console warnings/errors. Host
evidence after redeploy: `/healthz=ok`, recent console/controlplane logs showed
no actual 4xx/5xx/panic/database-lock/analytics-unavailable matches, no Doris
FE/BE containers were running, and memory remained within the small profile:
controlplane about 64.6 MiB / 1 GiB, console about 5.2 MiB / 256 MiB, Redis
about 6.9 MiB / 192 MiB, and ipq about 4.8 MiB / 128 MiB.

2026-06-07 session revocation and expired-session UX follow-up: a live
isolated-browser auth audit found two production session issues. First, when a
stale stored token opened a protected Network deep link, the UI cleared the
token and returned correctly after re-login, but the primary email sign-in form
did not show the "Session has expired. Please sign in again." message. Second,
clicking the profile-menu Sign out item cleared browser storage and returned to
`/console/login`, but the old bearer token still returned HTTP 200 from
`/api/v1/auth/me`, proving the UI had not revoked the backend session.

The fix preserves the existing auth model and features. `AuthProvider.signOut`
now calls `POST /api/v1/auth/logout` with the active bearer token before
clearing local state, and still clears local state in a `finally` block if the
network logout attempt fails. The profile menu intentionally fires the async
sign-out without leaking a promise into the menu handler. The login page now
surfaces provider-level auth notices on the primary email/password form with an
alert role, instead of hiding expired-session guidance behind advanced bearer
token options.

Regression coverage now includes `AuthProvider` proving sign-out sends
`POST /api/v1/auth/logout` with the active Authorization header before local
storage is cleared, and `Login` proving expired-session guidance appears on the
primary form. Local validation passed:
`npx vitest run src/providers/AuthProvider.test.tsx src/pages/Login.test.tsx src/App.test.tsx`,
`npm run build`, `npm run lint`, and `git diff --check`.

The console was rebuilt and redeployed. Live post-deploy evidence: the stale
token replay on
`/console/security/network?tab=connections&verify=session-ux-fixed-20260607`
landed on `/console/login`, removed `control-one-token`, displayed
`Session has expired. Please sign in again.`, and returned to the original
Network page after password login. A cleanup logout for that temporary session
returned HTTP 200. A separate profile-menu sign-out replay showed an
authenticated `/api/v1/auth/me` check returning HTTP 200 before sign-out, a
real `POST /api/v1/auth/logout` request with Authorization during sign-out,
browser storage cleared on `/console/login`, and the same old token returning
HTTP 401 afterward. The only browser console resource errors in this slice were
the expected 401s deliberately generated by stale-token and revoked-token probes.
Host evidence after deploy: `/healthz=ok`, strict recent console/controlplane
log scans showed no actual nginx 4xx/5xx or controlplane 5xx/panic/database
lock/analytics-unavailable/Doris-unavailable matches, no Doris FE/BE containers
were running, and memory stayed inside the small-fleet envelope: console about
6.8 MiB / 256 MiB, controlplane about 80.0 MiB / 1 GiB, Redis about 6.7 MiB /
192 MiB, and ipq about 4.8 MiB / 128 MiB.

2026-06-07 public route boundary and intake link follow-up: an isolated live
browser sweep checked unauthenticated public pages and protected redirects at
desktop and mobile sizes. `/console/trust/default`, `/console/intake`, and
`/console/intake-status` rendered without a session token, root aliases such as
`/intake` and `/intake-status` canonicalized to the `/console/...` routes,
protected `/console/security/network?tab=connections` redirected to
`/console/login`, and mobile login/intake/status views had no horizontal
overflow. The sweep found one real public-flow defect in source: after an
anonymous misconduct report is submitted, the success-page "Check status with
your token" link used raw `href="/intake-status"`, which can escape the console
basename in deployments where public routes are served under `/console`.

The fix is additive and routing-safe: `WhistleblowerIntake` now uses React
Router `Link to="/intake-status"`, letting the configured console basename
render the correct `/console/intake-status` href. Regression coverage simulates
a successful anonymous intake submission under `MemoryRouter basename="/console"`
and proves the status link href is `/console/intake-status`.

Local validation passed:
`npx vitest run src/pages/WhistleblowerIntake.test.tsx src/pages/TrustCenter.test.tsx src/pages/Login.test.tsx`,
`npm run build`, `npm run lint`, and `git diff --check`. The console was rebuilt
and redeployed. Post-deploy browser evidence used Playwright route interception
for `/api/v1/misconduct/challenge` and `/api/v1/misconduct/submit`, so no real
production report was created; the submitted state rendered the audit token,
showed "Check status with your token", and the link resolved to
`https://control-one.cloudspacetechs.com/console/intake-status`. A fresh
post-deploy public/protected sweep again showed Trust Center, root intake-status
canonicalization, mobile login, mobile intake, and protected Network redirecting
as expected, with no visible auth leak, no horizontal overflow, no same-origin
failed requests, and zero browser console warnings/errors. Host evidence after
deploy: `/healthz=ok`, strict recent console/controlplane log scans showed no
actual nginx 4xx/5xx or controlplane 5xx/panic/database-lock/analytics-unavailable
matches, no Doris FE/BE containers were running, and memory stayed within the
small-fleet envelope: console about 4.5 MiB / 256 MiB, controlplane about
83.7 MiB / 1 GiB, Redis about 6.7 MiB / 192 MiB, and ipq about 4.8 MiB /
128 MiB.

2026-06-07 node drill-down go-live audit: this pass focused on the core
operator path from fleet inventory into a specific host. The live API first
selected node `0d4893c0-867a-4bf1-8aa9-e247680280ab`
(`vmi2172335.contaboserver.net`) from `/api/v1/nodes?limit=20&offset=0`.
Direct contract checks returned HTTP 200 for node metadata, node health, node
telemetry, node services, node packages, and node-scoped connections. Observed
timings were approximately: metadata 671 ms, health 341 ms, telemetry 526 ms,
services 469 ms, packages 607 ms, and connections 944 ms.

Live browser verification then opened `/console/nodes`, authenticated through
the normal password login flow, and confirmed the fleet overview rendered the
two active agents. The selected node detail page rendered the expected
predictive score, vitals, current telemetry, and host identity. A desktop
browser pass exercised all node detail tabs: Overview, Activity, Connections,
Knowledge graph, Packages, Recommendations, and Settings. Each tab selected
correctly, showed expected content, had no visible critical error or failed-load
message, and produced no same-origin 4xx/5xx responses, request failures, page
errors, or browser console warnings/errors. The calls observed from that pass
included `/api/v1/nodes/{id}`, `/health`, `/telemetry/nodes/{id}/metrics`,
node-scoped `/connections`, `/services`, `/packages`, and
`/compliance/recommendations`.

Mobile verification at 390x844 exercised the same node's Overview, Connections,
Packages, and Settings tabs. The dense tables and tab strip stayed inside the
viewport (`overflowX=false`), expected content rendered, and there were no
same-origin failed requests, browser console warnings/errors, or page errors.
No product code changes were required in this slice. Host evidence after the
audit: `/healthz=ok`, strict recent console/controlplane log scans showed no
actual nginx 4xx/5xx or controlplane 5xx/panic/database-lock/analytics-unavailable
matches, no Doris FE/BE containers were running, and memory remained within the
small-fleet envelope: console about 4.5 MiB / 256 MiB, controlplane about
87.5 MiB / 1 GiB, Redis about 6.7 MiB / 192 MiB, and ipq about 4.8 MiB /
128 MiB.

2026-06-07 SIEM source-health export follow-up: source inspection found one
remaining export-path defect after the earlier evidence/report hardening. The
SIEM source-health investigation panel and source-investigation rows rendered
`export_url` as a plain new-tab link. SOC case export endpoints are bearer-token
protected and the login flow does not set an auth cookie, so those links could
open a 401 tab instead of delivering the audit packet. A live direct API check
confirmed the risk on an existing SOC case: the raw export URL returned HTTP
401 without a bearer token, while the same endpoint returned HTTP 200 with
`export_version=soc-case-export-v1`, 6 evidence refs, and 5 guardrails when
called with Authorization.

The fix preserves the feature: both SIEM Export controls now call
`api.exportSOCCase(...)` through the authenticated API client and save a
pretty-printed JSON packet named `soc-case-{case_id}-{date}.json`; no export
button or SOC packet capability was removed. Regression coverage proves the
source-health export is a button rather than a raw link, calls
`exportSOCCase(case_id, tenant_id)`, saves the packet through `saveBlob`, and
keeps the existing source-health workflows intact.

Local validation passed:
`npm --prefix ui run test -- src/pages/SIEMCoverage.test.tsx`,
`npm --prefix ui run build`, `npm --prefix ui run lint`, and
`git diff --check` (lint only emitted the existing ESLintRC deprecation
warning). The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key C:/Users/Son/OneDrive/cowork/bigbundle.pem`.
Production currently has four SIEM source-health rows, all `collecting`, and no
live source-health SOC case row, so browser verification used Playwright route
interception for only the source-health case list and export packet to avoid
creating a synthetic production incident. The deployed `/console/security/siem`
page rendered the intercepted source-investigation row, had zero raw
`/api/v1/soc/cases/.../export` anchors, clicked the Export button, sent an
authenticated bearer request to the export endpoint, and produced
`soc-case-22222222-3333-4444-5555-666666666666-2026-06-07.json`. The page had
no document-level horizontal overflow, no browser console warnings/errors, and
no same-origin app 4xx/5xx responses; the only request failure observed was a
Cloudflare RUM `net::ERR_ABORTED` beacon.

Post-deploy host evidence remained healthy: `/healthz=ok`, Compose config still
reports `CONTROLPLANE_ANALYTICS_MODE=small`,
`CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`, no Doris FE/BE containers were
running under the OLAP profile, memory stayed light at about console
6.0 MiB / 256 MiB, controlplane 71.4 MiB / 1 GiB, Redis 6.7 MiB / 192 MiB, and
ipq 4.8 MiB / 128 MiB, and a strict 20-minute console/controlplane log scan
showed no actual nginx 4xx/5xx, controlplane 5xx, panic, fatal, permission,
database-lock, analytics-unavailable, or Doris-unavailable matches.

2026-06-07 Users/RBAC replacement-flow follow-up: source review found a real
role-management UX and state bug in the Users console. The bulk action was
labelled "Bulk Assign Roles", but the existing API/storage contract replaces
the full role set for each selected user. The same boolean also controlled both
modal visibility and the in-flight request, so the confirmation button was
disabled as soon as the modal opened. The single-user editor also let operators
uncheck every role and press Save, although the server rejects empty role sets.

The fix preserves the feature and makes the contract explicit. The bulk action
is now "Bulk Replace Roles"; modal-open state is separate from request-in-flight
state; the success path clears selection, closes the modal, reloads users and
roles, and shows page-level confirmation; and single-user role edits now warn
that at least one role is required and disable Save until a role is selected.

Regression coverage proves empty single-user role saves are blocked without an
API call and bulk replacement calls `updateUserRoles(userId, { roles: [...] })`
once per selected user. Local validation passed:
`npm --prefix ui run test -- src/pages/Users.test.tsx`,
`go test ./controlplane/internal/server -run 'TestUserAndRoleManagement|TestRequireTenantAccess' -count=1`,
`go test ./controlplane/internal/storage -run 'TestIsBuiltInRoleName' -count=1`,
`npm --prefix ui run build`, `npm --prefix ui run lint`, and
`git diff --check` (only existing lint deprecation and CRLF warnings).

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key C:/Users/Son/OneDrive/cowork/bigbundle.pem`.
Post-deploy API integrity showed 6 users, 5 unique built-in roles, no duplicate
user-role rows, 28 permissions, admin carrying all 28 permissions, and
unauthenticated `/api/v1/users` returning HTTP 401.

Live browser verification opened `/console/users` and `/console/roles` on the
deployed site. Users rendered 6 users and 5 available roles; empty single-user
role save was blocked; bulk replacement showed replacement copy, enabled only
after a role was selected, sent one bearer-authenticated
`PATCH /api/v1/users/{id}` with body `{"roles":["operator"]}`, closed the modal,
and showed `Successfully replaced roles for 1 user(s)`. That PATCH was fulfilled
by Playwright route interception, so no production user roles were changed. The
Roles page rendered 5 total roles, 5 built-in roles, 0 custom roles, and no
delete buttons for built-ins. Both pages had no document-level horizontal
overflow, browser console warnings/errors, unexpected request failures, or
same-origin app 4xx/5xx responses.

Host evidence remained healthy: `/healthz=ok`, small analytics config still
reports `CONTROLPLANE_ANALYTICS_MODE=small`,
`CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`, no Doris FE/BE containers were
running under the OLAP profile, memory stayed light at about console
5.8 MiB / 256 MiB, controlplane 73.4 MiB / 1 GiB, Redis 5.6 MiB / 192 MiB, and
ipq 4.8 MiB / 128 MiB, and strict recent console/controlplane log scans showed
no actual nginx 4xx/5xx, controlplane 5xx, panic, fatal, permission,
database-lock, analytics-unavailable, or Doris-unavailable matches.

2026-06-07 Settings MFA and Trust Center follow-up: source review found two
bank-grade UX defects in the Settings console. MFA factor load failures were
silently converted into the empty state, so an operator could see "No MFA
factors enrolled" when the security status request had actually failed. MFA
factor revoke buttons also exposed only an icon with no explicit accessible
name, and revoke failures could be missed instead of remaining visible in the
confirmation modal. The Trust Center action used the root `/trust/:tenant`
alias, which worked by nginx redirect but should resolve directly inside the
console basename.

The fix preserves the existing features and tightens their contracts. MFA load
failures now render `MFA status unavailable: ...` and do not show the empty
factor state. Revoke controls have factor-specific accessible names and titles,
the confirmation copy names the selected factor, the modal stays open during
revocation failure, and the inline error remains visible as
`MFA action failed: ...`. The Trust Center link now uses React Router `useHref`
so the public portal link resolves directly to `/console/trust/:tenant-name`.

Regression coverage proves the Trust Center href is `/console/trust/Tenant%20A`,
MFA load failures surface as an alert instead of an empty state, and failed
revocation keeps a visible modal error while showing an error toast. Local
validation passed: `npm --prefix ui run test -- src/pages/Settings.test.tsx`,
`npm --prefix ui run build`, `npm --prefix ui run lint`, and `git diff --check`
(only the existing React Router future-flag, ESLintRC deprecation, and CRLF
warnings appeared).

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key C:/Users/Son/OneDrive/cowork/bigbundle.pem`.
The final live browser verification used isolated Playwright contexts and
route interception for `/api/v1/mfa/factors` so no production MFA state changed.
Desktop verification logged in through the real API, rendered an explicit
simulated MFA outage, confirmed the empty-factor state was not shown, refreshed
to a synthetic TOTP factor, verified one named `Revoke Authenticator app MFA
factor` control, opened factor-specific revoke confirmation copy, intercepted
one bearer-authenticated DELETE with a simulated 500, kept
`MFA action failed: Internal Server Error` visible, and confirmed the Trust
Center link resolved to `/console/trust/default`. The desktop page had no
horizontal overflow, no unexpected same-origin app 4xx/5xx responses, no
unexpected request failures, and only the expected simulated 503/500 console
resource errors. A separate isolated mobile pass at 390x844 rendered the same
synthetic MFA factor, verified the named revoke control, and had no horizontal
overflow, request failures, page errors, or console warnings/errors.

Real API and host checks after deploy remained clean: `GET /api/v1/mfa/factors`
returned `{"data":[]}`, confirming the browser pass did not leave production
MFA factors behind; `/console/settings` and `/console/trust/default` returned
HTTP 200; root `/trust/default` returned HTTP 308 to
`/console/trust/default`; `/healthz=ok`; no Doris FE/BE containers were running;
small analytics config still reports `CONTROLPLANE_ANALYTICS_MODE=small`,
`CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`; memory stayed light at about
console 4.5 MiB / 256 MiB, controlplane 78.4 MiB / 1 GiB, Redis 5.6 MiB /
192 MiB, and ipq 4.8 MiB / 128 MiB; and strict recent console/controlplane log
scans showed no actual nginx 4xx/5xx, controlplane 4xx/5xx, panic, fatal,
permission, database-lock, analytics-unavailable, or Doris-unavailable matches.

2026-06-07 Access/JIT and command-policy follow-up: source review found
operator-risk defects in the privileged-access console. JIT request creation
failures could leave the form with no visible error, access approval/denial
failures could disappear inside the inline decision panel, command ACL load
failures were silently converted into the empty state, and command ACL
create/delete failures were not reliably visible. Custom JIT TTL values below
the server's 60-second minimum could also be entered before submit.

The fix preserves the existing Access workflow while making failure modes
explicit. JIT request failures now render `Access request failed: ...` while
preserving the requested access fields. The approve/deny panel now catches
decision failures, keeps the panel open, disables controls only during the
in-flight request, and renders `Decision failed: ...`. Command policy load
failures now render `Command policy unavailable: ...` instead of the false
`No command policy rules` empty state; create/delete failures remain visible,
and failed deletes keep the confirmation modal open. Custom TTL submit is
disabled until the value is finite and at least 60 seconds.

Regression coverage proves failed JIT request submission preserves the form,
invalid custom TTLs do not call the API, failed approve decisions keep a visible
error panel, command ACL load failures do not show a false empty state, command
policy delete buttons are named for assistive technology, and command ACL
creation still uses the canonical role API list. Local validation passed:
`npm --prefix ui run test -- src/pages/Access.test.tsx`,
`npm --prefix ui run lint`, `npm --prefix ui run build`, and
`git diff --check` (only existing ESLintRC and CRLF warnings appeared).

The console-only production deploy initially exposed an operational reliability
gap: Paramiko's fixed 20-second SSH banner/auth timeout failed on the live host,
while OpenSSH succeeded after a slower handshake. `deploy/deploy_console.py`
now uses 60-second SSH handshake/auth timeouts and three connection attempts;
`python -m py_compile deploy/deploy_console.py` passed. The same deploy command
then completed successfully and rebuilt/restarted only the console container:
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`.

Live browser verification on `/console/access` used safe Playwright route
interception for mutating Access and command-policy calls, so no production JIT
request or command ACL was created, approved, or deleted. The deployed page
rendered a synthetic pending `root@prod-db-01` request; an intercepted failed
JIT POST showed `Access request failed: Service Unavailable` and preserved the
form; an intercepted failed approve kept the decision panel open with
`Decision failed: Service Unavailable`; an intercepted command ACL load failure
showed `Command policy unavailable: Service Unavailable` and did not show
`No command policy rules`; a synthetic ACL row rendered with one accessible
delete button; intercepted create/delete failures showed command-policy errors,
and the failed delete kept the confirmation modal open. A final clean Access
tab loaded with the heading and command-policy tab visible, no page alerts, and
zero browser console warnings/errors.

Real read-only API checks from the authenticated browser session returned HTTP
200 for tenants, tenant-scoped `/api/v1/access-requests`, and tenant-scoped
`/api/v1/command-acls`. Post-deploy host evidence stayed healthy: origin
`/healthz=ok`, `/console/` returned HTTP 200 with the refreshed asset timestamp,
no Doris FE/BE containers were running under the OLAP profile,
`ANALYTICS_MODE=small`, `DORIS_ENABLED=false`, memory stayed light at about
console 7.1 MiB / 256 MiB, controlplane 199.7 MiB / 1 GiB, Redis 6.7 MiB /
192 MiB, and ipq 4.8 MiB / 128 MiB, host memory had about 3.5 GiB available,
and strict recent console/controlplane log scans showed no actual nginx 4xx/5xx,
controlplane 5xx, panic, fatal, permission, database-lock,
analytics-unavailable, or Doris-unavailable matches.

2026-06-07 Small-fleet analytics architecture decision: the demo should not
try to make Doris smaller as the default path. The selected architecture is
Control One Lite Analytics: Postgres as durable ingest journal and product
truth, SQLite/WAL as the embedded recent evidence projection, Redis as bounded
hot state/queues/freshness, and Doris FE/BE at 0 MB unless an operator
explicitly selects `ANALYTICS_MODE=olap`, `DORIS_ENABLED=true`, and the Compose
`olap` profile.

The decision preserves useful product capability instead of deleting routes or
workflows. Dashboard, network security, investigation, timeline, search,
citation, and export flows should keep the same API and UI contracts in small
mode. Redis-only data is not evidence, SQLite projections must cite stable
event IDs or raw references, and Postgres remains the replay/rebuild boundary.
Projection gaps should return source/guardrail metadata with analytics-neutral
copy, not Doris-specific errors or hidden UI affordances. The detailed design
and implementation proof plan are now captured in
`docs/small-fleet-analytics-architecture.md`.

2026-06-07 Secrets vault and Redis hot-state follow-up: source review found
several operator-risk issues in the Secrets console. Secret group delete used a
native browser confirmation and a generic row action, delete and sync failures
could disappear after a toast, sync state disabled every row without naming the
active group, create allowed invalid/blank sync intervals to be silently coerced
to 900 seconds, the create overlay lacked dialog semantics and could be
accidentally dismissed by clicking the backdrop, and a secret-group load failure
could still render the false `No secret groups found` empty state.

The fix preserves the Secrets workflow while making failure and destructive
states explicit. Secret group delete now uses the shared in-app confirmation
modal with group/backend-specific copy, disabled in-flight actions, and a
durable modal error on failure. Row Sync/Delete controls now have group-specific
accessible names, failed syncs render a persistent `Secret operation failed`
alert, invalid sync intervals below 60 seconds disable submit and make no API
call, the create modal exposes dialog semantics and a named close action, and
load failures no longer show the empty state. `ConfirmModal` now supports
optional disabled confirm/cancel states so shared destructive flows can prevent
double submits while work is in flight.

Regression coverage proves the in-app delete modal avoids native `confirm`,
successful delete refreshes the list, failed delete stays visible in the modal,
row actions are named for assistive technology, failed sync remains visible
after the toast, load failures do not show a false empty state, and invalid
sync intervals do not call create. Local validation passed:
`npm --prefix ui run test -- src/pages/Secrets.test.tsx`,
`npm --prefix ui run lint`, `npm --prefix ui run build`, and
`git diff --check` (only the existing ESLintRC deprecation and CRLF warnings
appeared).

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`; the refreshed `/console/` and
`/console/secrets` assets returned HTTP 200 with `Last-Modified: Sun, 07 Jun
2026 22:44:02 GMT`.

Live desktop browser verification on `/console/secrets` used real login and
real tenant/API reads, then safe Playwright route interception for mutating
Secrets calls so no production secret group was created, synced, or deleted.
The deployed page showed `Failed to load secret groups` without
`No secret groups found` under an intercepted list outage, rendered a synthetic
`prod-vault-safe` row with named Sync/Delete controls, kept
`Sync failed for prod-vault-safe: Service Unavailable` visible after an
intercepted sync failure, opened an in-app `Delete secret group?` modal with
group/backend-specific copy, kept `Secret group delete failed: Service
Unavailable` visible after an intercepted delete failure, and blocked a 30
second create interval with no create POST. A clean real Secrets reload had no
alerts, no horizontal overflow, zero browser console warnings/errors, and the
tenant-scoped real `/api/v1/secrets/groups?limit=5` read returned HTTP 200 with
`data: []`.

Mobile verification at 390x844 reloaded the real Secrets page, found no alerts
or horizontal overflow, opened the create dialog, confirmed the invalid interval
message and disabled submit state, and recorded zero page errors, request
failures, console warnings/errors, or same-origin 4xx/5xx responses.

During host checks, live Redis was found running the stale `allkeys-lru`
eviction policy even though the repository default had already moved to
`volatile-lru`. That could evict protected queue/control keys under memory
pressure, so the live `deploy/docker-compose.yaml` was synced from the repo and
only Redis was recreated with the existing persisted volume. Redis came back
healthy with `maxmemory=128mb`, container memory `192MiB`, and
`maxmemory-policy=volatile-lru`, protecting non-TTL queue/control keys while
still allowing TTL-bound hot analytics keys to evict.

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/secrets`
returned HTTP 200, authenticated tenants and secret-groups API reads returned
HTTP 200, Redis answered `PONG`, no Doris FE/BE containers were running under
the OLAP profile, the controlplane container reported
`CONTROLPLANE_ANALYTICS_MODE=small`, `CONTROLPLANE_DORIS_ENABLED=false`, and
`CONTROLPLANE_ANALYTICS_SQLITE_CACHE_MB=16`, memory stayed light at about Redis
10.5 MiB / 192 MiB, console 6.9 MiB / 256 MiB, controlplane 121.7 MiB / 1 GiB,
and ipq 8.8 MiB / 128 MiB, and strict recent log scans showed no console
4xx/5xx, controlplane 5xx, panic, fatal, permission, database-lock,
analytics-unavailable, Doris-unavailable, small-analytics unavailable, or
Secrets 4xx/5xx matches.

2026-06-08 SOC Cases failure-mode hardening: source review found operator-risk
gaps in the incident-packet workflow. A case-list outage could collapse into a
false empty queue, a failed detail fetch could leave stale evidence from the
previous case on screen, export preview failures could become an unhandled
dead-end, and note-write failures were not durable enough for an analyst to
recover confidently.

The fix preserves the existing SOC Cases workflow and makes the failure states
explicit. Tenant changes and list failures now clear stale case/detail/export
state, the selected case is preserved only if it still exists in the refreshed
list, detail fetch failures clear stale evidence and show a `Case data
unavailable` alert, list failures show queue-recovery copy instead of `No SOC
cases yet`, export preview has disabled/loading/error states, and failed note
writes keep the analyst draft visible with a persistent alert.

Regression coverage now proves list failures do not show the false empty state,
detail failures clear stale evidence, export failures surface without rendering
an export version, and failed note submissions preserve the draft. Local
validation passed: `npm --prefix ui run test -- src/pages/Cases.test.tsx`,
`npm --prefix ui run lint`, `npm --prefix ui run build`, and
`git diff --check` for the touched files.

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`; `/console/cases` returned HTTP
200 with `Last-Modified: Sun, 07 Jun 2026 23:12:01 GMT` and no-store headers.

Live browser verification on `/console/cases` used Playwright route
interception for SOC Cases only, so no production case note or export was
created. A synthetic list outage rendered `Case data unavailable`, surfaced
`Service Unavailable`, showed queue-recovery copy, and did not show `No SOC
cases yet`. A synthetic evidence-backed case rendered the detail heading,
facts, evidence drawer, timeline, and export packet. Intercepted export and
note failures each fired exactly once, showed `Export preview failed: Service
Unavailable` and `Note failed: Service Unavailable`, did not render a failed
export version, and preserved the note draft. A clean real production reload
then had no case alerts, zero new browser warnings/errors, and the
tenant-scoped authenticated `/api/v1/soc/cases?limit=5` read returned HTTP 200
with five rows. Mobile verification at 390x844 had no alerts and no horizontal
overflow (`381/381`).

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/cases` returned
HTTP 200, no Doris FE/BE containers were running under the OLAP profile, Redis
remained `maxmemory-policy=volatile-lru` with `maxmemory=134217728`, memory
stayed light at about console 5.6 MiB / 256 MiB, Redis 10.5 MiB / 192 MiB,
controlplane 126.8 MiB / 1 GiB, and ipq 8.8 MiB / 128 MiB. Strict recent
console/controlplane log scans showed no console 4xx/5xx, no controlplane 5xx,
panic, fatal, permission, deadline, database-lock, analytics-unavailable,
Doris-unavailable, or small-analytics unavailable matches. The only recent
Cases 4xx/5xx line was an expected unauthenticated HTTP 401 from the initial
browser-side API probe before the bearer header was added; a shorter post-clean
window showed no `/api/v1/soc/cases` 4xx/5xx matches.

2026-06-08 Threat Feeds failure-mode hardening: source review found
operator-risk gaps in threat-intelligence source management. Plain-text Go
`http.Error` responses were flattened to generic status text, so important
backend guidance like missing OTX API keys could become `Bad Request`. A
threat-feed list outage could render the false `No threat feeds configured`
empty state, blacklist-summary failures could look like a warming cache,
enable/disable failures could disappear as unhandled actions, delete failures
could close the confirmation path, and blank numeric inputs could be coerced
into misleading values.

The fix preserves the existing Threat Feeds workflow while making every
operator decision recoverable. API errors now read JSON or plain-text response
bodies and suppress HTML bodies. Feed-list, summary, form, row-action, and
delete failures have separate durable states, tenant changes clear stale
errors, failed feed loads clear stale rows and show recovery copy, failed
summary loads no longer show warming-cache copy, OTX and URL-backed sources are
validated before submit, score and refresh fields keep blank state instead of
coercing to zero, row enable/disable controls are feed-specific and loading
aware, and failed deletes remain inside the shared confirmation modal with the
affected feed name visible.

Regression coverage now proves plain-text backend errors are preserved, feed
list failures avoid the false empty state, blacklist-summary failures avoid the
warming-cache copy, failed enable toggles stay visible and name the row action,
failed deletes remain in the confirmation modal, and invalid refresh intervals
block create without making an API call. Local validation passed:
`npm --prefix ui run test -- src/pages/ThreatFeeds.test.tsx
src/lib/api.normalize.test.ts`, `npm --prefix ui run lint`,
`npm --prefix ui run build`, and `git diff --check` for the touched files.

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`; `/console/security/network?tab=threats`
returned HTTP 200 with no-store headers and `Last-Modified: Sun, 07 Jun 2026
23:37:12 GMT`.

Live browser verification on
`/console/security/network?tab=threats` used the authenticated session and
safe Playwright route interception for Threat Feeds only, so no production
feed was created, toggled, or deleted. The deployed page showed a feed-list
outage alert, avoided the false empty state, showed table recovery copy,
surfaced a blacklist-summary outage without warming-cache copy, rendered a
synthetic `FireHOL production` row, kept an intercepted toggle failure visible,
kept an intercepted delete failure inside the modal, blocked missing OTX API
keys before create, preserved a plain-text create error, and confirmed the
intercepted create, toggle, and delete routes fired exactly once. A clean real
production reload then had no Threat Feeds alerts, zero browser console
warnings/errors, a healthy direct threat-feeds API read, and a healthy direct
threat-summary API read. Mobile verification at 390x844 had no alerts and no
horizontal overflow (`381/381`).

Post-fix host evidence stayed healthy: `/healthz=ok`,
`/console/security/network?tab=threats` returned HTTP 200, no Doris FE/BE
containers were running under the OLAP profile, Redis remained
`maxmemory-policy=volatile-lru` with `maxmemory=134217728`, memory stayed light
at about console 4.5 MiB / 256 MiB, Redis 10.5 MiB / 192 MiB, controlplane
129.2 MiB / 1 GiB, and ipq 8.8 MiB / 128 MiB. Strict recent
console/controlplane log scans showed no console 4xx/5xx, no controlplane 5xx,
panic, fatal, permission, deadline, database-lock, analytics-unavailable,
Doris-unavailable, small-analytics unavailable, or Threat Feeds 4xx/5xx
matches.

2026-06-08 Users and Roles RBAC hardening: source review and saved browser
evidence found bank-grade access-administration risks. User rows could render
duplicate role chips such as `VIEWER VIEWER` or `ADMIN ADMIN`, users and roles
load failures could still collapse into false empty states, the single-user and
bulk role editors lacked proper dialog semantics, bulk role failures were not
visible inside the active modal, the Roles page used native browser
`confirm`/`alert`, failed permission writes could feel accepted until a later
refresh, and custom role create/delete errors were not recoverable enough for a
CISO/admin workflow.

The fix preserves the existing Users directory, bulk role replacement, and live
Role / permission matrix. User role names are now normalized and deduplicated
for display, KPI counting, side-panel role counts, and edit initialization.
Selected users are pruned when the visible page changes. Users and roles load
failures now show recovery empty states instead of `No users found` or `No
roles yet`. The user role modals expose dialog semantics and named close
actions, failed bulk replacements stay visible inside the modal, Roles uses the
shared in-app confirmation modal for custom role deletion, custom role create
failures render inline instead of using `alert`, destructive delete failures
stay in the modal, permission checkboxes have role/permission-specific names,
and failed permission writes rollback the optimistic checkbox state with a
durable `Role operation failed` alert.

Regression coverage now proves duplicate role assignments render once, user
load failures avoid the false empty state, empty single-user role sets cannot be
saved, bulk replacement copy and API behavior remain explicit, failed bulk role
updates stay in the modal, built-in roles remain delete-protected even when IDs
vary, older built-in payloads are still protected by name, role load failures
avoid the false empty state, failed permission writes rollback, failed custom
role deletes stay in the confirmation modal, and custom role create failures do
not call browser `alert`. Local validation passed:
`npm --prefix ui run test -- src/pages/Users.test.tsx
src/pages/Roles.test.tsx`, `npm --prefix ui run lint`,
`npm --prefix ui run build`, and `git diff --check` for the touched files.

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`; `/console/users` and
`/console/roles` returned HTTP 200 with no-store headers and `Last-Modified:
Mon, 08 Jun 2026 00:07:40 GMT`.

Live browser verification used the authenticated production session and safe
Playwright route interception for RBAC mutations, so no production user or role
was changed. The deployed Users page showed a synthetic user-list outage alert
without the false `No users found` state, rendered a duplicate-role synthetic
`Ada Admin` row with one `viewer` and one `operator` chip, kept an intercepted
bulk role replacement failure visible inside the modal, and confirmed the
PATCH route fired exactly once. The deployed Roles page showed a synthetic role
catalog outage alert without `No roles yet`, protected the built-in role, only
exposed delete for the synthetic custom role, rolled back an intercepted
permission-write failure, preserved a plain-text create error, kept an
intercepted delete failure inside the confirmation modal, and confirmed the
PUT, POST, and DELETE routes fired exactly once. A clean real production reload
then had no Users or Roles alerts, zero browser console warnings/errors, direct
authenticated API reads returned HTTP 200 for users, roles, permissions, and
the role matrix, and live users had no duplicate-role API row at verification
time. Mobile verification at 390x844 had no alerts and no body horizontal
overflow on Users or Roles (`381/381`).

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/users` and
`/console/roles` returned HTTP 200, no Doris FE/BE containers were running
under the OLAP profile, Redis remained `maxmemory-policy=volatile-lru` with
`maxmemory=134217728`, memory stayed light at about console 4.5 MiB / 256 MiB,
Redis 10.6 MiB / 192 MiB, controlplane 132.5 MiB / 1 GiB, and ipq 8.8 MiB /
128 MiB. Strict recent console/controlplane log scans showed no console
4xx/5xx, no controlplane 5xx, panic, fatal, permission, deadline,
database-lock, analytics-unavailable, Doris-unavailable, small-analytics
unavailable, or Users/Roles/Permissions 4xx/5xx matches.

2026-06-08 Alerts SOC triage failure-mode hardening: source review found
operator-risk gaps in the top-level alert triage surface. Alert-list failures
could leave stale rows or collapse into false `All clear` / `No alerts` copy,
failed acknowledgements were not visible as durable row-scoped action errors,
failed dispositions wrote into generic page error state instead of the active
evidence modal, correlation-rule list failures were swallowed as false empty
state, rule create failures were not shown in the form, and rule delete
failures could disappear from the confirmation path. The rule delete icon also
lacked a rule-specific accessible name.

The fix preserves the existing inbox, critical response center, evidence
disposition modal, and correlation-rule workflow while making failures
recoverable. Alert load, alert action, disposition, rules load, rule create,
and rule delete errors now have separate durable states. Failed alert loads
clear stale rows and show `Alerts could not be loaded`; failed ACKs name the
affected alert and keep the row visible; failed dispositions stay inside the
open modal; failed rule loads show `Correlation rules could not be loaded`;
failed creates keep the entered rule name; failed deletes remain inside the
confirmation modal with the rule name visible. ACK, review, and delete controls
now expose row-specific accessible names and loading/disabled states.

Regression coverage now proves alert-list failures avoid false empty/all-clear
states, failed acknowledgements remain visible and row-specific, failed
dispositions stay in the resolution modal, correlation-rule list failures avoid
false empty state, failed rule creates preserve the draft, and failed rule
deletes stay in the confirmation modal. Local validation passed:
`npm --prefix ui run test -- src/pages/Alerts.test.tsx`,
`npm --prefix ui run lint`, `npm --prefix ui run build`, and
`git diff --check -- ui/src/pages/Alerts.tsx ui/src/pages/Alerts.test.tsx`.

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`; `/console/alerts` returned HTTP
200 with no-store headers and `Last-Modified: Mon, 08 Jun 2026 00:32:52 GMT`.

Live browser verification on `/console/alerts` used the authenticated
production session and safe Playwright route interception for Alerts and
correlation-rule endpoints, so no production alert or rule was acknowledged,
resolved, created, or deleted. Synthetic checks showed the alert-list outage
error and recovery copy without `All clear` or `No alerts`, rendered a
synthetic `Critical SSH burst` row, kept an intercepted ACK failure visible,
kept an intercepted disposition failure inside the evidence modal, showed the
correlation-rule outage without `No correlation rules`, preserved a failed
rule-create draft, and kept an intercepted rule-delete failure inside the
confirmation modal. Intercept counts were alert list 503 x2, ACK 503 x1,
disposition 503 x1, rule list 503 x1, create 400 x1, and delete 503 x1. A
clean real production reload then had the Alerts heading visible, zero
role-alert errors, zero browser console warnings/errors, no desktop or mobile
horizontal overflow, and direct authenticated API reads returned HTTP 200 for
tenants, open alerts, and correlation rules for tenant
`00000000-0000-0000-0000-000000000001`.

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/alerts`
returned HTTP 200, no Doris FE/BE containers were running under the OLAP
profile, Redis remained `maxmemory-policy=volatile-lru` with
`maxmemory=134217728`, memory stayed light at about console 4.5 MiB / 256 MiB,
Redis 10.6 MiB / 192 MiB, controlplane 132.2 MiB / 1 GiB, and ipq 8.8 MiB /
128 MiB. Strict recent console/controlplane log scans showed no console
4xx/5xx, no controlplane 5xx, panic, fatal, permission, deadline,
database-lock, analytics-unavailable, Doris-unavailable, small-analytics
unavailable, or Alerts/correlation-rules 4xx/5xx matches.

2026-06-08 Control Room action failure and stale-data hardening: source review
found several operator-risk gaps in the main command surface. A failed overview
refresh could leave previous data on screen without clearly marking it stale,
or an initial load failure could collapse critical panels into false healthy
states such as `No open incidents`, `No approvals waiting`, `No open IP
behavior anomalies`, `No webserver inventory yet`, and `No lanes yet`.
Webserver apply/rollback and network isolation actions used native browser
confirmation prompts, failed action errors were not kept inside the active
confirmation path, and repeated row buttons lacked action-specific accessible
names.

The fix preserves the existing Control Room, lane map, queue, IP behavior,
incident, webserver, and isolation workflows while making degraded states
explicit. Failed initial overview loads now clear same-scope data and show
unavailable states for each dependent panel. Failed refreshes keep only
same-tenant/same-period last-known data and label it `Last known data`.
Overview failures render as alerts. Webserver apply/rollback and network
isolation changes now use the shared in-app confirmation modal, failures stay
visible in the modal and row, success and busy states are row scoped, and
webserver/isolation buttons expose target-specific accessible names.

Regression coverage now proves initial overview failures avoid false healthy
empty states, failed refreshes mark last-known data stale, failed webserver
apply actions use the in-app modal and remain visible, and failed isolation
changes use the in-app modal and remain visible. Local validation passed:
`npm --prefix ui run test -- src/pages/ControlRoom.test.tsx`,
`npm --prefix ui run lint`, `npm --prefix ui run build`, and
`git diff --check -- ui/src/pages/ControlRoom.tsx
ui/src/pages/ControlRoom.test.tsx`.

The console-only production deploy completed with
`python deploy/deploy_console.py --host 139.162.40.237 --user root --key
C:/Users/Son/OneDrive/cowork/bigbundle.pem`; `/console/control-room` returned
HTTP 200 with no-store headers and `Last-Modified: Mon, 08 Jun 2026 00:58:14
GMT`.

Live browser verification on `/console/control-room` used the authenticated
production session and safe Playwright route interception for Control Room
reads and mutations, so no production webserver policy or node isolation state
was changed. Synthetic checks showed an overview-store outage alert,
unavailable states for confidence, lanes, queue, IP behavior, incidents, and
webserver inventory, and none of the false healthy empty states. A synthetic
overview then rendered `Server Health`, `nginx nginx`, and `core-api-01`; a
failed refresh kept `Server Health` as last-known data with stale copy. Failed
webserver apply and failed isolation changes stayed visible inside the
confirmation modal and the affected row. Intercept counts were overview 503 x4,
webserver apply 503 x1, and isolation 503 x1. A clean real production reload
then had the Control Room heading visible, zero role-alert errors, zero browser
console warnings/errors, no desktop or mobile horizontal overflow, and a direct
authenticated overview API read returned HTTP 200 with six lanes for tenant
`00000000-0000-0000-0000-000000000001`.

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/control-room`
returned HTTP 200, no Doris FE/BE containers were running under the OLAP
profile, Redis remained `maxmemory-policy=volatile-lru` with
`maxmemory=134217728`, memory stayed light at about console 4.5 MiB / 256 MiB,
Redis 10.6 MiB / 192 MiB, controlplane 131.7 MiB / 1 GiB, and ipq 8.8 MiB /
128 MiB. Strict recent console/controlplane log scans showed no console
4xx/5xx, no controlplane 5xx, panic, fatal, permission, deadline,
database-lock, analytics-unavailable, Doris-unavailable, small-analytics
unavailable, or Control Room/webserver/isolation 4xx/5xx matches.

2026-06-08 Nodes fleet failure-state and action hardening: source review found
several production-readiness gaps in the Servers/Nodes workflow. The at-risk
fleet request was not scoped to the active tenant, so a tenant-scoped page could
ask for aggregate at-risk data. Nodes load failures could clear data while the
page still rendered misleading healthy empty states such as `No nodes` and `No
nodes online`. At-risk and node-health enrichment failures were swallowed, the
table quick Airgap/Return actions executed without in-app confirmation, failed
isolation changes were only toasted, failed agent-update queue requests closed
their confirmation path, and repeated table/view buttons lacked target-specific
accessible names.

The fix preserves the existing fleet map, cards, table, rollout, health, and
isolation workflows while making degraded states and risky actions explicit.
At-risk requests now pass the active tenant ID. Initial nodes failures render
`Fleet data unavailable`, `Fleet map unavailable`, and a fleet-list unavailable
state instead of false empties. At-risk and health-score failures render alerts
while keeping the rest of the page usable. Table isolation changes now use the
shared confirmation modal, failed isolation errors remain visible in the modal
and row, failed agent update queue requests stay inside the update modal, and
node/action buttons now expose target-specific accessible names.

Regression coverage now proves fleet and at-risk reads are scoped to the active
tenant, nodes load failures avoid false empty states, at-risk lookup failures
surface visibly, failed isolation changes require in-app confirmation and remain
visible, and failed agent-update queue requests remain in the confirmation
modal. Local validation passed:

- `npm --prefix ui run test -- src/pages/Nodes.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check -- ui/src/pages/Nodes.tsx ui/src/pages/Nodes.test.tsx`

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/nodes` returned HTTP 200 with no-store headers and
`Last-Modified: Mon, 08 Jun 2026 01:49:01 GMT`.

Live browser verification on `/console/nodes` used the authenticated production
session and safe Playwright route interception for Nodes reads and mutations, so
no production node isolation or agent-update state was changed. Synthetic outage
checks showed the nodes-list outage state, the at-risk outage alert, no `No
nodes` or `No nodes online` false empty copy, and intercept counts of nodes list
503 x2 and at-risk 503 x1. A synthetic node then rendered in the table; failed
Airgap and failed agent update requests stayed visible in the confirmation
modal, with the isolation failure also retained on the row. Mutation intercept
counts were isolation 503 x1 and agent update 503 x1. A clean real production
reload then had the Nodes heading visible, zero role-alert errors, zero browser
console warnings/errors, no desktop or mobile horizontal overflow, direct
authenticated `/api/v1/nodes` returning HTTP 200 with two nodes, and direct
authenticated `/api/v1/health/at-risk` returning HTTP 200 with zero current
at-risk nodes.

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/nodes`
returned HTTP 200, no Doris FE/BE containers were running under the OLAP
profile, Redis remained `maxmemory-policy=volatile-lru` with
`maxmemory=134217728`, memory stayed light at about console 4.5 MiB / 256 MiB,
Redis 10.6 MiB / 192 MiB, controlplane 132.6 MiB / 1 GiB, and ipq 8.8 MiB /
128 MiB. Strict recent console/controlplane log scans showed no actual console
4xx/5xx, no controlplane 5xx, panic, fatal, permission, deadline,
database-lock, analytics-unavailable, Doris-unavailable, or small-analytics
unavailable matches. Two earlier Nodes API 401s came from an initial
unauthenticated verification probe; the bearer-token rerun returned HTTP 200 for
both Nodes and at-risk reads.

2026-06-08 Audit trail and compliance-reporting hardening: source review found
several bank-demo correctness and UX gaps in the audit/reporting workflow.
Audit log load failures already showed an error banner, but the dependent KPIs,
table empty state, and pagination still collapsed into false zero/empty states
such as `No audit entries` and `Page 1 of 1`. The `Export CSV` action exported
only the currently loaded page and ignored the client-side search filter, while
its label implied a broader audit export. Audit Reports defaulted to the first
tenant in the tenant list instead of the active tenant, swallowed report-history
load failures into `No generated reports`, allowed inverted reporting periods to
reach the create endpoint, left form fields under-labelled, and presented failed
or pending reports as downloadable.

The fix preserves the existing audit log, filters, chart, pagination, report
history, generate, and download workflows while making degraded states and
export scope explicit. Audit-log failures now render `Audit trail unavailable`,
show `N/A` in dependent KPIs, disable export, and replace pagination with an
unavailable state instead of false empty results. The CSV action is now labelled
`Export page CSV` and exports the visible filtered page. Audit Reports now
defaults to the active tenant, surfaces `Report history unavailable` with retry,
keeps download failures visible, validates period start/end before create,
associates labels with report controls, and disables downloads for reports that
are not ready.

Regression coverage now proves audit failures avoid false empty states, audit
CSV exports only visible filtered rows, report history loads for the active
tenant, report-history failures avoid false `No generated reports` copy,
inverted report periods do not call create, download failures stay visible, and
failed reports cannot be downloaded. Local validation passed:

- `npm --prefix ui run test -- src/pages/Audit.test.tsx src/pages/AuditReports.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check -- ui/src/pages/Audit.tsx ui/src/pages/AuditReports.tsx ui/src/pages/Audit.test.tsx ui/src/pages/AuditReports.test.tsx`

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/audit` returned HTTP 200 with no-store headers and
`Last-Modified: Mon, 08 Jun 2026 02:23:41 GMT`.

Live browser verification on `/console/audit` used the authenticated production
session and safe Playwright route interception for audit/report reads and the
report download path, so no production report was created, downloaded, or
mutated. Synthetic checks showed audit-log outage copy with two `Audit trail
unavailable` states, three `N/A` KPI values, disabled export, pagination
unavailable copy, and no `No audit entries`; the audit-read intercept count was
503 x1. Report-history outage checks showed two `Report history unavailable`
states and no `No generated reports`; the report-list intercept count was 503
x1. A synthetic ready report then kept an intercepted download failure visible
with `artifact missing`, a synthetic failed report rendered a disabled
`PCI-DSS report is not ready` action, and inverted period dates showed the
validation error without sending a create request. Intercept counts were
synthetic report list 200 x1, download 503 x1, and create POST x0.

A clean real production reload then had the Audit heading visible, zero
role-alert errors, zero browser console warnings/errors, and direct
authenticated API reads returning HTTP 200 for `/api/v1/audit` with five sampled
rows out of 2,618 and `/api/v1/compliance/reports` with three reports. Desktop
showed zero horizontal overflow. A 390x844 mobile reload had zero role-alert
errors and no document/body horizontal overflow; the audit table is wider than
the viewport but is contained inside its own `overflow-auto` table scroller, so
the page itself does not scroll sideways.

Post-fix host evidence stayed healthy: `/healthz=ok`, `/console/audit` returned
HTTP 200, no Doris FE/BE containers were running under the OLAP profile, Redis
remained `maxmemory-policy=volatile-lru` with `maxmemory=134217728`, memory
stayed light at about console 7.1 MiB / 256 MiB, Redis 3.7 MiB / 192 MiB,
controlplane 197 MiB / 1 GiB, and ipq 8.8 MiB / 128 MiB. Strict recent
console/controlplane log scans showed no console 4xx/5xx and no controlplane
5xx, panic, fatal, permission, deadline, database-lock, analytics-unavailable,
Doris-unavailable, or small-analytics unavailable matches; recent audit and
report API log lines were normal HTTP 200 reads.

2026-06-08 Webserver auto-control failure-state and action hardening: source
review found several operator-risk gaps in the dedicated webserver
capture/enforcement workflow. Inventory load failures surfaced an error but
could still collapse the KPIs and inventory panel into misleading zero/empty
states such as `No webserver inventory`. Config-action and receipt-history load
failures were folded into a generic status string while the dependent panels
still rendered `No config actions` and `No receipts`. Capture, enforcement, and
rollback actions used native browser confirmation prompts, failed apply errors
were easy to lose after the confirmation path closed, and repeated action
buttons lacked target-specific accessible names.

The fix preserves the existing webserver inventory, application-root context,
plan, capture, enforcement, rollback, action history, receipt history, and
Control Room navigation workflows while making degraded states explicit.
Inventory failures now clear same-scope rows, show `Webserver inventory
unavailable`, render `N/A` in dependent KPIs, and provide retry. History and
receipt failures now show `Webserver action history unavailable`, `Config
action history unavailable`, and `Receipts unavailable` instead of false
empty-state copy. Capture/enforcement/rollback actions now use the shared
in-app confirmation modal, keep failed action errors visible inside the modal
and selected-instance context, keep busy/success/error state scoped by
instance/action, and expose target-specific accessible names such as `Apply
capture for nginx nginx`.

Regression coverage now proves inventory failures avoid false empty states,
failed capture apply actions use the in-app modal without `window.confirm` and
remain visible, and history failures avoid false `No config actions` / `No
receipts` copy. Local validation passed:

- `npm --prefix ui run test -- src/pages/WebserverAutoControl.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check -- ui/src/pages/WebserverAutoControl.tsx ui/src/pages/WebserverAutoControl.test.tsx`

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/security/webservers` returned HTTP 200 with
no-store headers and `Last-Modified: Mon, 08 Jun 2026 02:41:22 GMT`.

Live browser verification on `/console/security/webservers` used the
authenticated production session and safe Playwright route interception for
synthetic webserver reads/history/action failures, so no production webserver
configuration was changed. Synthetic inventory outage checks returned
`Webserver inventory unavailable` and four `N/A` KPI values, with zero `No
webserver inventory` false-empty states and an inventory 503 intercept count of
1. Synthetic history outage checks rendered `nginx nginx`, `Webserver action
history unavailable`, `Config action history unavailable`, and `Receipts
unavailable`, with zero `No config actions` or `No receipts` false-empty
states; intercept counts were webserver list 200 x1 and history/receipt 503 x2.
Synthetic capture failure checks showed the in-app `Apply capture webserver
control?` modal before any POST, `applyHitsBeforeConfirm=0`,
`applyHitsAfterConfirm=1`, `nativeDialogs=0`, visible `Webserver action
failed`, and the failed message retained in the dialog and selected-instance
context.

A clean real production reload then had the `Capture and enforcement` heading
visible, zero role-alert errors, zero browser console warnings/errors, no
desktop document/body horizontal overflow, and direct authenticated
`/api/v1/webservers` returning HTTP 200 with two webserver records for tenant
`00000000-0000-0000-0000-000000000001`. A 390x844 mobile reload also had zero
role-alert errors and no document/body horizontal overflow; the application-root
table is wider than the viewport but is contained inside its intended
horizontal table scroller, so the page itself does not scroll sideways.

Post-fix host evidence stayed healthy: `/healthz=ok`,
`/console/security/webservers` returned HTTP 200, no Doris FE/BE containers
were running under the OLAP profile, Redis remained
`maxmemory-policy=volatile-lru` with `maxmemory=134217728`, memory stayed light
at about console 4.5 MiB / 256 MiB, Redis 3.7 MiB / 192 MiB, controlplane 233.1
MiB / 1 GiB, and ipq 8.8 MiB / 128 MiB. Strict recent log scans showed no
console 4xx/5xx and no controlplane 5xx, panic, fatal, permission, deadline,
database-lock, analytics-unavailable, Doris-unavailable, or small-analytics
unavailable matches. Recent webserver API log lines were normal HTTP 200 reads
in a few milliseconds; one 401 line was from an intentional direct fetch without
the bearer header during verification and the corrected bearer-token rerun
returned HTTP 200.

2026-06-08 Control Room drill-down exposure containment hardening: source
review found a remaining operator-risk gap in the lane drill-down workflow.
The exposure drill-down's whitelist and airgap containment controls still used
native `window.confirm`, so a bank operator could not see consistent product
context, queued scope, or retained failure evidence inside the app. Failed
isolation updates were rendered only as small inline text. A same-scope refresh
failure could keep prior detail data visible without clearly labelling it as
last-known data, and a cross-period/tenant failed refresh could retain old-scope
detail rows under the newly selected scope.

The fix preserves the existing Control Room drill-down, source navigation,
exposure posture, public-listener table, firewall table, app/DB coverage, period
filtering, and node isolation workflows while making risky actions and stale
state explicit. Exposure containment now uses the shared in-app confirmation
modal, with action-specific names such as `Apply 24 hour whitelist to
edge-web-02`. Failed containment stays visible in the modal and in the exposure
panel context, with row-scoped busy/error state. Same-scope refresh failures now
render `Detail refresh failed` and explicitly label the data below as
last-known; cross-scope loads clear old detail data before fetching the selected
scope.

Regression coverage now proves the exposure drill-down still renders firewall
and isolation posture, containment uses the in-app modal without
`window.confirm`, failed containment remains visible, and the existing app/DB
coverage filters still work. Local validation passed:

- `npm --prefix ui run test -- src/pages/ControlRoomDrilldown.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check -- ui/src/pages/ControlRoomDrilldown.tsx ui/src/pages/ControlRoomDrilldown.test.tsx`

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/control-room/exposure?period=24h` returned HTTP 200
with no-store headers and `Last-Modified: Mon, 08 Jun 2026 03:04:35 GMT`.

Live browser verification on `/console/control-room/exposure?period=24h` used
the authenticated production session and safe Playwright route interception for
overview and isolation failure paths, so no production node isolation state was
changed. Synthetic initial overview outage checks showed `Detail data
unavailable`, surfaced `overview store unavailable`, and avoided false `No
evidence rows` / `No queued actions` detail empties. Synthetic same-scope
refresh failure checks showed `Detail refresh failed`, `last-known detail data
for 24h`, and kept the synthetic exposure lane visible. Synthetic containment
failure checks showed the `Apply whitelist exposure containment?` modal before
any POST, `postHitsBeforeConfirm=0`, `postHitsAfterConfirm=1`,
`nativeDialogs=0`, visible `Exposure action failed`, and the failed message
retained in the modal and page context.

A clean real production reload then had the exposure posture panel visible,
zero role-alert errors, zero browser console warnings/errors, and direct
authenticated `/api/v1/control-room/overview?period=24h` returning HTTP 200 with
six lanes and 49 exposure public-listener rows for tenant
`00000000-0000-0000-0000-000000000001`. Desktop showed no document/body
horizontal overflow. A 390x844 mobile reload also had zero role-alert errors and
no document/body horizontal overflow; the public-listener table is wider than
the viewport but is contained inside its intended table scroller, so the page
itself does not scroll sideways.

Post-fix host evidence stayed healthy: `/healthz=ok`,
`/console/control-room/exposure?period=24h` returned HTTP 200, no Doris FE/BE
containers were running under the OLAP profile, Redis remained
`maxmemory-policy=volatile-lru` with `maxmemory=134217728`, memory stayed light
at about console 6.5 MiB / 256 MiB, Redis 3.7 MiB / 192 MiB, controlplane 289.8
MiB / 1 GiB, and ipq 8.8 MiB / 128 MiB. Strict recent log scans showed no
console 4xx/5xx and no controlplane 5xx, panic, fatal, permission, deadline,
database-lock, analytics-unavailable, Doris-unavailable, or small-analytics
unavailable matches. Recent control-room overview API log lines were normal
HTTP 200 reads in about 13-21 ms, and no production node-isolation API line was
emitted by the synthetic containment test because the POST was intercepted in
the browser.

2026-06-08 Hypervisor inventory failure-state and destructive-action
hardening: source review found remaining operator-risk gaps in the dedicated
hypervisor and provider-credential workflow. Inventory load failures could
collapse the page into misleading tenant-empty states such as `No hypervisor
hosts` and `No credentials`. Host and credential deletion still used native
browser confirmation prompts, which gave operators less context than the rest
of the app, and failed removal attempts were easy to lose after the prompt
flow. Host scan, verify, and remove controls also lacked target-specific
accessible names when multiple rows were present.

The fix preserves the existing hypervisor registration, provider credential,
scan, verify, delete, cluster-reference, job, and audit-history workflows while
making degraded states explicit. Inventory failures now clear same-scope rows,
render `Hypervisor inventory unavailable`, keep failed load details visible,
and show `Hypervisor hosts unavailable` / `Provider credentials unavailable`
instead of false tenant-empty copy. Host and credential removal now use the
shared in-app confirmation modal, keep failed deletion evidence visible in the
modal and row context, scope busy/error state by target, and expose action names
such as `Remove hypervisor host lon-kvm-01` and `Delete provider credential
kvm-root`.

Regression coverage now proves inventory failures avoid false empty states,
failed host removal uses the in-app modal without `window.confirm`, and failed
credential deletion remains visible after confirmation. Local validation
passed:

- `npm --prefix ui test -- Hypervisors.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check`

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/hypervisors` returned HTTP 200 with no-store
headers and `Last-Modified: Mon, 08 Jun 2026 04:12:48 GMT`.

Live browser verification on `/console/hypervisors` used the authenticated
production session and safe Playwright route interception for synthetic
inventory and delete failures, so no production hypervisor host or provider
credential was removed. Synthetic inventory outage checks showed `Hypervisor
inventory unavailable`, `Hypervisor hosts unavailable`, and `Provider
credentials unavailable`, with zero false `No hypervisor hosts` / `No
credentials` empty states. Synthetic host removal and credential deletion
checks showed the in-app confirmation dialog before any DELETE, had
`deleteHitsBeforeConfirm=0`, `deleteHitsAfterConfirm=1`, `nativeDialogs=0`, and
kept `Removal failed` plus the target-specific failure message visible in the
dialog and row context.

A clean real production reload then had the hypervisor page heading visible,
zero role-alert errors, zero browser console warnings/errors, no stale Doris
copy, and no document/body horizontal overflow. Post-fix host evidence stayed
healthy: `/healthz=ok`, `/console/hypervisors` returned HTTP 200, no Doris
FE/BE containers were running under the small OLAP profile, and memory stayed
light at about console 4.5 MiB / 256 MiB, Redis 3.7 MiB / 192 MiB,
controlplane 370.8 MiB / 1 GiB, landing 4.7 MiB / 128 MiB, and ipq 8.8 MiB /
128 MiB. Strict recent log scans showed no panic, fatal, SQLite lock,
analytics, hypervisor, or provider-credential errors; recent hypervisor and
provider-credential API lines were normal HTTP 200 reads in a few milliseconds.

2026-06-08 Patch Management partial-load and fleet-action hardening: source
review found several remaining operator-risk gaps in the fleet patching
workflow. Secondary API failures for managed proxies, maintenance windows, and
pending approvals were silently converted to empty lists, so an outage could
look like `No managed proxies`, `No maintenance windows`, or `No pending
approvals`. A deployment-list failure could also leave the KPI row looking like
zero activity. Proxy removal, maintenance-window force-close, and approval
denial still used native browser confirmation prompts, so operators did not get
consistent Control One context, target scope, or retained failure evidence
before queueing fleet-impacting actions.

The fix preserves the existing deployment list, per-node deployment selector,
proxy install/remove, maintenance-window schedule/open/close/force-close, and
approval approve/deny workflows while making degraded states explicit. Patch
loads now settle each data source independently, clear failed same-scope rows,
show `Patch management data unavailable` or `Patch management data partially
unavailable`, render deployment KPIs as `N/A` when deployments cannot load, and
use `Patch deployments unavailable`, `Managed proxies unavailable`,
`Maintenance windows unavailable`, and `Patch approvals unavailable` instead of
false empty-state copy. Proxy removal, force-close, and approval denial now use
the shared in-app confirmation modal, keep failed action evidence visible in
the modal and table row, scope busy/error state by target, and expose
target-specific accessible action names such as `Remove patch proxy
patch-proxy.local:3128`, `Force-close maintenance window Emergency patch
window`, and `Deny patch deployment for node node-synth-123456`.

Regression coverage now proves partial load failures avoid false empty states,
deployment failures render unavailable KPIs, and failed proxy removal,
maintenance-window force-close, and approval denial use the in-app modal
without native `confirm` while retaining failed action evidence. Local
validation passed:

- `npm --prefix ui test -- PatchManagement.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check`
- `rg -n "window\\.confirm|confirm\\(" ui/src/pages/PatchManagement.tsx ui/src/pages/PatchManagement.test.tsx` returned no matches.

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/infrastructure/patch` returned HTTP 200 with
no-store headers and `Last-Modified: Mon, 08 Jun 2026 04:38:09 GMT`. The
deployed build served `PatchManagement-DSL0STtn.js`.

Live browser verification on `/console/infrastructure/patch` used the
authenticated production session and safe Playwright route interception for
synthetic patch API outages and action failures, so no production proxy,
maintenance window, or patch approval was changed. Synthetic full patch-store
outage checks showed `Patch management data unavailable`, `Patch deployments
unavailable`, zero false `No deployments yet` copy, and unavailable KPI values.
Synthetic action failure checks showed the in-app dialogs before any mutation
request, with `deletesBeforeConfirm=0` and `postsBeforeConfirm=0`; after
confirmation, each intercepted request count was exactly 1, `nativeDialogs=0`,
and `Proxy removal failed`, `Window force-close failed`, and `Patch approval
denial failed` remained visible with target-specific failure messages in both
modal and table context.

A clean real production reload then had the Patch Management heading visible,
zero role-alert errors, zero browser console warnings/errors, no desktop
document/body horizontal overflow, and no mobile document/body horizontal
overflow at 390x844. Post-fix host evidence stayed healthy: `/healthz=ok`,
`/console/infrastructure/patch` returned HTTP 200, no Doris FE/BE containers
were running, Redis remained `maxmemory-policy=volatile-lru` with
`maxmemory=134217728`, and memory stayed light at about console 6.3 MiB / 256
MiB, Redis 3.7 MiB / 192 MiB, controlplane 383.2 MiB / 1 GiB, landing 4.7 MiB
/ 128 MiB, and ipq 8.8 MiB / 128 MiB. Recent controlplane patch API lines were
normal HTTP 200 GET reads in about 2-4 ms, and a strict recent log scan showed
no real patch POST/DELETE lines during the synthetic browser test window.

2026-06-08 Compliance policy inventory and deletion hardening: source review
found a remaining operator-risk gap in the compliance policy workflow. Policy
inventory load failures only showed a toast and could leave the policy manager
stuck at the load prompt or stale from the prior tenant/filter context.
Deleting a policy still used a native browser confirmation prompt, so a bank
operator did not see consistent Control One context, impact copy, target scope,
or retained failure evidence before removing a policy that can affect future
evaluations, assignments, reports, and audit posture.

The fix preserves the existing compliance posture, policy creation, assignment,
version, promotion, evaluation, evidence, framework, and report workflows while
making the risky policy-manager states explicit. Policy loads now clear
same-scope rows on failure, reset stale rows when tenant scope/filter changes,
show `Compliance policies unavailable`, keep the backend load message visible,
and render an unavailable empty state instead of false `No policies` copy.
Policy deletion now uses the shared in-app confirmation modal, explains that
existing scan results, audit history, and reports remain available, keeps failed
deletion evidence visible in the modal and policy row, scopes busy/error state
by policy, and exposes target-specific accessible names such as `Delete
compliance policy SSH baseline`.

Regression coverage now proves failed policy loads avoid false empty states and
failed policy deletion uses the in-app modal without native `window.confirm`
while retaining the failed backend message. Local validation passed:

- `npm --prefix ui test -- Compliance.test.tsx`
- `npm --prefix ui run lint`
- `npm --prefix ui run build`
- `git diff --check`
- `rg -n "window\\.confirm|\\bconfirm\\(" ui/src/pages/Compliance.tsx ui/src/pages/Compliance.test.tsx` returned no matches.

The console-only production deploy completed successfully against
`139.162.40.237`; `/console/compliance?tab=policies` returned HTTP 200 with
no-store headers and `Last-Modified: Mon, 08 Jun 2026 04:56:25 GMT`. The
deployed build served `Compliance-CwpNYoFJ.js`.

Live browser verification on `/console/compliance?tab=policies` used the
authenticated production session and safe Playwright route interception for
synthetic policy inventory and delete failures, so no production compliance
policy was deleted. Synthetic policy-store outage checks showed two visible
`Compliance policies unavailable` signals, surfaced `synthetic policy store
offline`, and had zero false `No policies` states. Synthetic deletion checks
showed the `Delete compliance policy SSH baseline?` in-app dialog before any
DELETE, `deleteHitsBeforeConfirm=0`, `deleteHitsAfterConfirm=1`,
`nativeDialogs=0`, visible `Policy deletion failed`, and the failed delete
message retained in modal and row context.

A clean real production reload then had the Compliance policies panel visible,
zero role-alert errors, zero browser console warnings/errors, no desktop
document/body horizontal overflow, and no mobile document/body horizontal
overflow at 390x844. Post-fix host evidence stayed healthy: `/healthz=ok`,
`/console/compliance?tab=policies` returned HTTP 200, no Doris FE/BE containers
were running, Redis remained `maxmemory-policy=volatile-lru` with
`maxmemory=134217728`, and memory stayed light at about console 4.5 MiB / 256
MiB, Redis 3.7 MiB / 192 MiB, controlplane 426.5 MiB / 1 GiB, landing 4.8 MiB
/ 128 MiB, and ipq 8.8 MiB / 128 MiB. Recent policy API lines were normal HTTP
200 GET reads in a few milliseconds, and a strict recent log scan showed no
real policy DELETE lines during the synthetic browser test window.
