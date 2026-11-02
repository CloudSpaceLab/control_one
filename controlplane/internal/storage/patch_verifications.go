package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

type PatchVerification struct {
	JobID             uuid.UUID       `json:"job_id"`
	TenantID          uuid.UUID       `json:"tenant_id"`
	NodeID            uuid.UUID       `json:"node_id"`
	DeploymentID      uuid.UUID       `json:"deployment_id"`
	State             string          `json:"state"`
	CurrentState      string          `json:"current_state"`
	Baseline          json.RawMessage `json:"baseline"`
	Evidence          json.RawMessage `json:"evidence"`
	CompletedAt       *time.Time      `json:"execution_completed_at,omitempty"`
	UpdatedAt         time.Time       `json:"updated_at"`
	PromotionEligible bool            `json:"promotion_eligible"`
	CurrentReasons    []string        `json:"current_reasons"`
}

func createPatchVerificationTx(ctx context.Context, tx *sql.Tx, jobID, tenantID, nodeID, deploymentID uuid.UUID, payload []byte) error {
	var policy struct {
		Allow []string `json:"package_allowlist"`
		Deny  []string `json:"package_denylist"`
	}
	if err := json.Unmarshal(payload, &policy); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO patch_verifications(job_id,tenant_id,node_id,deployment_id,state,baseline)
 SELECT $1,$2,$3,$4,'execution_pending',COALESCE(jsonb_agg(jsonb_build_object('name',f.package_name,'source',f.package_source,'arch',f.arch,'cve_id',f.cve_id,'installed_version',f.installed_version,'fixed_version',f.fixed_version,'advisory_url',f.advisory_url,'finding_id',f.id,'provenance',f.evidence)),'[]'::jsonb)
 FROM node_vulnerability_findings f WHERE f.tenant_id=$2 AND f.node_id=$3 AND f.resolved_at IS NULL
 AND (COALESCE(cardinality($5::text[]),0)=0 OR f.package_name=ANY($5::text[])) AND NOT(f.package_name=ANY(COALESCE($6::text[],'{}'::text[])))
 AND (EXISTS(SELECT 1 FROM node_packages p WHERE p.node_id=f.node_id AND p.name=f.package_name AND p.version=f.installed_version AND p.source=f.package_source AND COALESCE(p.arch,'')=f.arch)
 OR EXISTS(SELECT 1 FROM node_app_dependencies d WHERE d.node_id=f.node_id AND d.name=f.package_name AND d.version=f.installed_version AND d.ecosystem=f.package_source AND f.arch=''))
 ON CONFLICT(job_id) DO NOTHING`, jobID, tenantID, nodeID, deploymentID, pq.Array(policy.Allow), pq.Array(policy.Deny))
	return err
}

func completePatchVerificationTx(ctx context.Context, tx *sql.Tx, p CompletePatchJobParams, deploymentID, tenantID uuid.UUID) error {
	state := "execution_failed"
	if p.Succeeded {
		state = "verification_pending"
	}
	var health any
	if len(p.Health) > 0 && json.Valid(p.Health) {
		health = []byte(p.Health)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO patch_verifications(job_id,tenant_id,node_id,deployment_id,state,baseline,execution_completed_at,execution_health,evidence)
 VALUES($1,$2,$3,$4,$5,'[]',NOW(),$6,'{"reason":"legacy_no_security_baseline"}') ON CONFLICT(job_id) DO UPDATE SET state=$5,execution_completed_at=NOW(),execution_health=$6,evidence=jsonb_build_object('reason','later_inventory_assessment_and_health_required'),updated_at=NOW()
 WHERE patch_verifications.execution_completed_at IS NULL`, p.JobID, tenantID, p.NodeID, deploymentID, state, health)
	return err
}

