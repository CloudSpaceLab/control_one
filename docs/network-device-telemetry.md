# Network telemetry sources (#261)

Network-device source readiness is separate from inventory, reachability, and
collector freshness. A successful manual inventory refresh does not start SNMP
polling. Missing Flow or Syslog does not mark a reachable device down.

## Browser-only operator workflow

Prerequisite: a platform administrator has registered and deployed a tenant/site
collector using the existing content-pack collector workflow. Registration alone
does not prove that a receiver or poller is deployed.

1. Sign in as an operator and select the intended tenant.
2. Open **Network devices**, then select the onboarded network device.
3. Scroll to **Network telemetry sources**. Review each source separately;
   unconfigured and unsupported integrations remain explicit.
4. Click **Configure source binding** and choose the source you are assigning:
   **Syslog**, **SNMP polling**, **SNMP traps**, **NetFlow**, **IPFIX**,
   **sFlow**, or a configuration source (**SSH config snapshot**, **NETCONF**,
   **RESTCONF**, or **Vendor API**). Enter the existing tenant collector ID
   and its assigned collection site. Configuration snapshot sources require
   matching local adapter settings on the site collector; see
   [network-device-configuration.md](network-device-configuration.md).
5. For receiver sources (Syslog, traps, and Flow), enter the device's transport sender IP as seen by that collector.
   With NAT, use the observed sender address. A shared NAT address cannot be
   assigned to several devices on one collector; use separate collectors or
   distinct transport identities. Message hostnames never determine ownership.
6. Set a freshness window from 60 to 86,400 seconds and save. Earlier collection
   evidence is cleared when a binding is replaced. Saving does not claim Ready.
7. Once the assigned collector forwards observations, click **Refresh source
   states**. Read source state/contact time separately from collector state and
   heartbeat. Queue depth and lag are reported collection evidence.
8. Open **Observability** and expand the network device under **Network device
   source readiness**. These are protocol sources, not processes or services.
   Device pagination and All tenants reads use the existing authorized target API.

Product operators do not need shell commands to save a source binding. Receiver,
poller, or management-adapter deployment and credentials remain
platform/collector administration work. The Add device wizard's protocol menu
is a separate one-time identity test and currently supports SNMPv3 and SSH;
NETCONF, RESTCONF, and vendor API are configured as snapshot sources after the
device is saved.

## Recurring SNMP site collector

The repository now ships `controlplane/cmd/networkcollector`. A platform
administrator deploys it on a host that can reach the site's devices, using
`controlplane/config/networkcollector.example.yaml` as the configuration template.
Register a dedicated collector with kind `node_agent` through the existing token
workflow and run the process under the site's service supervisor, supplying the
configuration path with `--config`. The collector host is separate from the
agentless device identity.

Configuration contains tenant/collector IDs, a polling interval, allowed CIDRs
and local target IDs/addresses. Secrets use named environment variables; the
control plane never exports saved device credentials. Production workers use
the scoped collector token and HTTPS; `ca_file` supplies a trusted custom CA.
Only active bindings assigned to this tenant/collector are polled. Saving an
SNMP binding in the operator UI enables polling for a locally configured target,
without per-device shell commands. Set freshness above the polling interval.

The worker uses bounded SNMPv3 authPriv reads of `sysUpTime.0` and `ifNumber.0`.
SHA/SHA256 and AES reuse the existing protocol implementation. All DNS answers
must pass the configured destination policy; connections pin the resolved IP.
Missing local configuration reports Policy blocked. Missing optional counters
give Partial. Successful observations enter the existing durable server journal
as `network.snmp`, with canonical target/source IDs and no node identity.

Heartbeats run independently of device polling. Failed sends retain at most the
latest report per binding in a bounded memory queue; revoked/replaced bindings
are dropped. Queue depth and lag accompany reports. This coalesces observations
and is not a durable history buffer; accepted records use server-side durability
and replay handling.

## Collector integration contract

Reuse existing collector registration, token rotation, heartbeat, and approved
content-pack configuration. All endpoints below are relative to
`/api/v1/content-packs/collectors/{collector_id}` and require the assigned tenant
and its collector token, or an authorized operator principal.

