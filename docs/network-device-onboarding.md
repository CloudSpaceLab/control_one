# Network device onboarding (#259)

The dedicated **Network devices** flow verifies read-only SNMPv3 or SSH access
before saving an agentless target. It never generates a machine enrollment
token or creates a node. Identity-only registration from #258 remains available
through **Register identity only** and clearly identifies unverified records.

## Browser-only guide

1. Sign in to your console with an admin/operator account, or a custom role
   with `targets.read`, `targets.connect`, and `targets.write`. The current
   local console is `http://192.168.1.56:4173/console/login`.
2. Select the intended tenant. Open **Network devices**, then **Add network
   device**. You can also reach this through **Enroll → Add network device**.
3. Check **Tenant**. Enter **Device name**, **Management address** (IP/DNS),
   **Port**, and optional **Site** and **Group**. Use the address reachable
   from the control plane, without a URL scheme or port in the address field.
4. Select **SNMPv3 (authPriv)** or **SSH (read-only)**.
   For SNMPv3, enter the read-only username, SHA256 (or SHA compatibility)
   authentication algorithm, authentication secret, and AES privacy secret.
   For SSH, enter the read-only username, trusted SHA256 host-key fingerprint
   supplied by your device administrator, and password or private key.
   The account needs only `show version` permission. No enable/config access
   is requested.
5. Click **Test connection**. Credential material is encrypted using the
   existing credential store; tests use its reference. Secret inputs clear
   after encrypted storage succeeds.
6. If authentication fails, click **Use another credential** and correct it.
   If unreachable, check the address, port, routing, firewall, and protocol.
   A silent SNMP rejection can also look like a timeout. **Policy blocked**
   means the destination is outside the configured allowed networks or an
   SSH host key did not match. Failed tests cannot save a verified device.
7. On success, review vendor, model, platform, confidence, evidence, and
   required privileges. Choose the correct **Device type**. Low-confidence
   detection explicitly requests review; an override retains the original
   evidence and confidence.
8. Optionally keep the detected identity source selection. This records your
   choice; this slice does not start recurring collection, syslog, or flow.
   Check **I reviewed the classification and connection result**.
9. Click **Save verified device** within 15 minutes of the successful test.
   Inspect the saved target: agentless, reachable at test time, collection
   `authenticated`, detected identity, and classification provenance.
   **Last successful collection: Never** remains accurate until a subsequent
   collection implementation actually collects data.
10. Return to inventory and use search/type/site/group filters. Compute
    enrollment remains under **Servers/Enroll** with its existing agent flow.

Product users perform these steps through the UI; they do not write shell code.

## Operator prerequisites

The existing `secrets` sealer must be configured before credentials can be
stored. Without it the endpoint refuses the request. Configure the control
plane's explicitly permitted appliance destinations, for example:

```yaml
network_onboarding:
  allowed_cidrs: ["192.168.10.0/24"]
```

The example configuration defaults to an empty list, which denies probes.
All DNS answers must be allowed; the probe pins one resolved IP. Loopback,
link-local, unspecified, and multicast destinations remain blocked.
Use a trusted HTTPS console in production when entering credentials.

## API and persistence

- `POST /api/v1/network-onboarding/credentials` encrypts a protocol credential
  and returns metadata/reference only. It requires `targets.connect`.
- `POST /api/v1/network-onboarding/tests` accepts tenant ID, credential ID,
  address, and optional port. It records a receipt and audits the test before
  opening a bounded connection. It returns normalized identity evidence.
- `POST /api/v1/network-onboarding/save` consumes the caller's authenticated
  receipt, identity fields, and source selection. It requires `targets.write`.
  Saving is atomic and retrying the same receipt returns the same target.

Migration 159 stores test receipts and target connection bindings. Target and
connection rows contain secret references only. Encrypted credentials reuse
`provider_credentials`. Composite tenant foreign keys and actual role grants
enforce tenant isolation; expired grants cannot connect or save. Audit/log
records omit credential material and raw device banners.

