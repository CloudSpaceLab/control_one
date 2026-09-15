[CmdletBinding()]
param(
    [string]$Server = "http://127.0.0.1:8443",
    [string]$Mailpit = "http://127.0.0.1:8025",
    [string]$Email = "admin@local",
    [string]$TenantID = "",
    [string]$CredentialsFile = "",
    [switch]$KeepRule
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
if (-not $CredentialsFile) {
    $CredentialsFile = Join-Path $repositoryRoot "tmp/local/credentials.json"
}

function Get-PlainTextPassword {
    param([Security.SecureString]$SecurePassword)

    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecurePassword)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
}

function Find-Message {
    param(
        [string]$MailpitBaseURL,
        [string]$Subject
    )

    $messages = Invoke-RestMethod -Method Get -Uri "$MailpitBaseURL/api/v1/messages"
    return $messages.messages |
        Where-Object Subject -eq $Subject |
        Select-Object -First 1
}

$password = $null
if (Test-Path -LiteralPath $CredentialsFile) {
    $account = Get-Content -Raw -LiteralPath $CredentialsFile |
        ConvertFrom-Json |
        Where-Object email -eq $Email |
        Select-Object -First 1
    if ($account) {
        $password = $account.password
    }
}

if (-not $password) {
    $securePassword = Read-Host "Password for $Email" -AsSecureString
    $password = Get-PlainTextPassword -SecurePassword $securePassword
}

$loginBody = @{
    email = $Email
    password = $password
} | ConvertTo-Json
$password = $null

$login = Invoke-RestMethod `
    -Method Post `
    -Uri "$Server/api/v1/auth/login" `
    -ContentType "application/json" `
    -Body $loginBody

$headers = @{ Authorization = "Bearer $($login.token)" }

if (-not $TenantID) {
    $tenants = Invoke-RestMethod `
        -Method Get `
        -Uri "$Server/api/v1/tenants?limit=1" `
        -Headers $headers
    $TenantID = $tenants.data[0].id
}
if (-not $TenantID) {
    throw "No tenant is available. Supply -TenantID or create a local tenant first."
}

$runID = [guid]::NewGuid().ToString("N").Substring(0, 10)
$title = "Repeated high-risk activity ($runID)"
$expectedSubject = "Control One alert: [HIGH] $title"
$rule = $null
$alert = $null
$message = $null

try {
    $ruleBody = @{
        tenant_id = $TenantID
        name = $title
        description = "Local correlation email delivery test"
        event_types = @("security.event")
        window_seconds = 300
        threshold = 3
        dimension = "tenant_id"
        severity = "high"
        enabled = $true
    } | ConvertTo-Json

    $rule = Invoke-RestMethod `
        -Method Post `
        -Uri "$Server/api/v1/correlation-rules" `
        -Headers $headers `
        -ContentType "application/json" `
        -Body $ruleBody

    Write-Host "Created correlation rule $($rule.id)."
    Write-Host "Waiting 31 seconds for the correlation rule cache to refresh..."
    Start-Sleep -Seconds 31

    1..3 | ForEach-Object {
        $attempt = $_
        $eventBody = @{
            tenant_id = $TenantID
            event_type = "demo.repeated_failed_login"
            severity = "high"
            source = "correlation-email-demo"
            details = @{
                test_run = $runID
                attempt = $attempt
            }
            dedup_key = "correlation-email-demo/$runID/$attempt"
        } | ConvertTo-Json -Depth 4

        $event = Invoke-RestMethod `
            -Method Post `
            -Uri "$Server/api/v1/security-events" `
            -Headers $headers `
            -ContentType "application/json" `
            -Body $eventBody

        Write-Host "Created security event ${attempt}: $($event.id)"
    }

    for ($attempt = 1; $attempt -le 20; $attempt++) {
        $alerts = Invoke-RestMethod `
            -Method Get `
            -Uri "$Server/api/v1/alerts?tenant_id=$TenantID&limit=100" `
            -Headers $headers
        $alert = $alerts.data |
            Where-Object rule_id -eq $rule.id |
            Select-Object -First 1
        if ($alert) {
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $alert) {
        throw "No alert was created for correlation rule $($rule.id)."
    }

    for ($attempt = 1; $attempt -le 20; $attempt++) {
        $message = Find-Message -MailpitBaseURL $Mailpit -Subject $expectedSubject
        if ($message) {
            break
        }
        Start-Sleep -Seconds 1
    }
    if (-not $message) {
        throw "Mailpit did not receive '$expectedSubject'."
    }

    [pscustomobject]@{
        Result = "PASS"
        RunID = $runID
        TenantID = $TenantID
        RuleID = $rule.id
        AlertID = $alert.id
        AlertSource = $alert.source
        AlertState = $alert.state
        MailID = $message.ID
        MailSubject = $message.Subject
        MailURL = "$Mailpit/view/$($message.ID)"
    }
}
finally {
    if ($rule -and -not $KeepRule) {
        try {
            Invoke-RestMethod `
                -Method Delete `
                -Uri "$Server/api/v1/correlation-rules/$($rule.id)?tenant_id=$TenantID" `
                -Headers $headers | Out-Null
            Write-Host "Deleted temporary correlation rule $($rule.id)."
        }
        catch {
            Write-Warning "Could not delete temporary correlation rule $($rule.id): $($_.Exception.Message)"
        }
    }
}
