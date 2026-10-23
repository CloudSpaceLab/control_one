# Patch posture qualification update — 21 October 2026

**Latest evidence, 23 October:** [the three-area report](patch-posture-three-areas-verification-2026-10-23.md) supersedes earlier pending Linux-rollout and historical-version limitations: signed Linux/server candidates are deployed, independent release/cache checks pass, and a third bounded app upgrade resolves a real Ubuntu finding. Recovery qualification is recorded there with its scope. All four Phase 0 groups and Phase 1 remain open for their remaining gates.

**23 October continuation:** policy-key/cache and installer signature-failure fixes pass focused Linux/Windows policy tests and Linux bundle/installer tests under the original Trust group. See [the dated trust evidence, live-rollout boundary and remaining gates](patch-posture-trust-verification-2026-10-23.md). These fixes have not been rolled out to the enrolled agents; none of the four evidence groups is closed.

The same continuation restored Ubuntu connectivity after the recreated API lost its lab network, and updated the existing `scripts/local.ps1` to restore that optional connection on startup. The app again shows Ubuntu connected with a current heartbeat. This runtime repair is separate from pending signed-mode deployment; see the dated report for verification limits.

The second real Ubuntu upgrade verified the corrected counter in the live app. Telemetry recovery, a local HTTPS endpoint and Windows runtime checks also progressed. **Phase 0 and Phase 1 remain open.** This is local qualification on branch `0y3/Me/patch-posture-review`, not a signed production release or a claim of complete platform coverage. No new `.ps1` file was created.

Final quarantine catalog: **760 files containing 1,847 retained events**, with file hashes saved in `quarantine-catalog.json`. This includes the initial administrator repair and subsequent automatic quarantine by the corrected service. The Windows configuration hash remained unchanged. The UI now labels intentionally disabled collectors **Limited by profile**, rather than leaving them indefinitely **Starting**; this still means partial coverage.

A final post-repair PostgreSQL checkpoint, `tmp/local/patch-posture-demo/recovery/controlone-qualification-20261021.dump`, was also restored successfully in isolation. SHA-256: `481554EE3F1035C9F5FAD0D01DEA621166214D29AB5B70DCED09445AE206AF2B`. Restore verification confirmed migration 165, both reconciled raw-log tables and the persisted applied count of two. The temporary restore database was removed. Build hashes, native test results and a review snapshot of 46 changed source/document files are recorded in `qualification-manifest.json`; this does not assert byte-reproducible or signed production builds.

## Phase 0 continuation — assessment guard and VirtualBox preparation

On 21 October, available host memory was 1.82 GB before work and 2.09 GB at the final VM preparation check. C: initially had 128.30 GB free. Existing applications and Docker services were retained; no user processes were terminated to reclaim memory.

- [x] Fix assessment-time validation: reject nonempty inventory assessment when no active feed exists; require readable content/bundle receipts, matching content SHA-256, bundle identity and recorded signature provenance; reject stale/expired content and expired/missing bundle expiry, including the exact expiry boundary.
- [x] Pass native Windows offline-bundle tests and focused Linux signed-import/server tests. New server regressions verify that missing, expired and altered feed evidence does not resolve or refresh an existing finding. Valid explicit-version and range matching continue to pass.
- [x] Build and deploy `verification-run/controlplane-feed-guard` using the existing candidate compose override. SHA-256: `E48AC72D46E29AE3F1E3C39AC06361CE48FD0FD8FC1A6F8EBC9EE6F4C4BEE640`. Health endpoint returned HTTP 200. Retain `controlplane-stream-fixed` for explicit executable rollback; omitting the override does not restore the previous executable.
- [x] Demonstrate the valid path visibly: Local Development → Offline bundle → Signed content bundles → Activate the existing `controlone-local-7zip-nvd-assessment` sequence 1 → Activate bundle. The app records 21 October at 11:25:37 Africa/Lagos; the activation API returned HTTP 200 and the server reported two imported findings across the tenant. This is reassessment of the existing limited checkpoint, not broader advisory coverage or two newly remediated vulnerabilities. Screenshot: `verification-run/feed-guard-live.png`.
- [x] Download Canonical's [Ubuntu 24.04 VirtualBox OVA](https://cloud-images.ubuntu.com/noble/current/) and compare its SHA-256 with the vendor checksum fetched over HTTPS: `CE599F89777C6EAA1A3156698F004B7938DCC16680132D5F10DA1269770E74A2`. This is checksum verification, not a completed detached-signature verification. Import as `controlone-phase0-ubuntu-vm`, UUID `c75ff28c-40a3-47ae-91fe-207941e4f9a5`, under the ignored project lab directory.
- [x] Configure that VM with 4096 MB RAM, two CPUs, NAT networking, disabled clipboard/drag-and-drop and a 40960 MB dynamically allocated VDI (about 1838 MB on disk at preparation). Verify state `poweroff`.
- [ ] Provision guest access, boot and enroll this new VM, then perform the collector/reboot/recovery tests. It is not yet a connected Control One node. Free sufficient host RAM before starting it; aim for approximately 6 GB available for the planned 4 GB guest and host headroom.

