# Phase 1 non-VM acceptance evidence — 27 October 2026

> **Current Phase 2 qualification, 28 October 2026:** P2-01 through P2-10 software-contract checks are qualified using the existing Ubuntu lab and isolated tests. [Implementation, test evidence, live demonstration and scope limits](patch-posture-phase2-state-2026-10-27.md#complete-state-contract-qualification--28-october-2026). Full-host reboot/platform, external recovery, managed production trust and production rollout gates remain open in their respective phases. Earlier checkpoints below are historical; use the [canonical acceptance checklist](patch-posture-phase-acceptance.md) for current status.

> **Offline bundle checkpoint, 28 October 2026:** Token eligibility and authenticated downloads are fixed; 15 UI/API tests, targeted backend tests, TypeScript, lint and production build passed. [Dropdown sources, repeatable steps and evidence limits](patch-posture-self-service-recovery-guide.md#offline-bundle-token-eligibility-and-dropdown-sources--28-october-2026). Final live browser recheck passed on 28 October: expired/revoked entries remain disabled after Reload tokens, no old token is selected, and Download bundle is disabled with an empty raw token field. Screenshot: tmp/local/patch-posture-demo/token-picker-20261028/token-picker-final-live.png. No overall phase closure or new credential is claimed. That token-picker checkpoint used controlplane-token-qualified (SHA-256 1761B04CD5F3D3C076614756671FDF091AC361C3B0A42B3D3C89FDFEA38AE86F), with its prior candidate retained for recovery; see the later P2-06 checkpoint for the current runtime selection.

The defined P1-09 and P1-10 checks passed on the existing local disposable lab. **Phase 1 as a whole remains open** for P1-11 full-host/storage power-loss and P1-12 supported OS/mode qualification. This report does not certify physical storage durability, Windows servicing, reboot recovery or production readiness.

## P1-10 — Persistence failures and directory sync

Review found that a failed completion write retained a result in memory that pending() could expose before the result reached disk. The replacement file was synced, but its parent directory was not synced on Unix. The journal now tracks an unpersisted snapshot, retries persistence before reporting/acknowledging it, retains execution reservations and syncs the Unix parent directory after replacement. Failed acknowledgment writes roll back their in-memory acknowledgment; a failed snapshot remains withheld until persistence recovers. A short write is an error.

Windows retains its previous file-sync behavior; Unix directory-sync guarantees are not claimed there. No directory-flush or physical power-loss acceptance is inferred from native Windows tests. P1-11/P1-12 remain unchecked.

### Qualification

- [x] Three operations (reserve/result/acknowledgment) × eight save stages (mkdir/create/chmod/write/file sync/close/rename/directory sync) × three injected error classes (ENOSPC/EIO/EROFS): 72 cases per run, three Linux runs. Pre-replacement failures preserve old bytes; post-rename sync failure is explicitly uncertain and not acknowledged as persisted. Temporary files are cleaned up.
- [x] Failed reservation does not fetch or execute a fixture package command. Failed completion/ack snapshots cannot be reported; after fault removal, retained results persist and reopen correctly, acknowledgment retries converge, and an existing job remains reserved.
- [x] Actual disk-full errors on a dedicated **1 MiB tmpfs**, plus actual read-only-mount errors, three runs each in a network-isolated disposable container. Host disk was not filled. Read-only startup rejects the journal without modifying it; disk-full retains old evidence and recovers after removing the bounded filler.
- [x] Native Windows journal/fault/identity tests passed, including a temporary clone of the six-entry lab baseline. Windows service/enrollment was not replaced.
- [x] The Linux journal/dispatcher suite passed three runs, including baseline compatibility, corrupt-evidence rejection, duplicate delivery, serialization and interrupted fixture-command recovery.

Initial execution of the isolated filesystem test binary had incorrectly quoted test arguments on Windows and ran no tests; the corrected invocation passed. An older acknowledgment test initially expected reporting during a persistence outage; it was updated to assert retained-but-withheld evidence and successful durable retry. The passing final logs, rather than either failed attempt, establish acceptance.

Evidence root: `tmp/local/patch-posture-demo/phase1-nonvm-20261027/`. Logs: `journal-linux-final.txt`, `journal-windows.txt`, `real-filesystem-faults-final.txt`. Injected EIO and operation failures model syscall boundaries; they are not a claim that a real storage device was physically failed.

## Controlled candidate rollout

Built Linux/amd64 agent `phase1-storage-qualified-20261027`, SHA-256 **4B7BC75E44BD15F474CB147B40739C34A302458C9F7FDC2C1BE31DA2229DA351**. Signed its digest using the existing local release key, then independently verified staged bytes in the lab against its administratively provisioned pinned public key. Existing executable/config/journal were retained under `/var/lib/control-one/rollback-phase1-storage-20261027`; replacement used the existing `/usr/local/bin/controlone-agent.candidate` path and same container enrollment.

The app visibly showed the new version on node `d033fa85-5796-48ae-8e8a-f82706387326`, connected/current heartbeat. Config SHA-256 stayed **FB9C597D247B27728656691107DC33218F5E0F0119D0CA00E839F69A84D87030**. Evidence: `release-verification.txt`, `storage-agent-live.png`. This is local pinned-signature evidence, not managed production release governance.

## P1-09 — Real installation interrupted before completion

Target: Local Development / phase0-ubuntu-lab only. Official Ubuntu package **libbrotli1 1.1.0-2build2 -> 1.1.0-2ubuntu0.1**. Fresh APT metadata passed strict error handling after the existing proxy became ready. Simulation showed one upgrade, no additions/removals. Downloaded package SHA-256: **70B188A86D81F30D7510AAB30E25D638ED5037DDED6691AF1533952B71AC61DE**.

The first metadata attempt hit proxy startup connection refusal and was not counted as success. No TLS or repository-signature checks were disabled. The proxy/node config was restored after the drill.

### Observed sequence

1. Retain a private pre-test container checkpoint, current journal and local database dump; no active patch job targeted the lab. Database dump SHA-256: **4FDBADCCFF8AB32AE568E76990447EE68C891920C343FC74AAB511353B004B0E**.
2. Add a temporary, package-specific dpkg status observer inside the disposable lab. It stops the actual dpkg ancestor only after the libbrotli1 `half-installed` status event. This leaves the original official .deb unmodified and does not replace apt/dpkg with a fake command.
3. Visibly submit/approve a fresh libbrotli1-only request in the built-in browser, auto mode using the existing registered proxy, inventory refresh enabled. The earlier zlib1g display request remains unapproved.
4. Observe **`Status: install reinstreq half-installed`** while the actual dpkg `--unpack` process is stopped. The audit reports a package needing reinstallation and pending libc triggers. Save status events, process evidence, the durable reservation and a snapshot of `/var/lib/dpkg`/package log. This is an actual mid-unpack interruption point, not the earlier post-install APT hook.
5. SIGKILL/start only that disposable container. Remove the temporary APT hook immediately. No apt/dpkg process restarts automatically. The journal records **failed / agent restarted during reserved patch execution; outcome unknown, manual reconciliation required**. The database commits one receipt and one completion marker; the app visibly shows the failure.
6. Diagnose the retained half-installed state. Verify the exact cached official package hash again; perform an explicit operator recovery with dpkg --unpack on that file and dpkg --configure --pending. Final audit is empty and status is **install ok installed**, version 1.1.0-2ubuntu0.1. This is a controlled manual package repair, not a claim of automatic repair by Control One.
7. Visibly submit and approve a separate scoped recovery-verification request. It completes **APPLIED / PKGS 0**, because the operator repair already installed the selected version. The interrupted result remains failed; it is not rewritten as success.
8. Verify later inventory, no pending journal reports, one receipt/marker per new job, no additional observer invocation, and no remaining package process. Restore direct/no proxy/no window through the app; stop the existing repository proxy; remove temporary observer/marker and staging files; retain the latest execution history and private post-test checkpoint.

| Request | Deployment | Job | Observed outcome |
| --- | --- | --- | --- |
| Real mid-install drill | 25b0e563-e6bd-4de6-b60e-1bf92cf98ec0 | 3ce3ff63-e387-4768-ba11-6adff51629e7 | Failed / unknown after restart; one receipt/marker |
| Separate recovery verification | 632ae611-bd7b-47bb-9cec-fe4f94fef0c8 | 80b1206f-93c5-4d65-b143-76d2a930b8f6 | Completed / applied / PKGS 0; one receipt/marker |

Browser times are Africa/Lagos: drill requested 21:55:12; interrupted failure recorded 22:08:34; recovery verification requested 22:10:02 and applied 22:11:34. The operator console session expired during preparation; normal sign-in using the existing local development account restored access. No new credential or enrollment was created.

Evidence: `midinstall-observed-state.txt`, `dpkg-status-events.log`, `midinstall-dpkg-state.tar`, `journal-during-midinstall.json`, `journal-after-midinstall-restart.json`, `after-interruption-audit.txt`, `package-recovery.txt`, `journal-final.json`, `live-recovery-receipts.txt`, `final-package-health.txt`; screenshots `midinstall-pending-live.png`, `midinstall-failed-live.png`, `midinstall-recovery-applied-live.png`.

## Recovery checkpoint and limits

Private pre-test image: `controlone-phase0-patch-lab:20261027-before-midinstall`, ID `sha256:4dcbe53ff7f574dadf7f43fdcd99d757001b2a225be9ce19916cf172828de34b`.

Private post-test image: `controlone-phase0-patch-lab:20261027-midinstall-storage-qualified`, ID `sha256:1287843800a8ccf274c4d15427f931b5dd89fdaaa34466675a0d2c45578d9983`. Existing local lab Compose reference updated. Final journal SHA-256 **D637E22EB71A5B7495E91EF82BCE6AB3F7E1C614962749FD6827541A8AEC020D**, eight retained acknowledged entries, zero pending reports. Never publish these enrolled images, run duplicate identities, or restore an older journal over later reservations. These are local checkpoints, not off-machine backup qualification.

Full-host power loss, physical storage guarantees, every possible package maintainer-script stage and the full OS/mode matrix remain unqualified. No new .ps1 file, Windows service replacement, Git commit or push was made. Phase 0 readiness remains open. [Current acceptance checklist](patch-posture-phase-acceptance.md); [normal operator guide](patch-posture-self-service-recovery-guide.md).

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
