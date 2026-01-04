# Phase 1 & 2 Implementation Plan

## Phase 1 – Control Plane MVP (Weeks 3–4)

### Objectives
- Deliver a functional control-plane API with tenant/node/job CRUD and authentication wired end-to-end.
- Stand up the background worker executing provisioning/compliance jobs through mocked adapters.
- Provide a UI slice surfacing tenants, nodes, and job status using OIDC-authenticated requests.

### Workstreams & Milestones
1. **API Hardening & Feature Completion**
   - Finalize registration handler (bootstrap token validation, tenant creation, duplicate suppression) ✅
   - Implement tenant/node listing, creation, and pagination filters.
   - Add job submission endpoint with payload validation, tenant scoping, and RBAC guardrails.
   - Flesh out error handling middleware and structured logging fields.
   - **Deliverable**: `/api/v1/register`, `/api/v1/tenants`, `/api/v1/nodes`, `/api/v1/jobs` stable with unit tests and docs.

2. **Auth & RBAC**
   - Integrate OIDC ID token verification path with static token fallback (completed in Step 2).
   - Persist users/roles on login, assign default role, fetch role mappings.
   - Expose `/api/v1/me` endpoint returning principal profile and roles.
   - **Deliverable**: Auth middleware + tests, role mapping fixtures, user table migrations verified.

3. **Background Jobs**
   - Ensure worker manager supports both memory and Asynq backends (complete) and add job retry policies.
   - Implement provisioning/compliance job handlers using new mock engines with telemetry logging.
   - Record metrics (enqueued, execution, duration) – completed in Step 5.
   - **Deliverable**: Provision/apply and compliance/scan jobs process payloads, update status, log outcomes.

4. **Database & Migrations**
   - Expand schema for jobs, job events, users, roles, audit logs.
   - Provide migration-runner instructions in `docs/development-environment.md`.
   - **Deliverable**: `go test ./controlplane/internal/storage` passing with Postgres Testcontainers.

5. **UI Integration**
   - Use typed API client (completed Step 4) to fetch tenants/nodes/jobs.
   - Add job table view with status chips and tenant filter.
   - Provide login screen handling OIDC redirect (stub) and static token input for dev.
   - **Deliverable**: React routes for `/tenants`, `/nodes`, `/jobs`; manual smoke test verifying job creation.

6. **Observability**
   - Maintain Zap logging; add request ID propagation.
   - Confirm `/metrics` exposed via docker-compose; add README section for Prometheus dashboards.
   - **Deliverable**: Prometheus scrape config validated; Grafana dashboard stub committed under `infra/`.

### Success Criteria
- End-to-end smoke test: mock agent registers, provisioning job queued and executed through mock engine, results visible in UI.
- 80% unit test coverage for API handlers, storage, middleware.
- Documentation: Update `README.md` with Phase 1 features and migration steps.

---

## Phase 2 – Operational Excellence (Weeks 5–6)

### Objectives
- Harden the platform for multi-tenant, compliance-sensitive environments.
- Introduce telemetry pipelines, auditing, and external provider integrations.

### Workstreams & Milestones
1. **Provisioning Providers**
   - Implement provider adapter interface with mock + AWS baseline. ✅ Engine now delegates to adapters (`internal/provisioning/adapter.go`), including HTTP, mock, and AWS variants with metadata enrichment from `DetectProvider`.
   - Add regression tests for metadata merging and AWS region detection (`internal/provisioning/engine_test.go`, `internal/provisioning/detect_test.go`). ✅
   - Support template storage in Postgres, versioning, and rollout hooks.
   - Enrich provisioning job payloads with audit metadata and emit signed artifacts.

2. **Compliance Engine Enhancements**
   - Add policy definition CRUD, rule-set management, and evaluation results storage.
   - Integrate compliance engine with reporting UI (charts, status summaries).
   - Implement webhook/notification when critical severity failures detected.

3. **Audit Logging & Evidence**
   - Persist audit events for user actions, job executions, role changes.
   - Expose `/api/v1/audit` endpoint with filtering/export.
   - Hook provisioning/compliance handlers to append evidence pointers.

4. **Telemetry Pipeline**
   - Stand up Loki/Tempo or alternative for log/trace ingestion in docker-compose.
   - Instrument agent and control plane with OpenTelemetry spans.
   - Document telemetry retention policies and exporter configuration.

5. **Security & Compliance Controls**
   - Enforce TLS certificate rotation workflow (wizard updates).
   - Integrate HashiCorp Vault dev server for secret distribution; publish bootstrap scripts.
   - Complete SOC2/ISO control mapping table in `docs/compliance-matrix.md`.

6. **Deployment & Infra Automation**
   - Author Terraform/Helm starter for deploying control plane services.
   - Add GitHub Actions environment promotion workflow.
   - Document blue/green rollout process and backup/restore steps.

7. **Process & Feedback Cadence**
   - Establish weekly sprint review template, backlog grooming checklist, and decision log (`docs/adr/` directory).
   - Sync roadmap with Linear/Jira, align stakeholders on deliverables.

### Success Criteria
- External AWS provisioning demo completes using test credentials (non-production).
- Compliance dashboards render evaluation history with export capability.
- Audit log proves end-to-end traceability for provisioning + compliance events.
- Telemetry stack captures logs/metrics/traces with documented retention & access controls.
- Infra automation can deploy a staging control plane with single command.

---

## Dependencies & Risks
- **OIDC Provider Availability**: need dev IdP or static tokens until ready.
- **Provider credentials**: secure storage for AWS/Azure keys to unblock Phase 2 provisioning.
- **Telemetry stack resources**: ensure local dev machines can run Loki/Grafana without performance issues.
- **Compliance mapping**: requires stakeholder review to finalize control coverage.

## Tracking & Cadence
- Maintain sprint goals in Linear/Jira synced weekly.
- Use decision log entries for architecture deviations (add `docs/adr/ADR-0001.md` as template).
- Schedule security review at end of Phase 1 and before Phase 2 completion.
