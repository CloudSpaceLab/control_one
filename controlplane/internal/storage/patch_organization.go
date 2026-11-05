package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type PatchOrganizationPolicy struct {
	Revision           int64                              `json:"revision"`
	Status             string                             `json:"status"`
	Policy             patchgovernance.OrganizationPolicy `json:"policy"`
	UrgencyRevision    int64                              `json:"urgency_revision"`
	OperationsRevision int64                              `json:"operations_revision"`
	AuthoredBy         uuid.UUID                          `json:"authored_by"`
	ChangedBy          uuid.UUID                          `json:"changed_by"`
	ApprovedBy         *uuid.UUID                         `json:"approved_by,omitempty"`
	ApprovedAt         *time.Time                         `json:"approved_at,omitempty"`
	UpdatedAt          time.Time                          `json:"updated_at"`
	BoundControls      json.RawMessage                    `json:"bound_controls"`
}

const organizationColumns = `revision,status,document,urgency_revision,operations_revision,authored_by,changed_by,approved_by,approved_at,updated_at,bound_controls`

func scanOrganization(row interface{ Scan(...any) error }) (PatchOrganizationPolicy, error) {
	var out PatchOrganizationPolicy
	var raw []byte
	e := row.Scan(&out.Revision, &out.Status, &raw, &out.UrgencyRevision, &out.OperationsRevision, &out.AuthoredBy, &out.ChangedBy, &out.ApprovedBy, &out.ApprovedAt, &out.UpdatedAt, &out.BoundControls)
	if e == nil {
		e = json.Unmarshal(raw, &out.Policy)
	}
	return out, e
}

func (s *Store) GetPatchOrganizationPolicy(ctx context.Context, tenant uuid.UUID) (*PatchOrganizationPolicy, error) {
	out, e := scanOrganization(s.db.QueryRowContext(ctx, `SELECT `+organizationColumns+` FROM patch_organization_policy WHERE tenant_id=$1`, tenant))
	if e == sql.ErrNoRows {
		return nil, nil
	}
	return &out, e
}

