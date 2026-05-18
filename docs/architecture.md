# Control One Platform Architecture

## Vision Overview
Control One delivers a unified control plane and node agent for regulated industries (banks, telcos) to provision infrastructure, enforce compliance, orchestrate access, and capture operational telemetry across hybrid environments (VMware, Azure, AWS, Libvirt). The current repository contains the node agent scaffold; this document extends the blueprint to the upcoming control-plane services and UI.

## High-Level Components
1. **Node Agent**  
   - Deployed per managed host, written in Go (`cmd/nodeagent`).  
   - Handles bootstrap/registration, policy sync, provisioning, compliance scanning, telemetry streaming, access/secrets synchronization, hooks execution, and mesh coordination.
2. **Control Plane API** *(new)*  
   - Go service responsible for tenancy, inventory, job orchestration, policy/template management, and audit logging.  
   - Provides REST/gRPC endpoints secured via mTLS for agents and OIDC for human clients.
3. **Workflow/Job Orchestrator** *(new)*  
   - Temporal/Asynq-style background workers executing provisioning, compliance, access-sync, and remediation tasks.  
   - Persists job state, retries, SLAs, and integrates with hook/script engine.
4. **Web UI / Dashboard** *(new)*  
   - React + TypeScript SPA backed by Control Plane API.  
   - Offers provisioning wizard, cluster builder, compliance dashboards, access governance, telemetry analytics, and session replay viewer.
5. **Telemetry & Analytics Pipeline** *(new)*  
   - Ingests metrics/log compliance results from agents, storing in time-series (Prometheus/ClickHouse) and log aggregation (Loki/Elastic).  
   - Supports alerting and rule-based triggers.
6. **Session Recording Service ("Scribery")** *(new)*  
   - Agent-deployed recorder that streams session artifacts to the control plane for tamper-evident storage and replay UI.
7. **Infrastructure Layer**  
   - Terraform modules + Helm charts to deploy the control plane, supporting HA/DR, secure networking, and secrets management (Vault/KMS).

## C4 Context Summary
- **Actors**: Platform operators, compliance officers, engineers, external directory services (AD/LDAP), VPN providers (NetBird/OpenVPN/Fortinet), cloud/hypervisor providers (VMware, Azure, AWS, Libvirt), BI tools.  
- **Dependencies**: External PKI, identity providers (OIDC), monitoring/alerting systems, artifact storage, and compliance evidence repositories.

## Container Architecture (Planned)
```
┌─────────────────────────────┐      ┌─────────────────────────────┐
│        Web UI (React)       │◀────▶│     Control Plane API       │
└──────────────┬──────────────┘      ├───────────────┬─────────────┤
               │                     │ AuthZ Engine  │ Job Queue   │
               │                     └──────┬────────┴─────┬───────┘
┌──────────────▼──────────────┐            │              │
│ Temporal/Background Workers │◀───────────┘              │
└──────────────┬──────────────┘                           │
               │                                          │
┌──────────────▼──────────────┐      ┌────────────────────▼──────────────────┐
│  Provisioning Adapters      │◀────▶│  Cloud/Hypervisor APIs (VMware/AWS/…) │
└──────────────┬──────────────┘      └───────────────────────────────────────┘
               │
┌──────────────▼──────────────┐
│ Telemetry / Log Pipeline    │◀────────┐
└──────────────┬──────────────┘         │
               │                        │
┌──────────────▼──────────────┐         │
│ Session Recorder (Scribery) │─────────┘
└──────────────┬──────────────┘
               │
┌──────────────▼──────────────┐
│      Node Agent (Go)        │
└─────────────────────────────┘
```

## Data Flows
1. **Node Registration**: Agent → Control Plane (mTLS) → store node metadata → respond with configuration.  
2. **Provisioning**: Operator triggers template via UI → API enqueues job → workers invoke provider adapters → status events returned to agent via hooks.  
3. **Compliance**: Scheduler on agent runs scanners → results sent to API → stored + visualized; non-compliance triggers remediation scripts/hook events.  
4. **Access Governance**: Control plane syncs directories, assigns roles, pushes entitlements to agents; wizard UI manages approvals and expiry dates.  
5. **Telemetry**: Agent streams logs/metrics → telemetry pipeline → alerts/analytics → feedback into automation rules.  
6. **Session Recording**: Scribery service records user activity on nodes → uploads artifacts → stored in immutable bucket → UI supports playback.

