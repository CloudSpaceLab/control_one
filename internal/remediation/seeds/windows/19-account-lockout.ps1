# CIS Windows 1.2.x — Account lockout threshold, duration, reset counter
# SOC2 CC6.1 — Defend against brute force
$ErrorActionPreference = 'Stop'

# 5 invalid attempts locks for 15 min, counter resets after 15 min.
$proc = Start-Process -FilePath 'net.exe' -ArgumentList 'accounts','/lockoutthreshold:5' -Wait -PassThru -WindowStyle Hidden
if ($proc.ExitCode -ne 0) { throw "net accounts lockoutthreshold failed with $($proc.ExitCode)" }

$proc = Start-Process -FilePath 'net.exe' -ArgumentList 'accounts','/lockoutduration:15' -Wait -PassThru -WindowStyle Hidden
if ($proc.ExitCode -ne 0) { throw "net accounts lockoutduration failed with $($proc.ExitCode)" }

$proc = Start-Process -FilePath 'net.exe' -ArgumentList 'accounts','/lockoutwindow:15' -Wait -PassThru -WindowStyle Hidden
if ($proc.ExitCode -ne 0) { throw "net accounts lockoutwindow failed with $($proc.ExitCode)" }

& net.exe accounts | Write-Output
Write-Output 'Account lockout set to threshold=5 duration=15m window=15m'