Receipt/checksum validation relies on the local store populated by verified import; it does not reverify signatures against a trusted key on every assessment or protect against an attacker able to rewrite both content and receipts. Missing/invalid feeds now fail assessment before finding reconciliation; the UI still needs an explicit assessment-completeness/status workflow. Product/distro coverage, feed automation and proof of real vulnerability remediation remain open. Existing findings outside the current feed's coverage also require a separate reconciliation review.

To repeat the code checks with limited build concurrency in the running development container:

```text
docker exec -e GOMAXPROCS=2 -w /workspace control-one-local-controlplane-1 go test -p 1 ./controlplane/internal/offlinebundle ./controlplane/internal/server -run "Test(ImportSignedVulnerability|VulnerabilityAssessment|UnavailableVulnerability|OfflineBundleImport)" -count=1
```

No package upgrade was run during this continuation. No external backup was uploaded, no production PKI/signing claim was made and no new `.ps1` file was created. The four Phase 0 evidence groups remain open.

## Assessment coverage and reconciliation continuation — 21 October

This continuation addresses the assessment-coverage work group while the VM remains powered off. It does not complete any of the four production-readiness groups.

The patch summary now reports feed health separately from vulnerability counts: `partial` means active feeds passed receipt/checksum/expiry validation but estate-wide product/version coverage has not been established; `expired` and `unavailable` cannot be read as successful assessment. The tenant and all-tenant patch pages display that distinction and explicitly explain that zero known findings is not proof of a vulnerability-free machine. Zero known critical/affected counts use a neutral tone. This status describes currently readable feed evidence, not a persisted, successful scan of every asset.

Offline-feed reconciliation no longer treats every inventoried package as assessed. Resolution requires matching package identity and CVE plus an explicit fixed boundary reached by the installed version; a `fixed_version` label alone, malformed range or unsupported comparison scheme cannot authorize resolution. Database reconciliation is also limited to the eligible CVE list. An omitted advisory remains unresolved. Exact-only advisories and unsupported/ambiguous evidence remain conservative; this change does not yet close findings recorded against a different historical installed version or prove real remediation on the VM.

The regression fixtures use synthetic advisory identifiers and isolated test data; they are not new production vulnerability-feed content. Broad advisory coverage, upstream refresh, persisted per-node assessment completeness, signed-release/PKI qualification and external recovery remain open.

- [x] Focused Linux feed/server tests cover explicit fix boundaries, omitted-advisory preservation and unavailable evidence; final boundary tests also reject malformed ranges and unsupported schemes.
- [x] PostgreSQL integration proves an explicit CVE filter resolves only the eligible advisory and an empty filter resolves nothing. Used dedicated database `controlone_patch_security_test_assessment_20261021`, then removed it and verified its absence. Live findings were not used as negative-test fixtures.
- [x] All 13 patch-page UI tests and the final TypeScript check pass, including the partial/expired/unavailable notice. The shared notice is used in both tenant and all-tenant views. An initial sandbox filesystem restriction prevented Vitest dependency resolution; the ordinary existing test command passed with the required host access, without dependency or ACL changes.
- [x] Deploy `verification-run/controlplane-assessment-status` through the existing compose override. SHA-256: `E35A1BB24D0FFA49730640D0479AC425FD9B15EDE8EF60EB34DBE89E8DD31F74`. Health HTTP 200, both existing agent heartbeats fresh, two Local Development findings still unresolved. The prior executable `controlplane-feed-guard` remains retained for explicit rollback. No schema migration or new `.ps1` file was required.
- [x] Demonstrate the live **Assessment coverage is partial** notice in both Local Development and All tenants, then return the browser to Local Development. Screenshots: `verification-run/assessment-partial-live.png` and `verification-run/assessment-all-tenants-live.png`. Check summary: `verification-run/assessment-status-verification.json`.