- `GET /network-bindings?tenant_id=...`: paginated binding IDs and assigned
  targets, source types, sites, and observation metadata. Follow `next_offset`.
  It never returns target credentials.
- `POST /network-reports?tenant_id=...`: `{reports:[...]}` with binding ID,
  source state, observation/contact times, queue depth and lag. Only the assigned
  tenant/collector can report. Ready/Partial require contact evidence. Reports
  outside the last day or over one minute in the future are rejected.
- `POST /network-events?tenant_id=...`: `{batch_id,events:[...]}` with transport
  sender IP, receiver observation time and message. The server resolves target
  identity from the binding and journals events through existing ingestion.
- `POST /network-otlp?tenant_id=...`: OTLP/HTTP logs in JSON or protobuf. Records
  require a textual body, receiver observation timestamp, and transport attribute
  `net.peer.ip` or `network.peer.address`. Resource hostnames/target attributes
  cannot override binding ownership. Compression is currently unsupported.

The existing OTel config renderer accepts optional `network_logs_endpoint` and
routes Syslog sources through `otlphttp/controlone.network`, with compression
`none`, existing persistent queues/retries, and batches of at most 100 records.
Other sources retain their existing exporter. The Syslog receiver's transport
attributes are enabled by default. See the upstream
[Syslog receiver documentation](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/syslogreceiver/README.md).

Source report and event batches are limited to 100 records; messages to 8 KiB;
OTLP/event bodies to 1 MiB; report bodies to 64 KiB. Collector ingestion has a
tenant/collector token bucket of 1,000 records/sec with a 2,000-record burst.
Journal replay keys preserve idempotence without refreshing old contact times.
Rebinding changes the binding ID, rejecting reports from obsolete assignments.

Syslog forwarding is **Partial** because this bridge preserves raw text without
claiming parser coverage. A collector may report Ready only after successful
collection/processing. SNMP source reports are independent of Syslog reports;
administrators deploy the supplied SNMP collector and configure local credential
access. Saving a binding does not deploy a process or export target credentials.

## Supported collection adapters and limits

The site collector supports SNMPv3 polling and SNMP trap reception, plus fixed
read-only SSH configuration snapshots for Cisco, Juniper and Fortinet, NETCONF
`get-config`, RESTCONF GET, and a configured vendor API GET. Management credentials
and trusted CA files stay on the collector host and are referenced by environment
variable names. SSH host keys are pinned by fingerprint. Commands are fixed in
code; arbitrary commands, config changes and API redirects are rejected. Sensitive
configuration fields are redacted before snapshot records leave the site.

SNMP traps accept authenticated SNMPv3 authPriv or a locally configured SNMPv2c
community. Restrict the optional UDP listener to expected sender networks at the
collector host firewall. NetFlow v5/v9, IPFIX and sFlow v5 can be decoded by the
existing OTel netflow receiver and forwarded through the same source identity
endpoint; bind each observed UDP exporter address to its source type. The upstream
OTel receiver does not cover sFlow counter samples or custom fields. Collector
configuration examples are in `controlplane/config/networkcollector.example.yaml`
and the approved OTel content-pack recipe. Physical vendor interoperability still
needs testing against the corresponding hardware and firmware. Device operational
health and automatic remediation are outside this source-readiness change.

## Verification evidence

Local checks cover source/collector freshness independently, negative and missing
contact evidence, source write/read authorization, tenant/collector isolation,
out-of-order reports, rebinding, expired grants, migration rollback compatibility,
and unchanged target reachability/node identity. PostgreSQL integration uses a
real isolated database; source observations in those tests are synthetic.

UI tests cover source/collector state display, unconfigured Flow, site ownership,
read-only permissions, and saving bindings. OTLP handler tests use authored
payloads; they are not a physical device or deployed OTel transport demonstration.
Do not equate these fixtures with physical-switch compatibility.

### Live local verification, 5 October 2026

