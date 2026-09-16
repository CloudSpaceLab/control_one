package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

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
// substring, limited to `limit` (<= 0 means 50).
func (s *Store) ListTenantUsers(ctx context.Context, tenantID uuid.UUID, query string, limit int) ([]TeamUser, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	pattern := "%" + escapeLike(strings.ToLower(strings.TrimSpace(query))) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (u.id) u.id, u.display_name, u.email
		FROM user_roles ur
		JOIN users u ON u.id = ur.user_id
		JOIN roles r ON r.id = ur.role_id
		WHERE (ur.tenant_id IS NULL OR ur.tenant_id = $1)
		  AND LOWER(r.name) IN ('investigator', 'operator', 'admin')
		  AND (ur.expires_at IS NULL OR ur.expires_at > NOW())
		  AND (LOWER(COALESCE(u.display_name, '')) LIKE $2
		       OR LOWER(COALESCE(u.email, '')) LIKE $2)
		ORDER BY u.id, LOWER(COALESCE(u.display_name, u.email, u.id::text))
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

// Notification is one inbox row for a recipient.
type Notification struct {
	ID          uuid.UUID  `json:"id"`
	TenantID    uuid.UUID  `json:"tenant_id"`
	RecipientID uuid.UUID  `json:"recipient_id"`
	ActorID     uuid.UUID  `json:"actor_id,omitempty"`
	ActorName   string     `json:"actor_name,omitempty"`
	Kind        string     `json:"kind"`
	CaseID      uuid.UUID  `json:"case_id"`
	CaseTitle   string     `json:"case_title"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// notificationSelect is the shared column list for notification rows, joined
// to the actor's display name so the inbox can show who acted.
const notificationSelect = `
	SELECT n.id, n.tenant_id, n.recipient_id, n.actor_id,
	       COALESCE(a.display_name, '') AS actor_name,
	       n.kind, n.case_id, n.case_title, n.read_at, n.created_at
	FROM notifications n
	LEFT JOIN users a ON a.id = n.actor_id`

// scanNotification decodes one notification row produced by notificationSelect.
func scanNotification(scan func(dest ...any) error) (Notification, error) {
	var n Notification
	var actorID uuid.NullUUID
	var caseID uuid.NullUUID
	var readAt sql.NullTime
	if err := scan(&n.ID, &n.TenantID, &n.RecipientID, &actorID, &n.ActorName, &n.Kind, &caseID, &n.CaseTitle, &readAt, &n.CreatedAt); err != nil {
		return Notification{}, err
	}
	if actorID.Valid {
		n.ActorID = actorID.UUID
	}
	if caseID.Valid {
		n.CaseID = caseID.UUID
	}
	if readAt.Valid {
		t := readAt.Time
		n.ReadAt = &t
	}
	return n, nil
}

// CreateNotificationParams describes one inbox row to persist.
type CreateNotificationParams struct {
	TenantID    uuid.UUID
	RecipientID uuid.UUID
	ActorID     uuid.UUID
	Kind        string
	CaseID      uuid.UUID
	CaseTitle   string
}

// CreateNotification inserts a new inbox row for a recipient and returns it.
func (s *Store) CreateNotification(ctx context.Context, p CreateNotificationParams) (*Notification, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		INSERT INTO notifications (tenant_id, recipient_id, actor_id, kind, case_id, case_title)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, recipient_id, actor_id,
		          COALESCE((SELECT display_name FROM users WHERE id = actor_id), '') AS actor_name,
		          kind, case_id, case_title, read_at, created_at
	`, p.TenantID, p.RecipientID, nullableUUID(p.ActorID), p.Kind, p.CaseID, p.CaseTitle)
	out, err := scanNotification(row.Scan)
	if err != nil {
		return nil, fmt.Errorf("insert notification: %w", err)
	}
	return &out, nil
}

// NotificationFilter scopes the notification inbox query.
type NotificationFilter struct {
	TenantID    uuid.UUID
	RecipientID uuid.UUID
	UnreadOnly  bool
}

