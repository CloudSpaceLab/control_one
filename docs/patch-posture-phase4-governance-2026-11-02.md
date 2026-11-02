# Phase 4 recommendation-only governance — 2 November 2026

> **Repeat live operator demonstration, 2 November 2026:** demonstrated the real saved package/canary/window scope, unsaved scheduled-mode/UTC examples, failed host readiness and disabled activation. Reopened the earlier real CA-package deployment and matching installed inventory, while showing its no-security-baseline/unknown verification and blocked promotion. Saved policy revision 7 remained unchanged; no new installation or schedule was created. [Repeatable steps and exact evidence limits](patch-posture-phase4-automation-2026-11-02.md#repeatable-operator-demonstration).

> **Scheduled execution engineering, 2 November 2026:** durable exact-plan scheduling, individual canary authority, narrow automatic follower gates, current host readiness, policy withdrawal/stop controls and retained execution receipts are implemented, tested in isolation and deployed locally for readiness verification. The live Docker readiness check reports unknown service health and incomplete service inventory; installation remains disabled. [Procedure, passing conditions, test evidence and current limits](patch-posture-phase4-automation-2026-11-02.md). Earlier draft-only statements below are historical; full-host/production qualification and overall P4-07 remain open.

> **Phase 4 operational controls, 2 November 2026:** stable queue/history pagination, complete retained-review snapshot exports, guarded archive-before-purge retention, explicit in-app notification recipients/deduplication and scoped disabled scheduled/automatic policy drafts are implemented. Dedicated database/API regressions and 53 affected/shared UI tests passed, plus TypeScript, scoped lint and production build. See the [operator procedure, live evidence and limits](patch-posture-phase4-operations-2026-11-02.md). Installation remains recommendation-only; saving a draft does not qualify service/reboot outcomes, activate an executor or complete Phase 5. VM/full-host and external recovery/trust gates remain open.

This checkpoint implements tenant-owned priority rules and finding review tracking, using the existing Local Development environment and temporary lab tooling. The user explicitly selected a **recommendation-only lab demonstration**. No new patch installation, VM, enrollment token, credential or PowerShell script is required by this checkpoint. It does not close full Phase 4 or production rollout qualification.

## What the operator gets

Open **Patch posture → Review policy & ownership**. Node Recommendations also links to the same tenant review workspace. The workspace provides:

- An administrator-managed, versioned policy with an accountable owner/team, ordered rules, minimum severity, known-exploitation requirement, exposure, asset importance, priority and a deadline measured from the finding's first observation. A refresh cannot reset that first-observation deadline.
- A hypothetical scenario evaluator, clearly separated from observed machine evidence. A saved tenant policy is required; there are no universal production urgency weights or deadlines.
- A review queue for actual active findings. Priorities require current selected-advisory assessment evidence and a recent finding observation. Stale, uncovered or unavailable evidence produces **review required**. Complete vulnerability coverage is never inferred.
- Explicit owners, escalation destinations, due-date overrides, reasons, expiring reviewed context and administrator-controlled exceptions. Owners/destinations are accountable team labels, not new permissions or outbound messages.
- An overdue/unassigned/expired-exception escalation queue. This is an in-app queue; external notifications are not configured.
- Durable review revisions/events and a tenant-scoped JSON export that also includes approval history. This JSON is an evidence export, not an installation payload.
- Explicit recommendation-only automation state and stop conditions. Scheduled/automatic installation policy values are rejected. Existing manual deployment and approval workflows remain separate.

Exposure, importance and compatibility can remain unknown. If an operator records non-unknown context, an expiry within 90 days is required; expired context is evaluated as unknown. Exceptions must expire within 90 days, remain visible with the finding, and never resolve it, suppress it, bypass approval or authorize a job.

## Repeat the live demonstration

1. Select **Local Development** and open **Patch posture → Review policy & ownership**.
2. Expand **Tenant urgency policy (administrator)**. **Load lab example (unsaved)** fills editable example rules. It does not save or activate anything until **Save recommendation policy** is clicked.
3. Read the example label. The demonstrated lab rules were: known exploited + exposed + at least high severity → urgent/24 hours; critical severity → high/72 hours; other supported findings → normal/336 hours. These are deliberately labelled examples, not recommended production deadlines. The accountable team label is **Local Development operator**.
4. Save the policy. The live tenant stored revision **1**, mode **recommend_only**. Concurrent stale edits are rejected with conflict; reload before retrying.
5. Collapse the policy and open **Try a hypothetical priority scenario**. With critical severity, known exploitation, exposed, critical importance and current evidence, **Evaluate hypothetical scenario** returns **urgent**, names the matching rule and explains its deadline. Unknown compatibility still requires operator review; nothing is installed.
6. Change exposure to **unknown** and evaluate again. The exposure-specific rule does not match. The demonstrated critical fallback returns **high**, with the 72-hour deadline and an explanation of the missing exposure evidence.
7. Uncheck **Scenario evidence current** and evaluate. The result becomes **review_required** with no inferred deadline. These inputs are hypothetical and are never saved as findings or telemetry.
8. In the actual queue, read each finding's current-evidence state. The live tenant showed three active findings: a current Windows 7-Zip finding and stale findings on older Ubuntu/demo nodes. No package update or comprehensive-security claim was inferred from those rows.
9. Click **Review USN-8945-1 on c6732033**. The lab demonstration assigned owner **Local Development demo reviewer**, escalation destination **Local Development review queue**, and a reason explicitly stating that fresh evidence is required and no installation/remediation is authorized.
10. Set an explicit UTC due-date override and short future exception expiry. Read **Pending review dates** before saving. The corrected live record retained due **2026-11-02 11:47 UTC** (12:47 Lagos) and exception expiry **2026-11-02 12:08 UTC** (13:08 Lagos). The overdue date is intentional demonstration data, not a production SLA.
11. Save. The live record became review revision **2**, retained the stale finding and owner, showed **exception active** and **escalation required**. Review revision 1 recorded ownership only; the next revision records the corrected dates, preserving the audit trail.
12. The queue recomputes expiry/overdue state every minute, or on **Refresh review evidence**. Live observation at 12:09 UTC (13:09 Lagos) confirmed **exception expired**, with escalation and the unresolved finding retained. Database read-back confirmed the stored expiry was in the past. This was an actual elapsed-time test, not a changed system clock.
13. Click **Export review evidence**. The browser downloads `controlone-patch-review.json`. Check tenant, evaluated time, revision, recommendation-only state and truncation flags. Do not share it outside the approved audience; it contains tenant finding and review evidence.

## Live fixes and verification

The walkthrough exposed native UTC date input values that could appear filled without reaching React state. The handler now processes input events and displays the parsed UTC values before saving. A regression verifies that both dates reach the API payload; live read-back confirmed the corrected due date and exception.

The existing node-only findings API correctly requires a node ID. A separate explicitly tenant-scoped query was added for review, preserving that restriction. It uses a repeatable-read snapshot for its count and bounded results.

The current Windows advisory lacked a trusted severity value. The final explanation distinguishes **unknown severity** from **stale advisory evidence**: a current advisory can still require severity review before a severity-based policy applies. The engine does not invent a severity or urgency to fill the gap.

Verification completed:

- Policy evaluation tests cover ordered matches, unknown exposure, unchanged first-observation deadlines, stale evidence and rejection of unsupported automatic modes.
- API tests cover tenant roles, agent rejection, administrator-only policy/exception writes, invalid/trailing JSON, stale-assessment priority withholding, evidence export and zero-job behavior.
- Dedicated PostgreSQL tests cover first-write concurrency (one success/one conflict), stale revisions, cross-tenant finding rejection, transactional events, unresolved findings after exception and zero jobs.
- Full related Go/API/storage suites passed sequentially with `go test -p 1` and both database environment variables pointed at `controlone_patch_security_test_phase2_20261028`. An earlier parallel package run hit the unrelated generic job-cancellation integration test while storage fault fixtures shared that database; it is retained in the local evidence and is not reported as a passing run. Sequential validation avoids that shared-fixture interference.
- **40 focused UI tests passed** across review, shared evidence and deployment views. After the live date correction, the six review tests passed, including UTC payload assertions and a fresh-review check that does not renew expired context or exceptions. TypeScript, scoped lint and the production UI build passed for the final date-handler state. The six review tests additionally cover opening a fresh review without silently renewing an expired exception or expired context.
- The live export contained the correct tenant, policy revision 1, **recommend_only**, `installation_authorized: false`, three findings, three review events and 26 approval records; all three truncation flags were false at export time. A local evidence copy is kept under the ignored temporary directory.
- Database read-back confirmed **zero new patch-deployment jobs** during this recommendation-only demonstration.

## Runtime, evidence and rollback

Migration **0176** adds policy, finding-review and event tables. It does not alter installed packages or existing approval receipts. Database schema version 176 reported clean after startup.

The final local control plane uses `controlplane-governance-qualified-20261102`, SHA-256 `57099ebd5c1ac7bd6ae230609dcb54cfaca52a255eea8a8dec12b40b92d1fad9`. The earlier governance candidate was retained as `controlplane-governance-final-20261102` (SHA-256 `731c0899bb817b4cf71d7d846eb9637ca9fa41e97bc7c4a452e1a466e9463391`). The previous exact-plan control-plane executable is retained as `controlplane-online-plan-20261102`; selecting it in the existing ignored candidate override and running `scripts/local.ps1 start` rolls back the application binary. Keep the additive tables and history; do not apply the destructive down migration as a routine rollback.

Before migration, a local custom-format PostgreSQL backup was retained as `tmp/local/patch-posture-demo/phase3-offline-20261029/patch-review-before-20261102.dump`. This is a local rollback precaution, not an external recovery acceptance test.

No agent executable, Windows service, enrollment or repository configuration changed during this governance checkpoint. The prior real CA canary remains documented in [exact online plan evidence](patch-posture-exact-online-plans-2026-11-02.md).

Final read-back retained the lab agent configuration SHA-256 `fb9c597d247b27728656691107dc33218f5e0f0119d0ca00e839f69a84d87030`, dpkg status `df9d9535fb469def049b908ad3ca3eb3197caed9c21a719ddbf7f1a21ae29432` and original repository sources `92d2ff28918561a6c5e4ebf241769a57b13eebd1504d77d7a3a77a73e368c5ec`. Package audit was clean and trusted TLS health returned `ok`. Zero pending approvals and zero new patch-deployment jobs remained during this review-only checkpoint.

Local screenshots, downloaded export, executable candidates, backups and test logs are evidence under `tmp/local/patch-posture-demo/phase3-offline-20261029/`. They are not files to commit. Source, migration, meaningful tests and repository documentation are the intended changes; no commit or push was requested for this checkpoint.

## Current operational follow-up and remaining acceptance

The initial bounded checkpoint above is historical. The subsequent [operational-controls follow-up](patch-posture-phase4-operations-2026-11-02.md) replaces its 100-finding/1,000-event/100-approval export caps with keyset pages and a complete retained-review database snapshot. The history window now affects the interactive history only. Legal hold, explicit archive-before-purge batches, delivery receipts, authorised in-app recipients and disabled scheduled/automatic policy drafts are implemented and tested there.

Recommendation-only remains the effective installation mode. Scheduled and narrowly automatic modes can be reviewed and saved as disabled drafts; activation is rejected until Phase 5 execution/promotion qualification. Email/SMS/webhook delivery is outside the implemented in-app channel. The lab owner/example policy is not organisational production approval.

Full Phase 4 remains open for measured service/reboot/compatibility outcomes and complete intended platform/transport qualification under P4-04e, and for active scheduled/automatic execution under P4-07. VM/full-host acceptance remains deferred. The canonical checklist records each demonstrated operator-control check separately and does not convert a draft or isolated fixture into production rollout evidence.