// History is retained separately from purgeable review events.
func (s *Store) ListPatchOrganizationHistory(ctx context.Context, tenant uuid.UUID) ([]PatchOrganizationPolicy, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+organizationColumns+` FROM patch_organization_history WHERE tenant_id=$1 ORDER BY revision DESC LIMIT 50`, tenant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PatchOrganizationPolicy{}
	for rows.Next() {
		p, e := scanOrganization(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func organizationRole(ctx context.Context, tx *sql.Tx, tenant, user uuid.UUID, roles []string) error {
	var allowed bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id JOIN users u ON u.id=ur.user_id WHERE ur.user_id=$1 AND u.disabled_at IS NULL AND (ur.tenant_id=$2 OR ur.tenant_id IS NULL) AND r.name=ANY($3) AND (ur.expires_at IS NULL OR ur.expires_at>NOW()))`, user, tenant, pq.Array(roles)).Scan(&allowed)
	if e != nil {
		return e
	}
	if !allowed {
		return fmt.Errorf("identity lacks the current tenant role required for this policy action")
	}
	return nil
}

func organizationAdmin(ctx context.Context, tx *sql.Tx, tenant, user uuid.UUID) error {
	return organizationRole(ctx, tx, tenant, user, []string{"admin"})
}
func organizationApprover(ctx context.Context, tx *sql.Tx, tenant, user uuid.UUID) error {
	return organizationRole(ctx, tx, tenant, user, []string{"admin", "ciso"})
}

func (s *Store) ListPatchOrganizationPeople(ctx context.Context, tenant uuid.UUID) ([]PatchReviewRecipient, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT u.id,COALESCE(NULLIF(u.display_name,''),u.external_id) FROM users u WHERE u.disabled_at IS NULL AND EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND (ur.tenant_id=$1 OR ur.tenant_id IS NULL) AND (ur.expires_at IS NULL OR ur.expires_at>NOW()) AND r.name IN ('admin','ciso')) ORDER BY u.id`, tenant)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PatchReviewRecipient{}
	for rows.Next() {
		var person PatchReviewRecipient
		if e = rows.Scan(&person.ID, &person.Name); e != nil {
			return nil, e
		}
		out = append(out, person)
	}
	return out, rows.Err()
}

func (s *Store) SavePatchOrganizationPolicy(ctx context.Context, tenant, actor uuid.UUID, expected int64, action string, p patchgovernance.OrganizationPolicy) (PatchOrganizationPolicy, error) {
	var out PatchOrganizationPolicy
	if tenant == uuid.Nil || actor == uuid.Nil || expected < 0 {
		return out, fmt.Errorf("tenant, administrator and revision required")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var locked uuid.UUID
	if e = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&locked); e != nil {
		return out, e
	}
	checkActor := organizationAdmin
	if action == "approve" {
		checkActor = organizationApprover
	}
	if e = checkActor(ctx, tx, tenant, actor); e != nil {
		return out, e
	}
	current, e := scanOrganization(tx.QueryRowContext(ctx, `SELECT `+organizationColumns+` FROM patch_organization_policy WHERE tenant_id=$1 FOR UPDATE`, tenant))
	if e != nil && e != sql.ErrNoRows {
		return out, e
	}
	if current.Revision != expected {
		return out, ErrGovernanceConflict
	}
	now := time.Now().UTC()
	var urgency, operations int64
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM patch_governance_policy WHERE tenant_id=$1),0),COALESCE((SELECT revision FROM patch_review_operations WHERE tenant_id=$1),0)`, tenant).Scan(&urgency, &operations); e != nil {
		return out, e
	}
	out = current
	switch action {
	case "draft":
		if e = p.Validate(now); e != nil {
			return out, e
		}
		if e = organizationAdmin(ctx, tx, tenant, p.OwnerID); e != nil {
			return out, e
		}
		if e = organizationApprover(ctx, tx, tenant, p.ApproverID); e != nil {
			return out, e
		}
		out.Policy = p
		out.Status = "draft"
		out.AuthoredBy = actor
		out.ApprovedBy = nil
		out.ApprovedAt = nil
		out.UrgencyRevision = urgency
		out.OperationsRevision = operations
		if e = tx.QueryRowContext(ctx, `SELECT jsonb_build_object('urgency',(SELECT document FROM patch_governance_policy WHERE tenant_id=$1),'operations',(SELECT document FROM patch_review_operations WHERE tenant_id=$1))`, tenant).Scan(&out.BoundControls); e != nil {
			return out, e
		}
	case "submit", "approve":
		if (action == "submit" && current.Status != "draft") || (action == "approve" && current.Status != "review") {
			return out, fmt.Errorf("invalid policy lifecycle transition")
		}
		if e = current.Policy.Validate(now); e != nil {
			return out, e
		}
		if e = organizationAdmin(ctx, tx, tenant, current.Policy.OwnerID); e != nil {
			return out, e
		}
		if e = organizationApprover(ctx, tx, tenant, current.Policy.ApproverID); e != nil {
			return out, e
		}
		if urgency == 0 || operations == 0 || urgency != current.UrgencyRevision || operations != current.OperationsRevision {
			return out, fmt.Errorf("saved urgency/operational controls changed or are missing; save and review a fresh draft")
		}
		var controls struct {
			Urgency    json.RawMessage `json:"urgency"`
			Operations json.RawMessage `json:"operations"`
		}
		if json.Unmarshal(current.BoundControls, &controls) != nil || len(controls.Urgency) == 0 || string(controls.Urgency) == "null" || len(controls.Operations) == 0 || string(controls.Operations) == "null" {
			return out, fmt.Errorf("save a fresh draft containing the exact controls snapshot before review")
		}
		if action == "submit" {
			out.Status = "review"
		} else {
			if actor != current.Policy.ApproverID || actor == current.AuthoredBy {
				return out, fmt.Errorf("designated independent administrator must approve; author cannot self-approve")
			}
			out.Status = "approved"
			out.ApprovedBy = &actor
			out.ApprovedAt = &now
		}
	case "withdraw":
		if current.Status == "" || current.Status == "withdrawn" {
			return out, fmt.Errorf("no active policy record to withdraw")
		}
		out.Status = "withdrawn"
	default:
		return out, fmt.Errorf("unknown policy action")
	}
	out.Revision = expected + 1
	out.ChangedBy = actor
	out.UpdatedAt = now
	raw, e := json.Marshal(out.Policy)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO patch_organization_policy(tenant_id,`+organizationColumns+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(tenant_id) DO UPDATE SET revision=$2,status=$3,document=$4,urgency_revision=$5,operations_revision=$6,authored_by=$7,changed_by=$8,approved_by=$9,approved_at=$10,updated_at=$11,bound_controls=$12`, tenant, out.Revision, out.Status, raw, out.UrgencyRevision, out.OperationsRevision, out.AuthoredBy, out.ChangedBy, out.ApprovedBy, out.ApprovedAt, now, out.BoundControls)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO patch_organization_history SELECT * FROM patch_organization_policy WHERE tenant_id=$1`, tenant)
	if e != nil {
		return out, e
	}
	event, e := json.Marshal(out)
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) VALUES($1,$2,$3,$4,$5)`, tenant, "organization_"+action, out.Revision, actor.String(), event)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
