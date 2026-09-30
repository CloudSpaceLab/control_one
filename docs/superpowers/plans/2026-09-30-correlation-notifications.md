# Correlation Rule Notifications Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver rule-scoped email and webhook notifications after a correlation rule creates an alert, without allowing a delivery failure to suppress the alert.

**Architecture:** Store a validated notification policy with each correlation rule. The correlation engine persists an alert first, then invokes a narrow best-effort dispatcher implemented by the server and backed by the existing SMTP and webhook delivery paths. The rule editor owns notification-policy input and displays durable policy state after reload.

**Tech Stack:** Go, PostgreSQL migrations, React/TypeScript, Vitest, existing SMTP and webhook delivery services.

---

## File map

- `controlplane/internal/migrate/sql/0151_correlation_notification_policy.{up,down}.sql` — durable policy column and reversible migration.
- `controlplane/internal/storage/correlation.go` — correlation rule data model and persistence.
- `controlplane/internal/storage/correlation_test.go` — storage round-trip and tenant-scoping coverage.
- `controlplane/internal/correlation/engine.go` — alert-first notification dispatch port.
- `controlplane/internal/correlation/engine_test.go` — engine ordering and non-blocking failure behavior.
- `controlplane/internal/server/correlation.go` — request validation, API representation, and notification delivery adapter.
- `controlplane/internal/server/correlation_test.go` — policy validation and server-to-engine wiring.
- `ui/src/lib/api.ts` — correlation-rule policy request and response types.
- `ui/src/pages/Alerts.tsx` — correlation editor policy controls.
- `ui/src/pages/Alerts.test.tsx` — policy persistence and validation UI coverage.

### Task 1: Persist an explicit correlation notification policy

**Files:**
- Create: `controlplane/internal/migrate/sql/0151_correlation_notification_policy.up.sql`
- Create: `controlplane/internal/migrate/sql/0151_correlation_notification_policy.down.sql`
- Modify: `controlplane/internal/storage/correlation.go`
- Modify: `controlplane/internal/storage/correlation_test.go`

- [ ] **Step 1: Write a storage round-trip test**

Add a `NotificationPolicy` field to the test create parameters and assert that `GetCorrelationRule` returns this exact policy:

```go
NotificationPolicy: storage.CorrelationNotificationPolicy{
    EmailRecipients: []string{"soc@example.test"},
    WebhookIDs: []uuid.UUID{webhookID},
    MinimumSeverity: "high",
},
```

- [ ] **Step 2: Run the focused storage test and verify it fails**

Run: `go test ./controlplane/internal/storage -run TestCorrelationRuleNotificationPolicy -count=1 -timeout=20s`

Expected: FAIL because `CorrelationNotificationPolicy` and its persistence do not exist.

- [ ] **Step 3: Add the reversible migration**

Create an additive nullable JSONB column with a safe empty default:

```sql
ALTER TABLE correlation_rules
    ADD COLUMN notification_policy JSONB NOT NULL DEFAULT '{}'::jsonb;
```

The down migration must execute:

```sql
ALTER TABLE correlation_rules DROP COLUMN IF EXISTS notification_policy;
```

- [ ] **Step 4: Add the policy type and JSON storage mapping**

In `correlation.go`, define the exact wire and storage type:

```go
type CorrelationNotificationPolicy struct {
    EmailRecipients  []string    `json:"email_recipients,omitempty"`
    WebhookIDs       []uuid.UUID `json:"webhook_ids,omitempty"`
    MinimumSeverity  string      `json:"minimum_severity,omitempty"`
}
```

Add this field to `CorrelationRule` and `CreateCorrelationRuleParams`; marshal it for create/update, add `notification_policy` to `correlationRuleSelectSQL`, and unmarshal it in `scanCorrelationRule`. Treat `{}` and SQL NULL as an empty policy, never as a scan failure.

- [ ] **Step 5: Run the storage test and format**

Run: `gofmt -w controlplane/internal/storage/correlation.go controlplane/internal/storage/correlation_test.go; go test ./controlplane/internal/storage -run TestCorrelationRuleNotificationPolicy -count=1 -timeout=20s`

