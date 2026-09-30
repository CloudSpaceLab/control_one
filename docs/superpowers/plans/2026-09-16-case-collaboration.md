# SOC Case Collaboration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add case ownership, @-mention notifications, and a notification inbox to SOC cases so analysts can hand work off and surface attention.

**Architecture:** Owner lives on `ai_investigations` (new columns); mentions ride the existing note audit metadata; a new `notifications` table drives the inbox. Backend follows existing soc-cases / team-activity conventions (Store methods, role-gated handlers, `writeJSON`). UI reuses the Command+Popover combobox pattern for assignee and mention pickers.

**Tech Stack:** Go (net/http, lib/pq, google/uuid), React (tanstack table, Command/Popover shadcn-style primitives), vitest + @testing-library/react, Postgres migrations via golang-migrate (embedded `sql/*.sql`).

**Spec:** `docs/superpowers/specs/2026-09-16-case-collaboration-design.md`

**Working directory:** `C:\dev\control_one_ws3` (worktree, branch `ws3/team-activity`). Go module root is the repo root (run Go commands from there); UI commands run from `ui/`. Follow existing gofmt/eslint. Commit frequently on `ws3/team-activity`.

---

### Task 1: Migration 0149 (case ownership + notifications schema)

**Files:**
- Create: `controlplane/internal/migrate/sql/0149_team_collaboration.up.sql`
- Create: `controlplane/internal/migrate/sql/0149_team_collaboration.down.sql`

- [ ] **Step 1: Create the up migration**

```sql
-- SOC case collaboration. Owner columns on ai_investigations; an inbox table
-- for assignment/mention notifications. Mentions themselves live in the
-- existing note audit metadata (soc.case.note.add -> metadata->'mentions').
ALTER TABLE ai_investigations
    ADD COLUMN IF NOT EXISTS assignee_id UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS assigned_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS assigned_at   TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_ai_investigations_assignee
    ON ai_investigations (tenant_id, assignee_id, status);

CREATE TABLE IF NOT EXISTS notifications (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    recipient_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    actor_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    kind         TEXT NOT NULL CHECK (kind IN ('case_assigned', 'case_mentioned')),
    case_id      UUID NOT NULL REFERENCES ai_investigations(id) ON DELETE CASCADE,
    case_title   TEXT NOT NULL DEFAULT '',
    read_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_recipient_created
    ON notifications (recipient_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_recipient_unread
    ON notifications (recipient_id, read_at)
    WHERE read_at IS NULL;
```

- [ ] **Step 2: Create the down migration**

```sql
DROP TABLE IF EXISTS notifications;
DROP INDEX IF EXISTS idx_ai_investigations_assignee;
ALTER TABLE ai_investigations
    DROP COLUMN IF EXISTS assignee_id,
    DROP COLUMN IF EXISTS assigned_by,
    DROP COLUMN IF EXISTS assigned_at;
```

- [ ] **Step 3: Build**

Run (from `C:\dev\control_one_ws3`): `go build ./...`
Expected: exit 0 (migrations are glob-embedded, no manifest to update).

- [ ] **Step 4: Commit**

```bash
git add controlplane/internal/migrate/sql/0149_team_collaboration.up.sql controlplane/internal/migrate/sql/0149_team_collaboration.down.sql
git commit -m "feat: add soc case collaboration schema (assignee + notifications)"
```

---

### Task 2: Storage — ListTenantUsers (assignee/mention picker source)

**Files:**
- Create: `controlplane/internal/storage/team_collab.go` (new file: all collaboration storage lives here)
- Test: `controlplane/internal/storage/team_collab_test.go`

- [ ] **Step 1: Write the failing test**

```go
package storage

import (
	"context"
	"testing"
)

type teamCollabStoreTest struct {
	store *Store
}

// Uses the package-level test helper that opens the in-memory/docker store if
// available; skip cleanly when no DB is configured (same as existing storage
// tests when neither testcontainers nor a DATABASE_URL is present).
func TestListTenantUsersMatchingQuery(t *testing.T) {
	// Skipped in unit mode (no DB); these live behind the same _integration_
	// guard as other Store tests in this package.
	if testing.Short() {
		t.Skip("requires database")
	}
	// Dataset: user A "Ada CISO" (email ada@x), user B "Bob Ops" — both in
	// tenant T with role investigator; user C viewer-only.
	// Assert: ListTenantUsers(T, "ada") returns Ada only; viewers excluded.
}
```

Note: this package's existing tests (e.g. `rbac_test.go`) use real-PG helpers with a `t.Skip` path — mirror the file's existing DB-availability helper instead of writing a bespoke one. The concrete assertion query uses the SQL below.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./controlplane/internal/storage/ -run TestListTenantUsersMatchingQuery -short`
Expected: FAIL — compile error, `ListTenantUsers` not defined.

- [ ] **Step 3: Implement**

Add to `controlplane/internal/storage/team_collab.go`:

```go
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// TeamUser is the tenant-scoped member used by the assignee picker and
// mention autosuggest.
type TeamUser struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email,omitempty"`
}

// ListTenantUsers returns tenant members holding investigator/operator/admin
// roles (tenant-scoped or org-global), deduplicated, matched textually on
// prefix, limited to `limit` (<= 0 means 50).
func (s *Store) ListTenantUsers(ctx context.Context, tenantID uuid.UUID, query string, limit int) ([]TeamUser, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	pattern := "%" + escapeLike(strings.TrimSpace(query)) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT u.id, u.display_name, u.email
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r        ON r.id = ur.role_id
		WHERE (ur.tenant_id = $1 OR ur.tenant_id IS NULL)
		  AND r.name IN ('investigator', 'operator', 'admin')
		  AND (LOWER(COALESCE(u.display_name, '')) LIKE $2
		       OR LOWER(COALESCE(u.email, '')) LIKE $2)
		ORDER BY LOWER(COALESCE(u.display_name, u.email, u.id::text))
		LIMIT $3
	`, tenantID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("query tenant users: %w", err)
	}
	defer rows.Close()

	out := make([]TeamUser, 0, limit)
	for rows.Next() {
		var u TeamUser
		var name, email sql.NullString
		if err := rows.Scan(&u.ID, &name, &email); err != nil {
			return nil, fmt.Errorf("scan tenant user: %w", err)
		}
		u.Name = strings.TrimSpace(name.String)
		u.Email = strings.TrimSpace(email.String)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant users: %w", err)
	}
	return out, nil
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./controlplane/internal/storage/ -run TestListTenantUsersMatchingQuery`
Expected: PASS (when a DB is configured).

- [ ] **Step 5: Commit**

```bash
git add controlplane/internal/storage/team_collab.go controlplane/internal/storage/team_collab_test.go
git commit -m "feat: add tenant-scoped team member listing for pickers"
```

---

### Task 3: Storage — Notification insert/list/count/read

**Files:**
- Modify: `controlplane/internal/storage/team_collab.go`
- Test: `controlplane/internal/storage/team_collab_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestNotificationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	// Insert two notifications for user R in tenant T (kinds case_assigned,
	// case_mentioned). Assert: List returns 2 total newest-first; unread count
	// is 2; mark-read one -> count 1; MarkAll -> count 0; marking another
	// tenant's notification read is rejected (no rows affected).
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./controlplane/internal/storage/ -run TestNotificationLifecycle -short`
Expected: FAIL — `CreateNotification` undefined.

- [ ] **Step 3: Implement (append to team_collab.go)**

```go
// Notification is one inbox row for a recipient.
type Notification struct {
	ID         uuid.UUID  `json:"id"`
	TenantID   uuid.UUID  `json:"tenant_id"`
	RecipientID uuid.UUID `json:"recipient_id"`
	ActorID    uuid.UUID  `json:"actor_id,omitempty"`
	ActorName  string     `json:"actor_name,omitempty"`
	Kind       string     `json:"kind"`
	CaseID     uuid.UUID  `json:"case_id"`
	CaseTitle  string     `json:"case_title"`
	ReadAt     *time.Time `json:"read_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// CreateNotificationParams describes a notification to persist.
type CreateNotificationParams struct {
	TenantID    uuid.UUID
	RecipientID uuid.UUID
	ActorID     uuid.UUID
	Kind        string
	CaseID      uuid.UUID
	CaseTitle   string
}

// notificationSelect is the shared column list for reads.
const notificationSelect = `id, tenant_id, recipient_id, actor_id, kind, case_id, case_title, read_at, created_at`

func (s *Store) CreateNotification(ctx context.Context, p CreateNotificationParams) (*Notification, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO notifications (tenant_id, recipient_id, actor_id, kind, case_id, case_title)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+notificationSelect,
		p.TenantID, p.RecipientID, nullableUUID(p.ActorID), p.Kind, p.CaseID, p.CaseTitle)
	var n Notification
	var actorID, readAt sql.NullString
	if err := row.Scan(&n.ID, &n.TenantID, &n.RecipientID, &actorID, &n.Kind, &n.CaseID, &n.CaseTitle, &readAt, &n.CreatedAt); err != nil {
		return nil, fmt.Errorf("create notification: %w", err)
	}
	if actorID.Valid {
		n.ActorID, _ = uuid.Parse(actorID.String)
	}
	if readAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, readAt.String); err == nil {
			n.ReadAt = &t
		}
	}
	return &n, nil
}

