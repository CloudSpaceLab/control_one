# Control One organizational SSO setup and demonstrated behavior

<!-- identity-audit-status-20261106 -->
6 November identity follow-up: verified-first sign-in now checks the backend profile before publishing/storing a candidate token; initial account refusal and established-session expiry have separate guidance. Reviewed legacy/provisioning migration and protected global identity audit readback were tested and demonstrated, with the temporary replay stopped afterward. The intended-provider token contract/current-authority/execution revalidation acceptance remains open; the multi-organization expansion stays withdrawn. [Evidence, operator walkthrough and limits](patch-posture-organization-policy-2026-11-03.md#global-identity-evidence-and-reviewed-migration---6-november-2026).


5 November 2026. Current work remains P4-08b identity/permissions, in the agreed Phase 4 order. This guide describes the actual repository implementation, an isolated live demonstration, and the work required before a real organization can qualify Microsoft Entra. It is not production sign-off.

## What the administrator configures once

SSO uses an organizational application registration, not a list of individual employee tokens in backend configuration. The identity administrator owns the registration, assignments, authentication policies and signing keys. The Control One deployment administrator configures the trusted issuer, application identifiers, callback address and role mapping. Changes to domains, registrations or policy require controlled updates; “once” means per deployment/registration rather than per employee sign-in.

For example, an organization might use `https://control-one.example.org/console/` for its console. The exact callback is `https://control-one.example.org/console/auth/callback`. The provider registration, frontend configuration and deployed route must agree exactly on scheme, host, port and path. The `/console` part matters.

### Microsoft Entra administrator procedure

These are setup/review steps for an organization-owned tenant. No real tenant or approved registration was supplied or changed during this demonstration.

1. In Microsoft Entra admin center, select the intended directory. Navigate **Entra ID → App registrations → New registration**. Name the registration, for example **Control One Console**, and select accounts in this organizational directory only for a single-organization deployment. Record the Directory (tenant) ID and Application (client) ID; neither is an employee credential.
2. In the registration's **Authentication** configuration, add a **Single-page application** platform and the exact HTTPS callback above. The current browser exchanges the code directly, so SPA redirect/CORS behavior must be correct. Use authorization code with S256 PKCE. A browser is a public client: do not create or embed a client secret in `VITE_*` variables. Do not enable implicit grants merely to obtain an ID token from code flow.
3. Read the registration's **Endpoints** / tenant-specific discovery document. For the v2 single-tenant case, the relevant addresses are:

   | Purpose | Example |
   | --- | --- |
   | Issuer | `https://login.microsoftonline.com/<tenant-id>/v2.0` |
   | Discovery | issuer + `/.well-known/openid-configuration` |
   | Authorization | `https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/authorize` |
   | Token | `https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token` |
   | Published keys | use `jwks_uri` from discovery |

   Use the actual tenant values and discovered metadata. The current verifier is configured for a fixed issuer; this guide does not qualify `common`, `organizations` or arbitrary multi-tenant trust.
4. Under **App roles**, define the approved role values, or configure approved group claims. For the current direct-claim path, values such as `viewer` and `operator` must match Control One roles. Display names such as “Patch Operators” are not necessarily token values. Under **Enterprise applications → Control One → Users and groups**, assign only approved employees/groups. Review **Assignment required** and organization authentication policies, including MFA/Conditional Access, with the identity administrator. This lab does not qualify those policies or license-dependent group assignment behavior.
5. Keep approval responsibilities distinct. For example, an employee can be a Viewer for visibility, an operations employee can have Operator authority, and a designated security reviewer can have separately approved tenant CISO authority. An SSO assignment, an email suffix, or membership in the Entra directory does not by itself grant access to every Control One tenant. Do not grant global admin to make onboarding pass.
6. Review the token contract described below before enabling production access. The current Control One path uses API access tokens, never the browser ID token. Use the existing API access-token contract and matching registration/scope below; registration alone is not production qualification.

Official references: [authorization code flow and SPA PKCE](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow), [app roles and assignments](https://learn.microsoft.com/en-us/entra/identity-platform/howto-add-app-roles-in-apps), [ID tokens](https://learn.microsoft.com/en-us/entra/identity-platform/id-tokens), [API access tokens](https://learn.microsoft.com/en-us/entra/identity-platform/access-tokens).

### Current frontend deployment settings

These are public application settings, supplied to Vite at startup/build. They contain no employee tokens and no client secret. A compiled deployment needs a rebuild/redeployment when its embedded values change.

```dotenv
VITE_OIDC_AUTH_URL=https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/authorize
VITE_OIDC_TOKEN_URL=https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token
VITE_OIDC_CLIENT_ID=<console-application-client-id>
VITE_OIDC_REDIRECT_URI=https://control-one.example.org/console/auth/callback
VITE_OIDC_SCOPE=openid profile email
```

The first three enable **More sign-in options → Single Sign-On → Continue with SSO**. The corrected default callback is the current origin plus `/console/auth/callback`; explicit production configuration is still useful. `VITE_OIDC_AUDIENCE` is an optional generic-provider parameter in this code. It is not a replacement for requesting an Entra API scope. Do not request a Microsoft Graph token and configure Control One to accept Graph's audience.

### Current backend settings and their limits

The following illustrates the existing **ID-token** verifier configuration; it is useful for understanding the lab and reviewing the code. It is not a production API access-token implementation:

```yaml
auth:
  oidc:
    enabled: true
    issuer_url: https://login.microsoftonline.com/<tenant-id>/v2.0
    client_id: <console-application-client-id>
    audience:
      - <console-application-client-id>
    username_claim: preferred_username
    groups_claim: groups
  rbac:
    default_role: viewer
    role_mappings:
      operator:
        - <approved-operations-group-object-id>
```

`client_id` is checked by go-oidc against token audience. The optional audience list is an additional check, not an override of the client-ID check. Therefore, copying the example `control-one-api` audience alongside a different SPA client ID can break the current ID-token path. The server uses discovery and published signing keys; no employee token or browser client secret belongs here.

The current role resolver adds the configured default (Viewer when omitted), accepts direct `roles` strings, and maps configured groups to roles. It does not implement a separate direct-role allowlist in this resolver. Only the trusted provider should issue approved application role values; review assignments and the default role deliberately. Missing group claims do not prove membership. Group overage retrieval and real Entra group-lifecycle behavior are unqualified.

After first successful authentication, an authorized Control One administrator reviews the user and source grants in **Users & Roles**. Tenant permissions are managed separately through **Patch posture → Review policy & ownership**, with the existing authorization/expiry/review controls. A person receives only the tenant/action scope actually approved there; identity authentication is not tenant authorization.

## What each employee does

1. Open the organization's Control One console and choose **More sign-in options → Continue with SSO**.
2. The browser generates a fresh state and PKCE verifier, stores the pending transaction in that tab's session storage, computes its S256 challenge and redirects to the configured provider.
3. The employee authenticates on the provider's page using the organization's normal sign-in and MFA policy. They do not enter their organizational password into Control One, backend YAML, or chat.
4. The provider redirects to `/console/auth/callback` with a short-lived authorization **code** and state. The code is not the API token.
5. Control One checks the pending transaction/state, then the browser automatically POSTs the code, client ID, exact callback and original PKCE verifier to the token endpoint. No copying or pasting is required. The pending transaction is consumed before exchange; the new bounded check rejects malformed, future-dated or at-least-ten-minute-old transactions.
6. Currently, the browser prefers `id_token` and otherwise selects `access_token`. `AuthCallback` passes it to `signIn`; `AuthProvider` sends it as a bearer on API requests and stores it in local storage. This is the actual implementation, including the production token-contract limitation below. There is no implemented refresh-token renewal in this flow.
7. The backend validates through its discovered OIDC verifier: signature against published keys, configured issuer, client audience and expiry, plus its additional audience configuration. It rejects invalid credentials. It binds the account to a hash of verified authority and immutable subject; email/name are metadata, not permission to merge another identity. Ambiguous legacy identities require explicit migration; disabled/provider-mismatched records fail closed.
8. Authentication-owned global grants are reconciled atomically and audited while independent administrator sources remain separate. Tenant access and action permissions are then enforced independently. A new Viewer may legitimately see **No tenants available** until an administrator approves a tenant grant.

SSO logout here clears the local application credential; it does not implement provider-wide logout or immediate upstream token revocation. A signed token can remain valid until expiry. No live revocation/introspection or execution-time provider qualification is claimed.

## Visible isolated demonstration actually performed

The demonstration used a disposable Node protocol fixture, not Microsoft Entra or LDAP. It implemented discovery/JWKS, an actual lab-password form, exact registered callback/client checks, one-use codes, S256 verification and RS256-signed ID tokens. It is a narrow standards-compatible code-flow fixture, not a certified or complete OIDC provider. HTTP was limited to an isolated loopback lab; production requires managed HTTPS trust.

| Component | Isolated scope |
| --- | --- |
| UI | `http://127.0.0.1:4177/console/login` |
| Provider | `http://127.0.0.1:18447` |
| Backend | `http://127.0.0.1:18448` |
| Successful replay DB | `controlone_oidc_demo_verified_20261105` |
| User | `sso-reviewer@phase4.invalid`, ID `8ffe9c76-712b-4f07-840c-de94b2e5cf73` |
| Authority and role | issuer-bound OIDC identity; Viewer; authentication-owned source |
| Tenant access | none; no tenant permission added |

1. Opened the isolated login in the visible in-app browser, expanded More sign-in options, and clicked Continue with SSO. Saved `oidc-sso-button-20261105.png`.
2. Showed the distinct provider page and its explicit lab label, then entered the disposable inventory account through its normal login form. Saved the blank provider page as `oidc-provider-login-20261105.png`; no password or token appears in screenshots.
3. Provider receipts recorded accepted code/S256 request, successful authentication/code redirect and PKCE-verified token issuance. Browser handled the callback and exchange automatically. Backend discovery/JWKS receipt and successful `/api/v1/me`/`tenants` responses established the backend path.
4. The first attempt exposed concurrent new-user creation: `/me` returned 200 but `/tenants` returned 401 due to `users_email_unique_idx`. Fixed same-authority/subject transaction serialization with a transaction advisory lock before insertion. Twelve concurrent first-sign-in database requests now return one user and one authentication event; cross-authority email conflicts still fail rather than merge accounts.
5. Restored the pre-creation backup into a **new** verified database, deployed the tested candidate, and repeated the normal browser flow. Both first profile/tenant calls returned 200. Profile menu showed the new email and Viewer; the main view showed **No tenants available / No tenant access is available for this account**. Saved `oidc-signed-in-viewer-20261105.png`.
6. Opened `/console/users` as that Viewer. Backend returned 403 for users and roles; the visible page displayed Forbidden. Saved `oidc-viewer-admin-denied-20261105.png`. The generic error/zero-counter presentation remains tracked under .3u.
7. After the two-minute credential lifetime, reloaded the same browser. Backend rejected the expired token and the app returned to login with **Session has expired. Please sign in again.** Saved `oidc-expired-session-20261105.png`. This is expiry, not provider revocation. Opened `/evidence` visibly to inspect the sanitized provider receipts.

The original failed attempt retains its own separate database `controlone_oidc_demo_20261105`. It is not substituted for successful replay evidence. The provider's token lifetime is two minutes for expiry verification; it is not an organizational lifetime policy.

## Verification, artifacts and open acceptance

Retained artifacts are under ignored `tmp/local/patch-posture-demo/`. Candidate binary `controlplane-oidc-20261105` SHA-256: `F14E3A441A9194F4154DC1F5B40153E591707E0545BBDD389CA85307A33E028B`. Secrets remain in ignored inventory/configuration; do not publish this directory.

Passing verification: seven pending-transaction tests; three existing AuthProvider tests; TypeScript check; isolated PostgreSQL concurrent-first-sign-in, role reconciliation and audit-rollback tests; auth package including six published-key cases (valid, bad signature, wrong issuer, wrong audience, expired, missing subject). The separate optional LDAP disposable test was skipped because its fixture variables were not supplied in this SSO run. It is not an OIDC test failure or new LDAP qualification.

Scoped ESLint passed using the repository's legacy configuration (`ESLINT_USE_FLAT_CONFIG=false`); an initial direct invocation failed to discover that configuration and was corrected. The production UI build passed into the ignored `oidc-ui-build-20261105` output directory. Its public provider settings are supplied separately at deployment; the build check alone is not provider qualification.

Bounded checklist status: checked .3p.3a.1 (isolated code-flow demonstration) and .3p.3c (same-identity first-login race); unchecked parent .3p.3a (intended real provider), .3p.3b (full transaction/nonce/token/error qualification), .3p.3d (production token contract), .3p.3 (migration/revocation/execution authority), and parent P4-08b/Phase 4. The ten-minute transaction guard is implemented/tested but does not close all of .3b.

Cleanup: stopped only `controlone-oidc-backend-verified-20261105` and `controlone-oidc-provider-20261105`; the initial failed-attempt backend was already stopped. Updated the ignored credential inventory with the successful replay user UUID and inactive status. Database/audit evidence remains retained. The expired browser contains no valid session. A repeat demonstration requires starting these isolated containers, setting the fixture inventory active for the run, and starting the UI with the existing Vite command and the three public OIDC settings above replaced by the lab addresses/client. The provider fixture is not a production service or employee directory.

Repeat the bounded lab using the retained containers (do not point these settings at the main app). The isolated UI process on 4177 was left serving its signed-out login; reuse it if still running. Otherwise run the UI command below from `C:\Projects\control_one\ui`. The fixture password is available only in the ignored local inventory and is entered into the provider form, never into a token field. Status in the inventory is bookkeeping; stopping the provider is what disabled this lab sign-in.

```powershell
$docker = 'C:/Users/wilso/AppData/Local/Programs/DockerDesktop/resources/bin/docker.exe'
& $docker start controlone-oidc-provider-20261105
& $docker start controlone-oidc-backend-verified-20261105

# Only if the isolated UI is no longer running:
$env:CONTROL_ONE_API_URL = 'http://127.0.0.1:18448'
$env:VITE_OIDC_AUTH_URL = 'http://127.0.0.1:18447/authorize'
$env:VITE_OIDC_TOKEN_URL = 'http://127.0.0.1:18447/token'
$env:VITE_OIDC_CLIENT_ID = 'control-one-isolated-spa'
node node_modules/vite/bin/vite.js --host 127.0.0.1 --port 4177 --strictPort
```

Open the 4177 login, follow the employee steps, and inspect the provider's `/evidence` page. Repeating on the retained database signs in the existing lab user; it is not another fresh-account creation proof. For cleanup, sign out or observe expiry, stop only the two named SSO containers with `docker stop`, and mark the local inventory inactive again. Do not restart the failed-attempt backend at the same time; it shares the candidate port. Do not import this fixture's issuer or HTTP setup into an organizational deployment.

The parent SSO acceptance stays open. Pending work includes nonce/ID-token binding and safe callback errors/replay behavior, production API token contract, real Entra app/tenant/MFA/Conditional Access and role-change qualification, upstream revocation semantics, key rotation/outage, legacy migration, execution-time provider revalidation, and intended tenant isolation/failure coverage. Existing queued-execution guards are not evidence that every manual/job boundary is certified.

For production, choose and implement a clear contract: normally a SPA requests an access token scoped to the Control One API and the backend validates that API's audience and required claims; alternatively a reviewed server-side authentication/session design keeps an ID token out of API bearer use. Neither alternative is implemented by changing these environment variables alone. Microsoft explicitly distinguishes ID-token authentication from access-token API authorization.

To qualify real Entra, the organization must provide an approved tenant/registration, callback/domain and HTTPS deployment, controlled test identities/assignments, an identity administrator for authentication policy, and acceptance access for allowed/denied, role/tenant, disable/expiry/revocation and failure scenarios. Passwords and tokens must be entered only into the appropriate provider/browser UI or protected deployment secret store, never pasted into chat.

The main port 4173, Windows agent/service/enrollment and earlier isolated runtimes were preserved. No VM, new PowerShell script, commit or push was used. Full Phase 4 and deferred host/production prerequisites remain open.

## Provider sign-in presentation follow-up

The user requested a UI improvement on the lab provider page. Added the reviewable presentation module `scripts/oidc-lab-ui.mjs`, used by the ignored local provider adapter. It renders a centered dark card, clear heading and input labels, consistent full-width fields/button, keyboard focus styles, compact brand/lab header, back navigation and responsive narrow-screen spacing. UTF-8 document metadata and response headers fix the original garbled dash. The generated markup uses system fonts and inline SVG icons without third-party assets or dependencies.

Demonstrated the result visibly through the existing app's Continue with SSO entry point. Saved `oidc-provider-redesign-desktop-20261105.png` and `oidc-provider-redesign-mobile-20261105.png` under ignored `tmp/local/patch-posture-demo/`. Inspected the default desktop and 390×844 preview; restored the default browser viewport afterward. Syntax/render checks passed, including UTF-8 metadata, password autocomplete and the disabled preview CTA. The rendered browser exposed both correctly associated input labels and the inactive preview state. This cosmetic change did not rerun the earlier full authentication demonstration.

Current runtime status supersedes the earlier cleanup paragraph: restarted **only** `controlone-oidc-provider-20261105` to keep this requested UI preview available on loopback 18447. The verified and failed-attempt SSO backends remain stopped. Both fixture identity inventory records remain inactive. The preview button is disabled with “Preview mode · Lab sign-in is paused”; the fixture login handler also rejects inactive credentials. The main app and prior lab runtimes were preserved. Checked only bounded presentation item .3p.3a.2; intended-provider qualification, the production token contract and Phase 4 remain open. No commit/push was made.

## Saved frontend environment configuration

On the user's follow-up request, created actual ignored files `ui/.env.local` and
`ui/.env.sso-lab.local`. Normal mode retains backend 8443 and unconfigured real
SSO; the explicit `sso-lab` mode contains the complete saved lab authorization,
token, client, callback and scope values and targets backend 18448. API base,
live-event mode and agent proxy settings are documented in the files. The
optional audience parameter is empty for this fixture. No individual credential
or client secret was placed in frontend settings. No `deploy/.env` was generated
with invented production credentials.

The proxy previously read only `process.env`, which would ignore file-based
`CONTROL_ONE_API_URL`. `vite.config.ts` now uses Vite `loadEnv` for that server-only
prefix and preserves explicit process overrides. Resolved-config assertions
passed for normal/lab separation, actual file-backed proxy targets, existing
process precedence and exclusion of server-only names from public browser env.
TypeScript and scoped Vite-config lint passed. Both local env files are ignored
by Git. These checks validate file loading; no new production build or end-to-end
provider authentication was performed in this follow-up.

Started UI4177 with `--mode sso-lab` without setting SSO process variables. HTTP
200 and the actual served `src/config/oidc.ts` confirmed the configured public
values. The visible in-app browser showed **More sign-in options → Continue with
SSO**; screenshot `tmp/local/patch-posture-demo/oidc-env-file-ui-20261105.png`.
The main UI4173 remained HTTP200. The isolated UI is left running in lab mode;
fixture credentials remain inactive and neither SSO backend was started here.

See [local file locations and commands](development-environment.md#local-frontend-environment-files).
The Entra URLs/client ID remain intentionally unset in the main file because no
approved registration exists. The real-provider token contract and backend
qualification remain unchecked; saved frontend variables do not complete those
requirements. No commit/push was made.


SSO lab restart at user request, 5 November 2026: restarted the existing provider and verified backend, enabled only the existing disposable Viewer fixture, and started UI4177 using the retained --mode sso-lab environment. Frontend login, backend18448 health and provider18447 discovery returned HTTP200. Visible Continue with SSO redirected to the active provider password form; no user token was pasted and no password login was submitted in this restart check. The lab is intentionally left running for user testing. Fixture credentials inventory reflects active-disposable status. Evidence: ignored tmp/local/patch-posture-demo/oidc-restarted-ready-20261105.png. This supersedes prior paused-preview runtime notes, not production qualification or acceptance closure.


## Main local app connected to existing SSO lab - 5 November 2026

Explicit user authorization selected the existing isolated provider for local testing on main UI4173. Updated ignored ui/.env.local and tmp/local/controlplane.yaml with lab endpoints/client/issuer/audience. Original login layout remains intact: More sign-in options → Single Sign-On → Continue with SSO. No always-visible organization card was added. Windows agent config/enrollment/service were not edited.

The existing optional compose.patch-posture-candidate.yml now selects the previously verified controlplane-oidc-20261105 binary and adds a loopback-only issuer forwarder in the main API network namespace. The forwarder allows the Docker backend to discover and fetch keys from the provider whose issuer is http://127.0.0.1:18447, matching the browser issuer exactly. It forwards only to the named existing provider container on18447 and publishes no additional host port. It is local lab runtime support, not production deployment configuration. Main API database was backed up before candidate switch; schema is188. Existing local.ps1 start was used without modification. One overlapping Compose invocation produced a container-removal-in-progress error; the subsequent launcher run completed with healthy API/forwarder and refreshed the existing TLS proxy.

The disposable provider now explicitly registers both exact callbacks for4177 and4173. Its browser-origin handling allows only those two loopback origins. Each authorization code retains its original redirect, and token exchange must match that redirect. Provider remains a protocol fixture, not Microsoft Entra. It must be running before the main SSO test; existing launcher does not own/start this independently created provider container.

Live main-port callback initially failed during development StrictMode replay because the pending PKCE transaction was consumed twice. AuthCallback now retains the in-flight exchange within the same mounted callback and ignores stale effect subscriptions. This does not reuse exchanges across page reloads or make authorization codes reusable. Regression checks passed for one successful exchange/signIn under StrictMode and failed exchange without retry; existing7 pending-session tests and login guidance test also passed (10 total). TypeScript passed. A fresh browser run then completed provider password login → exact4173 callback → automatic token exchange → main Control Room. Profile showed sso-reviewer@phase4.invalid with Viewer. No bearer token was manually supplied. Main database user6660e15e-620e-4b29-a96e-01a653d522af has one authentication-owned Viewer grant. Current global Viewer behavior exposes Local Development in this database; this is not a demonstration of tenant-scoped employee access or approval authority. Existing main admin email/password login, profile and logout separately passed. Browser ended signed out with main SSO option visible.

Evidence: ignored tmp/local/patch-posture-demo/main-sso-20261105/signed-in-main.png and login-ready-main.png. Updated existing credential inventory entry with main local environment/UUID; no new password was generated. Inventory remains tracked under the user's earlier explicit exception; its current update is uncommitted. Configuration backups: ui.env.before, controlplane.before.yaml, candidate.before.yml, provider.before.mjs and main-before-sso.dump under the same ignored main-sso-20261105 directory.

Repeat: ensure Docker Desktop/provider are running; use existing scripts/local.ps1 start; open http://127.0.0.1:4173/console/login → More sign-in options → Continue with SSO. Sign in at the lab provider with the existing fixture credential from the local inventory. The lab token expires after120seconds. Existing4177 environment retains its original callback registration.

Configuration rollback: use scripts/local.ps1 stop, restore ui.env.before to ui/.env.local, controlplane.before.yaml to tmp/local/controlplane.yaml, candidate.before.yml to compose.patch-posture-candidate.yml and provider.before.mjs to its original ignored provider path. Remove the lab forwarder service container using the matching Compose service before replacing its definition, restart the provider and run the existing launcher. Do not automatically restore the database backup: that would discard subsequent local records. Retain schema migrations and evidence unless an explicit database recovery is intended. Revert the callback source fix only by a separately reviewed source change if requested. Main SSO connection is local testing, not production activation; full provider/token contract and Phase4 acceptance stay open. Nothing from this follow-up was committed or pushed.


## Verified API access-token profile - 6 November 2026

The earlier in-progress entry is superseded by this bounded verification. The browser now selects only access_token and requires Bearer token_type; it never falls back to id_token. Backend verifies an RS256 signed token using issuer discovery/JWKS with the explicitly configured API resource audience, then requires token type at+jwt/application/at+jwt, the registered client_id, nonempty subject/jti, iat/exp, controlone.access scope and applicable nbf. The single API audience must differ from the browser client ID. The API resource in this lab is controlone-api. This is a declared RFC9068-style JWT access-token contract, not universal access-token or Entra support. Opaque tokens and providers issuing other formats are rejected; no introspection adapter is implemented. Reference: https://www.rfc-editor.org/rfc/rfc9068.html . ID tokens remain provider identity artifacts and are not API credentials.

Passing automated checks: auth package, including16 signed verifier scenarios (valid, signature/issuer/audience/expiry/subject errors, ID token substitution including an ID token with API audience, missing token type/scope/client/jti/iat/exp, future iat/nbf); config tests including distinct API resource requirement;12 UI tests across token exchange/callback/login, plus TypeScript. Existing HTTP loopback provider is test-only; managed production HTTPS is not newly qualified.

Built binary tmp/local/patch-posture-demo/controlplane-api-access-20261106, SHA256 c2f0e1285fbd7f83019fb1c8abb217cdd62f30719982ed8ea3997a2b62743160. Main existing local.ps1 launcher selected it through the untracked candidate override. Provider now issues distinct signed browser-client ID tokens and controlone-api access tokens. Both exact4173/4177 callbacks remain registered; both frontend example/lab settings request controlone.access. Main backend runs it with existing DB/enrollment. The older4177 verifier was stopped and replaced by controlone-oidc-api-access-20261106 (provider network namespace, backend18448) with matching audience; its separate browser flow was not rerun in this follow-up. Old binaries remain retained; do not restart an old verifier with the new frontend/token configuration.

Main live UI: More sign-in options → Continue with SSO → existing lab provider password sign-in → automatic main callback/code/PKCE exchange → Viewer in Control One. One first attempt surfaced Vite's /console base-URL hint; a fresh repeated normal browser flow reached the console automatically and profile showed sso-reviewer@phase4.invalid / Viewer. The hint is retained as an open routing hard-refresh qualification, not claimed fixed. The lab credential expires after120seconds; no extended lifetime was introduced.

Running API evidence from the same issuance: access_token /api/v1/me returned200 with Viewer; id_token /api/v1/me returned401; access_token /api/v1/users returned403. Sanitized results: ignored tmp/local/patch-posture-demo/api-access-20261106/live-token-contract-result.json. Screenshot: live-access-token-viewer.png in that directory. Private token artifacts were kept only in ignored local test storage and were not added to the tracked credential inventory. Historical tracked credentials exception remains unchanged.

Acceptance: bounded API-contract implementation checked. Parent .3p.3d stays open for the intended organizational provider, registration/scopes/claim formats and production activation; .3p.3b remains open for broader authorization transaction/provider failure qualification. Nonce/ID-token binding must be assessed against the chosen flow; this contract does not use the ID token for application authentication. Revocation/introspection, signing-key rotation/outage and queued execution authority remain unchecked. No Phase4 completion or Entra readiness claim. No commit/push performed.

Rollback/configuration: pre-change main config, candidate override and provider source copies are under ignored tmp/local/patch-posture-demo/api-access-20261106. Reverting token format requires reverting the coordinated frontend/provider/backend configuration; simply starting an older binary undermines the new contract. Existing Windows agent service/config/enrollment were not edited. Use the retained previous binary only through a reviewed local rollback; do not revert DB evidence automatically.

Expiry follow-up,6 November2026: same issued API access token subsequently returned401 after its120-second lifetime; browser reload returned to main sign-in. Retained expired-access-result.json and expired-access-token-login.png under the ignored api-access-20261106 evidence directory. This proves token expiry, not early provider revocation.


6 November 2026 OIDC recovery update: bounded discovery retry and signing-key outage/rotation checks passed, including race tests and visible main4173 SSO recovery without API restart. Evidence and limits: [organization identity record](patch-posture-organization-policy-2026-11-03.md#oidc-discovery-and-signing-key-recovery---6-november-2026-bounded-verification-passed). Intended-provider qualification and early revocation stay open; Phase4 remains open.


6 November 2026 SSO landing update: local Vite root refresh repaired and demonstrated on main4173;20 focused tests, TypeScript, production asset build and dev/preview HTTP checks passed. See [identity evidence](patch-posture-organization-policy-2026-11-03.md#sso-landing-route-consistency---6-november-2026). Local landing item checked; production ingress query/refresh qualification added unchecked. Real-provider/revocation and Phase4 remain open.


6 November 2026 Nginx routing update: repository base redirects now preserve queries; both Nginx variants passed16 isolated routing checks and visible built-app login/invalid-callback demonstration. [Evidence and fixture limits](patch-posture-organization-policy-2026-11-03.md#nginx-ingress-routing---6-november-2026). Implementation child checked; actual intended ingress deployment and Phase4 remain open.


6 November 2026 Users access-denied update: P4-08b.3d.6a.3u checked after15 focused tests, TypeScript/lint/build and live existing Operator403/Admin200 verification on main4173. Counters/cached directory/edit controls are hidden for denied access. [Steps and evidence](patch-posture-organization-policy-2026-11-03.md#users-access-denied-view---6-november-2026). Profile Account .3v and parent identity/Phase4 remain open.

## Expansion withdrawn - 6 November 2026

6 November 2026 scope correction: the owner withdrew the new multi-organization SSO expansion. Its connection/isolation/onboarding checklist additions, configurable Entra token-profile adapter, examples and design were removed. These are not Phase4 completion gates. Original single-provider API-token verification, recovery, routing, Account and callback fixes remain. Original acceptance requirements and deferred host/provider checks retain their existing status.

The current backend retains the single configured issuer and explicit RS256 API-access-token contract (at+jwt, distinct API audience, authorized client and controlone.access scope). The removed entra-v2 adapter and organization examples are not supported configuration. Intended-provider qualification already in the original tracker remains separate.


Navigation clarification, 6 November: administrator user management is reached through the top-right **Profile menu → Users & roles**. **Account** opens personal identity information. Separating Account previously removed the only Users shortcut; the replacement administrator entry is restored and live-verified, with 11 existing navigation/account tests passing (P4-08b.3d.6a.3v.n). The primary sidebar stays focused on its existing destinations.


## Current-provider execution authority - 6 November 2026

Control One now has an explicit, optional connection for current authority at patch boundaries. It is separate from OIDC discovery, JWKS and token exchange. Configure one provider for this deployment; this does not introduce multiple organization providers. Keep `auth.execution_authority.enabled: false` until an approved adapter is supplied and qualified. The main local environment deliberately retains that default.

The deployment administrator configures `auth.execution_authority.endpoint`, `credential_file` and optionally `ca_file` in the existing backend YAML. The endpoint must use HTTPS with normal hostname/certificate validation. The credential file contains a service credential, never an employee's sign-in token. Configuration errors prevent backend startup. Credentials are loaded at startup; use a controlled restart when rotating the service credential or trust file. Do not commit these files. See `controlplane/config/controlplane.example.yaml` for placeholder values.

The organization supplies an adapter that can consult its actual provider's supported account, membership, permission and revocation APIs. It must map those results to approved Control One roles and decide the requested tenant/purpose. An ordinary OIDC token or a newly generated timestamp does not establish current account status. The sign-in fixture is not such an adapter. Control One implements the consuming side of this contract; an actual Entra/directory deployment still needs its provider-specific adapter, configuration and qualification.

The request is JSON over an authenticated HTTPS POST. Version 1 includes a fresh 256-bit nonce, `authority` (`oidc:` followed by the configured issuer), the opaque provider `subject`, Control One `user_id`, `tenant_id`, `boundary` (`approval`, `schedule`, `dispatch` or `start`), `purpose` (`organization_owner`, `organization_approver`, `individual_approval` or `tenant_policy`) and an approval/run/job `reference`. The response echoes every request field and supplies `decision_id`, `active`, `allowed`, mapped `roles`, `observed_at` and `expires_at`. All coordinates must match exactly. Observations older than five seconds, future observations beyond one second of skew, expired results and validity beyond ten seconds are rejected. A decision is never cached across boundaries. The provider snapshot has a short validity window; this is bounded revalidation, not a claim of instantaneous external revocation.

Control One requires `admin` for organizational ownership or tenant-policy execution; `admin` or `ciso` for organizational approval; `admin` or `operator` for individual execution approval. Provider results only restrict existing enabled Control One identities and current role assignments. They do not create grants, revive expired grants or bypass independent approval, exact package plans, windows, canary or health gates. Static-token and LDAP identities do not gain an unattended provider path through this OIDC adapter.

Provider requests happen before execution transactions acquire their locks. Each transaction rechecks the same identity binding and decision deadline while holding the identity lock, then evaluates current local grants. Tenant serialization also protects installation-start checks against simultaneous tenant-permission changes. If a policy selects a different person while the provider request is in flight, that person has no corresponding proof and execution is denied. Any deadline expiry, invalid response, refusal or provider outage requires a new check; it does not trigger a fallback.

Verified authentication and explicitly reviewed legacy migration now retain issuer/subject bindings transactionally. Existing hashed identities are not reverse-engineered or backfilled. An existing SSO user must sign in normally once after migration 0193 before its binding is available. The UI exposes only binding readiness, never the provider subject or service credential.

For the operator, open **Profile menu → Users & roles**, locate the SSO account and choose **Edit Roles**. **Unattended patch authority** states whether a binding is retained and whether the authority connection is configured. It explicitly says that readiness does not authorize a patch. The control is read-only. Keep role-source changes separate from configuring provider trust.

Authority check evidence is retained as `identity.execution_authority.check` in **Audit log → Audit scope → Global identity & roles**, restricted to a current global administrator. Metadata includes tenant, purpose, boundary, reference, outcome and decision identifier; it omits raw subject, tokens and service credentials. `provider_snapshot_allowed` means only that the provider snapshot passed; the separate execution transaction must still authorize the job. `provider_check_denied` records a refusal or unavailable/invalid authority. Existing audit retention/archive acceptance remains open.

Qualification evidence for this increment is recorded in `docs/patch-posture-organization-policy-2026-11-03.md` and the canonical acceptance tracker. The actual provider connection, live permitted provider-backed installation workflow and deferred real-host acceptance must remain unchecked until demonstrated with the approved environment.


## Bounded browser token exchange - 6 November 2026

P4-08b.3d.6a.3p.3b.n addresses a Control One safe-failure gap: the browser's provider token request had no deadline, read an unbounded JSON body and could expose arbitrary network-error text. The exchange now has a 15-second deadline covering headers and streamed response, aborts/cancels on completion or failure, and rejects actual response bytes above 64 KiB even without Content-Length. JSON media type, UTF-8, object shape, Bearer token type and header-safe token text are validated. The request does not follow redirects, send ambient browser credentials, cache the response or send a referrer. Provider error bodies and raw parser/network errors are not displayed.

The single-use pending transaction is consumed before any exchange. Empty, oversized or control-character authorization codes are rejected before sending the PKCE verifier; duplicate code/state callback parameters are rejected before exchange. Timeout or failure does not silently retry a one-use authorization code. The previously verified API-access-token profile remains unchanged; an ID token is never used as an API bearer fallback.

A realistic operator scenario is a user signing in while the identity provider or an intervening proxy is unavailable. Instead of waiting indefinitely, Control One provides a fixed timeout or connection-failure message and a Return to sign-in action. The operator should:

1. Read the failure message. Do not paste tokens or repeatedly reload the old callback URL.
2. Choose Return to sign-in.
3. Once the provider is reachable, open More sign-in options and choose Continue with SSO.
4. Authenticate normally with the configured provider. Control One creates a new transaction and checks the resulting profile before publishing the session.
5. Check My account for the expected identity and roles. A successful sign-in does not grant new roles or authorize patch execution.

Verification: 42 affected OIDC, bounded-network, callback and AuthProvider cases passed, including request/header stall, streamed-body stall, declared/actual oversized payloads, malformed responses, ambiguous callbacks, one-use refusal and fresh recovery. TypeScript, scoped lint and production Vite build passed. Structured results and build logs are retained locally as oidc-exchange-tests.json and oidc-exchange-build.txt under p4-access-completion-20261106.

Live main-app demonstration used existing accounts only. After normal administrator sign-out, a diagnostic callback with dummy duplicate codes was rejected with Invalid authorization parameters and Return to sign-in. That diagnostic is an isolated validation example, not a step an ordinary employee should perform. Return to sign-in followed by More sign-in options, Continue with SSO and the existing local provider login succeeded; Profile menu → Account then confirmed the same Viewer identity. Screenshots: oidc-ambiguous-callback-refused.png and oidc-exchange-fresh-sso-recovered.png.

Live network-fault injection was NOT performed: the browser declined raw CDP permission. No alternative network interception, provider shutdown, endpoint substitution or workaround was attempted. Automated timeout/outage evidence and the live callback/recovery demonstration remain distinct; .3b.n.3 and parent .3b.n stay unchecked. No organizational identity-provider qualification, live outage pass or Phase 4 completion is claimed. No user, password, permission grant, PS1, VM or package upgrade was added by this increment.


Callback presentation follow-up: visual inspection of the first refusal screenshot found inherited low-contrast login-card text on an otherwise empty light page. Tracked as .3b.n.ui before fixing it. The callback now uses a centered themed card, readable foreground/error text, a labelled region, status/alert semantics and the standard recovery button. The actual light-theme error screen was rechecked, Return to sign-in worked, and fresh normal SSO plus Profile menu → Account confirmed Viewer. The final 42 tests, TypeScript, scoped lint and production build passed again. Corrected screenshot: oidc-readable-callback-refused.png. The old screenshot records the discovered defect; it must not be presented as the final UI. Live network outage remains unperformed after the declined browser permission; no workaround or network interception was used. Administrator access is restored after the demonstration.
