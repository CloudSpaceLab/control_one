# Patch posture review and remediation plan

Review date: 20 October 2026. Source baseline: `a56a6b48640df42e9999d636fec092b3bd2be49d` and the working tree present during review.

Control One has substantive patch-management components, but the current workflow does not yet justify a claim of verified enterprise patch compliance. The first fixes should address authorization, execution reliability, finding reconciliation, and maintenance-window enforcement. Dashboard improvements should follow those foundations.

This is an end-to-end source review, with local tests where executable. It is not a production penetration test or certification assessment. No production patch deployment, reboot, firewall change, or package installation was performed. Existing changes to `ui/vite.config.ts` and the untracked `compose.dockerhub-demo.yml` were left untouched.

## Current workflow

| Stage                    | Implemented behavior                                                                                                                                  | Material boundary                                                                                                                                                          |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Inventory                | Agent collects installed packages; hashes reduce heartbeat payloads; full inventory is refreshed periodically or on request.                          | Linux dpkg/rpm, Windows uninstall registry, and AIX inventory exist. macOS has no first-class package collector here. Collection support does not establish patch support. |
| Vulnerability assessment | Signed offline feeds match installed packages and app dependencies. Findings carry CVE, version, severity, CVSS, EPSS, KEV, references, and evidence. | Coverage depends on installed feed content, package identity, and version semantics. No-match does not establish a successful assessment of every asset.                   |
| Recommendations          | Node vulnerability plans group package findings and prioritize KEV before severity.                                                                   | These are proposals; fixed-version and verification instructions are not an enforced deployment contract.                                                                  |
| Planning                 | Backend supports dry run, canaries, explicit waves, package lists, and a post-patch inventory request.                                                | Main deployment form submits nodes, mode, and reason; it does not expose those backend controls.                                                                           |
| Safety                   | Creation applies isolation/manual-only checks, tenant change windows, circuit breaker, and approval.                                                  | Read errors can select defaults or skip a check; approval dispatch bypasses the non-approval gates.                                                                        |
| Delivery                 | Per-node patch state references a job; heartbeats deliver the action; the agent fetches its payload.                                                  | Delivery is repeated while patch state remains pending. No patch execution lease or agent deduplication is present in this path.                                           |
| Execution                | apt/dnf/yum upgrades on supported Linux systems; WinGet upgrades applications on Windows; apt-only airgapped path.                                    | Windows OS servicing, reboot coordination, verified recovery, and application health gates are not provided by these commands.                                             |
| Results                  | Agent queues completion metadata; heartbeat updates per-node and deployment state and creates action receipts.                                        | Completion ownership, durable acknowledgment, and distinction between installed and verified require fixes.                                                                |
| Reassessment             | Full inventory sync calls offline vulnerability matching and reconciliation.                                                                          | Version changes/removals are not reconciled correctly; deployment completion does not wait for reassessment.                                                               |
| Reporting                | Patch summary queries full database counts; tenant UI separately labels latest 50 deployments.                                                        | Control Room uses a different score; inventory freshness is not scan/feed confidence.                                                                                      |

Primary paths: [agent inventory](../cmd/nodeagent/inventory.go), [agent heartbeat](../cmd/nodeagent/heartbeat.go), [server heartbeat](../controlplane/internal/server/heartbeat.go), [feed import and refresh](../controlplane/internal/server/vulnerability_feed_import.go), [patch API](../controlplane/internal/server/patch.go), [approvals](../controlplane/internal/server/patch_approvals.go), [agent execution](../cmd/nodeagent/patch_exec.go), and [patch UI](../ui/src/pages/PatchManagement.tsx).

## Findings and qualification gaps

Priorities indicate remediation order: P1 is a high-priority security or operational correctness defect; P2 is a significant control or reporting gap. These are engineering priorities, not CVSS ratings. The scenarios below follow from the code; exploit attempts against a running service were not performed.