Repeat the visible check: open **Patch posture**, select **Local Development**, click **Refresh**, then read the assessment notice alongside **2 / 5** fresh inventories and the known finding counts. Select **All tenants** to see the same coverage distinction; return to Local Development afterward. A valid current feed is deliberately labelled partial because supported product/version coverage has not been established. Expired and unavailable display paths were exercised in isolated tests; the live feed was not expired, deleted or tampered with to manufacture a demo.

## Verified results

| Check | Result and evidence |
| --- | --- |
| Real bounded upgrade | Only `libaudit-common` and `libaudit1:amd64` changed, from `1:3.1.2-2.1build1.1` to `1:3.1.2-2.1ubuntu0.1`. Independent comparison of the entire installed-package list against the pre-run image confirmed exactly two changes. No new packages or removals. |
| Approval and completion | Request created 20 October, approved through the app on 21 October after rechecking validity and repository access. Deployment `2c791a29-2559-40ea-98b4-f5b6f8235641`; job `a7898e2e-9dd3-46cf-bcdb-a65d4ff7aa67`. App: **Completed → Per-node → APPLIED, PKGS 2**, applied at **08:58:27 Africa/Lagos** on 21 October. |
| Correct mode display | List and detail now derive auto-mode labels from the recorded node plan. The earlier misleading `direct` header displays **auto (proxy)**; mixed plans display **auto (per node)**. Existing stored audit data is preserved. Ten UI tests and TypeScript checks passed. |
| Stream security | Agent access is bound to its registered tenant/node and permitted topics. Live HTTPS probe: own stream HTTP 200; another node, another tenant and an unpermitted topic HTTP 403. User subscriptions now also enforce tenant access. |
| Stream longevity | Fixed the ordinary HTTP write timeout terminating streams. Logging wrappers expose deadline control; each streaming write refreshes a bounded 30-second deadline, with 20-second keepalives. A real TLS probe received the keepalive after 20.001 seconds, and an HTTP-server regression test verifies survival beyond an ordinary request write timeout. |
| Windows telemetry recovery | Initial repair preserved **639 expired files / 1,598 events**, with hashes checked before/after moving. The corrected existing Windows service then quarantined additional expired batches automatically. Final observed total: **760 quarantined files**, zero active backlog. In the measured window beginning **09:30:57 Africa/Lagos**, 26 ingestion requests returned HTTP 200 and zero returned HTTP 400. Quarantine remains evidence requiring manual review. |
| Timestamp defect | Network observations incorrectly used the connection's original start time as their event timestamp. Long-lived connections therefore produced apparently expired data. New observations use current UTC time while retaining original connection start/end timestamps in details. Regression test passed. |
| Bounded quarantine | Expired/future-skew event batches are retained outside the active replay queue, allowing newer batches to progress. Quarantine has a separate budget equal to the configured spool budget; exhaustion retains the original active record and returns an error. Payload-preservation, budget and fresh-replay tests passed on Linux; quarantine and spool tests also ran natively on Windows. |
| Existing Windows service | Updated `ControlOneAgent` in place; original executable backed up and configuration hash unchanged. Same Cloudspace enrollment and fresh heartbeat. Candidate SHA-256 `BD9C6AF63711E40C277435F96470075981022C6141663E9D84D8D6E895713476`. Original SHA-256 `DFD8C68960EF72D1E644DAB317CA369312AD1FA408FEA5CFCB6CA4D7C6439306`. No Windows application/OS patch job was dispatched. |
| Native Windows security checks | Journal duplicate/lost acknowledgment handling, interrupted execution, corruption/write failures, capacity, node/mode binding, counter and readiness tests passed natively. These are functional runtime tests, not a host reboot/power-loss or complete ACL/security qualification. |
| Missing raw-log tables | Found `agent_log_dumps` and `agent_log_dump_chunks` absent despite migration version 165. Took a fresh backup, restored that exact dump in isolation, applied existing migration 0154 twice successfully, then applied its non-destructive create/index statements as one live transaction. Both tables and ten indexes exist. Migration ledger was not rewound; no existing data was deleted. Isolated test database removed. The original cause of schema drift remains unproven. |

