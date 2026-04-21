# CIS Windows 18.3.7 — Enable LSA Protection (RunAsPPL)
# SOC2 CC6.8 — Protect credential store from memory scraping
$ErrorActionPreference = 'Stop'

$regPath = 'HKLM:\SYSTEM\CurrentControlSet\Control\Lsa'
if (-not (Test-Path $regPath)) {
    throw "Registry path $regPath does not exist (unexpected on Windows)"
}

# 1 = LSA runs as a Protected Process Light; a reboot is required to take effect.
Set-ItemProperty -Path $regPath -Name RunAsPPL -Value 1 -Type DWord

# On Windows Server 2022 / Win11 22H2+, RunAsPPLBoot enforces the protection at
# boot even against kernel drivers. Set it if the host supports it.
try {
    Set-ItemProperty -Path $regPath -Name RunAsPPLBoot -Value 1 -Type DWord
} catch {
    Write-Output 'RunAsPPLBoot not supported on this host (pre-Win11 22H2 / WS2022): skipping'
}

Write-Output 'LSA Protection (RunAsPPL) enabled — reboot required to activate.'
