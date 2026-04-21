# Rollback — allow AutoRun again (Microsoft default: enabled for fixed and
# network drives, disabled for removable only via the Autorun.inf signing rules).
$ErrorActionPreference = 'Stop'

$explorerKey = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\Explorer'
if (Test-Path $explorerKey) {
    Remove-ItemProperty -Path $explorerKey -Name NoDriveTypeAutoRun      -ErrorAction SilentlyContinue
    Remove-ItemProperty -Path $explorerKey -Name NoAutorun               -ErrorAction SilentlyContinue
    Remove-ItemProperty -Path $explorerKey -Name NoAutoplayfornonVolume  -ErrorAction SilentlyContinue
}

& gpupdate.exe /force | Out-Null
Write-Warning 'AutoRun policy overrides removed by rollback.'
