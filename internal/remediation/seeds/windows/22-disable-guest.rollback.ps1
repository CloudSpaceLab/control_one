# Rollback — re-enable the Guest account. Strongly discouraged.
$ErrorActionPreference = 'Stop'

$guest = Get-LocalUser | Where-Object { $_.SID.Value.EndsWith('-501') }
if ($guest -and -not $guest.Enabled) {
    Enable-LocalUser -SID $guest.SID
    Write-Warning "Guest account '$($guest.Name)' RE-ENABLED by rollback."
} else {
    Write-Output 'Guest account already enabled or missing; nothing to do'
}
