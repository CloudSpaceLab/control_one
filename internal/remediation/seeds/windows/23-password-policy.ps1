# CIS Windows 1.1.x — Password policy: min length 14, complexity, max age 90d
# SOC2 CC6.1 — Strong credentials + rotation
$ErrorActionPreference = 'Stop'

# Use secedit for complexity (net accounts can't set it); export, patch, import.
$tmp = Join-Path $env:TEMP ('controlone-pw-' + [guid]::NewGuid().ToString('N') + '.inf')
$cfg = Join-Path $env:TEMP ('controlone-pw-' + [guid]::NewGuid().ToString('N') + '.sdb')

try {
    & secedit.exe /export /cfg $tmp /quiet
    if ($LASTEXITCODE -ne 0) { throw "secedit export failed ($LASTEXITCODE)" }

    $content = Get-Content -Path $tmp -Raw
    $content = $content -replace '(?m)^MinimumPasswordLength\s*=.*$', 'MinimumPasswordLength = 14'
    $content = $content -replace '(?m)^PasswordComplexity\s*=.*$',   'PasswordComplexity = 1'
    $content = $content -replace '(?m)^MaximumPasswordAge\s*=.*$',   'MaximumPasswordAge = 90'
    $content = $content -replace '(?m)^MinimumPasswordAge\s*=.*$',   'MinimumPasswordAge = 1'
    $content = $content -replace '(?m)^PasswordHistorySize\s*=.*$',  'PasswordHistorySize = 24'
    Set-Content -Path $tmp -Value $content -Encoding Unicode

    & secedit.exe /configure /db $cfg /cfg $tmp /quiet
    if ($LASTEXITCODE -ne 0) { throw "secedit configure failed ($LASTEXITCODE)" }

    # Force policy refresh so the new settings apply without waiting.
    & gpupdate.exe /force | Out-Null
    Write-Output 'Password policy: minlen=14, complexity=on, maxage=90, minage=1, history=24'
}
finally {
    Remove-Item $tmp -Force -ErrorAction SilentlyContinue
    Remove-Item $cfg -Force -ErrorAction SilentlyContinue
}
