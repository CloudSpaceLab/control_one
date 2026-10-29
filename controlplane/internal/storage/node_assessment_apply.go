package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/lib/pq"
)

var ErrAssessmentSuperseded = errors.New("assessment input generation or attempt superseded")

// ApplyNodeAssessment commits findings, conservative resolution and the attempt
// record together. Inventory writers take the same node lock. This prevents a
// stale snapshot or a partially committed assessment from certifying a fix.
func (s *Store) ApplyNodeAssessment(ctx context.Context, a NodeAssessment, findings []VulnerabilityFinding, resolutions []ReconcileVulnerabilityFindingsParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var platform sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT os FROM nodes WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, a.NodeID, a.TenantID).Scan(&platform); err != nil {
		return fmt.Errorf("lock assessment node: %w", err)
	}
	var commitEvidence struct {
		Feeds      []offlinebundle.AssessmentFeedEvidence `json:"feeds"`
		Generation string                                 `json:"feed_generation"`
	}
	if a.Status != "partial" || json.Unmarshal(a.Evidence, &commitEvidence) != nil {
		return ErrAssessmentSuperseded
	}
	for _, feed := range commitEvidence.Feeds {
		if feed.ExpiresAt == nil || !feed.ExpiresAt.After(time.Now().UTC()) {
			return ErrAssessmentSuperseded
		}
	}
	if commitEvidence.Generation != "" {
		var published string
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM vulnerability_publication WHERE id=1`).Scan(&published); err != nil {
			return err
		}
		if published != commitEvidence.Generation {
			return ErrAssessmentSuperseded
		}
	}
	// Include the identical OS/application identity fields used by the matcher.
	rows, err := tx.QueryContext(ctx, `SELECT name,version,source,COALESCE(arch,''),'','' FROM node_packages WHERE node_id=$1
 UNION ALL SELECT name,version,ecosystem,'',purl,cpe FROM node_app_dependencies WHERE node_id=$1`, a.NodeID)
	if err != nil {
		return err
	}
	var inventory []offlinebundle.PackageInventory
	for rows.Next() {
		var p offlinebundle.PackageInventory
		p.Platform = platform.String
		if err := rows.Scan(&p.Name, &p.Version, &p.Source, &p.Arch, &p.PURL, &p.CPE); err != nil {
			rows.Close()
			return err
		}
		inventory = append(inventory, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if offlinebundle.VulnerabilityInventoryGeneration(inventory) != a.InventoryGeneration {
		return ErrAssessmentSuperseded
	}
	var newer bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_assessments WHERE tenant_id=$1 AND node_id=$2 AND (started_at,id)>($3,$4))`, a.TenantID, a.NodeID, a.StartedAt, a.ID).Scan(&newer); err != nil {
		return err
	}
	if newer {
		return ErrAssessmentSuperseded
	}
	for _, f := range findings {
		if f.TenantID != a.TenantID || f.NodeID != a.NodeID {
			return errors.New("assessment finding ownership mismatch")
		}
	}
	if len(findings) > 0 {
		if err := s.upsertVulnerabilityFindingsTx(ctx, tx, findings); err != nil {
			return err
		}
	}
	for _, r := range resolutions {
		if r.TenantID != a.TenantID || r.NodeID != a.NodeID || r.CVEIDs == nil || !r.ResolveHistoricalVersions {
			return errors.New("assessment resolution requires explicit fixed-boundary scope")
		}
		for _, scope := range r.PackageScopes {
			var fresh bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS (
			 SELECT 1 FROM node_packages p JOIN node_inventory_sync i ON i.node_id=p.node_id
			 WHERE p.node_id=$1 AND p.name=$2 AND p.version=$3 AND p.source=$4 AND COALESCE(p.arch,'')=$5
			 AND i.last_full_sync BETWEEN NOW()-INTERVAL '24 hours' AND NOW()
			 AND i.package_count=(SELECT count(*) FROM node_packages WHERE node_id=$1)
			) OR EXISTS (
			 SELECT 1 FROM node_app_dependencies d WHERE d.node_id=$1 AND d.name=$2 AND d.version=$3 AND d.ecosystem=$4
			 AND $5='' AND d.observed_at BETWEEN NOW()-INTERVAL '24 hours' AND NOW()
			)`, a.NodeID, scope.PackageName, scope.InstalledVersion, scope.PackageSource, scope.Arch).Scan(&fresh); err != nil {
				return err
			}
			if !fresh {
				return ErrAssessmentSuperseded
			}
		}
		if _, err := s.reconcileVulnerabilityFindings(ctx, tx, r); err != nil {
			return err
		}
	}
	if err := reconcileRemovedNativePackages(ctx, tx, &a, inventory); err != nil {
		return err
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT inventory_revision FROM nodes WHERE id=$1`, a.NodeID).Scan(&revision); err != nil {
		return err
	}
	var storedEvidence map[string]any
	if err := json.Unmarshal(a.Evidence, &storedEvidence); err != nil {
		return err
	}
	storedEvidence["inventory_revision"] = revision
	a.Evidence, err = json.Marshal(storedEvidence)
	if err != nil {
		return err
	}
	if err := recordNodeAssessment(ctx, tx, a); err != nil {
		return err
	}
	if err := advancePatchVerificationsTx(ctx, tx, a, inventory, findings); err != nil {
		return err
	}
	return tx.Commit()
}

