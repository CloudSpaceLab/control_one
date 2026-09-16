# Design: SOC case collaboration (assign / @-tag / comments / notification inbox)

Date: 2026-09-16
Scope: Control plane + web UI (worktree `control_one_ws3`, branch `ws3/team-activity`).
Status: Design (pending approval → implementation plan).

## Problem

SOC analysts share cases but have no way to formally hand work off or call
attention. Comments already exist (case notes in `audit_logs`,
`action='soc.case.note.add'`, written by both the Cases UI and the AI workflow
tool), but there is no owner field, no way to @-mention a teammate, and no
inbox to surface "you were assigned / mentioned".

This spec adds: a single-case owner, @-mention autosuggest in notes,
case-header badges for the owner and mentioned teammates, and an in-app
notification inbox. Assignment is also reachable from the Team activity page.

## Non-goals

- No alert-level assignment (cases only).
- No notification channel beyond the in-app inbox (no email/other).
- Mentions are annotations with notifications; they do not create a
  "requested reviewer" workflow state.
- No change to the entity_tags feature (that tags investigated entities, not
  people).

## Schema (migration `0149_team_collaboration`)

`ai_investigations` gains owner columns:

```sql
ALTER TABLE ai_investigations
    ADD COLUMN IF NOT EXISTS assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS assigned_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS assigned_at   TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_ai_investigations_assignee
    ON ai_investigations (tenant_id, assignee_id, status);
```

New inbox table:

```sql
CREATE TABLE IF NOT EXISTS notifications (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    recipient_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id    UUID REFERENCES users(id) ON DELETE SET NULL,
    kind        TEXT NOT NULL CHECK (kind IN ('case_assigned', 'case_mentioned')),
    case_id     UUID NOT NULL REFERENCES ai_investigations(id) ON DELETE CASCADE,
    case_title  TEXT NOT NULL DEFAULT '',
    read_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient_created
    ON notifications (recipient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_recipient_unread
    ON notifications (recipient_id, read_at)
    WHERE read_at IS NULL;
```

Notes continue to live in `audit_logs`. Mentions are stored on the note's
`metadata.mentions` (array of user UUID strings) so the comment path — humans
and the AI workflow tool — stays unchanged. No schema change for mentions.

## Backend

All endpoints: `authorize(roleInvestigator, roleOperator, roleAdmin)` +
`requireTenantAccessFromQuery`, and case resources verified to belong to the
tenant before any write.

### Storage (new methods in `controlplane/internal/storage`)

- `AssignCase(ctx, tenantID, caseID, assigneeID uuid.UUID /* uuid.Nil = unassign */, assignedBy, now)` — sets the three owner columns; no-op on same assignee.
- `CaseAssignee(ctx, tenantID, caseID)` — read the owner for the response.
- `CreateNotification(params)`; `ListNotifications(ctx, tenantID, recipientID, filter{unreadOnly bool, limit, offset}) ([]Notification, total, error)`; `CountUnreadNotifications(ctx, tenantID, recipientID) (int, error)`; `MarkNotificationRead(ctx, tenantID, recipientID, id)` — idempotent, only own rows; `MarkAllNotificationsRead(ctx, tenantID, recipientID)`.
- `ListTenantUsers(ctx, tenantID, query string) ([]TeamUser, error)` — users holding role `investigator`, `operator`, or `admin` in the tenant (`user_roles.tenant_id = $1`) or globally (`user_roles.tenant_id IS NULL`), `DISTINCT` on user id, excludes `viewer`. Matches leader name/local-part/email, first 25 ordered by display name.
- `CaseMentionedUsers(ctx, tenantID, caseID) ([]uuid.UUID, error)` — distinct `actor`-free users from `audit_logs.metadata->'mentions'` for the case (independent of the notes list cap of 3 in the collection endpoint).

### Endpoints

