# CIS Windows 18.3.2 — Disable SMBv1 server and client
# SOC2 CC6.6 — Remove known-vulnerable legacy network protocols
$ErrorActionPreference = 'Stop'

# Server side — disable protocol on the SMB server.
Set-SmbServerConfiguration -EnableSMB1Protocol $false -Force

# Client side — disable the SMB1 feature so the workstation stops negotiating it.
$feature = Get-WindowsOptionalFeature -Online -FeatureName SMB1Protocol -ErrorAction SilentlyContinue
if ($feature -and $feature.State -eq 'Enabled') {
    Disable-WindowsOptionalFeature -Online -FeatureName SMB1Protocol -NoRestart | Out-Null
}

# Belt-and-braces: block the legacy driver from starting.
$regPath = 'HKLM:\SYSTEM\CurrentControlSet\Services\mrxsmb10'
if (Test-Path $regPath) {
    Set-ItemProperty -Path $regPath -Name Start -Value 4
}

Write-Output 'SMBv1 disabled (server + client + driver)'
