# Webserver Adapter Roadmap

This roadmap separates observe, capture, and enforce support for post-V1 adapters. The V1 agent already supports nginx/OpenResty, Apache HTTPD, lighttpd, Tomcat, and an HAProxy edge-proxy adapter. Future adapters must keep the same safety model: detect first, plan dry-run changes, validate syntax, prefer reload over restart, health-check, rollback, and write immutable receipts.

## V2 Support Matrix

| Server | Default role | Observe metadata | Capture feasibility | Validation | Reload/restart | Enforcement caveat |
| --- | --- | --- | --- | --- | --- | --- |
| IIS | Edge proxy / app host | Sites, bindings, app pools, log directories, Windows service state, modules | Use W3C fields and Advanced Logging where present | `appcmd list config` plus PowerShell config parse | App-pool recycle or IIS service action requires approval | Use IIS IP Restrictions only when module is installed; Windows firewall is safer default |
| Caddy | Edge proxy | Caddyfile/JSON config path, apps, routes, log sinks, admin API availability | Managed JSON log block per server/route | `caddy validate --config <path>` | `caddy reload --config <path>` | Prefer route-level deny only when config context is known |
| Envoy | Edge proxy / service mesh | Static/bootstrap config, listeners, routes, clusters, admin endpoint | Access log filter with JSON format and response headers | `envoy --mode validate -c <path>` | Hot restart or xDS push; static restart requires approval | Enforce through RBAC/local rate-limit only with explicit policy |
| Traefik | Edge proxy | Static/dynamic providers, routers, services, middlewares, access-log settings | Access log JSON fields through static config | `traefik check-config` where available plus provider parse | Service reload/restart depends on deployment platform | Enforce with IPAllowList/Deny middleware only in managed dynamic provider |
| Jetty | App container | Base/home, modules, contexts, request logs, connectors | CustomRequestLog pattern or JSON writer | `java -jar start.jar --list-config` plus XML parse | Restart usually required; maintenance window default | Enforce at upstream proxy unless directly exposed |
| WildFly/JBoss | App container | Server config, deployments, access log valve/filter, interfaces, socket bindings | Undertow access-log pattern and server log categories | CLI `:read-resource` / config parse | Reload/restart requires approval | Enforce at edge; container-level deny only when direct exposure is proven |
| WebLogic | Enterprise app container | Domains, servers, deployments, access logs, listen ports, node manager | HTTP access log extended fields where enabled | WLST/domain config validation | Restart or rolling restart, maintenance window required | Edge enforcement default; app-server changes are high risk |
| WebSphere | Enterprise app container | Profiles, servers, virtual hosts, access logs, transports | NCSA/custom access log settings where available | wsadmin config validation | Restart/rolling restart, maintenance window required | Edge enforcement default; direct deny is exceptional |

## HAProxy Status

HAProxy is promoted into the near-term edge-proxy path because connection lifecycle monitoring needs request and response header capture before traffic reaches app and DB tiers. V1 support is intentionally conservative:

- Observe: detect config, service, logs, and backends.
- Capture: produce a managed snippet for HTTP frontend/listen contexts with request and response header capture plus JSON log-format fields.
- Enforce: produce a managed ACL/deny include, scoped by approved include hooks.
- Safety: missing include hooks require manual approval; response-header capture should be canaried per server group.

## Minimum Observe-Only Metadata

Every V2 adapter must report:

- Server kind, version, service manager, process owner, config paths, access/error log paths, and reload strategy.
- Edge/app-container classification and whether enforcement should happen locally or at an upstream proxy.
- Vhosts/routes/sites/apps, ports, TLS/SNI bindings when discoverable, and backend/upstream targets for proxies.
- Managed capture capability, response-header capture capability, validation command, restart risk, and maintenance-window requirement.
- Application roots or deployment directories where applicable, with detected framework/app type and suggested parser skill when Control One lacks a matching profile.

## Milestone Split

V2 implementation should be split after V1 stabilization:

- M7 edge adapters: HAProxy hardening, Caddy, Envoy, Traefik.
- M8 Windows edge/app support: IIS inventory, capture, and Windows-safe receipts.
- M9 enterprise app containers: Jetty, WildFly/JBoss, WebLogic, WebSphere with approval-first restart workflows.

No V2 adapter may bypass the shared approval, circuit-breaker, allowlist, TTL, audit, diff, receipt, and rollback model.