### R01 Tenant authorization is inconsistent across patch routes

**P1.** Deployment creation, deployment listing, summary, and approval routes call tenant-access checks. Deployment plan, deployment node detail, and wave advance only check global roles. `authorize` validates roles, not membership of the resource tenant. The router wraps authentication but supplies no resource-specific tenant check.

A user with an eligible role and another tenant's deployment ID can reach detail or wave-advance logic without the membership check used by creation. Plan accepts a tenant ID without checking caller access. Proxy/window lists and patch-config reads also omit tenant checks. Admin mutations need an explicit documented distinction between platform administrators and tenant administrators; globally privileged administration must not be assumed for every admin principal.

Evidence: `patch.go:728`, `patch.go:754`, `patch.go:801`, `patch.go:1481`, `patch.go:1588`, `patch.go:1774`; `server.go:599`; `tenant_access.go:61`.

Fix all routes through shared authorization helpers. Validate that node, deployment, proxy, and maintenance window belong to the same authorized tenant before reads or writes. Test denied requests for every route and linked-resource combination, including tenant-scoped administrators.

### R02 A reporting agent is not bound to its patch completion

**P1.** Heartbeat authentication binds a certificate to the heartbeat URL node. However, `processHeartbeatCompletedActions` discards the node-ID argument and looks up patch state only by submitted job ID. The patch branch does not compare the patch state's node/tenant with the authenticated reporting node or validate the stored job type against the reported action.

If an agent obtains another node's patch job ID, its own valid heartbeat can submit success or failure for that job. Job-fetch authorization correctly limits payload reads to the intended agent; that protection does not substitute for completion ownership checks.

Evidence: `heartbeat.go:856`, patch completion branch at `heartbeat.go:959`; job-read ownership in `jobs.go` functions `agentCanReadJob` and `jobPayloadNodeID`.

Require authenticated node, tenant, job payload, patch state, job type, and expected execution attempt to agree. Reject mismatches before any write or receipt creation. Test wrong-node, wrong-tenant, wrong-action, unknown-job, and replay cases.

A temporary in-memory server probe dispatched a patch to node A, then submitted its completion with a different reporting node ID. The stored state changed from pending to applied. The expected rejection assertion failed, confirming the ownership defect in the completion processor. This was not an attempt against a deployed service; the temporary test was removed afterward.

### R03 Approval dispatch can bypass changed safety conditions

**P1.** Approval resolves the row and dispatches directly. A comment explicitly acknowledges that change-window and circuit-breaker checks are bypassed. Isolation or manual-only labels may also have changed since the original request. If deployment lookup fails, package policy falls back to an empty policy, potentially replacing an intended package selection with a full-system upgrade.

Evidence: `patch_approvals.go`, function `handleApprovePatchApproval`; `patch.go`, function `runPatchSafetyGates`.

Recheck all non-approval gates at approval, dispatch, and execution start. Preserve an immutable approved plan and reject missing policy. Bind approval to the plan digest, target packages/versions, resource scope, expiry, and requester. Commit approval plus dispatch intent transactionally so a crash cannot consume approval without recoverable work. Resolve the approver to a stored user and audit that actor; current approve/deny audit uses `systemActor`, and UUID parsing of the subject can yield an empty approver.

### R04 Repeated heartbeat delivery can start concurrent upgrades

**P1.** `ListPendingNodePatchStates` returns the same job until patch state becomes applied/failed. Marking the job running does not remove it from that query. Each receipt of the action starts a new goroutine in the agent, with no patch job deduplication or per-node package-manager lock in this path.

A patch lasting longer than the heartbeat interval can therefore start more than once. Package-manager lock failures from duplicate executions can race with the original success; node-state writes only transition from pending, so the first terminal result can determine the displayed outcome.

Evidence: `storage/patch.go:411`, `server/heartbeat.go:403`, `cmd/nodeagent/heartbeat.go:267`, `cmd/nodeagent/patch_exec.go:56`.

