package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
)

var ErrGovernanceConflict = errors.New("review changed; reload before saving")

// Tenant review scope is explicit and separate from the node-only inventory
// API. Keep its 100-row bound visible to callers rather than widening that API.
func (s *Store) ListTenantPatchReviewFindings(ctx context.Context, tenant uuid.UUID) ([]VulnerabilityFinding, int, error) {
	if tenant == uuid.Nil {
		return nil, 0, fmt.Errorf("tenant required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var total int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM node_vulnerability_findings WHERE tenant_id=$1 AND resolved_at IS NULL`, tenant).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,tenant_id,node_id,package_name,installed_version,package_source,arch,cve_id,severity,cvss_score,epss_score,kev,fixed_version,advisory_url,evidence_source,reference_urls,vex_status,exception_status,evidence,first_seen_at,last_seen_at,resolved_at FROM node_vulnerability_findings WHERE tenant_id=$1 AND resolved_at IS NULL ORDER BY kev DESC,last_seen_at DESC,id LIMIT 100`, tenant)
	if err != nil {
		return nil, 0, err
	}
	out := []VulnerabilityFinding{}
	for rows.Next() {
		f, e := scanVulnerabilityFinding(rows)
		if e != nil {
			rows.Close()
			return nil, 0, e
		}
		out = append(out, *f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

type PatchGovernancePolicy struct {
	Revision  int64                  `json:"revision"`
	Policy    patchgovernance.Policy `json:"policy"`
	UpdatedBy string                 `json:"updated_by"`
	UpdatedAt time.Time              `json:"updated_at"`
}
type PatchGovernanceTracking struct {
	FindingID uuid.UUID                `json:"finding_id"`
	Revision  int64                    `json:"revision"`
	Tracking  patchgovernance.Tracking `json:"tracking"`
	UpdatedBy string                   `json:"updated_by"`
	UpdatedAt time.Time                `json:"updated_at"`
}
type PatchGovernanceEvent struct {
	ID        int64           `json:"id"`
	FindingID *uuid.UUID      `json:"finding_id,omitempty"`
	Kind      string          `json:"kind"`
	Revision  int64           `json:"revision"`
	Actor     string          `json:"actor"`
	Document  json.RawMessage `json:"document"`
	CreatedAt time.Time       `json:"created_at"`
}

func (s *Store) GetPatchGovernancePolicy(ctx context.Context, tenant uuid.UUID) (*PatchGovernancePolicy, error) {
	var p PatchGovernancePolicy
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision,document,updated_by,updated_at FROM patch_governance_policy WHERE tenant_id=$1`, tenant).Scan(&p.Revision, &raw, &p.UpdatedBy, &p.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &p.Policy)
	return &p, err
}
func (s *Store) SavePatchGovernancePolicy(ctx context.Context, tenant uuid.UUID, expected int64, p patchgovernance.Policy, actor string) (*PatchGovernancePolicy, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || actor == "" || expected < 0 {
		return nil, fmt.Errorf("tenant, actor and current revision required")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// A tenant lock serializes first-write races as well as updates.
	var owned uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&owned); err != nil {
		return nil, err
	}
	var current int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM patch_governance_policy WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if current != expected {
		return nil, ErrGovernanceConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_governance_policy(tenant_id,revision,document,updated_by) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id) DO UPDATE SET revision=$2,document=$3,updated_by=$4,updated_at=NOW()`, tenant, current+1, raw, actor)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) VALUES($1,'policy_updated',$2,$3,$4)`, tenant, current+1, actor, raw)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetPatchGovernancePolicy(ctx, tenant)
}
func (s *Store) SavePatchGovernanceTracking(ctx context.Context, tenant, finding uuid.UUID, expected int64, p patchgovernance.Tracking, actor string) (*PatchGovernanceTracking, error) {
	if err := p.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	if expected < 0 || actor == "" {
		return nil, fmt.Errorf("actor and current revision required")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM node_vulnerability_findings WHERE id=$1 AND tenant_id=$2 AND resolved_at IS NULL FOR UPDATE`, finding, tenant).Scan(&id); err != nil {
		return nil, fmt.Errorf("active tenant finding required: %w", err)
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM patch_governance_tracking WHERE tenant_id=$1 AND finding_id=$2 FOR UPDATE`, tenant, finding).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if revision != expected {
		return nil, ErrGovernanceConflict
	}
	var saved PatchGovernanceTracking
	saved.FindingID = finding
	saved.Tracking = p
	saved.Revision = revision + 1
	saved.UpdatedBy = actor
	err = tx.QueryRowContext(ctx, `INSERT INTO patch_governance_tracking(tenant_id,finding_id,revision,document,updated_by) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,finding_id) DO UPDATE SET revision=$3,document=$4,updated_by=$5,updated_at=NOW() RETURNING updated_at`, tenant, finding, saved.Revision, raw, actor).Scan(&saved.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,finding_id,kind,revision,actor,document) VALUES($1,$2,'finding_reviewed',$3,$4,$5)`, tenant, finding, saved.Revision, actor, raw)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &saved, nil
}
func (s *Store) ListPatchGovernanceTracking(ctx context.Context, tenant uuid.UUID) ([]PatchGovernanceTracking, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT finding_id,revision,document,updated_by,updated_at FROM patch_governance_tracking WHERE tenant_id=$1 ORDER BY finding_id LIMIT 1001`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PatchGovernanceTracking{}
	for rows.Next() {
		var v PatchGovernanceTracking
		var raw []byte
		if err = rows.Scan(&v.FindingID, &v.Revision, &raw, &v.UpdatedBy, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &v.Tracking); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if len(out) > 1000 {
		return nil, fmt.Errorf("review scope exceeds 1000; narrow scope before export")
	}
	return out, rows.Err()
}
func (s *Store) ListPatchGovernanceEvents(ctx context.Context, tenant uuid.UUID, days int) ([]PatchGovernanceEvent, bool, error) {
	if days < 1 || days > 3650 {
		return nil, false, fmt.Errorf("valid retention interval required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,finding_id,kind,revision,actor,document,created_at FROM patch_governance_events WHERE tenant_id=$1 AND created_at>=$2 ORDER BY id DESC LIMIT 1001`, tenant, time.Now().UTC().Add(-time.Duration(days)*24*time.Hour))
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []PatchGovernanceEvent{}
	for rows.Next() {
		var v PatchGovernanceEvent
		if err = rows.Scan(&v.ID, &v.FindingID, &v.Kind, &v.Revision, &v.Actor, &v.Document, &v.CreatedAt); err != nil {
			return nil, false, err
		}
		out = append(out, v)
	}
	truncated := len(out) > 1000
	if truncated {
		out = out[:1000]
	}
	return out, truncated, rows.Err()
}
