> **Final selection update, 5 November 2026:** the repository owner explicitly confirmed including the real `tmp/local/credentials.json` despite Git-history exposure. That exact file is an exception to the earlier exclusion below. Added public `ui/.env.example`, `ui/.env.sso-lab.example` and the `.gitignore` exceptions. The main selection is now 127 files (123 original + 3 template/ignore changes + the real credential inventory). The optional lab renderer remains excluded. Local commit only; no push authorized. Historical proposals below are superseded where they conflict with this selection.

# Phase 4 commit selection - 5 November 2026

This is a proposed selection from the current working tree, not a completed code review, staged index or production-readiness sign-off. Branch: `0y3/Me/patch-posture-review`. Nothing was staged or committed while preparing this list. Shared files include multiple identity/governance/SSO changes; splitting into commits requires reviewing hunks and dependencies, not simply omitting the new storage files or migrations.

The main selection includes implementation, tests, schema migrations 0179–0188 (both directions), SSO/environment guidance, acceptance status and related documentation. AGENTS.md carries the user's repository workflow rules. Older phase documents have related status/evidence notices and belong with those updates. Phase 4 remains open; the commit message must not claim full completion or production rollout readiness.

## Main implementation and documentation selection

Exact snapshot allowlist:

```text
AGENTS.md
controlplane/cmd/bootstrap-admin/main.go
controlplane/internal/auth/ldap.go
controlplane/internal/auth/ldap_disposable_integration_test.go
controlplane/internal/auth/ldap_group_failure_test.go
controlplane/internal/auth/ldap_session_guard.go
controlplane/internal/auth/middleware.go
controlplane/internal/auth/middleware_test.go
controlplane/internal/auth/oidc_provider.go
controlplane/internal/auth/oidc_provider_test.go
controlplane/internal/migrate/sql/0179_patch_organization_policy.down.sql
controlplane/internal/migrate/sql/0179_patch_organization_policy.up.sql
controlplane/internal/migrate/sql/0180_patch_organization_approval_bindings.down.sql
controlplane/internal/migrate/sql/0180_patch_organization_approval_bindings.up.sql
controlplane/internal/migrate/sql/0181_tenant_role_control_history.down.sql
controlplane/internal/migrate/sql/0181_tenant_role_control_history.up.sql
controlplane/internal/migrate/sql/0182_global_role_sources.down.sql
controlplane/internal/migrate/sql/0182_global_role_sources.up.sql
controlplane/internal/migrate/sql/0183_global_role_source_expiry.down.sql
controlplane/internal/migrate/sql/0183_global_role_source_expiry.up.sql
controlplane/internal/migrate/sql/0184_role_source_review.down.sql
controlplane/internal/migrate/sql/0184_role_source_review.up.sql
controlplane/internal/migrate/sql/0185_role_source_changes.down.sql
controlplane/internal/migrate/sql/0185_role_source_changes.up.sql
controlplane/internal/migrate/sql/0186_global_provisioning_source.down.sql
controlplane/internal/migrate/sql/0186_global_provisioning_source.up.sql
controlplane/internal/migrate/sql/0187_role_source_instance.down.sql
controlplane/internal/migrate/sql/0187_role_source_instance.up.sql
controlplane/internal/migrate/sql/0188_authentication_role_reconciliation.down.sql
controlplane/internal/migrate/sql/0188_authentication_role_reconciliation.up.sql
controlplane/internal/server/audit.go
controlplane/internal/server/audit_test.go
controlplane/internal/server/auth_ldap_qualification_test.go
controlplane/internal/server/auth_login.go
controlplane/internal/server/global_user_roles_test.go
controlplane/internal/server/patch.go
controlplane/internal/server/patch_governance.go
controlplane/internal/server/patch_online.go
controlplane/internal/server/patch_organization.go
controlplane/internal/server/patch_organization_test.go
controlplane/internal/server/patch_review_operations.go
controlplane/internal/server/permissions.go
controlplane/internal/server/permissions_rbac_test.go
controlplane/internal/server/role_source_review.go
controlplane/internal/server/role_source_review_test.go
controlplane/internal/server/server.go
controlplane/internal/server/server_test.go
controlplane/internal/server/tenant_role_controls.go
controlplane/internal/server/tenant_role_controls_test.go
controlplane/internal/storage/administrator_role_source.go
controlplane/internal/storage/administrator_role_source_test.go
controlplane/internal/storage/authentication_role_sources.go
controlplane/internal/storage/authentication_role_sources_test.go
controlplane/internal/storage/directory_role_sources.go
controlplane/internal/storage/directory_role_sources_test.go
controlplane/internal/storage/global_role_concurrency_test.go
controlplane/internal/storage/global_role_state.go
controlplane/internal/storage/global_role_state_test.go
controlplane/internal/storage/global_role_writer.go
controlplane/internal/storage/global_role_writer_test.go
controlplane/internal/storage/local_auth.go
controlplane/internal/storage/patch_approvals.go
controlplane/internal/storage/patch_automation.go
controlplane/internal/storage/patch_automation_test.go
controlplane/internal/storage/patch_dispatch.go
controlplane/internal/storage/patch_governance_pages.go
controlplane/internal/storage/patch_organization.go
controlplane/internal/storage/patch_organization_execution.go
controlplane/internal/storage/patch_organization_execution_test.go
controlplane/internal/storage/patch_organization_test.go
controlplane/internal/storage/rbac.go
controlplane/internal/storage/rbac_permissions.go
controlplane/internal/storage/role_source_review.go
controlplane/internal/storage/role_source_review_test.go
controlplane/internal/storage/tenant_role_controls.go
controlplane/internal/storage/tenant_role_controls_test.go
docs/control-one-organizational-sso-guide-2026-11-05.md
docs/development-environment.md
docs/patch-posture-end-to-end-review.md
docs/patch-posture-exact-online-plans-2026-11-02.md
docs/patch-posture-local-demo-baseline.md
docs/patch-posture-organization-policy-2026-11-03.md
docs/patch-posture-phase-acceptance.md
docs/patch-posture-phase1-execution-2026-10-27.md
docs/patch-posture-phase1-nonvm-2026-10-27.md
docs/patch-posture-phase2-state-2026-10-27.md
docs/patch-posture-phase3-platforms-2026-10-29.md
docs/patch-posture-phase4-automation-2026-11-02.md
docs/patch-posture-phase4-commit-plan-2026-11-05.md
docs/patch-posture-phase4-governance-2026-11-02.md
docs/patch-posture-phase4-operations-2026-11-02.md
docs/patch-posture-real-package-demo.md
docs/patch-posture-self-service-recovery-guide.md
docs/patch-posture-three-areas-verification-2026-10-23.md
docs/patch-posture-trust-verification-2026-10-23.md
docs/patch-posture-verification-2026-10-21.md
internal/patchgovernance/organization.go
internal/patchgovernance/organization_test.go
ui/package.json
ui/src/components/shell/Sidebar.patch-review.test.tsx
ui/src/components/shell/Sidebar.tsx
ui/src/config/oidc.ts
ui/src/features/patch-management/GlobalRoleSources.test.tsx
ui/src/features/patch-management/GlobalRoleSources.tsx
ui/src/features/patch-management/PatchOperationsPanel.test.tsx
ui/src/features/patch-management/PatchOperationsPanel.tsx
ui/src/features/patch-management/PatchOrganizationPanel.test.tsx
ui/src/features/patch-management/PatchOrganizationPanel.tsx
ui/src/features/patch-management/TenantRolePanel.test.tsx
ui/src/features/patch-management/TenantRolePanel.tsx
ui/src/features/patch-management/organizationTypes.ts
ui/src/features/patch-management/tenantRoleTypes.ts
ui/src/lib/api.global-roles.test.ts
ui/src/lib/api.ts
ui/src/lib/oidc.test.ts
ui/src/lib/oidc.ts
ui/src/pages/PatchManagement.test.tsx
ui/src/pages/PatchManagement.tsx
ui/src/pages/Users.test.tsx
ui/src/pages/Users.tsx
ui/src/providers/AuthProvider.test.tsx
ui/src/providers/AuthProvider.tsx
ui/vite.config.ts
```

