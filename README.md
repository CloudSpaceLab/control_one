# Control One Platform

## Overview
Control One delivers a unified control plane, background worker service, and node agent that together provision infrastructure, enforce compliance, and surface telemetry across hybrid environments (VMware, LibVirt, AWS, Azure). The repository also hosts a React-based operator UI and infrastructure tooling to stand up a complete development stack.

### Core Components
- **Control Plane API** (`controlplane/cmd/controlplane`): Go service that exposes authenticated REST endpoints for tenants, nodes, jobs, and registration. Integrates Postgres for persistence, Asynq for background work, and optional observability endpoints.
- **Worker Manager** (`controlplane/internal/worker`): Dispatches provisioning and compliance jobs via in-memory queues or Asynq/Redis, coordinating with the job store and external integrations.
- **Node Agent** (`cmd/nodeagent`): Runs on managed hosts, orchestrating bootstrap, scheduling, provisioning, compliance, telemetry, access, and secrets workflows.
- **Web UI** (`ui/`): React + TypeScript SPA providing authenticated dashboards, routing, and placeholders for tenants, nodes, and login surfaces.
- **Docs & Diagrams** (`docs/`): Architecture brief, threat model, and C4/sequence diagrams underpinning Phase 0 planning.

## Architecture
- **Agent Core** (`cmd/nodeagent/main.go`): wires configuration, registration, scheduling, telemetry, mesh, provisioning, access, and secrets workflows.
- **Configuration** (`internal/config/`): loads YAML config, applies defaults, and ensures runtime directories for policy, mesh, and state artifacts.
- **API Client** (`internal/api/`): mTLS-enabled REST client for Control One Core communication.
- **Registration** (`internal/registration/`): bootstraps the node, persists state, and prevents duplicate registrations.
- **Mesh Manager** (`internal/mesh/`): prepares zero-touch WireGuard mesh state, polls the coordinator, and rotates keys.
- **Provisioning Engine** (`internal/provisioning/`): applies node templates, baseline hardening, and optional auto-remediation.
  - Templates are authored in the control plane and referenced by name via `provisioning.template`; metadata keys (e.g. `cluster`, `resource_group`) are forwarded untouched in the apply payload, enabling provider-specific workflows.
- **Provisioning Adapter Architecture**
  - `Engine` now delegates to pluggable adapters (`internal/provisioning/adapter.go`) so each provider can customize how templates/baselines are applied.
  - The default HTTP adapter invokes the control plane’s `/api/v1/provisioning/*` endpoints; a mock adapter provides deterministic local behavior, while the AWS adapter enriches metadata (e.g., region detection from `AWS_REGION`/`AWS_DEFAULT_REGION`).
  - Auto-detected provider metadata (from `DetectProvider`) is merged with caller-supplied metadata before invoking adapters, ensuring downstream workflows always receive consistent hints.
  - Tests covering metadata merge semantics, AWS region injection, and baseline delegation live in `internal/provisioning/engine_test.go` and `internal/provisioning/detect_test.go`. Run `go test ./internal/provisioning` when iterating on adapters.
- **Provisioning Templates API**
  - Control plane stores templates and their versions via the new `/api/v1/templates` endpoints. Templates persist provider metadata, labels, and a promoted version pointer backed by the migration `0004_provisioning_templates`.
  - Use the API to create templates and upload versions, then promote a version before triggering provisioning jobs. Example:
    ```bash
    # create template shell
    curl -H "Authorization: Bearer <token>" -H "Content-Type: application/json" \
      -d '{"name":"web-tier","provider":"aws","labels":{"env":"dev"}}' \
      https://localhost:8443/api/v1/templates

    # upload version body
    curl -H "Authorization: Bearer <token>" -H "Content-Type: application/json" \
      -d '{"body":"#cloud-config ...","checksum":"sha256:..."}' \
      https://localhost:8443/api/v1/templates/<template_id>/versions

    # promote version 1
    curl -X POST -H "Authorization: Bearer <token>" \
      https://localhost:8443/api/v1/templates/<template_id>/versions/1/promote
    ```
  - Listing and detail responses include pagination metadata plus promoted version details so operators can audit rollouts.
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
- **Go version**: `go 1.24.0` per `go.mod`.
- **GoReleaser**: `.goreleaser.yaml` produces multi-OS archives, checksums, and optional Docker images using `build/docker/Dockerfile`.
- **CI**: `.github/workflows/ci.yaml` runs Go formatting, vetting, unit tests, storage integration tests, UI lint/tests, and binary builds across Linux/macOS/Windows runners.
- **Deployment Wizard**: `scripts/wizard/setup_control_one.sh` generates self-contained control plane and node agent bundles (config, binaries, optional TLS assets). The CI workflow publishes a ready-made wizard artifact named `control-one-wizard-<commit>` for Ubuntu runners.

