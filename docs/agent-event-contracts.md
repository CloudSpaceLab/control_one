# Agent Job and Event Contracts

This note pins the V1 contracts connecting agents, control plane, IP behavior detection, webserver control, and remediation status.

## Heartbeat Capability Negotiation

Agents send `capabilities[]` on heartbeat. Current nodeagent advertises:

- `firewall_control.v1`
- `patch_management.v1`
- `event_filters.v1`
- `webserver_control.v1`
- `server_purpose_inventory.v1`
- `connection_lifecycle_headers.v1`

The control plane persists these under node label `agent.capabilities`. Webserver jobs are dispatched only when the node advertises `webserver_control.v1`; otherwise the job and webserver config action fail with an explicit compatibility error. This prevents older agents from silently ignoring unknown pending action prefixes.

Heartbeat keeps forward compatibility by accepting unknown request fields. Older servers ignore newer agent fields; newer servers tolerate older agents that do not send optional capabilities.

Agents that advertise `server_purpose_inventory.v1` also send `server_purposes[]` derived from installed package evidence, for example `db_node`, `app_node`, `load_balancer`, `cache_server`, `monitoring_server`, and `message_queue`. The control plane stores these under `agent.server_purposes`, `agent.primary_purpose`, and `agent.server_purpose_evidence` node labels so dashboards and policies can distinguish database, application, load-balancer, cache, and monitoring hosts without manual tagging.

Agents that advertise `connection_lifecycle_headers.v1` can preserve request/response lifecycle headers from managed webserver or load-balancer logs. `web.request.details` may include `request_id`, `correlation_id`, `response_request_id`, `response_correlation_id`, `traceparent`, `frontend`, `backend`, `upstream_server`, `termination_state`, `captured_request_headers`, and `captured_response_headers`.

## Pending Action Format

Pending actions are heartbeat strings:

```text
<job_type>:<job_id>
```

The agent fetches `/api/v1/jobs/{job_id}`, decodes the payload, executes locally, and reports completion through `completed_actions[]` on the next heartbeat.

## Webserver Job Types

Supported V1 job types:

- `webserver.inventory_scan`
- `webserver.config_plan`
- `webserver.config_apply`
- `webserver.blocklist_update`
- `webserver.config_rollback`

Payload shape:

```json
{
  "contract_version": "webserver.jobs.v1",
  "idempotency_key": "job:<job_id>",
  "correlation_id": "webserver:<finding-or-block-or-job>",
  "webserver_instance_id": "<uuid>",
  "tenant_id": "<uuid>",
  "node_id": "<uuid>",
  "action": "webserver.config_apply",
  "policy": {},
  "instance": {}
}
```

Required fields: `tenant_id`, `node_id`, and `action`. `contract_version` must be empty for legacy payloads or exactly `webserver.jobs.v1`. The control plane stamps new jobs with `contract_version`, `idempotency_key`, and `correlation_id`.

## Completion Contract

Agent completion shape:

```json
{
  "action": "webserver.blocklist_update",
  "job_id": "<uuid>",
  "status": "succeeded",
  "error": "",
  "metadata": {
    "plan": {},
    "receipt": {
      "action": "webserver.blocklist_update",
      "checksum_before": "",
      "checksum_after": "",
      "validation_status": "passed",
      "reload_status": "reloaded",
      "rollback_ref": "",
      "diff": "",
      "metadata": {}
    }
  }
}
```

`config_apply`, `blocklist_update`, and `config_rollback` require a receipt on success. Missing receipts, failed health checks, or elevated post-apply error rates mark the action/job failed and can trip the webserver enforcement circuit breaker.

Blocklist updates link back to the source block proposal through `policy.source_block_entry` and `policy.metadata.ip_blocklist_entry_id`. The control plane rolls the block proposal to `active` only after required firewall/webserver layers succeed; failures keep it out of active state.

## Event Types

Accepted IP behavior and webserver event types:

- `log.line`
- `web.request`
- `web.error`
- `anomaly.ip_behavior`
- `remediation.webserver_block.applied`
- `remediation.webserver_block.failed`
- `webserver.config.changed`

`web.request` requires a valid `src_ip`; counters cannot be negative; `details.status_code`, when present, must be a valid HTTP status. `anomaly.ip_behavior` requires `src_ip`, `severity`, and `dedup_key`. Remediation/webserver config events require `correlation_id` or `dedup_key`.

Log-derived `log.line` and `web.request` events share a correlation ID:

```text
log:<node_id>:<timestamp_nanos>:<message_hash>
```

`anomaly.ip_behavior` uses its dedup key as correlation ID, letting findings, evidence, and downstream remediation proposals stay linked without depending on raw log storage layout.

## Versioning Rules

- New job payload fields must be optional for agents.
- New required agent behavior must be guarded by a capability string.
- Contract-breaking webserver job changes require a new `contract_version`.
- Pending action prefixes must remain stable because older agents switch on the prefix.
- Control plane must fail unsupported capability-dependent jobs clearly instead of leaving them queued/running indefinitely.
