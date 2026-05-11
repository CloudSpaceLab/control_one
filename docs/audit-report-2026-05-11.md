# Audit Report - 2026-05-11

Branch: `fix/audit-errors`
Base: `origin/main` at `369d4d2`

## Scope

Audited the current `main` branch with the repository CI paths:

- Go formatting, vet, short tests, and race tests.
- Frontend lint, full test suite with coverage, focused failing UI tests, and production build.
- CI workflow review for `.github/workflows/ci.yml` and `.github/workflows/ci.yaml`.

## Findings Fixed

### 1. Non-service nodeagent joins require `/var/log`

`cmd/nodeagent` failed in unprivileged test and CI-like environments:

```text
TestRunJoinEmitsParseableYAML: runJoin: create dir /var/log/control-one/nodeagent: mkdir /var/log/control-one: permission denied
```

Root cause: `runJoin` always created `/var/log/control-one/nodeagent` on non-Windows platforms, even when the caller passed writable `configDir` and `dataDir` values and did not request service installation.

Fix: non-service joins now write logs under `<dataDir>/logs`; service installs on non-Windows still use `/var/log/control-one/nodeagent`. The end-to-end YAML test now asserts the writable log path.

### 2. Dashboard tests omitted tenant context and current API stubs

Frontend tests for `Dashboard` and mobile breakpoints failed after `Dashboard` began using `useTenant` and executive metrics APIs.

Root cause: tests mocked `useTenants` but not `useTenant`, and the stub API client omitted the executive metric methods used by the page.

Fix: tests now stub `useTenant` and provide valid metric response fixtures matching the API/component contracts.

### 3. `worldMap.ts` violated lint rules

Frontend lint failed on explicit `any` usage in `ui/src/lib/worldMap.ts`.

Fix: replaced `any` casts with `geojson` and `topojson-specification` types.

### 4. Fleet enrollment tests could retrigger effects indefinitely

The full UI test suite could hang while running `FleetEnroll.test.tsx`.

Root cause: the mocked `useApiClient` hook returned a fresh object on every render. `FleetEnroll` effects depend on the API client, so the test mock repeatedly invalidated the dependency and kept retriggering state-setting effects.

Fix: the test now uses a hoisted, stable mock API client while still resetting each mocked method before every test.

## Verification

Passing locally:

```bash
test -z "$(gofmt -s -l .)"
go vet ./...
go test -short ./...
go test -short -race ./...
npm run lint --prefix ui
npm run build --prefix ui
npm run test --prefix ui
npm run test --prefix ui -- Dashboard.test.tsx MobileBreakpoint.test.tsx
npx vitest run --coverage src/pages/FleetEnroll.test.tsx
```

Notes:

- `npm run lint --prefix ui` exits 0 with four existing React hook warnings.
- `npx vitest run --coverage src/pages/FleetEnroll.test.tsx` exits 0 and reproduces the previously hanging area in isolation.
