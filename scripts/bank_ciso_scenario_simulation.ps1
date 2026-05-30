param(
    [string]$ArtifactDir = "",
    [switch]$ContinueOnFailure
)

$ErrorActionPreference = "Stop"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "go is required to run the bank CISO scenario simulation"
}

if ([string]::IsNullOrWhiteSpace($ArtifactDir)) {
    $stamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssZ")
    $ArtifactDir = Join-Path "build/ciso-scenario-simulation" $stamp
}

New-Item -ItemType Directory -Force -Path $ArtifactDir | Out-Null
$evidenceFile = Join-Path $ArtifactDir "evidence.ndjson"
$summaryFile = Join-Path $ArtifactDir "summary.md"
if (Test-Path $evidenceFile) {
    Remove-Item -LiteralPath $evidenceFile
}

$scenarios = @(
    [ordered]@{
        Id = "01"
        Name = "Public exposure and private-access drift"
        CisoQuestion = "Can we prove a crown-jewel service is not accidentally public?"
        ExpectedResponse = "Classify public/private reachability, account for firewall default-deny/private-access state, and create a cited SOC case from exposure findings."
        Suites = @(
            [ordered]@{
                Package = "./controlplane/internal/server"
                Run = "Test(PrivateAccessObservationsUseNodePublicIPAndDefaultDeny|PrivateAccessExposureFindingCreatesSOCCase|ControlRoomDefaultDenyFirewallReducesCriticalExposure)$"
            }
        )
    },
    [ordered]@{
        Id = "02"
        Name = "Credential attack against internet-facing banking app"
        CisoQuestion = "Do we detect a real auth-failure burst and turn it into a risk view and investigation case?"
        ExpectedResponse = "Open a critical IP-behavior alert at full confidence, aggregate the credential-attack notable, and persist a tenant-scoped cited incident."
        Suites = @(
            [ordered]@{
                Package = "./controlplane/internal/server"
                Run = "Test(DetectIPBehaviorOpensAlertAtFullConfidence|RiskNotablesAggregatesExistingRiskEvidence|IncidentCreateToolPersistsTenantScopedInvestigation)$"
            }
        )
    },
    [ordered]@{
        Id = "03"
        Name = "Ransomware or suspicious PowerShell signal"
        CisoQuestion = "Can content packs normalize Windows and bank security logs, replay detections, and alert without duplicate noise?"
        ExpectedResponse = "Validate/replay the bank starter pack, normalize PowerShell/Sysmon events, and create a deduped content-pack detection alert."
        Suites = @(
            [ordered]@{
                Package = "./internal/contentpacks"
                Run = "Test(ParserRuntimeNormalizesPowerShellScriptBlock|ParserRuntimeNormalizesSysmonProcessCreate|BankSecurityStarterPackValidatesAndReplays|BankSecurityStarterPackWindowsSemanticParsers|BankSecurityStarterPackWAFSemanticParser)$"
            },
            [ordered]@{
                Package = "./controlplane/internal/server"
                Run = "Test(EvaluateContentPackDetectionsCreatesDedupedAlert|HandleContentPackDetectionReplayReturnsReports|HandleContentPackLifecycleEnableReplaysAndAudits)$"
            }
        )
    },
    [ordered]@{
        Id = "04"
        Name = "Critical CVE to governed patch execution"
        CisoQuestion = "Can we move from CVE evidence to a patch plan without bypassing approvals or change controls?"
        ExpectedResponse = "Return CVE/package/fixed-version evidence, generate a proposal-only patch plan, enforce canary/wave/package policy, and require dual approval for high-risk action plans."
        Suites = @(
            [ordered]@{
                Package = "./controlplane/internal/server"
                Run = "Test(HandleNodeVulnerabilitiesReturnsCVEPackageEvidence|NodeVulnerabilityPatchPlanIsProposalOnlyAndCited|VulnerabilityPatchPlanAIToolRequiresOperatorAndReturnsPlan|PatchDeploy_CanaryWaveAdvanceAndPackagePolicy|ActionPlansDualApprovalWorkflow)$"
            }
        )
    },
    [ordered]@{
        Id = "05"
        Name = "SIEM blind spot, collector backpressure, and duplicate storm"
        CisoQuestion = "Can we show collection health, open a case for a blind spot, and avoid storing redundant log storms as raw hot facts?"
        ExpectedResponse = "Expose source-health evidence, convert bad source health to a cited SOC case, report agent spool pressure, project local-source runtime, and coalesce 1,200 identical hot messages into one analytic fact."
        Suites = @(
            [ordered]@{
                Package = "./controlplane/internal/server"
                Run = "Test(ContentPackSourceHealthAPIListsCollectorEvidence|ContentPackSourceHealthInvestigationCreatesSOCCase|HeartbeatProjectsAgentSpoolBackpressureToSourceHealth|CoalesceDorisHotEventsGroupsRepeatedLogLinesInTwentyMinuteBucket|LogIngestProjectsAgentLocalSourceRuntimeState)$"
            }
        )
    },
    [ordered]@{
        Id = "06"
        Name = "Laravel log growth and disk-pressure temporary fix"
        CisoQuestion = "Can we predict disk pressure from runaway Laravel logs and stage a safe temporary repair before the server fails?"
        ExpectedResponse = "Score a 20 point disk-usage surge before the disk is critical, recognize stale Laravel DB config/cache drift from error logs, and require approval before dispatching the temporary cache refresh/reload runbook."
        Suites = @(
            [ordered]@{
                Package = "./controlplane/internal/server"
                Run = "Test(HealthPredictScoresDiskUsageSurgeBeforeCritical|BuildAILogFixerTriggerBundleRecognizesLaravelConfigCacheDrift|LaravelTemporaryFixScriptRequiresApprovalBeforeExecution)$"
            }
        )
    }
)

