# Local Development patch posture demonstration

> **Repeat live operator demonstration, 2 November 2026:** demonstrated the real saved package/canary/window scope, unsaved scheduled-mode/UTC examples, failed host readiness and disabled activation. Reopened the earlier real CA-package deployment and matching installed inventory, while showing its no-security-baseline/unknown verification and blocked promotion. Saved policy revision 7 remained unchanged; no new installation or schedule was created. [Repeatable steps and exact evidence limits](patch-posture-phase4-automation-2026-11-02.md#repeatable-operator-demonstration).

> **Scheduled execution engineering, 2 November 2026:** durable exact-plan scheduling, individual canary authority, narrow automatic follower gates, current host readiness, policy withdrawal/stop controls and retained execution receipts are implemented, tested in isolation and deployed locally for readiness verification. The live Docker readiness check reports unknown service health and incomplete service inventory; installation remains disabled. [Procedure, passing conditions, test evidence and current limits](patch-posture-phase4-automation-2026-11-02.md). Earlier draft-only statements below are historical; full-host/production qualification and overall P4-07 remain open.

> **Phase 4 operational controls, 2 November 2026:** stable queue/history pagination, complete retained-review snapshot exports, guarded archive-before-purge retention, explicit in-app notification recipients/deduplication and scoped disabled scheduled/automatic policy drafts are implemented. Dedicated database/API regressions and 53 affected/shared UI tests passed, plus TypeScript, scoped lint and production build. See the [operator procedure, live evidence and limits](patch-posture-phase4-operations-2026-11-02.md). Installation remains recommendation-only; saving a draft does not qualify service/reboot outcomes, activate an executor or complete Phase 5. VM/full-host and external recovery/trust gates remain open.

> **Recommendation-only governance, 2 November 2026:** tenant priority rules, reviewed owners/deadlines, expiring context/exceptions, escalation queue and scoped evidence export are implemented in the existing lab. Forty UI checks and sequential Go/API/storage regressions passed; the live demo created review records and no new patch-installation jobs. Lab policy examples are not a production policy. That initial bounded checkpoint is historical; the operational-controls follow-up above qualifies pagination/export, retention and in-app escalation. Full Phase 4 remains open for complete support/health and active automation acceptance. [Walkthrough and limits](patch-posture-phase4-governance-2026-11-02.md).

> **Exact online plans, 2 November 2026:** bounded direct/proxy Ubuntu APT preparation, acquired exact versions, approval/expiry binding and install-time drift checks are implemented. Security/database/UI tests and an isolated real proxy upgrade passed. A separate live app canary (896236db) then completed the exact ca-certificates upgrade through the deployed authorization hook: APPLIED, PKGS 1, clean audit/TLS and fresh inventory. Verification remains unknown (no security baseline), with promotion blocked. Original lab sources/configuration were preserved; one authorized lab package changed. Full Phase 3/4 and production rollout remain open. [Walkthrough and evidence](patch-posture-exact-online-plans-2026-11-02.md).

> **Phase 4 exact offline review, 30 October 2026:** newly prepared format-3 plans bind exact before/after package versions, requested/dependency scope and a one-hour expiry into the manifest and approval identity. Approval deadlines cannot outlive the plan; expired or changed plans reject before installation. Deployment, Approvals and per-node history share the review, with reboot impact unknown and service review required. Legacy format-2 plans remain explicitly labelled without these new guarantees. Signed native tests and a real network-disabled reinstall passed; the enrolled lab package/configuration baseline stayed unchanged. P4-04b is bounded to the existing Ubuntu offline path; direct/proxy exact planning, tenant urgency/governance/automation, full-host qualification and full Phase 4 remain open. [Walkthrough and evidence](patch-posture-phase3-platforms-2026-10-29.md#phase-4-exact-offline-plan-review---30-october-2026).

> **Phase 4 update explanations, 30 October 2026:** bounded P4-04a now provides server-derived, read-only candidate reviews in node Packages and Recommendations: installed/candidate versions, selected findings addressed, evidence age/expiry, explicit dependency/reboot/health limitations and required operator steps. Expired, unverified or superseded evidence cannot retain a positive review. Live Local Development showed a newer ca-certificates candidate without a supported security-fix claim, and base-passwd requiring more evidence. Go regressions, 14 UI tests, TypeScript, scoped lint and production build passed. Full P4-04/Phase 4 and Phase 3 production/full-host acceptance remain open. [Evidence and repeatable walkthrough](patch-posture-phase3-platforms-2026-10-29.md#phase-4-read-only-update-explanations---30-october-2026).

> **Proxy status evidence correction, 30 October 2026:** the proxy list now derives its visible status from matching host-policy/job/receipt/proof evidence. Legacy saved installing without a managed job is Legacy / unverified; a new binding without an action is Registered / unverified. Pending requests say Awaiting target result, never imply installation has started. Recorded verification is historical, not continuous health. Real isolated Docker/Squid source/destination/TLS/redirect tests passed again. Full-host/systemd/reboot/power-loss and production acceptance remain open.

> **Repository probe follow-up, 30 October 2026:** proxy readiness now requires HTTP 2xx from the exact reviewed HTTPS URL. Redirects are rejected without following them; login/redirect responses cannot certify readiness. Agent regressions, the production probe through real network-isolated Squid, and five management-dialog tests passed. The existing bound container remains registered/unverified with lifecycle actions unavailable because it lacks systemd. P3-07d/full-host and production gates remain open.

> **Managed proxy workflow, 30 October 2026:** the bounded Ubuntu 24.04 amd64/systemd software path now includes immutable host binding, reviewed policy revisions, explicit client CIDRs, HTTPS repository probes, managed install/reconfigure/remove, transactional receipts and timeout/concurrency guards. Agent/API/database tests, real isolated Squid ACL/TLS tests, 32 UI tests, TypeScript, lint, production UI build and Windows cross-compilation passed. The live lab registration is bound at revision 1 but remains registered/unverified with no lifecycle job: the existing container has no systemd. P3-07g is checked for bounded software evidence; P3-07d/full-host and production rollout acceptance remain open. VM deferral is unchanged. Earlier gate-only/foundation walkthroughs are historical; use the Manage workflow documented below.

> **Proxy durability foundation, 30 October 2026:** bounded P3-07f adds durable agent reservations/results, no replay after restart, a shared patch/proxy mutation lock, expiring target-bound request validation, transactional database job creation/deduplication and authenticated receipt read-back before acknowledgment. Agent, server, dedicated PostgreSQL and existing dispatch regressions passed. Heartbeat delivery reads durable jobs behind an explicit capability gate; public lifecycle actions remain blocked and the current agent does not advertise that capability. Proxy health is not inferred from receipts. P3-07d and overall Phase 3 remain open for public endpoint integration, remote-client ACLs, actual lifecycle/upstream and full-host qualification. VM work remains deferred.

> **Proxy lifecycle safety, 30 October 2026:** proxy registration now records registered status and explicitly saves connection details only. Unqualified install/reconfigure/removal and generic lifecycle job creation reject; legacy workers return an error instead of claiming execution success. Live removal rejection preserved the existing proxy. Bounded P3-07e is checked; P3-07/P3-07d remain open for actual durable lifecycle implementation, remote ACLs, concurrency, upstream and host qualification. VM deferral is unchanged.

> **Management overview navigation, 30 October 2026:** select a tenant, open sidebar Patch posture, then click View management overview beside Refresh. Use Open source view on the detail page to return. The link is available in the selected-tenant operational view; phase acceptance and deferred VM gates are unchanged.

> **VM-dependent work deferred by user, 30 October 2026:** full-host isolation, interruption during installation, power-loss/reboot and host recovery qualification are on hold because the local VM is currently unavailable. Existing acceptance boxes remain unchecked; Phase 3 and production sign-off remain open. Work continues on the existing enrolled lab and isolated tests, beginning with assessment consistency under P4-03. No VM preparation or Windows agent/service change is authorized by this deferral. Bounded P4-03a assessment presentation passed in the selected tenant: shared Control Room coverage warning, saved node assessment/verification and truthful unavailable states; full P4-03 remains open.

> **Offline plan identification, 30 October 2026:** available plans now show the successful preparation request ID and its stable completion time, matched to the exact content digest. Re-preparing the same package version updates those details while keeping one latest plan per machine. Older/manual plans explicitly show unavailable preparation details. [Display behavior and validation](patch-posture-phase3-platforms-2026-10-29.md#offline-plan-preparation-identity---30-october-2026). Phase acceptance gates are unchanged.

> **Airgapped recovery checks, 30 October 2026:** preparation expiry/source revalidation, cancellation, durable late-result receipts and polling reconnection have been hardened. Live app request a01e1d6d expired naturally; 79f7c04d failed after its source was disabled; 4044c310 reported interruption after a controlled agent restart without replay. Fresh request 1ae94861 succeeded and published the selectable plan; all 22 journal entries are acknowledged. Bounded P3-03l is checked. Native signed-repository/failure checks, isolated PostgreSQL tests and 50 focused UI tests passed. Full-host power-loss/reboot/service isolation and production rollout gates remain open. [Acceptance and live recovery evidence](patch-posture-phase3-platforms-2026-10-29.md#airgapped-preparation-recovery-qualification---30-october-2026).

> **App-driven offline preparation, 30 October 2026:** repository registration and Prepare offline plan are implemented and deployed to the existing lab. Administrators register the trusted Ubuntu source; operators request exact package versions through the app. Preparation never authorizes installation. The missing-content request failed safely; a fresh app preparation succeeded without installing packages. Its separately approved deployment 1d1fd425 completed APPLIED / PKGS 0, with later unknown verification and blocked promotion. Bounded acceptance P3-03k is checked. Manual staging and JSON import remain available. [Procedure, security contract and live evidence](patch-posture-phase3-platforms-2026-10-29.md#app-driven-offline-preparation---30-october-2026). Full-host/production gates stay open.

> **Agent-published offline plans, 30 October 2026:** the app now defaults to agent-published plan selection; manual JSON import remains available. Staging publishes the latest validated plan through the enrolled agent heartbeat, with a five-minute server receipt lease and approval/dispatch revalidation. App-approved lab requests 140f0755 and final guarded 3c10d82f completed APPLIED / PKGS 0 (same-version reinstalls), with later unknown verification and blocked promotion. This qualifies bounded discovery/selection, not full production readiness. [How entries are created, operator steps, tests and limits](patch-posture-phase3-platforms-2026-10-29.md#agent-published-offline-plans---30-october-2026).

> **Live offline qualification, 30 October 2026:** the existing Ubuntu 24.04 amd64 lab passed app plan import, mandatory approval and a real offline upgrade (74fbbc9f, APPLIED / PKGS 2). A separate corrupted-archive request (0e42a303) failed before installation; after restoring verified bytes, a freshly approved recovery request (17e6eb2f) completed APPLIED / PKGS 0. Later complete inventory retained verification unknown (no security baseline), with promotion blocked. Bounded software workflow P3-03i is checked; P3-03e/parent P3-03 and full-host/production gates remain open. [Current implementation and evidence](patch-posture-phase3-platforms-2026-10-29.md#live-offline-workflow---29-october-2026). Earlier all-offline-blocked checkpoints below are historical.

> **Phase 3 checkpoint, 29 October 2026:** P3-02 runtime/mode availability and pre-approval rejection gates are locally qualified. Live offline request 23b92d9c created no approval/job; separately approved Linux/apt proxy request 45754a16 completed APPLIED / PKGS 0 with promotion correctly blocked. Current agent IP 172.19.0.2 is now distinguished from enrollment/configured IP 172.19.0.3. [Matrix, implementation, live steps and remaining qualification](patch-posture-phase3-platforms-2026-10-29.md). Overall Phase 3, full-host and production gates remain open. Earlier demonstration documentation was committed as 9d400d58; this implementation checkpoint is uncommitted.

> **Fresh live demonstration, 29 October 2026:** New app-approved deployment b4e48dd8 completed APPLIED / PKGS 0; later assessment 4788a20d produced verification unknown (no security baseline), with promotion blocked. Signed repository refresh, isolated security/reconciliation tests and fresh live-database restoration passed. The temporary proxy ACL was repaired for the changed lab IP; original direct/no-proxy settings were restored. [Exact steps, evidence and pending gates](patch-posture-self-service-recovery-guide.md#fresh-live-workflow-demonstration--29-october-2026). Phase acceptance is unchanged; remaining host/platform/production gates stay open.

> **Current Phase 2 qualification, 28 October 2026:** P2-01 through P2-10 software-contract checks are qualified using the existing Ubuntu lab and isolated tests. [Implementation, test evidence, live demonstration and scope limits](patch-posture-phase2-state-2026-10-27.md#complete-state-contract-qualification--28-october-2026). Full-host reboot/platform, external recovery, managed production trust and production rollout gates remain open in their respective phases. Earlier checkpoints below are historical; use the [canonical acceptance checklist](patch-posture-phase-acceptance.md) for current status.

> **Offline bundle checkpoint, 28 October 2026:** Token eligibility and authenticated downloads are fixed; 15 UI/API tests, targeted backend tests, TypeScript, lint and production build passed. [Dropdown sources, repeatable steps and evidence limits](patch-posture-self-service-recovery-guide.md#offline-bundle-token-eligibility-and-dropdown-sources--28-october-2026). Final live browser recheck passed on 28 October: expired/revoked entries remain disabled after Reload tokens, no old token is selected, and Download bundle is disabled with an empty raw token field. Screenshot: tmp/local/patch-posture-demo/token-picker-20261028/token-picker-final-live.png. No overall phase closure or new credential is claimed. That token-picker checkpoint used controlplane-token-qualified (SHA-256 1761B04CD5F3D3C076614756671FDF091AC361C3B0A42B3D3C89FDFEA38AE86F), with its prior candidate retained for recovery; see the later P2-06 checkpoint for the current runtime selection.

> **Current phase acceptance, 27 October 2026:** [The six-phase checklist](patch-posture-phase-acceptance.md) records P1-09/P1-10 as passed within the [real mid-install and storage-fault scope](patch-posture-phase1-nonvm-2026-10-27.md). The non-VM Phase 1 work is complete; overall Phase 1 remains open for P1-11/P1-12 full-host/storage and support-matrix qualification. [Phase 2 engineering has started](patch-posture-phase2-state-2026-10-27.md). Phase 0 and the remaining production gates stay open. Earlier dated unchecked rows below are historical checkpoints, not competing current closure claims.

**Current status, 27 October 2026:** The signed Linux journal candidate and transactional completion/dispatch recovery fixes are deployed locally. The defined dispatch concurrency/connection-loss matrix and a real zlib1g post-install interruption with an overlapping request passed; see [the latest Phase 1 acceptance evidence](patch-posture-phase1-execution-2026-10-27.md#dispatch-recovery-and-real-package-interruption--27-october-2026). Phase 1 remains open for full-host/filesystem power-loss and supported OS/mode qualification. Phase 0 remains open. Earlier staged-only, no-new-upgrade and unfixed-dispatch statements are historical checkpoints.

**27 October candidate prepared:** the new journal-fix executable passed pinned-key verification and an isolated startup-rejection check. [Candidate hash, test evidence and installation boundary](patch-posture-phase1-execution-2026-10-27.md) are recorded. The active agent is unchanged; no new app demo is claimed.

**27 October follow-on:** [isolated PostgreSQL rollback/concurrency checks and lab-journal compatibility](patch-posture-phase1-execution-2026-10-27.md) passed. The requested built-in-browser demo is pending tool access; backend verification is recorded separately from visible app evidence.

**27 October Phase 1 continuation:** [journal corruption and acknowledgment persistence fixes](patch-posture-phase1-execution-2026-10-27.md) are being qualified independently of the remaining Phase 0 resources. Focused Linux and native Windows regressions pass; no new live agent rollout, app demonstration or package installation is claimed. Both phases remain open.

**Latest, 23 October:** [the three-area report](patch-posture-three-areas-verification-2026-10-23.md) records deployed signed Linux/server candidates, independent live-cache verification, recovery qualification and the third app upgrade (`liblzma5`, **PKGS 1**) with actual historical finding resolution. Earlier two-advisory/two-upgrade and pending Linux-rollout statements below are historical. All four Phase 0 groups remain open; the VM remains off.

**23 October Trust continuation:** configured policy keys no longer silently downgrade when missing, cached policies require verification, and supplied installer signatures fail closed. Focused Linux/Windows policy and Linux installer/bundle tests pass. [The dated report](patch-posture-trust-verification-2026-10-23.md) distinguishes those code/test results from pending agent rollout and live signed-mode qualification. The VM remains powered off; all four Phase 0 groups remain open.

**23 October connectivity repair:** restored the existing lab network alias/TLS upstream after API recreation disconnected Ubuntu. The existing `scripts/local.ps1` now restores that optional lab connection; its syntax and idempotent recovery block passed checks. Ubuntu's current heartbeat was verified visibly under the same enrollment. This is not a signed-agent rollout or full-host qualification.

**Latest verification, 21 October:** the counter was verified with a second real upgrade, mode labels were corrected, Windows telemetry recovered with retained quarantine, both agent runtimes were exercised and the lab HTTPS hop was tested. See [the current checklist, self-service steps and remaining gates](patch-posture-verification-2026-10-21.md). This supersedes earlier status statements about no native Windows deployment or no counter runtime verification. Both phases remain open.

**Later evidence:** a real one-package Ubuntu upgrade has now completed through the visible deployment/approval flow. See [the real-package demonstration](patch-posture-real-package-demo.md) for versions, IDs, screenshots, discovered defects and cleanup. Earlier statements below that no patch was dispatched describe the baseline before this separate bounded lab experiment. Both phases remain open.

## Current Phase 0 checklist — updated 23 October 2026

- [x] Deploy signed Linux/server candidates and independently verify the live signed cache; reject negative copies in isolation.
- [x] Add three selected Ubuntu advisories, demonstrate the single-package app approval/upgrade and resolve the historical finding from refreshed inventory.
- [x] Encrypt the defined local application recovery archive; verify all 44 file hashes, SQLite integrity, isolated PostgreSQL restoration and wrong-passphrase rejection; clean up disposable databases/staging. Scope and exclusions are in the three-area report.
- [ ] Complete approved off-machine recovery retrieval/restoration; local recovery preparation does not close this gate.
- [ ] Complete managed production HTTPS and cross-platform signed-only/replay/rotation qualification.
- [ ] Run the prepared VM's full-host checks after sufficient RAM is available.
- [ ] Establish maintained comprehensive assessment coverage and persisted per-node assessment completeness.

The following paragraphs retain the preceding checkpoint; use the linked current report and consolidated checklist for superseding evidence.

**Latest continuation:** assessment now fails before reconciliation for missing/expired/altered feed evidence; focused tests passed and the guard was deployed locally. The existing signed checkpoint was reactivated in the visible app. A real Ubuntu VirtualBox VM is prepared but powered off and not enrolled: host RAM remains insufficient for its planned 4 GB allocation. See [the continuation evidence and repeatable checks](patch-posture-verification-2026-10-21.md#phase-0-continuation--assessment-guard-and-virtualbox-preparation).

**Assessment follow-up, deployed and demonstrated:** tenant and all-tenant patch views now show partial/expired/unavailable feed status, and reconciliation is restricted to explicit package/CVE fixed-boundary evidence. The live app shows partial coverage and retains the two unresolved findings. Focused Go tests, PostgreSQL reconciliation, 13 UI tests and TypeScript checks passed. [Implementation, screenshots, repeatable steps and remaining limits](patch-posture-verification-2026-10-21.md#assessment-coverage-and-reconciliation-continuation--21-october) are recorded separately from the historical checkpoint below. The four evidence groups remain open.

**Phase 0 remains open. Phase 1 is implemented in a local candidate and partially qualified; it also remains open.** The checkboxes below reflect the recorded verification results, not a new runtime check today. Detailed evidence, IDs, limitations and repeatable app steps are in [the current verification record](patch-posture-verification-2026-10-21.md).

### Completed evidence

- [x] Identify Local Development, the existing Cloudspace Windows service and the disposable Ubuntu enrollment; capture inventory and heartbeat evidence.
- [x] Restore connectivity while preserving enrollment and the existing service/launcher; create no new `.ps1` files.
- [x] Verify local database restores, content recovery and enrolled lab recovery checkpoints.
- [x] Demonstrate a signed, explicitly limited two-advisory Windows 7-Zip assessment.
- [x] Implement and test candidate tenant isolation, approval binding and durable completion/journal protections. This records implementation and focused tests, not Phase 1 closure.
- [x] Complete two bounded real Ubuntu upgrades through approval/completion, refresh inventory and verify the corrected **PKGS 2** counter in the app.
- [x] Update the existing Windows service in place, preserve configuration/enrollment and run focused native Windows tests.
- [x] Verify scoped event streams, keepalive and local lab HTTPS with trusted, wrong-hostname and untrusted-certificate checks.
- [x] Recover Windows telemetry ingestion while retaining expired events in quarantine; display incomplete collector coverage explicitly.
- [x] Repair the missing raw-log tables using the existing migration statements after an isolated rehearsal, then restore-test the final database checkpoint.
- [x] Record candidate hashes, source-review snapshot and rollback artifacts. Signed, reproducible production provenance remains unqualified.
- [x] Enforce feed receipt/checksum and expiry checks at assessment time, with regression proof that invalid/unavailable evidence preserves existing findings; deploy and demonstrate the valid signed-feed path locally.
- [x] Prepare `controlone-phase0-ubuntu-vm` in VirtualBox with 4 GB RAM, two CPUs, NAT and a 40 GB dynamic disk. It remains powered off; guest setup, enrollment and full-host tests are outstanding.
- [x] Implement partial/expired/unavailable assessment notices and explicit package/CVE fix-boundary reconciliation; pass focused Go tests, a dedicated PostgreSQL regression, 13 UI tests and TypeScript checks. Live verification is recorded in the linked assessment follow-up.
- [x] Pass isolated policy-key/cache and installer supplied-signature regressions on 23 October; signed-agent rollout remains pending.
- [x] Repair the lab network attachment and verify the same Ubuntu enrollment connected in the app; update and check the existing local launcher recovery block without creating a new `.ps1` file.

### Remaining Phase 0 evidence groups

- [ ] Off-machine recovery copy, retrieval and restore, with an owner, retention and access controls.
- [ ] Production-reachable HTTPS, managed trust/renewal, end-to-end transport and policy/binary signing qualification. Local lab TLS does not complete this gate.
- [ ] Supported VM/host qualification for authentication logs, file-access monitoring, hardening, permissions, kernel/reboot and recovery. The minimal container remains partial.
- [ ] Broader vulnerability coverage, Linux assessment, feed refresh and visible assessment completeness. Assessment-time feed expiry is now enforced in the local candidate; two selected advisories still do not assess the whole fleet.

Before closing Phase 0, reconcile these results against the original review and explicitly resolve or scope the remaining evidence gaps: build provenance, stale fleet inventory, quarantine ownership/retention and the unexplained schema drift. Record acceptance results and owners; an untested capability cannot be marked passed. Finishing the four groups does not automatically establish production readiness.

### Separate Phase 1 work still open

- [ ] Complete failure/concurrency and recovery qualification, including dispatch creation/linking, action-plan receipts and deployment rollups. Native tests and successful lab upgrades do not close these boundaries or qualify wider production rollout.

## Historical checkpoint — 20 October 2026

Everything from this heading onward preserves the earlier walkthrough and its observations. Its unchecked items, executable hashes and statements such as “no patch installation” or “Cloudspace still runs its original agent” describe that earlier checkpoint; use the current checklist above for present status.

Observed on 20 October 2026 in the running local Control One console. The initial walkthrough records the state before repair. Subsequent connection repair, partial assessment, and fresh disposable Linux enrollment are recorded separately. At that initial checkpoint, no patch deployment or patch approval was performed. Linux installer prerequisites were installed in the disposable lab; the existing Windows installation was preserved.

### Phase 0 checkpoint — 20 October 2026

**Historical status, 20 October 2026: Phase 0 remained open. Phase 1 security fixes were implemented in a local candidate and partially qualified; Phase 1 was not closed.** At this checkpoint, no patch installation had been dispatched.

## Verification update: local security candidate

All work remains on `0y3/Me/patch-posture-review`, based on commit `7f3a75eeacc8057258157661519d3fd0df9ee81b`, with uncommitted changes. No new PowerShell script was created. The existing Windows service and its binary were preserved.

### Confirmed in this run

- [x] Replace the disposable Ubuntu lab's manual background-agent launch with an agent main process, Docker init and `restart: unless-stopped`, defined in `compose.patch-posture-lab.yml`. Keep the original manual container stopped as recovery evidence.
- [x] Verify automatic recovery after deliberately killing the candidate agent process, then an explicit container restart. Preserve node identity `d033fa85-5796-48ae-8e8a-f82706387326`. The app reports `phase1-candidate-7f3a75ee (linux/amd64)` and a current heartbeat.
- [x] Make missing `journalctl` report an explicit unsupported collector and incomplete authentication-log coverage. Verify **Needs attention** in the live node overview. Skip host-hardening operations on detected containers rather than treating container restrictions as host hardening success.
- [x] Take a fresh PostgreSQL backup after lab enrollment and feed assessment; restore that exact dump to an isolated test database and pass the migration/security integration test there. Backup: `tmp/local/patch-posture-demo/recovery/controlone-pre-phase1-20261020_115628.dump`, SHA-256 `DB79CD9FA608DCC72F65E321227464648C41870821C65019D879CBC131F7B5EA`.
- [x] Implement tenant checks on patch planning, configuration, maintenance-window and proxy routes; reject cross-tenant configuration references.
- [x] Bind completion reports to the reporting node, tenant, job type and recorded deployment/state identifiers. Persist terminal job and patch-state updates together in a PostgreSQL transaction; reject cancelled jobs and conflicting terminal reports.
- [x] Retain agent completion reports in a durable execution journal until the server explicitly acknowledges persisted completion. Reserve jobs before execution, serialize patch execution, reject duplicate dispatch, and preserve interrupted jobs as an unknown outcome requiring reconciliation.
- [x] Bind approvals to a policy snapshot hash, reject changed or expired approval context, recheck safety gates, and retain the approved proxy endpoint for dispatch. Migration 165 adds the policy hash. Legacy approvals without this hash require reissue.
- [x] Pass focused Go server, agent and telemetry tests; journal/dispatch race tests; seven patch-page UI tests; TypeScript checks; Linux agent/server builds and a Windows agent cross-build. PostgreSQL integration tests verify wrong-node/action rejection, cancellation, rollback on event-write failure, concurrent completion and approval expiry/snapshot persistence. The Windows cross-build does not prove Windows runtime behavior.
- [x] Deploy the candidate control plane locally through `compose.patch-posture-candidate.yml`; verify schema 165, normal node API responses and fresh heartbeats from both Ubuntu and Cloudspace. Preserve the actual previous server executable for rollback. No active queued/running patch job existed at this deployment check.

Candidate agent SHA-256: `F959F05785F382F8E866BE0285D16672DBC53FDAA09B963970E928B6A7A11E8B`. Candidate server SHA-256: `9F18EF3FECB3DA89B2981A48D07DADD04A8F62D45B0AA7C0226E602D07B4F8F6`. Build metadata records a modified working tree; this is a local candidate, not a signed production release. Evidence and the source overlay are under `tmp/local/patch-posture-demo/phase1/`.

### Still open — do not mark complete

- [ ] Production HTTPS/bootstrap and policy-signing trust; the lab still uses HTTP and has no configured policy verification key.
- [ ] A recovery copy outside this machine, with an agreed owner, retention and access controls. Local restore success does not cover losing this host.
- [ ] Supported vulnerability coverage beyond the two selected Windows 7-Zip advisories, including Linux, feed refresh and assessment-time expiry enforcement. Tenant freshness remains two of five nodes.
- [ ] Full host/VM qualification for hardening, permissions, kernel, reboot and repository/package behavior. This minimal Ubuntu container cannot establish those guarantees.
- [ ] Resolve/qualify remaining telemetry: fileaccess has no available backend and exits; authentication logs lack journalctl; agent SSE reconnects receive HTTP 403; event-ingest requests include HTTP 400 responses. These were observed after candidate deployment and are not successful telemetry evidence. Initial connection-refused messages during the server restart are separate transient startup failures.
- [ ] Complete Phase 1 runtime security qualification, including Windows deployment/acknowledgment compatibility and durable-journal crash/storage failure behavior across platforms. Only the Ubuntu agent received this candidate; Cloudspace still runs its original agent.
- [ ] Review remaining dispatch/approval concurrency and recovery boundaries. Completion's job/state transaction does not include action-plan receipts or deployment rollups; dispatch creation/linking is not one atomic transaction. These limitations must remain explicit in the later recovery work.
- [ ] Run an authorized bounded installation, reboot handling, fresh inventory/reassessment and recovery demonstration only after the security qualification gate passes. No package remediation result is claimed here.

### Repeat the visible app checks yourself

1. Select **Local Development** → **Servers** → **phase0-ubuntu-lab** → **Overview**. Confirm the candidate version and current heartbeat. This proves the intended executable is reporting under the same enrolled identity.
2. Scroll to **Telemetry readiness**. Read the unsupported reason. A green predictive score is not proof that every collector works.
3. Open **Packages**, clear the text filter and inspect the system-package toggle. The recorded inventory contains 109 real dpkg packages; this is inventory evidence, not proof that Linux vulnerabilities have been assessed.
4. Open **Patch posture**. Review fleet freshness and the signed feed's limited scope before interpreting finding counts. Do not approve/install packages as part of this evidence-only check.
5. For a restart drill, use the existing compose service from an administrator terminal at the repository root: `docker compose -f compose.patch-posture-lab.yml restart ubuntu-agent`. Return to the same node page and confirm a newer heartbeat with the same ID. Do not start the stopped manual backup alongside it: it contains the same enrollment credentials.

### Local candidate deployment and rollback

The tested deployment command is `docker compose -f docker-compose.local.yml -f compose.patch-posture-candidate.yml up -d --no-deps controlplane`. It relies on this machine's existing local configuration and internal lab network; it is not a fresh production installation recipe.

The preserved old server executable is `tmp/local/patch-posture-demo/phase1/controlplane-before-phase1`, SHA-256 `3BB7DF27B409B3522F21442211ACF317D5FE29DC33162BAC562663D0116D97D5`. To restore that executable, change only the candidate override's command path to `/workspace/tmp/local/patch-posture-demo/phase1/controlplane-before-phase1`, then run the same compose command and check health/heartbeats. Merely omitting the override returns to `go run` against the current modified source, so it does not restore old code. Migration 165 is additive; do not drop its column or restore the live database without a separately reviewed recovery decision. Recheck the lab network alias after any compose change.

The Ubuntu recovery image retains the original `/usr/local/bin/controlone-agent` and candidate `/usr/local/bin/controlone-agent.candidate`. Agent-only rollback uses the original path as the lab compose entrypoint and recreates that single lab service. Preserve the execution journal and any pending results during rollback. All enrolled recovery images contain credentials and must stay local and access controlled.

### Completed

- [x] Identify the tenant and installed Windows enrollment: Local Development / Cloudspace, with the service state matching the console node ID.
- [x] Restore connectivity through the existing service and launcher. Agent API: `http://127.0.0.1:8443`; console: `http://127.0.0.1:4173`. Preserve enrollment and save connection rollback instructions below. No new `.ps1` file remains.
- [x] Capture current inventory and heartbeat evidence. Cloudspace has 41 registry packages; full inventory sync was `2026-10-20T09:07:08.974293Z`, with a subsequent heartbeat at `2026-10-20T09:31:08.480315Z`. Tenant inventory freshness improved to 1 of 4 nodes. The other three remain stale.
- [x] Record platform scope. Windows execution currently uses WinGet for applications; Windows OS servicing is not demonstrated. The estate also contains stale Ubuntu and Debian enrollments.
- [x] Inspect initial vulnerability coverage. At the initial baseline, no vulnerability feed was installed; five imported bundles were versions of a SIEM content pack. The active pack was `2026.09.14.5`, imported `2026-09-14T11:40:29Z`. Initial findings were zero. The later real, partial vulnerability-feed demonstration is recorded below; it supersedes that initial feed state without establishing full coverage.
- [x] Inventory local role assignments and test the read-only planning boundary. Global assignments: two administrators, one CISO, one operator, one viewer. Operator planning returned HTTP 200 with `approval_required`; viewer planning returned HTTP 403. These checks do not prove tenant isolation or safe approval/completion handling.
- [x] Capture deployment history: one pending deployment, zero completed deployments, and one expired approval. Cancel the separate stale September agent-update job through the normal audited cancellation API.
- [x] Record runtime topology: local Docker control plane, PostgreSQL 16.15, local Vite UI, installed Windows service, named runtime-data volume. PostgreSQL schema migration is 164, with 155 public base tables.
- [x] Record source and executable fingerprints. Working branch: `0y3/Me/patch-posture-review`; source HEAD at observation: `7f3a75eeacc8057258157661519d3fd0df9ee81b`, with existing working-tree changes. Control-plane executable SHA-256: `3bb7df27b409b3522f21442211acf317d5fe29dc33162bac562663d0116d97d5`; installed Windows agent SHA-256: `DFD8C68960EF72D1E644DAB317CA369312AD1FA408FEA5CFCB6CA4D7C6439306`. Neither executable embeds a VCS revision, so an exact match to source HEAD cannot be asserted.
- [x] Create a PostgreSQL recovery checkpoint and successfully restore it into a separate temporary database. Verify migration 164, 155 tables, and matching counts for tenants/nodes/deployments/approvals/packages/findings: `36 / 25 / 1 / 1 / 474 / 0`. Remove only that temporary restore database after verification; the live database is unchanged by the restore drill.
- [x] Archive offline content and verify recovery by isolated extraction and matching SHA-256 hashes for all 13 files. This content is a SIEM pack; this check does not establish vulnerability-feed coverage.
- [x] Create a consistent analytics SQLite backup through SQLite's online backup API. Verify a scratch copy with full `PRAGMA integrity_check`: `ok`, two tables. The original slow verification over the Windows bind mount was superseded by the successful full check on container-local scratch storage.
- [x] Repair the existing NVD feed-factory reference parser, retain legacy input compatibility, and pass the factory tests. Import an existing-key-signed checkpoint of two real NVD advisories with an explicit reviewed 7-Zip registry mapping. Verify a current Cloudspace finding and the tenant summary live. Details and coverage limits follow below.
- [x] Identify a separate disposable nonproduction Linux environment and verify its recovery method. Created `controlone-phase0-patch-lab-20261020` from the existing Ubuntu image, with network disabled, no published ports or host mounts, and no Control One enrollment. Exported/reloaded its checkpoint and verified restoration in a separate disposable container. Scope and remaining qualification steps follow below.
- [x] Enroll a clean lab clone through the visible app's Linux command-install flow and existing installer. Verify `phase0-ubuntu-lab` as connected, with 109 real dpkg packages, then save and verify an enrolled recovery checkpoint. Tenant inventory freshness is now 2 of 5. Hardening and full telemetry remain partial; see the exact steps and limitations below.

### Outstanding or partial

- [ ] Expand the demonstrated partial assessment into supported vulnerability coverage for the intended fleet. The real signed checkpoint below covers two selected 7-Zip advisories only. Other applications, Windows OS, Linux coverage, automated refresh, and enforced assessment-time feed expiry remain unqualified. A zero count outside this scope cannot mean clean.
- [ ] Map deployed binaries to reproducible source revisions. Hashes identify the current files; they do not establish which commit built them.
- [ ] Complete execution qualification prerequisites: reviewed package/repository plan, bounded execution permissions, and explicit disposition of minimal-container hardening/collector gaps. Fresh lab enrollment and internal control-plane connectivity are complete. Container-based recovery does not qualify kernel, reboot, firewall, or Windows servicing; those require a VM or dedicated target. No patch has been dispatched to the lab.
- [ ] Move a recovery copy outside this development machine and establish recovery ownership, retention, and access. The local checkpoint tests recovery mechanics, not protection against losing this host.

Phase 0 can be closed when the qualification target is identified and the remaining evidence gaps have an agreed disposition. Initial feed absence has progressed to a real but limited assessment; broad coverage and missing build provenance remain gaps. Actual patch approval, installation, reboot, vulnerability remediation verification, and package rollback are later qualification work and have not been demonstrated. Phase 1 still needs the security fixes described in the review. The feed-tool compatibility repair is separate from those execution-security fixes.

## Live Linux enrollment — repeatable steps and reasons

Completed on 20 October 2026, with final verification at 12:17 Africa/Lagos. The browser was visible throughout the app steps. The app had initially stopped responding on port 4173; restored it through the existing `scripts/local.ps1 start -BindAddress 127.0.0.1`. No new PowerShell script was created.

### What to click in the app

1. Select **Local Development** in the top tenant selector. This determines the tenant receiving the enrollment.
2. Click **Enroll** at the top, then **Command install**. This creates an installation command to run on the target; it does not remotely execute it.
3. Select **Linux** for Ubuntu. Choosing the correct platform selects the appropriate installer and binary.
4. Click **Generate token**. The app specifies one machine and a 24-hour token. Treat the generated command as a secret until the token is consumed or revoked. This demonstration used a fresh token; the API later confirmed `max_nodes=1` and `nodes_enrolled=1`. Do not reuse that consumed command for another machine.
5. Run the installer on the intended Ubuntu target as administrator/root. For the container-specific work performed here, see the next subsection. A command containing `127.0.0.1` works only when the target can reach the control plane at its own loopback address; our separate container cannot.
6. Return to **Servers**, refresh, and expand **Local Development**. The new machine is **phase0-ubuntu-lab**. The fleet now shows two online nodes out of five, including Cloudspace.
7. Open **phase0-ubuntu-lab** and verify **Connected · heartbeat current**. Enrollment success alone does not prove that the running agent can deliver heartbeats.
8. Open **Packages**. There are 109 dpkg packages; the default system-package filter displays 81 and hides 28. Filter `curl` to see the real installed version `8.5.0-2ubuntu10.16`, source `dpkg`. Clear the filter to inspect other packages. This verifies real inventory collection; it does not prove vulnerability coverage.

Node URL: `http://127.0.0.1:4173/console/nodes/d033fa85-5796-48ae-8e8a-f82706387326`. Screenshot: `tmp/local/patch-posture-demo/lab/enrolled-lab-packages.png`.

### What was done on the Ubuntu target

These are the target-side operations behind step 5, not browser actions. They are specific to this local container lab, not a general production deployment recipe.

1. Start a **new clone** of `controlone-phase0-patch-lab:20261020-baseline`, with container name `controlone-phase0-enrolled-lab-20261020` and hostname `phase0-ubuntu-lab`. Preserve the original stopped recovery-drill container. The clone has no host mounts or published ports, is nonprivileged, has `no-new-privileges`, memory 768 MiB, and PID limit 192. Unlike the earlier offline recovery-check container, this enrollment clone uses Docker's default capability set; least-privilege runtime qualification remains outstanding.
2. Temporarily use Docker's ordinary bridge to install `curl` and `ca-certificates` from the configured Ubuntu repositories. These are installer prerequisites. No application-wide upgrade or patch deployment was requested.
3. Disconnect that bridge. Create internal Docker network `controlone-phase0-agent-internal`; attach only this lab and the existing control plane, with alias `phase0-controlplane` for the latter. The control plane retains its original database network; the lab does not join it. Verify `http://phase0-controlplane:8443/healthz` responds. Internal Docker networking removes ordinary external egress; it is not a substitute for a production firewall policy.
4. Fetch the existing server-generated Linux installer, then use the token generated in the app with `CONTROL_PLANE_URL=http://phase0-controlplane:8443`. Run that existing installer with `--no-service`, because this container is not a full service-managed host. The actual invocation used the same structure as:

   ```bash
   TOKEN="<fresh token from the app>" \
   CONTROL_PLANE_URL="http://phase0-controlplane:8443" \
   bash /tmp/controlone-existing-installer.sh --no-service
   ```

   Replace the placeholder only on the intended target; avoid retaining raw tokens in history or shared transcripts. In this run the token was passed without printing it, and temporary token/installer files were removed afterward. A normal VM should use its reachable control-plane HTTPS address and normal service installation instead of these container-specific settings.

5. The existing installer downloaded the binary, verified its SHA-256 checksum, installed it, and enrolled a fresh identity. Its log did not show successful signature verification, so no claim of verified signed-binary trust is made. The local transport remains HTTP inside Docker; production transport/bootstrap trust still requires qualification.
6. Start the installed agent with its generated configuration:

   ```bash
   /usr/local/bin/controlone-agent --config /etc/control-one/nodeagent.yaml
   ```

   Here it runs as an explicit detached container process with output in `/tmp/phase0-agent.log`. Docker's lab keepalive command does not automatically restart the agent; after a container restart, start this process again. Production requires supervised service/process lifecycle checks.

7. Verify the new node ID, tenant, heartbeat, and packages through both the API and visible console. The installed Linux agent SHA-256 is `598e3a795ae4bd9ab1e90b25bf417e4b73749bd74fd445ef8872d98a2b72430f`; its version is still `dev (linux/amd64)`, so source-commit provenance remains unresolved.

### Recovery and observed limits

Stopped the enrollment clone to capture a consistent filesystem checkpoint, committed `controlone-phase0-patch-lab:20261020-enrolled`, and exported `tmp/local/patch-posture-demo/lab/lab-enrolled-image.tar` (89,241,088 bytes; SHA-256 `C7BBF31F0377EDD9DBD607B6DE99DDB2DA08AD9B57998694C794A63ADCE3A1F3`). An offline temporary restore container confirmed matching dpkg inventory and nonempty agent config/state, then exited. Restarted the original clone and its agent; API heartbeat advanced to `2026-10-20T11:16:51Z` after the checkpoint procedure.

**The enrolled image contains node credentials.** Protect it as credential-bearing recovery data, keep it out of Git/shared client artifacts, and restore only this same logical machine. Never run simultaneous clones of the enrolled identity. For a fresh machine/demo identity, start from the unenrolled baseline and generate a fresh token. The recovery check ran with network `none`, so it never contacted the control plane as a duplicate node.

The join process attempted optional fail2ban/hardening package installation after external egress was removed. Those downloads failed; the installer still reported enrollment complete. The node's Overview also reports missing `journalctl` for Linux authentication-log collection. These are real partial-install/collector limitations; no security check was disabled to make the demonstration appear healthy. A full VM with approved repositories, supervised services, and required hardening/logging is needed for broader host qualification. The lab's 109-package inventory and heartbeat are valid despite these separate gaps.

Remaining production work includes manifest/signature fail-closed bootstrap validation, trusted HTTPS, least-privilege runtime, supervision/restart behavior, full platform hardening, explicit Linux vulnerability coverage, repository/content control, and Phase 1 patch execution-security fixes. The Windows-specific two-advisory feed does not assess Ubuntu packages. No patch was approved or dispatched, and no package-remediation or reboot success is claimed.

Sanitized evidence: `tmp/local/patch-posture-demo/lab/enrolled-lab-evidence.json` and `enrolled-live-verification.json`. The latter records five inventoried nodes, two fresh inventories, two existing Windows findings, and zero completed deployments. These runtime files are ignored by Git; no new `.ps1` file was added.

## Disposable qualification environment and recovery drill — 20 October 2026

Inspected the existing stopped candidates without starting them. `my-ubuntu` has an existing agent installation and a published host port 8082; the Alpine network-onboarding fixture serves a different apparent purpose. Both were preserved. Created a separate named disposable lab from the already-installed `ubuntu:24.04` image rather than inheriting an old agent's identity or configuration.

| Item | Evidence |
| --- | --- |
| Lab container | `controlone-phase0-patch-lab-20261020` |
| OS observed | Ubuntu 24.04.5 LTS; apt 2.8.3; dpkg 1.22.6ubuntu6.6 |
| Isolation | Docker network `none`; no published ports; no host mounts; nonprivileged; all capabilities dropped; no-new-privileges; memory 512 MiB; PID limit 128 |
| Enrollment | None; intentionally absent from the Control One console |
| State after drill | Stopped; restore check container automatically removed after completion |
| Recovery image | `controlone-phase0-patch-lab:20261020-baseline` |
| Exported checkpoint | `tmp/local/patch-posture-demo/lab/lab-baseline-image.tar`, 29,775,872 bytes |
| Checkpoint SHA-256 | `CF43FBD142B3EBF9B849B39235E798C5A502EBDC48291C5297FC0AA45723A4CD` |
| Package baseline SHA-256 | `354e391b2b8f9c2a246c5fe9f1d2966b30a92232ed88c655f94480be9ff35ac2` |
| Verification time | `2026-10-20T10:38:53Z` |

Recovery procedure exercised:

1. Record `dpkg-query -W` inside the isolated container and commit a baseline image.
2. Export the image to the local checkpoint archive and successfully reload that archive with Docker.
3. Create `/tmp/phase0-after-checkpoint-marker` in the original lab after taking the checkpoint.
4. Start a separate temporary container from the recovery image with the same isolation settings. Verify that the marker is absent and that a newly generated package inventory matches the saved baseline byte for byte.
5. Let the disposable restore-check container exit and be removed, then stop the original lab. The original lab retains the drill marker; future execution qualification should start from the clean recovery image.

Results are saved in `tmp/local/patch-posture-demo/lab/lab-environment.json` and `lab-recovery-result.json`. No repository script was added. The existing Ubuntu candidate, installed Windows agent, live control plane, and database were not restarted or replaced by this drill.

This qualifies checkpoint export/load and container filesystem/package-inventory recovery only. It does not qualify an actual failed package upgrade, application availability, agent execution, package repositories, kernel recovery, reboot handling, or Windows OS patching. Network `none` also deliberately prevents package downloads and control-plane contact at this stage.

Phase 0 now has an identified nonproduction userspace environment and demonstrated local recovery mechanics. The subsequent enrollment and real package tests remain explicit qualification work. Off-machine recovery storage is awaiting the user's destination; no external copy has been made. Existing binaries lack source-revision metadata, so their commit provenance remains unknown until a controlled build/deployment records it.

## Live signed vulnerability-feed demonstration — 20 October 2026

### Source, repair, and scope

Downloaded the actual NVD API 2.0 records for `CVE-2025-0411` and `CVE-2026-58052` from `https://services.nvd.nist.gov/rest/json/cves/2.0?cveId=...`. Cross-checked 7-Zip's [vendor release history](https://www.7-zip.org/history.txt), which lists the latter fix in 26.03. This is real advisory content, not a planted vulnerability or altered inventory.

The existing `vulnerability-feed-factory` failed on the real response: `cannot unmarshal array ... references ... nvdReferencesNode`. Updated `controlplane/internal/vulnfeedfactory/factory.go` to accept the current reference array and the existing legacy `referenceData` wrapper. Added a regression case for API 2.0 references and range preservation. Existing legacy-input tests still pass. No running control-plane or Windows agent binary was replaced; the repaired CLI was run from the existing mounted source.

The existing Windows collector reports registry display names, not CPE product identities. For this checkpoint, the normalized feed explicitly maps only `cpe:2.3:a:7-zip:7-zip:*:*:*:*:*:*:*:*` to the observed `7-Zip 26.02 (x64)` / `registry` identity, preserving NVD's affected-version boundaries. Unrelated product entries are excluded. This is a reviewed content mapping for this display name, not an automatic fleet-wide identity mapping; future display-name changes require a revised mapping. Mapping provenance is retained in the finding's advisory metadata.

Used the already-existing `offline-content-factory` and `tmp/local/offline-content-private.pem`; the existing configured public key verified the signed archive. No key was generated or trust configuration changed. Imported through the normal authenticated API for Local Development, then refreshed the existing signed-content page and Patch posture page in the browser.

| Evidence | Result |
| --- | --- |
| Bundle | `controlone-local-7zip-nvd-assessment`, version `2026.10.20.1`, sequence 1, active |
| Import time | `2026-10-20T09:51:43Z` |
| Expiry | `2026-10-27T09:51:16Z`; manual seven-day checkpoint, not an automated subscription |
| Coverage | Two selected advisories for the mapped Windows 7-Zip package only |
| Current Cloudspace result | `CVE-2026-58052`, installed 26.02, fixed version 26.03, explicit version-range match; NVD severity low, CVSS 3.3 |
| Negative comparison | `CVE-2025-0411` has boundary `< 24.09`; it did not create a finding for installed 26.02 |
| Tenant summary | 2 unresolved findings / 2 known affected nodes / 2 with known fix; inventory remains 1 of 4 fresh |
| Second finding | `content-pack-demo.local` has the same package in stale inventory; last heartbeat `2026-09-23T22:02:39Z`. This is historical inventory assessment, not a current scan of that machine |
| Source evidence | Finding retains NVD URL, vendor history reference, advisory timestamps, bundle identity, sequence, content hash, mapping provenance, and match type |
| Installation | None; no patch approval, deployment, package upgrade, or reboot was performed |

Local evidence is under `tmp/local/patch-posture-demo/feed/`: original `nvd-*.json`, `nvd-normalized.json`, `7zip-reviewed-feed.json`, `manifest.json`, `7zip-nvd-signed.tar.gz`, `import-receipt.json`, and `cloudspace-assessment.json`. Live screenshot: `tmp/local/patch-posture-demo/patch-posture-real-feed.png`. These artifacts are ignored by Git; the source repair and this document remain on `0y3/Me/patch-posture-review`.

Validation passed with `go test ./controlplane/internal/vulnfeedfactory ./controlplane/cmd/vulnerability-feed-factory ./controlplane/cmd/offline-content-factory -count=1` in the existing development container. The signed bundle factory also self-verified the archive before import. The API returned one active Cloudspace finding; the browser independently showed the new active bundle and changed tenant summary.

**Remaining assessment work:** define supported product identities and sources for the rest of the inventory; broaden advisory coverage; own and automate refresh; enforce expiry and assessment completeness; expose partial/unknown coverage in the UI. The current UI counts do not communicate this feed's limited scope. The previously reviewed R07/R13 gaps remain. The pre-import recovery checkpoint above does not contain these newly imported feed/findings records.

### Recovery evidence

Local artifacts are under `C:/Projects/control_one/tmp/local/patch-posture-demo/recovery/` and are ignored by Git. They contain application data; handle them with the same access controls as the local database.

- PostgreSQL checkpoint: `controlone-phase0-20261020_093213.dump`, 18,873,292 bytes.
- Dump SHA-256: `97FD97BCF1E3C483878A1A1D0F183463AC40E9079C4A50A548398D71396CFF8A`.
- Archive listing: `controlone-phase0-20261020_093213-manifest.txt`.
- Restore evidence: `controlone-phase0-20261020_093213-result.json`, including successful restore, matching counts, and temporary database cleanup.
- Offline content: `offline-content-phase0-20261020-verified.tar.gz`, 8,691 bytes; SHA-256 `a72ccf3bc2704c64134b4c5c1dca5bb3ee507fc9bc5dd82fd83cc1de34a124a4`. Extraction and 13 file-hash comparisons passed; evidence is in `offline-content-phase0-20261020-result.json`.
- Analytics: `analytics-phase0-20261020_093359.db`, 762,650,624 bytes; SHA-256 `7bbac19591da7719d3b1c8360212813ea707e8df592e95e547b78543bbe20a0e`. Full integrity verification passed; evidence is in `analytics-phase0-20261020_093359-result.json`.
- Latest sanitized API snapshot: `../phase0-evidence-20261020.json`, captured `2026-10-20T09:38:48Z`, including the `09:38:08Z` Cloudspace heartbeat and tenant freshness summary.
- This is a control-plane data recovery check. It does not establish application availability during a patch, Windows system recovery, or package downgrade safety.

## Environment and machine identity — before repair

| Item | Observed value |
| --- | --- |
| Console | `http://192.168.1.5:4173/console/infrastructure/patch` |
| Selected tenant | Local Development |
| Tenant ID | `91fc1ed4-fb36-4fcf-91e6-a4d0f4eebe7f` |
| Local Windows computer name | `CLOUDSPACE` |
| Enrolled hostname | Cloudspace |
| Installed service enrollment ID | `9a2f25fc-9e52-4a0e-a353-77a13c09b842` |
| Recorded OS | Microsoft Windows 11 Home Single Language 10.0.26200.9457, amd64 |
| Recorded agent version | `dev (windows/amd64)` |
| Service | `ControlOneAgent`, running at observation |
| Installed configuration | `C:/ProgramData/ControlOne/nodeagent.yaml` |
| Installed agent endpoint | `http://192.168.1.10:4173` |
| Current Wi-Fi address | `192.168.1.5` |
| Local API health | `http://127.0.0.1:8443/healthz` returned HTTP 200, `ok` |

The installed service state file matches the node ID shown in the console. This identifies the current machine without relying only on its hostname.

The older development launcher state at `tmp/local/live-nodeagent/state.json` uses enrollment ID `af52be71-4d94-466b-86ed-ab2559e15c35`. The console lists that ID prefix under `content-pack-demo.local`. It is distinct from the installed Cloudspace service enrollment. Starting that older launcher would not be a demonstrated repair of the current installed service.

## Live demonstration

1. Refreshed Patch posture with Local Development selected.
2. Opened the existing deployment's per-node drawer.
3. Opened Servers, expanded Local Development, and opened Cloudspace.
4. Inspected node availability, recorded packages, and recommendations.
5. Matched the node to the local installed service using its state file.
6. Opened Deploy patches, selected only Cloudspace, and observed the enabled deployment button.
7. Cancelled the form without submitting it and left Patch posture visible.

## Observed posture

| Measure | Live value | Interpretation |
| --- | --- | --- |
| Fresh package inventory | 0 / 4 | All four inventoried nodes are stale. |
| Known critical findings | 0 | This is a recorded finding count; stale inventory does not establish current safety. |
| Known KEV findings | 0 | Current assessment/feed coverage was not established in this demonstration. |
| Known affected nodes | 0 | Does not establish that all nodes were successfully assessed. |
| Pending approvals | 0 | Summary also reports one expired approval. |
| Recent deployments | 1 | Requested 30 September 2026 at 11:15:19 as displayed locally. |
| Existing deployment | Pending, direct, 0 / 1 applied | Per-node drawer says no nodes received the deployment. |
| Completed deployments | 0 | No completed patch was demonstrated. |
| Online nodes | 0 / 4 | Enrollment state active is not the same as online availability. |

The expired approval and pending deployment were observed separately; their exact relationship was not established from the UI alone.

Cloudspace is explicitly marked **OFFLINE / STALE HEARTBEAT**, with last seen **12 October 2026, 16:12:10**, as displayed in Africa/Lagos local time. Its Packages tab shows **41 recorded application packages**, all from the Windows registry. These are historical records, not a new inventory scan triggered by this demonstration.

The node overview also says "The agent is connected" in its predictive-health explanation despite the offline banner. That text is inconsistent with the node's current availability and should be corrected with the shared evidence/freshness work.

The deployment form lists Cloudspace as active, allows its selection, and enables **Deploy to 1 node**. The form does not establish that this node is online, freshly assessed, or ready to execute. No submission or attempt to bypass approval occurred.

## Connection mismatch

The installed service points to the previous LAN address `192.168.1.10`, while the current machine and working console use `192.168.1.5`. A read-only HTTP probe to the configured old endpoint timed out after five seconds. The local API health endpoint responded successfully.

This is a concrete connection mismatch to repair before a fresh inventory or patch execution demonstration. A running Windows service alone does not prove that its heartbeat is reaching the control plane. Service logs were not available in the configured log directory during this check, so the precise repeated connection error was not independently confirmed from agent logs.

The server list also shows an agent update for Cloudspace still marked updating, started approximately 25 days ago. Its actual dispatch/terminal state needs inspection before reconnecting the agent. No update was queued or approved in this demonstration.

## Connection repair and resumed demonstration

After the user authorized fixing issues during the live demonstration, the following repairs were completed using the existing installation:

1. Inspected queued/running jobs for Cloudspace. The only matching active job found was agent update `737a5a6a-068f-4502-afb1-516e1285b97a`, created 24 September. Cancelled that stale running job through the existing authenticated job-cancel API, which records the cancellation in audit history.
2. Backed up `C:/ProgramData/ControlOne/nodeagent.yaml` and changed only its `api_url` line to `http://127.0.0.1:8443`. Preserved the same node ID, tenant, authentication material, and all other configuration. Restarted the existing `ControlOneAgent` Windows service using Administrator rights.
3. The Wi-Fi address changed again, from `192.168.1.5` to `192.168.1.4`, while the old Vite process still listened on `.5`. Changed only the bind-address default in the already-existing `scripts/local.ps1` to `127.0.0.1`, and restarted the existing Vite UI on loopback using the same startup settings and existing log/PID files. The explicit `-BindAddress` parameter remains available for a separately configured LAN setup.
4. Opened `http://127.0.0.1:4173/console/`, signed in with the existing local demo account, and visibly verified Cloudspace as **Connected / heartbeat current**.
5. Verified inventory improved from **0 / 4 fresh** to **1 / 4 fresh**, with three other nodes still stale. Cloudspace's package inventory shows 41 registry packages. API heartbeats continued advancing; one verified timestamp was `2026-10-20T09:10:08Z` (10:10:08 Africa/Lagos).
6. Ran the existing deployment-plan API for Cloudspace, mode auto, with post-patch rescan enabled. The real server preview returned direct mode and **approval_required**, reason **approval required**. This did not create a deployment or approval and did not install updates.

The agent and browser now use loopback for this same-machine setup, so those connections no longer depend on its Wi-Fi address. The API and UI health checks both returned HTTP 200. This does not automatically configure access from other computers.

The configuration backup used for the completed repair is `C:/ProgramData/ControlOne/nodeagent.yaml.before-loopback-repair-20261020090705.bak`.

A temporary PowerShell helper had been created for the first queued-job inspection. It was removed when the user requested no new `.ps1` files. All subsequent work used inline commands and the existing service/launcher; no new `.ps1` file remains.

## Revert loopback connections to the Wi Fi address

These are saved instructions for a future revert; saving them does not apply the revert. Use the machine's current Wi-Fi IPv4 address, not the historical `192.168.1.10` address unless it is currently assigned to this PC.

1. In the existing `C:/Projects/control_one/scripts/local.ps1`, change the default `$BindAddress` from `127.0.0.1` to the current Wi-Fi address. Keep the rest of the launcher intact.
2. Restart the UI with that bind address. The existing launcher returns early if it finds an already-running Vite process, so merely changing the default or running `start` again will not rebind that process. Stop the project's existing Vite UI process first, confirming it against `tmp/local/ui.pid`, then run the existing launcher from the project directory:

   ```powershell
   .\scripts\local.ps1 start -BindAddress YOUR-CURRENT-WIFI-IP
   ```

   Replace `YOUR-CURRENT-WIFI-IP` with the actual address before running the command. No new `.ps1` file is needed.

3. Open `C:/ProgramData/ControlOne/nodeagent.yaml` as Administrator. To revert the agent as well, change only:

   ```yaml
   api_url: "http://127.0.0.1:8443"
   ```

   to:

   ```yaml
   api_url: "http://YOUR-CURRENT-WIFI-IP:4173"
   ```

   Preserve its node token, certificate settings, state-file path, and all other configuration. Do not re-enroll the machine or start the older development-launcher agent.

4. In Administrator PowerShell, restart the installed service:

   ```powershell
   Restart-Service ControlOneAgent
   ```

5. Open `http://YOUR-CURRENT-WIFI-IP:4173/console/`. Select Local Development and verify Cloudspace shows **Connected / heartbeat current**, then check that its inventory remains fresh.

If the goal is only to let other devices open the UI, the agent on this PC can retain its loopback API address; changing that agent address is not required for LAN browser access. A full return to Wi-Fi-based connections can break again when DHCP assigns a different address.

The pre-repair backup is `C:/ProgramData/ControlOne/nodeagent.yaml.before-loopback-repair-20261020090705.bak`. It contains the old endpoint, so do not blindly restore the whole file when only the address needs changing. This connection revert does not undo the separately recorded cancellation of the stale agent-update job.

## Remaining execution steps

1. Verify successful vulnerability assessment with known feed coverage; a fresh inventory and zero recorded findings do not establish this by themselves.
2. Resolve the historical pending deployment and expired approval through the planned lifecycle fixes. The connection repair did not change their historical records.
3. Continue Phase 1 fixes and use a disposable target for actual installation and failure/recovery demonstrations. The current Windows patch executor updates WinGet applications; Windows OS servicing remains a separate support decision.

The Phase 0 checklist above supersedes the initial evidence gaps from this walkthrough. Runtime fingerprints, role assignments, recovery checks, and a real partial vulnerability assessment have now been checked. Exact build provenance, broad assessment coverage, and a confirmed disposable qualification target remain outstanding.

## Local screenshots

Screenshots are saved under the ignored local runtime directory, rather than committed to Git:

- `tmp/local/patch-posture-demo/patch-posture-baseline.png`
- `tmp/local/patch-posture-demo/windows-node-before.png`
- `tmp/local/patch-posture-demo/deployment-selection.png`
- `tmp/local/patch-posture-demo/windows-node-after.png`
- `tmp/local/patch-posture-demo/patch-posture-after.png`

Related plan: [Patch posture review and remediation plan](patch-posture-end-to-end-review.md).

## Live built-in browser walkthrough — 27 October 2026

Browser control became available in this continuation, superseding the earlier browser-access blocker. Visibly demonstrated Local Development > phase0-ubuntu-lab: connected/current heartbeat, installed agent phase0-signed-lab-20261023, Packages filtered to liblzma5 at 5.6.1+really5.4.5-1ubuntu0.4. Opened Patch posture > latest completed deployment > Per-node: deployment 2d7d2a1c, node d033fa85, APPLIED, PKGS 1, and the historical 23 October package-manager log showing ubuntu0.3 -> ubuntu0.4. The dashboard still reports partial assessment coverage. Recommendations says no open recommendations matched; this alone does not establish finding-resolution provenance or complete assessment.

Prepared a deployment form preview with only phase0-ubuntu-lab selected, liblzma5 allowlisted and inventory refresh enabled, then cancelled it. No job was submitted, no package was upgraded today and the new Phase 1 candidate was not installed. This closes the visible walkthrough of existing app evidence only; the candidate-installation/heartbeat demonstration checkbox remains open. Screenshots: tmp/local/patch-posture-demo/phase1-20261027/live-app-deployment.png and live-app-deployment-preview.png. Phase 0 and Phase 1 remain open. See [Phase 1 execution evidence](patch-posture-phase1-execution-2026-10-27.md) for candidate status and remaining work.

## Controlled Phase 1 lab rollout and visible verification — 27 October 2026

This checkpoint supersedes earlier statements that the candidate is staged only or that installation/live verification is pending. The enrolled Ubuntu lab now runs **phase1-journal-qualified-20261027 (linux/amd64)**. The built-in browser visibly confirmed the same node d033fa85-5796-48ae-8e8a-f82706387326 under Local Development, CONNECTED / HEARTBEAT CURRENT, with Last seen advancing to 27/10/2026 18:00:34 Africa/Lagos after recovery checks. No patch job was submitted and no package was upgraded in this rollout.

- [x] Recheck candidate SHA-256 912BAD267AF8A22C19ACC3AE5E45417D14E85CBDBD910B0DAE29B21C408A72F4 and independently verify its signature against the lab's existing pinned release public key. The journal source hash still matches the qualified build record.
- [x] Confirm no unfinished job targets the lab and no package-manager process is running. Initial preflight queries used nonexistent column names; inspecting the schema and using the jobs payload produced the successful zero-row check. No database data was changed by these queries.
- [x] Preserve old executable, configuration and agent state under /var/lib/control-one/rollback-phase1-20261027 in the same private lab container; atomically replace the existing executable and restart the same container without enrollment/configuration changes.
- [x] Verify the installed hash matches the candidate and configuration remains FB9C597D247B27728656691107DC33218F5E0F0119D0CA00E839F69A84D87030. Execution journal hash remains E2E3F7BEA658E57AE291A6D589A7934EDA73C459412DA66F8A84A88AF76E7997: three retained entries, zero pending reports.
- [x] Verify the current cached signed policy using the existing pinned policy key; altered-version, unsigned and wrong-key copies are rejected. The cache is an empty rule set, so this is signature/cache continuity, not proof of policy enforcement across all rule types.
- [x] Abruptly stop the idle lab with SIGKILL, explicitly start it again, then briefly disconnect only this lab's Docker network for 12 seconds and reconnect in a finally block. Verify unchanged journal/configuration and a later live heartbeat. This proves idle restart and reconnection only; it does not qualify interruption during a package upgrade, an unacknowledged completion or host power loss.
- [x] Retain private local image controlone-phase0-patch-lab:20261027-phase1-journal-qualified, ID sha256:165114098def025558550bdc8b42e40d88b5622b8d6ad92b52d2dfe247f0a225, and update the existing local Compose image reference. This enrolled image must not be published or run as a duplicate identity. It is a local checkpoint, not an external backup.
- [x] Later bounded qualification supersedes this checkpoint: completion/receipt/rollup recovery, database dispatch concurrency and controlled real post-install interruption/overlap passed. See P1-02 through P1-08 in [the current checklist](patch-posture-phase-acceptance.md#phase-1--secure-execution). Real mid-install interruption, additional filesystem faults and full-host/support qualification remain unchecked there.
- [ ] Complete filesystem/power-loss and supported OS/mode qualification. Existing full-host VM and other Phase 0 evidence gaps remain open.

To repeat the visible verification: select Local Development > Servers > phase0-ubuntu-lab > Overview; check the version, node ID and advancing Last seen after Refresh. Screenshot evidence is tmp/local/patch-posture-demo/phase1-20261027/agent-rollout-before.png and agent-rollout-after.png. The overview still correctly reports incomplete authentication-log and file-access coverage; startup briefly showed stopped collectors before their readiness reports settled. Do not describe this container as a fully qualified Ubuntu host.

Rollback of the executable, if required, uses the retained agent at /var/lib/control-one/rollback-phase1-20261027/agent, an atomic replacement of /usr/local/bin/controlone-agent.candidate and restart of the same container, followed by version/heartbeat verification. Preserve the current execution journal: blindly restoring an older journal can discard later reservations and permit duplicate execution. The local Compose reference must follow the chosen qualified image if a future recreation is required. No new .ps1 file, Windows-service change, commit or push was made in this rollout. Phase 0 and Phase 1 remain open.

## Completion recovery hardening — 27 October 2026

This continuation fixes the previously documented gap between core completion and its receipt/deployment summary. Migration 166 adds patch_completion_effects, a per-job durable acknowledgment marker. CompletePatchJob now commits node state, job state, terminal event, action receipt, action-plan state, parent summary and marker in one transaction. The parent row lock serializes summary calculations across different nodes; approval outcomes use the same locked summary path. A matching legacy terminal retry without a marker reconciles missing effects using saved package count/log/error, rather than changed retry metadata. A fully committed retry avoids rewriting receipts and timestamps. Acknowledgment requires the marker, so a failed receipt/summary/marker write cannot suppress the agent's retained result.

Dispatched patch jobs now reject the generic cancellation endpoint with HTTP 409 for direct, proxy and airgapped actions. Marking a database row cancelled cannot stop an OS package-manager command and can race away its real outcome. Approval denial before dispatch remains available; this safeguard does not add operating-system process cancellation.

Verified evidence:

- [x] Expanded database fault matrix: failures at node state, job, event, receipt, action plan, summary and acknowledgment marker all roll back; eight identical reports commit once. Separate concurrent node outcomes using direct/proxy/airgapped labels converge to partial, and summary retries retain the original completion time. Final shared-rollup storage matrix passed three runs in an explicitly disposable database. Mode labels in this database test do not establish real package behavior on every OS/mode.
- [x] Server regression tests cover reconciliation before acknowledgment, marker-read/storage failures, inventory refresh on matching retries, operator and administrator denial across six unassigned-tenant routes, changed isolation/policy approval denial, and dispatched cancellation rejection across three modes and queued/running/succeeded job statuses.
- [x] Real Linux dispatcher tests restrict PATH to a temporary fake apt-get: 40 repeated deliveries of two jobs yield two fetches and four serialized command invocations. Lost acknowledgment retains both outcomes after journal reopen. A separate dispatcher subprocess killed during its fake command recovers an explicit unknown-outcome failure and refuses re-execution. No system package manager is selected by these tests.
- [x] Live authenticated replay of the existing 23 October liblzma5 completion: first HTTP 200 response deliberately discarded without processing; control plane restarted; first retry received a transient startup HTTP 502; retry after direct/proxy health readiness received HTTP 200 and the same job acknowledgment. Receipt count stayed one, marker count moved from zero to one, original package count stayed one, job remained succeeded, node applied and deployment completed. Lab configuration/journal hashes stayed unchanged. This is controlled acknowledgment-loss/restart evidence for an existing result, not a new package upgrade or a socket-fault injection during dpkg.
- [x] Built-in browser demonstration recovered in a fresh tab after the original tab stopped responding. Patch posture > latest completed deployment > Per-node still shows APPLIED / PKGS 1 and the original 23 October timestamp/log. Screenshot: tmp/local/patch-posture-demo/phase1-20261027/completion-recovery-live.png. The dashboard now reports Vulnerability feed expired; historical findings are retained. This assessment gap remains open separately.
- [x] Both task-created disposable databases were removed. A local pre-migration dump is retained at tmp/local/patch-posture-demo/phase1-20261027/controlone-before-effects.dump, SHA-256 C24C1A3533B889817D539578C9A0212AC152B7DA042850BEBC2E6338851048F0. This is a local checkpoint, not off-machine recovery evidence.
- [x] The defined dispatch/wave/connection-loss matrix and controlled post-install real-package interruption/overlap were subsequently qualified; see P1-02 through P1-08 in [the current checklist](patch-posture-phase-acceptance.md#phase-1--secure-execution). P1-09 through P1-12 remain unchecked; this does not establish real mid-install or power-loss recovery.

One mixed-mode test initially used unsupported parent mode auto; changed only that test fixture to the schema-supported direct value and retained the database constraint. A redundant older test compilation was stopped to reduce RAM pressure; the corrected storage runs and final server suite are recorded separately. No failure injection was performed against the live database. No new .ps1 file, Windows-service replacement, Git commit or push was made.

Repeat the visible check by selecting Local Development > Patch posture > the 23 October 11:37:53 deployment > Per-node. Verify node d033fa85, APPLIED, PKGS 1 and the original applied time. The one-receipt/one-marker assertions are database qualification evidence; the app does not currently expose those counts as controls. Do not describe a normal browser refresh as performing fault injection.

Current memory check during this continuation: 15.68 GiB total, 0.44 GiB free. The prepared VM needs 4 GiB; it was not started and user applications were not closed to reclaim memory. This is a snapshot during qualification, not a permanent machine capacity estimate. Phase 0 remains open for the recorded production trust, external recovery, full-host and assessment gaps. See [Phase 1 acceptance and evidence](patch-posture-phase1-execution-2026-10-27.md).
### Final deployed checkpoint for completion recovery

The final shared-rollup candidate passed the patch server suite and the seven-fault/concurrent-node storage matrix (three runs), then was deployed through the existing local Compose override. Running executable: tmp/local/patch-posture-demo/phase1-20261027/controlplane-effects-qualified-final; SHA-256 **D71C276CCC1AE47410CBEA81DA7F0E76FBEAE2039ADE0FDA047C9FCCE11FBDE8**. The earlier first-effects binary remains retained as a checkpoint; the 23 October qualified binary and original pre-effects launcher copy are also retained. Migration 166 is additive; reverting the executable does not require deleting completion evidence or automatically restoring an older database. Later live writes must not be discarded by an unreviewed database restore.

Final direct/proxy health checks passed. An additional authenticated retry on the final candidate again returned the same acknowledgment; database assertions remained succeeded / applied / PKGS 1 / completed / one receipt / one marker. Refreshed and reopened the result in the built-in browser and saved completion-recovery-live.png. Runtime replay helper and temporary PostgreSQL dump copy were removed; private workspace evidence is retained. Final free-memory snapshot after builds: **0.79 GiB of 15.68 GiB**. The VM was not started.

Evidence files in tmp/local/patch-posture-demo/phase1-20261027/: completion-effects-rollup-final.txt, server-rollup-final.txt, authorization-cancellation-final.txt, execution-process-final.txt, completion-effects-live-summary.json and completion-recovery-live.png. This closes the defined completion-recovery defect and its checked scenarios. It does **not** close the remaining real-package interruption/overlap, concurrent-wave/dispatch recovery or full-host/power-loss/platform gates. Phase 1 remains open, as does Phase 0. No new package upgrade, .ps1 file, commit or push was made.
## Latest Phase 1 qualification checkpoint — 27 October 2026

This section supersedes the earlier combined unchecked interruption/dispatch rows for the specifically tested cases. The full acceptance record and repeatable visible steps are in [Phase 1 dispatch recovery and real-package interruption](patch-posture-phase1-execution-2026-10-27.md#dispatch-recovery-and-real-package-interruption--27-october-2026).

- [x] Deploy atomic approval/action-plan/job/event/state-link dispatch. Concurrent identical dispatches converge to one linked job; a rejected duplicate cannot fail an active linked state. A database session lock prevents concurrent wave advancement and releases after connection death.
- [x] Pass the isolated PostgreSQL dispatch rollback/concurrency matrix, including actual backend termination during link creation, three times. Reconcile stale unlinked/legacy orphan intents to explicit failure; preserve fresh intents and valid pending approvals. Server patch/action-plan regressions pass. The disposable database was removed.
- [x] Demonstrate in the built-in browser an approved zlib1g-only upgrade on phase0-ubuntu-lab, version 1:1.3.dfsg-3.1ubuntu2.2 -> 1:1.3.dfsg-3.1ubuntu2.3. Submit and approve a second same-node request while the first pauses after dpkg and before completion. Both reservations exist, only one package workflow enters the post-install hook. After SIGKILL/start, both report failed / outcome unknown / manual reconciliation required, are acknowledged and do not rerun. New version retained; dpkg audit healthy. This is a controlled post-install container interruption, not mid-dpkg or host power loss.
- [x] Fix the existing app's node patch editor: choose mode, select the existing repository proxy when required, then explicitly Save patch settings. Preserve proxy/window associations unless explicitly changed; failed configuration loading blocks saving. Sixteen UI tests and the production UI build pass.
- [ ] Qualify full-host and filesystem/power-loss durability, mid-package-manager interruption/recovery, and the intended supported OS/mode scope. These gates still prevent blanket Phase 1 completion or wider production rollout.

The app still reports Vulnerability feed expired; package installation evidence does not establish current vulnerability assessment or complete Phase 0. No new .ps1 file, Windows-service replacement, commit or push was made. Existing local service/configuration paths and enrollment were retained.
### Final verified post-recovery checkpoint

- [x] Fresh separately approved deployment f3fb9aca-8698-42b7-9275-b9ec32d2ce18 completed; job b9ac085c-4e4e-4292-a4e7-7cb22608ee88 succeeded; app shows APPLIED / PKGS 0. Zero is correct: the interrupted first request already installed the new zlib1g version. This request confirms recovery, not another version upgrade.
- [x] Each of the two interrupted jobs and the fresh verification has exactly one receipt and one completion marker. All six retained journal entries are acknowledged; zero pending reports. Original failed/unknown history is preserved. The post-install invocation log still contains one line, with no re-execution of either interrupted reservation.
- [x] Built-in browser > same Ubuntu node > Packages > filter zlib1g shows 1:1.3.dfsg-3.1ubuntu2.3, with connected/current heartbeat. Evidence: real-zlib-inventory-live.png and post-recovery-verification-live.png under tmp/local/patch-posture-demo/phase1-20261027/.
- [x] Remove temporary APT hook/ready marker, verify healthy dpkg audit, restore node config to direct / no proxy / no window through the fixed app editor, and stop the existing repository proxy as before. Agent config hash remains FB9C597D247B27728656691107DC33218F5E0F0119D0CA00E839F69A84D87030.
- [x] Retain a private post-test recovery image controlone-phase0-patch-lab:20261027-dispatch-interruption-qualified, ID sha256:a0296855650bfdc21a15e475a24e4a00ffb7187abdf0b4ee6646768620bda9d1, and update the existing local Compose image reference. Current journal SHA-256 E2F8591E00BBC3B88E05586493F3FCABBBE54D49783CFEB4C88B58464E0198C6. Do not publish this enrolled image, run duplicate identities, or restore the pre-test journal over later reservations.

The agreed checks that can use the current disposable lab are now recorded as passed within their stated scope. **Phase 1 is still open** for full-host/filesystem power-loss, mid-package-manager interruption/recovery and supported OS/mode qualification. Phase 0's external recovery, managed production trust, full-host and maintained assessment coverage gates remain open. No new .ps1 file, Windows-service replacement, commit or push was made.

## Patch UI revalidation — 27 October 2026

Retested the previously blocked UI runner in response to the readiness review. The dependency-link permission issue did not recur with the existing approved toolchain access; tests were collected and executed successfully.

- [x] PatchManagement.test.tsx: 13 passed; PatchManagement.config.test.tsx: 3 passed (16 total, two files, exit 0). Covers scoped deployments, partial-load states, destructive-action confirmations/failures, explicit settings Save, required proxy, preserved associations and settings-load failure.
- [x] Focused ESLint on the patch page and both test files: exit 0, no lint errors. Existing eslintrc configuration emits a deprecation notice; no rules were disabled to pass the check.
- [x] npm run build: TypeScript and production Vite bundling passed, 2,794 modules, exit 0. Patch page chunk 44.31 kB / 11.03 kB gzip. This verifies build output, not load testing or every supported browser.
- [x] Visible built-in-browser smoke check: page data loads; expired feed is clearly disclosed; the completed result remains APPLIED / PKGS 0. Proxy mode without a proxy disables Save with an explanation; choosing the existing proxy enables explicit Save. Closing without Save and reopening preserves direct / no proxy. No deployment or settings mutation was submitted by this retest.

Retained evidence under tmp/local/patch-posture-demo/phase1-20261027/: ui-retest-20261027.txt, ui-eslint-retest.txt, ui-production-build-retest.txt, ui-retest-source-hashes.json and ui-retest-proxy-required.png. These results supersede the earlier before-test-collection permission blocker. They support the tested patch UI behavior; they do not certify the entire product for production or close the remaining Phase 0 / Phase 1 host, platform and operational gates. No source change, new .ps1, commit or push was needed for this retest.

Self-service instructions: [repeat the fresh recovery verification](patch-posture-self-service-recovery-guide.md), including package health, proxy setup, approval scope, result checks and cleanup. The earlier guide-save permission-review blocker is cleared; this link now targets the saved repository guide.


## Approval package visibility and saved operator guide — 27 October 2026

The approval API now adds package allowlist/denylist and an explicit availability flag from the tenant-owned deployment. The app's Approvals table adds Package scope: named packages (for example zlib1g), All eligible packages for an empty allowlist, exclusions and a dependency warning. Missing/legacy/unreadable or foreign-tenant evidence is shown as Package scope unavailable rather than inferred as all packages. The independent bound-policy validation still runs before dispatch; displaying names does not bypass it.

The [self-service recovery guide](patch-posture-self-service-recovery-guide.md) is saved and linked from all related trackers. It covers the existing proxy, package health/candidate checks, fresh scoped request, approval, three result fields, inventory verification and cleanup. No new .ps1 file is used. Browser control is disabled in Work settings in this continuation, so a new visible demonstration is not claimed. Validation and local deployment status follow below.

Validation: all 19 patch UI tests pass, focused ESLint passes (existing config deprecation notice only), TypeScript/Vite production build passes, and focused server approval tests including scope ownership/legacy handling pass. Evidence files are approval-scope-ui-tests.txt, approval-scope-lint.txt, approval-scope-ui-build.txt and approval-scope-server-tests.txt under tmp/local/patch-posture-demo/phase1-20261027/. The first binary-build command used the wrong entrypoint and failed without altering the running service; the corrected entrypoint is controlplane/cmd/controlplane.


Local deployment confirmed: corrected build passed; existing candidate launcher now runs tmp/local/patch-posture-demo/phase1-20261027/controlplane-approval-scope, SHA-256 05E52519C9CC05C65BEF38D8C44634C9C24094D79C6905124C2BE3C40EF58AD9. Direct API and CA-verified lab HTTPS health passed after refreshing the existing nginx upstream. UI production bundle includes the Package scope column; refresh the browser to load it. Browser automation remains disabled, so no new live screenshot/demo is claimed. No new package deployment, .ps1 file, Windows-service replacement, commit or push was made. Phase 0 and Phase 1 remaining host/platform/operational gates are unchanged.
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

## Phase 3 proxy recovery and calendar safety follow-up

Actual stopped-proxy failure and a fresh separately approved recovery were demonstrated in the app. Proxy activation validates before replacement and recovers failed activation; calendar-only windows bind start bounds while unqualified network windows are blocked. Force-close does not claim physical firewall closure or stop running installations. Offline/platform/full-host/proxy lifecycle gates remain open. See [latest evidence and steps](patch-posture-phase3-platforms-2026-10-29.md#proxy-recovery-and-calendar-safety-follow-up) and [acceptance checkboxes](patch-posture-phase-acceptance.md#phase-3-proxy-recovery-and-calendar-safety-follow-up).

The live calendar follow-up also exposed and fixed missing worker enqueue. Queue-level tests passed; the app demonstrated OPEN followed by force-closed CLOSED, and obsolete demonstration jobs were cancelled through Jobs. P3-05d records this bounded evidence; complete Phase 3 remains open.

Compatibility follow-up: new windowed jobs require the authenticated agent runtime capability `calendar_window_start_check`. Older agents cannot silently ignore the new bounds. The capability is bound to approval/dispatch and independently rechecked under the node lock; ordinary unwindowed paths keep their existing availability. See the Phase 3 tracker for the final deployed qualification.

## Client explanation and live walkthrough - 29 October 2026

A fresh lab calendar example and an intentionally denied lab-only liblzma5 request demonstrated scheduling and the approval boundary. Retained real proxy failure/recovery/offline rejection records were explained in the app, including why APPLIED / PKGS 0 and verification unknown do not authorize promotion. The training request has no execution job; node settings, installed packages and journal stayed unchanged. No new acceptance gate was closed. See [realistic use cases, repeatable steps and exact demonstration limits](patch-posture-self-service-recovery-guide.md#client-demonstration-with-realistic-reasons).

## Offline repository foundation - 29 October 2026

The existing Ubuntu container passed signed file-only APT refresh and read-only package simulation using the new `internal/offlineapt` package. Normal source parts, ambient APT configuration/hooks/proxy variables and network acquisition methods were excluded. The native positive case refreshed actual Ubuntu signed metadata with the existing proxy stopped; the HTTP trap and injected command hook were unused. Invalid signatures, changed or missing indexes, unavailable repositories, expired signed metadata and an unavailable package were rejected. A failed refresh also invalidated simulation eligibility despite retained index files.

This is checked as bounded P3-03g only. It does not implement or qualify offline package installation. P3-03e, parent P3-03 and Phase 3 remain open; the app continues refusing offline execution. [Detailed implementation, evidence and remaining work](patch-posture-phase3-platforms-2026-10-29.md#offline-repository-foundation---29-october-2026). No new VM, dependency, PowerShell script, enrollment or credential was created. Local test artifacts remain excluded from commits.

## Offline snapshot and archive checkpoint - 29 October 2026

Bounded P3-03h passed on the existing Ubuntu lab: exact requested versions and staged repository bytes are pinned to a manifest digest, with keyring and installed-state baseline checks. Real file-only archive acquisition passed with the proxy stopped; changed/missing archives, unavailable versions and an isolated missing-dependency case were rejected. These were simulation/download-only checks. Approval/dispatch integration, complete resolved dependency manifest, real offline installation and recovery remain open under P3-03e; airgapped execution remains blocked in the app. [Implementation and exact evidence limits](patch-posture-phase3-platforms-2026-10-29.md#offline-snapshot-and-archive-checkpoint---29-october-2026).

## Offline approval identity follow-up - 29 October 2026

The offline content identity is now carried in source through the request/persisted deployment policy, approval policy hash, action-plan diff, approval response and agent payload. It includes the manifest, installed-state and keyring hashes, repository selectors and exact requested versions. Changed identity fields alter the approval hash; malformed/mixed-mode content is refused. Snapshot preflight requires all approved fields to match its staged identity. Storage now unconditionally blocks offline dispatch even when legacy payloads omit runtime evidence, and the obsolete default-path dispatch fallback was removed.

This is tested preparatory plumbing, not an enabled offline workflow. No offline approval or install is allowed; no new acceptance parent was closed. Protected persistent staging, resolved dependency closure, operator review, actual install/journal/recovery integration and real remediation evidence remain required under P3-03e. [Detailed implementation and validation](patch-posture-phase3-platforms-2026-10-29.md#offline-approval-identity-follow-up---29-october-2026).
