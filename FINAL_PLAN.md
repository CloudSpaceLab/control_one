# Deepened Plan: IP Behavior Intelligence + Safe Webserver Auto-Control

## Summary
Control One should treat IP behavior as a first-class security signal, not just a list of connections. The platform needs to learn normal behavior by country, ASN, IP, server group, service, time of day, day of week, week, and month, then compare live traffic against those baselines using request frequency, bytes transferred, HTTP status patterns, and correlated host/app signals.

For webservers, Control One should safely auto-configure supported servers to capture the right access-log fields and optionally enforce managed blocklists. This must be done through managed snippets, validation, reload, rollback, audit, and approval/circuit breakers.

## Current Gaps Confirmed
- Existing behavioral baselines are narrow: current code has a `port_state` rollup, but not country/IP/time/status/bytes behavioral modeling.
- Nginx and Apache formatters already parse `remote_ip`, `status`, and `bytes`, but those fields are not promoted into a full web request intelligence pipeline.
- Log ingestion still needs to be made reliable before any IP behavior model can be trusted.
- There is host firewall blocking, but no webserver-specific adapter layer for nginx, Apache, lighttpd, Tomcat, IIS, HAProxy, Caddy, Envoy, or Traefik.
- The current architecture does not yet have a dedicated model for “country X is normal on weekdays at 10am but abnormal at 4am on Sunday with 401 spikes and high outbound bytes.”

## IP Behavior Model
Create a dedicated `web.request` derived event from access logs and webserver telemetry. Keep raw `log.line` events as evidence, but use `web.request` for detection.

Each `web.request` event should include:
- Tenant, node, server group, webserver instance, vhost/app, environment, criticality.
- Real client IP, socket remote IP, X-Forwarded-For chain, trusted proxy decision, country, region, ASN, ISP, reputation score.
- Method, path template, raw path hash, status code, status family, response bytes, request bytes when available, duration, upstream status, user agent hash, referrer host.
- Source log file, parser profile, correlation ID, process/service owner, local port, TLS SNI where available.

Baseline dimensions:
- By `tenant + server_group + app/vhost + country`.
- By `tenant + node + app/vhost + country`.
- By `tenant + app/vhost + ASN`.
- By `tenant + app/vhost + source IP`.
- By business criticality group, such as Core Banking, DB, DMZ, ATM, Payment Switches, Domain Controllers.

Time dimensions:
- Current windows: 1m, 5m, 15m, 1h.
- Seasonal baselines: hour of day, day of week, week of month, month.
- Rolling windows: 7d, 30d, 90d, 365d.
- Optional business calendar: weekends, public holidays, maintenance windows, batch windows, month-end processing.

Tracked metrics:
- Connection/request frequency.
- Unique source IP count.
- Countries seen and first-seen country per server group/app.
- Bytes in/out min, avg, p50, p95, p99, and peak.
- Status counts and ratios for `301`, `401`, `403`, `404`, `429`, `500`, `502`, `503`, and status families `2xx`, `3xx`, `4xx`, `5xx`.
- Error-log counts by severity.
- Auth failure to success sequence.
- New successful login after failure burst.
- Sensitive path hits, admin path hits, upload path hits, export/download path hits.
- User-agent novelty and scanner-like patterns.

## Detection Logic
Add an `anomaly.ip_behavior` detector that scores live traffic against baselines.

Default scoring:
- New or rare country for that server group/app: up to 20 points.
- Unusual time/day/month for that country: up to 15 points.
- Request frequency above seasonal p95/p99: up to 15 points.
- `401`/`403` spike above baseline: up to 20 points.
- `500`/`502`/`503` spike after new-country traffic: up to 15 points.
- Bytes out above historical peak or p99: up to 25 points.
- New ASN or risky ISP/VPN/Tor/hosting provider: up to 15 points.
- Threat-intel/reputation match: up to 25 points.
- Correlation bonus for process spawn, file write, DB bulk query, or new outbound connection after web traffic: up to 25 points.

Severity:
- `0-49`: normal or watch.
- `50-69`: suspicious, show in control room.
- `70-84`: high, create finding and propose block.
- `85-100`: critical, propose immediate containment; auto-block only if tenant policy allows it.

Important default:
- Country alone must never be enough for destructive blocking. It is a strong feature, but autonomous action requires corroborating signals such as status spike, bytes anomaly, reputation, sensitive paths, or exploit pattern.

## Example Practical Detections
- **Credential stuffing:** unusual country at 4am, high `401`, many usernames, one later `200`.
- **Admin brute force:** repeated `401/403` against `/admin`, `/login`, `/wp-login`, `/api/auth`, or bank-specific auth routes.
- **Scanner/prober:** high `301/403/404` variety across many paths from one country/ASN.
- **Exploit attempt:** unusual country plus `500/502/503` spike after suspicious paths or payloads.
- **Data exfiltration:** normal request count but bytes out exceeds country/app historical peak.
- **Slow distributed attack:** many unique IPs from same ASN/country with low per-IP volume but abnormal aggregate.
- **Webshell callback:** suspicious request followed by new process, new file under web root, and outbound connection.
- **Partner drift:** trusted partner country/ASN starts hitting outside normal batch windows or transferring abnormal bytes.

