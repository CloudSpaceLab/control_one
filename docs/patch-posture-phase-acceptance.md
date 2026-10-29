# Patch posture phases and acceptance checklist

> **Fresh live demonstration, 29 October 2026:** New app-approved deployment b4e48dd8 completed APPLIED / PKGS 0; later assessment 4788a20d produced verification unknown (no security baseline), with promotion blocked. Signed repository refresh, isolated security/reconciliation tests and fresh live-database restoration passed. The temporary proxy ACL was repaired for the changed lab IP; original direct/no-proxy settings were restored. [Exact steps, evidence and pending gates](patch-posture-self-service-recovery-guide.md#fresh-live-workflow-demonstration--29-october-2026). Phase acceptance is unchanged; remaining host/platform/production gates stay open.

> **Current Phase 2 qualification, 28 October 2026:** P2-01 through P2-10 software-contract checks are qualified using the existing Ubuntu lab and isolated tests. [Implementation, test evidence, live demonstration and scope limits](patch-posture-phase2-state-2026-10-27.md#complete-state-contract-qualification--28-october-2026). Full-host reboot/platform, external recovery, managed production trust and production rollout gates remain open in their respective phases. Earlier checkpoints below are historical; use the [canonical acceptance checklist](patch-posture-phase-acceptance.md) for current status.

> **Offline bundle checkpoint, 28 October 2026:** Token eligibility and authenticated downloads are fixed; 15 UI/API tests, targeted backend tests, TypeScript, lint and production build passed. [Dropdown sources, repeatable steps and evidence limits](patch-posture-self-service-recovery-guide.md#offline-bundle-token-eligibility-and-dropdown-sources--28-october-2026). Final live browser recheck passed on 28 October: expired/revoked entries remain disabled after Reload tokens, no old token is selected, and Download bundle is disabled with an empty raw token field. Screenshot: tmp/local/patch-posture-demo/token-picker-20261028/token-picker-final-live.png. No overall phase closure or new credential is claimed. That token-picker checkpoint used controlplane-token-qualified (SHA-256 1761B04CD5F3D3C076614756671FDF091AC361C3B0A42B3D3C89FDFEA38AE86F), with its prior candidate retained for recovery; see the later P2-06 checkpoint for the current runtime selection.

Updated 28 October 2026, Africa/Lagos. This is the authoritative current phase-status and acceptance checklist. Earlier dated checkboxes in the review/demo documents remain historical checkpoints. Checked means the stated, bounded evidence exists; it does not imply every platform or production environment passed. The latest runtime evidence is recorded in the linked dated implementation reports; changing a checkbox alone is not execution evidence.

## Current status and progression rule

| Phase | Current status | Closure boundary |
| --- | --- | --- |
| 0 Establish evidence | Open; local evidence collected | External recovery, managed trust, full-host and assessment coverage evidence remain incomplete |
| 1 Secure execution | Non-VM scope complete; overall phase open | P1-09/P1-10 passed; P1-11 full-host/storage power-loss and P1-12 supported-scope qualification remain |
| 2 Correct state | Software contract complete and locally qualified; P2-01 through P2-10 checked | Existing lab plus isolated native/database/concurrency/UI tests; full-host/platform and production qualification remain in Phases 0/1/3/5 |
| 3 Qualify platforms | Open; bounded Linux proxy and Windows runtime evidence only | Intended platform/mode, network, health, reboot and recovery matrix must pass |
| 4 Operator and governance | Open; selected patch UI improvements verified | Complete recommendation, ownership, policy, deadline and evidence workflow remains |
| 5 Production qualification | Open | Production-scope fault, scale, pilot, recovery and operational acceptance remain |

Phase 2 engineering can proceed while Phase 0/1 checks remain open. This does not mark earlier phases complete, enable scheduled mass patching or authorize a wider production rollout. A later-phase UI improvement can be checked without closing that phase. Shared evidence is referenced across phases rather than treated as a second independent qualification.

An unchecked item is not automatically a VM blocker: the next section separates work that can use the existing disposable lab from work requiring a suitable full-host target. A VM alone also does not establish physical-disk power-loss durability; the test must exercise the storage failure boundary actually claimed.

## Phase 0 — Establish evidence

Acceptance: capture the actual deployment and prove the readiness claims for the intended support scope, with owners and evidence for exclusions.

- [x] P0-01 Identify the Local Development tenant, enrolled Windows machine and disposable Ubuntu container; record deployed versions, inventory and connectivity.
- [x] P0-02 Restore local PostgreSQL checkpoints in isolation and complete the bounded encrypted local recovery drill. These are local recovery mechanics, not off-machine protection.
- [x] P0-03 Record real bounded Ubuntu upgrades, approval/completion results, inventory and scoped historical finding resolution; retain the original failure history.
- [x] P0-04 Verify the defined local TLS, pinned Linux release and policy/cache signature cases; preserve the existing Windows enrollment/configuration during its service update.
- [x] P0-05 Expose expired/partial/unavailable assessment and incomplete collector coverage; retain quarantined telemetry evidence.
- [ ] P0-06 Copy a complete recovery package to an approved destination outside this machine, retrieve it, restore it and verify coordinated application startup. Record destination owner, access, retention and recovery results.
- [ ] P0-07 Qualify production-reachable HTTPS, managed trust distribution/renewal, protected release/policy signing and rotation/recovery ownership across the intended scope.
- [ ] P0-08 Qualify a full Ubuntu host's authentication logs, file monitoring, permissions/hardening, reboot and recovery. The existing minimal container is not equivalent.
- [ ] P0-09 Establish maintained advisory coverage and refresh for the intended fleet, with persisted assessment completeness and authoritative inventory evidence. Selected advisories do not establish comprehensive coverage.
- [ ] P0-10 Reconcile the baseline's release provenance, stale fleet inventory, unexplained schema drift and quarantined-event ownership/retention; document accepted support exclusions explicitly.

Evidence: [consolidated verification](patch-posture-verification-2026-10-21.md), [three-area verification](patch-posture-three-areas-verification-2026-10-23.md), [trust verification](patch-posture-trust-verification-2026-10-23.md).

## Phase 1 — Secure execution

Acceptance: authorized, reviewed work executes without unintended duplicates; uncertain outcomes remain honest and recoverable; the declared failure/support scope is qualified.

### Verified within the recorded scope

- [x] P1-01 Pass the defined tenant-role, node/job ownership, action identity, stale-policy approval and fail-closed safety-read regressions. Approval cannot bypass the independent policy binding.
- [x] P1-02 Commit approval resolution, reviewed plan, job/event and node link atomically. Pass defined concurrent dispatch, wave exclusion, stale/orphan recovery and actual database-connection-loss cases.
- [x] P1-03 Commit terminal state, receipt, plan outcome, deployment rollup and acknowledgment marker atomically; pass defined rollback, concurrent retry and matching legacy-replay cases.
- [x] P1-04 Reject malformed/semantically corrupt execution journals without overwriting evidence; retain reservations and pending completion results across the tested persistence/restart cases. Focused Linux/native Windows tests passed.
- [x] P1-05 Verify signed Linux candidate installation into the existing enrollment, current heartbeat, idle abrupt restart and bounded network reconnection. Preserve configuration and journal history.
- [x] P1-06 Exercise repeated delivery, serialization and interruption with a fixture package manager. This is separate from a real package installation.
- [x] P1-07 Demonstrate a real zlib1g upgrade paused **after dpkg finished but before completion reporting**, with a second reserved request waiting; container SIGKILL/start produced honest failed/unknown results, no automatic rerun and one receipt/marker each.
- [x] P1-08 Inspect actual package health after that interruption; a fresh separately approved request completed APPLIED / PKGS 0, with the upgraded version in inventory. Preserve the interrupted failures.

### Non-VM scope completed — bounded acceptance

- [x] P1-09 Perform a controlled **real mid-install interruption** in a disposable container/isolated package environment, at an observed installation stage. Retain package database/log/journal checkpoints and verify package-manager diagnosis/recovery, honest Control One outcomes, no duplicate rerun, healthy inventory and a fresh reviewed request afterward. Do not count the post-install drill as this test.
- [x] P1-10 Extend local filesystem fault qualification around reservation, result and acknowledgment persistence: relevant disk-full/I/O/read-only/rename-sync failures must not permit an unreserved execution, erase prior evidence or acknowledge an unpersisted result. Record the exercised boundaries; fixture write failure is not proof of every storage fault.

### Full-host and declared-support closure checks

- [ ] P1-11 Qualify abrupt full-host loss and the claimed filesystem/storage durability boundary with a suitable disposable target and recovery procedure. Container process death is insufficient evidence for power-loss guarantees.
- [ ] P1-12 Complete the execution-security portions of the intended supported OS/mode matrix, linked to Phase 3; reject unsupported combinations explicitly. Native Windows fixture tests do not qualify Windows servicing or reboot recovery.

Phase 1 remains open until these unchecked gates pass or an authorized, explicit support-scope restriction removes the corresponding claim. No restriction is assumed merely to close a checkbox.

Evidence: [completed real mid-install and filesystem-fault qualification](patch-posture-phase1-nonvm-2026-10-27.md), [Phase 1 execution/dispatch results](patch-posture-phase1-execution-2026-10-27.md), [real-package record](patch-posture-real-package-demo.md). P1-09 is the observed libbrotli1 mid-unpack case; P1-10 is the defined syscall matrix plus bounded real Linux disk-full/read-only tests. They do not qualify arbitrary storage devices or every package installation stage.

## Phase 2 — Correct assessment and verification state

The declared software contract is implemented and locally qualified: see [complete implementation, tests and live evidence](patch-posture-phase2-state-2026-10-27.md#complete-state-contract-qualification--28-october-2026). P2-06 through P2-10 now have native snapshot, publication, verification, health-gate, recovery and candidate evidence. Unsupported ecosystems, kernel runtime mapping and unavailable full-host health remain explicit unknown and cannot certify a fix. Checking this phase records its state/evidence enforcement contract; it does not claim an actual full-host reboot or production support certification.

Acceptance: trustworthy evidence distinguishes installed, still vulnerable, fixed, removed, unknown and verification pending. An installer exit code alone cannot certify a fix.

- [x] P2-01 Carry forward the defined missing/expired/altered feed rejection, conservative reconciliation regressions and visible assessment-confidence warning. This does not establish the full per-node assessment lifecycle.
- [x] P2-02 Carry forward one scoped Ubuntu advisory detection -> real liblzma5 upgrade -> historical finding-resolution example. This is bounded evidence, not the complete matrix below.
- [x] P2-03 Carry forward the defined transactional dispatch/completion recovery matrix from P1-02/P1-03. No second independent qualification is implied.
- [x] P2-04 Persist assessment provenance: tenant/node, inventory generation and completeness, feed generation/freshness/coverage, supported version semantics, timestamps, outcome and exclusions. Unknown/partial/expired must survive reload and API/UI use.
- [x] P2-05 Qualify native package-version comparisons for the declared ecosystems, including epochs, distro revisions, prereleases and backports where applicable; unsupported comparisons remain explicit unknown.
- [x] P2-06 Qualify generation-based finding reconciliation: fixed, still-vulnerable and removed-package cases; partial inventory, narrower feed coverage and stale/out-of-order scans cannot incorrectly resolve findings. Native isolated-root removal, PostgreSQL atomicity and cross-process publication cases passed.
- [x] P2-07 Persist explicit post-install verification states, tied to the execution and a later authoritative inventory/assessment. Distinguish execution success, verification pending, verified fix and failed/unknown verification. Baseline capture, later assessment transitions, tenant scope and historical unknown migration passed; live records remain correctly blocked.
- [x] P2-08 Require applicable reboot and service-health evidence before verification or rollout promotion. A healthy command exit with an affected version remains unverified. Positive/negative isolated gate cases and live container-health rejection passed; actual full-host reboot/service and kernel mapping qualification remains P3-06.
- [x] P2-09 Qualify crash/race/expiry recovery for the complete assessment-to-verification lifecycle, including duplicate or delayed reports. Database rollback, connection termination, independent-process crash, pool pressure, expiry/revision changes, race detector and local seven-table restore fingerprints passed. Physical power-loss and off-machine recovery remain open elsewhere.
- [x] P2-10 Define and verify the update-candidate contract: installed version, applicable candidate/fixed boundary, vendor/repository and advisory provenance, observation time and coverage. Fresh signed Ubuntu metadata and adversarial fixtures passed; live liblzma5 installed/candidate equality and already-met fixed boundary demonstrated. Other repository paths remain unknown rather than inferred; scheduled discovery and automated recommendations remain later phases.

### P2-06 acceptance breakdown — 28 October 2026

All required P2-06 subchecks now pass for the declared native state contract. These checked rows record local software-contract qualification, not full production or platform acceptance.

- [x] P2-06a Resolve the explicitly covered historical finding at a supported fixed boundary; retain still-vulnerable findings and advisories omitted by a narrower feed.
- [x] P2-06b Block resolution when co-installed versions are unknown, missing, still vulnerable or unmapped, or applicable advisory declarations conflict. Keep independent architecture scopes separate. Nine signed-input scenarios passed.
- [x] P2-06c Retain a missing package's finding when collector completeness is unestablished. Absence is not classified as a verified removal or fix. Seven server outcome scenarios passed, including partial/stale inventory and narrower coverage.
- [x] P2-06d Reject stale/out-of-order generations and incomplete resolution inputs without partial commits. Eight additional PostgreSQL scenarios and the existing injected-failure, terminated-connection, duplicate, delayed and tenant-isolation matrix passed.
- [x] P2-06e Deploy the qualified local candidate and demonstrate the explanation in the built-in browser on the existing Ubuntu lab. Four UI tests, TypeScript, scoped lint and production build passed; lab TLS health passed and configuration/journal hashes stayed unchanged. The live view showed the existing saved assessment, not a new adversarial scan.
- [x] P2-06f Establish authoritative complete-inventory evidence and qualify a genuine removed-package outcome separately from fixed and unknown, including incomplete/empty snapshot handling. Real dpkg isolated-root before/after snapshots were consumed by dedicated PostgreSQL reconciliation; legacy/incomplete/application-outside-scope cases retain findings.
- [x] P2-06g Complete feed-publication concurrency qualification for the shared PostgreSQL/shared-content-root topology so a superseded feed cannot certify resolution. Cross-process database/file leases, cancellation, process crash, divergent-generation rejection and pool pressure passed. Other topologies require separate qualification.

Evidence: [complete state-contract qualification, runtime and live demonstration](patch-posture-phase2-state-2026-10-27.md#complete-state-contract-qualification--28-october-2026). Earlier peer-only checkpoints are historical. Full-host/platform/production gates are still unchecked in their respective phases; Phase 2 does not enable mass rollout.

## Phase 3 — Qualify platforms and installation paths

Acceptance: the declared platform/mode combinations actually enforce repository, dependency, permissions, health, window and reboot controls.

- [x] P3-01 Carry forward bounded real Ubuntu proxy-mode upgrades, pinned Linux-agent checks and Windows in-place service/runtime evidence. These are distinct from a complete matrix.
- [ ] P3-02 Publish the intended OS/package-manager and direct/proxy/offline support matrix; enforce explicit rejection for unsupported combinations before approval.
- [ ] P3-03 Qualify repository provenance and isolation in each mode, including HTTPS/signature failures, dependency changes and offline fallback/source-file exclusion.
- [ ] P3-04 Decide and qualify Windows application updates separately from Windows OS servicing, including required privileges and restart behavior.
- [ ] P3-05 Verify actual maintenance-window/network enforcement, expiry and closure on connected/disconnected targets and after restart; a database status is insufficient.
- [ ] P3-06 Qualify service probes, reboot handling, installation recovery and operator recovery procedures on full-host targets. Share applicable evidence with P1-09/P1-11/P1-12.
- [ ] P3-07 Qualify the proxy lifecycle and readiness/connection controls required for each supported path; incomplete operations must not claim success.

## Phase 4 — Operator workflow, prioritization and governance

Acceptance: operators can understand the reason, exact reviewed scope, risk, authority, timing and verified outcome of each proposed change.

- [x] P4-01 Verify package scope in Approvals, Deployments and Per-node details, including unavailable evidence, exclusions and dependency warning. Requested scope remains distinct from actual PKGS; visible zlib1g / APPLIED / PKGS 0 example recorded.
- [x] P4-02 Verify explicit node-settings Save, required proxy selection, association preservation and load-error blocking; focused UI tests, lint and production build passed (latest 27 tests).
- [ ] P4-03 Make Patch posture, node recommendations and Control Room agree on assessment/verification evidence; zero findings outside known coverage must not imply clean.
- [ ] P4-04 Implement explainable update recommendations showing current/proposed versions, findings addressed, evidence age, dependencies, reboot/health requirements and known limitations.
- [ ] P4-05 Define a tenant-owned urgency policy using severity, exploitation evidence, exposure, asset/service importance, compatibility and applicable deadlines. Explain each priority; newer alone cannot determine urgency. Do not invent universal weights/deadlines.
- [ ] P4-06 Provide owners, due dates, escalation, expiring exceptions, approval history and exportable evidence with access/retention controls.
- [ ] P4-07 Provide explicit automation policies: notify/recommend only, approved scheduled installation, or narrowly authorized automatic installation; define target/package scope, exclusions, maintenance windows, approval requirements and stop conditions. Default scope expansion must not silently enable automation.

## Phase 5 — Production and automatic-promotion qualification

Acceptance: the actual release/support scope passes fault, scale, pilot, recovery and operational review before broad rollout.

- [ ] P5-01 Qualify scheduled inventory/advisory/update discovery, refresh failure handling and recommendation deduplication at intended fleet scale. Display last successful observation and stale/unknown inputs.
- [ ] P5-02 Qualify canary/wave selection, immutable reviewed scope and authorization under the explicit policy. Newly available updates must not silently change an approved plan.
- [ ] P5-03 Qualify automatic promotion only after the required fresh inventory, assessment, health, reboot and observation-period criteria pass. Failed, unknown, stale or unsupported evidence blocks promotion.
- [ ] P5-04 Verify stop/circuit-breaker behavior, operator intervention, recovery and rollback where supported; no unsupported rollback promise or blanket automatic retry after an unknown installation.
- [ ] P5-05 Pass intended fault, scale and soak scenarios; complete a reviewed canary pilot and wider-wave evidence on the actual support matrix.
- [ ] P5-06 Complete external recovery, managed trust and full-host prerequisites from Phase 0; approve operational owners, runbooks, support exclusions and evidence retention.
- [ ] P5-07 Map retained evidence to the agreed customer/control requirements and obtain recorded release acceptance. Passing this checklist does not itself assert standards certification.

## Automatic update workflow and cross-phase gates

| Step | Required evidence/decision | Acceptance reference |
| --- | --- | --- |
| Detect | Fresh inventory, trusted advisory/repository observations, applicable candidate | P0-09, P2-04, P2-10, P5-01 |
| Compare | Supported version semantics and affected/fixed ranges | P2-05, P2-06 |
| Prioritize/recommend | Explainable security/business urgency and visible exact change | P4-04, P4-05 |
| Authorize/schedule | Bound plan and explicit tenant policy; required approval/window | P1-01/P1-02, P3-05, P4-07 |
| Install canary | Qualified path, scope/dependencies and retained execution evidence | Phase 1 and P3-02 through P3-06 |
| Verify | Later authoritative package/assessment plus required health/reboot evidence | P2-07, P2-08 |
| Promote | Defined success/observation criteria and circuit breakers; uncertainty blocks | P5-02 through P5-04 |

Automatic discovery/recommendation, automatic installation and automatic promotion are separate capabilities. Existing severity/exploitation fields, an upgrade scan or wave API do not prove this whole pipeline is implemented or qualified. No automatic policy was enabled by this plan update. The defined P1-09/P1-10 track is complete. Engineering has moved into Phase 2's assessment/verification contract; P1-11/P1-12 remain separate acceptance obligations.

## How to check an item

Keep an item unchecked until its stated case passes. Record source/deployed versions, date, exact target/support scope, test or demonstration, observed result, artifact location, cleanup and limitations. Attach evidence to this checklist through the dated trackers, then change only the matching checkbox. Failed or blocked tests remain visible. A design, successful build, old result or simulated command cannot substitute for the requested real-host scenario.

Suggested accountable functions remain those in the [original delivery plan](patch-posture-end-to-end-review.md#delivery-plan-and-acceptance-gates); individual operational owners and rollout decisions still need explicit assignment. The [operator guide](patch-posture-self-service-recovery-guide.md) is for normal reviewed verification, not a production fault-injection procedure.
## Documentation and evidence audit — 27 October 2026

Reconciled this tracker with the original delivery table, consolidated verification summary and dated Phase 1 records. All eight related operator/evidence documents link to this checklist. Acceptance IDs are unique; relative document targets and references to this checklist's anchors resolve. Thirteen selected Phase 1/latest UI artifacts were checked present, and the retained passing dispatch and 27-test UI/build results were inspected. This was a documentation/evidence audit, not a new runtime fault-injection or package-installation run. No unchecked gate was closed merely because its test can run without a VM. No automation was enabled, new .ps1 created, commit made or push performed.

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
