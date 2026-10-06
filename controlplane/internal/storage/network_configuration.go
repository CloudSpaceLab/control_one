package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/google/uuid"
)

type NetworkConfigurationSnapshot struct {
	ID             uuid.UUID `json:"id"`
	TenantID       uuid.UUID `json:"tenant_id"`
	TargetID       uuid.UUID `json:"target_id"`
	SourceID       uuid.UUID `json:"source_id"`
	SourceType     string    `json:"source_type"`
	Adapter        string    `json:"adapter"`
	AdapterVersion string    `json:"adapter_version"`
	Format         string    `json:"format"`
	Content        string    `json:"content"`
	ContentHash    string    `json:"content_hash"`
	Revision       int       `json:"revision"`
	ObservedAt     time.Time `json:"observed_at"`
	CreatedAt      time.Time `json:"created_at"`
}

type NetworkConfigurationHistory struct {
	TargetID  uuid.UUID                      `json:"target_id"`
	State     string                         `json:"state"`
	Snapshots []NetworkConfigurationSnapshot `json:"snapshots"`
}

func (s *Store) SaveNetworkConfigurationSnapshot(ctx context.Context, tenantID uuid.UUID, collector string, sourceID uuid.UUID, sourceType string, observedAt time.Time, snapshot networkdevice.ConfigurationSnapshot) (*NetworkConfigurationSnapshot, error) {
	if s.db == nil || tenantID == uuid.Nil || sourceID == uuid.Nil || collector == "" || observedAt.IsZero() || !snapshot.Sanitized {
		return nil, errors.New("invalid network configuration snapshot")
	}
	normalized, err := networkdevice.NormalizeConfigurationSnapshot(snapshot.Adapter, snapshot.AdapterVersion, snapshot.Format, snapshot.Content)
	if err != nil || normalized.ContentHash != snapshot.ContentHash {
		return nil, errors.New("configuration snapshot hash or sanitization is invalid")
	}
	if sourceType != "ssh_config" && sourceType != "netconf" && sourceType != "restconf" && sourceType != "vendor_api" {
		return nil, errors.New("source does not collect configuration snapshots")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var targetID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT n.target_id FROM network_telemetry_sources n
 JOIN content_pack_edge_collectors c ON c.tenant_id=n.tenant_id AND c.collector_id=n.collector_id
 JOIN targets t ON t.id=n.target_id AND t.tenant_id=n.tenant_id
 WHERE n.id=$1 AND n.tenant_id=$2 AND n.collector_id=$3 AND n.source_type=$4
 AND c.status!='disabled' AND t.lifecycle_state='active' FOR UPDATE OF n,t`, sourceID, tenantID, collector, sourceType).Scan(&targetID)
	if err != nil {
		return nil, err
	}
	// Serialize revisions for this target/source even when collectors retry.
	var locked uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM targets WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, targetID, tenantID).Scan(&locked); err != nil {
		return nil, err
	}
	var latestObserved time.Time
	var latestHash string
	err = tx.QueryRowContext(ctx, `SELECT observed_at,content_hash FROM network_configuration_snapshots WHERE target_id=$1 AND source_type=$2 ORDER BY revision DESC LIMIT 1`, targetID, sourceType).Scan(&latestObserved, &latestHash)
	if err == nil && (observedAt.Before(latestObserved) || (observedAt.Equal(latestObserved) && normalized.ContentHash != latestHash)) {
		return nil, errors.New("stale or conflicting configuration snapshot")
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var existing NetworkConfigurationSnapshot
	err = tx.QueryRowContext(ctx, `SELECT id,tenant_id,target_id,source_id,source_type,adapter,adapter_version,format,content,content_hash,revision,observed_at,created_at
 FROM network_configuration_snapshots WHERE target_id=$1 AND source_type=$2 AND content_hash=$3`, targetID, sourceType, normalized.ContentHash).
		Scan(&existing.ID, &existing.TenantID, &existing.TargetID, &existing.SourceID, &existing.SourceType, &existing.Adapter, &existing.AdapterVersion, &existing.Format, &existing.Content, &existing.ContentHash, &existing.Revision, &existing.ObservedAt, &existing.CreatedAt)
	if err == nil {
		if observedAt.After(existing.ObservedAt) {
			_, err = tx.ExecContext(ctx, `UPDATE network_configuration_snapshots SET observed_at=$2,source_id=$3,adapter=$4,adapter_version=$5 WHERE id=$1`, existing.ID, observedAt.UTC(), sourceID, normalized.Adapter, normalized.AdapterVersion)
			if err != nil {
				return nil, err
			}
			existing.ObservedAt = observedAt.UTC()
			existing.SourceID = sourceID
			existing.Adapter = normalized.Adapter
			existing.AdapterVersion = normalized.AdapterVersion
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return &existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var revision int
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM network_configuration_snapshots WHERE target_id=$1 AND source_type=$2`, targetID, sourceType).Scan(&revision); err != nil {
		return nil, err
	}
	var result NetworkConfigurationSnapshot
	err = tx.QueryRowContext(ctx, `INSERT INTO network_configuration_snapshots(id,tenant_id,target_id,source_id,source_type,adapter,adapter_version,format,content,content_hash,revision,observed_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
 RETURNING id,tenant_id,target_id,source_id,source_type,adapter,adapter_version,format,content,content_hash,revision,observed_at,created_at`, uuid.New(), tenantID, targetID, sourceID, sourceType, normalized.Adapter, normalized.AdapterVersion, normalized.Format, normalized.Content, normalized.ContentHash, revision, observedAt.UTC()).
		Scan(&result.ID, &result.TenantID, &result.TargetID, &result.SourceID, &result.SourceType, &result.Adapter, &result.AdapterVersion, &result.Format, &result.Content, &result.ContentHash, &result.Revision, &result.ObservedAt, &result.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("save network configuration snapshot: %w", err)
	}
	return &result, tx.Commit()
}

func (s *Store) GetNetworkConfigurationHistory(ctx context.Context, id uuid.UUID, access TargetAccess) (*NetworkConfigurationHistory, error) {
	target, err := s.GetTarget(ctx, id, access)
	if err != nil {
		return nil, err
	}
	if target == nil || target.Family != "network_security" {
		return nil, sql.ErrNoRows
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,tenant_id,target_id,source_id,source_type,adapter,adapter_version,format,content,content_hash,revision,observed_at,created_at
 FROM network_configuration_snapshots WHERE target_id=$1 ORDER BY revision DESC LIMIT 20`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &NetworkConfigurationHistory{TargetID: id, State: "not_collected", Snapshots: []NetworkConfigurationSnapshot{}}
	for rows.Next() {
		var snapshot NetworkConfigurationSnapshot
		if err := rows.Scan(&snapshot.ID, &snapshot.TenantID, &snapshot.TargetID, &snapshot.SourceID, &snapshot.SourceType, &snapshot.Adapter, &snapshot.AdapterVersion, &snapshot.Format, &snapshot.Content, &snapshot.ContentHash, &snapshot.Revision, &snapshot.ObservedAt, &snapshot.CreatedAt); err != nil {
			return nil, err
		}
		result.Snapshots = append(result.Snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(result.Snapshots)-1; i < j; i, j = i+1, j-1 {
		result.Snapshots[i], result.Snapshots[j] = result.Snapshots[j], result.Snapshots[i]
	}
	if len(result.Snapshots) > 0 {
		result.State = "ready"
	}
	return result, nil
}