The local app at `http://192.168.1.8:4173` was checked using the existing operator
account. The collector was explicitly named `issue261-simulated-collector`.
This was a live API/database/browser check with synthetic input, not a physical
switch or a deployed OTel receiver test.

- Saved a Syslog binding through the Network devices UI, using documentation-only
  sender address `192.0.2.10` and assigned site Demo Lagos.
- Submitted an authored OTLP/HTTP JSON log. The endpoint returned HTTP 200.
  PostgreSQL confirmed an accepted journal row containing the bound target ID
  and a null node ID.
- Refreshed the browser: Syslog was Partial, the earlier simulated SNMP report
  was Stale, and the collector was independently Not reporting with no heartbeat.
  The target remained reachable and missing Flow remained Not configured.
- Opened Observability and expanded the same device. Its protocol source states,
  tenant, assigned site, collector freshness and source contact times were visible.

### Acceptance coverage

| #261 acceptance criterion | Evidence | Result |
| --- | --- | --- |
| Syslog resolves target identity | OTLP handler tests, PostgreSQL integration, live HTTP journal check | Passed with synthetic input |
| SNMP state is independent of Syslog/Flow | State/API/UI tests and live browser with SNMP Stale, Syslog Partial | Passed locally |
| Missing Flow does not mark a reachable device down | PostgreSQL reachability check and live browser | Passed locally |
| Collector ownership/site is visible | Tenant isolation tests and both live UI views | Passed locally |
| Stale collector differs from stale device/source | Freshness unit/API tests; live UI separates Not reporting collector from Stale SNMP source | Passed locally |
| Observability uses source readiness without process/service semantics | UI tests and live Observability device expansion; journal has no node identity | Passed locally |

### Fresh extended audit, 5 October 2026

Fresh backend package checks, real PostgreSQL integration, TypeScript checking
and the original nine focused UI tests passed again. Additional tests exercise
the real network Observability panel rather than mocking it out: authorized
All tenants pagination, target detail links and failed target reads. Both new
tests passed; the focused network panel suite passed all four tests, bringing
the verified relevant UI cases to eleven across the two runs.

An actual official `otel/opentelemetry-collector-contrib:0.123.0` container ran
the configuration produced by the existing content-pack renderer. Its binary
validated the configuration. A separate Alpine container sent synthetic RFC5424
Syslog over UDP. The receiver exported OTLP/protobuf to the local control plane;
PostgreSQL verified two accepted batches, both with the canonical network target
ID and no node ID. Parsed message hostnames did not determine target ownership.
This is deployed collector transport evidence with a synthetic sender, not a
physical switch test. The collector used the existing local operator principal
with explicit user approval; deployed production collectors should use their
existing scoped collector-token workflow.

The initial test endpoint, `host.docker.internal`, did not resolve on the local
Docker network. The exporter retried and persisted the packet. After changing
only the ignored test configuration to service DNS (`controlplane:8443`) and
restarting the collector, the persistent queue recovered the first packet; a
second packet also reached ingestion. Both temporary containers were stopped and
removed after the checks, including the container holding the approved token.

Twelve live API assertions passed, covering receipts, binding reads without target
credentials, replay idempotence, rejection of unbound senders, independent source
and collector states, site/queue/lag evidence, rejection of Ready without contact,
unauthenticated-read rejection, unchanged reachability with absent Flow, and no
compute-node identity. SNMP observations and queue/lag numbers in this check were
synthetic reports, not a running SNMP poller. The receiver itself did not submit
collector heartbeats, so its platform state correctly remained Not reporting.
Local evidence is stored in ignored
`tmp/local/network-target-demo/issue261-full-api-demo.json`.

The initial fresh visible browser walkthrough was interrupted during the earlier
attempt. The completed rerun is documented below.

### Recurring-poller gap closure, 5 October 2026

The recurring scheduling test and real Net-SNMP authPriv integration test passed,
including counter reads and explicit unknown-user rejection. The repository-built
collector then ran seven actual polling cycles against the existing synthetic
Net-SNMP container. PostgreSQL verified seven network.snmp observations containing
uptime counters, the canonical target ID and no node ID. Source state was Ready;
the independently reported collector state was Healthy with a real heartbeat.
The temporary worker exited after the bounded demo. This closes the recurring
poller implementation and local protocol verification gap, using a synthetic
device rather than physical hardware.

