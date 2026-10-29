# Repeat the post-recovery patch verification

> **Current Phase 2 qualification, 28 October 2026:** P2-01 through P2-10 software-contract checks are qualified using the existing Ubuntu lab and isolated tests. [Implementation, test evidence, live demonstration and scope limits](patch-posture-phase2-state-2026-10-27.md#complete-state-contract-qualification--28-october-2026). Full-host reboot/platform, external recovery, managed production trust and production rollout gates remain open in their respective phases. Earlier checkpoints below are historical; use the [canonical acceptance checklist](patch-posture-phase-acceptance.md) for current status.

See [the offline bundle token and dropdown guide](#offline-bundle-token-eligibility-and-dropdown-sources--28-october-2026) for the 28 October picker/download fix and its verification limits.

> **Current phase acceptance, 27 October 2026:** [The six-phase checklist](patch-posture-phase-acceptance.md) records P1-09/P1-10 as passed within the [real mid-install and storage-fault scope](patch-posture-phase1-nonvm-2026-10-27.md). The non-VM Phase 1 work is complete; overall Phase 1 remains open for P1-11/P1-12 full-host/storage and support-matrix qualification. [Phase 2 engineering has started](patch-posture-phase2-state-2026-10-27.md). Phase 0 and the remaining production gates stay open. Earlier dated unchecked rows below are historical checkpoints, not competing current closure claims.

Use the existing **Local Development** tenant and **phase0-ubuntu-lab**, node `d033fa85-5796-48ae-8e8a-f82706387326`. This guide creates a fresh reviewed request after inspecting an interrupted outcome. It does not require a new PowerShell script or re-enrollment.

## 1. Verify the machine

Open Servers > phase0-ubuntu-lab. Confirm the tenant, full node ID, CONNECTED / HEARTBEAT CURRENT and advancing Last seen after Refresh. Open Packages and filter `zlib1g`.

The recorded upgraded version was `1:1.3.dfsg-3.1ubuntu2.3`; this is historical evidence, not a claim about today's newest candidate.

In Windows PowerShell, independently check the existing lab:

```powershell
$dockerExe = Join-Path $env:LOCALAPPDATA 'Programs\DockerDesktop\resources\bin\docker.exe'
& $dockerExe exec controlone-phase0-enrolled-lab-20261020 dpkg-query -W zlib1g
& $dockerExe exec controlone-phase0-enrolled-lab-20261020 dpkg --audit
```

The version should agree with fresh app inventory. `dpkg --audit` should succeed without output. Investigate errors before requesting more work. An interrupted completion can be unknown even after a package installed; retain the failed request and execution journal rather than changing its status or deleting history.

## 2. Prepare the lab repository connection

Cleanup stopped the existing repository proxy. Start it and refresh metadata with strict error handling:

```powershell
& $dockerExe start controlone-phase0-repo-proxy-20261020
& $dockerExe exec controlone-phase0-enrolled-lab-20261020 apt-get -o Acquire::https::Proxy=http://phase0-repo-proxy:3128 -o Acquire::Retries=0 -o Acquire::https::Timeout=20 -o APT::Update::Error-Mode=any update -qq
& $dockerExe exec controlone-phase0-enrolled-lab-20261020 apt-cache policy zlib1g
& $dockerExe exec controlone-phase0-enrolled-lab-20261020 apt-get --simulate install --only-upgrade zlib1g
```

Require a successful refresh without failed-index warnings. Review Installed, Candidate and simulated dependency changes. Equal versions should need zero upgrades; a newer candidate can cause another real upgrade. Resolve connectivity/signature errors without bypassing verification.

In Patch posture > an existing lab deployment > Per-node > Config, choose **proxy**, select **phase0-repo-proxy:3128**, then **Save patch settings**. Close/reopen Config to verify. Mode selection alone changes only the draft.

## 3. Submit a fresh scoped request

Open Patch posture > Deploy patches. Set:

| Field | Value |
| --- | --- |
| Mode | auto (per-node config) |
| Reason | Post-recovery verification - zlib1g only - your date/time |
| Package allowlist | zlib1g |
| Refresh package inventory after patching | Checked |
| Selected machine | Only phase0-ubuntu-lab |

Confirm **Deploy to 1 node**, then submit. An empty allowlist means all eligible packages; keep this demonstration explicitly scoped. Dependencies may also change.

## 4. Review and approve

Open Approvals. Identify your new request by time/deployment ID and check the node, mode and **Package scope**. For this example, scope should show `zlib1g`. **All eligible packages** means the allowlist is empty, not a single-package request. **Package scope unavailable** means the evidence could not be displayed; inspect the deployment before proceeding. Review any exclusions and dependency warning.

Click Approve once after confirming the intended scope. If approval fails, inspect that request's error rather than creating duplicates or bypassing approval. The server independently verifies the approval-bound policy before dispatch; visible package names do not replace that check.

## 5. Verify the result

Open Deployments > your request > Per-node. Allow several minutes for execution and heartbeat reporting. Check:

| Field | Meaning |
| --- | --- |
| STATUS | APPLIED means successful normal completion |
| PKGS | Number actually upgraded; zero is valid when already current |
| ERROR / LOG TAIL | Inspect any failure or available output |

Our fresh request `f3fb9aca-8698-42b7-9275-b9ec32d2ce18` completed with **APPLIED / PKGS 0**: the interrupted first request had already installed the new zlib version. This verified normal recovery without claiming another upgrade. The old interrupted requests remained failed/unknown.

Return to the node's Packages tab, filter zlib1g and compare with `dpkg-query` again. Verify current heartbeat and healthy `dpkg --audit`. Inventory Refresh reloads reported inventory; it does not itself prove a new scan has finished.

If pending/in progress persists, check heartbeat, approval, repository connectivity and active package operations before creating another request. If failed, inspect ERROR / LOG TAIL and host health before deciding on a fresh reviewed request.

## 6. Finish

Once terminal and no package operation needs the proxy, restore Config to **direct / No proxy selected**, then Save. Preserve any existing maintenance-window association; the recorded lab value was empty. Stop the temporary proxy:

```powershell
& $dockerExe stop controlone-phase0-repo-proxy-20261020
```

Record deployment/node IDs, before/after versions, package count and time. Preserve the latest execution journal; restoring an old journal can discard reservations and allow duplicate execution.

The prior interruption drill used a temporary post-install pause and SIGKILL/start only in the disposable container after dpkg completed. It is not part of this normal workflow. Do not downgrade packages or kill a client host to reproduce it. Full-host/power-loss/platform checks and the app's expired vulnerability-feed issue remain separate gates. See [Phase 1 evidence](patch-posture-phase1-execution-2026-10-27.md).
## Live approval package-scope demonstration — 27 October 2026

Browser control became available, superseding the earlier browser-settings blocker for this checkpoint. Visibly submitted a new Local Development request with only phase0-ubuntu-lab selected, explicit zlib1g allowlist and inventory refresh enabled. The app opened Approvals and showed deployment fd65e7d2, node d033fa85, direct mode, Package scope zlib1g and the required-dependencies warning. Requested 27 October 2026 20:17:33 Africa/Lagos; expires 28 October 2026 20:17:33.

The request was deliberately left pending/unapproved for the operator to inspect. No package execution or completion is claimed for this display demonstration; do not approve it solely to clear the queue. Its direct mode reflects the restored node configuration; any later execution needs the repository/preflight checks in the self-service guide. Screenshot: tmp/local/patch-posture-demo/phase1-20261027/approval-package-scope-live.png. The app remains open on Approvals. This closes the visible package-name walkthrough only; other Phase 0/1 gates are unchanged.
## Deployment and per-node package scope — 27 October 2026

Deployments now includes Package scope from the recorded deployment summary. Per-node details repeats Requested package scope above the actual outcome table and explains that PKGS counts actual upgrades and can legitimately be zero. Explicit empty/null allowlists mean All eligible packages; absent or malformed allowlist/denylist evidence displays Package scope unavailable rather than inferring a clean or broad result. Exclusions and the dependency-change warning are retained. The displayed scope is the requested selection, not a claim that every named package upgraded or a list of resolved vulnerabilities.

Visibly demonstrated the existing lab requests in the built-in browser: the list shows zlib1g, liblzma5 and earlier scoped packages; completed recovery request f3fb9aca shows requested zlib1g beside APPLIED / PKGS 0. The existing fd65e7d2 display-demo request remains pending/unapproved. No additional patch request, package execution, node configuration change or backend rollout was needed. Evidence: tmp/local/patch-posture-demo/phase1-20261027/deployment-package-scope-live.png and per-node-package-scope-live.png. Proxies and maintenance Windows remain connection/schedule views, not package execution records. Validation results follow below; Phase 0/1 host/platform/operational gates remain unchanged.

Final deployment-scope validation: all 27 patch UI tests pass, focused ESLint passes, TypeScript and the Vite production build pass (2,794 modules; patch chunk 46.18 kB / 11.45 kB gzip). The initial build caught test-only typing errors (Testing Library role options and fixture narrowing); these were corrected and the full focused validation rerun passed. Evidence: deployment-scope-tests-final.txt, deployment-scope-lint-final.txt and deployment-scope-build-final.txt under tmp/local/patch-posture-demo/phase1-20261027/. Refreshed the app after the successful build and visibly verified requested zlib1g beside APPLIED / PKGS 0; final screenshot retained. The app remains open on that detail view. No new package execution, .ps1 file, backend restart, commit or push was made.
## Non-VM Phase 1 completed; Phase 2 started — 27 October 2026

P1-09/P1-10 passed within their recorded scope: actual libbrotli1 half-installed interruption, honest failed/unknown result without automatic rerun, explicit operator package repair, fresh app-approved APPLIED / PKGS 0 verification and current inventory; plus shared persistence-failure regressions, Unix directory sync, native Windows journal tests, and bounded real Linux disk-full/read-only tests. The new signed agent is installed on the same Ubuntu enrollment, original config is unchanged, temporary proxy/config/observer are restored/removed, and the latest private recovery image preserves all eight acknowledged journal entries. No .ps1 or Windows service replacement, commit or push was made.

See [the detailed non-VM evidence and exact limits](patch-posture-phase1-nonvm-2026-10-27.md). Overall Phase 1 is still open for P1-11/P1-12; do not mark full-host power loss or the full OS/mode matrix passed. [Phase 2 work](patch-posture-phase2-state-2026-10-27.md) has begun with reproduced unsafe version-comparison fallbacks and conservative corrections; its full native-version, persisted provenance, reconciliation and verified-remediation contract remains open. No automatic rollout policy was enabled.

## Restart and Phase 2 checkpoint — 28 October 2026

The host restarted during Phase 2 qualification. The existing launcher now preserves the optional candidate Compose override and waits for candidate API readiness before refreshing the lab TLS upstream. No new PowerShell script was created. The launcher was exercised successfully; UI and API returned HTTP 200 and the enrolled lab reached `/healthz` with its pinned CA. This does not configure automatic Windows startup.

The qualified Phase 2 binary is deployed locally: `tmp/local/patch-posture-demo/phase2-20261028/controlplane-version-qualified`, SHA-256 `3364FFA9C2237C05CDF59FE5844101A8A7C4B896663B5EF0095DE45091F80CA1`. The offline-bundle suite and targeted server vulnerability, bundle, fixed-boundary and approval-scope regressions passed after restart. An obsolete unsigned-feed test fixture initially failed; it now imports an ephemeral-key signed bundle through the real verifier. Production signature enforcement was preserved. Evidence: `offlinebundle-after-restart.txt` and `server-regressions-final.txt` in that directory.

Live app verification selected **Local Development → Servers → phase0-ubuntu-lab → Packages**, filtered `libbrotli1`, and showed `1.1.0-2ubuntu0.1` with a current heartbeat. Configuration and eight-entry acknowledged journal hashes were unchanged; `dpkg --audit` was empty and the temporary interruption hook remained absent. No package installation was repeated. Screenshot: `tmp/local/patch-posture-demo/phase2-20261028/inventory-after-restart-live.png`.

Phase 1 non-VM acceptance remains complete (P1-09/P1-10); overall Phase 1 remains open for P1-11/P1-12. This unplanned restart is continuity evidence, not a controlled physical power-loss test. Phase 2 has a qualified first comparison-safety fix; full native ecosystem ordering, persisted assessment provenance, reconciliation and verified remediation remain open. An expired feed must not be presented as a clean assessment.

The local launcher `scripts/local.ps1` is currently ignored by Git. Its restart fix exists and was tested locally, but must be explicitly included if a later authorized change is packaged for review; no staging, commit or push was performed here. Candidate binaries, enrolled images, credentials and private evidence must not be published.

## Phase 2 assessment checkpoint — 28 October 2026

P2-04 saved assessment evidence and P2-05 the declared Debian/RPM/SemVer comparison matrix are qualified in the local scope. The live app preserves expired assessment evidence across application restart; native differential tests passed 800 comparisons. P2-06 through P2-10 remain open. See [implementation, limitations and evidence](patch-posture-phase2-state-2026-10-27.md#assessment-persistence-and-native-comparisons--28-october-2026) and [the authoritative checklist](patch-posture-phase-acceptance.md).

## Current reconciliation checkpoint — 28 October 2026

Selected signed feeds are now refreshed to sequence 2; the live lab assessment is **partial**, superseding its earlier expired attempt. Atomic finding/assessment updates, stale-input rejection and failure/concurrency tests passed locally. P2-06 through P2-10 remain open; no overall Phase 2 completion is claimed. See [the latest implementation evidence](patch-posture-phase2-state-2026-10-27.md#atomic-reconciliation-and-refreshed-feed-checkpoint--28-october-2026).

Final live attempt after the coverage-count correction: `44409478-9ff2-4525-a813-685d305d7fc0` at 09:27:59 Africa/Lagos, **partial**, 109 inputs, 106 distinct products outside the selected feed mappings and two validated feeds. The three mapped package inputs do not establish complete advisory coverage for those products. Configuration/journal hashes stayed unchanged and package audit passed. Final screenshot: `tmp/local/patch-posture-demo/phase2-20261028/assessment-final-qualified-live.png`; qualification summary is adjacent.

## Offline bundle token eligibility and dropdown sources — 28 October 2026

The Offline agent bundle picker now labels and disables expired, revoked, fully used or invalid-metadata tokens. It selects only an eligible entry, updates eligibility as tokens expire while the page is open, and rechecks expiry on submission. No raw value means Download remains disabled. Tenant changes and failed reloads clear stale token selections. Historical records are retained. The list loads the latest 50 records; an empty eligible list is not proof that every older record is unusable.

### Where the options come from

| Control or entry | Source |
| --- | --- |
| Enrollment token | Stored token metadata for the selected tenant, loaded by listEnrollmentTokens; each record becomes an option using its name and unique ID |
| command-install-linux/windows plus timestamp | Enroll command-install token generation names a new record using the selected OS and Date.now(); this flow requests a 24-hour lifetime and one-node limit |
| re-enroll plus machine name | Repair / re-enroll token generation creates a new named record with a 24-hour lifetime and one-node limit |
| Other descriptive token names | The name provided when that token was created through the UI/API |
| Operating system | Fixed OS_OPTIONS in ui/src/pages/OfflineBundle.tsx: Linux, macOS and Windows |
| Architecture | Fixed ARCH_OPTIONS in the same page: amd64/x86_64 and arm64/aarch64 |
| Tenant selector | Tenant records loaded by TenantProvider; selection scopes the token list |

Opening a dropdown or clicking Reload tokens does not generate a credential. Repeated generation creates separate records, even if names look similar. The token-list API returns metadata rather than recoverable secrets; the raw credential is returned only when generated.

### Repeat the download yourself

1. Select the intended tenant. Open Enroll and select the target operating system.
2. Generate a token only when onboarding is intended. Keep its raw value private; record its expiry and node limit.
3. Open Offline bundle and click Reload tokens. Select the matching usable entry.
4. Paste its corresponding raw value if the picker has no secret. If using a different fresh raw token, clear an obsolete selection first.
5. Select the target machine's OS and architecture; these are not automatic detection of the browser's machine.
6. Click Download bundle. The page sends the signed-in user's authorization as well as the enrollment credential, saves the returned bundle and displays server errors rather than a premature success message.
7. Transfer/install only on the intended authorized target, using the existing bundled installer and signature checks.

Bundle downloads retain normal user authentication. The backend now rejects a token at or above its node limit (zero remains unlimited), as well as expired/revoked credentials. A rejected enrollment credential does not by itself log a valid user out. Installer scripts and existing enrollment were not changed.

### Verification and scope

- [x] Fifteen focused UI/API tests passed, covering eligibility, unlimited limits, expiry timer/submission races, download authorization, errors and session preservation.
- [x] Targeted backend bundle tests passed, including capacity boundaries and rejection without user authentication.
- [x] TypeScript, focused lint and production build passed. Lint reports the existing legacy-configuration deprecation warning.
- [x] Existing launcher deployed the qualified controlplane candidate. SHA-256: 1761B04CD5F3D3C076614756671FDF091AC361C3B0A42B3D3C89FDFEA38AE86F.
- [x] Earlier live app inspection showed expired/revoked options disabled, no expired auto-selection and guidance for obtaining a fresh token.
- [x] Final visible browser check passed on 28 October after browser access recovered: opened the token picker, confirmed expired/revoked entries disabled, reloaded metadata and confirmed Download bundle disabled with an empty raw token field. Evidence: token-picker-final-live.png.
- [ ] A fresh-credential successful download has not been demonstrated live during this fix; successful and rejected downloads are covered by isolated tests.

No new token, package upgrade, Windows service replacement, PS1, commit or push was created for this fix. Initial test-fixture failures were corrected by authenticating the protected route and correcting test types; production authentication was not weakened. Private evidence is under tmp/local/patch-posture-demo/token-picker-20261028/, including the qualified test/build logs and the earlier token-picker-fixed-live.png screenshot. This checkpoint does not close Phase 0, Phase 1 or the remaining Phase 2 acceptance gates.


## Repeat the Phase 2 reconciliation evidence demonstration

1. Select Local Development and open Servers. Expand its group and select phase0-ubuntu-lab, node d033fa85-5796-48ae-8e8a-f82706387326.
2. Confirm CONNECTED / HEARTBEAT CURRENT, then select Packages.
3. Read Assessment evidence before interpreting any zero count. The recorded example is partial, with completeness unknown; 0 known matches is not a clean-host certificate.
4. Expand Evidence and limitations. Compare the saved attempt time/ID, inventory generation, feed generation, exclusions and feed expiry. Read the warning about absent packages and unknown co-installed versions.
5. Filter Installed packages for liblzma5. The bounded demonstration shows 5.6.1+really5.4.5-1ubuntu0.4; compare it with the advisory's applicable fixed boundary using the supported package version semantics, not ordinary text ordering.
6. Keep these views separate: an installed version, a historical finding and execution success are different evidence. This demonstration reads the saved assessment; Refresh inventory reads the latest stored inventory and does not prove a fresh scan or host removal.
7. If a package is missing or an unknown peer exists, retain the finding and investigate inventory completeness/version provenance. Do not manually mark it fixed to make the dashboard green. A trusted complete removal contract and post-install verification are still pending acceptance items.

The adversarial signed-feed and rollback cases were run in isolated fixtures and the dedicated disposable test database. They were not seeded into the live tenant or simulated by uninstalling a package from this enrolled lab. Screenshot: tmp/local/patch-posture-demo/phase2-reconciliation-20261028/reconciliation-limits-live.png.
