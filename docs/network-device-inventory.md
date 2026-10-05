# Network device inventory (#260)

Issue #260 adds manual SNMPv3 inventory collection to the verified connections
created by #259. Authentication remains separate from inventory collection.
The refresh performs read-only GET/BULKWALK operations; it neither installs an
agent nor enables recurring telemetry, device configuration, or remediation.

## Browser-only guide

1. Sign in to the existing console and select the intended tenant.
2. Open **Network devices** and select a device onboarded through a verified
   **SNMPv3** connection. An identity-only device must first be onboarded with
   credentials using the #259 flow. SSH-only inventory collection is explicitly
   unsupported in this implementation.
3. In the device detail, find **Collected inventory**. Before the first
   successful refresh, the panel says no successful inventory was recorded.
4. Click **Refresh inventory**. This uses the saved encrypted credential
   reference and management endpoint; the UI asks for no shell command or
   additional secret. The account needs `targets.connect`; readers need
   `targets.read` to inspect snapshots.
5. Review **Latest attempt** and **Snapshot observed**. A successful inventory
   updates the device's collection state and last successful collection time.
   This records an inventory read, not device health or recurring telemetry.
6. Expand **Interfaces**, **LLDP / CDP neighbors**, **Chassis / modules**,
   **IP addresses**, **VLANs**, **ARP**, **Route summary**, and **CPU / storage /
   physical sensors**. Each returned fact shows protocol, source OID or
   heuristic source, and observation time. Empty sections mean no facts were
   collected, not proof that the device has no such objects.
7. Expand **Unavailable / incomplete fields** to see unsupported, inaccessible,
   timed-out, or bounded sections. Unknown facts are omitted; default serials,
   firmware, neighbors, health or configuration hashes are never invented.
8. **Sanitized protocol evidence** contains bounded OID/value evidence for
   debugging. It is access-controlled and excludes known credential strings.
9. Refresh again to replace the snapshot without appending duplicate rows.
   After a failed refresh, the panel retains the previous snapshot and its
   original observation times, displays the failure separately, and the device
   collection state becomes stale if it previously collected inventory.

## Coverage and limitations

The common adapter reads system identity, IF/IF-X interfaces, ENTITY chassis
metadata, LLDP remote entries, Cisco CDP entries, IPv4 address/ARP/route tables,
Q-BRIDGE VLANs, HOST-RESOURCES CPU/storage, and ENTITY-SENSOR observations where
the device exposes them. Sensor values retain type, scale, precision, units,
and status; they are not converted into an invented health signal.

