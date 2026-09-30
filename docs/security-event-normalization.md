# Security event normalization

Control One converts collector-specific telemetry into a stable `security.event` signal before correlation rules evaluate it. The original event remains in the pipeline for analytics and investigation, while the normalized signal supplies consistent fields to detection templates.

## Supported sources

| Source | Accepted input | Normalized signal |
| --- | --- | --- |
| Linux `sshd` / systemd journal | OpenSSH accepted and failed authentication messages | `ssh.authentication_failure`, `authentication.failure`, `authentication.success` |
| Windows Security Event Log | Event IDs 4625 and 4624 | `windows.authentication_failure`, `authentication.failure`, `authentication.success` |
| Windows Hello / Biometrics | Hello Event ID 7001 and Biometrics Event ID 1005 | `windows.authentication_failure`, `authentication.failure` |
| Nginx and Apache | Structured request fields or Common/Combined access-log lines | `web.request` |
| IIS | Structured W3C fields or the default W3C access-log field order | `web.request` |
| Reverse proxies and WAFs | Structured HTTP fields or Common/Combined access-log lines | `web.request` |
| Network connection collectors | `conn.*` events | `network.connection` |
| Database audit logs | PostgreSQL, MySQL/MariaDB, SQL Server and Oracle authentication failures | `database.authentication_failure`, `authentication.failure` |
| Finacle monitoring | Authentication failures and operational failure/unavailable/timeout messages | `database.authentication_failure` or `finacle.operation_failure` |

## Correlation fields

Rules use the flat fields below so administrators can build expressions without knowing a vendor log format:

```json
{
  "event_type": "ssh.authentication_failure",
  "event_category": "authentication",
  "event_action": "login",
  "outcome": "failure",
  "auth_result": "failure",
  "src_ip": "203.0.113.25",
  "src_port": 54321,
  "dst_ip": "10.0.0.20",
  "dst_port": 22,
  "protocol": "tcp",
  "user_name": "root",
  "node_id": "af52be71-4d94-466b-86ed-ab2559e15c35",
  "timestamp": "2026-09-15T10:00:00Z",
  "source": "linux.sshd",
  "source_os": "linux",
  "source_channel": "/var/log/auth.log"
}
```

Translators accept common source aliases including `source_ip`, `destination_ip`, `source_port`, `destination_port`, `username`, and `outcome`. Normalized output always uses `src_ip`, `dst_ip`, `src_port`, `dst_port`, `user_name`, and `auth_result`.

`source_os`, `source_channel`, and `source_event_id` are evidence fields. They identify the platform, native log stream, and native record ID without making correlation rules depend on vendor IDs. The Control One `event_id` remains the unique event reference. For example, a Windows Security 4625 record has `source_os: windows`, `source_channel: Security`, and `source_event_id: 4625`, while a Linux SSH record may have `source_channel: /var/log/auth.log` and no native numeric ID.

Each event also includes the nested `normalized` object defined by `controlone.security_event` schema version 1. This carries portable ECS-aligned fields such as `event.category`, `event.action`, `event.outcome`, `source.ip`, `destination.port`, `user.name`, `host.hostname`, and `network.protocol`. Platform-specific aliases such as `windows.authentication_failure` and `ssh.authentication_failure` are emitted alongside the portable `authentication.failure` event so existing rules continue to work during migration.

## Validation and parser errors

### Alert scope and resource evidence

Correlation alerts retain event-specific tenant and node scope, message, severity,
source event reference, and recorded resources in their contributing events.
Resources can include destination endpoints, paths, services, processes,
sensor names, and accounts. A Windows biometric mismatch (Event ID 1005) does
not contain a username, so its resource is shown as `Sensor: <name>` and the
account remains an explicit `unknown` bucket for same-node correlation. The
alert review shows these independently from the rule's grouping
scope: a tenant-wide rule does not establish that every resource was affected.
These are observed resources, not proof of compromise. Missing fields remain
unavailable; historical evidence without these fields is not reconstructed from
the alert's merged context. Newly captured details require the updated backend.

Every normalized event requires `event_type`, `source`, `node_id`, `timestamp`, and `outcome`. Detection-specific requirements are also enforced:

- SSH authentication failures require source IP, destination port, protocol, user, and authentication result.
- Windows and database authentication failures require user and authentication result.
- Web requests require client IP, destination port, protocol, method, path, and status.
- Network connections require source IP, destination IP, destination port, and protocol.

An unrelated log line is ignored. A record recognized as a supported source but malformed produces a `security.normalization_error` event with `parser_status: error`, and the control-plane logs a structured warning containing the tenant, node, source event ID, source family, and reason. This makes ingestion problems observable without rejecting the rest of a collector batch.

Representative parser fixtures live in `controlplane/internal/server/testdata/security_normalization`. They cover Linux SSH, Windows Security events, Nginx, Apache, IIS, HAProxy, WAF aliases, network aliases, PostgreSQL, MySQL, SQL Server, Oracle, Finacle, and malformed input.

## Representative local checks

Run the normalization and correlation tests from the repository root:

```powershell
$env:GOCACHE = "$PWD\.tmp-go-cache"
go test ./controlplane/internal/server -run TestNormalizeSecurityEvents -count=1
go test ./controlplane/internal/correlation -count=1
```

To reproduce an SSH signal through a configured local agent, write four standard OpenSSH failure lines within 20 seconds to a journal or log path collected by that agent:

```text
Failed password for invalid user root from 203.0.113.25 port 54321 ssh2
```

Enable the **SSH brute-force detection** template first. All four events must use the same source IP and node. The expected result is one alert grouped by `src_ip + node_id`; further matching activity is subject to the rule's suppression period.

Use representative data only. The normalizer ignores unrelated log lines and preserves the original source message as evidence on the normalized event.
