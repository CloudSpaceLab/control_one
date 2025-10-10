Param(
    [string]$InstallDir = "C:\Program Files\ControlOne\NodeAgent",
    [string]$ConfigPath = "C:\ProgramData\ControlOne\nodeagent\config.yaml"
)

$ErrorActionPreference = 'Stop'

if (-not (Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

if (-not (Test-Path (Split-Path $ConfigPath))) {
    New-Item -ItemType Directory -Path (Split-Path $ConfigPath) | Out-Null
}

$binarySource = Join-Path $PSScriptRoot 'nodeagent.exe'
if (-not (Test-Path $binarySource)) {
    Write-Error "nodeagent.exe not found alongside script."
}

Copy-Item $binarySource -Destination $InstallDir -Force

$configSource = Join-Path $PSScriptRoot '..\configs\example-config.yaml'
if (Test-Path $configSource) {
    Copy-Item $configSource -Destination $ConfigPath -Force
}

$serviceName = 'ControlOneNodeAgent'
$existingService = Get-Service -Name $serviceName -ErrorAction SilentlyContinue

if ($existingService) {
    Stop-Service -Name $serviceName -Force
    sc.exe delete $serviceName | Out-Null
}

New-Service -Name $serviceName -BinaryPathName "`"$InstallDir\nodeagent.exe`" -config `"$ConfigPath`"" -Description "Control One Node Agent" -DisplayName "Control One Node Agent" -StartupType Automatic
Start-Service -Name $serviceName

Write-Output "Control One Node Agent installed as Windows service."
