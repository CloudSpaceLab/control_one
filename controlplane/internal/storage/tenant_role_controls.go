package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type TenantRoleAssignment struct {
	ID        uuid.UUID  `json:"id"`
	UserID    uuid.UUID  `json:"user_id"`
	Name      string     `json:"name"`
	Role      string     `json:"role"`
	Global    bool       `json:"global"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}
type TenantRoleEvent struct {
	Revision     int64     `json:"revision"`
	ActorID      uuid.UUID `json:"actor_id"`
	UserID       uuid.UUID `json:"user_id"`
	AssignmentID uuid.UUID `json:"assignment_id"`
	Action       string    `json:"action"`
	Role         string    `json:"role"`
	ExpiresAt    time.Time `json:"expires_at"`
	Reason       string    `json:"reason"`
	CreatedAt    time.Time `json:"created_at"`
}
type TenantRoleControlPage struct {
	Revision    int64                  `json:"revision"`
	Assignments []TenantRoleAssignment `json:"assignments"`
	History     []TenantRoleEvent      `json:"history"`
}
type TenantRoleControlChange struct {
	ExpectedRevision int64     `json:"expected_revision"`
	UserID           uuid.UUID `json:"user_id"`
	Role             string    `json:"role"`
	Action           string    `json:"action"`
	ExpiresAt        time.Time `json:"expires_at"`
	Reason           string    `json:"reason"`
}

// Scope grants cannot create global administrators. Until the wider global
// administration surface is qualified for scoped admins, grant management is
// restricted to currently active global administrators.
func (s *Store) HasCurrentGlobalAdmin(ctx context.Context, user uuid.UUID) (bool, error) {
	var allowed bool
	e := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.id=$1 AND u.disabled_at IS NULL AND ur.tenant_id IS NULL AND r.name='admin' AND (ur.expires_at IS NULL OR ur.expires_at>NOW()))`, user).Scan(&allowed)
	return allowed, e
}

func (s *Store) GetTenantRoleControls(ctx context.Context, tenant, user uuid.UUID, manage bool) (TenantRoleControlPage, error) {
	out := TenantRoleControlPage{Assignments: []TenantRoleAssignment{}, History: []TenantRoleEvent{}}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM tenant_role_controls WHERE tenant_id=$1),0)`, tenant).Scan(&out.Revision)
	if e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT ur.id,u.id,COALESCE(NULLIF(u.display_name,''),u.external_id),r.name,ur.tenant_id IS NULL,ur.expires_at FROM user_roles ur JOIN users u ON u.id=ur.user_id JOIN roles r ON r.id=ur.role_id WHERE u.disabled_at IS NULL AND (ur.tenant_id=$1 OR ur.tenant_id IS NULL) AND ($3 OR ur.user_id=$2) ORDER BY u.id,ur.tenant_id NULLS FIRST,r.name`, tenant, user, manage)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var a TenantRoleAssignment
		if e = rows.Scan(&a.ID, &a.UserID, &a.Name, &a.Role, &a.Global, &a.ExpiresAt); e != nil {
			rows.Close()
			return out, e
		}
		out.Assignments = append(out.Assignments, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT revision,actor_id,user_id,assignment_id,action,role,expires_at,reason,created_at FROM tenant_role_control_events WHERE tenant_id=$1 AND ($3 OR user_id=$2) ORDER BY revision DESC LIMIT 50`, tenant, user, manage)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var event TenantRoleEvent
		if e = rows.Scan(&event.Revision, &event.ActorID, &event.UserID, &event.AssignmentID, &event.Action, &event.Role, &event.ExpiresAt, &event.Reason, &event.CreatedAt); e != nil {
			rows.Close()
			return out, e
		}
		out.History = append(out.History, event)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}

func (s *Store) SaveTenantRoleControl(ctx context.Context, tenant, actor uuid.UUID, p TenantRoleControlChange) error {
	if tenant == uuid.Nil || actor == uuid.Nil || p.UserID == uuid.Nil || p.UserID == actor || p.ExpectedRevision < 0 {
		return fmt.Errorf("select another existing account, a tenant and the current revision")
	}
	if p.Role != "viewer" && p.Role != "operator" && p.Role != "ciso" {
		return fmt.Errorf("only viewer, operator and ciso tenant assignments are supported; administrator grants are unavailable")
	}
	if p.Action != "grant" && p.Action != "revoke" {
		return fmt.Errorf("choose grant or revoke")
	}
	if strings.TrimSpace(p.Reason) == "" || len(p.Reason) > 2000 {
		return fmt.Errorf("a change reason of at most 2000 bytes is required")
	}
	now := time.Now().UTC()
	if p.Action == "grant" && (!p.ExpiresAt.After(now) || p.ExpiresAt.After(now.Add(30*24*time.Hour))) {
		return fmt.Errorf("tenant role must expire within 30 days")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if e = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&locked); e != nil {
		return e
	}
	var actorAssignment uuid.UUID
	e = tx.QueryRowContext(ctx, `SELECT ur.id FROM users u JOIN user_roles ur ON ur.user_id=u.id JOIN roles r ON r.id=ur.role_id WHERE u.id=$1 AND u.disabled_at IS NULL AND ur.tenant_id IS NULL AND r.name='admin' AND (ur.expires_at IS NULL OR ur.expires_at>NOW()) FOR SHARE OF ur,u`, actor).Scan(&actorAssignment)
	if e != nil {
		return fmt.Errorf("current global administrator required for tenant access management")
	}
	var revision int64
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM tenant_role_controls WHERE tenant_id=$1),0)`, tenant).Scan(&revision); e != nil {
		return e
	}
	if revision != p.ExpectedRevision {
		return ErrGovernanceConflict
	}
	var target uuid.UUID
	if e = tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 AND disabled_at IS NULL FOR SHARE`, p.UserID).Scan(&target); e != nil {
		return fmt.Errorf("active existing account required")
	}
	var role uuid.UUID
	if e = tx.QueryRowContext(ctx, `SELECT id FROM roles WHERE name=$1`, p.Role).Scan(&role); e != nil {
		return e
	}
	var assignment uuid.UUID
	if p.Action == "grant" {
		e = tx.QueryRowContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id,assigned_by,expires_at) VALUES($1,$2,$3,$4,$5) ON CONFLICT(user_id,role_id,tenant_id) WHERE tenant_id IS NOT NULL DO UPDATE SET assigned_by=$4,expires_at=$5 RETURNING id`, p.UserID, role, tenant, actor, p.ExpiresAt.UTC()).Scan(&assignment)
	} else {
		p.ExpiresAt = now
		e = tx.QueryRowContext(ctx, `UPDATE user_roles SET expires_at=$4 WHERE user_id=$1 AND role_id=$2 AND tenant_id=$3 AND (expires_at IS NULL OR expires_at>$4) RETURNING id`, p.UserID, role, tenant, now).Scan(&assignment)
	}
	if e != nil {
		return fmt.Errorf("tenant assignment could not be changed; reload its current state: %w", e)
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO tenant_role_controls(tenant_id,revision,changed_by) VALUES($1,$2,$3) ON CONFLICT(tenant_id) DO UPDATE SET revision=$2,changed_by=$3,updated_at=NOW()`, tenant, revision+1, actor)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO tenant_role_control_events(tenant_id,revision,actor_id,user_id,assignment_id,action,role,expires_at,reason) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, tenant, revision+1, actor, p.UserID, assignment, p.Action, p.Role, p.ExpiresAt.UTC(), p.Reason)
	if e != nil {
		return e
	}
	return tx.Commit()
}
