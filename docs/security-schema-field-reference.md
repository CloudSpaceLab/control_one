# Security Schema Field Reference

Status: first operator and content-author reference for
`controlone.security_event` schema version 1.

Control One parser packs, detections, event ingest, and exports should use this
shared vocabulary for normalized security fields. Vendor-specific fields remain
allowed, but production parser packs should promote common actor, host,
network, process, authentication, and rule semantics into these fields.

## Enforcement Points

- Content-pack sample replay validates known normalized field types by default.
- `/api/v1/events/ingest` validates known normalized fields from top-level event
  attributes and parser-provided `details.fields` / `details.normalized` maps.
- ECS-compatible export aliases are one-to-one for the first schema slice.
- UDM-compatible export aliases cover first-pass Google SecOps fields for
  metadata, principal, target, process, and security-result records. The export
  helper also derives `metadata.event_type` for common authentication, network,
  DNS, process launch, and process termination events.
- OCSF-compatible export aliases project first-pass object fields and derive
  `category_name` / `class_name` for authentication, DNS, network/firewall,
  process/EDR, file, email, detection finding, and fallback application events.

## Field Dictionary

| Field | Type | ECS alias | OCSF hint | UDM alias | Purpose |
| --- | --- | --- | --- | --- | --- |
| `destination.ip` | `ip` | `destination.ip` | `dst_endpoint.ip` | `target.ip` | Destination endpoint IP address. |
| `destination.port` | `int` | `destination.port` | `dst_endpoint.port` | `target.port` | Destination transport port. |
| `destination.user.name` | `string` | `destination.user.name` | `dst_endpoint.user.name` | `target.user.userid` | Destination or impersonated user where present. |
| `event.action` | `string` | `event.action` | `activity_name` | - | Normalized action inside the event category. |
| `event.category` | `string` | `event.category` | `category_name` | - | High-level event family such as authentication, process, network, or file. |
| `event.code` | `string` | `event.code` | `type_uid` | `metadata.product_event_type` | Vendor or platform event identifier. |
| `event.dataset` | `string` | `event.dataset` | `metadata.log_name` | `metadata.log_type` | Source dataset, channel, or log stream. |
| `event.kind` | `string` | `event.kind` | `metadata` | - | Event record kind, usually `event` or `alert`. |
| `event.outcome` | `string` | `event.outcome` | `status` | - | `success`, `failure`, `unknown`, or equivalent normalized outcome. |
| `event.provider` | `string` | `event.provider` | `metadata.product.name` | `metadata.product_name` | Vendor, product, or provider that emitted the event. |
| `host.hostname` | `string` | `host.hostname` | `device.hostname` | `principal.hostname` | Host that emitted or owns the event. |
| `network.protocol` | `string` | `network.protocol` | `connection_info.protocol_name` | - | L4/L7 protocol name. |
| `process.command_line` | `string` | `process.command_line` | `process.cmd_line` | `target.process.command_line` | Process command line after policy-approved parsing. |
| `process.executable` | `string` | `process.executable` | `process.file.path` | `target.process.file.full_path` | Process image path. |
| `process.parent.command_line` | `string` | `process.parent.command_line` | `actor.process.cmd_line` | `principal.process.command_line` | Parent process command line. |
| `process.parent.executable` | `string` | `process.parent.executable` | `actor.process.file.path` | `principal.process.file.full_path` | Parent process image path. |
| `process.pid` | `string` | `process.pid` | `process.pid` | `target.process.pid` | Process identifier. String preserves Windows hex IDs. |
| `rule.id` | `string` | `rule.id` | `rule.uid` | `security_result.rule_id` | Detection or source rule identifier. |
| `rule.name` | `string` | `rule.name` | `rule.name` | `security_result.rule_name` | Detection or source rule name. |
| `source.hostname` | `string` | `source.hostname` | `src_endpoint.hostname` | `principal.hostname` | Source endpoint hostname. |
| `source.ip` | `ip` | `source.ip` | `src_endpoint.ip` | `principal.ip` | Source endpoint IP address. |
| `source.port` | `int` | `source.port` | `src_endpoint.port` | `principal.port` | Source transport port. |
| `source.user.name` | `string` | `source.user.name` | `actor.user.name` | `principal.user.userid` | Initiating user when distinct from target user. |
| `user.domain` | `string` | `user.domain` | `user.domain` | - | Account domain, realm, or tenant. |
| `user.name` | `string` | `user.name` | `user.name` | `target.user.userid` | Primary actor or target account. |

## Authoring Rules

- Use integers for ports. Do not emit `"443"` for `source.port` or
  `destination.port`.
- Use valid textual IP addresses for `source.ip` and `destination.ip`.
- Preserve vendor fields under a product namespace such as
  `fortinet.*`, `cef.extensions.*`, `leef.extensions.*`, or
  `windows.event_data.*`.
- Keep raw refs or raw-retention labels alongside normalized fields when
  collect mode permits them.
- Prefer stable action names such as `logon_success`, `logon_failure`,
  `process_start`, `process_end`, and `network_connection` for cross-pack
  detections.

## Example

```json
{
  "event": {
    "kind": "event",
    "category": "authentication",
    "action": "logon_success",
    "outcome": "success",
    "code": "4624",
    "provider": "Microsoft-Windows-Security-Auditing",
    "dataset": "Security"
  },
  "host": {
    "hostname": "dc1.bank.local"
  },
  "user": {
    "name": "alice",
    "domain": "BANK"
  },
  "source": {
    "ip": "10.10.1.25",
    "port": 51514,
    "user": {
      "name": "svc-collector"
    }
  }
}
```

## Current Gaps

- OCSF export is a first-pass class/category/object projection; complete
  product-specific OCSF validation profiles and numeric IDs are still pending.
- UDM export aliases are first-pass field projections; complete
  product-specific UDM validation profiles are still pending.
- Doris typed columns and indexes for every high-value normalized field are not
  complete.
- Schema migration tests beyond version 1 are still pending.