type NotificationFilter struct {
	TenantID    uuid.UUID
	RecipientID uuid.UUID
	UnreadOnly  bool
}

func (s *Store) ListNotifications(ctx context.Context, filter NotificationFilter, limit, offset int) ([]Notification, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	conds := []string{"tenant_id = $1", "recipient_id = $2"}
	if filter.UnreadOnly {
		conds = append(conds, "read_at IS NULL")
	}
	where := strings.Join(conds, " AND ")

	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE `+where, filter.TenantID, filter.RecipientID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT %s FROM notifications
		WHERE %s
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4`, notificationSelect, where),
		filter.TenantID, filter.RecipientID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	out := make([]Notification, 0, limit)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate notifications: %w", err)
	}
	return out, total, nil
}

func (s *Store) CountUnreadNotifications(ctx context.Context, tenantID, recipientID uuid.UUID) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM notifications WHERE tenant_id = $1 AND recipient_id = $2 AND read_at IS NULL`,
		tenantID, recipientID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

// MarkNotificationRead marks a single notification read; only the
// recipient's own rows in the tenant are touched.
func (s *Store) MarkNotificationRead(ctx context.Context, tenantID, recipientID, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if _, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE id = $1 AND tenant_id = $2 AND recipient_id = $3 AND read_at IS NULL`,
		id, tenantID, recipientID); err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	return nil
}

func (s *Store) MarkAllNotificationsRead(ctx context.Context, tenantID, recipientID uuid.UUID) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE notifications SET read_at = NOW() WHERE tenant_id = $1 AND recipient_id = $2 AND read_at IS NULL`,
		tenantID, recipientID)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

type notificationScanner interface {
	Scan(dest ...any) error
}

func scanNotification(row notificationScanner) (Notification, error) {
	var n Notification
	var actorID, readAt sql.NullString
	if err := row.Scan(&n.ID, &n.TenantID, &n.RecipientID, &actorID, &n.Kind, &n.CaseID, &n.CaseTitle, &readAt, &n.CreatedAt); err != nil {
		return n, fmt.Errorf("scan notification: %w", err)
	}
	if actorID.Valid {
		n.ActorID, _ = uuid.Parse(actorID.String)
	}
	if readAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, readAt.String); err == nil {
			n.ReadAt = &t
		}
	}
	return n, nil
}
```

Add `"time"` to the imports in `team_collab.go`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./controlplane/internal/storage/ -run TestNotificationLifecycle`
Expected: PASS (with a configured DB), skips otherwise.

- [ ] **Step 5: Run gofmt/vet**

Run: `gofmt -w controlplane/internal/storage/team_collab.go; go vet ./controlplane/internal/storage/`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/storage/team_collab.go controlplane/internal/storage/team_collab_test.go
git commit -m "feat: add notification inbox storage"
```

---

### Task 4: Storage — Case assignee + mentions read

**Files:**
- Modify: `controlplane/internal/storage/team_collab.go`
- Modify: `controlplane/internal/storage/ai_operator.go` (assignee_id on AIInvestigation, create/scan/list)
- Test: `controlplane/internal/storage/team_collab_test.go`

- [ ] **Step 1: Write the failing tests**

```go
func TestAssignCaseOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	// Case C in tenant T. AssignCase(T, C, userA, userB, now) -> CaseAssignee
	// is userA. Re-assign to userA (no-op) keeps assigned_by userB. Unassign
	// (uuid.Nil) -> CaseAssignee is uuid.Nil.
}

func TestCaseMentionedUsers(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	// Two audit logs action soc.case.note.add for case C, metadata mentions
	// [A, B] and [B]. CaseMentionedUsers returns [A, B] deduplicated.
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./controlplane/internal/storage/ -run 'TestAssignCaseOwnership|TestCaseMentionedUsers' -short`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement storage methods (append to team_collab.go)**

```go
func (s *Store) CaseAssignee(ctx context.Context, tenantID, caseID uuid.UUID) (uuid.UUID, error) {
	if s.db == nil {
		return uuid.Nil, errors.New("store database not initialized")
	}
	var assignee sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT assignee_id FROM ai_investigations WHERE id = $1 AND tenant_id = $2`,
		caseID, tenantID).Scan(&assignee); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, sql.ErrNoRows
		}
		return uuid.Nil, fmt.Errorf("query case assignee: %w", err)
	}
	if !assignee.Valid {
		return uuid.Nil, nil
	}
	parsed, err := uuid.Parse(assignee.String)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse case assignee: %w", err)
	}
	return parsed, nil
}