Metric API tests verify bearer collector-token authentication, replay handling,
canonical identity and rejection of unassigned bindings/unknown metric fields.
Worker tests verify recurring scheduling, assignment filtering and heartbeats.
The fresh visible UI walkthrough was completed later in this verification, as
documented below. Physical-switch compatibility remains unverified. Protocol and
ownership tests for the broader adapters use software fixtures and do not establish
compatibility with every vendor or firmware release.

### Completed fresh browser acceptance walkthrough, 5 October 2026

The in-app browser connection became available again. The six acceptance
checkboxes were re-read directly from GitHub; all remain unchecked there.
No issue checkbox, comment, or status was changed.

The visible walkthrough used the existing local operator session and the
synthetic device `Demo repeat verified switch`. It opened device details,
refreshed source states, opened the binding form without replacing bindings,
navigated to Observability, expanded the same device and followed Open device
back to its details. Both views showed separate SNMP/Syslog states, assigned
collectors and tenant/site, source contacts, collector heartbeats, queue/lag
and explicit unconfigured Flow. SNMP and its collector were Stale after the
bounded worker had stopped; Syslog was Stale and its collector Not reporting.
The device remained reachable. Network sources appeared under their own
readiness section rather than as device processes/services.

The walkthrough found and corrected misleading inventory wording that said
recurring telemetry was disabled. It now says inventory refresh is manual and
directs operators to Network telemetry sources for recurring readiness. The
corrected text was verified in the live browser.

Fresh focused UI verification passed all 14 tests across the telemetry panel,
inventory panel, Network devices and Observability. The real isolated PostgreSQL
telemetry integration and network/Syslog content-pack renderer checks also passed.
An uncached short-mode run passed the networkcollector, networkdevice, server
and storage packages. TypeScript checking and `git diff --check` passed.
Together with the earlier actual OTel UDP transport and recurring Net-SNMP
poller evidence, this completes local verification of the six acceptance
criteria. The fresh screenshot is stored in ignored
`tmp/local/network-target-demo/issue261-acceptance-fresh.png`.

Physical-switch compatibility remains unverified. It is separate from the six
written acceptance checks, which do not explicitly require physical hardware.
The broader source adapter paths now have local implementation and software
fixture coverage: SNMP trap UDP decoding and sender binding, NetFlow/IPFIX/sFlow
OTLP source attribution, SSH/NETCONF read-only snapshots, and RESTCONF/vendor API
GET with pinned destination, trusted TLS and redaction. Package tests for
networkdevice, networkcollector, server, storage and the content-pack renderer
passed after these adapters were present. The OTel Syslog receiver also completed
a real local UDP-to-journal test.

### Live IPFIX and sFlow software-exporter verification, 5 October 2026

The official OpenTelemetry Collector Contrib receiver (`0.123.0`) listened on
UDP 2056 for IPFIX and UDP 6343 for sFlow. A temporary software sender sent an
IPFIX v10 template and flow record from `172.20.0.9`, plus an sFlow v5 raw
packet-header flow sample from `172.20.0.10`. The collector forwarded both into
Control One. PostgreSQL journal rows were accepted and attributed to the demo
network device with the correct source type, sender IP and source binding ID;
IPFIX decoded the expected TCP tuple and 900 bytes/15 packets, while sFlow
decoded its TCP tuple and 54-byte/one-packet sample. Neither event had a node
identity. Local evidence is in ignored
`tmp/local/network-target-demo/issue261-ipfix-sflow-live-demo.txt`.

The temporary receiver, sender and builder containers were removed after the
run. Their demo source bindings remain in the local app and will become Stale
when their five-minute freshness window expires without new packets. This is a
software-generated protocol test, not a physical-switch or vendor/firmware
compatibility test. It tested IPFIX v10 and sFlow v5 flow samples only; sFlow
counter samples and custom field mappings remain unsupported by the selected
upstream receiver.
