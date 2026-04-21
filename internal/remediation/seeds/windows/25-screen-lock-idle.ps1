# CIS Windows 19.1.3.1/2/3 — Screen saver password-protected with 900s (15m) timeout
# SOC2 CC6.1 — Idle-session lockout for workstations
$ErrorActionPreference = 'Stop'

# Per-machine policy under HKLM covers every interactive user on the host.
$policyKey = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Control Panel\Desktop'
if (-not (Test-Path $policyKey)) {
    New-Item -Path $policyKey -Force | Out-Null
}

Set-ItemProperty -Path $policyKey -Name ScreenSaveActive      -Value '1' -Type String
Set-ItemProperty -Path $policyKey -Name ScreenSaverIsSecure   -Value '1' -Type String
Set-ItemProperty -Path $policyKey -Name ScreenSaveTimeOut     -Value '900' -Type String
# Blank screensaver — harmless, always available, no GPU workload.
Set-ItemProperty -Path $policyKey -Name 'SCRNSAVE.EXE'        -Value 'scrnsave.scr' -Type String

# Inactivity limit at the logon boundary (takes precedence over screensaver).
$lsaKey = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System'
if (-not (Test-Path $lsaKey)) {
    New-Item -Path $lsaKey -Force | Out-Null
}
Set-ItemProperty -Path $lsaKey -Name InactivityTimeoutSecs -Value 900 -Type DWord

& gpupdate.exe /force | Out-Null
Write-Output 'Screen lock policy set: 15min idle → locked with password'