Introduce durable execution attempts and leases, agent deduplication by job/attempt ID, and serialization of package transactions on each node. Redelivery must return the same attempt's progress/result. Test multiple heartbeats during a slow upgrade, agent restart, lease expiry, and simultaneous deployments to one node.

### R05 Completion results can be lost before acknowledgment

**P1.** Results live in a bounded in-memory queue. Enrichment drains and clears that queue before the HTTP request succeeds. Marshal failure, network failure, server rejection, or agent restart can lose the result. Queue overflow drops the oldest records. Server mutations and receipts are not coordinated into one acknowledged result transaction.

Evidence: `cmd/nodeagent/firewall_exec.go:21`, `cmd/nodeagent/heartbeat.go:179`, `cmd/nodeagent/heartbeat.go:349`; patch completion branch in server heartbeat.

Persist results in an agent outbox until the server acknowledges their result IDs. Apply node/job/receipt transitions idempotently and atomically. Acknowledgment must distinguish accepted, duplicate, and rejected results. Add a reconciliation process for lost links, stale attempts, and offline nodes. Test failed responses, dropped acknowledgments, restart, and duplicate delivery.

### R06 Upgrades and removals can leave old vulnerability findings active

**P1.** Reconciliation scopes include installed version. Refresh builds scopes from the current inventory. A finding for version A is outside the scope after the package upgrades to version B. A removed package is absent from scope entirely; an empty inventory returns without reconciliation. These findings can remain unresolved even after the affected software is gone.

Evidence: `vulnerability_feed_import.go`, functions `vulnerabilityPackageScopes` and `importOfflineVulnerabilityFeedFindingsForNode`; `storage/vulnerability_findings.go:173`, particularly equality on `s.installed_version = f.installed_version`.

Use a successfully completed inventory/assessment generation to reconcile the entire assessed node/source scope, with explicit handling for replaced versions and removed packages. Partial inventory or scan failures must preserve prior findings as unverified rather than silently resolve them. Test A-to-B fixed upgrade, A-to-C still-vulnerable upgrade, removal, authoritative empty inventory, partial collection, and independent evidence sources.

### R07 Missing feeds can be interpreted as a clean assessment

**P1.** `MatchActiveVulnerabilityFeeds` returns an empty match set without error when no active feed files exist. For nonempty inventory, refresh then reconciles that empty set against existing same-version findings. The matcher reads receipts for provenance but does not enforce their current expiry before matching. Bundle import validates signature/manifest and status can mark staleness; those existing protections do not establish assessment coverage at match time.

Evidence: `offlinebundle/vulnerability_feed.go:84`; `vulnerability_feed_import.go:66`; `offlinebundle/bundle.go` import validation and receipt-staleness helpers.

Return explicit assessment status: successful with coverage, unavailable, stale, partial, or failed. Require a valid feed generation and supported matching semantics before resolving findings. Store feed digest, coverage, age, and scan completion per node. Test missing feed directory, empty feeds, expired receipts, malformed content, and partial coverage.

A temporary local review probe confirmed that a fresh directory with no feeds returns zero matches and a nil error. The probe expected an unavailable assessment and failed. The temporary probe was removed after execution.

### R08 Installed state is reported before remediation is verified

**P1.** Package-command exit success marks the node applied, the job successful, and an action receipt successful. Optional `post_patch_rescan` requests full inventory; deployment rollup does not wait for the resulting assessment or check that cited CVEs resolved. The receipt records the rescan boolean as `post_patch_inventory`, although it represents a request rather than proof of completed verification.

Evidence: `heartbeat.go:959`, `heartbeat.go:1279`; `patch_action_plans.go:106`; `patch.go:919`.

Separate installed, awaiting reboot, verifying, verified, and verification-failed outcomes. Bind verification to inventory collected after the attempt, the approved findings and package versions, feed generation, and application health checks. Permit waves to advance only after their defined verification and observation period. A mitigation/exception must be visible separately from a fixed vulnerability.