// AssignCase sets (or clears, when assigneeID is uuid.Nil) the case owner.
func (s *Store) AssignCase(ctx context.Context, tenantID, caseID, assigneeID, assignedBy uuid.UUID, now time.Time) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if assigneeID == uuid.Nil {
		res, err := s.db.ExecContext(ctx,
			`UPDATE ai_investigations
			 SET assignee_id = NULL, assigned_by = $3, assigned_at = $4
			 WHERE id = $1 AND tenant_id = $2`,
			caseID, tenantID, nullableUUID(assignedBy), now)
		if err != nil {
			return fmt.Errorf("clear case assignee: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
		return nil
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE ai_investigations
		 SET assignee_id = $3, assigned_by = $4, assigned_at = $5
		 WHERE id = $1 AND tenant_id = $2`,
		caseID, tenantID, assigneeID, nullableUUID(assignedBy), now)
	if err != nil {
		return fmt.Errorf("assign case: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CaseMentionedUsers reads distinct mentioned user ids from note metadata.
func (s *Store) CaseMentionedUsers(ctx context.Context, tenantID, caseID uuid.UUID) ([]uuid.UUID, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT m.value
		FROM audit_logs al
		CROSS JOIN LATERAL jsonb_array_elements_text(COALESCE(al.metadata->'mentions', '[]'::jsonb)) AS m(value)
		WHERE al.tenant_id = $1 AND al.action = 'soc.case.note.add'
		  AND al.resource_type = 'ai_investigation' AND al.resource_id = $2
		  AND m.value IS NOT NULL AND m.value <> ''
	`, tenantID, caseID.String())
	if err != nil {
		return nil, fmt.Errorf("query case mentions: %w", err)
	}
	defer rows.Close()

	seen := map[uuid.UUID]struct{}{}
	var out []uuid.UUID
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan case mention: %w", err)
		}
		if id, perr := uuid.Parse(value); perr == nil {
			if _, ok := seen[id]; !ok {
				seen[id] = struct{}{}
				out = append(out, id)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate case mentions: %w", err)
	}
	return out, nil
}
```

Also expose the assignee on the case row so responses don't need a second read per case. In `controlplane/internal/storage/ai_operator.go`:

- Add field to `AIInvestigation` (after `CreatedBy`):
```go
	AssigneeID uuid.NullUUID `json:"assignee_id,omitempty"`
```
- Add a local var `assigneeID sql.NullString` in `scanAIInvestigation`, append `&assigneeID` to the Scan list in the same position as the column, and after scan:
```go
	if assigneeID.Valid {
		if parsed, err := uuid.Parse(assigneeID.String); err == nil {
			out.AssigneeID = uuid.NullUUID{UUID: parsed, Valid: true}
		}
	}
```
- Add `assignee_id` to the RETURNING list of `CreateAIInvestigation`, and to the SELECT lists of `GetAIInvestigation` and `ListAIInvestigations` (immediately after `created_by` in each, matching the Scan order). `CreateAIInvestigation` does not insert the column (new cases start unassigned).

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./controlplane/internal/storage/ -run 'TestAssignCaseOwnership|TestCaseMentionedUsers'`
Expected: PASS (with DB), skips otherwise.

- [ ] **Step 5: gofmt + build**

Run: `gofmt -w controlplane/internal/storage/team_collab.go controlplane/internal/storage/ai_operator.go; go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add controlplane/internal/storage/team_collab.go controlplane/internal/storage/ai_operator.go controlplane/internal/storage/team_collab_test.go
git commit -m "feat: add case assignee + mentions read storage"
```

---

### Task 5: Server — interface, routes, team/users + notifications handlers

**Files:**
- Modify: `controlplane/internal/server/server.go` (Store interface + routes)
- Create: `controlplane/internal/server/team_collab.go` (team.users + notifications handlers)
- Modify: `controlplane/internal/server/server_test.go` (fakeStore stubs)

- [ ] **Step 1: Write failing handler tests**

Append to `server_test.go` (using the file's existing `newTestEnv`/handler harness — see the team_activity tests added earlier for the exact fixture used to exercise `writeJSON` responses):

```go
func TestHandleListTeamUsersScopesToTenantAndRoles(t *testing.T) {
	// GET /api/v1/team/users?tenant_id=T&q=ada with a fakeStore whose
	// ListTenantUsers returns Ada -> 200 with Ada, and list filtered by query.
}

func TestHandleNotificationsReadOnlyOwnRows(t *testing.T) {
	// POST /api/v1/notifications/{id}/read for someone else's notification
	// returns 200 but MarkNotificationRead is never invoked with a foreign id
	// (assert via fakeStore call recording).
}
```

- [ ] **Step 2: Run to verify they fail**

Run (from `C:\dev\control_one_ws3`): `go test ./controlplane/internal/server/ -run 'TestHandleListTeamUsers|TestHandleNotifications'`
Expected: FAIL — handlers undefined.

- [ ] **Step 3: Add Store interface methods**

In `server.go`, in the `Store` interface (after the `GetTeamCoverageGaps` line added in the previous feature), add:

```go
	// SOC case collaboration
	AssignCase(ctx context.Context, tenantID, caseID, assigneeID, assignedBy uuid.UUID, now time.Time) error
	CaseAssignee(ctx context.Context, tenantID, caseID uuid.UUID) (uuid.UUID, error)
	CaseMentionedUsers(ctx context.Context, tenantID, caseID uuid.UUID) ([]uuid.UUID, error)
	ListTenantUsers(ctx context.Context, tenantID uuid.UUID, query string, limit int) ([]storage.TeamUser, error)
	CreateNotification(ctx context.Context, p storage.CreateNotificationParams) (*storage.Notification, error)
	ListNotifications(ctx context.Context, filter storage.NotificationFilter, limit, offset int) ([]storage.Notification, int, error)
	CountUnreadNotifications(ctx context.Context, tenantID, recipientID uuid.UUID) (int, error)
	MarkNotificationRead(ctx context.Context, tenantID, recipientID, id uuid.UUID) error
	MarkAllNotificationsRead(ctx context.Context, tenantID, recipientID uuid.UUID) (int64, error)
```

- [ ] **Step 4: Add fakeStore stubs**

In `server_test.go`, extend `fakeStore` with:

```go
	teamUsers        []storage.TeamUser
	notifications    []storage.Notification
	unreadCount      int
	markReadCalls    []uuid.UUID
	assignCaseCalls  []storage.AssignCaseRecord // see step note
```

and implement the interface methods with realistic behavior (return stored slices; `MarkNotificationRead` appends the id and returns nil). Define `storage.AssignCaseRecord` in the storage package if needed, or record on the fake with plain struct fields (fake-only type is fine).

- [ ] **Step 5: Implement handlers**

Create `controlplane/internal/server/team_collab.go`:

```go
package server

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type teamMemberResponse struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// handleTeamUsers backs the assignee/mention pickers.
// GET /api/v1/team/users?tenant_id&q
func (s *Server) handleTeamUsers(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	users, err := s.store.ListTenantUsers(r.Context(), tenantID, query, 25)
	if err != nil {
		s.logger.Warn("list team users", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]teamMemberResponse, 0, len(users))
	for _, u := range users {
		out = append(out, teamMemberResponse{ID: u.ID.String(), Name: u.Name, Email: u.Email})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// serveNotifications routes /api/v1/notifications and /api/v1/notifications/.
func (s *Server) serveNotifications(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.requireTeamTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	recipient, ok := s.userIDForPrincipal(r.Context(), principal)
	if !ok {
		http.Error(w, "principal has no local user account", http.StatusForbidden)
		return
	}

	path := r.URL.Path
	switch {
	case path == "/api/v1/notifications" && r.Method == http.MethodGet:
		s.handleListNotifications(w, r, tenantID, recipient)
	case path == "/api/v1/notifications/unread-count" && r.Method == http.MethodGet:
		s.handleUnreadNotificationsCount(w, r, tenantID, recipient)
	case path == "/api/v1/notifications/read-all" && r.Method == http.MethodPost:
		_, err := s.store.MarkAllNotificationsRead(r.Context(), tenantID, recipient)
		if err != nil {
			s.logger.Warn("mark all notifications read", zap.Error(err),
				zap.String("tenant_id", tenantID.String()), zap.String("recipient", recipient.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"marked_read": true})
	case strings.HasPrefix(path, "/api/v1/notifications/") && r.Method == http.MethodPost:
		idStr := strings.TrimPrefix(path, "/api/v1/notifications/")
		id, err := uuid.Parse(idStr)
		if err != nil {
			http.Error(w, "invalid notification id", http.StatusBadRequest)
			return
		}
		if err := s.store.MarkNotificationRead(r.Context(), tenantID, recipient, id); err != nil {
			s.logger.Warn("mark notification read", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"read": true})
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request, tenantID, recipient uuid.UUID) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	unreadOnly := parseBoolQuery(r.URL.Query().Get("unread_only"))
	items, total, err := s.store.ListNotifications(r.Context(), storage.NotificationFilter{
		TenantID:    tenantID,
		RecipientID: recipient,
		UnreadOnly:  unreadOnly,
	}, limit, offset)
	if err != nil {
		s.logger.Warn("list notifications", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.hydrateNotificationActorNames(r.Context(), items)
	writeJSON(w, http.StatusOK, paginatedResponse[storage.Notification]{
		Data:       items,
		Pagination: newPaginationMeta(total, limit, offset, len(items)),
	})
}

func (s *Server) handleUnreadNotificationsCount(w http.ResponseWriter, r *http.Request, tenantID, recipient uuid.UUID) {
	count, err := s.store.CountUnreadNotifications(r.Context(), tenantID, recipient)
	if err != nil {
		s.logger.Warn("unread notifications count", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"unread": count})
}

// hydrateNotificationActorNames resolves actor names in place (best effort).
func (s *Server) hydrateNotificationActorNames(ctx context.Context, items []storage.Notification) {
	ids := make([]uuid.UUID, 0, len(items))
	index := map[uuid.UUID]int{}
	for i, n := range items {
		if n.ActorID == uuid.Nil {
			continue
		}
		if _, seen := index[n.ActorID]; seen {
			continue
		}
		index[n.ActorID] = i
		ids = append(ids, n.ActorID)
	}
	if len(ids) == 0 {
		return
	}
	names, err := s.store.lookupUserDisplayNames(ctx, ids)
	if err != nil {
		s.logger.Warn("resolve notification actor names", zap.Error(err))
		return
	}
	for _, n := range items {
		n.ActorName = names[n.ActorID]
	}
}
```

`hydrateNotificationActorNames` cannot mutate the range copy — instead iterate by index:

```go
	for i := range items {
		items[i].ActorName = names[items[i].ActorID]
	}
```

(The plan's earlier range-copy loop is intentionally replaced by the indexed version.)

- [ ] **Step 6: Wire routes**

In `server.go`, near the team activity routes (after the `gaps` route), add:

```go
	s.baseRouter.HandleFunc("/api/v1/team/users", s.handleTeamUsers)
	s.baseRouter.HandleFunc("/api/v1/notifications", s.serveNotifications)
	s.baseRouter.HandleFunc("/api/v1/notifications/", s.serveNotifications)
```

- [ ] **Step 7: Add `userIDForPrincipal` (helper used by notifications)**

`userIDForPrincipal` resolves the authenticated user's local id (mirroring the idiomatic lookup seen in `soc_cases.go`):

```go
// userIDForPrincipal resolves the principal's local user id, or returns
// false when no matching local account exists.
func (s *Server) userIDForPrincipal(ctx context.Context, principal *auth.Principal) (uuid.UUID, bool) {
	if principal == nil || strings.TrimSpace(principal.Subject) == "" {
		return uuid.Nil, false
	}
	user, err := s.store.GetUserByExternalID(ctx, principal.Subject)
	if err != nil || user == nil {
		return uuid.Nil, false
	}
	return user.ID, true
}
```

- [ ] **Step 8: Run handler tests + full package**

Run: `go test ./controlplane/internal/server/ -run 'TestHandleListTeamUsers|TestHandleNotifications'`
Expected: PASS. Then `go build ./...; go vet ./controlplane/internal/server/`
Expected: clean.

- [ ] **Step 9: Commit**

```bash
git add controlplane/internal/server/server.go controlplane/internal/server/team_collab.go controlplane/internal/server/server_test.go
git commit -m "feat: add team users + notifications inbox API"
```

---

### Task 6: Server — assign endpoint, note mentions, response hydration

**Files:**
- Modify: `controlplane/internal/server/soc_cases.go`
- Modify: `controlplane/internal/server/team_collab.go`
- Modify: `controlplane/internal/server/server_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestHandleAssignCaseValidatesAssigneeInTenant(t *testing.T) {
	// POST /api/v1/soc/cases/{id}/assign with an assignee not in the tenant's
	// user list -> 400.
}

func TestHandleCreateSOCCaseNoteStoresMentions(t *testing.T) {
	// POST .../notes with mentions [valid, foreign] -> 400 (foreign rejected).
	// Generating fakeStore-observable CreateAuditLog metadata asserts
	// metadata["mentions"] == [valid].
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./controlplane/internal/server/ -run 'TestHandleAssignCase|TestHandleCreateSOCCaseNoteStoresMentions'`
Expected: FAIL.

- [ ] **Step 3: Add assign fields + hydration**

Extend the response types in `soc_cases.go`:

```go
type socCaseUserRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
```

Add to `socCaseResponse` (after `Notes`):
```go
	Assignee       *socCaseUserRef   `json:"assignee,omitempty"`
	MentionedUsers []socCaseUserRef  `json:"mentioned_users,omitempty"`
```

Add the hydration helper in `team_collab.go`:

```go
// hydrateCaseCollaboration fills assignee + mentioned_users on the response.
func (s *Server) hydrateCaseCollaboration(ctx context.Context, tenantID uuid.UUID, row storage.AIInvestigation, resp *socCaseResponse) {
	ids := make([]uuid.UUID, 0, 9)
	if row.AssigneeID.Valid && row.AssigneeID.UUID != uuid.Nil {
		ids = append(ids, row.AssigneeID.UUID)
	}
	mentioned, err := s.store.CaseMentionedUsers(ctx, tenantID, row.ID)
	if err != nil {
		s.logger.Warn("case mentioned users", zap.Error(err), zap.String("case_id", row.ID.String()))
	} else {
		for i, id := range mentioned {
			if i >= 8 {
				break
			}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	names, err := s.store.lookupUserDisplayNames(ctx, ids)
	if err != nil {
		s.logger.Warn("case collab names", zap.Error(err))
		return
	}
	if row.AssigneeID.Valid && row.AssigneeID.UUID != uuid.Nil {
		id := row.AssigneeID.UUID
		resp.Assignee = &socCaseUserRef{ID: id.String(), Name: names[id]}
	}
	for idx, id := range mentioned {
		if idx >= 8 {
			break
		}
		resp.MentionedUsers = append(resp.MentionedUsers, socCaseUserRef{ID: id.String(), Name: names[id]})
	}
}
```

Wire it into every place that builds a `socCaseResponse` from a row: in
`soc_cases.go`, locate the handlers that call `newSOCCaseResponse(...)` (the
collection list, the single-case detail, and the create-from-alert response)
and after each response is built add:
```go
		s.hydrateCaseCollaboration(r.Context(), tenantID, row, &resp)
```
using the handler's actual `tenantID`/`row` identifiers.

- [ ] **Step 4: Add assign handler**

In `soc_cases.go`, add the assign endpoint inside the case path dispatch
(`handleSOCCaseSubroutes`) so `.../assign` POST routes here:

```go
// handleAssignSOCase sets or clears the case owner and notifies the assignee.
func (s *Server) handleAssignSOCase(w http.ResponseWriter, r *http.Request, principal *auth.Principal, tenantID uuid.UUID, row storage.AIInvestigation) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		AssigneeID *uuid.UUID `json:"assignee_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid assign payload", http.StatusBadRequest)
		return
	}

	newAssignee := uuid.Nil
	if req.AssigneeID != nil {
		newAssignee = *req.AssigneeID
		allowed, err := s.store.ListTenantUsers(r.Context(), tenantID, "", 1000)
		if err != nil {
			s.logger.Warn("assign case list users", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		valid := false
		for _, u := range allowed {
			if u.ID == newAssignee {
				valid = true
				break
			}
		}
		if !valid {
			http.Error(w, "assignee must be a team member in this tenant", http.StatusBadRequest)
			return
		}
	}

	actorID, _ := s.userIDForPrincipal(r.Context(), principal)
	before, err := s.store.CaseAssignee(r.Context(), tenantID, row.ID)
	if err != nil {
		s.logger.Warn("assign case read assignee", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if before != newAssignee {
		if err := s.store.AssignCase(r.Context(), tenantID, row.ID, newAssignee, actorID, time.Now().UTC()); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.Error(w, "case not found or not in tenant", http.StatusNotFound)
				return
			}
			s.logger.Warn("assign case", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}
	s.recordAudit(r.Context(), principal, tenantID, "soc.case.assign", "ai_investigation", row.ID.String(), map[string]any{
		"assignee_id":  nullableUUIDString(newAssignee),
		"assigned_by":  nullableUUIDString(actorID),
		"case_id":      row.ID.String(),
		"case_source":  "ai_investigation",
		"guardrails":   []string{"tenant_scoped", "owner_only"},
		"allowed_users": len(allowedSafe(tenantID, newAssignee)),
	})

	if newAssignee != uuid.Nil && newAssignee != actorID && before != newAssignee {
		title := firstNonEmptyString(cleanSOCCaseText(row.Summary), strings.TrimSpace(row.TriggerEventType), "SOC case")
		if _, nerr := s.store.CreateNotification(r.Context(), storage.CreateNotificationParams{
			TenantID:    tenantID,
			RecipientID: newAssignee,
			ActorID:     actorID,
			Kind:        "case_assigned",
			CaseID:      row.ID,
			CaseTitle:   title,
		}); nerr != nil {
			s.logger.Warn("create assignment notification", zap.Error(nerr))
		}
	}

	row.AssigneeID = uuid.NullUUID{}
	if newAssignee != uuid.Nil {
		row.AssigneeID = uuid.NullUUID{UUID: newAssignee, Valid: true}
	}
	resp := newSOCCaseResponse(row)
	s.hydrateCaseCollaboration(r.Context(), tenantID, row, &resp)
	writeJSON(w, http.StatusOK, resp)
}
```

Add small helpers used above, also in `team_collab.go`:

```go
func nullableUUIDString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

// allowedSafe is a validation helper that mirrors the tenant membership
// check; returns the assigned id when it is valid (or nil) so the audit log
// stays truthful. Replace with the validated boolean during implementation.
```

**Planning note:** the `allowed_users`/`allowedSafe` construction above is
conflated — simplify before committing: drop the `allowed_users` audit field
and `allowedSafe`; the 400 guard already ran. The audit metadata should be:

```go
	s.recordAudit(r.Context(), principal, tenantID, "soc.case.assign", "ai_investigation", row.ID.String(), map[string]any{
		"assignee_id": nullableUUIDString(newAssignee),
		"assigned_by": nullableUUIDString(actorID),
		"case_id":     row.ID.String(),
		"case_source": "ai_investigation",
		"guardrails":  []string{"tenant_scoped", "owner_only"},
	})
```

- [ ] **Step 5: Extend note handler with mentions**

In `soc_cases.go`, `handleCreateSOCCaseNote`:

Add to the request struct:
```go
		Mentions []string `json:"mentions"`
```

After citations validation, validate mentions against tenant users:
```go
	mentions := sanitizeStringSlice(req.Mentions, 8)
	if len(mentions) > 0 {
		allowedUsers, lerr := s.store.ListTenantUsers(r.Context(), row.TenantID, "", 1000)
		if lerr != nil {
			s.logger.Warn("note mention lookup", zap.Error(lerr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		validIDs := map[string]struct{}{}
		for _, u := range allowedUsers {
			validIDs[u.ID.String()] = struct{}{}
		}
		for _, id := range mentions {
			if _, ok := validIDs[id]; !ok {
				http.Error(w, "mention must reference a team member in this tenant", http.StatusBadRequest)
				return
			}
		}
	}
	if len(mentions) > 0 {
		metadata["mentions"] = mentions
	}
```

After the note persists, emit mention notifications (skip the author):
```go
	actorID, _ := s.userIDForPrincipal(r.Context(), principal)
	title := firstNonEmptyString(cleanSOCCaseText(row.Summary), strings.TrimSpace(row.TriggerEventType), "SOC case")
	for _, idStr := range mentions {
		mentionedID, perr := uuid.Parse(idStr)
		if perr != nil || mentionedID == actorID {
			continue
		}
		if _, nerr := s.store.CreateNotification(r.Context(), storage.CreateNotificationParams{
			TenantID:    row.TenantID,
			RecipientID: mentionedID,
			ActorID:     actorID,
			Kind:        "case_mentioned",
			CaseID:      row.ID,
			CaseTitle:   title,
		}); nerr != nil {
			s.logger.Warn("create mention notification", zap.Error(nerr))
		}
	}
```

Surface mentions on the note response — add to `socCaseNoteResponse`:
```go
	Mentions []string `json:"mentions,omitempty"`
```
and populate in `socCaseNoteFromAudit`:
```go
	resp.Mentions = stringSliceFromAny(log.Metadata["mentions"])
```

Name-list needs for response users are covered by `hydrateCaseCollaboration` reusing `lookupUserDisplayNames`.

- [ ] **Step 6: Route the assign path + imports**

In `handleSOCCaseSubroutes` (soc_cases.go) add a branch: when the last path
segment equals `assign`, call `s.handleAssignSOCase(...)`. Verify `sql` and
`errors` are imported in `soc_cases.go`; add them if missing.

- [ ] **Step 7: Run tests + build**

Run: `go test ./controlplane/internal/server/ -run 'TestHandleAssignCase|TestHandleCreateSOCCaseNoteStoresMentions'`
Expected: PASS. Then `gofmt -w controlplane/internal/server/soc_cases.go controlplane/internal/server/team_collab.go; go build ./...; go vet ./controlplane/internal/server/`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add controlplane/internal/server/soc_cases.go controlplane/internal/server/team_collab.go controlplane/internal/server/server_test.go
git commit -m "feat: add case assignment, note mentions, and notification emission"
```

---

### Task 7: UI — api.ts client additions

**Files:**
- Modify: `ui/src/lib/api.ts`

- [ ] **Step 1: Add types**

After `SOCCaseNote` (near line 2045):

```ts
export interface SOCUserRef {
  id: string;
  name: string;
}

export interface TeamUser {
  id: string;
  name: string;
  email?: string;
}

export interface Notification {
  id: string;
  tenant_id: string;
  recipient_id: string;
  actor_id?: string;
  actor_name?: string;
  kind: 'case_assigned' | 'case_mentioned';
  case_id: string;
  case_title: string;
  read_at?: string;
  created_at: string;
}
```

Extend `SOCCaseNote` with:
```ts
  mentions?: string[];
```
Extend `SOCCase` with:
```ts
  assignee?: SOCUserRef | null;
  mentioned_users?: SOCUserRef[];
```

- [ ] **Step 2: Add methods** (after `addSOCCaseNote`, line ~2766)

```ts
  async assignSOCCase(caseId: string, tenantId: string, payload: { assignee_id: string | null }): Promise<SOCCase> {
    return this.request<SOCCase>(`/api/v1/soc/cases/${caseId}/assign`, {
      method: 'POST',
      body: JSON.stringify(payload),
      headers: { 'x-tenant-id': tenantId },
    });
  }

  async getTeamUsers(tenantId: string, query = ''): Promise<TeamUser[]> {
    const params = new URLSearchParams({ tenant_id: tenantId });
    if (query) params.set('q', query);
    const res = await this.request<{ users: TeamUser[] }>(`/api/v1/team/users?${params}`);
    return res.users ?? [];
  }

  async listNotifications(tenantId: string, opts: { unreadOnly?: boolean; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<Notification>> {
    const params = new URLSearchParams({ tenant_id: tenantId });
    if (opts.unreadOnly) params.set('unread_only', '1');
    if (opts.limit) params.set('limit', String(opts.limit));
    if (opts.offset) params.set('offset', String(opts.offset));
    return this.request<PaginatedResponse<Notification>>(`/api/v1/notifications?${params}`);
  }

  async getUnreadNotificationsCount(tenantId: string): Promise<number> {
    const res = await this.request<{ unread: number }>(`/api/v1/notifications/unread-count?tenant_id=${tenantId}`);
    return res.unread ?? 0;
  }

  async markNotificationRead(notificationId: string, tenantId: string): Promise<void> {
    await this.request(`/api/v1/notifications/${notificationId}/read?tenant_id=${tenantId}`, { method: 'POST' });
  }

  async markAllNotificationsRead(tenantId: string): Promise<void> {
    await this.request(`/api/v1/notifications/read-all?tenant_id=${tenantId}`, { method: 'POST' });
  }
```

Check the existing client for how auth headers are attached; mirror the other SOC calls (the file uses a shared `request` splice — follow the shape used by `addSOCCaseNote`, including how it sends tenant/auth headers).

Amend `addSOCCaseNote` to pass mentions:
```ts
  async addSOCCaseNote(caseId: string, tenantId: string, payload: { note: string; citations?: string[]; mentions?: string[] }): Promise<SOCCaseNote> {
    return this.request<SOCCaseNote>(`/api/v1/soc/cases/${caseId}/notes?tenant_id=${tenantId}`, {
      method: 'POST',
      body: JSON.stringify(payload),
      headers: { 'x-tenant-id': tenantId },
    });
  }
```

- [ ] **Step 3: Verify**

Run (from `ui/`): `npx tsc --noEmit`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add ui/src/lib/api.ts
git commit -m "feat: add collaboration + notifications api client methods"
```

---

### Task 8: UI — AssigneePicker component

**Files:**
- Create: `ui/src/components/team/AssigneePicker.tsx`
- Test: `ui/src/components/team/AssigneePicker.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { AssigneePicker } from './AssigneePicker';

const OPTIONS = [
  { id: 'u1', name: 'Ada CISO' },
  { id: 'u2', name: 'Bob Ops' },
];

describe('AssigneePicker', () => {
  it('shows the current assignee or Unassigned', () => {
    render(<AssigneePicker value={{ id: 'u1', name: 'Ada CISO' }} options={OPTIONS} onChange={vi.fn()} />);
    expect(screen.getByText('Ada CISO')).toBeInTheDocument();
  });

  it('clears to Unassigned', async () => {
    const user = userEvent.setup();
    render(<AssigneePicker value={{ id: 'u1', name: 'Ada CISO' }} options={OPTIONS} onChange={vi.fn()} />);
    await user.click(screen.getByRole('button', { name: /clear assignee/i }));
    expect(screen.getByText('Unassigned')).toBeInTheDocument();
  });

  it('invokes onChange with the picked user', async () => {
    const user = userEvent.setup();
    const onChange = vi.fn();
    render(<AssigneePicker value={null} options={OPTIONS} onChange={onChange} />);
    await user.click(screen.getByRole('button', { name: /assign/i }));
    await user.click(await screen.findByRole('option', { name: 'Bob Ops' }));
    expect(onChange).toHaveBeenCalledWith({ id: 'u2', name: 'Bob Ops' });
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `npx vitest run src/components/team/AssigneePicker.test.tsx`
Expected: FAIL — component missing.

- [ ] **Step 3: Implement**

```tsx
import { useState } from 'react';
import { UserRound, X } from 'lucide-react';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command';
import type { TeamUser } from '@/lib/api';

export interface AssigneePickerProps {
  value: TeamUser | null;
  options: TeamUser[];
  onChange: (user: TeamUser | null) => void;
  disabled?: boolean;
}

export function AssigneePicker({ value, options, onChange, disabled }: AssigneePickerProps): JSX.Element {
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          disabled={disabled}
          className="inline-flex max-w-full items-center gap-1.5 rounded-full border border-border-subtle bg-elevated px-2 py-0.5 text-xs font-medium text-foreground hover:border-brand-500 disabled:cursor-not-allowed disabled:opacity-50"
          aria-label={value ? `Assignee: ${value.name}` : 'Assign owner'}
        >
          {value ? (
            <>
              <UserRound className="h-3 w-3 text-brand-400" aria-hidden />
              <span className="truncate">{value.name}</span>
              <button
                type="button"
                className="text-text-muted hover:text-text-secondary"
                aria-label="Clear assignee"
                onClick={(event) => {
                  event.stopPropagation();
                  onChange(null);
                }}
              >
                <X className="h-3 w-3" aria-hidden />
              </button>
            </>
          ) : (
            <span className="text-text-secondary">Assign owner</span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-56 p-0">
        <Command>
          <CommandInput placeholder="Search teammates…" autoFocus />
          <CommandList>
            <CommandEmpty>No teammates found.</CommandEmpty>
            <CommandGroup>
              {options.map((user) => (
                <CommandItem
                  key={user.id}
                  value={`${user.name} ${user.email ?? ''}`.toLowerCase()}
                  onSelect={() => {
                    onChange(user);
                    setOpen(false);
                  }}
                >
                  <span className="truncate">{user.name}</span>
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
```

Confirm `@/components/ui/command` exports `Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList` (it is the same module used by `GlobalSearch`/`TenantSwitcher`); align imports to what it actually exports.

- [ ] **Step 4: Run to verify it passes**

Run: `npx vitest run src/components/team/AssigneePicker.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ui/src/components/team/AssigneePicker.tsx ui/src/components/team/AssigneePicker.test.tsx
git commit -m "feat: add assignee picker component"
```

---

### Task 9: UI — Cases page (assignee, badges, mention composer)

**Files:**
- Modify: `ui/src/pages/Cases.tsx`
- Test: `ui/src/pages/Cases.test.tsx`

- [ ] **Step 1: Write failing tests** (follow the existing `Cases.test.tsx` mock pattern — spy on `useTenantModule.useTenant` + `useApiClientModule.useApiClient`)

```tsx
it('assigns an owner from the case header', async () => {
  // seeded case without assignee; open assignee picker, pick "Ada CISO";
  // assert api.assignSOCCase called with { assignee_id: 'u1' }.
});

it('shows mentioned user badges on the case header', async () => {
  // seeded case with mentioned_users [{id:'u1',name:'Ada CISO'}];
  // assert the badge with Ada's name is present.
});

it('adds mentions when posting a note with @ autosuggest', async () => {
  // type "@ad" in the note composer -> suggestion appears -> select it ->
  // chip visible -> submit -> api.addSOCCaseNote called with mentions: ['u1'].
});
```

- [ ] **Step 2: Run to verify they fail**

Run: `npx vitest run src/pages/Cases.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement**

- Fetch picker options: add a `teamUsers` state populated via `api.getTeamUsers(currentTenantId, '')` in the existing load effect (reload on refresh). Pass to the header + composer.
- In the case detail header (the selected-case summary region, near the status/severity tags), render:

```tsx
<div className="flex flex-wrap items-center gap-2">
  <AssigneePicker
    value={selectedCase.assignee ?? null}
    options={teamUsers}
    disabled={!currentTenantId || saveBusy}
    onChange={(user) => void assignOwner(user)}
  />
  {(selectedCase.mentioned_users ?? []).map((member) => (
    <span
      key={member.id}
      title="Mentioned in case"
      className="inline-flex items-center gap-1 rounded-full border border-border-subtle bg-elevated px-2 py-0.5 text-xs text-text-secondary"
    >
      <AtSign className="h-3 w-3 text-brand-400" aria-hidden />
      {member.name}
    </span>
  ))}
</div>
```

`assignOwner`:
```tsx
const assignOwner = async (user: TeamUser | null) => {
  if (!selectedCase || !currentTenantId) return;
  try {
    const updated = await api.assignSOCCase(selectedCase.case_id, currentTenantId, {
      assignee_id: user?.id ?? null,
    });
    setSelectedCase(updated);
    void refreshNotificationCount();
  } catch (err) {
    setCaseLoadError(errorMessage(err, 'Unable to update assignee.'));
  }
};
```

- Replace the plain `textarea` in `NotesPanel` with a `MentionComposer` (new component in the same file or `components/team/MentionComposer.tsx`) that:
  - holds `draft`, tracks `@`-triggered popover via the same `Command` primitives filtered by current `teamUsers`; on select inserts the raw `@Name` token + records the user id in a `mentions: string[]`, rendering chips above the textarea ("Will notify: Ada CISO ×").
  - calls the existing `addNote`-style submit passing `{ note, citations, mentions }`.
  - caps mentions at 8 with a status message.
- Extend the existing `addNote` handler to append `mentions` to the payload and reset it after save.

- [ ] **Step 4: Run to verify they pass**

Run: `npx vitest run src/pages/Cases.test.tsx`
Expected: PASS.

- [ ] **Step 5: Lint + typecheck**

Run (from `ui/`): `node scripts/run-eslint.cjs; npx tsc --noEmit`
Expected: exit 0 / clean.

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/Cases.tsx ui/src/pages/Cases.test.tsx ui/src/components/team/MentionComposer.tsx
git commit -m "feat: add case assignee, mention badges, and mention composer"
```

---

### Task 10: UI — Team page open-cases panel

**Files:**
- Modify: `ui/src/pages/TeamActivity.tsx`
- Test: `ui/src/pages/TeamActivity.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
it('lists open cases and assigns an owner inline', async () => {
  // Mock listSOCCases to return one open case; assert title renders and the
  // assignee picker can set an owner (assignSOCCase called).
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `npx vitest run --dir src/pages -t TeamActivity`
Expected: FAIL.

- [ ] **Step 3: Implement**

In `TeamActivity.tsx`:
- Add state: `openCases: PaginatedResponse<SOCCase> | null` and `teamUsers: TeamUser[]`.
- Load inside the existing `refresh`: `const [cases, users] = await Promise.all([api.listSOCCases({ tenantId: currentTenantId, status: 'open', limit: 25 }), api.getTeamUsers(currentTenantId, '')])`.
- Render below "Analyst workload" (using existing `Pagination` + `DataTable` imports):

```tsx
<Panel title="Open cases" eyebrow="Unassigned and owned cases awaiting action">
  <DataTable
    columns={openCaseColumns}
    rows={openCases?.data ?? []}
    loading={loading}
    rowKey={(row) => row.case_id}
    empty={<EmptyState title="No open cases" description="All cases for this tenant are closed." />}
  />
  {openCases && (
    <Pagination
      total={openCases.pagination.total}
      pageSize={openCases.pagination.limit}
      page={Math.floor(openCases.pagination.offset / openCases.pagination.limit) + 1}
      onPageChange={(p) => void loadOpenCases(p)}
    />
  )}
</Panel>
```

Define `openCaseColumns` as `ColumnDef<SOCCase>` with columns: severity `StatusTag`, title (Link to `/cases`), `created_at` (timeAgo), and an `assignee` action column that renders `AssigneePicker` (options from `teamUsers`, `onChange` → `api.assignSOCCase` then reload cases). Add `loadOpenCases(page)` that calls `listSOCCases` with the updated `offset`. Match the `api.listSOCCases` param shape (`ListSOCCasesParams`) used elsewhere.

- [ ] **Step 4: Run to verify it passes + full page suite**

Run: `npx vitest run src/pages/TeamActivity.test.tsx; npx vitest run --dir src/pages`
Expected: PASS (whole pages suite stays green).

- [ ] **Step 5: Lint + typecheck**

Run (from `ui/`): `node scripts/run-eslint.cjs; npx tsc --noEmit`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add ui/src/pages/TeamActivity.tsx ui/src/pages/TeamActivity.test.tsx
git commit -m "feat: add open cases panel with inline assignment to team page"
```

---

### Task 11: UI — Top bar notification bell

**Files:**
- Modify: `ui/src/components/shell/TopBar.tsx`
- Modify: `ui/src/components/shell/NavigationScope.test.tsx` (guards the TopBar — keep existing assertions passing)
- Create: `ui/src/components/shell/NotificationsBell.tsx`
- Test: `ui/src/components/shell/NotificationsBell.test.tsx`

- [ ] **Step 1: Write the failing test**

```tsx
describe('NotificationsBell', () => {
  it('shows unread badge and marks all read', async () => {
    // getUnreadNotificationsCount -> 2; badge "2" visible.
    // Open dropdown -> "Mark all read" -> markAllNotificationsRead called -> count refetches to 0.
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `npx vitest run src/components/shell/NotificationsBell.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement**

Create `NotificationsBell.tsx`:

```tsx
import { useCallback, useEffect, useRef, useState } from 'react';
import { Bell } from 'lucide-react';
import { Link } from 'react-router-dom';
import { useTenant } from '@/providers/TenantProvider';
import { useApiClient } from '@/hooks/useApiClient';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Button } from '@/components/ui/button';
import type { Notification } from '@/lib/api';

export function NotificationsBell(): JSX.Element {
  const api = useApiClient();
  const { currentTenantId } = useTenant();
  const [unread, setUnread] = useState(0);
  const [open, setOpen] = useState(false);
  const [items, setItems] = useState<Notification[]>([]);
  const [loading, setLoading] = useState(false);
  const pollRef = useRef<number | null>(null);

  const refreshCount = useCallback(async () => {
    if (!currentTenantId) return;
    try {
      setUnread(await api.getUnreadNotificationsCount(currentTenantId));
    } catch {
      /* keep last count */
    }
  }, [api, currentTenantId]);

  // Reshare refreshCount to the caller so in-app assign/mention can notify.
  useEffect(() => {
    void refreshCount();
    pollRef.current = window.setInterval(() => void refreshCount(), 30_000);
    const onFocus = () => void refreshCount();
    window.addEventListener('focus', onFocus);
    return () => {
      if (pollRef.current !== null) window.clearInterval(pollRef.current);
      window.removeEventListener('focus', onFocus);
    };
  }, [refreshCount]);

  const openPanel = async () => {
    if (!currentTenantId) return;
    setOpen(true);
    setLoading(true);
    try {
      const res = await api.listNotifications(currentTenantId, { unreadOnly: true, limit: 20 });
      setItems(res.data);
    } catch {
      setItems([]);
    } finally {
      setLoading(false);
    }
  };

  const markAll = async () => {
    if (!currentTenantId) return;
    await api.markAllNotificationsRead(currentTenantId);
    setItems([]);
    setUnread(0);
  };

  const markRead = async (n: Notification) => {
    if (!currentTenantId) return;
    await api.markNotificationRead(n.id, currentTenantId);
    void refreshCount();
  };

  return (
    <Popover open={open} onOpenChange={(next) => (next ? void openPanel() : setOpen(false))}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={unread > 0 ? `Notifications, ${unread} unread` : 'Notifications'}
          className="relative rounded-md p-1.5 text-text-secondary hover:bg-elevated hover:text-foreground"
        >
          <Bell className="h-4 w-4" aria-hidden />
          {unread > 0 && (
            <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-state-critical px-1 text-[10px] font-semibold text-white">
              {unread > 99 ? '99+' : unread}
            </span>
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between border-b border-border-subtle px-3 py-2">
          <p className="text-sm font-medium text-foreground">Notifications</p>
          {items.length > 0 && (
            <Button size="sm" variant="ghost" onClick={() => void markAll()}>
              Mark all read
            </Button>
          )}
        </div>
        {items.length === 0 ? (
          <p className="px-4 py-6 text-center text-sm text-text-secondary">No notifications</p>
        ) : (
          <ul className="max-h-80 overflow-y-auto">
            {items.map((n) => (
              <li key={n.id}>
                <Link
                  to="/cases"
                  className="flex flex-col gap-0.5 px-3 py-2 hover:bg-elevated"
                  onClick={() => void markRead(n)}
                >
                  <span className="text-xs font-medium text-foreground">
                    {n.kind === 'case_assigned'
                      ? `${n.actor_name || 'A teammate'} assigned: ${n.case_title}`
                      : `${n.actor_name || 'A teammate'} mentioned you in ${n.case_title}`}
                  </span>
                  <span className="text-xs text-text-secondary">{timeAgo(n.created_at)}</span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </PopoverContent>
    </Popover>
  );
}

function timeAgo(dateStr: string): string {
  const ms = Date.now() - new Date(dateStr).getTime();
  if (ms < 60_000) return 'just now';
  const mins = Math.floor(ms / 60_000);
  if (mins < 60) return `${mins}m ago`;
  const hrs = Math.floor(mins / 60);
  return hrs < 24 ? `${hrs}h ago` : `${Math.floor(hrs / 24)}d ago`;
}
```

Mount `<NotificationsBell />` in `TopBar.tsx` adjacent to the existing profile menu; keep `NavigationScope.test.tsx`'s TopBar assertions intact (adding the bell must not remove or rename existing landmarks; if the balance of roles/links they assert changes, update the test's link count deliberately and note it in the commit).

- [ ] **Step 4: Run to verify it passes + shell suite**

Run: `npx vitest run src/components/shell/NotificationsBell.test.tsx; npx vitest run src/components/shell/NavigationScope.test.tsx`
Expected: PASS.

- [ ] **Step 5: Lint + typecheck**

Run (from `ui/`): `node scripts/run-eslint.cjs; npx tsc --noEmit`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add ui/src/components/shell/TopBar.tsx ui/src/components/shell/NotificationsBell.tsx ui/src/components/shell/NotificationsBell.test.tsx ui/src/components/shell/NavigationScope.test.tsx
git commit -m "feat: add notification bell with unread badge to top bar"
```

---

### Task 12: Final verification

- [ ] **Step 1: Backend**

Run (from `C:\dev\control_one_ws3`): `gofmt -l controlplane/internal/; go build ./...; go vet ./...; go test ./controlplane/internal/storage/ ./controlplane/internal/server/`
Expected: gofmt empty, build/vet clean; storage + server tests pass (the 4 pre-existing Postgres-integration server tests may fail without a DB — confirm they are the same 4 before/after).

- [ ] **Step 2: UI**

Run (from `ui/`): `npx tsc --noEmit; node scripts/run-eslint.cjs; npx vitest run src/components/team src/components/shell src/pages`
Expected: all clean/passing (skip `IpLifecyclePanel.test.tsx` if it OOMs — pre-existing).

- [ ] **Step 3: Migration sanity**

Run: `git diff HEAD --stat` — confirm only intended files; verify `0149_team_collaboration.up.sql` includes both the `assignee` columns and `notifications` table.

- [ ] **Step 4: Commit any stragglers**

```bash
git add -A
git commit -m "chore: final verification tweaks for case collaboration"
```

(Only if this commit would be non-empty; otherwise skip.)

---

## Self-Review Notes

- Spec coverage: schema ✓ (Task 1), storage ✓ (Tasks 2-4), endpoints ✓ (Tasks 5-6), api client ✓ (Task 7), picker/mentions/badges ✓ (Task 8-9), team page panel ✓ (Task 10), inbox bell ✓ (Task 11).
- Placeholder scan: the `allowedSafe`/`allowed_users` construction in Task 6 was flagged and replaced inline (see planning note). One inline check remains: `hydrateNotificationActorNames` must use indexed iteration (fixed note included).
- Type consistency: `TeamUser` in api.ts matches `storage.TeamUser` shape (id/name/email); `SOCCase.assignee`/`mentioned_users` match `socCaseUserRef`; `Notification.kind` union matches the SQL CHECK.
- `Command`/`Popover` imports must be confirmed against the actual exports of `@/components/ui/command` / `@/components/ui/popover` during Task 8 (they are the modules used by `GlobalSearch`/`TenantSwitcher`).