## Data Architecture
Use three storage layers:
- **Redis/current counters:** 1m, 5m, 15m, and 1h counters for real-time scoring.
- **Doris analytics:** raw `web_requests`, long-retention rollups, exploratory queries, dashboards.
- **Postgres truth:** compact baselines, findings, block policies, config receipts, approvals, and audit.

Add these core models:
- `webserver_instances`: detected nginx/apache/lighttpd/tomcat/etc instances per node.
- `web_requests`: Doris table for normalized request events.
- `ip_behavior_rollups`: hourly/daily rollups by country, ASN, IP, server group, app, status, and bytes.
- `ip_behavior_baselines`: seasonal baseline JSON with sample count, min/avg/p95/p99/peak, status ratios, and computed window.
- `ip_behavior_findings`: high-level anomaly findings linked to raw logs, requests, connections, and remediation proposals.
- `ip_blocklist_entries`: IP/CIDR, reason, score, TTL, scope, source finding, enforcement target, status.
- `webserver_config_receipts`: every managed config change, checksum, validation result, reload result, rollback pointer.

## Webserver Auto-Configuration
Build a `WebServerAdapter` interface in the agent:

```go
type WebServerAdapter interface {
    Detect(ctx context.Context) ([]WebServerInstance, error)
    Plan(ctx context.Context, instance WebServerInstance, desired WebPolicy) (ConfigPlan, error)
    Validate(ctx context.Context, plan ConfigPlan) error
    Apply(ctx context.Context, plan ConfigPlan) (ConfigReceipt, error)
    Rollback(ctx context.Context, receipt ConfigReceipt) error
}
```

Supported actions:
- `observe`: detect configs and logs only.
- `capture`: add a Control One managed access-log target with required fields.
- `enforce`: install/update a managed webserver blocklist.
- `rollback`: restore previous config receipt.
- `audit`: report unmanaged drift.

Safety rules:
- Never rewrite arbitrary user config inline.
- Prefer managed include files under `/var/lib/control-one/` plus one approved include hook when needed.
- Always snapshot before changes.
- Always run syntax validation before reload.
- Prefer reload over restart.
- Run post-reload health check.
- Roll back automatically if validation, reload, or health check fails.
- Use canary rollout per server group.
- Enforce TTLs and max block changes per hour.
- Respect allowlists for bank-owned CIDRs, VPNs, payment partners, regulators, and monitoring systems.
- Every change must have audit, diff, actor, policy, finding ID, and rollback receipt.

## Webserver Adapter Details
V1 adapters:
- **Nginx/OpenResty**
  - Add managed JSON access log format with real client IP, status, bytes, request time, upstream status, vhost, request, user agent, and XFF chain.
  - Configure `real_ip_header` only for trusted proxy CIDRs.
  - Install managed blocklist include.
  - Validate with `nginx -t`; reload with `nginx -s reload` or systemd reload.
- **Apache HTTPD**
  - Add managed `LogFormat`/`CustomLog` for Control One JSON access logs.
  - Configure `mod_remoteip` only for trusted proxy CIDRs.
  - Install managed blocklist through a supported template for the detected Apache version/modules.
  - Validate with `apachectl configtest`; reload with systemd/apachectl graceful.
- **lighttpd**
  - Detect access log configuration and add/verify required fields where supported.
  - Install managed deny include only when the required modules/context are available.
  - Validate with `lighttpd -tt -f <config>`; reload safely.
- **Tomcat**
  - Add or verify an AccessLogValve pattern for Control One fields.
  - Use RemoteAddrValve block enforcement only when Tomcat is directly exposed.
  - If Tomcat is behind nginx/Apache/HAProxy, enforce at the edge proxy by default.
  - Restart only inside a maintenance window unless explicitly approved.

V2 adapters:
- IIS, HAProxy, Caddy, Envoy, Traefik, Jetty, WildFly/JBoss, WebLogic, WebSphere.

## Enforcement Strategy
Use layered enforcement:
- **Firewall block:** fastest broad protection at host/network layer.
- **Webserver block:** precise app/vhost/path-aware protection and better HTTP evidence.
- **Fail2Ban-style jail:** optional compatibility path.
- **WAF/proxy integration:** future enterprise edge integration.

Default behavior:
- Suspicious IPs become block proposals.
- Critical IPs with corroborating evidence can auto-block only when tenant policy enables it.
- Blocks get TTLs by default: 15m, 1h, 24h, or permanent with approval.
- Every block is scoped: node, server group, app/vhost, or fleet.
- Control One should show whether an IP is blocked at firewall, webserver, both, or pending.