// ListNotifications returns a recipient's notifications for a tenant, newest
// first, with total count and LIMIT/OFFSET paging applied to the returned
// page.
func (s *Store) ListNotifications(ctx context.Context, filter NotificationFilter, limit, offset int) ([]Notification, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	where := " WHERE n.tenant_id = $1 AND n.recipient_id = $2"
	args := []any{filter.TenantID, filter.RecipientID}
	if filter.UnreadOnly {
		where += " AND n.read_at IS NULL"
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM notifications n" + where
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, notificationSelect+where+`
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT $3 OFFSET $4`, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	out := make([]Notification, 0, limit)
	for rows.Next() {
		n, err := scanNotification(rows.Scan)
		if err != nil {
			return nil, 0, fmt.Errorf("scan notification: %w", err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate notifications: %w", err)
	}
	return out, total, nil
}

// CountUnreadNotifications returns the number of unread notifications for a
// recipient within a tenant.
func (s *Store) CountUnreadNotifications(ctx context.Context, tenantID, recipientID uuid.UUID) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM notifications
		WHERE tenant_id = $1 AND recipient_id = $2 AND read_at IS NULL
	`, tenantID, recipientID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count unread notifications: %w", err)
	}
	return count, nil
}

// MarkNotificationRead marks a single notification read, scoped to both
// tenant and recipient so a foreign recipient's row can't be touched. It is
// idempotent: marking an already-read or inaccessible notification is a
// harmless no-op that affects no rows.
func (s *Store) MarkNotificationRead(ctx context.Context, tenantID, recipientID, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE notifications
		SET read_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND recipient_id = $3 AND read_at IS NULL
	`, id, tenantID, recipientID)
	if err != nil {
		return fmt.Errorf("mark notification read: %w", err)
	}
	return nil
}

// MarkAllNotificationsRead marks every unread notification for a recipient
// within a tenant as read, returning the number of rows affected.
func (s *Store) MarkAllNotificationsRead(ctx context.Context, tenantID, recipientID uuid.UUID) (int64, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE notifications
		SET read_at = NOW()
		WHERE tenant_id = $1 AND recipient_id = $2 AND read_at IS NULL
	`, tenantID, recipientID)
	if err != nil {
		return 0, fmt.Errorf("mark all notifications read: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected for mark all read: %w", err)
	}
	return affected, nil
}

// CaseAssignee returns the current owner of a case. Unassigned cases return
// uuid.Nil.
func (s *Store) CaseAssignee(ctx context.Context, tenantID, caseID uuid.UUID) (uuid.UUID, error) {
	if s.db == nil {
		return uuid.Nil, errors.New("store database not initialized")
	}

	var assigneeID sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT assignee_id FROM ai_investigations WHERE id = $1 AND tenant_id = $2`, caseID, tenantID).Scan(&assigneeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return uuid.Nil, sql.ErrNoRows
		}
		return uuid.Nil, fmt.Errorf("get case assignee: %w", err)
	}
	if !assigneeID.Valid {
		return uuid.Nil, nil
	}

	parsed, err := uuid.Parse(assigneeID.String)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse case assignee: %w", err)
	}
	return parsed, nil
}

// AssignCase sets or clears a case owner. A nil assignee clears ownership.
func (s *Store) AssignCase(ctx context.Context, tenantID, caseID, assigneeID, assignedBy uuid.UUID, now time.Time) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}

	if assigneeID == uuid.Nil {
		result, err := s.db.ExecContext(ctx, `
			UPDATE ai_investigations
			SET assignee_id = NULL, assigned_by = $3, assigned_at = $4, updated_at = NOW()
			WHERE id = $1 AND tenant_id = $2
		`, caseID, tenantID, nullableUUID(assignedBy), now)
		if err != nil {
			return fmt.Errorf("clear case assignee: %w", err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("clear case assignee: %w", err)
		}
		if affected == 0 {
			return fmt.Errorf("clear case assignee: %w", sql.ErrNoRows)
		}
		return nil
	}

	var exists bool
	err := s.db.QueryRowContext(ctx, `
		WITH target AS (
			SELECT id, assignee_id
			FROM ai_investigations
			WHERE id = $1 AND tenant_id = $2
			FOR UPDATE
		), updated AS (
			UPDATE ai_investigations ai
			SET assignee_id = $3, assigned_by = $4, assigned_at = $5, updated_at = NOW()
			FROM target
			WHERE ai.id = target.id
			  AND target.assignee_id IS DISTINCT FROM $3
			RETURNING ai.id
		)
		SELECT EXISTS(SELECT 1 FROM target)
	`, caseID, tenantID, assigneeID, nullableUUID(assignedBy), now).Scan(&exists)
	if err != nil {
		return fmt.Errorf("assign case: %w", err)
	}
	if !exists {
		return fmt.Errorf("assign case: %w", sql.ErrNoRows)
	}
	return nil
}

// CaseMentionedUsers returns distinct valid users mentioned in notes on a case.
func (s *Store) CaseMentionedUsers(ctx context.Context, tenantID, caseID uuid.UUID) ([]uuid.UUID, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT m.value
		FROM audit_logs al
		CROSS JOIN LATERAL jsonb_array_elements_text(
			CASE
				WHEN jsonb_typeof(al.metadata->'mentions') = 'array' THEN al.metadata->'mentions'
				ELSE '[]'::jsonb
			END
		) AS m(value)
		WHERE al.tenant_id = $1
		  AND al.action = 'soc.case.note.add'
		  AND al.resource_type = 'ai_investigation'
		  AND al.resource_id = $2
		  AND m.value <> ''
	`, tenantID, caseID.String())
	if err != nil {
		return nil, fmt.Errorf("query case mentioned users: %w", err)
	}
	defer rows.Close()

	seen := make(map[uuid.UUID]struct{})
	mentioned := []uuid.UUID{}
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, fmt.Errorf("scan case mentioned user: %w", err)
		}
		id, err := uuid.Parse(value)
		if err != nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		mentioned = append(mentioned, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate case mentioned users: %w", err)
	}
	return mentioned, nil
}