## Local HTTPS choice and limits

Chosen Windows-side qualification endpoint: **`https://localhost:9443`**. The Ubuntu lab uses Docker alias **`https://phase0-tls-proxy:9443`**. Certificate hostname verification and TLS 1.3 passed. Explicit trust succeeds; wrong hostname and an untrusted certificate both fail with curl exit code 60. No insecure certificate-validation switch or machine-wide root import was used.

The lab agent now uses HTTPS with the qualification certificate appended to its existing CA bundle. The nginx endpoint is managed by the existing lab compose file, with an internal agent network, a separate host-connectivity network, restart policy `unless-stopped`, a read-only certificate mount and a port bound only to `127.0.0.1:9443`. Start the local setup with:

```powershell
docker compose -f compose.patch-posture-lab.yml --profile local-tls up -d
```

The certificate expires **20 November 2026 at 08:59:12 Africa/Lagos**. Its private key is local under the ignored evidence directory; do not commit or share it. This endpoint qualifies the lab's TLS hop. The internal reverse-proxy-to-control-plane hop remains HTTP. The existing Windows agent configuration still uses its prior loopback API address; the browser UI remains on port 4173. Fleet-wide HTTPS, production PKI ownership, renewal and policy-signing trust are not closed by this experiment. `localhost` does not provide a reachable endpoint for remote client machines.

## Repeat the visible checks

1. Select **Local Development → Patch posture**. The two recent completed deployments should display **auto (proxy)**.
2. Open **Per-node** for deployment prefix **2c791a29**. Confirm **APPLIED**, **PKGS 2**, and the log lines replacing the two audit packages. **1/1** counts applied nodes; **2** counts packages on the selected node.
3. Open **Servers → phase0-ubuntu-lab → Packages**. Disable **Hide system packages** and filter `libaudit`. Verify both new dpkg versions. Repeating an upgrade when those packages are already current should legitimately change zero packages.
4. Open the same node's **Overview**. Confirm the candidate version and a current heartbeat. Missing file-access/journald prerequisites remain **unsupported** with coverage visibly incomplete. The container has no real authentication journal or supported file-access backend; it has not been made into a full host by changing a status label.
5. Open **Servers → Cloudspace → Overview**. Confirm the same node identity and candidate Windows version with a current heartbeat. The existing service, installation path and enrollment were reused.
6. For another real upgrade, first inspect actual available versions and simulate the package-manager transaction on the disposable target. Create a recovery checkpoint, explicitly select only that target and the reviewed package allowlist, retain the rescan checkbox, inspect the plan and follow the approval flow. The temporary repository proxy is stopped after this run, and this lab's original **direct** mode has been restored; prepare and validate an appropriate repository path before a future installation. Do not select all tenant nodes for a demonstration.

## Recovery and retained artifacts

- Evidence directory: `tmp/local/patch-posture-demo/verification-run/`, including `counter-verified.png`, `completion.json`, `package-changes.json`, native test results, `live-stream-verification.json`, `ingest-recovery.json`, TLS checks and `schema-reconciliation.json`.
- Pre-upgrade lab image: `controlone-phase0-patch-lab:20261020-before-counter-verification`. Latest enrolled recovery image: `controlone-phase0-patch-lab:20261021-telemetry-qualification`. These images contain enrollment credentials; keep them private and never run duplicate enrolled identities concurrently.
- Windows original executable: `C:\Program Files\Control One\ControlOneAgent\controlone-agent.exe.before-phase1-20261021`. Original configuration backup: `C:\ProgramData\ControlOne\nodeagent.yaml.before-phase1-20261021.bak`. The administrator update verified candidate and backup hashes, preserved the config and included rollback on installation failure. A later manual rollback requires an administrator to stop the existing service, restore the saved executable and restart that same service; preserve the journal and quarantined evidence. Restoring old code also restores its known defects.
- Windows quarantine: `C:\ProgramData\ControlOne\nodeagent\event-spool\quarantine`. Moves retained payloads and original access controls. Do not put expired files back into the active queue without reviewing their timestamps and replay policy. They were not silently accepted as current security events.
- Lab TLS rollback copies: `/etc/control-one/nodeagent-before-tls.yaml` and `/var/lib/control-one/nodeagent/certs/ca-before-tls.crt`. Restore the appropriate config/trust files and restart the same lab service when reverting transport; preserve node state and the patch execution journal.
- Database backup: `tmp/local/patch-posture-demo/recovery/controlone-pre-schema-reconcile-20261021.dump`, with hash and isolated-restore evidence in `schema-reconciliation.json`. Do not use a schema down migration to remove newly reconciled tables after data has been captured; use a separately reviewed recovery decision.

