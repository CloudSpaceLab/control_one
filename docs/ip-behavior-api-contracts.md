# IP Behavior, Webserver, and Block Proposal API Contracts

These endpoints support the Control Room IP Behavior workflow. All human calls use OIDC/RBAC; agent calls use mTLS. Every request is tenant scoped by `tenant_id`, and write/remediation paths require `operator` or `admin`.

## IP Behavior Views

`GET /api/v1/ip-behavior/overview?tenant_id=&since=`

- Role: `viewer`, `operator`, or `admin`.
- Response: tenant ID, window start, total request count, bytes out, status totals, generated timestamp, and top country summaries.
- Use: Control Room high-level "unusual now" and current traffic cards.

`GET /api/v1/ip-behavior/countries?tenant_id=&since=`

- Role: `viewer`, `operator`, or `admin`.
- Response: `countries[]` with country code/name, unique source IP count, request count, bytes out, status counts, first seen, and last seen.
- Use: country table/map and status spike triage.

`GET /api/v1/ip-behavior/countries/{code}?tenant_id=&since=`

- Role: `viewer`, `operator`, or `admin`.
- Response: one country summary or `404` when no traffic exists in the window.

`GET /api/v1/ip-behavior/ips/{ip}?tenant_id=&since=`

- Role: `viewer`, `operator`, or `admin`.
- Response: source IP, countries, ASNs, request count, bytes out, status counts, first seen, and last seen.
- Invalid IP returns `400`.

`GET /api/v1/ip-behavior/baselines?tenant_id=&dimension=&limit=&offset=`

- Role: `viewer`, `operator`, or `admin`.
- Response: paginated seasonal baseline rows, including baseline JSON and sample counts.
- Use: current-vs-normal explanation and baseline sufficiency checks.

## Webserver Workflow

`GET /api/v1/webservers?tenant_id=&node_id=&limit=&offset=`

- Role: `viewer`, `operator`, or `admin`.
- Response: detected webserver instances with kind, version, service name, config/log paths, vhosts, capabilities, and observed time.

`POST /api/v1/webservers/inventory`

- Role: `operator` or `admin`.
- Body: `tenant_id`, `node_id`, optional policy.
- Response: queued job/action IDs for `webserver.inventory_scan`.

`POST /api/v1/webservers/{id}/config/plan`

- Role: `operator` or `admin`.
- Body: `tenant_id`, `node_id`, policy.
- Response: queued job/action IDs for `webserver.config_plan`.

`POST /api/v1/webservers/{id}/config/apply`

- Role: `operator` or `admin`.
- Body: `tenant_id`, `node_id`, policy.
- Safety: restart-sensitive servers such as Tomcat require explicit approval and an active maintenance window.
- Response: queued job/action IDs for `webserver.config_apply`.

`POST /api/v1/webservers/{id}/config/rollback`

- Role: `operator` or `admin`.
- Body: `tenant_id`, `node_id`, rollback receipt/policy.
- Safety: receipt is required for rollback-capable actions.
- Response: queued job/action IDs for `webserver.config_rollback`.

## Block Proposal Workflow

`GET /api/v1/network/block-proposals?tenant_id=&status=&ip_cidr=&finding_id=&server_group=&target_type=&target_id=&app=&vhost=&limit=&offset=`

- Role: `viewer`, `operator`, or `admin`.
- Response: paginated block proposals with lifecycle status, TTL expiry, score, scope, target, app/vhost, approval metadata, last error, protected override evidence, and linked finding/action IDs.

`POST /api/v1/network/block-proposals`

- Role: `operator` or `admin`.
- Body: `tenant_id`, `ip_cidr`, `reason`, optional `finding_id`, `score`, `ttl_seconds`, `scope`, `target_type`, `target_id`, `server_group`, `app`, `vhost`, `enforcement`.
- TTL values: `900`, `3600`, `86400`, or `0` for permanent with approval.
- Safety: protected tenant CIDRs require admin override and `protected_override_reason`.
- Response: typed block proposal in `proposed` status.

`POST /api/v1/network/block-proposals/{id}/approve`

- Role: `operator` or `admin`; protected overrides require `admin`.
- Behavior: records an entity action, dispatches firewall/webserver/combined enforcement, and transitions to `dispatching` or `canary`.

`POST /api/v1/network/block-proposals/{id}/promote`

- Role: `operator` or `admin`; protected overrides require `admin`.
- Behavior: promotes a successful canary to remaining eligible nodes.

`POST /api/v1/network/block-proposals/{id}/reject`

- Role: `operator` or `admin`.
- Body: optional `reason`.
- Behavior: only valid for `proposed` entries; writes `rejected` audit state.

`POST /api/v1/network/block-proposals/{id}/rollback`

- Role: `operator` or `admin`.
- Body: optional `reason`.
- Behavior: marks `rolled_back`, queues firewall removal, refreshes webserver managed blocklists, and writes audit evidence.

## Failure Semantics

- `400`: malformed UUID/IP/CIDR, unsupported TTL, invalid payload, or invalid route parameter.
- `403`: caller lacks the required role or protected override approval requires admin.
- `404`: requested resource does not exist.
- `409`: lifecycle conflict, protected target, open circuit breaker, or maintenance-window rejection.
- `429`: block proposal rate/circuit safety limit exceeded.
- `503`: required store/backend unavailable.

The API response is deliberately typed for UI use. Human-facing explanation should be derived from response fields, baseline JSON, finding evidence, and block proposal state rather than hardcoded page text.