The common contract is based on standard MIB semantics, including
[IF-MIB](https://www.rfc-editor.org/rfc/rfc2863),
[ENTITY-MIB](https://www.rfc-editor.org/rfc/rfc4133), and
[ENTITY-SENSOR-MIB](https://www.rfc-editor.org/rfc/rfc3433).
Every snapshot includes an adapter version. Serial and firmware are promoted
to device fields only from an explicitly identified chassis. Classification
heuristics retain confidence and normalized evidence.

| Vendor family | Identity signature | Inventory route | Fixture coverage | Physical-device validation |
| --- | --- | --- | --- | --- |
| Cisco IOS/IOS XE/NX-OS | Cisco/IOS XE/NX-OS | Common SNMP MIBs + CDP | Authored synthetic Cisco PDUs | Not performed |
| Fortinet FortiOS | FortiGate/FortiOS | Common SNMP MIBs | Authored synthetic Fortinet PDUs | Not performed |
| Juniper Junos | Junos | Common SNMP MIBs when exposed | Identity parser only | Not performed |
| Arista EOS | Arista | Common SNMP MIBs when exposed | Identity parser only | Not performed |
| Palo Alto PAN-OS | PAN-OS | Common SNMP MIBs when exposed | Identity parser only | Not performed |
| F5 BIG-IP | BIG-IP | Common SNMP MIBs when exposed | Identity parser only | Not performed |

This matrix does not claim vendor firmware parity or proprietary MIB support.
HA/cluster role, routing-neighbor protocols, IPv6/ND, configuration revision/hash,
and vendor-specific SSH inventory commands remain unsupported and explicitly
visible as such. The current firewall fixture validates authored common-MIB
responses; it does not prove a physical FortiGate exposes those exact tables.

Limits: 10-second refresh context, 1-second request timeout, no retries,
20 repetitions per bulk request, 2,048 PDUs per tree, 8,192 PDUs overall,
512 records per section, 256-byte textual values, 2,048 raw evidence records,
and a 4 MiB storage limit. Oversized/partial walks are identified as incomplete.
Concurrent refreshes of the same target are refused; an abandoned refresh can
be superseded after two minutes. Late completions cannot overwrite newer work.

Existing destination CIDR policy, DNS pinning, encrypted credentials,
tenant-grant expiry, bounded connection pool, and audit patterns are reused.
The read-only SNMP account may need access beyond sysDescr/sysObjectID for the
additional MIBs. No SNMP SET permission is required.

## API and persistence

- `GET /api/v1/network-inventory/{target_id}` returns the latest attempt and
  last successful snapshot. It returns `not_collected` for an authorized
  network target without a snapshot and 404 for inaccessible targets.
- `POST /api/v1/network-inventory/{target_id}` accepts `{}` only and refreshes
  the saved connection. Callers cannot inject credentials, commands or an
  alternate address. It audits start/completion and returns the latest state.

Migration 160 stores one inventory snapshot per target. Refresh uses a new
attempt ID; successful refresh atomically replaces the snapshot and updates
target collection timestamps. Failed refresh preserves previous evidence.
An empty successful field remains unknown rather than reusing old detected
vendor/model/serial/firmware as a new observation. Interface IDs use the
device's interface index; their stability across a device reboot depends on
the device's IF-MIB implementation. LLDP neighbor keys omit its changing
time-mark index; LLDP and CDP evidence remain separately attributable.

## Acceptance verification

| Issue criterion | Local evidence |
| --- | --- |
| Cisco + one firewall vendor fixture-tested | Synthetic Cisco and Fortinet PDU fixtures exercise identity, chassis/interface data, missing fields and provenance. They are not physical-device captures. |
| Stable and deduplicated interface inventory | Fixture tests repeat identical snapshots; real Net-SNMP transport tests repeat Linux container interfaces; PostgreSQL replaces rather than appends snapshots. |
| LLDP/CDP source and freshness | Fixture tests preserve discovery protocol, local port index, source OID and observation time; changing LLDP time marks do not duplicate a neighbor. |
| Unsupported fields absent/unknown | Unknown facts are omitted and unavailable sections are returned; UI presents empty sections without asserting absence. |
| Idempotent and bounded refresh | Protocol tests exercise bounds; PostgreSQL tests verify concurrency, replacement, failed refresh preservation and late-completion rejection. |

Commands used for local verification:

```text
go test ./controlplane/internal/networkdevice -count=1
go test ./controlplane/internal/networkdevice ./controlplane/internal/server ./controlplane/internal/storage ./controlplane/internal/migrate -short
go test ./controlplane/internal/storage -run 'Test(NetworkInventory|NetworkOnboarding|TargetIdentity)WithPostgres' -count=1
cd ui
node node_modules/typescript/bin/tsc --noEmit
node node_modules/vitest/vitest.mjs run src/components/NetworkInventoryPanel.test.tsx src/components/NetworkDeviceWizard.test.tsx src/pages/NetworkDevices.test.tsx src/pages/Onboard.test.tsx
```

The isolated Net-SNMP fixture performs real SNMPv3 authentication and inventory
requests. Its Cisco identity is configured synthetic text; its interfaces and
resources belong to the Linux container, not a Cisco chassis. Physical-device,
production deployment, and GitHub CI validation are not claimed.

Local verification recorded on 2026-10-02: backend regression suites,
PostgreSQL integration, TypeScript compilation, and all 18 relevant UI tests
passed. The running local API refreshed the simulated device twice and returned
two stable Linux-container interface IDs, `inventory_ready`, an actual inventory
collection timestamp, no node link, and no fixture auth/privacy secrets in the
response. Saved API evidence is in the ignored local demo directory.

Fresh live browser verification completed on 2026-10-05 in Chrome using the
existing local operator account. The in-app browser still timed out. The
computer's LAN address had changed; the existing UI launcher was restarted on
`http://192.168.1.8:4173/console/network-devices`.

The walkthrough opened **Demo repeat verified switch** in the demo tenant,
clicked **Refresh inventory** twice, and observed successful snapshots at
09:24:43 and 09:25:03 local time. Both returned two Linux-container interfaces
with IDs `1` and `2`, without duplicates. Expanded interface facts showed
SNMPv3, source OIDs, and the new observation time; unavailable fields explicitly
reported missing adapters or unexposed MIBs. Reloading the page retained the
09:25:03 snapshot and the target's `inventory ready` collection state.

This verifies the live UI success path and persistence against the simulated
Net-SNMP device. The configured Cisco identity remains synthetic. Physical
switch validation and a live UI failure-path demonstration were not performed.