func (s *Store) RecordPatchHealth(ctx context.Context, nodeID uuid.UUID, h packageinventory.Health) error {
	if h.ObservedAt.IsZero() || h.ObservedAt.After(time.Now().UTC()) || time.Since(h.ObservedAt) > 5*time.Minute {
		h = packageinventory.Health{ObservedAt: time.Now().UTC(), PackageAudit: "unknown", ServiceHealth: "unknown"}
	}
	raw, err := json.Marshal(h)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO node_patch_health(node_id,observed_at,evidence) VALUES($1,$2,$3) ON CONFLICT(node_id) DO UPDATE SET observed_at=$2,evidence=$3 WHERE node_patch_health.observed_at<$2`, nodeID, h.ObservedAt, raw)
	return err
}

func (s *Store) GetNodePatchHealth(ctx context.Context, nodeID uuid.UUID) (*packageinventory.Health, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT evidence FROM node_patch_health WHERE node_id=$1`, nodeID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var h packageinventory.Health
	if err = json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

func advancePatchVerificationsTx(ctx context.Context, tx *sql.Tx, a NodeAssessment, inventory []offlinebundle.PackageInventory, findings []VulnerabilityFinding) error {
	var evidence struct {
		Snapshot *packageinventory.Snapshot             `json:"native_snapshot"`
		Covered  []string                               `json:"assessed_advisory_ids"`
		Fixed    []offlinebundle.VerificationScope      `json:"verified_fixed_scopes"`
		Updated  bool                                   `json:"findings_updated"`
		Feeds    []offlinebundle.AssessmentFeedEvidence `json:"feeds"`
	}
	if err := json.Unmarshal(a.Evidence, &evidence); err != nil {
		return err
	}
	var healthRaw []byte
	err := tx.QueryRowContext(ctx, `SELECT evidence FROM node_patch_health WHERE node_id=$1`, a.NodeID).Scan(&healthRaw)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	var health *packageinventory.Health
	if len(healthRaw) > 0 {
		health = &packageinventory.Health{}
		if err = json.Unmarshal(healthRaw, health); err != nil {
			return err
		}
	}
	var items []packageinventory.Item
	if evidence.Snapshot != nil {
		for _, p := range inventory {
			if p.Source == evidence.Snapshot.Scope {
				items = append(items, packageinventory.Item{Name: p.Name, Version: p.Version, Source: p.Source, Arch: p.Arch})
			}
		}
	}
	active := make([]offlinebundle.VerificationScope, 0, len(findings))
	for _, f := range findings {
		active = append(active, offlinebundle.VerificationScope{Name: f.PackageName, Source: f.PackageSource, Arch: f.Arch, CVEID: f.CVEID})
	}
	trusted := evidence.Updated && a.Status == "partial" && len(evidence.Feeds) > 0
	for _, f := range evidence.Feeds {
		if f.ExpiresAt == nil || !f.ExpiresAt.After(time.Now().UTC()) {
			trusted = false
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT job_id,baseline,execution_completed_at,execution_health FROM patch_verifications WHERE node_id=$1 AND tenant_id=$2 AND execution_completed_at IS NOT NULL AND state<>'execution_failed' FOR UPDATE`, a.NodeID, a.TenantID)
	if err != nil {
		return err
	}
	type pending struct {
		id              uuid.UUID
		baseline        []byte
		completed       time.Time
		executionHealth []byte
	}
	var pendingRows []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.baseline, &p.completed, &p.executionHealth); err != nil {
			rows.Close()
			return err
		}
		pendingRows = append(pendingRows, p)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err = rows.Close(); err != nil {
		return err
	}
	for _, p := range pendingRows {
		input := offlinebundle.VerificationInput{CompletedAt: p.completed, Now: time.Now().UTC(), Snapshot: evidence.Snapshot, Health: health, Inventory: items, Fixed: evidence.Fixed, Active: active, Covered: evidence.Covered, FeedTrusted: trusted}
		if err = json.Unmarshal(p.baseline, &input.Baseline); err != nil {
			return err
		}
		if len(p.executionHealth) > 0 {
			input.ExecutionHealth = &packageinventory.Health{}
			if err = json.Unmarshal(p.executionHealth, input.ExecutionHealth); err != nil {
				return err
			}
		}
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM patch_verifications WHERE node_id=$1 AND execution_completed_at>$2)`, a.NodeID, p.completed).Scan(&input.Superseded); err != nil {
			return err
		}
		state, reason := offlinebundle.EvaluatePatchVerification(input)
		raw, err := json.Marshal(map[string]any{"reason": reason, "assessment_id": a.ID, "inventory_generation": a.InventoryGeneration, "feed_evidence": evidence.Feeds, "health": health, "snapshot": evidence.Snapshot})
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE patch_verifications SET state=$2,assessment_id=$3,evidence=$4,updated_at=NOW() WHERE job_id=$1`, p.id, state, a.ID, raw); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO patch_verification_events(id,job_id,assessment_id,state,evidence) VALUES($1,$2,$3,$4,$5) ON CONFLICT(job_id,assessment_id) DO NOTHING`, uuid.New(), p.id, a.ID, state, raw); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListNodePatchVerifications(ctx context.Context, tenantID, nodeID uuid.UUID) ([]PatchVerification, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT job_id,tenant_id,node_id,deployment_id,state,baseline,evidence,execution_completed_at,updated_at FROM patch_verifications WHERE tenant_id=$1 AND node_id=$2 ORDER BY updated_at DESC LIMIT 50`, tenantID, nodeID)
	if err != nil {
		return nil, err
	}
	var out []PatchVerification
	for rows.Next() {
		var v PatchVerification
		if err = rows.Scan(&v.JobID, &v.TenantID, &v.NodeID, &v.DeploymentID, &v.State, &v.Baseline, &v.Evidence, &v.CompletedAt, &v.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		v.CurrentState = v.State
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range out {
		v := &out[i]
		v.PromotionEligible, err = s.PatchVerificationAllowsPromotion(ctx, v.JobID)
		if err != nil {
			return nil, err
		}
		if (v.State == "verified_fixed" || v.State == "verified_removed") && !v.PromotionEligible {
			v.CurrentState = "verification_unknown"
			v.CurrentReasons = append(v.CurrentReasons, "verification_inputs_changed_expired_or_health_unavailable")
		}
		if v.State != "execution_failed" && v.CompletedAt != nil && time.Since(*v.CompletedAt) > 24*time.Hour {
			v.CurrentState = "verification_unknown"
			v.PromotionEligible = false
			v.CurrentReasons = append(v.CurrentReasons, "verification_deadline_expired")
		}
	}
	return out, nil
}

func (s *Store) PatchVerificationAllowsPromotion(ctx context.Context, jobID uuid.UUID) (bool, error) {
	var ready bool
	err := s.db.QueryRowContext(ctx, patchPromotionQuery, jobID).Scan(&ready)
	return ready, err
}

const patchPromotionQuery = `SELECT EXISTS(SELECT 1 FROM patch_verifications v JOIN nodes n ON n.id=v.node_id JOIN node_assessments a ON a.id=v.assessment_id JOIN node_inventory_sync i ON i.node_id=v.node_id JOIN node_patch_health h ON h.node_id=v.node_id JOIN vulnerability_publication f ON f.id=1
 WHERE v.job_id=$1 AND v.state IN ('verified_fixed','verified_removed') AND v.execution_completed_at>NOW()-INTERVAL '24 hours'
 AND a.id=(SELECT id FROM node_assessments WHERE node_id=v.node_id ORDER BY started_at DESC,id DESC LIMIT 1)
 AND (a.evidence->>'inventory_revision')::bigint=n.inventory_revision
 AND a.evidence->>'feed_generation'=f.generation AND i.snapshot->>'sha256'=a.evidence->'native_snapshot'->>'sha256'
 AND jsonb_array_length(a.evidence->'feeds')>0 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(a.evidence->'feeds') fe WHERE fe->>'expires_at' IS NULL OR (fe->>'expires_at')::timestamptz<=NOW())
 AND h.observed_at BETWEEN NOW()-INTERVAL '5 minutes' AND NOW() AND h.evidence->>'package_audit'='healthy' AND h.evidence->>'service_health'='healthy' AND h.evidence->>'reboot_required'='false'
 AND h.evidence->>'services_complete'='true' AND h.evidence->>'consumers_complete'='true'
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements_text(COALESCE(v.execution_health->'active_services','[]'::jsonb)) AS units(unit) WHERE COALESCE(h.evidence->'service_checks'->>units.unit,'unknown')<>'active')
 AND h.evidence->>'boot_id'=v.evidence->'health'->>'boot_id'
 AND NOT EXISTS(SELECT 1 FROM patch_verifications newer WHERE newer.node_id=v.node_id AND newer.execution_completed_at>v.execution_completed_at))`