### R09 Maintenance-window status does not prove network enforcement

**P1.** Window jobs queue firewall work and mark a window open/closed without waiting for enforcement receipts; individual dispatch failures are logged and processing continues. Force-close only updates the database status. Dispatch ignores the selected `windowID` for execution authorization; the whitelist-isolation gate treats a non-nil window ID as sufficient without validating that window. Tenant recurring change windows and these repository-access windows are separate concepts in code.

No caller enforcing automatic open/close at `opens_at`/`closes_at` was found in the reviewed control-plane code. The dates therefore must not be presented as a demonstrated automatic enforcement guarantee.

Evidence: `patch.go:1404`, `patch.go:1709`, `patch.go:1270`; `storage/patch_squid.go:202`.

Define one scheduling contract and distinguish scheduled, opening, enforced-open, closing, enforced-closed, and failed states. Validate node membership, tenant, current time, policy, and receipts. Force-close must request and verify network closure, with escalation for unreachable nodes and a local expiry mechanism. Restore the prior network state rather than assuming a new block rule is an exact reversal. Test clock boundaries, offline nodes, partial firewall failure, and server restart.

### R10 Dispatch and wave advancement lack atomic recovery

**P1.** Node state, action plan, job creation, and job linkage are separate writes. Failure to persist `SetNodePatchStateJobID` is only logged and dispatch still returns success. State upsert deduplicates the row but not subsequent job creation. Concurrent wave-advance requests can inspect the same next wave and create multiple jobs. Rollup leaves a deployment in progress when fewer rows exist than the target count, including unrecoverable creation failures. Expiring approval rows does not terminalize their pending node state.

Evidence: `patch.go:801`, `patch.go:1221`, `patch.go:1270`; `storage/patch.go:344`; `heartbeat.go:1282`; `patch_approvals.go:400`.

Use transactions, unique dispatch/attempt keys, and atomic wave claims. Preserve every intended target with an explicit status. Recover interrupted dispatch; reconcile expiry and cancellation into node/deployment outcomes. Race two advances and inject failures after each persistence boundary. Report approval-required, blocked, denied, expired, deferred, offline, dispatch-failed, and execution-failed separately.

### R11 Platform and package controls are narrower than the product label

**P2.** Windows inventory reads uninstall registry entries; execution calls `winget upgrade --all`. This is application updating, not Windows OS cumulative/security update servicing. Windows package allowlists are rejected. Linux supports apt/dnf/yum, while airgapped execution supports apt only. AIX inventory does not imply an AIX patch executor. The main UI does not select capability-compatible targets before creating deployments.

Package requests select names rather than a verified version/repository transaction. A denylist is checked for overlap with the allowlist but is not passed as a package-manager exclusion/pin; it cannot guarantee that denied dependencies remain unchanged. The full-system path upgrades eligible packages rather than limiting itself to identified security fixes.

Evidence: `cmd/nodeagent/inventory_windows.go`; `cmd/nodeagent/patch_exec.go:184`, `patch_exec.go:276`; `patch.go:919`. Microsoft's [WinGet upgrade documentation](https://learn.microsoft.com/en-us/windows/package-manager/winget/upgrade) describes application upgrades.

Publish and enforce a support matrix for inventory, assessment, OS updates, application updates, offline mode, reboot, and recovery. Add a Windows servicing adapter or an integration with an enterprise update service. Preview the exact transaction, include dependency changes and excluded packages, validate repository availability, and bind approval to that plan. Make unsupported/unknown targets explicit.

### R12 Recovery and health checks are descriptive rather than enforced

**P2.** Patch action plans contain generic rollback advice. The patch executor does not implement snapshot/backup readiness, package-version restore, reboot detection/coordinated restart, or post-patch application health verification. Existing canaries/waves are valuable, but advancing on applied/failed states alone cannot establish service safety.

