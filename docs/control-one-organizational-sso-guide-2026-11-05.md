# Control One organizational SSO setup and demonstrated behavior

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
6. Review the token contract described below before enabling production access. The existing Control One path uses an ID token as the API bearer; Microsoft's API guidance requires access tokens. Registration alone does not resolve that implementation gap.

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
