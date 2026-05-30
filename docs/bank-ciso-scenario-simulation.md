# Bank CISO Scenario Simulation

This is a repeatable, code-backed demo path for six scenarios a bank CISO will
usually care about. It does not mock a slide narrative; it runs focused tests
against the implemented parser, detection, source-health, exposure,
vulnerability, patch, approval, and investigation code paths.

Run from the repository root:

```powershell
powershell -ExecutionPolicy Bypass -File scripts/bank_ciso_scenario_simulation.ps1
```

The runner writes:

- `build/ciso-scenario-simulation/<timestamp>/summary.md`
- `build/ciso-scenario-simulation/<timestamp>/evidence.ndjson`
- one verbose `go test` log per scenario suite

## 1. Public Exposure And Private-Access Drift

CISO question: Can we prove a crown-jewel service is not accidentally public?

Simulation response:

- Classifies service reachability using node public IP, firewall default-deny,
  private-access provider state, NAT/LB exposure labels, and explicit public
  allow rules.
- Reduces critical exposure when default-deny evidence exists.
- Opens a cited SOC case from a private-access exposure finding.

Primary code evidence:

- `controlplane/internal/server/private_access.go`
- `controlplane/internal/server/control_room.go`
- `controlplane/internal/server/private_access_test.go`
- `controlplane/internal/server/control_room_test.go`

Runner tests:

- `TestPrivateAccessObservationsUseNodePublicIPAndDefaultDeny`
- `TestPrivateAccessExposureFindingCreatesSOCCase`
- `TestControlRoomDefaultDenyFirewallReducesCriticalExposure`

## 2. Credential Attack Against Banking App

CISO question: Do we detect a real auth-failure burst and turn it into a risk
view and investigation case?

Simulation response:

- Detects repeated auth failures against `/admin/login` as an IP-behavior
  anomaly.
- Opens a critical alert at 100 percent confidence without exposing scoring
  internals in operator copy.
- Aggregates the evidence into a credential-attack risk notable.
- Persists a tenant-scoped investigation incident with cited evidence.

Primary code evidence:

- `controlplane/internal/server/ip_behavior.go`
- `controlplane/internal/server/risk_notables.go`
- `controlplane/internal/server/ai_case_workflow_tools.go`
- `controlplane/internal/server/ip_behavior_test.go`
- `controlplane/internal/server/risk_notables_test.go`
- `controlplane/internal/server/ai_case_workflow_tools_test.go`

Runner tests:

- `TestDetectIPBehaviorOpensAlertAtFullConfidence`
- `TestRiskNotablesAggregatesExistingRiskEvidence`
- `TestIncidentCreateToolPersistsTenantScopedInvestigation`

## 3. Ransomware Or Suspicious PowerShell Signal

CISO question: Can content packs normalize Windows and bank security logs,
replay detections, and alert without duplicate noise?

Simulation response:

- Validates and replays the bank starter content pack before enablement.
- Normalizes Sysmon and PowerShell event data into queryable security fields.
- Covers common bank security inputs such as WAF and Windows telemetry.
- Creates a deduped content-pack alert for encoded PowerShell behavior.

Primary code evidence:

- `internal/contentpacks/bank_security_pack.go`
- `internal/contentpacks/parser_formats.go`
- `controlplane/internal/server/content_pack_detections.go`
- `internal/contentpacks/bank_security_pack_test.go`
- `internal/contentpacks/parser_test.go`
- `controlplane/internal/server/content_pack_detections_test.go`

Runner tests:

- `TestBankSecurityStarterPackValidatesAndReplays`
- `TestBankSecurityStarterPackWindowsSemanticParsers`
- `TestBankSecurityStarterPackWAFSemanticParser`
- `TestParserRuntimeNormalizesPowerShellScriptBlock`
- `TestParserRuntimeNormalizesSysmonProcessCreate`
- `TestEvaluateContentPackDetectionsCreatesDedupedAlert`
- `TestHandleContentPackDetectionReplayReturnsReports`
- `TestHandleContentPackLifecycleEnableReplaysAndAudits`

## 4. Critical CVE To Governed Patch Execution

CISO question: Can we move from CVE evidence to a patch plan without bypassing
approvals or change controls?

Simulation response:

- Returns CVE, package, fixed-version, KEV/EPSS/CVSS, and source-row evidence.
- Generates proposal-only patch plans from the AI investigation tool layer.
- Enforces package allow/deny policy, canary waves, and manual wave advance.
- Requires dual approval for high-risk action plans and records receipts.