Evidence: `patch_action_plans.go:16`, `cmd/nodeagent/patch_exec.go:56`, `patch.go:1193`.

Record platform-specific recovery capability before approval. Use snapshots, known-good images, failover, or supported package rollback as appropriate; do not promise universal automatic rollback. Define application probes and reboot ownership. Exercise recovery and clustered-service availability in a lab before production qualification.

### R13 Posture confidence and the Control Room score can disagree

**P2.** The patch page reads inventory/findings from the summary query. Control Room derives its patch score from deployment failures, approvals, and optional deployment-summary keys. An empty activity set can therefore yield 100 without establishing assessment coverage. UI risk tiles use healthy tone when summary data is missing. A fixed 24-hour inventory last-seen threshold is not feed freshness or scan-completion evidence. Only nodes whose enrollment state is active contribute to the database posture denominator; exclusions need to be visible against the total managed estate.

Evidence: `storage/patch.go:68`; `control_room.go:856`; `ui/src/pages/PatchManagement.tsx:184`; `ui/src/features/patch-management/AllTenantPatchSummary.tsx:72`.

Use one shared posture contract with per-node assessment status and transparent denominators. Show risk, evidence confidence, and deployment activity separately. Missing data must be unknown. Any score must disclose its inputs and coverage. Count stale inventory, missing feeds, unsupported systems, offline agents, accepted exceptions, pending reboots, and overdue remediation explicitly.

### R14 The operator flow does not expose the safer backend workflow

**P2.** The main deployment form omits dry-run preview, package selection, post-patch verification choice, canaries, wave sizing, and advancement. `post_patch_rescan` defaults false when absent. Thus normal UI deployment does not opt into these backend safeguards. The top-level refresh also lacks stale-response cancellation across tenant switches, unlike the node-loading effect.

Evidence: `ui/src/pages/PatchManagement.tsx:54`, `PatchManagement.tsx:1323`; `patch.go:112`, `patch.go:919`.

Make the default path assess, preview, select a canary, approve, execute, verify, and advance. Capture the reviewed scope and render per-node reasons. Cancel/ignore requests when tenant context changes and reset tenant-scoped drawers/forms. Test delayed responses from the previous tenant and partial API failures.

### R15 Proxy lifecycle contains incomplete operations

**P2.** Removing a proxy marks its database row `removing` and returns success without queuing uninstall/decommission work in that branch. Proxy install/reconfigure create jobs, but the reviewed heartbeat dispatch/completion path has no corresponding Squid action handling. The inventory-scan job similarly has an agent implementation and registration but no dedicated completion handling in `processHeartbeatCompletedActions` and no dedicated pending-state delivery path found here.

Evidence: `patch.go:1851`, `patch.go:1903`, `patch.go:1391`; `heartbeat.go:398`, `heartbeat.go:856`; `cmd/nodeagent/heartbeat.go:267`.

Trace generic-job delivery and worker behavior in integration tests before labeling these operations complete. Implement explicit desired state, durable delivery, completion receipts, and recovery for install/reconfigure/remove. Validate host/port/whitelist independently of whether Squid happens to be installed on the control plane; current `validateSquidConfig` returns early when Squid is absent. Prove proxy routing and repository restrictions with packet/transaction evidence in a lab.

### R16 Native package version semantics are approximated

**P1.** Debian and RPM schemes resolve to the generic tokenizer rather than distinct native comparators. That tokenizer discards punctuation other than tilde and lowercases letters. A temporary review probe confirmed that `compareVulnerabilityVersion("1.0+a", "1.0a", "debian")` returns zero with supported=true. Debian's documented ordering makes the first version greater: nonletter characters sort after letters. The resulting affected-range decision can therefore be incorrect.

