# Control Plane Service

This directory hosts the Control One control plane services responsible for tenancy, inventory, workflows, and policy management.

## Getting started (dev stack)

Prerequisites:

- Docker / Docker Compose
- Go 1.23+

```bash
cd controlplane
docker compose -f docker-compose.dev.yaml up --build
```

The compose stack launches:

1. **Postgres 16** seeded with user/db `controlone`.
2. **controlplane** container leveraging `Dockerfile.dev`, running `go run ./cmd/controlplane` with live code volume mount.

The service reads configuration from `config/controlplane.local.yaml`. Copy the example file and modify as needed:

```bash
cp controlplane/config/controlplane.example.yaml controlplane/config/controlplane.local.yaml
```

Key configuration sections:

- `http`: bind address and timeouts.
- `tls`: listener certificates and optional mTLS.
- `observability`: enable Prometheus metrics and path.
- `database`: Postgres DSN, connection pool settings, and `apply_migrations` toggle.

On startup the binary will:

1. Load configuration and initialize structured logging.@controlplane/cmd/controlplane/main.go#20-33
2. Establish a Postgres connection, run health checks, and optionally apply embedded migrations.@controlplane/cmd/controlplane/main.go#34-58
3. Launch the HTTP server with `/healthz` and `/metrics` endpoints (if enabled).@controlplane/internal/server/server.go#26-63

## Roadmap

### Delivered

- Control plane bootstrap with TLS, metrics, migrations, and worker manager.
- Authentication middleware supporting mTLS and bearer tokens.
- `/api/v1/ping` authenticated health endpoint.
- `/api/v1/nodes` collection (GET/POST) backed by Postgres via `storage.Store`.

### In flight / Upcoming

1. Expand nodes API with filtering (by tenant), pagination, and detail endpoints.
2. Implement tenant CRUD + policy/template management APIs.
3. Wire provisioning/compliance job scheduling into worker manager and surface job status endpoints.
4. Add OIDC/OAuth user identities, RBAC enforcement, and session replay metadata ingestion.
5. Build integration test suite covering TLS auth flows, nodes CRUD, and migration bootstraps.

## Worker & Job Architecture Alignment

The embedded worker manager (@controlplane/internal/worker/manager.go#1-117) will orchestrate provisioning and compliance flows via structured jobs:

1. **Queues & Payloads**: standardize job payload schemas (JSON) for provisioning plans, compliance scans, and remediation actions. Persist job metadata in Postgres tables (`jobs`, `job_events`) with status transitions.
2. **Job Lifecycles**: provisioning jobs enqueue per-tenant plans, triggering cloud adapter tasks and recording hook events; compliance jobs run on schedules with retry policies and evidence capture.
3. **API Surface**: expose `/api/v1/jobs` endpoints for submission, status polling, and cancellation; integrate with nodes/tenants to filter jobs by scope.
4. **Worker Execution**: leverage the worker manager concurrency controls to process queues, emitting structured logs/metrics and invoking downstream provisioning engines.
5. **Observability & Alerting**: instrument Prometheus metrics (queue depth, job latency, failure counts) and integrate with hooks to notify operators on SLA breaches.

Initial implementation will focus on provisioning baseline jobs, followed by compliance scans and remediation workflows.
