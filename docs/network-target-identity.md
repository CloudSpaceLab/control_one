# Network target identity

This implements the identity foundation of issue #257 / sub-issue #258.
Protocol discovery, credential verification, telemetry binding, and operator
onboarding screens are subsequent slices (#259–#263).

## Compatibility

`targets` is the estate identity table. Existing compute nodes are backfilled
with `targets.id = nodes.id` and `targets.node_id = nodes.id`. Node APIs retain
their identifiers, enrollment behavior, and state machine; node responses add
`target_id`. Database triggers synchronize node identity, labels, lifecycle,
and last-observed time with targets for both existing and future node writers.
Agent-declared capabilities are projected separately from collection readiness.
Node retirement retires its target. Node deletion clears the node link and
preserves the retired target and address evidence. Tenant deletion removes
the tenant's targets.

Network devices have `family=network_security` and no node row. Supported types:
router, switch, firewall, load_balancer, waf, vpn_gateway, wireless_controller,
access_point, ids_ips, and network_appliance. The schema also reserves
`other_infrastructure` for hypervisor, storage_appliance, and unknown targets.

Management addresses and observed addresses are separate evidence records.
Node public IP changes retain old evidence with `current=false`. Addresses
carry source, confidence, and first/last-seen timestamps; an address is never
the target's primary identity.

## API

- `GET /api/v1/targets`: mixed compute/network inventory.
- `GET /api/v1/targets/{id}`: identity and address evidence.
- `POST /api/v1/targets`: create a manually classified network identity.

List filters: `tenant_id`, `family`, `type`, `site`, `group`, `vendor`, `model`,
`platform`, `firmware`, `reachability_state`, `collection_state`,
`lifecycle_state`, `search`, `limit`, and `offset`. Omit `tenant_id` for All
tenants accessible to the signed-in user. Search matches display name,
hostname, serial, and current addresses. Literal SQL wildcard characters are
escaped. Results sort by display name and UUID. Count and page share one
repeatable-read transaction; totals do not depend on the page length.

Creation example:

```json
{
  "tenant_id": "00000000-0000-4000-8000-000000000001",
  "type": "switch",
  "display_name": "Branch switch",
  "hostname": "switch.branch.example",
  "site": "Lagos",
  "group": "Branch network",
  "management_addresses": ["192.0.2.10", "switch.branch.example"]
}
```

Creation returns a `Location` header and the new target. Classification is
explicitly operator-supplied, with evidence stating that it is not protocol
verified. Reachability starts `unknown`; collection starts `discovered`;
capabilities and management modes are empty. Neither observation nor successful
collection timestamps are fabricated. This endpoint does not probe a device,
install an agent, create an enrollment token, or claim inventory readiness.

Unknown creation fields are rejected, including credentials, node links,
claimed capabilities, and readiness. The next onboarding slice will reference
credentials through a separate secret system.

## Access control

Endpoints require a persisted user identity. Agent principals and unresolved
users are refused. `targets.read` and `targets.write` are new permissions.
Read defaults: admin, operator, viewer, investigator, ciso. Write defaults:
admin and operator. Custom roles can receive these permissions.

SQL authorization checks the actual role permission, tenant assignment, and
assignment expiry before reading, counting, or inserting. A global role applies
to all tenants; a scoped role applies only to its tenant. An inaccessible detail
ID returns 404; an unauthorized creation returns 403. All tenants never selects
the first tenant implicitly. Creation audit records contain target family and
type, with no credential material.

## Validation

`go test ./controlplane/internal/server ./controlplane/internal/storage ./controlplane/internal/migrate -short`
tests request validation, strict payloads, identity compatibility, permission
propagation, pagination totals, routing, and failure behavior.

`go test ./controlplane/internal/storage -run TestTargetIdentityWithPostgres -count=1 -v`
tests the full migration chain, real tenant grants/expiry, independent network
targets, node synchronization, address history, and tenant constraints using
PostgreSQL through Testcontainers.

## Issue #258 acceptance audit (2026-10-02)

These are local verification results, not a claim that GitHub CI has run or
that the issue has been closed. The results were recorded before committing;
remote CI and merge verification remain pending.

| GitHub acceptance criterion | Result | Evidence |
| --- | --- | --- |
| Existing node enrollment/tests remain unchanged | Pass | Existing enrollment tests were not edited; the enrollment/target regression suite passed, including machine-ID deduplication, pending enrollment, re-enrollment and reaper behavior. |
| A network device can exist without a node row/agent | Pass | PostgreSQL integration test and live API records show switch/firewall targets with no node link. |
| Target search can return both nodes and network devices | Pass | Live search returned two network targets and one managed-compute target; the compute target uses its original node UUID. |
| Tenant/site/type filters are indexed and server-side | Pass | Live filtered requests passed; the actual database contains `targets_tenant_site_type_idx`, `targets_family_type_idx` and related tenant/group indexes. |
| Classification provenance is returned through API | Pass | Live target responses include operator source, confidence and explicit evidence that classification is not protocol verified. |
| No network device is represented as a stale/failed agent | Pass | Live switch/firewall records are `network_security`, have no node link, and start as `discovered` with `unknown` reachability. |

The live API exercise also verified exact pagination (one returned row with
total three), All tenants across two demo tenants, and HTTP 400 for credential
fields in the identity payload. Tenant isolation and expired scoped grants were
tested against real PostgreSQL in the integration suite.

The interrupted demo left three fictional identity records in two local demo
tenants under prefix `network-demo-20261002-115250-c84bbb`. These are not physical
device discovery results. Its temporary demo script was removed at the user's
request; this audit does not require that script.

## Browser-only guide: register and inspect a network device

This is identity registration, not verified protocol onboarding. The current
build saves devices through the real target API and shows them in the console.
SNMPv3/SSH credentials, connection tests, vendor fingerprinting and telemetry
collection are still outstanding in #259–#261. This flow is available in the
local build; it has not been deployed to production.

1. Open the existing app at `http://192.168.1.56:4173/console/login` and sign in
   with your existing operator/admin account. A production user uses their
   normal console URL and account after this change is deployed.
2. Select the intended tenant in the top tenant selector.
3. Click **Network devices** under Operations in the sidebar. Alternatively,
   click **Enroll**, then **Add network device** on the enrollment page.
4. Click **Add network device** in the inventory. Check the **Tenant** field.
   In All tenants scope you must choose a specific tenant to own the device.
5. Enter a **Device name**, choose **Device type**, and enter its **Management
   address** as an IP address or DNS name without a protocol or port. Optional
   fields are **Hostname**, **Site**, and **Group**.
6. Click **Save device**. If an address is invalid, the form displays an error
   and retains your entries so you can correct it. A successful save opens the
   persisted device detail.
7. Inspect **Agent requirement: Agentless**, **Reachability: Not verified**,
   **Collection: discovered**, and **Last successful collection: Never**.
   Classification lists **Source: operator** and explicitly says the selected
   type is not protocol verified. Vendor/model and platform/firmware remain
   **Not detected**. Saving an identity does not establish contact or health.
8. Click **Back to inventory**. Use **Search devices**, **Device type**,
   **Site**, and **Group** to locate it. Filters and pagination use the server's
   exact result set. Use **Refresh** to reload saved identities.
9. Choose **All tenants** in the top selector to see all network devices your
   account can read. Each row identifies its tenant. Choose a single tenant to
   narrow the list. Compute devices remain in **Servers**.

For a safe local walkthrough, use tenant
`network-demo-20261002-115250-c84bbb-a`, name `Demo Lagos branch switch`, type
**Switch**, address `192.0.2.30`, site `Demo Lagos`, and group `Demo branch`.
That record was saved through the UI on 2026-10-02 and remains in the local
database; search for it to inspect without creating another record. The address
is a documentation example, not a physical device.

Live browser verification covered invalid-address rejection, successful save,
persisted detail, cross-tenant Firewall filtering, and search for the saved
switch. UI tests cover save/evidence, failed-save form retention, exact paging
and All tenants filtering, and hiding creation from read-only users.

## Developer reference: existing local application

Follow `scripts/local.ps1` and `docs/local-development-windows.md`; do not create
a separate demo server. Run from the repository in PowerShell:

```powershell
./scripts/local.ps1 status
# If the UI is stopped and the configured LAN address is available:
./scripts/local.ps1 start
# For the current Wi-Fi address used during this verification:
./scripts/local.ps1 start -BindAddress 192.168.1.56
./scripts/local.ps1 status -BindAddress 192.168.1.56
```

The existing launcher's default bind address is
`http://192.168.1.10:4173/console/login`. Existing account credentials are in the
ignored `tmp/local/credentials.json`; the local guide identifies `admin@local`
for full access. Do not regenerate existing account passwords.

On the audit date, that address was not assigned to this machine. Its Wi-Fi
address was `192.168.1.56`, and the existing UI error log reported
`EADDRNOTAVAIL` for `192.168.1.10`. Restore the configured address or explicitly
choose an updated bind address when starting the browser demo. The control
plane remains available at `http://127.0.0.1:8443`.

With the user's selected current address, the existing launcher was started
using `-BindAddress 192.168.1.56`. Open
`http://192.168.1.56:4173/console/login` and sign in with the existing local
admin account. `-BindAddress` also sets the matching agent-facing URL; it does
not create another launcher or change account passwords.

For the same console demonstration, select **Servers**, then choose tenant
`network-demo-20261002-115250-c84bbb-a` in the top tenant selector and expand
its fleet group. Only the compute fixture appears. It shows enrollment failed
because no actual agent was attached; this is not the switch's status. Tenant
`network-demo-20261002-115250-c84bbb-b` has the firewall identity but no server
row. Network identity state is verified through the target API below.

The launcher and Windows local guide are ignored local-development files;
their bind-address update is present on this machine but is not part of the
tracked implementation diff.

The console now also has a Network devices inventory and registration form.
The browser-only guide above is the user workflow. The following API commands
are supplemental developer diagnostics, not steps required of product users:

```powershell
$accounts = Get-Content tmp/local/credentials.json -Raw | ConvertFrom-Json
$account = $accounts | Where-Object role -eq 'admin' | Select-Object -First 1
$session = Invoke-RestMethod -Method Post -Uri 'http://127.0.0.1:8443/api/v1/auth/login' `
  -ContentType 'application/json' `
  -Body (@{email=$account.email; password=$account.password} | ConvertTo-Json)
$headers = @{Authorization="Bearer $($session.token)"}
Invoke-RestMethod -Uri 'http://127.0.0.1:8443/api/v1/targets?search=network-demo-20261002-115250-c84bbb&limit=1' -Headers $headers |
  ConvertTo-Json -Depth 10
```

Expected: pagination total three with one returned identity. Increase `limit`
to see all three. Add `family=network_security` for the two network identities;
add `site=Demo%20Abuja&type=firewall` for the firewall. Read
`/api/v1/targets/{id}` for classification/address evidence. Existing
`/api/v1/nodes` responses include the stable `target_id` compatibility link.
Keep the session token private; it is used only in the request header.
