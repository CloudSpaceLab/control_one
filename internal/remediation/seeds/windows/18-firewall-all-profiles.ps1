# CIS Windows 9.1.1 / 9.2.1 / 9.3.1 — Ensure Windows Firewall is on for Domain, Private, and Public
# SOC2 CC6.6 — Host-based firewall boundary
$ErrorActionPreference = 'Stop'

foreach ($profile in @('Domain','Private','Public')) {
    Set-NetFirewallProfile -Profile $profile -Enabled True -DefaultInboundAction Block -DefaultOutboundAction Allow -NotifyOnListen True -AllowUnicastResponseToMulticast False -LogFileName "%SystemRoot%\System32\LogFiles\Firewall\pfirewall-$profile.log" -LogMaxSizeKilobytes 16384 -LogBlocked True
}

Get-NetFirewallProfile | Select-Object Name, Enabled, DefaultInboundAction, DefaultOutboundAction |
    Format-Table -AutoSize | Out-String | Write-Output

Write-Output 'All three firewall profiles enabled with default-deny inbound'
