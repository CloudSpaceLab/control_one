# Rollback — re-enable SMBv1. ONLY use for compatibility with ancient appliances.
# Leaves a warning in the event log so the regression is visible.
$ErrorActionPreference = 'Stop'

Set-SmbServerConfiguration -EnableSMB1Protocol $true -Force

$feature = Get-WindowsOptionalFeature -Online -FeatureName SMB1Protocol -ErrorAction SilentlyContinue
if ($feature -and $feature.State -ne 'Enabled') {
    Enable-WindowsOptionalFeature -Online -FeatureName SMB1Protocol -NoRestart | Out-Null
}

$regPath = 'HKLM:\SYSTEM\CurrentControlSet\Services\mrxsmb10'
if (Test-Path $regPath) {
    Set-ItemProperty -Path $regPath -Name Start -Value 2
}

Write-Warning 'SMBv1 RE-ENABLED by rollback. This lowers security; remediate ASAP.'