Primary code evidence:

- `controlplane/internal/server/node_vulnerabilities.go`
- `controlplane/internal/server/ai_investigation_tools.go`
- `controlplane/internal/server/patch.go`
- `controlplane/internal/server/action_plans.go`
- `controlplane/internal/server/node_vulnerabilities_test.go`
- `controlplane/internal/server/patch_test.go`
- `controlplane/internal/server/action_plans_test.go`

Runner tests:

- `TestHandleNodeVulnerabilitiesReturnsCVEPackageEvidence`
- `TestNodeVulnerabilityPatchPlanIsProposalOnlyAndCited`
- `TestVulnerabilityPatchPlanAIToolRequiresOperatorAndReturnsPlan`
- `TestPatchDeploy_CanaryWaveAdvanceAndPackagePolicy`
- `TestActionPlansDualApprovalWorkflow`

## 5. SIEM Blind Spot, Backpressure, And Duplicate Storm

CISO question: Can we show collection health, open a case for a blind spot, and
avoid storing redundant log storms as raw hot facts?

Simulation response:

- Shows source runtime health with accepted/parsed/error/drop/backpressure
  evidence.
- Converts bad source health into a cited SOC case.
- Projects agent spool pressure into source-health state.
- Projects approved local-source log ingest into runtime health.
- Coalesces 1,200 identical hot messages in 20 minutes into one Doris analytic
  fact with `coalesced_count=1200` and capped sample timestamps/refs.

Primary code evidence:

- `controlplane/internal/server/content_packs.go`
- `controlplane/internal/server/heartbeat.go`
- `controlplane/internal/server/telemetry.go`
- `controlplane/internal/server/events_ingest.go`
- `controlplane/internal/server/content_packs_test.go`
- `controlplane/internal/server/heartbeat_test.go`
- `controlplane/internal/server/recommendations_test.go`
- `controlplane/internal/server/events_ingest_test.go`

Runner tests:

- `TestContentPackSourceHealthAPIListsCollectorEvidence`
- `TestContentPackSourceHealthInvestigationCreatesSOCCase`
- `TestHeartbeatProjectsAgentSpoolBackpressureToSourceHealth`
- `TestLogIngestProjectsAgentLocalSourceRuntimeState`
- `TestCoalesceDorisHotEventsGroupsRepeatedLogLinesInTwentyMinuteBucket`

## 6. Laravel Log Growth And Disk-Pressure Temporary Fix

CISO question: Can we predict disk pressure from runaway Laravel logs and stage
a safe temporary repair before the server fails?

Simulation response:

- Scores a sudden disk usage move, for example 55 percent to 76 percent, as a
  predictive health risk before the server reaches a critical disk threshold.
- Recognizes Laravel database credential failures that are consistent with
  stale cached configuration after a credential rotation.
- Produces a temporary cache refresh/reload runbook with `php artisan
  config:clear`, `cache:clear`, `config:cache`, runtime reload, and post-fix log
  verification steps.
- Requires approval before dispatching a privileged temporary repair script, so
  the system can propose and stage the fix without silently mutating a bank
  production application.

Primary code evidence:

- `controlplane/internal/server/node_predictive.go`
- `controlplane/internal/server/ai_logfixer.go`
- `controlplane/internal/server/remediation.go`
- `controlplane/internal/server/node_predictive_test.go`
- `controlplane/internal/server/ai_logfixer_test.go`
- `controlplane/internal/server/remediation_execute_test.go`

Runner tests:

- `TestHealthPredictScoresDiskUsageSurgeBeforeCritical`
- `TestBuildAILogFixerTriggerBundleRecognizesLaravelConfigCacheDrift`
- `TestLaravelTemporaryFixScriptRequiresApprovalBeforeExecution`

## Demo Framing

For a CISO audience, "perfect response" should mean deterministic, cited,
governed, and repeatably test-passing:

- Deterministic: same inputs produce the same finding, alert, case, plan, or
  coalesced analytic fact.
- Cited: investigation and action paths carry evidence references.
- Governed: high-risk changes require approval, wave/canary controls, and
  receipts.
- Operationally honest: source-health, backpressure, parser failures, and
  coverage gaps are visible instead of hidden.

Do not position this as a substitute for a customer environment acceptance
test. Bank go-live still needs the HA/DR drill, private-access exposure
signoff, capacity sizing, backup/restore evidence, and customer-specific
change-control approvals described in the go-live runbooks.