## Current checklist and closure rule

This is the current consolidated checklist. Checkboxes in the older baseline document describe earlier checkpoints and are historical; use this document for current status.

### Completed evidence

- [x] Identify the Local Development tenant, Windows enrollment and disposable Ubuntu lab; capture inventory and connectivity evidence.
- [x] Back up local data and verify isolated PostgreSQL restores, including the final post-repair checkpoint.
- [x] Complete the 23 October encrypted local recovery drill: all 44 file hashes, SQLite integrity, PostgreSQL restore from the decrypted archive, wrong-passphrase rejection and disposable database/plaintext-stage cleanup pass. Restored counts: 36 tenants, 26 nodes, 5 deployments and 4 finding records. The external-copy/startup gate remains open; see the three-area report.
- [x] Demonstrate three bounded real Ubuntu package upgrades, approval/completion reporting and refreshed inventory; verify the corrected count, including `liblzma5` **PKGS 1** on 23 October.
- [x] Update the existing Windows service in place, preserve its enrollment/configuration and pass focused native runtime tests.
- [x] Verify agent stream isolation, stream keepalive and local lab TLS positive/negative checks.
- [x] Recover current Windows telemetry ingestion while retaining expired evidence in bounded quarantine; expose incomplete collector coverage accurately.
- [x] Record candidate binary hashes, test results, recovery artifacts and source-review snapshot. Reproducible, signed production provenance remains unqualified.
- [x] Test policy-key/cache and installer signature guards, deploy qualified Linux/server candidates and independently verify the pinned release before installation and the live signed policy cache. Production/cross-platform trust remains open.
- [x] Add three selected Ubuntu advisories and verify real historical finding resolution after the app-approved `liblzma5` upgrade; platform matching and conservative reconciliation regressions pass.
- [x] Restore the existing Ubuntu lab's network connection, verify its current heartbeat in the app and maintain the existing local launcher recovery block. Full launcher recreation qualification remains separate.

### Remaining Phase 0 evidence groups

The four groups below summarize the remaining evidence work; completing an activity does not automatically close Phase 0. Each group needs recorded acceptance results for the intended deployment scope, with an owner and explicit disposition for any remaining gap. Phase 0 closure must also reconcile the original review/checklist, including build provenance, stale fleet inventory, quarantined-event ownership/retention and unexplained schema drift. An exclusion must narrow the supported scope explicitly; an untested capability must not be marked passed. Implementation/security work discovered during qualification remains tracked in its relevant phase.

- [ ] Off-machine recovery copy and retrieval test. Only the C: drive and no mapped network destination were found. A local restore-tested copy exists, but there is no configured external backup destination in this workflow. Do not publish credential-bearing images/dumps to an arbitrary destination.
- [ ] Production-reachable HTTPS hostname, managed trust/renewal, end-to-end transport and signed policy/binary release qualification. The chosen localhost endpoint is a local test endpoint.
- [ ] Supported VM/host qualification for authentication logs, file-access monitoring, hardening, kernel/reboot and recovery behavior. Minimal-container coverage remains partial.
- [ ] Comprehensive maintained vulnerability coverage, upstream refresh and persisted per-node assessment completeness. Visible feed status, scoped Linux matching and one real Linux finding resolution are proven; five selected advisory entries remain limited coverage.

### Separate Phase 1 gate

- [ ] Full Phase 1 failure/concurrency qualification and later dispatch/receipt/rollup recovery work. Native tests, three real Linux upgrades and a Windows service update are useful evidence, not a blanket production-compliance declaration. Phase 0 evidence acceptance does not complete Phase 1 or authorize a wider production rollout.
