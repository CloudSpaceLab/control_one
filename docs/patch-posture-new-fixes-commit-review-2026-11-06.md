# Proposed follow-up commit — review before approval

Branch: `0y3/Me/patch-posture-review`
Base commit already pushed: `e44d461a`
Status: proposed only; nothing staged or committed by this review.

## Proposed message

```text
fix(patch): harden identity authority, audit evidence and SSO recovery

Preserve global role-source ownership, actors and existing grant deadlines.
Record reviewed role changes atomically and expose administrator-only global
identity audit readback, filtering and export without weakening tenant scope.

Add reviewed legacy authentication migration with authority/subject binding,
collision and disabled-account rejection, preserved independent grants and
retained migration evidence. Add schema migrations 0189-0193.

Implement an optional single-provider current-authority HTTPS contract for
approval, scheduling, dispatch and installation start. Bind decisions to nonce,
identity, tenant, purpose, boundary and request reference; enforce short decision
lifetimes and current local permissions. Recheck organizational withdrawal,
provider identity changes and late expiry before authorizing execution. Keep
unconfigured, refused or unavailable authority fail-closed. Record provider
snapshot evidence separately from installation success.

Require explicitly typed API access tokens; improve discovery/JWKS outage and
key-rotation recovery. Verify candidate profiles before publishing browser
sessions and prevent cancelled sign-in attempts from restoring stale sessions.
Bound browser token exchange to 15 seconds and 64 KiB; reject malformed or
ambiguous callbacks, redirects and unsafe tokens, and redact provider errors.

Improve Users permission-denied states, source-review readiness, Account and
administrator navigation, global audit UI and readable callback recovery.
Preserve console base-path queries in Vite and Nginx. Include affected tests,
public environment examples, reusable lab presentation/routing helpers and
updated acceptance and operator documentation.

Include the updated real local credential inventory under the repository
owner's previously confirmed exception. Working environment files, authority
service credentials, certificates, keys, backups, binaries, screenshots and
runtime database contents are excluded.

Validation recorded across the documented bounded checkpoints:
- 38 combined storage, identity, organization and scheduler checks passed;
  six authority cases passed across two repeated runs, including duplicate
  dispatch producing one job; 24 authority-client checks passed.
- Targeted authentication/config/API, role-source, migration, audit, routing
  and permission-boundary tests passed at their documented revisions.
- Final browser-exchange change: 42 UI tests, TypeScript, scoped lint and
  production build passed. Earlier affected UI checks are recorded in docs.
- Live main-app callback refusal and fresh normal SSO recovery retained Viewer
  access; administrator readiness showed a retained binding with the separate
  current-authority connection still unconfigured and execution blocked.

Remaining acceptance limitations:
- Phase 4 and its broader organizational identity/policy acceptance remain open.
- Actual organizational provider qualification and a configured/permitted live
  current-authority execution workflow remain outstanding.
- Live exchange network-outage/timeout injection was declined by browser
  permission; automated evidence does not close that live acceptance item.
- The earlier unexplained combined scheduling failure remains tracked.
- Deferred full-host/VM, managed trust, external recovery and wider rollout
  requirements retain their existing gates.
- These are bounded passes across checkpoints, not a single full-repository
  production release qualification. Multi-organization SSO is excluded.
```

## Exact proposed files (94)

Paths below are relative to `C:/Projects/control_one`. Includes all content changes reported by `git diff --name-only` plus the explicitly reviewed new source/test/migration files. Files reported modified only because of working-tree metadata/line endings with no content diff are not listed.

### Backend (40)

- `controlplane/cmd/controlplane/main.go`
- `controlplane/config/controlplane.example.yaml`
- `controlplane/internal/auth/middleware.go`
- `controlplane/internal/auth/oidc_provider.go`
- `controlplane/internal/auth/oidc_provider_test.go`
- `controlplane/internal/auth/oidc_recovery_test.go`
- `controlplane/internal/config/config.go`
- `controlplane/internal/config/config_test.go`
- `controlplane/internal/executionauthority/client.go`
- `controlplane/internal/executionauthority/client_test.go`
- `controlplane/internal/migrate/sql/0189_global_role_change_events.down.sql`
- `controlplane/internal/migrate/sql/0189_global_role_change_events.up.sql`
- `controlplane/internal/migrate/sql/0190_global_identity_audit.down.sql`
- `controlplane/internal/migrate/sql/0190_global_identity_audit.up.sql`
- `controlplane/internal/migrate/sql/0191_authentication_identity_migrations.down.sql`
- `controlplane/internal/migrate/sql/0191_authentication_identity_migrations.up.sql`
- `controlplane/internal/migrate/sql/0192_identity_migration_audit.down.sql`
- `controlplane/internal/migrate/sql/0192_identity_migration_audit.up.sql`
- `controlplane/internal/migrate/sql/0193_authentication_authority_bindings.down.sql`
- `controlplane/internal/migrate/sql/0193_authentication_authority_bindings.up.sql`
- `controlplane/internal/server/audit.go`
- `controlplane/internal/server/authentication_migration.go`
- `controlplane/internal/server/global_identity_audit_test.go`
- `controlplane/internal/server/role_source_review.go`
- `controlplane/internal/server/role_source_review_test.go`
- `controlplane/internal/server/server.go`
- `controlplane/internal/storage/authentication_identity_migration.go`
- `controlplane/internal/storage/authentication_identity_migration_test.go`
- `controlplane/internal/storage/authentication_role_sources.go`
- `controlplane/internal/storage/execution_authority.go`
- `controlplane/internal/storage/execution_authority_test.go`
- `controlplane/internal/storage/global_role_state.go`
- `controlplane/internal/storage/global_role_writer_test.go`
- `controlplane/internal/storage/patch_approvals.go`
- `controlplane/internal/storage/patch_automation.go`
- `controlplane/internal/storage/patch_dispatch.go`
- `controlplane/internal/storage/patch_organization.go`
- `controlplane/internal/storage/patch_organization_execution.go`
- `controlplane/internal/storage/rbac.go`
- `controlplane/internal/storage/store.go`