func reconcileRemovedNativePackages(ctx context.Context, tx *sql.Tx, a *NodeAssessment, inventory []offlinebundle.PackageInventory) error {
	var evidence struct {
		NativeSnapshot      *packageinventory.Snapshot `json:"native_snapshot"`
		AssessedAdvisoryIDs []string                   `json:"assessed_advisory_ids"`
		FindingsUpdated     bool                       `json:"findings_updated"`
	}
	if json.Unmarshal(a.Evidence, &evidence) != nil || evidence.NativeSnapshot == nil || !evidence.FindingsUpdated || len(evidence.AssessedAdvisoryIDs) == 0 {
		return nil
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT snapshot FROM node_inventory_sync WHERE node_id=$1`, a.NodeID).Scan(&raw); err != nil {
		return err
	}
	var current packageinventory.Snapshot
	if json.Unmarshal(raw, &current) != nil || current != *evidence.NativeSnapshot {
		return ErrAssessmentSuperseded
	}
	var items []packageinventory.Item
	for _, p := range inventory {
		if p.Source == current.Scope {
			items = append(items, packageinventory.Item{Name: p.Name, Version: p.Version, Source: p.Source, Arch: p.Arch})
		}
	}
	if err := current.Validate(items, time.Now().UTC()); err != nil {
		return ErrAssessmentSuperseded
	}
	// Removed is independent of fixed-version comparison, but requires current
	// trusted advisory coverage and a complete native-manager observation.
	result, err := tx.ExecContext(ctx, `UPDATE node_vulnerability_findings f SET resolved_at=$3,evidence=COALESCE(evidence,'{}'::jsonb)||jsonb_build_object('resolution_basis','complete_native_package_removal','resolution_assessment_id',$7::text,'resolution_snapshot_sha256',$6::text,'resolution_observed_at',$3::timestamptz)
 WHERE f.tenant_id=$1 AND f.node_id=$2 AND f.evidence_source='offline_vulnerability_feed' AND f.package_source=$4 AND f.cve_id=ANY($5::text[]) AND f.resolved_at IS NULL AND f.last_seen_at<=$3
 AND NOT EXISTS(SELECT 1 FROM node_packages p WHERE p.node_id=f.node_id AND p.name=f.package_name AND p.source=f.package_source AND (f.arch='' OR COALESCE(p.arch,'')=f.arch))`, a.TenantID, a.NodeID, current.ObservedAt, current.Scope, pq.Array(evidence.AssessedAdvisoryIDs), current.SHA256, a.ID.String())
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	var document map[string]any
	if err = json.Unmarshal(a.Evidence, &document); err != nil {
		return err
	}
	document["removed_findings"] = n
	a.Evidence, err = json.Marshal(document)
	return err
}
