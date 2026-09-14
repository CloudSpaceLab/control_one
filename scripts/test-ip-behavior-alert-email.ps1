[CmdletBinding()]
param(
    [string]$Server = "http://127.0.0.1:8443",
    [string]$Mailpit = "http://127.0.0.1:8025",
    [string]$Email = "admin@local",
    [string]$CredentialsFile = "",
    [string]$NodeSessionFile = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

$repositoryRoot = Split-Path -Parent $PSScriptRoot
if (-not $CredentialsFile) {
    $CredentialsFile = Join-Path $repositoryRoot "tmp/local/credentials.json"
}
if (-not $NodeSessionFile) {
    $NodeSessionFile = Join-Path $repositoryRoot "tmp/local/content-demo/node-session.json"
}
if (-not (Test-Path -LiteralPath $NodeSessionFile)) {
    throw "Node session not found at $NodeSessionFile. Supply -NodeSessionFile for an enrolled local node."
}

$node = Get-Content -Raw -LiteralPath $NodeSessionFile | ConvertFrom-Json
if (-not $node.node_token -or -not $node.tenant_id) {
    throw "Node session must contain node_token and tenant_id."
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

$runID = [guid]::NewGuid().ToString("N").Substring(0, 10)
$octets = [Convert]::ToInt32($runID.Substring(0, 2), 16), [Convert]::ToInt32($runID.Substring(2, 2), 16)
$sourceIP = "10.254.$(($octets[0] % 254) + 1).$(($octets[1] % 254) + 1)"
$eventID = "ip-behavior-email-demo-$runID"
$title = "100% confidence known malicious source from $sourceIP"
$expectedSubject = "Control One alert: [CRITICAL] $title"

$event = @{
    schema_version = 1
    event_id = $eventID
    type = "web.request"
    ts = (Get-Date).ToUniversalTime().ToString("o")
    collector = "local-ip-behavior-test"
    parser_status = "parsed"
    severity = "critical"
    src_ip = $sourceIP
    threat_feed = "local-test-feed"
    threat_score = 100
    message = "Known malicious source requested the login endpoint"
    details = @{
        status_code = 401
        path = "/login"
        country_code = "ZZ"
        country = "Local test"
        asn = "AS64512"
        app = "local-test-app"
        server_group = "local-test"
    }
} | ConvertTo-Json -Compress -Depth 5

$nodeHeaders = @{
    Authorization = "Bearer $($node.node_token)"
    "X-ControlOne-Replay-Key" = $eventID
}
$ingest = Invoke-RestMethod `
    -Method Post `
    -Uri "$Server/api/v1/events/ingest" `
    -Headers $nodeHeaders `
    -ContentType "application/x-ndjson" `
    -Body ($event + "`n")

$alert = $null
for ($attempt = 1; $attempt -le 20; $attempt++) {
    $alerts = Invoke-RestMethod `
        -Method Get `
        -Uri "$Server/api/v1/alerts?tenant_id=$($node.tenant_id)&limit=100" `
        -Headers $headers
    $alert = $alerts.data |
        Where-Object { $_.source -eq "ip_behavior" -and $_.title -eq $title } |
        Select-Object -First 1
    if ($alert) { break }
    Start-Sleep -Seconds 1
}
if (-not $alert) {
    throw "No IP-behavior alert was created for $sourceIP."
}

$message = $null
for ($attempt = 1; $attempt -le 20; $attempt++) {
    $messages = Invoke-RestMethod -Method Get -Uri "$Mailpit/api/v1/messages"
    $message = $messages.messages |
        Where-Object Subject -eq $expectedSubject |
        Select-Object -First 1
    if ($message) { break }
    Start-Sleep -Seconds 1
}
if (-not $message) {
    throw "Mailpit did not receive '$expectedSubject'."
}

[pscustomobject]@{
    Result = "PASS"
    RunID = $runID
    EventID = $eventID
    BatchID = $ingest.batch_id
    SourceIP = $sourceIP
    AlertID = $alert.id
    AlertSource = $alert.source
    AlertState = $alert.state
    MailID = $message.ID
    MailSubject = $message.Subject
    MailURL = "$Mailpit/view/$($message.ID)"
}
