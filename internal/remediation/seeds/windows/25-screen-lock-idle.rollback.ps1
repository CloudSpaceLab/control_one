# Rollback — remove the per-machine screen-lock policy overrides.
$ErrorActionPreference = 'Stop'

$policyKey = 'HKLM:\SOFTWARE\Policies\Microsoft\Windows\Control Panel\Desktop'
if (Test-Path $policyKey) {
    foreach ($name in @('ScreenSaveActive','ScreenSaverIsSecure','ScreenSaveTimeOut','SCRNSAVE.EXE')) {
        Remove-ItemProperty -Path $policyKey -Name $name -ErrorAction SilentlyContinue
    }
}

$lsaKey = 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System'
if (Test-Path $lsaKey) {
    Remove-ItemProperty -Path $lsaKey -Name InactivityTimeoutSecs -ErrorAction SilentlyContinue
}

& gpupdate.exe /force | Out-Null
Write-Warning 'Screen-lock policy overrides removed by rollback.'
