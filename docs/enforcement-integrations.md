# Enforcement Integration Notes

Control One V1 enforces malicious IP/CIDR decisions through host firewalls and managed webserver blocklists. Future integrations such as Fail2Ban-compatible jails, WAFs, load balancers, CDNs, service meshes, and API gateways must use the same operator intent model so approval, audit, TTL expiry, evidence, and rollback stay consistent.

## Shared Enforcement Model

Every enforcement provider receives a normalized block intent:

- `tenant_id`, `finding_id`, `ip_blocklist_entry_id`, and optional `entity_action_id`.
- `ip_cidr`, `scope`, `target_type`, `target_id`, `server_group`, `app`, and `vhost`.
- `enforcement` provider name or mode, for example `firewall`, `webserver`, `fail2ban`, `waf`, `proxy`, or `combined`.
- `reason`, `score`, `evidence_refs`, approval actor, and approval timestamp.
- `ttl_seconds` or permanent-with-approval marker.
- `protected_override` and `protected_override_reason` when a bank-owned/protected CIDR is intentionally touched.
- `correlation_id` and `idempotency_key` so retries cannot create duplicate blocks.

Every provider reports a normalized receipt:

- `provider`, `target`, `status`, `applied_at`, and optional `removed_at`.
- Validation result, apply/reload result, health check result, and rollback pointer where applicable.
- Provider-native rule ID, checksum/diff when config changed, and any stderr/log tail needed for audit.
- Failure class: validation, apply, reload, health, permission, unsupported capability, or drift.

The control plane owns lifecycle state. Providers do not decide whether an action is approved, expired, rejected, or rolled back; they only apply or remove a specific approved intent and return receipts.

## Fail2Ban Compatibility Path

Fail2Ban support should be a compatibility adapter, not a second policy engine. Control One would generate a managed jail/feed file or command payload containing approved `ip_cidr`, `ttl_seconds`, `reason`, and block identifiers. The adapter must report jail availability, backend type, rule count, applied bans, expired bans, and failed bans.

The Fail2Ban path must not parse arbitrary customer log rules for Control One detection in V1. Detection remains in Control One; Fail2Ban is only an optional local enforcement transport for environments that already trust it operationally.

Minimum adapter actions:

- `Detect`: report installed Fail2Ban version, service manager, configured backends, and writable managed jail location.
- `Plan`: produce a managed jail/feed update without editing user-owned jail files inline.
- `Validate`: verify the jail/feed syntax and service availability.
- `Apply`: update the managed feed and reload/restart only through approved safe commands.
- `Rollback`: restore the previous managed feed and report receipt.

## WAF, Proxy, and Edge Provider Boundaries

WAF/proxy integrations should implement the same provider shape as firewall and webserver enforcement:

- Capability discovery: provider type, account/site/app scope, supported match keys, TTL support, rate-limit support, and dry-run support.
- Plan/apply/rollback receipts: every provider must support a non-destructive plan and a durable receipt.
- Scoped targets: provider-specific targets map back to Control One `tenant`, `server_group`, `app`, `vhost`, or `node` concepts.
- Safety gates: protected CIDR allowlists, max changes per hour, canary rollout, and circuit breaker decisions remain in the control plane.
- Evidence links: provider rules must carry finding/block IDs in metadata/tags where the provider allows it.

V1 should not depend on any single WAF vendor SDK. A future provider can be added behind a generic enforcement-provider interface once there is a concrete customer deployment target.

## V1 Interface Hooks To Preserve

The current V1 design should keep these hooks stable:

- Provider-neutral `enforcement` values and `target_type`/`target_id` scope.
- `app` and `vhost` fields on block proposals.
- TTL-aware blocklist entries with terminal states for `expired`, `rejected`, and `rolled_back`.
- Receipt metadata that can carry provider-native rule IDs, checksums, diffs, validation status, reload/apply status, health status, and rollback references.
- Circuit breaker and rate-limit checks before dispatch.
- Idempotent job payloads keyed by block entry/action IDs.

These hooks are enough to add Fail2Ban, WAF, proxy, or edge integrations later without rewriting the firewall/webserver V1 lifecycle.
