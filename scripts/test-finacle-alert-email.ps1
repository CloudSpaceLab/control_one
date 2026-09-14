[CmdletBinding()]
param(
    [string]$Server = "http://127.0.0.1:8443",
    [string]$Mailpit = "http://127.0.0.1:8025",
    [string]$DatabaseURL = "postgresql://controlone:controlone@127.0.0.1:55432/controlone?sslmode=disable",
    [string]$Email = "admin@local",
    [string]$TenantID = "",
    [string]$CredentialsFile = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
if (-not $CredentialsFile) {
    $CredentialsFile = Join-Path $repositoryRoot "tmp/local/credentials.json"
}

$account = Get-Content -Raw -LiteralPath $CredentialsFile |
    ConvertFrom-Json |
    Where-Object email -eq $Email |
    Select-Object -First 1
if (-not $account) {
    throw "Account $Email was not found in $CredentialsFile."
}

$login = Invoke-RestMethod `
    -Method Post `
    -Uri "$Server/api/v1/auth/login" `
    -ContentType "application/json" `
    -Body (@{ email = $account.email; password = $account.password } | ConvertTo-Json)
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
$finacleUID = "LOCAL-EMAIL-TEST-$runID"
$expectedSubject = "Control One alert: [CRITICAL] Finacle shift rotation"
$connection = $null
$shift = $null
$profileSeeded = $false

$beforeMessages = Invoke-RestMethod -Method Get -Uri "$Mailpit/api/v1/messages"
$existingMailIDs = @{}
$beforeMessages.messages | ForEach-Object { $existingMailIDs[$_.ID] = $true }

try {
    $connection = Invoke-RestMethod `
        -Method Post `
        -Uri "$Server/api/v1/finacle/connections" `
        -Headers $headers `
        -ContentType "application/json" `
        -Body (@{
            tenant_id = $TenantID
            host = "https://finacle-unavailable-$runID.local"
            auth_method = "basic"
        } | ConvertTo-Json)

    $shift = Invoke-RestMethod `
        -Method Post `
        -Uri "$Server/api/v1/finacle/shift-configs" `
        -Headers $headers `
        -ContentType "application/json" `
        -Body (@{
            tenant_id = $TenantID
            branch_id = "LOCAL-TEST"
            model = "always_on"
            shifts = @()
            grace_minutes = 0
        } | ConvertTo-Json)

    Push-Location $repositoryRoot
    try {
        $profileID = & go run ./scripts/finacle-alert-fixture `
            -action seed `
            -database-url $DatabaseURL `
            -tenant-id $TenantID `
            -shift-id $shift.id `
            -finacle-uid $finacleUID
        if ($LASTEXITCODE -ne 0) {
            throw "Failed to seed the temporary Finacle profile."
        }
        $profileSeeded = $true
    }
    finally {
        Pop-Location
    }

    $rotation = Invoke-RestMethod `
        -Method Post `
        -Uri "$Server/api/v1/finacle/shift-rotate" `
        -Headers $headers `
        -ContentType "application/json" `
        -Body (@{
            tenant_id = $TenantID
            shift_id = $shift.id
            direction = "enable"
        } | ConvertTo-Json)

    $alert = $null
    for ($attempt = 1; $attempt -le 30; $attempt++) {
        $alerts = Invoke-RestMethod `
            -Method Get `
            -Uri "$Server/api/v1/alerts?tenant_id=$TenantID&limit=100" `
            -Headers $headers
        $alert = $alerts.data |
            Where-Object {
                $_.source -eq "finacle" -and
                $_.context.shift_id -eq $shift.id -and
                $_.context.direction -eq "enable"
            } |
            Select-Object -First 1
        if ($alert) { break }
        Start-Sleep -Seconds 1
    }
    if (-not $alert) {
        throw "No Finacle alert was created for shift $($shift.id)."
    }

    $message = $null
    for ($attempt = 1; $attempt -le 20; $attempt++) {
        $messages = Invoke-RestMethod -Method Get -Uri "$Mailpit/api/v1/messages"
        $message = $messages.messages |
            Where-Object { $_.Subject -eq $expectedSubject -and -not $existingMailIDs.ContainsKey($_.ID) } |
            Select-Object -First 1
        if ($message) { break }
        Start-Sleep -Seconds 1
    }
    if (-not $message) {
        throw "Mailpit did not receive a new '$expectedSubject' message."
    }

    [pscustomobject]@{
        Result = "PASS"
        RunID = $runID
        ConnectionID = $connection.id
        ShiftID = $shift.id
        ProfileID = "$profileID".Trim()
        RotateJobID = $rotation.rotate_job_id
        AlertID = $alert.id
        AlertSource = $alert.source
        AlertState = $alert.state
        MailID = $message.ID
        MailSubject = $message.Subject
        MailURL = "$Mailpit/view/$($message.ID)"
    }
}
finally {
    if ($profileSeeded) {
        Push-Location $repositoryRoot
        try {
            & go run ./scripts/finacle-alert-fixture `
                -action delete `
                -database-url $DatabaseURL `
                -tenant-id $TenantID `
                -finacle-uid $finacleUID
            if ($LASTEXITCODE -ne 0) {
                Write-Warning "Could not delete temporary Finacle profile $finacleUID."
            }
        }
        finally {
            Pop-Location
        }
    }
    if ($shift) {
        try {
            Invoke-RestMethod -Method Delete -Uri "$Server/api/v1/finacle/shift-configs/$($shift.id)" -Headers $headers | Out-Null
        }
        catch { Write-Warning "Could not delete temporary Finacle shift $($shift.id)." }
    }
    if ($connection) {
        try {
            Invoke-RestMethod -Method Delete -Uri "$Server/api/v1/finacle/connections/$($connection.id)" -Headers $headers | Out-Null
        }
        catch { Write-Warning "Could not delete temporary Finacle connection $($connection.id)." }
    }
}
