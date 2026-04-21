# CIS Windows 18.9.47.x — Ensure Windows Defender real-time protection is enabled
# SOC2 CC6.8 — Endpoint malware defence
$ErrorActionPreference = 'Stop'

# Fail fast if Defender is missing entirely (e.g. server without AV feature).
if (-not (Get-Command Set-MpPreference -ErrorAction SilentlyContinue)) {
    throw 'Windows Defender (Set-MpPreference) is not available on this host'
}

Set-MpPreference -DisableRealtimeMonitoring $false
Set-MpPreference -DisableBehaviorMonitoring $false
Set-MpPreference -DisableIOAVProtection $false
Set-MpPreference -MAPSReporting Advanced
Set-MpPreference -SubmitSamplesConsent SendSafeSamples

# Force a signature update so protection is actually current.
Update-MpSignature -ErrorAction SilentlyContinue

$status = Get-MpComputerStatus
Write-Output ("Defender RealTimeProtectionEnabled={0} AntivirusEnabled={1}" -f `
    $status.RealTimeProtectionEnabled, $status.AntivirusEnabled)

if (-not $status.RealTimeProtectionEnabled) {
    throw 'Real-time protection failed to enable'
}
