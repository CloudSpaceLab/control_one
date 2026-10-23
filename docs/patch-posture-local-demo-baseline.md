# Local Development patch posture demonstration

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
