package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type PatchReviewPage struct {
	Findings []VulnerabilityFinding    `json:"findings"`
	Tracking []PatchGovernanceTracking `json:"tracking"`
	Total    int                       `json:"total"`
	Next     string                    `json:"next_cursor,omitempty"`
}

// Stable finding IDs are the cursor; mutable severity/last-seen fields cannot
// move a row across pages. Each page has one repeatable-read database snapshot.
func (s *Store) GetPatchReviewPage(ctx context.Context, tenant uuid.UUID, after uuid.UUID, limit int) (PatchReviewPage, error) {
	out := PatchReviewPage{Findings: []VulnerabilityFinding{}, Tracking: []PatchGovernanceTracking{}}
	if tenant == uuid.Nil || limit < 1 || limit > 100 {
		return out, fmt.Errorf("tenant and page size 1–100 required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM node_vulnerability_findings WHERE tenant_id=$1 AND resolved_at IS NULL`, tenant).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,tenant_id,node_id,package_name,installed_version,package_source,arch,cve_id,severity,cvss_score,epss_score,kev,fixed_version,advisory_url,evidence_source,reference_urls,vex_status,exception_status,evidence,first_seen_at,last_seen_at,resolved_at FROM node_vulnerability_findings WHERE tenant_id=$1 AND resolved_at IS NULL AND id>$2 ORDER BY id LIMIT $3`, tenant, after, limit+1)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		f, e := scanVulnerabilityFinding(rows)
		if e != nil {
			rows.Close()
			return out, e
		}
		out.Findings = append(out.Findings, *f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if len(out.Findings) > limit {
		out.Findings = out.Findings[:limit]
		out.Next = out.Findings[limit-1].ID.String()
	}
	ids := []string{}
	for _, f := range out.Findings {
		ids = append(ids, f.ID.String())
	}
	rows, err = tx.QueryContext(ctx, `SELECT finding_id,revision,document,updated_by,updated_at FROM patch_governance_tracking WHERE tenant_id=$1 AND finding_id=ANY($2::uuid[]) ORDER BY finding_id`, tenant, pq.Array(ids))
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var t PatchGovernanceTracking
		var raw []byte
		if err = rows.Scan(&t.FindingID, &t.Revision, &raw, &t.UpdatedBy, &t.UpdatedAt); err != nil {
			rows.Close()
			return out, err
		}
		if err = json.Unmarshal(raw, &t.Tracking); err != nil {
			rows.Close()
			return out, err
		}
		out.Tracking = append(out.Tracking, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) GetPatchReviewEventPage(ctx context.Context, tenant uuid.UUID, days int, before int64, limit int) ([]PatchGovernanceEvent, int64, error) {
	if tenant == uuid.Nil || days < 0 || days > 3650 || limit < 1 || limit > 100 || before < 0 {
		return nil, 0, fmt.Errorf("invalid history page")
	}
	var cutoff any
	if days > 0 {
		cutoff = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,finding_id,kind,revision,actor,document,created_at FROM patch_governance_events WHERE tenant_id=$1 AND ($2::timestamptz IS NULL OR created_at>=$2) AND ($3::bigint=0 OR id<$3) ORDER BY id DESC LIMIT $4`, tenant, cutoff, before, limit+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []PatchGovernanceEvent{}
	for rows.Next() {
		var v PatchGovernanceEvent
		if err = rows.Scan(&v.ID, &v.FindingID, &v.Kind, &v.Revision, &v.Actor, &v.Document, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	next := int64(0)
	if len(out) > limit {
		out = out[:limit]
		next = out[limit-1].ID
	}
	return out, next, rows.Err()
}

// WritePatchReviewExport serializes all sections from one database snapshot.
// No list endpoint limits apply. A complete manifest is written only after all
// rows and queries succeed. The HTTP layer spools it before sending headers.
func (s *Store) WritePatchReviewExport(ctx context.Context, tenant uuid.UUID, w io.Writer, archive *PatchReviewArchiveInput) error {
	if tenant == uuid.Nil {
		return fmt.Errorf("tenant required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var high int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM patch_governance_events WHERE tenant_id=$1`, tenant).Scan(&high); err != nil {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT jsonb_build_object('revision',revision,'policy',document,'updated_by',updated_by,'updated_at',updated_at) FROM patch_governance_policy WHERE tenant_id=$1`, tenant).Scan(&raw)
	if err == sql.ErrNoRows {
		raw = []byte("null")
	} else if err != nil {
		return err
	}
	days := 0
	var policy PatchGovernancePolicy
	if string(raw) != "null" {
		if err = json.Unmarshal(raw, &policy); err != nil {
			return err
		}
		days = policy.Policy.RetentionDays
	}
	enc := json.NewEncoder(w)
	var automationRaw []byte
	err = tx.QueryRowContext(ctx, `SELECT document->'automation' FROM patch_review_operations WHERE tenant_id=$1`, tenant).Scan(&automationRaw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var configured struct {
		Enabled bool   `json:"enabled"`
		Mode    string `json:"mode"`
	}
	if len(automationRaw) > 0 && json.Unmarshal(automationRaw, &configured) != nil {
		return fmt.Errorf("invalid stored automation policy")
	}
	effective := "recommend_only"
	if configured.Enabled {
		effective = configured.Mode
	}
	automationMeta, _ := json.Marshal(map[string]any{"effective_mode": effective, "installation_authorized": false, "authorization": "separate retained exact run; current gates required"})
	if _, err = io.WriteString(w, `{"format":2,"tenant_id":`); err != nil {
		return err
	}
	if err = enc.Encode(tenant); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"generated_at":`); err != nil {
		return err
	}
	if err = enc.Encode(now); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"policy":`+string(raw)+`,"automation":`+string(automationMeta)+`,"snapshot":"repeatable_read","event_scope":"all_retained","history_window_days":`); err != nil {
		return err
	}
	if err = enc.Encode(days); err != nil {
		return err
	}
	counts := map[string]int{}
	sections := []struct {
		name, query string
		args        []any
	}{
		{"operations", `SELECT jsonb_build_object('revision',revision,'policy',document,'updated_by',updated_by,'updated_at',updated_at) FROM patch_review_operations WHERE tenant_id=$1`, []any{tenant}},
		{"findings", `SELECT row_to_json(f) FROM node_vulnerability_findings f WHERE tenant_id=$1 AND resolved_at IS NULL ORDER BY id`, []any{tenant}},
		{"tracking", `SELECT jsonb_build_object('finding_id',finding_id,'revision',revision,'tracking',document,'updated_by',updated_by,'updated_at',updated_at) FROM patch_governance_tracking WHERE tenant_id=$1 ORDER BY finding_id`, []any{tenant}},
		{"events", `SELECT row_to_json(e) FROM patch_governance_events e WHERE tenant_id=$1 ORDER BY id`, []any{tenant}},
		{"approval_history", `SELECT jsonb_build_object('id',id,'deployment_id',deployment_id,'node_id',node_id,'mode',mode,'status',status,'created_at',created_at,'expires_at',expires_at,'approved_at',approved_at,'approved_by',approved_by,'policy_hash',policy_hash) FROM patch_approvals WHERE tenant_id=$1 ORDER BY created_at,id`, []any{tenant}},
		{"escalation_receipts", `SELECT row_to_json(e) FROM patch_review_escalation_receipts e WHERE tenant_id=$1 ORDER BY created_at,finding_id,recipient_id`, []any{tenant}},
		{"automation_runs", `SELECT row_to_json(r) FROM patch_automation_runs r WHERE tenant_id=$1 ORDER BY created_at,id`, []any{tenant}},
		{"organization_policy", `SELECT row_to_json(p) FROM patch_organization_policy p WHERE tenant_id=$1`, []any{tenant}},
		{"organization_history", `SELECT row_to_json(h) FROM patch_organization_history h WHERE tenant_id=$1 ORDER BY revision`, []any{tenant}},
		{"organization_approval_bindings",`SELECT row_to_json(b) FROM patch_organization_approval_bindings b WHERE tenant_id=$1 ORDER BY created_at,approval_id`,[]any{tenant}},
	}
	for _, section := range sections {
		if _, err = io.WriteString(w, `,"`+section.name+`":[`); err != nil {
			return err
		}
		rows, e := tx.QueryContext(ctx, section.query, section.args...)
		if e != nil {
			return e
		}
		count := 0
		for rows.Next() {
			var item json.RawMessage
			if e = rows.Scan(&item); e != nil {
				rows.Close()
				return e
			}
			if count > 0 {
				if _, e = io.WriteString(w, ","); e != nil {
					rows.Close()
					return e
				}
			}
			if e = enc.Encode(item); e != nil {
				rows.Close()
				return e
			}
			count++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		if _, err = io.WriteString(w, "]"); err != nil {
			return err
		}
		counts[section.name] = count
	}
	if archive != nil {
		archive.HighEventID = high
		archive.Counts = counts
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if _, err = io.WriteString(w, `,"counts":`); err != nil {
		return err
	}
	if err = enc.Encode(counts); err != nil {
		return err
	}
	_, err = io.WriteString(w, `,"complete":true}`)
	return err
}
