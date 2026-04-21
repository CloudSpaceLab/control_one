# Rollback — disables real-time monitoring. Only use on hosts that run a
# third-party EDR which conflicts with Defender.
$ErrorActionPreference = 'Stop'

if (-not (Get-Command Set-MpPreference -ErrorAction SilentlyContinue)) {
    Write-Output 'Defender not present; nothing to rollback'
    return
}

Set-MpPreference -DisableRealtimeMonitoring $true
Write-Warning 'Windows Defender real-time protection DISABLED by rollback.'
