# Rollback — reset to Microsoft default password policy.
$ErrorActionPreference = 'Stop'

$tmp = Join-Path $env:TEMP ('controlone-pw-rb-' + [guid]::NewGuid().ToString('N') + '.inf')
$cfg = Join-Path $env:TEMP ('controlone-pw-rb-' + [guid]::NewGuid().ToString('N') + '.sdb')

try {
    & secedit.exe /export /cfg $tmp /quiet | Out-Null

    $content = Get-Content -Path $tmp -Raw
    $content = $content -replace '(?m)^MinimumPasswordLength\s*=.*$', 'MinimumPasswordLength = 0'
    $content = $content -replace '(?m)^PasswordComplexity\s*=.*$',   'PasswordComplexity = 0'
    $content = $content -replace '(?m)^MaximumPasswordAge\s*=.*$',   'MaximumPasswordAge = 42'
    $content = $content -replace '(?m)^MinimumPasswordAge\s*=.*$',   'MinimumPasswordAge = 0'
    $content = $content -replace '(?m)^PasswordHistorySize\s*=.*$',  'PasswordHistorySize = 0'
    Set-Content -Path $tmp -Value $content -Encoding Unicode

    & secedit.exe /configure /db $cfg /cfg $tmp /quiet | Out-Null
    & gpupdate.exe /force | Out-Null
    Write-Warning 'Password policy reverted to Windows defaults (weak).'
}
finally {
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    Remove-Item $cfg -Force -ErrorAction SilentlyContinue
}