## Control Room UI Additions
Add an **IP Behavior** view inside the control room:
- Live map/table by country with current vs normal traffic.
- Country cards showing request rate, unique IPs, bytes out, `301`, `401`, `403`, `500/502/503`, top ASNs, top apps, affected server groups.
- “Unusual now” panel: country/time/status/bytes anomalies ranked by risk.
- IP profile page: first seen, countries, ASN, reputation, request history, status mix, bytes trend, affected servers, active blocks, findings.
- Baseline explanation: “Nigeria usually sends 200-400 requests/hour to Core Banking API on weekdays; current traffic is 2,900/hour with 41% 401s.”
- One-click actions: block IP, block CIDR, block ASN, limit to vhost, suppress, allowlist partner, collect evidence, ask AI.

## APIs / Interfaces
Add:
- `GET /api/v1/ip-behavior/overview`
- `GET /api/v1/ip-behavior/countries`
- `GET /api/v1/ip-behavior/countries/{code}`
- `GET /api/v1/ip-behavior/ips/{ip}`
- `GET /api/v1/ip-behavior/baselines`
- `GET /api/v1/webservers`
- `POST /api/v1/webservers/{id}/config/plan`
- `POST /api/v1/webservers/{id}/config/apply`
- `POST /api/v1/webservers/{id}/config/rollback`
- `POST /api/v1/network/block-proposals`
- `POST /api/v1/network/block-proposals/{id}/approve`

Add agent job types:
- `webserver.inventory_scan`
- `webserver.config_plan`
- `webserver.config_apply`
- `webserver.blocklist_update`
- `webserver.config_rollback`

Add event types:
- `web.request`
- `web.error`
- `anomaly.ip_behavior`
- `remediation.webserver_block.applied`
- `remediation.webserver_block.failed`
- `webserver.config.changed`

## Implementation Plan
1. **Fix log ingestion first**
   - Make agent log batches reach the control plane reliably.
   - Emit normalized `log.line` and derived `web.request` events.
   - Ensure nginx/apache parsed fields become first-class fields, not buried only in untyped details.

2. **Add IP enrichment and request normalization**
   - Enrich real client IP with country, ASN, ISP, reputation, and trusted proxy status.
   - Prevent X-Forwarded-For spoofing by trusting only configured proxy CIDRs.
   - Add Doris `web_requests` and Postgres baseline/finding tables.

3. **Build real-time counters and seasonal baselines**
   - Use Redis for short-window counters.
   - Use Doris rollups for hourly/daily/monthly analytics.
   - Store compact seasonal baselines in Postgres.
   - Require minimum sample counts before autonomous action.

4. **Implement `anomaly.ip_behavior`**
   - Score country/time/status/bytes/IP/ASN/reputation deviations.
   - Emit one deduplicated finding per root pattern.
   - Link evidence to raw logs, web requests, connections, process events, and patch/exposure state.

5. **Build webserver inventory and adapters**
   - Detect nginx, Apache, lighttpd, and Tomcat instances.
   - Report config paths, versions, modules, vhosts, log paths, service manager, and reload strategy.
   - Implement dry-run config plans before any apply action.

6. **Add managed capture mode**
   - Install Control One access-log targets with required fields.
   - Leave existing logs untouched.
   - Validate, reload, health-check, and rollback on failure.

7. **Add managed enforcement mode**
   - Install/update TTL-based blocklist includes.
   - Support firewall-only, webserver-only, or combined enforcement.
   - Add approval, circuit breaker, allowlist, drift detection, and rollback.

8. **Add UI control room views**
   - Add IP Behavior lane and country/IP drilldowns.
   - Show current vs normal by country, server group, app, status codes, and bytes.
   - Add block proposal workflow and enforcement status.

9. **Harden for banks**
   - Add signed offline content bundles for geo DBs, threat feeds, rules, parser profiles, and webserver adapter templates.
   - Add immutable audit of every config change and block action.
   - Add canary rollout and maintenance-window policies for config changes.
   - Add tenant isolation and RBAC tests for every new endpoint and AI tool.

## Acceptance Tests
- Nginx access logs from a new country at 4am with high `401` count produce an `anomaly.ip_behavior` finding.
- Same country during normal business hours with normal request volume does not alert.
- A country with normal request count but bytes out above historical peak produces an exfiltration-risk finding.
- A `500/502/503` spike after suspicious paths from a rare ASN produces an exploit-attempt finding.
- Control One installs an nginx capture config, validates with `nginx -t`, reloads, and rolls back on failed health check.
- Control One installs an Apache capture config, validates with `apachectl configtest`, reloads gracefully, and records a config receipt.
- A malicious IP block proposal updates firewall and nginx managed blocklist with TTL, then expires cleanly.
- Trusted proxy handling rejects spoofed X-Forwarded-For when the proxy CIDR is not trusted.
- Tomcat direct exposure requires approval and maintenance window for config restart.
- Airgapped deployment imports geo/threat/parser/profile bundles and continues scoring without internet access.

## Assumptions
- The correct spelling is `lighttpd`; support should include it even if users call it lighthttpd.
- Webserver auto-modification means managed capture/enforcement snippets, not uncontrolled edits to customer config.
- Firewall blocking remains the safest broad default; webserver blocking adds app-aware precision and better evidence.
- Country is a behavioral feature, not a sole basis for blocking unless a tenant explicitly creates a policy for that.
- All destructive or outage-risking actions default to approval-required in banking environments.
