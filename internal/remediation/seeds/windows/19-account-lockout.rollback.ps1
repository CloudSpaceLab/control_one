# Rollback — reset lockout policy to Windows defaults (no lockout).
$ErrorActionPreference = 'Stop'

& net.exe accounts /lockoutthreshold:0 | Out-Null
# When threshold is 0, Windows forces duration/window to 0 too; explicit sets
# for documentation.
& net.exe accounts /lockoutduration:30 | Out-Null
& net.exe accounts /lockoutwindow:30 | Out-Null

Write-Warning 'Account lockout threshold set to 0 (no lockout) by rollback.'