## Optional isolated-demo presentation source

These are reusable source files, not a running process or captured evidence. Include in a separate reviewed demo-support commit or deliberately add to the main selection. The HTML renderer alone does not reproduce the full provider: the provider implementation/configuration/credentials remain ignored under tmp. Do not claim that including this file makes the demo fully reproducible.

```text
scripts/oidc-lab-ui.mjs
```

## Leave out of this Phase 4 commit

Current untracked runtime, unrelated deployment or package-manager artifacts:

```text
compose.dockerhub-demo.yml
compose.patch-posture-candidate.yml
compose.patch-posture-lab.yml
data/analytics/controlone-small-analytics.db
data/analytics/controlone-small-analytics.db-shm
data/analytics/controlone-small-analytics.db-wal
data/threat-intel/static-firehol-level1.json
data/threat-intel/static-spamhaus-drop.json
data/threat-intel/static-spamhaus-edrop.json
data/threat-intel/static-tor-exit.json
deploy/Dockerfile.console.dockerignore
deploy/Dockerfile.controlplane.dockerignore
ui/pnpm-lock.yaml
ui/pnpm-workspace.yaml
ui/vite.config.ts.timestamp-1793171106885-6949b3b9da5ba.mjs
```

In addition, leave all actual `.env.local`/`.env.sso-lab.local`, credentials.json, provider credentials, signing keys/certificates, database backups/dumps, SQLite databases/WAL/SHM files, local binaries/images, screenshots/logs/protocol receipts, node_modules, build outputs and tmp/local runtime files out. They may be ignored already and therefore absent from git status. Never force-add ignored secrets/evidence to fill a documentation link.