function Write-Evidence {
    param(
        [hashtable]$Scenario,
        [string]$SuitePackage,
        [string]$Status,
        [string]$Detail,
        [string]$LogFile
    )
    $entry = [ordered]@{
        timestamp = (Get-Date).ToUniversalTime().ToString("o")
        scenario_id = $Scenario.Id
        scenario = $Scenario.Name
        suite_package = $SuitePackage
        status = $Status
        detail = $Detail
        log_file = $LogFile
    }
    ($entry | ConvertTo-Json -Compress) | Add-Content -Path $evidenceFile -Encoding utf8
}

$results = @()
$overallFailed = $false

foreach ($scenario in $scenarios) {
    Write-Host "[$($scenario.Id)] $($scenario.Name)"
    Write-Host "  CISO question: $($scenario.CisoQuestion)"

    $scenarioFailed = $false
    foreach ($suite in $scenario.Suites) {
        $safePackage = ($suite.Package -replace "[^A-Za-z0-9]+", "_").Trim("_")
        $logFile = Join-Path $ArtifactDir "$($scenario.Id)-$safePackage.log"
        $args = @("test", $suite.Package, "-run", $suite.Run, "-count=1", "-v")

        Write-Host "  go $($args -join ' ')"
        $output = & go @args 2>&1
        $exitCode = $LASTEXITCODE
        $output | Set-Content -Path $logFile -Encoding utf8

        if ($exitCode -eq 0) {
            Write-Evidence -Scenario $scenario -SuitePackage $suite.Package -Status "passed" -Detail $scenario.ExpectedResponse -LogFile $logFile
            Write-Host "  passed: $($suite.Package)"
        } else {
            $scenarioFailed = $true
            $overallFailed = $true
            Write-Evidence -Scenario $scenario -SuitePackage $suite.Package -Status "failed" -Detail "go test exited $exitCode" -LogFile $logFile
            Write-Host "  failed: $($suite.Package) (see $logFile)"
            if (-not $ContinueOnFailure) {
                break
            }
        }
    }

    $results += [ordered]@{
        Id = $scenario.Id
        Name = $scenario.Name
        Status = $(if ($scenarioFailed) { "failed" } else { "passed" })
        ExpectedResponse = $scenario.ExpectedResponse
    }

    if ($scenarioFailed -and -not $ContinueOnFailure) {
        break
    }
}

$summary = @()
$summary += "# Bank CISO Scenario Simulation"
$summary += ""
$summary += "Generated: $((Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ"))"
$summary += ""
$summary += "Artifact directory: $ArtifactDir"
$summary += ""
$summary += "| Scenario | Status | Expected response proven by tests |"
$summary += "| --- | --- | --- |"
foreach ($result in $results) {
    $summary += "| $($result.Id) - $($result.Name) | $($result.Status) | $($result.ExpectedResponse) |"
}
$summary += ""
$summary += "Raw evidence: evidence.ndjson"
$summary | Set-Content -Path $summaryFile -Encoding utf8

Write-Host ""
Write-Host "Summary: $summaryFile"
Write-Host "Evidence: $evidenceFile"

if ($overallFailed) {
    exit 1
}
