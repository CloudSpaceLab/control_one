package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
	"github.com/CloudSpaceLab/control_one/internal/detections"
)

type ContentPackDetectionArtifact struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	RegistrySnapshotID uuid.UUID
	PackID             string
	PackVersion        string
	SourceID           string
	DetectionID        string
	Detection          contentpacks.Detection
	Rule               detections.Rule
	LoadedAt           time.Time
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type ReplaceContentPackDetectionArtifactsParams struct {
	TenantID           uuid.UUID
	RegistrySnapshotID uuid.UUID
	Artifacts          []ContentPackDetectionArtifact
}

func (s *Store) ReplaceContentPackDetectionArtifacts(ctx context.Context, p ReplaceContentPackDetectionArtifactsParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return errors.New("tenant id is required")
	}
	if p.RegistrySnapshotID == uuid.Nil {
		return errors.New("registry snapshot id is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin detection artifact replace: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM content_pack_detection_artifacts
		WHERE tenant_id = $1 AND registry_snapshot_id = $2
	`, p.TenantID, p.RegistrySnapshotID); err != nil {
		return fmt.Errorf("delete detection artifacts: %w", err)
	}
	for _, artifact := range p.Artifacts {
		normalized, detectionJSON, ruleJSON, err := normalizeContentPackDetectionArtifact(p.TenantID, p.RegistrySnapshotID, artifact)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO content_pack_detection_artifacts (
				tenant_id, registry_snapshot_id, pack_id, pack_version, source_id, detection_id,
				detection_json, rule_json, loaded_at, created_at, updated_at
			)
			VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb,$8::jsonb,$9,NOW(),NOW())
			ON CONFLICT (tenant_id, registry_snapshot_id, pack_id, pack_version, source_id, detection_id) DO UPDATE
			SET detection_json = EXCLUDED.detection_json,
			    rule_json = EXCLUDED.rule_json,
			    loaded_at = EXCLUDED.loaded_at,
			    updated_at = NOW()
		`, normalized.TenantID, normalized.RegistrySnapshotID, normalized.PackID, normalized.PackVersion, normalized.SourceID, normalized.DetectionID, detectionJSON, ruleJSON, normalized.LoadedAt); err != nil {
			return fmt.Errorf("insert detection artifact %s/%s: %w", normalized.SourceID, normalized.DetectionID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

func (s *Store) ListContentPackDetectionArtifacts(ctx context.Context, tenantID, registrySnapshotID uuid.UUID) ([]ContentPackDetectionArtifact, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if registrySnapshotID == uuid.Nil {
		return nil, errors.New("registry snapshot id is required")
	}
	rows, err := s.db.QueryContext(ctx, contentPackDetectionArtifactSelectSQL+`
		WHERE tenant_id = $1 AND registry_snapshot_id = $2
		ORDER BY source_id, detection_id, pack_id, pack_version
	`, tenantID, registrySnapshotID)
	if err != nil {
		return nil, fmt.Errorf("query content pack detection artifacts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []ContentPackDetectionArtifact
	for rows.Next() {
		artifact, err := scanContentPackDetectionArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *artifact)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func normalizeContentPackDetectionArtifact(tenantID, snapshotID uuid.UUID, artifact ContentPackDetectionArtifact) (ContentPackDetectionArtifact, []byte, []byte, error) {
	artifact.TenantID = tenantID
	artifact.RegistrySnapshotID = snapshotID
	artifact.PackID = strings.TrimSpace(artifact.PackID)
	artifact.PackVersion = strings.TrimSpace(artifact.PackVersion)
	artifact.SourceID = strings.TrimSpace(artifact.SourceID)
	artifact.DetectionID = strings.TrimSpace(artifact.DetectionID)
	if artifact.LoadedAt.IsZero() {
		artifact.LoadedAt = time.Now().UTC()
	}
	if artifact.PackID == "" || artifact.PackVersion == "" || artifact.SourceID == "" || artifact.DetectionID == "" {
		return ContentPackDetectionArtifact{}, nil, nil, errors.New("pack_id, pack_version, source_id, and detection_id are required")
	}
	if err := artifact.Rule.Validate(); err != nil {
		return ContentPackDetectionArtifact{}, nil, nil, fmt.Errorf("invalid detection rule %s: %w", artifact.DetectionID, err)
	}
	detectionJSON, err := json.Marshal(artifact.Detection)
	if err != nil {
		return ContentPackDetectionArtifact{}, nil, nil, fmt.Errorf("marshal detection metadata: %w", err)
	}
	ruleJSON, err := json.Marshal(artifact.Rule)
	if err != nil {
		return ContentPackDetectionArtifact{}, nil, nil, fmt.Errorf("marshal detection rule: %w", err)
	}
	return artifact, detectionJSON, ruleJSON, nil
}

const contentPackDetectionArtifactSelectSQL = `
	SELECT id, tenant_id, registry_snapshot_id, pack_id, pack_version, source_id, detection_id,
	       detection_json, rule_json, loaded_at, created_at, updated_at
	FROM content_pack_detection_artifacts
`

func scanContentPackDetectionArtifact(row scanner) (*ContentPackDetectionArtifact, error) {
	var artifact ContentPackDetectionArtifact
	var detectionJSON, ruleJSON []byte
	if err := row.Scan(
		&artifact.ID,
		&artifact.TenantID,
		&artifact.RegistrySnapshotID,
		&artifact.PackID,
		&artifact.PackVersion,
		&artifact.SourceID,
		&artifact.DetectionID,
		&detectionJSON,
		&ruleJSON,
		&artifact.LoadedAt,
		&artifact.CreatedAt,
		&artifact.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(detectionJSON, &artifact.Detection); err != nil {
		return nil, fmt.Errorf("decode detection metadata: %w", err)
	}
	if err := json.Unmarshal(ruleJSON, &artifact.Rule); err != nil {
		return nil, fmt.Errorf("decode detection rule: %w", err)
	}
	if err := artifact.Rule.Validate(); err != nil {
		return nil, fmt.Errorf("stored detection rule is invalid: %w", err)
	}
	return &artifact, nil
}