`ui/package-lock.json` is already tracked and unchanged; do not add a second generated pnpm lock/workspace just because a local dependency repair used pnpm. No new package dependencies were added by the SSO script-only package.json change. The untracked Docker ignore files are potentially reusable build source but are not required by these Phase 4 code changes; review separately with their deployment scope. Compose candidate/lab overrides point at local runtime identities/artifacts and are excluded. compose.dockerhub-demo.yml is an unrelated deployment demonstration and is excluded.

## Review and commit procedure

Review the exact manifest before staging. The ignored local manifest is an explicit filename list, not a recursive directory add. It is a snapshot and must be refreshed if files change. Recommended staging command, not executed here:

```powershell
git add --pathspec-from-file=tmp/local/phase4-commit-files-20261105.txt
git diff --cached --name-status
git diff --cached --check
git diff --cached
```

Review staged files for credentials, local artifacts and unrelated changes; run relevant checks for the staged candidate and verify new migrations/dependencies in a clean environment. Existing bounded lab test passes do not replace a clean staged-candidate qualification. Review the detailed commit message before git commit, then inspect `git show --stat HEAD` and confirm the running candidate separately. A git commit does not automatically rebuild/restart the running app. Push only after authorization.

Suggested title: `feat(patch): add Phase 4 governance, identity controls and SSO safeguards`.

The body should describe organizational policy and independent approval, scoped/global source-aware permissions and lifecycle checks, authority-bound authentication reconciliation, PKCE/callback/session safeguards, applicable tests/live evidence and remaining acceptance limitations. Do not say Phase 4 is complete or Entra production qualification passed.


## Detailed proposed commit message

This message describes the current 123-file main selection, not uncreated example files or the optional lab renderer. It is a draft for user review; no commit was performed.

