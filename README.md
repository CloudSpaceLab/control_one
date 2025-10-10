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
  - Templates are authored in the control plane and referenced by name via `provisioning.template`; metadata keys (e.g. `cluster`, `resource_group`) are forwarded untouched in the apply payload, enabling provider-specific workflows.
- **Policy Management** (`internal/policy/`): fetches signed policy bundles and caches them locally.
- **Compliance Engine** (`internal/compliance/`): evaluates rulesets/certifications and feeds telemetry summaries.
- **Scheduler** (`internal/scheduler/`): cron-based job runner used for policy sync, provisioning, compliance evaluation, telemetry, access sync, secrets sync, and heartbeat.
- **Scanner** (`internal/scanner/`): executes compliance checks with timeout/concurrency controls.
  - Log ingestion helpers live in `internal/telemetry/logs/` with pluggable collectors/formatters driven by `telemetry_prefs.log_sources`.
  - Built-in presets cover nginx, Apache, MySQL, PostgreSQL, Redis, Kafka, IIS, and more; custom entries only need to provide overrides such as log paths or additional labels.
  - The generic formatter (`formatter_generic.go`) consumes declarative `format_rules` (regex capture groups + templates) so complex log formats can be normalized without bespoke Go code.
- **Access Manager** (`internal/access/`): syncs user/groups from AD/API/local providers for fine-grained control.
- **Secrets Store** (`internal/secrets/`): manages secure retrieval/refresh of secrets across groups.
- **Utilities** (`internal/util/`): gathers system metadata, host metrics, and
### Provisioning Templates
- **Overview**
  - **`provisioning.template`** selects a control-plane workflow; templates should be created per provider (VMware, Libvirt, AWS, Azure) using matching names.
  - **Metadata** supplied at runtime is passed verbatim; choose keys expected by the template (e.g. `datacenter`, `vpc_id`).
  - **Baselines** listed in `provisioning.baselines` are replayed after template application and can include CIS or custom hardening bundles.

- **Recommended keys**
  - **VMware**: `cluster`, `datacenter`, `datastore`, `folder`, `network`.
  - **Libvirt**: `pool`, `network`, `image`, `cpu`, `memory`.
  - **AWS**: `region`, `vpc_id`, `subnet_id`, `iam_profile`, `security_groups`.
  - **Azure**: `subscription_id`, `resource_group`, `vnet`, `subnet`, `availability_zone`.

- **Testing**
  - **Dry run** using the control plane’s preview mode (if available) before enabling `auto_remediation`.
  - **Verify baselines** by confirming completion status in `/api/v1/provisioning/baselines` responses and reviewing remediation notes.

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

  collect_logs: true
  log_namespaces:
    - system
    - application
    - security
  # log_sources may be omitted to use baked-in presets. Provide entries only when overrides are required.
  log_sources:
    - program: nginx
      paths:
        - /custom/path/nginx/access.log
        - /custom/path/nginx/error.log
      labels:
        stack: web-tier
    - program: kafka
      formatter: generic
      format_rules:
        - regex: "^(?P<ts>\\d{4}-\\d{2}-\\d{2}\\s+\\d{2}:\\d{2}:\\d{2},\\d{3})\\s+(?P<level>\\w+)\\s+\\[(?P<thread>[^]]+)\\]\\s+(?P<class>[^ ]+)\\s+-\\s+(?P<message>.*)$"
          timestamp_layout: "2006-01-02 15:04:05,000"
          severity_field: level
          severity_map:
            WARN: warn
            ERROR: error
            INFO: info
            FATAL: critical
          fields:
            thread: "${thread}"
            class: "${class}"
