# Sprint Recovery Audit

**Date:** 2026-05-18
**Scope:** PR #73, PR #74, and PR #119 merge-order recovery
**Status:** implementation code recovered; sprint tracking still requires closeout work

## Summary

Sprint 5 was merged into `main` by PR [#73](https://github.com/CloudSpaceLab/control_one/pull/73) on 2026-05-15. Sprint 6 was then merged into the already-merged Sprint 5 branch by PR [#74](https://github.com/CloudSpaceLab/control_one/pull/74) on 2026-05-17, which meant `main` did not receive the Sprint 6 commit.

Recovery PR [#119](https://github.com/CloudSpaceLab/control_one/pull/119) merged on 2026-05-18 and brought the missing Sprint 6 event-capture commit path into `main`.

Recovered commits:

| Commit | Source | Purpose |
|---|---|---|
| `a789d6f` | PR #73 | Sprint 5 chat investigation loop |
| `bdb9a45` | PR #74 / #119 | Sprint 6 event-capture tools |
| `9c37090` | PR #74 | Merge Sprint 6 into Sprint 5 branch |
| `424a946` | PR #119 | Merge recovered Sprint 6 path into `main` |

## Verification

CI for PR #119 passed:

| Check | Result |
|---|---|
| Lint | pass |
| Test | pass |
| Security Scan | pass |
| Trivy | pass |
| lint-test-build (ubuntu-latest) | pass |
| lint-test-build (macos-latest) | pass |
| lint-test-build (windows-latest) | pass |
| Build | pass |

Targeted local test pass:

```bash
go test ./controlplane/internal/server -run 'TestSprint6AIAskUsesEventCaptureEvidence|TestNodeEventCaptureEndpointsReturnIncidentDeltas'
```

Known local test environment caveat:

```text
go test ./controlplane/internal/server
```

requires a local `controlone_test` Postgres database and `controlone` role on port 5432. The failure observed locally was environment setup, not a Sprint 6 regression.

## Sprint 5 Audit

PR #73 should be treated as an umbrella Sprint 5 merge, not proof that every originally planned Sprint 5 worktree is complete.

| Planned worktree | Current state on `main` | Remaining work |
|---|---|---|
| `c1-llm-router` | Partial. `controlplane/internal/llm` exists, but `llm.NewClient` supports Anthropic only. | Add OpenAI and Google providers, fallback policy, and smoke tests for provider routing. |
| `c1-mcp-wrapper` | Partial/superseded. The system has an internal AI tool registry, not an MCP wrapper. | Decide whether MCP transport is still required. If yes, wrap the internal tool registry behind MCP-compatible schemas. |
| `c1-tooluse-loop` | Merged. `/api/v1/ai/ask` runs a bounded tool-use loop with tool results and traces. | Keep as base dependency for later work. |
| `c1-streaming-citations` | Partial. Tool citations are returned; streaming is not implemented. | Add streaming only if still required for pilot. Otherwise update the sprint target to citations-only. |
| `c1-tool-rbac` | Mostly merged. Tools have minimum role checks and audit records for tool calls. | Add per-tool policy tests for high-risk tools such as log-tail and action proposal. |
| `c1-operator-mode` | Partial. `operator_propose_action` exists and execution is refused. | Add anomaly-triggered investigation persistence and explicit approval path integration. |
| `c1-cve-kev-osv` | Not complete. | Implement CVE/KEV/OSV enrichment or move it out of the Sprint 5 gate. |
| `c1-agent-fatal-cleanup` | Not verified as complete in this audit. | Re-check agent error paths and open a small reliability PR if still missing. |
| `c1-process-tree-hydrate` | Not verified as complete in this audit. | Re-check process observation coverage and open a hydration PR if still missing. |
| `c1-critical-test-coverage` | Partial. AI/tool-loop tests exist, but the four named critical modules are not all covered. | Add focused tests for `ai_ask`, `compliance_evidence`, `dlp_scan`, and `anomaly_baselines`. |
| `c1-trivy-cve-detail` | Not complete. | Preserve Trivy CVE detail instead of aggregate-only results. |

Sprint 5 closeout gate should be changed from "all 11 rows landed" to "the merged umbrella PR is accepted as a partial Sprint 5 baseline, with the rows above tracked as explicit follow-up PRs."

## Sprint 6 Audit

PR #119 recovered the missing Sprint 6 code into `main`, but it is also an umbrella/partial implementation relative to the original Sprint 6 plan.

| Planned worktree | Current state on `main` | Remaining work |
|---|---|---|
| `c1-fs-watcher` | Not complete. `listFileGrowthDeltas` currently returns an empty slice unless an injected store implements it. | Add agent-side file size snapshots, storage, rollups, and tests for fast log growth. |
| `c1-flowrate-aggregator` | Partial. Flow deltas are computed from existing Doris connection rows. | Add durable 1m/5m/1h rollups or document why on-read aggregation is the chosen pilot path. |
| `c1-bandwidth-rollups` | Partial. Current flow delta payload includes byte sums from connection rows. | Add durable byte rollups and spike fixtures for the 2 TB scenario. |
| `c1-resource-delta-tool` | Partial/merged. Tool returns before/after deltas from telemetry metric rows. | Validate ordering semantics, add missing metric fixtures, and prove the 20% to 99% CPU/memory scenario. |
| `c1-log-tail-tool` | Partial. Tool can query Doris/Postgres logs. | Add redaction, byte/line caps as policy tests, and per-tool RBAC for app/db logs. |
| `c1-root-cause-synth` | Partial. Current root cause is deterministic and in-request; there is no durable findings table or worker. | Add `root_cause_findings` persistence and worker orchestration over all evidence streams. |
| `c1-auto-deescalate` | Proposal-only baseline. Execution tool refuses all execution. | Add approval-gated dry-run proposals first; defer actual truncate/kill actions until safety gates are implemented and reviewed. |

Sprint 6 should not be marked complete until the synthetic disk-fill incident produces durable evidence for all five dimensions and a root-cause finding within the defined window.

## Recommended PR Sequence

1. `docs/sprint-recovery-audit`: land this reconciliation so future work starts from an accurate status table.
2. `feat/c1-s5-llm-router-closeout`: add OpenAI/Google providers and fallback tests, or explicitly downgrade the target to Anthropic-only for pilot.
3. `feat/c1-s5-operator-closeout`: persist anomaly-triggered investigations and connect proposal-only actions to existing approval objects.
4. `feat/c1-s5-cve-kev-closeout`: implement CVE/KEV/OSV enrichment plus Trivy CVE detail, or move it to Sprint 7 with owner approval.
5. `test/c1-s5-critical-coverage`: add the missing focused tests for the four named critical modules.
6. `feat/c1-s6-file-growth`: implement the agent file-growth collector and storage path.
7. `feat/c1-s6-flow-rollups`: decide and implement durable rollups for flow rate and bandwidth.
8. `feat/c1-s6-log-tail-policy`: add redaction, byte caps, and per-tool RBAC tests.
9. `feat/c1-s6-root-cause-findings`: add durable root-cause findings and the worker/orchestration path.
10. `feat/c1-s6-auto-deescalate-proposals`: keep execution gated; add dry-run proposals, audits, and approval integration before any mutation path.

Only after those closeout gates are green should Sprint 7 start.