```text
feat(patch): add Phase 4 governance, identity controls and bounded SSO verification

Add organizational patch-policy drafts and independent approval controls,
with revision-bound authorization checks at covered dispatch/start boundaries.
Expose policy, ownership and tenant permission controls in the patch UI.

Add tenant-scoped role lifecycle controls and source-aware global grants.
Separate administrator, directory, provisioning and authentication ownership;
preserve independent grants during reconciliation. Require explicit review
of ambiguous legacy grants, enforce source expiry/revocation and reject stale
administrator edits with retained actor/reason evidence.

Bind new authentication identities to authority and subject. Reconcile current
authentication roles without retaining removed claim authority. Fail closed
on identity/grant persistence errors, disabled identities or ambiguous origins.
Serialize concurrent first sign-in and commit grant changes with audit evidence.
Recheck LDAP session authority through the configured directory guard.

Add OIDC browser callback/base-path and pending PKCE transaction safeguards,
including transaction lifetime/input validation. Verify environment-mode
separation and server-only proxy settings. Retain the normal automatic
SSO redirect, code exchange and backend verification flow without requiring
users to paste tokens. Document local SSO setup and repeatable browser checks.

Include dependent schema migrations 0179-0188, backend/UI tests and updated
acceptance/evidence documentation. Preserve acceptance tracking instructions
in AGENTS.md. Keep actual local environments, credentials, keys, certificates,
database contents, binaries, screenshots and protocol logs outside Git.

Validation recorded:
- Focused authentication, authorization, source reconciliation, expiry,
  concurrency, audit-rollback and organizational execution checks.
- UI tests and bounded isolated browser demonstrations of distinct accounts,
  permission denials, role reduction, credential removal and SSO code flow.
- Isolated SSO verifier cases for invalid signature/issuer/audience/expiry,
  plus documented TypeScript/lint/build checks at their recorded revisions.

Remaining acceptance limitations:
- Phase 4 organizational policy/UI acceptance is still open; this commit does
  not represent complete Phase 4 delivery or approved production activation.
- Real Entra/intended-provider qualification and the production API token-use
  or server-side session contract remain open.
- Nonce/token binding, provider revocation/key rotation/outage, legacy identity
  migration and execution-time provider authority need further qualification.
- Unauthorized Users-route presentation and Profile Account navigation gaps
  remain tracked; an earlier combined scheduling failure remains undiagnosed.
- Deferred real-host/VM, managed trust and external recovery requirements remain
  governed by their existing acceptance gates.
- Validate the complete staged candidate and dependencies before merge; bounded
  tests at earlier recorded revisions are not one clean full-suite release pass.

```

## Sanitized example-file proposal (not yet implemented)

Recommended addition before sharing developer setup: create `ui/.env.example` and `ui/.env.sso-lab.example`, and update `.gitignore` to explicitly allow exactly those examples while continuing to ignore working environment files. These three proposed changes are not present in the current manifest; after implementation/review, the main selection would be 126 files. Do not rename or move the existing working environment files. Create new examples containing documented public settings, safe localhost lab values and obvious organization placeholders, with no passwords, bearer tokens, client secrets or private keys.

Do not commit the real credentials inventory. The setup guide should describe credential fields, secret storage and generation/provisioning steps using placeholders. An example configuration is not authorization to use this machine's current test identities. The optional scripts/oidc-lab-ui.mjs remains a separate explicitly chosen demo-support file, outside the 123/126-file main counts. Do not claim complete provider reproducibility from that renderer alone.


## Authorized local commit preparation

The final 127-file selection was staged after explicit owner confirmation of
including the real credential inventory. Public example files exist and are
allowed by Git; actual working environment files and the remaining runtime
artifacts are excluded. Staged whitespace checks passed. The 10 selected UI
test files passed all 73 tests, and TypeScript passed. Focused backend
authentication, PostgreSQL storage and server verification also passed. No remote push is part
of this authorization, and Phase 4 acceptance remains open.
