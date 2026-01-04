# Control One Threat Model (Phase 0)

## Scope
- Control Plane API and background workers (Go services)
- Node agent interactions over mTLS
- Web UI (React SPA) federated via OIDC
- Postgres database, job queue, telemetry and session storage buckets

## Assets
| Asset | Description | Sensitivity |
|-------|-------------|-------------|
| Tenant metadata | Tenants, nodes, policies, jobs | High |
| Secrets & credentials | Agent bootstrap tokens, provider API keys, OIDC secrets | Critical |
| Audit & compliance evidence | Job logs, approvals, session recordings | High |
| Telemetry streams | Metrics, logs, remediation outcomes | Medium |
| Build artifacts | Control plane / agent binaries, wizard bundle | Medium |

## Actors & Entry Points
- **Platform operators** (OIDC-authenticated) using UI + API tokens
- **Agents** using client certificates issued by the bootstrap wizard
- **Background jobs** pulling work from Asynq queues
- **External providers** (cloud APIs, directories) accessed via adapters
- **CI/CD pipelines** producing binaries and configurations

## Trust Zones
1. Public internet (UI users, agent bootstrap)
2. Control plane perimeter (Ingress, TLS termination, API/worker)
3. Internal data plane (Postgres, Redis/Asynq, telemetry buckets, Vault)
4. External integrations (cloud providers, IdP, monitoring)

## Threats & Mitigations
| ID | Threat | Mitigation |
|----|--------|------------|
| T1 | Credential theft from repo/CI | Store secrets in Vault; enforce commit signing; scrub artifacts via `.gitignore`; CI OIDC workloads |
| T2 | Agent impersonation | Mutual TLS with per-tenant certificates; revocation list; short-lived bootstrap tokens |
| T3 | Privilege escalation via UI | RBAC with least-privilege defaults; audit log on role changes; map IdP groups |
| T4 | Injection into provisioning workflows | Validate job payloads; sanitize metadata; use allow-list templates |
| T5 | Data residency breach | Regional data stores; tenancy-scoped buckets; policy enforcement in API; retention controls |
| T6 | Telemetry tampering | Signed job artifacts; immutable storage (WORM buckets); hash chaining for session recordings |
| T7 | Lateral movement in infra | Network segmentation; zero trust service mesh; OS hardening via agent baselines |
| T8 | Supply chain compromise | Go module checksum verification; pinned versions; reproducible builds; SBOM generation |
| T9 | Denial of service on API | Rate limiting per tenant; queue backpressure; worker autoscaling; Prometheus alerts |

## Residual Risks & Actions
- **Temporal/Asynq hardening**: enable TLS and auth once clusters provisioned (Phase 1).
- **Vault integration**: move bootstrap secrets from config to Vault secrets engine.
- **Session replay privacy**: add consent workflows and encryption keys per tenant.
- **Telemetry cost control**: define retention/partitioning to avoid billing-driven outages.

## References
- `docs/architecture.md`
- `docs/diagrams/context.mmd`, `docs/diagrams/container-control-plane.mmd`
- `controlplane/internal/auth` for RBAC middleware implementation
- `controlplane/internal/migrate/sql/0003_auth.up.sql` for RBAC schema
