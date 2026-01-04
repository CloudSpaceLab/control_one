# Development Environment Guide

This guide walks through the local setup required to develop and validate the Control One platform.

## Prerequisites
- **Operating system**: Linux, macOS, or Windows with WSL2
- **Go**: 1.24 or newer (matches `go.mod`)
- **Node.js**: 20.x (for the React UI)
- **Docker & Docker Compose**: for Postgres, Redis/Asynq, Prometheus, Grafana
- **Make** (or GNU make equivalent) for convenience targets

Optional tooling:
- VS Code with Remote Containers or Dev Containers extension
- `golangci-lint` if running additional linting locally
- `direnv` for automatic environment variable loading

## Repository Layout
```
cmd/                 # service entrypoints (control plane, node agent)
controlplane/        # API, worker, storage, auth, config
ui/                  # React + TypeScript single-page app
docs/                # architecture, threat model, diagrams, phase summaries
infra/               # infrastructure as code templates
 .github/workflows/  # CI definitions
```

## Quick Start
1. **Clone and enter the repository**
   ```bash
   git clone git@github.com:CloudSpaceLab/control_one.git
   cd control_one
   ```
2. **Copy the dev config**
   ```bash
   cp controlplane/config/controlplane.example.yaml controlplane/config/controlplane.dev.yaml
   ```
3. **Launch the docker-compose stack** (Redis, Postgres, control plane API, Prometheus, Grafana)
   ```bash
   make docker-up
   ```
   Services exposed:
   - Control Plane API: https://localhost:8443
   - Postgres: localhost:5432 (`controlone`/`controlone`)
   - Redis: localhost:6379
   - Prometheus: http://localhost:9090
   - Grafana: http://localhost:3000 (admin/admin)

4. **Run migrations & start services locally**
   ```bash
   CONTROL_ONE_CONFIG=controlplane/config/controlplane.dev.yaml make go-run
   ```
5. **Start Asynq worker (if not using docker)**
   ```bash
   CONTROL_ONE_CONFIG=controlplane/config/controlplane.dev.yaml go run ./controlplane/cmd/controlplane --worker-only
   ```
6. **Start the UI dev server**
   ```bash
   npm install --prefix ui
   npm run dev --prefix ui
   ```
   The UI proxies API traffic to `https://localhost:8443` (see `ui/vite.config.ts`).

7. **Tear down services when done**
   ```bash
   make docker-down
   ```

## Dev Container
A baseline dev container is available under `.devcontainer/`. Open the repo in VS Code and choose "Reopen in Container" to get Go, Node, and common tooling preinstalled.

## Useful Commands
- `make go-test`: run backend unit tests
- `go test ./controlplane/internal/storage -run TestJobLifecycleWithPostgres`: run integration tests with Testcontainers
- `npm test --prefix ui`: execute UI tests
- `go fmt ./... && go vet ./...`: formatting and static analysis

## Troubleshooting
- **Certificate warnings**: The dev API uses self-signed TLS; configure tooling to trust the generated cert or disable verification for local testing only.
- **Redis/Asynq**: Ensure `worker.backend` is set to `asynq` in `controlplane/config/controlplane.dev.yaml` and Redis is reachable at the configured address.
- **Ports already in use**: Adjust service ports in `docker-compose.dev.yml` or stop conflicting processes.

## Next Steps
- Review `docs/phase0-foundations.md` for Phase 0 status.
- Track future improvements and ADRs under `docs/` as the platform evolves.