Expected: PASS, or the package's documented database skip when no test database is configured.

- [ ] **Step 6: Commit the migration and storage contract**

```bash
git add controlplane/internal/migrate/sql/0151_correlation_notification_policy.* controlplane/internal/storage/correlation.go controlplane/internal/storage/correlation_test.go
git commit -m "feat: persist correlation notification policies"
```

### Task 2: Validate and expose policy through the correlation API

**Files:**
- Modify: `controlplane/internal/server/correlation.go`
- Modify: `controlplane/internal/server/correlation_test.go`

- [ ] **Step 1: Write request-validation tests**

Cover all of the following request outcomes in table-driven tests:

```go
// accepts: email recipient + minimum severity "high"
// accepts: one tenant-owned webhook ID + minimum severity "critical"
// rejects: recipient "not-an-email"
// rejects: minimum severity "urgent"
// rejects: >20 recipients or >20 webhook IDs
// rejects: a webhook ID that does not belong to the request tenant
// removes duplicate recipients case-insensitively and duplicate webhook IDs
```

- [ ] **Step 2: Run the focused server tests and verify failure**

Run: `go test ./controlplane/internal/server -run TestValidateCorrelationNotificationPolicy -count=1 -timeout=20s`

Expected: FAIL because the request field and validator do not exist.

- [ ] **Step 3: Add API types and deterministic validation**

Add `notification_policy` to the create/update request and response mapping. Implement `validateCorrelationNotificationPolicy` with these exact rules:

```go
validSeverity := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
// blank MinimumSeverity becomes "low" only if at least one channel exists.
// each recipient uses net/mail.ParseAddress and is stored lower-case.
// each listed webhook is loaded with tenant scope; missing or cross-tenant IDs return 400.
// an empty policy remains valid and means no delivery.
```

The validated value must flow unchanged into `storage.CreateCorrelationRuleParams` for both POST and PUT.

- [ ] **Step 4: Re-run focused server tests**

Run: `gofmt -w controlplane/internal/server/correlation.go controlplane/internal/server/correlation_test.go; go test ./controlplane/internal/server -run TestValidateCorrelationNotificationPolicy -count=1 -timeout=20s`

Expected: PASS.

- [ ] **Step 5: Commit API validation**

```bash
git add controlplane/internal/server/correlation.go controlplane/internal/server/correlation_test.go
git commit -m "feat: validate correlation notification policies"
```

### Task 3: Dispatch after alert persistence

**Files:**
- Modify: `controlplane/internal/correlation/engine.go`
- Modify: `controlplane/internal/correlation/engine_test.go`
- Modify: `controlplane/internal/server/correlation.go`
- Modify: `controlplane/internal/server/alert_email.go`
- Modify: `controlplane/internal/server/correlation_test.go`

- [ ] **Step 1: Write engine order and failure-isolation tests**

Use a fake `AlertCreator` and a fake dispatcher. Assert that a matching rule calls `CreateAlert` once before `DispatchCorrelationAlert`, and that a dispatcher error leaves the newly created alert returned from the fake storage and does not retry inside the event loop.

```go
type CorrelationAlertDispatcher interface {
    DispatchCorrelationAlert(ctx context.Context, rule storage.CorrelationRule, alert *storage.Alert)
}
```

- [ ] **Step 2: Run the focused engine tests and verify failure**

Run: `go test ./controlplane/internal/correlation -run 'TestEngine.*Notification' -count=1 -timeout=20s`

Expected: FAIL because the dispatch interface is absent.

- [ ] **Step 3: Add the optional best-effort dispatch port**

Extend `correlation.Engine` with an optional `CorrelationAlertDispatcher`, passed at construction without widening `AlertCreator`. Immediately after a successful `CreateAlert`, call the dispatcher in the same goroutine. The dispatcher must not return an error to the engine; it logs internally. Do not launch per-event goroutines and do not dispatch when `len(rule.NotificationPolicy.EmailRecipients) == 0 && len(rule.NotificationPolicy.WebhookIDs) == 0`.

- [ ] **Step 4: Implement the server adapter using existing delivery services**

Implement `DispatchCorrelationAlert` on `Server` with this payload shape:

