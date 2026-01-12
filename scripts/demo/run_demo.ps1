param(
  [switch]$NoUI,
  [int]$UiPort = 4173
)

$ErrorActionPreference = "Stop"

function Require-Command {
  param([string]$Name)
  if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
    throw "Required command '$Name' is not installed or not in PATH."
  }
}

function Invoke-Compose {
  param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Args)
  if (Get-Command docker -ErrorAction SilentlyContinue) {
    try {
      docker compose version *> $null
      & docker compose @Args
      return
    } catch {
      # fall through to docker-compose if subcommand unavailable
    }
  }
  if (Get-Command docker-compose -ErrorAction SilentlyContinue) {
    & docker-compose @Args
    return
  }
  throw "docker compose or docker-compose is required."
}

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = (Resolve-Path "$ScriptDir/../..").Path
$ConfigPath = Join-Path $RepoRoot "controlplane/config/controlplane.dev.yaml"
$ExampleConfig = Join-Path $RepoRoot "controlplane/config/controlplane.example.yaml"
$BuildDir = Join-Path $RepoRoot "build"
$UiLog = Join-Path $BuildDir "demo-ui.log"
$UiPidFile = Join-Path $BuildDir "demo-ui.pid"

Require-Command docker
if (-not $NoUI) { Require-Command npm }

if (-not (Test-Path $BuildDir)) { New-Item -ItemType Directory -Path $BuildDir | Out-Null }

if (-not (Test-Path $ConfigPath)) {
  Write-Host "Creating dev config from example..."
  Copy-Item $ExampleConfig $ConfigPath -Force
}

Write-Host "Starting Control One backend stack via docker compose..."
Invoke-Compose -Args @("-f", (Join-Path $RepoRoot "docker-compose.dev.yml"), "up", "-d", "--remove-orphans")

Write-Host "Backend started. Services:" 
Write-Host "  API:        https://localhost:8443"
Write-Host "  Postgres:   localhost:5432 (controlone/controlone)"
Write-Host "  Redis:      localhost:6379"
Write-Host "  Prometheus: http://localhost:9090"
Write-Host "  Grafana:    http://localhost:3000 (admin/admin)"

if (-not $NoUI) {
  Write-Host "Starting UI dev server on http://localhost:$UiPort ..."
  $UiDir = Join-Path $RepoRoot "ui"
  Push-Location $UiDir
  npm install | Out-Host
  $proc = Start-Process -FilePath "npm" -ArgumentList @("run","dev","--","--host","0.0.0.0","--port",$UiPort,"--clearScreen","false") -WorkingDirectory $UiDir -RedirectStandardOutput $UiLog -RedirectStandardError $UiLog -PassThru -WindowStyle Hidden
  Pop-Location
  Set-Content -Path $UiPidFile -Value $proc.Id
  Write-Host "UI dev server PID: $($proc.Id) (logs: $UiLog)"
}

Write-Host ""
Write-Host "Demo is starting. It may take ~30-60s for the API and UI to finish warming up."
Write-Host "When done, stop with:"
Write-Host "  docker compose -f docker-compose.dev.yml down"
if (-not $NoUI) {
  Write-Host "  if UI is running: Stop-Process -Id (Get-Content $UiPidFile)"
}
