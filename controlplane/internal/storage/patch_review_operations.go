package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type PatchReviewOperations struct {
	Revision  int64                      `json:"revision"`
	Policy    patchgovernance.Operations `json:"policy"`
	UpdatedBy string                     `json:"updated_by"`
	UpdatedAt time.Time                  `json:"updated_at"`
}
type PatchReviewRecipient struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
type PatchReviewOperationState struct {
	CheckedAt *time.Time `json:"checked_at,omitempty"`
	Delivered int        `json:"delivered"`
	LastError string     `json:"last_error"`
}
type PatchReviewArchiveInput struct {
	HighEventID int64          `json:"high_event_id"`
	Counts      map[string]int `json:"counts"`
}
type PatchRetentionPreview struct {
	Revision int64     `json:"revision"`
	Cutoff   time.Time `json:"cutoff"`
	Count    int       `json:"count"`
	SHA256   string    `json:"sha256"`
	Blocked  string    `json:"blocked,omitempty"`
	IDs      []int64   `json:"-"`
}

func (s *Store) GetPatchReviewOperations(ctx context.Context, tenant uuid.UUID) (PatchReviewOperations, error) {
	out := PatchReviewOperations{Policy: patchgovernance.DefaultOperations()}
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT revision,document,updated_by,updated_at FROM patch_review_operations WHERE tenant_id=$1`, tenant).Scan(&out.Revision, &raw, &out.UpdatedBy, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out.Policy)
	return out, err
}
func (s *Store) ListPatchReviewRecipients(ctx context.Context, tenant uuid.UUID) ([]PatchReviewRecipient, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT u.id,COALESCE(NULLIF(u.display_name,''),u.external_id) FROM users u WHERE EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=u.id AND (ur.tenant_id=$1 OR ur.tenant_id IS NULL) AND (ur.expires_at IS NULL OR ur.expires_at>NOW()) AND r.name=ANY($2)) ORDER BY u.id`, tenant, pq.Array([]string{"viewer", "operator", "admin"}))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PatchReviewRecipient{}
	for rows.Next() {
		var v PatchReviewRecipient
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) SavePatchReviewOperations(ctx context.Context, tenant uuid.UUID, expected int64, p patchgovernance.Operations, actor string) (PatchReviewOperations, error) {
	if err := p.Validate(time.Now().UTC()); err != nil {
		return PatchReviewOperations{}, err
	}
	if actor == "" || tenant == uuid.Nil || expected < 0 {
		return PatchReviewOperations{}, fmt.Errorf("actor, tenant and revision required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PatchReviewOperations{}, err
	}
	defer tx.Rollback()
	var owned uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR UPDATE`, tenant).Scan(&owned); err != nil {
		return PatchReviewOperations{}, err
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM patch_review_operations WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return PatchReviewOperations{}, err
	}
	if revision != expected {
		return PatchReviewOperations{}, ErrGovernanceConflict
	}
	for _, id := range p.Notifications.Recipients {
		var allowed bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND (ur.tenant_id=$2 OR ur.tenant_id IS NULL) AND (ur.expires_at IS NULL OR ur.expires_at>NOW()) AND r.name IN ('viewer','operator','admin'))`, id, tenant).Scan(&allowed)
		if err != nil || !allowed {
			return PatchReviewOperations{}, fmt.Errorf("notification recipient must have current tenant access")
		}
	}
	if p.Automation.Mode != "recommend_only" {
		var count int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM nodes WHERE tenant_id=$1 AND id=ANY($2::uuid[]) AND state='active'`, tenant, pq.Array(p.Automation.NodeIDs)).Scan(&count)
		if err != nil || count != len(p.Automation.NodeIDs) {
			return PatchReviewOperations{}, fmt.Errorf("all automation targets must be active tenant nodes")
		}
		var nodes []string
		err = tx.QueryRowContext(ctx, `SELECT node_ids::text[] FROM maintenance_windows WHERE id=$1 AND tenant_id=$2 AND ($3=false OR (status IN ('scheduled','open') AND closes_at>NOW() AND force_closed_at IS NULL AND cardinality(allow_repos)=0))`, p.Automation.WindowID, tenant, p.Automation.Enabled).Scan(pq.Array(&nodes))
		if err != nil {
			return PatchReviewOperations{}, fmt.Errorf("tenant maintenance window is required")
		}
		members := map[string]bool{}
		for _, id := range nodes {
			members[id] = true
		}
		for _, id := range p.Automation.NodeIDs {
			if !members[id] {
				return PatchReviewOperations{}, fmt.Errorf("automation target is outside the maintenance window")
			}
		}
		if p.Automation.Enabled {
			for _, id := range p.Automation.NodeIDs {
				if err = checkAutomationHealthTx(ctx, tx, tenant, uuid.MustParse(id)); err != nil {
					return PatchReviewOperations{}, err
				}
			}
		}
	}
	raw, _ := json.Marshal(p)
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_review_operations(tenant_id,revision,document,updated_by) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id) DO UPDATE SET revision=$2,document=$3,updated_by=$4,updated_at=NOW()`, tenant, revision+1, raw, actor)
	if err != nil {
		return PatchReviewOperations{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) VALUES($1,'operations_updated',$2,$3,$4)`, tenant, revision+1, actor, raw)
	if err != nil {
		return PatchReviewOperations{}, err
	}
	if err = tx.Commit(); err != nil {
		return PatchReviewOperations{}, err
	}
	return s.GetPatchReviewOperations(ctx, tenant)
}
func (s *Store) RecordPatchReviewArchive(ctx context.Context, tenant uuid.UUID, actor, hash string, input PatchReviewArchiveInput) (uuid.UUID, error) {
	if tenant == uuid.Nil || actor == "" || len(hash) != 64 || input.HighEventID < 0 {
		return uuid.Nil, fmt.Errorf("valid tenant archive identity required")
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return uuid.Nil, fmt.Errorf("valid archive SHA-256 required")
	}
	id := uuid.New()
	raw, _ := json.Marshal(input.Counts)
	_, err := s.db.ExecContext(ctx, `INSERT INTO patch_review_archives(id,tenant_id,actor,sha256,high_event_id,counts) VALUES($1,$2,$3,$4,$5,$6)`, id, tenant, actor, hash, input.HighEventID, raw)
	return id, err
}

const retentionCandidates = `SELECT e.id,e.document FROM patch_governance_events e WHERE e.tenant_id=$1 AND e.created_at<$2
 AND e.kind IN ('policy_updated','finding_reviewed','operations_updated')
 AND NOT EXISTS(SELECT 1 FROM patch_governance_policy p WHERE p.tenant_id=e.tenant_id AND e.kind='policy_updated' AND p.revision=e.revision)
 AND NOT EXISTS(SELECT 1 FROM patch_governance_tracking t WHERE t.tenant_id=e.tenant_id AND t.finding_id=e.finding_id AND e.kind='finding_reviewed' AND t.revision=e.revision)
 AND NOT EXISTS(SELECT 1 FROM patch_review_operations o WHERE o.tenant_id=e.tenant_id AND e.kind='operations_updated' AND o.revision=e.revision)
 ORDER BY e.id LIMIT 1000`

func retentionPreview(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, ops PatchReviewOperations, cutoff time.Time) (PatchRetentionPreview, error) {
	p := PatchRetentionPreview{Revision: ops.Revision, Cutoff: cutoff, IDs: []int64{}}
	if ops.Policy.Retention.LegalHold {
		p.Blocked = "Legal hold is enabled"
	} else if ops.Policy.Retention.Mode != "archive_then_purge" {
		p.Blocked = "Retention mode preserves all history"
	}
	rows, err := tx.QueryContext(ctx, retentionCandidates, tenant, cutoff)
	if err != nil {
		return p, err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%s\n", tenant, ops.Revision, cutoff.UTC().Format(time.RFC3339Nano))
	for rows.Next() {
		var id int64
		var raw []byte
		if err = rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return p, err
		}
		p.IDs = append(p.IDs, id)
		fmt.Fprintf(h, "%d:%s\n", id, raw)
	}
	err = rows.Err()
	rows.Close()
	p.Count = len(p.IDs)
	p.SHA256 = hex.EncodeToString(h.Sum(nil))
	return p, err
}
func (s *Store) PreviewPatchReviewRetention(ctx context.Context, tenant uuid.UUID) (PatchRetentionPreview, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return PatchRetentionPreview{}, err
	}
	defer tx.Rollback()
	var ops PatchReviewOperations
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,document FROM patch_review_operations WHERE tenant_id=$1`, tenant).Scan(&ops.Revision, &raw)
	if err != nil {
		return PatchRetentionPreview{}, fmt.Errorf("save operations policy first")
	}
	if err = json.Unmarshal(raw, &ops.Policy); err != nil {
		return PatchRetentionPreview{}, err
	}
	cut := time.Now().UTC().Add(-time.Duration(ops.Policy.Retention.Days) * 24 * time.Hour).Truncate(time.Hour)
	return retentionPreview(ctx, tx, tenant, ops, cut)
}
func (s *Store) PurgePatchReviewRetention(ctx context.Context, tenant uuid.UUID, revision int64, cutoff time.Time, hash string, archive uuid.UUID, actor, destination string) (int, error) {
	if len(destination) < 3 || len(destination) > 500 {
		return 0, fmt.Errorf("verified archive destination is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var ops PatchReviewOperations
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,document FROM patch_review_operations WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&ops.Revision, &raw)
	if err != nil {
		return 0, err
	}
	if json.Unmarshal(raw, &ops.Policy) != nil || ops.Revision != revision {
		return 0, ErrGovernanceConflict
	}
	if cutoff.IsZero() || cutoff.After(time.Now().UTC().Add(-time.Duration(ops.Policy.Retention.Days)*24*time.Hour)) {
		return 0, fmt.Errorf("retention cutoff exceeds policy")
	}
	p, err := retentionPreview(ctx, tx, tenant, ops, cutoff)
	if err != nil {
		return 0, err
	}
	if p.Blocked != "" {
		return 0, errors.New(p.Blocked)
	}
	if p.SHA256 != hash {
		return 0, ErrGovernanceConflict
	}
	var maxID int64
	err = tx.QueryRowContext(ctx, `SELECT high_event_id FROM patch_review_archives WHERE id=$1 AND tenant_id=$2 AND actor=$3 AND created_at>NOW()-INTERVAL '24 hours'`, archive, tenant, actor).Scan(&maxID)
	if err != nil || len(p.IDs) > 0 && p.IDs[len(p.IDs)-1] > maxID {
		return 0, fmt.Errorf("a recent complete archive from this administrator must cover every eligible event")
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM patch_governance_events WHERE tenant_id=$1 AND id=ANY($2::bigint[])`, tenant, pq.Array(p.IDs))
	if err != nil {
		return 0, err
	}
	count, err := result.RowsAffected()
	if err != nil || int(count) != p.Count {
		return 0, ErrGovernanceConflict
	}
	receipt, _ := json.Marshal(map[string]any{"archive_id": archive, "archive_destination": destination, "plan_sha256": hash, "cutoff": cutoff, "deleted": count})
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) VALUES($1,'retention_purged',$2,$3,$4)`, tenant, revision, actor, receipt)
	if err != nil {
		return 0, err
	}
	return int(count), tx.Commit()
}

func (s *Store) DispatchPatchReviewEscalations(ctx context.Context, tenant uuid.UUID) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var ops PatchReviewOperations
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT revision,document FROM patch_review_operations WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&ops.Revision, &raw)
	if err != nil {
		return 0, err
	}
	if err = json.Unmarshal(raw, &ops.Policy); err != nil {
		return 0, err
	}
	if !ops.Policy.Notifications.Enabled {
		return 0, nil
	}
	validation := ops.Policy
	validation.Automation = patchgovernance.DefaultOperations().Automation
	if err = validation.Validate(time.Now().UTC()); err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	bucket := now.Unix() / int64(ops.Policy.Notifications.RepeatHours*3600)
	delivered := 0
	for _, recipient := range ops.Policy.Notifications.Recipients {
		var allowed bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND (ur.tenant_id=$2 OR ur.tenant_id IS NULL) AND (ur.expires_at IS NULL OR ur.expires_at>NOW()) AND r.name IN ('viewer','operator','admin'))`, recipient, tenant).Scan(&allowed)
		if err != nil {
			return 0, err
		}
		if !allowed {
			continue
		}
		rows, e := tx.QueryContext(ctx, `SELECT f.id,f.cve_id,f.package_name,COALESCE(t.revision,0) FROM node_vulnerability_findings f LEFT JOIN patch_governance_tracking t ON t.tenant_id=f.tenant_id AND t.finding_id=f.id WHERE f.tenant_id=$1 AND f.resolved_at IS NULL
   AND (t.finding_id IS NULL OR NULLIF(t.document->>'owner','') IS NULL OR (t.document->>'due_at')::timestamptz<=NOW() OR (t.document->>'exception_until')::timestamptz<=NOW())
   AND NOT EXISTS(SELECT 1 FROM patch_review_escalation_receipts e WHERE e.tenant_id=f.tenant_id AND e.finding_id=f.id AND e.recipient_id=$2 AND e.review_revision=COALESCE(t.revision,0) AND e.operations_revision=$3 AND e.created_at>$4)
   ORDER BY f.id LIMIT 25`, tenant, recipient, ops.Revision, now.Add(-time.Duration(ops.Policy.Notifications.RepeatHours)*time.Hour))
		if e != nil {
			return 0, e
		}
		type entry struct {
			id       uuid.UUID
			cve, pkg string
			revision int64
		}
		batch := []entry{}
		for rows.Next() {
			var v entry
			if e = rows.Scan(&v.id, &v.cve, &v.pkg, &v.revision); e != nil {
				rows.Close()
				return 0, e
			}
			batch = append(batch, v)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, e
		}
		for _, v := range batch {
			notification := uuid.New()
			_, e = tx.ExecContext(ctx, `INSERT INTO notifications(id,tenant_id,recipient_id,kind,case_id,case_title) VALUES($1,$2,$3,'patch_review_escalation',NULL,$4)`, notification, tenant, recipient, "Patch review required: "+v.cve+" · "+v.pkg)
			if e != nil {
				return 0, e
			}
			_, e = tx.ExecContext(ctx, `INSERT INTO patch_review_escalation_receipts(tenant_id,finding_id,recipient_id,review_revision,operations_revision,interval_bucket,notification_id) VALUES($1,$2,$3,$4,$5,$6,$7)`, tenant, v.id, recipient, v.revision, ops.Revision, bucket, notification)
			if e != nil {
				return 0, e
			}
			delivered++
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO patch_review_operations_state(tenant_id,checked_at,delivered,last_error) VALUES($1,NOW(),$2,'') ON CONFLICT(tenant_id) DO UPDATE SET checked_at=NOW(),delivered=$2,last_error=''`, tenant, delivered)
	if err != nil {
		return 0, err
	}
	return delivered, tx.Commit()
}
func (s *Store) ListEnabledPatchReviewTenants(ctx context.Context, after uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tenant_id FROM patch_review_operations WHERE tenant_id>$1 AND document->'notifications'->>'enabled'='true' ORDER BY tenant_id LIMIT 25`, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (s *Store) GetPatchReviewOperationState(ctx context.Context, tenant uuid.UUID) (PatchReviewOperationState, error) {
	var v PatchReviewOperationState
	err := s.db.QueryRowContext(ctx, `SELECT checked_at,delivered,last_error FROM patch_review_operations_state WHERE tenant_id=$1`, tenant).Scan(&v.CheckedAt, &v.Delivered, &v.LastError)
	if errors.Is(err, sql.ErrNoRows) {
		return v, nil
	}
	return v, err
}
func (s *Store) RecordPatchReviewOperationError(ctx context.Context, tenant uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO patch_review_operations_state(tenant_id,checked_at,delivered,last_error) VALUES($1,NOW(),0,'Escalation delivery failed; the next check retries without claiming success') ON CONFLICT(tenant_id) DO UPDATE SET checked_at=NOW(),delivered=0,last_error=EXCLUDED.last_error`, tenant)
	return err
}