```go
payload := map[string]any{
    "event_type": "correlation.alert.opened",
    "alert_id": alert.ID.String(), "rule_id": rule.ID.String(),
    "rule_name": rule.Name, "severity": alert.Severity,
    "tenant_id": alert.TenantID.String(), "created_at": alert.CreatedAt.UTC().Format(time.RFC3339),
}
```

Only dispatch when `alert.Severity` is at or above the policy minimum. Send email through the existing SMTP sender with the policy recipient list. Load every selected webhook by tenant ID and call the existing `deliverWebhook` plus `RecordWebhookDelivery`. Log each error with rule and alert IDs; never change alert state, return an HTTP error, or block the correlation engine.

- [ ] **Step 5: Run engine and server tests**

Run: `gofmt -w controlplane/internal/correlation/engine.go controlplane/internal/correlation/engine_test.go controlplane/internal/server/correlation.go controlplane/internal/server/alert_email.go controlplane/internal/server/correlation_test.go; go test ./controlplane/internal/correlation ./controlplane/internal/server -run 'Test(Engine.*Notification|Correlation.*Notification)' -count=1 -timeout=30s`

Expected: PASS.

- [ ] **Step 6: Commit delivery behavior**

```bash
git add controlplane/internal/correlation/engine.go controlplane/internal/correlation/engine_test.go controlplane/internal/server/correlation.go controlplane/internal/server/alert_email.go controlplane/internal/server/correlation_test.go
git commit -m "feat: deliver correlation rule notifications"
```

### Task 4: Add notification policy controls to the rule editor

**Files:**
- Modify: `ui/src/lib/api.ts`
- Modify: `ui/src/pages/Alerts.tsx`
- Modify: `ui/src/pages/Alerts.test.tsx`

- [ ] **Step 1: Write UI tests before implementation**

Add tests that open the correlation-rule editor, enter `soc@example.test`, select `high`, save, and assert the POST body contains:

```ts
notification_policy: {
  email_recipients: ['soc@example.test'],
  webhook_ids: [],
  minimum_severity: 'high',
}
```

Add a second test that loads a rule with this policy and asserts the recipient and severity are rendered.

- [ ] **Step 2: Run the focused UI test and verify failure**

Run: `node C:\dev\control_one_main_fix\ui\node_modules\vitest\vitest.mjs run --root C:\dev\control_one\ui src/pages/Alerts.test.tsx`

Expected: FAIL because policy controls and API types are absent.

- [ ] **Step 3: Add compact policy controls and API types**

Add a `CorrelationNotificationPolicy` TypeScript type matching the API. The editor must have one comma-separated email input, a minimum-severity select (`Low`, `Medium`, `High`, `Critical`), and a multi-select/checkbox list of tenant webhooks already fetched for the rule editor. Submit a policy only when at least one channel is selected; retain the saved policy on edit. Customer-facing labels must be object-focused: `Alert delivery`, `Email recipients`, `Webhook targets`, and `Minimum severity`.

- [ ] **Step 4: Run focused UI tests**

Run: `node C:\dev\control_one_main_fix\ui\node_modules\vitest\vitest.mjs run --root C:\dev\control_one\ui src/pages/Alerts.test.tsx`

Expected: PASS without starting a development server.

- [ ] **Step 5: Commit the editor**

```bash
git add ui/src/lib/api.ts ui/src/pages/Alerts.tsx ui/src/pages/Alerts.test.tsx
git commit -m "feat: configure correlation alert delivery"
```

### Task 5: Verify the releasable notification slice

**Files:**
- Verify only.

- [ ] **Step 1: Run focused backend and UI verification**

Run: `go test ./controlplane/internal/correlation ./controlplane/internal/server -run 'Test(Engine.*Notification|Correlation.*Notification)' -count=1 -timeout=30s`

Expected: PASS.

Run: `node C:\dev\control_one_main_fix\ui\node_modules\vitest\vitest.mjs run --root C:\dev\control_one\ui src/pages/Alerts.test.tsx`

Expected: PASS.

- [ ] **Step 2: Inspect migration and working tree**

Run: `git diff --check; git status --short`

Expected: no whitespace errors; only intentionally tracked changes are present.

