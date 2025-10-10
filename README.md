# Control One Node Agent Scaffold

## Overview
The Control One Node Agent is a Go-based service deployed on managed hosts across VMware, LibVirt, AWS, Azure, and OpenStack environments. It handles secure bootstrap with the Control Plane, continuous compliance evaluations, telemetry streaming, and optional remediation actions.

## Architecture
- **Agent Core** (`cmd/nodeagent/main.go`): wires configuration, registration, scheduling, telemetry, mesh, provisioning, access, and secrets workflows.
- **Configuration** (`internal/config/`): loads YAML config, applies defaults, and ensures runtime directories for policy, mesh, and state artifacts.
- **API Client** (`internal/api/`): mTLS-enabled REST client for Control One Core communication.
- **Registration** (`internal/registration/`): bootstraps the node, persists state, and prevents duplicate registrations.
- **Mesh Manager** (`internal/mesh/`): prepares zero-touch WireGuard mesh state, polls the coordinator, and rotates keys.
- **Provisioning Engine** (`internal/provisioning/`): applies node templates, baseline hardening, and optional auto-remediation.
- **Policy Management** (`internal/policy/`): fetches signed policy bundles and caches them locally.
- **Compliance Engine** (`internal/compliance/`): evaluates rulesets/certifications and feeds telemetry summaries.
- **Scheduler** (`internal/scheduler/`): cron-based job runner used for policy sync, provisioning, compliance evaluation, telemetry, access sync, secrets sync, and heartbeat.
- **Scanner** (`internal/scanner/`): executes compliance checks with timeout/concurrency controls.
- **Telemetry** (`internal/telemetry/`): handles metrics, compliance reports, and heartbeats.
  - Log ingestion helpers live in `internal/telemetry/logs/` with pluggable collectors/formatters driven by `telemetry_prefs.log_sources`.
- **Access Manager** (`internal/access/`): syncs user/groups from AD/API/local providers for fine-grained control.
- **Secrets Store** (`internal/secrets/`): manages secure retrieval/refresh of secrets across groups.
- **Utilities** (`internal/util/`): gathers system metadata, host metrics, and other helpers.

## Build & Tooling
- **Go version**: `go 1.23.0` per `go.mod`.
- **GoReleaser**: `.goreleaser.yaml` produces multi-OS archives, checksums, and optional Docker images using `build/docker/Dockerfile`.
- **CI**: `.github/workflows/ci.yaml` runs gofmt, vet, tests, and cross-platform builds; tags trigger GoReleaser.

### Local Commands
```
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/nodeagent
```

## Installation Scripts
- **Linux/Unix**: `scripts/install.sh` deploys binary, config, and systemd unit (`build/nodeagent.service`).
- **Windows**: `scripts/install.ps1` installs the agent under `Program Files` and registers a Windows service.

## Configuration
Sample file: `configs/example-config.yaml`
```
api_url: https://control-plane.example.com/api
bootstrap_token: CHANGE_ME
node_name: example-node
...

mesh:
  enabled: true
  coordinator_url: https://mesh-control.example.com
  auth_token: CHANGE_ME
  namespace: default
  private_cidr: 10.1.0.0/16

provisioning:
  template: ubuntu-web-tier
  baselines:
    - cis-ubuntu-24
    - control-one-hardening
  auto_remediation: true

compliance:
  region: eu
  rule_sets:
    - cis-level1
    - iso27001-section8
  certifications:
    - gdpr
    - soc2

access:
  provider: active_directory
  sync_interval: 30m
  default_role: operator
  api_endpoint: https://directory.example.com

secrets:
  backend: vault
  endpoint: https://vault.example.com
  groups:
    - production
    - shared-services

telemetry_prefs:
  collect_logs: true
  log_namespaces:
    - system
    - application
    - security
  log_sources:
    - program: nginx
      type: file
      paths:
        - /var/log/nginx/access.log
        - /var/log/nginx/error.log
      formatter: default
      severity_map:
        notice: info
        crit: critical
      labels:
        stack: web
    - program: windows-iis
      type: eventlog
      event_channels:
        - "Microsoft-Windows-IIS-Logging/Operational"
      severity_map:
        Information: info
        Warning: warn
        Error: error
  metrics_interval: 30s
  activity_interval: 2m
```

Tune bootstrap token, TLS material locations, mesh coordinator URL, sync intervals, and node naming before deployment. The agent will automatically ensure required directories for state, policies, mesh, and secrets.

## Next Implementation Steps
- Integrate `internal/mesh` manager with the control-plane coordinator (e.g., Headscale via `wgctrl`).
- Implement provisioning engine support for cloud-init, Ansible-lite, and Terraform provider hooks.
- Connect compliance engine to policy metadata and remediation playbooks.
- Extend access manager with Active Directory/SCIM connectors and role-mapping rules.
- Back the secrets store with Vault or cloud secret managers and add envelope encryption.
- Add command channel listener (REST/WebSocket) for orchestration directives.
- Extend telemetry batching with compression/backpressure and include provisioning/access/secrets status streams.
- Write integration tests with a mock control plane and environment simulators.
