# Development Environment Guide

<!-- identity-audit-status-20261106 -->
6 November identity follow-up: verified-first sign-in now checks the backend profile before publishing/storing a candidate token; initial account refusal and established-session expiry have separate guidance. Reviewed legacy/provisioning migration and protected global identity audit readback were tested and demonstrated, with the temporary replay stopped afterward. The intended-provider token contract/current-authority/execution revalidation acceptance remains open; the multi-organization expansion stays withdrawn. [Evidence, operator walkthrough and limits](patch-posture-organization-policy-2026-11-03.md#global-identity-evidence-and-reviewed-migration---6-november-2026).


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


## Main local app connected to existing SSO lab - 5 November 2026

Explicit user authorization selected the existing isolated provider for local testing on main UI4173. Updated ignored ui/.env.local and tmp/local/controlplane.yaml with lab endpoints/client/issuer/audience. Original login layout remains intact: More sign-in options → Single Sign-On → Continue with SSO. No always-visible organization card was added. Windows agent config/enrollment/service were not edited.

The existing optional compose.patch-posture-candidate.yml now selects the previously verified controlplane-oidc-20261105 binary and adds a loopback-only issuer forwarder in the main API network namespace. The forwarder allows the Docker backend to discover and fetch keys from the provider whose issuer is http://127.0.0.1:18447, matching the browser issuer exactly. It forwards only to the named existing provider container on18447 and publishes no additional host port. It is local lab runtime support, not production deployment configuration. Main API database was backed up before candidate switch; schema is188. Existing local.ps1 start was used without modification. One overlapping Compose invocation produced a container-removal-in-progress error; the subsequent launcher run completed with healthy API/forwarder and refreshed the existing TLS proxy.

The disposable provider now explicitly registers both exact callbacks for4177 and4173. Its browser-origin handling allows only those two loopback origins. Each authorization code retains its original redirect, and token exchange must match that redirect. Provider remains a protocol fixture, not Microsoft Entra. It must be running before the main SSO test; existing launcher does not own/start this independently created provider container.

Live main-port callback initially failed during development StrictMode replay because the pending PKCE transaction was consumed twice. AuthCallback now retains the in-flight exchange within the same mounted callback and ignores stale effect subscriptions. This does not reuse exchanges across page reloads or make authorization codes reusable. Regression checks passed for one successful exchange/signIn under StrictMode and failed exchange without retry; existing7 pending-session tests and login guidance test also passed (10 total). TypeScript passed. A fresh browser run then completed provider password login → exact4173 callback → automatic token exchange → main Control Room. Profile showed sso-reviewer@phase4.invalid with Viewer. No bearer token was manually supplied. Main database user6660e15e-620e-4b29-a96e-01a653d522af has one authentication-owned Viewer grant. Current global Viewer behavior exposes Local Development in this database; this is not a demonstration of tenant-scoped employee access or approval authority. Existing main admin email/password login, profile and logout separately passed. Browser ended signed out with main SSO option visible.

Evidence: ignored tmp/local/patch-posture-demo/main-sso-20261105/signed-in-main.png and login-ready-main.png. Updated existing credential inventory entry with main local environment/UUID; no new password was generated. Inventory remains tracked under the user's earlier explicit exception; its current update is uncommitted. Configuration backups: ui.env.before, controlplane.before.yaml, candidate.before.yml, provider.before.mjs and main-before-sso.dump under the same ignored main-sso-20261105 directory.

Repeat: ensure Docker Desktop/provider are running; use existing scripts/local.ps1 start; open http://127.0.0.1:4173/console/login → More sign-in options → Continue with SSO. Sign in at the lab provider with the existing fixture credential from the local inventory. The lab token expires after120seconds. Existing4177 environment retains its original callback registration.

Configuration rollback: use scripts/local.ps1 stop, restore ui.env.before to ui/.env.local, controlplane.before.yaml to tmp/local/controlplane.yaml, candidate.before.yml to compose.patch-posture-candidate.yml and provider.before.mjs to its original ignored provider path. Remove the lab forwarder service container using the matching Compose service before replacing its definition, restart the provider and run the existing launcher. Do not automatically restore the database backup: that would discard subsequent local records. Retain schema migrations and evidence unless an explicit database recovery is intended. Revert the callback source fix only by a separately reviewed source change if requested. Main SSO connection is local testing, not production activation; full provider/token contract and Phase4 acceptance stay open. Nothing from this follow-up was committed or pushed.


API token contract update (6 November 2026, deployment verification pending):
OIDC-enabled backends require one auth.oidc.audience value distinct from the
browser client ID. The configured provider must issue the declared RS256,
at+jwt API access-token profile with controlone.access scope and client_id,
jti, iat, exp and sub metadata. Public frontend examples request the additional
API scope. Scope names/claims are provider-registration requirements, not proof
of authorization to real tenants. Browser ID-token fallback is removed. An old
ID-token-only provider or unchanged verifier binary is incompatible; do not
silently enable this setup against production Entra. Existing default callback,
login layout and local.ps1 entry point are retained.


## Verified API access-token profile - 6 November 2026

The earlier in-progress entry is superseded by this bounded verification. The browser now selects only access_token and requires Bearer token_type; it never falls back to id_token. Backend verifies an RS256 signed token using issuer discovery/JWKS with the explicitly configured API resource audience, then requires token type at+jwt/application/at+jwt, the registered client_id, nonempty subject/jti, iat/exp, controlone.access scope and applicable nbf. The single API audience must differ from the browser client ID. The API resource in this lab is controlone-api. This is a declared RFC9068-style JWT access-token contract, not universal access-token or Entra support. Opaque tokens and providers issuing other formats are rejected; no introspection adapter is implemented. Reference: https://www.rfc-editor.org/rfc/rfc9068.html . ID tokens remain provider identity artifacts and are not API credentials.

Passing automated checks: auth package, including16 signed verifier scenarios (valid, signature/issuer/audience/expiry/subject errors, ID token substitution including an ID token with API audience, missing token type/scope/client/jti/iat/exp, future iat/nbf); config tests including distinct API resource requirement;12 UI tests across token exchange/callback/login, plus TypeScript. Existing HTTP loopback provider is test-only; managed production HTTPS is not newly qualified.

Built binary tmp/local/patch-posture-demo/controlplane-api-access-20261106, SHA256 c2f0e1285fbd7f83019fb1c8abb217cdd62f30719982ed8ea3997a2b62743160. Main existing local.ps1 launcher selected it through the untracked candidate override. Provider now issues distinct signed browser-client ID tokens and controlone-api access tokens. Both exact4173/4177 callbacks remain registered; both frontend example/lab settings request controlone.access. Main backend runs it with existing DB/enrollment. The older4177 verifier was stopped and replaced by controlone-oidc-api-access-20261106 (provider network namespace, backend18448) with matching audience; its separate browser flow was not rerun in this follow-up. Old binaries remain retained; do not restart an old verifier with the new frontend/token configuration.

Main live UI: More sign-in options → Continue with SSO → existing lab provider password sign-in → automatic main callback/code/PKCE exchange → Viewer in Control One. One first attempt surfaced Vite's /console base-URL hint; a fresh repeated normal browser flow reached the console automatically and profile showed sso-reviewer@phase4.invalid / Viewer. The hint is retained as an open routing hard-refresh qualification, not claimed fixed. The lab credential expires after120seconds; no extended lifetime was introduced.

Running API evidence from the same issuance: access_token /api/v1/me returned200 with Viewer; id_token /api/v1/me returned401; access_token /api/v1/users returned403. Sanitized results: ignored tmp/local/patch-posture-demo/api-access-20261106/live-token-contract-result.json. Screenshot: live-access-token-viewer.png in that directory. Private token artifacts were kept only in ignored local test storage and were not added to the tracked credential inventory. Historical tracked credentials exception remains unchanged.

Acceptance: bounded API-contract implementation checked. Parent .3p.3d stays open for the intended organizational provider, registration/scopes/claim formats and production activation; .3p.3b remains open for broader authorization transaction/provider failure qualification. Nonce/ID-token binding must be assessed against the chosen flow; this contract does not use the ID token for application authentication. Revocation/introspection, signing-key rotation/outage and queued execution authority remain unchecked. No Phase4 completion or Entra readiness claim. No commit/push performed.

Rollback/configuration: pre-change main config, candidate override and provider source copies are under ignored tmp/local/patch-posture-demo/api-access-20261106. Reverting token format requires reverting the coordinated frontend/provider/backend configuration; simply starting an older binary undermines the new contract. Existing Windows agent service/config/enrollment were not edited. Use the retained previous binary only through a reviewed local rollback; do not revert DB evidence automatically.


6 November 2026 OIDC recovery update: bounded discovery retry and signing-key outage/rotation checks passed, including race tests and visible main4173 SSO recovery without API restart. Evidence and limits: [organization identity record](patch-posture-organization-policy-2026-11-03.md#oidc-discovery-and-signing-key-recovery---6-november-2026-bounded-verification-passed). Intended-provider qualification and early revocation stay open; Phase4 remains open.


6 November 2026 SSO landing update: local Vite root refresh repaired and demonstrated on main4173;20 focused tests, TypeScript, production asset build and dev/preview HTTP checks passed. See [identity evidence](patch-posture-organization-policy-2026-11-03.md#sso-landing-route-consistency---6-november-2026). Local landing item checked; production ingress query/refresh qualification added unchecked. Real-provider/revocation and Phase4 remain open.


6 November 2026 Nginx routing update: repository base redirects now preserve queries; both Nginx variants passed16 isolated routing checks and visible built-app login/invalid-callback demonstration. [Evidence and fixture limits](patch-posture-organization-policy-2026-11-03.md#nginx-ingress-routing---6-november-2026). Implementation child checked; actual intended ingress deployment and Phase4 remain open.


6 November 2026 Users access-denied update: P4-08b.3d.6a.3u checked after15 focused tests, TypeScript/lint/build and live existing Operator403/Admin200 verification on main4173. Counters/cached directory/edit controls are hidden for denied access. [Steps and evidence](patch-posture-organization-policy-2026-11-03.md#users-access-denied-view---6-november-2026). Profile Account .3v and parent identity/Phase4 remain open.


6 November 2026 scope correction: the owner withdrew the new multi-organization SSO expansion. Its connection/isolation/onboarding checklist additions, configurable Entra token-profile adapter, examples and design were removed. These are not Phase4 completion gates. Original single-provider API-token verification, recovery, routing, Account and callback fixes remain. Original acceptance requirements and deferred host/provider checks retain their existing status.


Execution-authority follow-up (6 November): include the new executionauthority client/tests, storage boundary guards and binding migration 0193 when reviewing the source change. Keep local candidate executables, backups, runtime logs and real authority service credentials out of a routine source commit. The main app is still unconfigured for current-provider authority; actual organizational qualification and the permitted live installation workflow remain open. See the canonical acceptance tracker and current-provider section of the organizational SSO guide.


Browser exchange follow-up (6 November): review ui/src/lib/oidc.ts, oidc.test.ts, oidc-network.test.ts, AuthCallback.tsx and AuthCallback.test.tsx with the updated acceptance/evidence guides. Timeout and size guards are product code; local screenshots/test reports/build output are evidence artifacts. The live network-outage test was declined and remains unperformed; do not describe this change as complete Phase 4 or actual-provider qualification.