### Ingress (2)

- `deploy/nginx/control-one.host.conf`
- `deploy/nginx/edge.conf`

### Documentation (21)

- `docs/control-one-organizational-sso-guide-2026-11-05.md`
- `docs/development-environment.md`
- `docs/patch-posture-end-to-end-review.md`
- `docs/patch-posture-exact-online-plans-2026-11-02.md`
- `docs/patch-posture-local-demo-baseline.md`
- `docs/patch-posture-organization-policy-2026-11-03.md`
- `docs/patch-posture-phase-acceptance.md`
- `docs/patch-posture-phase1-execution-2026-10-27.md`
- `docs/patch-posture-phase1-nonvm-2026-10-27.md`
- `docs/patch-posture-phase2-state-2026-10-27.md`
- `docs/patch-posture-phase3-platforms-2026-10-29.md`
- `docs/patch-posture-phase4-automation-2026-11-02.md`
- `docs/patch-posture-phase4-commit-plan-2026-11-05.md`
- `docs/patch-posture-phase4-governance-2026-11-02.md`
- `docs/patch-posture-phase4-operations-2026-11-02.md`
- `docs/patch-posture-real-package-demo.md`
- `docs/patch-posture-self-service-recovery-guide.md`
- `docs/patch-posture-three-areas-verification-2026-10-23.md`
- `docs/patch-posture-trust-verification-2026-10-23.md`
- `docs/patch-posture-verification-2026-10-21.md`

- `docs/patch-posture-new-fixes-commit-review-2026-11-06.md`

### Reusable demonstration/test helpers (2)

- `scripts/oidc-lab-ui.mjs`
- `scripts/test-console-ingress.py`

### Frontend (28)

- `ui/.env.example`
- `ui/.env.sso-lab.example`
- `ui/build/console-base.test.ts`
- `ui/build/console-base.ts`
- `ui/src/App.tsx`
- `ui/src/components/shell/ProfileMenu.tsx`
- `ui/src/features/patch-management/GlobalRoleSources.test.tsx`
- `ui/src/features/patch-management/GlobalRoleSources.tsx`
- `ui/src/hooks/useAuditLogs.test.tsx`
- `ui/src/hooks/useAuditLogs.ts`
- `ui/src/hooks/useDirectoryAccess.test.tsx`
- `ui/src/hooks/useRoles.ts`
- `ui/src/hooks/useUsers.ts`
- `ui/src/lib/api.ts`
- `ui/src/lib/oidc-network.test.ts`
- `ui/src/lib/oidc.test.ts`
- `ui/src/lib/oidc.ts`
- `ui/src/pages/Account.test.tsx`
- `ui/src/pages/Account.tsx`
- `ui/src/pages/Audit.test.tsx`
- `ui/src/pages/Audit.tsx`
- `ui/src/pages/AuthCallback.test.tsx`
- `ui/src/pages/AuthCallback.tsx`
- `ui/src/pages/Users.test.tsx`
- `ui/src/pages/Users.tsx`
- `ui/src/providers/AuthProvider.test.tsx`
- `ui/src/providers/AuthProvider.tsx`
- `ui/vite.config.ts`

### Previously approved credential-inventory exception (1)

- `tmp/local/credentials.json`

## Excluded pending files

- `compose.dockerhub-demo.yml`
- `compose.patch-posture-candidate.yml`
- `compose.patch-posture-lab.yml`
- `data/`
- `deploy/Dockerfile.console.dockerignore`
- `deploy/Dockerfile.controlplane.dockerignore`
- `ui/pnpm-lock.yaml`
- `ui/pnpm-workspace.yaml`
- `ui/vite.config.ts.timestamp-1793171106885-6949b3b9da5ba.mjs`

Also exclude ignored working `.env` files, `ui/dist/`, node_modules, database/VM files, logs, test reports, screenshots, local candidate executables, backups, certificates, private keys and authority service credentials. The Dockerfile ignore proposals are separate build-context work; this commit does not include or claim Docker-image qualification. The pnpm workspace file contains an unresolved build-approval placeholder and is not an approved package-manager migration.

`tmp/local/credentials.json` remains the explicit owner-approved exception carried forward from the prior commit. Its updated contents must not be printed in the review. `ui/build/console-base.ts` and its test are source, not generated output. The provider HTML renderer is reusable lab presentation only; it does not make the ignored provider runtime or production SSO setup reproducible.

Before committing after approval: stage only this manifest, inspect staged dependency completeness and whitespace, and verify the staged candidate as appropriate. Do not use `git add .`.