- `POST /api/v1/cases/{id}/assign` `{"assignee_id": "uuid" | null}` → `AssignCase`,
  audit entry `soc.case.assign` (`metadata`: `{assignee_id, assigned_by}`), and a
  `case_assigned` notification for the assignee unless it is the actor.
  Returns the updated `socCaseResponse` (assignee included).
- `POST /api/v1/cases/{id}/notes` — extended, backwards compatible: optional
  `mentions: string[]` (validated: every id resolves to a `ListTenantUsers`
  member; cap 8; else 400). Stored on `metadata.mentions`. For each distinct
  mentioned user except the note author: `case_mentioned` notification.
- `GET /api/v1/team/users?tenant_id&q` — autosuggest source.
- `GET /api/v1/notifications?tenant_id&unread_only&limit&offset`,
  `GET /api/v1/notifications/unread-count?tenant_id`,
  `POST /api/v1/notifications/{id}/read`, `POST /api/v1/notifications/read-all?tenant_id`.

### Response shape

`socCaseResponse` gains:
- `assignee: {id, name} | null`
- `mentioned_users: [{id, name}]` (from `CaseMentionedUsers` + batched name
  lookup, capped at 8 in the response).

## UI

### Cases page (`ui/src/pages/Cases.tsx`)
- Case header: assignee control — `Popover` + `Command` combobox (search,
  debounced 200ms) over `GET /team/users`; "Clear" to unassign; default
  placeholder "Unassigned". Badge shows assignee avatar+name next to status.
  Mentioned users render as up to 4 avatar chips with a `+N` overflow tooltip.
- `NotesPanel`: `@` opens the same combobox filtered by the typed prefix;
  Enter inserts an inline chip token (raw `@Name` text also kept for
  provenance); cap 8 mentioned per note. Rendered note displays mention chips.
- After assign/mention actions, refetch case + notification count immediately.

### Team activity page (`ui/src/pages/TeamActivity.tsx`)
- New "Open cases" panel below "Analyst workload": reuses `listSOCCases`
  (`status != closed`, sort `created_at desc`, real `Pagination`, 25/page).
  Columns: severity tag, title, creator, created, assignee picker (same
  component). Empty state: "No open cases for this tenant."

### Top bar (`ui/src/components/shell/TopBar.tsx`)
- Notification bell with unread-count badge. Poll every 30s, refetch on
  visibility focus and after in-app assign/mention actions. No badge when no
  tenant is selected. Dropdown: latest 20 unread, per-row kind copy
  ("Assigned to case" / "Mentioned in case"), case title, relative time,
  click navigates to `/cases` and marks read. "Mark all read" button. Empty
  state: "No notifications."

### Notification copy (human, not "AI")
- kind labels: "Assigned to case" / "Mentioned in case"; body: `{actor} assigned
  {title} to you` / `{actor} mentioned you in {title}`; errors in standard
  product tone. No emojis.

## Notification semantics

- `case_assigned`: created on assign/reassign to a different owner, not on
  no-op or unassign; skipped when assignee === actor.
- `case_mentioned`: one per distinct mentioned user, skipped for the author;
  an existing identical unread mention for the same case by the same actor is
  not duplicated.
- Comments without mention/assignment do not notify (keeps signal high;
  designed extension point).

## Testing

- Storage: assign/unassign/no-op, notification create/list/count/mark read/all,
  `ListTenantUsers` DISTINCT + global-role inclusion, `CaseMentionedUsers`.
- Server: handler tests via `fakeStore` — assign validation (case in tenant,
  assignee in tenant users, 400 on malformed), mention validation (count cap,
  non-member 400), notification read only own rows.
- UI: assignee combobox (set/clear), mention autosuggest (type `@`, select,
  chip render, cap), bell badge + mark-all-read, Team page "Open cases"
  pagination, following the `Cases.test.tsx` mock pattern.

## Risks / notes

- `ListTenantUsers` grows with users; capped at 25 returned and autosuggest
  picks the text prefix server-side.
- Notifications and assignee FKs `ON DELETE CASCADE/SET NULL` keep lineage
  safe when users/cases are removed.
- No migration changes the AI workflow tool's note writer.