## Security Considerations
- End-to-end mTLS between agents and control plane; certificate lifecycle owned by wizard/PKI integration.  
- OIDC + RBAC for human operators; least-privilege roles aligning with compliance personas.  
- Secrets centrally managed via Vault/KMS with short-lived tokens.  
- Audit logging for all configuration changes, provisioning actions, access approvals, and session replay usage.  
- Encryption at rest for databases, telemetry storage, and session archives.
- Enforcement provider boundaries for firewall, webserver, Fail2Ban-style, and future WAF/proxy integrations are captured in `docs/enforcement-integrations.md`.
- IP behavior/API contracts and agent event/job contracts are captured in `docs/ip-behavior-api-contracts.md` and `docs/agent-event-contracts.md`.

## Tenancy & RBAC Model
- **Isolation**: Every resource (nodes, jobs, policies, telemetry artifacts) is owned by a tenant. Tenant context is enforced via database foreign keys and middleware lookups.  
- **Agent Trust**: Agents authenticate with client certificates bound to a tenant/cluster. Certificates are rotated via the bootstrap wizard and revoked through the audit trail.  
- **Human Roles**: RBAC roles (`viewer`, `operator`, `admin`, `compliance`) map to IdP groups. The control plane persists role grants in `users`, `roles`, `user_roles`, enabling historic audit and delegated administration.  
- **Least Privilege Defaults**: Unknown users fall back to `viewer`; write paths (provisioning, access approvals, policy edits) demand `operator` or `admin`. Compliance exports require `compliance` or `admin`.  
- **Cross-Tenant Safety**: API handlers require tenant IDs from headers or tokens; background jobs validate tenant ownership before invoking external providers.
- **Schema Source**: RBAC tables and audit logs are seeded via `controlplane/internal/migrate/sql/0003_auth.up.sql`; keep migrations in sync with middleware expectations.

## Data Residency & Compliance Targets
- **Regional Deployment**: Control plane clusters are deployed per-region; telemetry buckets (S3/Blob) adhere to residency policies.  
- **Boundary Enforcement**: Multi-region data egress is disabled by default. Cross-region analytics rely on anonymized aggregates.  
- **Control Mapping**: SOC 2 CC2/CC6, ISO 27001 A.9/A.12, and GDPR Articles 32/33 mapped to automation hooks (policy enforcement, incident logging, consent tracking).  
- **Retention Strategy**: Default retention—telemetry 90 days, session recordings 365 days, audit logs 7 years—configurable per tenant.  
- **Regulatory Evidence**: Jobs emit signed artifacts stored in immutable buckets referenced in the audit log for certification evidence.

## Diagram References
- `docs/diagrams/context.mmd`: system context diagram (C4 Level 1) showing actors and external dependencies.  
- `docs/diagrams/container-control-plane.mmd`: container diagram (C4 Level 2) detailing API, worker, telemetry, and UI components.  
- `docs/diagrams/sequence-provisioning.mmd`: sequence flow for provisioning template execution (UI ➜ API ➜ job queue ➜ provider adapter).  
- `docs/diagrams/sequence-compliance.mmd`: sequence flow for scheduled compliance scan and remediation feedback loop.  

These diagrams act as living artifacts; updates to architecture should include synchronized diagram revisions.

## Compliance Targets & Controls (Initial)
- **SOC 2 / ISO 27001**: Change management, access control, logging, incident response, vulnerability management.  
- **PCI / SWIFT / GDPR** (banks/telcos): Data residency, encryption, segmentation, retention policies.  
- Evidence artifacts captured through automated hooks, job logs, telemetry snapshots, and approval workflows.

## Next Steps
- Elaborate component-level designs (API service, worker topology, telemetry pipeline).  
- Produce sequence diagrams for provisioning wizard, cluster deployment, and compliance remediation.  
- Align with upcoming control-plane implementation plan (Phase 0 sprint backlog).
