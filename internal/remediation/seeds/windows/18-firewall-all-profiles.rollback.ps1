# Rollback — disables all three firewall profiles. Dangerous; only for
# debugging or transitioning to a third-party firewall product.
$ErrorActionPreference = 'Stop'

foreach ($profile in @('Domain','Private','Public')) {
    Set-NetFirewallProfile -Profile $profile -Enabled False
}

Write-Warning 'All Windows Firewall profiles DISABLED by rollback.'