Evidence: `offlinebundle/vulnerability_versions.go:144`, `vulnerability_versions.go:267`; [Debian version ordering specification](https://www.debian.org/doc/debian-policy/ch-controlfields.html#version). The probe failed its expected greater-than assertion and was removed after execution.

Use tested ecosystem-specific comparison libraries and fixtures checked against native dpkg/rpm behavior, including epochs, revisions, punctuation, case, tilde/caret where applicable, prereleases, and invalid inputs. Reject unsupported schemes and range syntax explicitly. Add distribution/release and vendor advisory context to identity/matching so a package name and generic version do not imply coverage across unrelated distributions or backports.

### R17 Safety read failures can select a less restrictive path

**P1.** Node lookup errors skip label/isolation checks, remediation-config errors use defaults, and circuit-breaker lookup errors skip breaker enforcement. Auto mode falls back to direct mode if per-node patch configuration cannot be read. Default approval may still stop an initial dispatch, so this is not a claim that every read failure immediately starts an upgrade. It does mean that the evaluated plan can be less restrictive than the stored policy and, combined with approval bypass, unsafe to rely on.

Evidence: `patch.go:465`, `patch.go:559`, `patch.go:1056`, and initial per-node mode resolution in deployment creation.

Distinguish a deliberately absent configuration with a documented default from failure to read existing policy. On failure, block and return a retriable policy-unavailable outcome. Never replace a configured proxy/airgapped path with direct connectivity because storage is unavailable. Exercise each failed read in preview, approval, dispatch, and execution-start tests.

## Standards baseline

There is no single global patch certification that a code change can confer. Use the following control objectives, then map them to customer scope and evidence requirements.

| Reference                                                                                                         | Applicable objective                                                                                                                                                                                                             | Required product evidence                                                                                                                                                         |
| ----------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| [NIST SP 800-40 Rev. 4](https://csrc.nist.gov/pubs/sp/800/40/r4/final)                                             | Identify, prioritize, acquire, install, and verify patches within an enterprise strategy.                                                                                                                                        | Inventory and assessment coverage; approved plan; trusted content; execution and verification results; recovery procedure.                                                        |
| [CIS Controls v8.1 Control 7](https://www.cisecurity.org/controls/continuous-vulnerability-management)             | Continuously assess and track vulnerabilities and remediate them. The [Control 7 safeguards](https://www.cisecurity.org/controls/cis-controls-navigator/v8) include documented remediation and automated OS/application patching. | Assessment schedule, accountable owner, documented risk policy, OS/application scope, and measurable remediation outcomes.                                                        |
| [CISA KEV catalog](https://www.cisa.gov/known-exploited-vulnerabilities-catalog)                                   | Prioritize known exploitation within vulnerability management.                                                                                                                                                                   | Feed age and provenance, affected asset context, priority, due date, remediation or mitigation evidence. Federal directives are not automatically obligations for every customer. |
| [PCI DSS applicability guidance for requirements 6.3.1 and 6.3.3](https://www.pcisecuritystandards.org/faqs/1597/) | For PCI-scoped environments, assign environmental risk and resolve critical vulnerabilities within one month of patch release; other applicable updates follow justified risk time frames.                                       | Vendor release date, risk ranking, due date, installation/verification dates, and applicable exception/compensating-control evidence.                                             |

ISO/IEC 27001 or sector requirements can be mapped during customer scoping; this review does not assert certification or jurisdiction-specific legal compliance. Proposed operational deadlines must be policy choices, not invented universal mandates. KEV, exposure, asset criticality, availability constraints, and applicable contractual/regulatory deadlines should drive the risk policy together.

## Target control model

Maintain separate assessment and execution records. Assessment records need node and tenant identity, inventory generation/completeness, platform support, feed generation and freshness, assessment outcome, findings and source evidence, and coverage exclusions. A zero-finding count is healthy only when the assessment succeeded with adequate coverage.

Each approved change needs immutable target nodes, package names and intended versions, repository/content provenance, dependency changes, referenced CVEs, policy version, approval identities, permitted window, canary/waves, health probes, reboot handling, and recovery plan. Bind every execution attempt and receipt to this approved change.

Governance also needs an asset/service owner; vendor patch release date and detection date; environment-specific remediation deadline; escalation and breach state; approved exceptions with reason, compensating control, approver, and expiry; and exportable evidence with retention/access policy. Preserve both historical and current findings, and distinguish fixed, mitigated, accepted, and not affected. These should be structured policy and lifecycle records rather than strings that silently suppress dashboard risk.

Suggested lifecycle:

```text
assessed -> planned -> awaiting approval -> scheduled -> preflight
         -> executing -> installed -> reboot pending if needed
         -> verifying -> verified
```

Use explicit deferred, blocked, denied, expired, failed, cancelled, and recovery-required outcomes. A successful command is an execution result. A verified fix additionally requires a later authoritative assessment and the required health/reboot evidence. Accepted risk, mitigated risk, and not-affected determinations remain distinguishable from installed fixes.

## Delivery plan and acceptance gates

| Phase                      | Scope                                                                                                                                                                           | Suggested accountable functions     | Exit gate                                                                                                                                          |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| 0 Establish evidence       | Capture deployed versions, topology, platform support, tenant roles, feeds, inventory/assessment coverage, recent deployments, and backups in a read-only environment baseline. | Platform operations and security    | Source/deployed-version differences recorded; nonproduction qualification environment identified.                                                  |
| 1 Secure execution         | R01-R05 and R17, authorization portions of R09, immutable approvals, atomic completion ownership, fail-closed safety reads.                                                     | API, agent, security engineering    | Every unauthorized or stale-plan request denied; repeated delivery cannot start a second upgrade; results survive restart and lost acknowledgment. |
| 2 Correct state            | R06-R08, R10 and R16, generation-based reconciliation, native version comparisons, explicit verification, durable dispatch and expiry recovery.                                 | Backend, storage, agent engineering | Fixed/removed/still-vulnerable packages produce correct findings; unavailable feeds never clear risk; crash/race tests converge correctly.         |
| 3 Qualify platforms        | R09, R11, R12, R15, repository isolation, Windows OS servicing decision, actual window closure, health and recovery tests.                                                      | Agent, infrastructure, QA           | Supported OS/mode matrix passes real package/firewall/reboot tests; unsupported combinations rejected before approval.                             |
| 4 Operator and governance  | R13-R14, shared summary, full workflow UI, deadlines/owners, expiring exceptions, evidence export.                                                                              | UI, security operations, backend    | Patch page and Control Room agree; unknown coverage stays visible; every remediation has owner, due date, outcome, and audit trail.                |
| 5 Production qualification | Fault injection, scale and soak tests, canary pilot, recovery drill, standards evidence mapping and operational runbooks.                                                       | QA, operations, security owner      | All release-blocking scenarios pass on the actual support matrix; pilot and rollback results reviewed before broad rollout.                        |

Phase 1 should precede adding scheduled mass patching or widening platform claims. Phase 2's assessment/verification contract should be stable before redesigning the dashboard. Estimates should follow agreement on supported operating systems, Windows integration, availability requirements, and whether existing fleets must be migrated without downtime.

## Required qualification scenarios

1. User in tenant A cannot plan, read node results, advance a wave, read config/proxies/windows, or mutate linked resources in tenant B. Repeat with tenant-scoped administrator roles.
2. Node A cannot complete node B's job; payload type and execution attempt mismatches are rejected without state changes.
3. Approval after a window closes, circuit breaker trips, isolation changes, or package policy disappears does not dispatch.
4. Slow upgrade across several heartbeats executes once; concurrent deployments on one node serialize; duplicate/concurrent advances create one wave dispatch.
5. Agent restart, network loss, server error, lost acknowledgment, and queue pressure preserve results and produce one consistent terminal receipt.
6. A-to-B fixed upgrade resolves A's findings; still-vulnerable upgrade creates/retains the right active findings; removal resolves only after authoritative complete inventory.
7. Missing/expired feed, unsupported version semantics, partial scan, and collector failure show unknown/partial status and cannot imply remediation.
8. Exit success with vulnerable package, pending reboot, or failed service probe remains unverified and cannot advance the rollout.
9. Windows application updates and Windows OS updates are tested separately. Linux direct/proxy/offline modes verify package provenance, network behavior, and resulting versions.
10. Window open/close/force-close and expiry verify actual firewall state, including disconnected nodes and restart. Offline apt configuration must exclude unintended source files and network fallbacks.
11. Inject failures after state insert, plan insert, job insert, job link, approval resolution, and receipt write; reconciliation recovers without duplicates or orphaned work.
12. Failed/stale requests, tenant switches, zero nodes, unsupported nodes, and no feeds render unknown or explicit errors consistently on every screen.
13. Perform recovery and application availability drills, plus scale tests at the intended fleet size, with bounded query/heartbeat latency and no database work amplification from repeated completions.

## Validation record

The existing PatchManagement UI suite passed: 7 tests. It covers summary scope, separation of measured posture from recent activity, partial loading failures, and confirmation/error flows. Those tests do not prove package execution, tenant denial, durable results, or verified remediation.

Existing offline vulnerability tests passed with `go test -p 1 ./controlplane/internal/offlinebundle -run 'Test.*Vulnerabilit' -count=1 -timeout=60s`. Two additional temporary probes failed as expected for the defects in R07 and R16: missing feed reported zero matches without error, and distinct Debian versions compared equal. Their test file was removed; no production source was changed.

A third temporary probe failed the expected ownership rejection in R02: an in-memory patch completion from another node changed state to applied. Its test file was also removed. All three probe failures represent confirmed behavioral gaps, distinct from the passing existing suites below.

Initial sandbox execution failed on Go-cache access. After using the installed cache outside the sandbox and completing compilation, the following existing focused suites passed:

```powershell
go test -p 1 ./controlplane/internal/server -run 'Test(Patch|ResolvePatchTargets|NodeVulnerabilit|OfflineBundleImport)' -count=1 -timeout=120s -v
go test -p 1 ./controlplane/internal/server -run 'Test(Heartbeat|HandleNodeVulnerabilities|VulnerabilityPatchPlan)' -count=1 -timeout=120s
go test -p 1 ./cmd/nodeagent -run 'Test.*(Patch|Heartbeat|Upgrad|Inventory|CompletedAction)' -count=1 -timeout=120s -v
go test -p 1 ./controlplane/internal/storage -run 'Test.*Vulnerabilit' -short -count=1 -timeout=60s -v
node node_modules/vitest/vitest.mjs run src/pages/PatchManagement.test.tsx --maxWorkers=1 --minWorkers=1 --pool=threads
```

The storage run passed one normalization unit test and explicitly skipped three PostgreSQL integration tests in short mode. Docker was not found on PATH in this session; database integration is unverified. The dedicated CI storage selection currently does not name vulnerability reconciliation tests, while its broad Go test command uses `-short`. Interrupted earlier broad runs are superseded by the completed focused runs above; they are not additional passing results.

The patch-specific server tests use an in-memory fake store. They are useful for request/state behavior but do not establish PostgreSQL transaction behavior or a real package-manager round trip. The agent patch test file currently tests upgrade-list parsing. Add the qualification scenarios above to API, PostgreSQL, agent failure-injection, browser, and disposable-OS integration suites.

No deployed service, live database, customer tenant, or disposable supported-OS lab was exercised. Runtime exploitability, package-manager behavior, recovery, firewall receipt convergence, feed coverage, and fleet-scale performance remain qualification work. The confirmed source defects are sufficient to prioritize the repair sequence without claiming a production incident occurred.