## Local Development

### Prerequisites
- Go 1.24+
- Node.js 20+
- Docker & Docker Compose

### Quick Start (API + Worker + Postgres + Observability)
1. Copy the sample config and adjust as needed:
   ```bash
   cp controlplane/config/controlplane.example.yaml controlplane/config/controlplane.dev.yaml
   ```
2. Launch the local stack (Postgres, control plane API, Prometheus, Grafana):
   ```bash
   make docker-up
   ```
   Services expose:
   - Control Plane API: https://localhost:8443 (self-signed during dev)
   - Postgres: localhost:5432 (`controlone`/`controlone`)
   - Prometheus: http://localhost:9090
   - Grafana: http://localhost:3000 (admin/admin)
3. Apply database migrations and start the API/worker locally (if not using Docker):
   ```bash
   CONTROL_ONE_CONFIG=controlplane/config/controlplane.dev.yaml make go-run
   ```
4. Tear down when finished:
   ```bash
   make docker-down
   ```

### Background Jobs
The worker manager defaults to an in-memory queue. Enable Asynq/Redis by configuring `worker.backend: asynq` and `worker.asynq.*` fields in `controlplane/config/controlplane.dev.yaml`. When Asynq is enabled, ensure a Redis instance is reachable at the configured address.

Newer builds expose worker resiliency knobs:

- `worker.max_attempts` – default `1`. Controls how many times the manager will retry a task before surfacing a failure.
- `worker.retry_backoff` – default `5s`. Governs the base delay between attempts (the manager multiplies this delay by the attempt number for simple linear backoff). Individual tasks can override both fields.

Metrics for queue depth, backend availability, enqueue success/failure, and execution duration are published under the API’s Prometheus endpoint (`/metrics` by default). Look for the `controlone_worker_*` series to confirm worker health in dashboards.

### Web UI
1. Install dependencies: `npm install --prefix ui`
2. Run the dev server: `npm run dev --prefix ui`
3. The SPA expects the API to be reachable via the configured proxy (see `ui/vite.config.ts`).

### Testing
- Backend: `make go-test`
- UI: `npm test --prefix ui`
- Storage integration (Postgres via Testcontainers): `go test -v ./controlplane/internal/storage -run TestJobLifecycleWithPostgres`

### Wizard Usage
Run the guided setup to emit binaries, configuration, and summary docs:

```
bash scripts/wizard/setup_control_one.sh \
  --output ./dist/wizard \
  --config-name controlplane.yaml
```

Flags such as `--non-interactive` and `--no-certs` allow automated pipelines. Generated assets include `bin/controlplane`, `bin/nodeagent`, TLS placeholders (`certs/`), and a summary README for operators.

### Local Commands
```
go fmt ./...
go vet ./...
go test ./...
go build ./cmd/nodeagent
```

## Production Authentication
The control plane enforces role-based access control (RBAC) for all HTTPS endpoints. Recommended production setup:

1. **OIDC Provider** – Configure `auth.oidc` in `controlplane/config/controlplane.example.yaml` (issuer URL, client ID, optional audiences). Tokens are validated using `github.com/coreos/go-oidc/v3` with claim-based role resolution.
2. **RBAC Defaults** – Map IdP groups to Control One roles via `auth.rbac.role_mappings`. Users without matches inherit `auth.rbac.default_role` (viewer).
3. **Database Migration** – Apply `controlplane/internal/migrate/sql/0003_auth.up.sql` to seed `users`, `roles`, `user_roles`, and `audit_logs` tables.
4. **Bootstrapping** – After running migrations, insert at least one admin assignment (e.g. `INSERT INTO roles` and `user_roles`) or supply an OIDC group mapped to `admin`.
5. **CI Artifacts** – The wizard bundle published in CI contains binaries, configs, and TLS placeholders. Update its `auth` section before promotion.

Reference `controlplane/internal/server/server_test.go` for RBAC integration tests covering viewer vs. admin paths.

## Installation Scripts
- **Linux/Unix**: `scripts/install.sh` deploys binary, config, and systemd unit (`build/nodeagent.service`).
- **Windows**: `scripts/install.ps1` installs the agent under `Program Files` and registers a Windows service.

## Configuration
Sample node-agent file: `configs/example-config.yaml`
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
