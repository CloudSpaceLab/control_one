# Network configuration snapshots and posture (#263)

The Network device detail view can show a bounded history of sanitized
configuration snapshots collected by an assigned site collector. The workflow
is read-only. It does not expose a command runner, write adapter, or automatic
remediation action.

## Operator workflow

1. A platform administrator configures an assigned site collector with a local
   target entry and the credentials for the management adapter. See the example
   [network collector configuration](../controlplane/config/networkcollector.example.yaml).
2. Save the device using the Add device wizard. Its SNMPv3/SSH choices verify
   identity only; they do not configure snapshot collection.
3. On the saved device, open **Network telemetry sources** → **Configure source
   binding**, select **SSH config snapshot**, **NETCONF**, **RESTCONF**, or
   **Vendor API**, and enter the assigned collector ID and site. See the
   [source-binding workflow](network-device-telemetry.md).
4. The collector runs only its fixed read operations: SSH snapshots for Cisco,
   Juniper, or Fortinet; NETCONF `get-config`; RESTCONF GET; or a configured
   vendor API GET. Credentials and CA trust stay on the collector host.
5. Open **Network devices**, select the target, and review **Configuration
   snapshots and posture**. If no report has arrived, the panel says so and
   does not imply that the device has no risks.
6. Expand a revision to inspect the collector adapter/version, observed time,
   SHA-256 content hash, sanitized configuration, posture evidence, and the
   deterministic line diff from the preceding revision.
7. Select a finding's line reference to jump to the exact line in that
   revision. A finding links to that immutable snapshot ID, so later reports do
   not change the evidence behind the finding.

Secrets are redacted by the site collector and normalized again by the control
plane before snapshot persistence or event journaling. Identical normalized
content is deduplicated; a changed hash creates the next revision. Only text,
JSON, and XML snapshots under 16 KiB are accepted. Unsupported or malformed
formats are rejected instead of stored as evidence.

Accepted reports also enter the existing durable event journal with the bound
target, source binding, snapshot ID, revision, and normalized hash. Collector
replays use the journal's existing idempotency handling.

The current text posture rules report explicit evidence for Telnet management,
cleartext HTTP management, and SNMPv1/v2c community configuration. They do not
claim a clean result when a rule finds no matching line (`Not observed`). Rules
that need public-route context, complete management ACL context, firmware
advisories, vendor-aware cipher parsing, intended-service policy, HA peer state,
interface counter history, or routing-neighbor history remain `Unsupported`.
JSON/XML snapshots also show the configuration-text rules as `Unsupported`
until format-aware checks exist. Drift is reported by comparing the current
normalized snapshot to its previous source revision, with links to both pieces
of evidence.

## Safety boundary

The collector allow-lists read operations and rejects arbitrary commands,
configuration writes, and API redirects. The control-plane endpoint is
`GET /api/v1/network-configuration/{target_id}` and requires target read access.
Other HTTP methods are rejected. The detail panel has no remediation controls.

Any future write workflow needs a separate implementation and acceptance pass
for typed actions, explicit permission and approval, preflight validation,
backup and before/after diff, bounded execution, rollback, and post-change
verification, and a durable receipt. Nothing in this snapshot feature
authorizes device changes.

## Verification coverage

Unit and handler tests cover secret redaction and hashing, supported formats,
deterministic diffs, evidence-linked findings, unsupported posture, read-only
authorization, and collector-report sanitization before journaling. PostgreSQL
integration coverage exercises revisions, deduplication, tenant binding, and
read access. These code tests use synthetic configuration payloads; they do not
prove interoperability with a physical switch or a particular vendor firmware.
