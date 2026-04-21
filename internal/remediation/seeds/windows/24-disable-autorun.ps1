# CIS Windows 18.9.8.x — Disable Autoplay / AutoRun for all drives
# SOC2 CC6.8 — Prevent automatic execution from removable media
$ErrorActionPreference = 'Stop'

$explorerKey = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Explorer'
if (-not (Test-Path $explorerKey)) {
    New-Item -Path $explorerKey -Force | Out-Null
}

# NoDriveTypeAutoRun = 0xFF (255) disables AutoRun on every drive type.
Set-ItemProperty -Path $explorerKey -Name NoDriveTypeAutoRun -Value 0xFF -Type DWord

# NoAutorun = 1 disables the AutoRun command as well.
Set-ItemProperty -Path $explorerKey -Name NoAutorun -Value 1 -Type DWord

# NoAutoplayfornonVolume = 1 disables autoplay for non-volume devices (MTP, etc).
Set-ItemProperty -Path $explorerKey -Name NoAutoplayfornonVolume -Value 1 -Type DWord

& gpupdate.exe /force | Out-Null
Write-Output 'AutoRun/AutoPlay disabled for all drive types'
