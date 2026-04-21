# CIS Windows 2.3.1.3 — Ensure built-in Guest account is disabled
# SOC2 CC6.1 — Remove anonymous local access paths
$ErrorActionPreference = 'Stop'

# The built-in Guest account has the well-known SID S-1-5-*-501, so look it up
# by SID instead of name (localized builds rename it).
$guest = Get-LocalUser | Where-Object { $_.SID.Value.EndsWith('-501') }
if (-not $guest) {
    Write-Output 'No built-in Guest account found on this host'
    return
}

if ($guest.Enabled) {
    Disable-LocalUser -SID $guest.SID
    Write-Output "Guest account '$($guest.Name)' disabled"
} else {
    Write-Output "Guest account '$($guest.Name)' already disabled"
}
