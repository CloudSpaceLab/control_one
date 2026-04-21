# Rollback — disable LSA Protection. Use only if a legacy driver is blocked.
$ErrorActionPreference = 'Stop'

$regPath = 'HKLM:\SYSTEM\CurrentControlSet\Control\Lsa'
if (Test-Path $regPath) {
    Set-ItemProperty -Path $regPath -Name RunAsPPL -Value 0 -Type DWord
    Remove-ItemProperty -Path $regPath -Name RunAsPPLBoot -ErrorAction SilentlyContinue
}

Write-Warning 'LSA Protection (RunAsPPL) DISABLED by rollback. Reboot to apply.'
