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
docs/                # current bank go-live issue log plus API/agent contract notes
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

## Local frontend environment files

The current workstation has ignored `ui/.env.local` (normal local mode) and
`ui/.env.sso-lab.local` (explicit disposable SSO demo mode). These are real local
files, not aliases for `deploy/.env.example`. The latter remains a separate
deployment template; no production deployment `.env` or credentials were created.

Run the normal UI from `ui` with `npm run dev`. Its API proxy targets
`http://127.0.0.1:8443`, and organizational SSO is disabled because no approved
provider registration has been supplied. Configure the three required
`VITE_OIDC_AUTH_URL`, `VITE_OIDC_TOKEN_URL` and `VITE_OIDC_CLIENT_ID` values in
`ui/.env.local` only when an approved real provider is available. The file also
documents callback, scope and optional generic-provider audience. Backend issuer,
client/audience and role mappings remain separate in the active backend YAML;
the current local backend uses `tmp/local/controlplane.yaml`. A frontend file
does not configure or restart the backend or resolve the open production SSO
token contract.

Run the isolated frontend with `npm run dev:sso-lab` from `ui`. This uses mode
`sso-lab`, loopback port 4177, the saved protocol-provider endpoints on 18447 and
the separate backend on 18448. Equivalent verified command:

```powershell
node node_modules/vite/bin/vite.js --mode sso-lab --host 127.0.0.1 --port 4177 --strictPort
```

The mode selects `ui/.env.sso-lab.local`, which overrides the common local file.
Previously inherited process variables still take precedence: start from a fresh
terminal if old `VITE_OIDC_*` or `CONTROL_ONE_*` values are set. Restart Vite after
editing environment files. `vite.config.ts` explicitly loads `CONTROL_ONE_*`
values for its server-side proxy configuration; those names are not exposed as
browser environment variables. `VITE_*` settings are public browser configuration
and must not contain passwords, bearer tokens or client secrets. Both local files
are ignored by Git and must be recreated deliberately on another workstation.

The lab frontend can show the SSO option while the provider/backend is stopped or
the fixture is inactive. File configuration does not activate accounts or prove
provider availability. See the [SSO guide](control-one-organizational-sso-guide-2026-11-05.md)
for provider runtime status, earlier demonstration and open production acceptance.

## Dev Container setup
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
- Review `docs/bank-sales-go-live-issue-log.md` before adding platform or connector work.
- Keep future docs narrow and code-backed so stale planning material does not become a competing source of truth.


## Shared environment templates

Copy `ui/.env.example` to `ui/.env.local` for normal development. Copy
`ui/.env.sso-lab.example` to `ui/.env.sso-lab.local` for the isolated lab mode.
The templates contain public configuration only. The working files remain
ignored and are not changed by this commit. They do not start a backend/provider
or provide production identity credentials. See the organizational SSO guide
for the declared support and outstanding production qualification.

Explicit repository-owner exception, 5 November 2026: the owner requested and
confirmed committing the existing real `tmp/local/credentials.json` after being
informed that its contents persist in Git history. This exact file is included
in the local commit; it is not a sanitized example and must not be represented
as production onboarding configuration. Other local runtime artifacts remain
excluded. No remote push is authorized by this commit request.