Adapters share a bounded read-only probe/result contract and declare required
privileges. SNMPv3 performs GETs of sysDescr/sysObjectID; SSH executes only
`show version` with pinned host-key verification and bounded output. NETCONF,
RESTCONF, and vendor API adapters are reserved extensions, not implemented
protocol choices. SNMPv2c is omitted. Detection only returns fields supported
by available evidence; unknown identities remain low confidence.

## Verification

```text
go test ./controlplane/internal/networkdevice -count=1
go test ./controlplane/internal/server ./controlplane/internal/config ./controlplane/internal/migrate -short
go test ./controlplane/internal/storage -run 'Test(NetworkOnboarding|TargetIdentity)WithPostgres' -count=1 -v
cd ui
node node_modules/typescript/bin/tsc --noEmit
node node_modules/vitest/vitest.mjs run src/components/NetworkDeviceWizard.test.tsx src/pages/NetworkDevices.test.tsx src/pages/Onboard.test.tsx
```

The protocol tests include an isolated Net-SNMP fixture with public test-only
credentials and a local SSH fixture. They are not physical-device validation.
PostgreSQL integration covers tenant isolation, expiry, secret references,
receipt ownership/staleness, failed-save denial, classification overrides,
idempotency, and absence of nodes/enrollment tokens.

## Issue #259 acceptance audit (2026-10-02)

All six criteria passed local verification. This does not claim GitHub CI,
production deployment, physical-device testing, or closure of the issue.

| GitHub acceptance criterion | Result | Evidence |
| --- | --- | --- |
| Router/switch/firewall can be onboarded without an agent | Pass | Network type validation and PostgreSQL onboarding tests preserve agentless identity. Switch and firewall saves have no node; the integration database has zero nodes and enrollment tokens. Live UI saved a verified switch without a node. |
| Failed auth and unreachable are distinct states | Pass | Real SSH authentication/reachability tests and Net-SNMP rejection test passed. Live UI produced separate `auth_failed` and `unreachable` receipts; neither offered a verified save. |
| Successful connection fingerprints vendor/model/platform where evidence permits | Pass | Real Net-SNMP identity read and live UI detected Cisco / C9300-48P / IOS XE. Parser tests cover supported signatures and unknown identities. |
| User can review/override low-confidence classification | Pass | Wizard requires explicit review and permits type selection; UI test and PostgreSQL integration verify low-confidence overrides retain evidence and confidence. |
| SNMPv3 auth/priv secrets never appear in API responses/logs | Pass | API/audit/logger tests use secret sentinels and inspect responses/logs. Encrypted storage tests verify reference-only bindings. Recent live backend logs contained neither public fixture auth nor privacy secret. |
| Existing server/computer onboarding remains separate and unchanged | Pass | Existing server enrollment regressions and all six machine onboarding UI tests passed. Network onboarding uses separate endpoints and creates no enrollment token. |

Backend networkdevice/server/config/migrate suites, PostgreSQL integration,
TypeScript compilation, and all 15 relevant UI tests passed. `git diff --check`
passed after cleanup. Live UI verification also caught and fixed HTTP LAN
compatibility for credential names and success notifications.

### Repeat the local UI demo

The existing local app contains **Demo verified SNMP switch** in tenant
`network-demo-20261002-115250-c84bbb-b`, target
`4481e2c1-875c-43ee-9d5d-68b351c670ef`. Search for that name to inspect the saved
device without creating another record.

For a fresh walkthrough, use the browser-only steps above with the isolated
local Net-SNMP fixture: address `172.20.0.6`, port `161`, username
`fixture-readonly`, SHA256 authentication secret `fixture-auth-secret`, and
AES privacy secret `fixture-priv-secret`. These are public test-only values
from the repository's fixture, not production credentials. Use a distinct
device name. The ignored local configuration allows only this fixture IP.
Its container must remain running for connection tests; its address may change
if recreated.

To reproduce a rejected authentication result, use username
`fixture-unknown-user`. To reproduce unreachable, use port `162`. After either
failure, correct the credential or port and test again before reviewing and
saving. Source selection records the choice only; recurring telemetry and
posture remain subsequent issue work.
