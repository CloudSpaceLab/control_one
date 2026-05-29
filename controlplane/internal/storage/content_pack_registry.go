package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

const (
	ContentPackRegistrySnapshotStatusActive     = "active"
	ContentPackRegistrySnapshotStatusSuperseded = "superseded"
)

type ContentPackRegistrySnapshotRecord struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	Status            string
	Source            string
	ControlOneVersion string
	PackCount         int
	Snapshot          contentpacks.RegistrySnapshot
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type SaveContentPackRegistrySnapshotParams struct {
	TenantID uuid.UUID
	Source   string
	Snapshot contentpacks.RegistrySnapshot
}

func (s *Store) SaveContentPackRegistrySnapshot(ctx context.Context, p SaveContentPackRegistrySnapshotParams) (*ContentPackRegistrySnapshotRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if p.Snapshot.ExportedAt.IsZero() {
		p.Snapshot.ExportedAt = s.clock().UTC()
	}
	snapshotBytes, err := marshalContentPackRegistrySnapshot(p.Snapshot)
	if err != nil {
		return nil, err
	}
	source := strings.TrimSpace(p.Source)
	if source == "" {
		source = "unspecified"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if _, err := tx.ExecContext(ctx, `
		UPDATE content_pack_registry_snapshots
		SET status = 'superseded', updated_at = NOW()
		WHERE tenant_id = $1 AND status = 'active'
	`, p.TenantID); err != nil {
		return nil, fmt.Errorf("supersede active content pack registry snapshot: %w", err)
	}

	var id uuid.UUID
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO content_pack_registry_snapshots (
			tenant_id, status, source, control_one_version, pack_count, snapshot, created_at, updated_at
		)
		VALUES ($1,'active',$2,$3,$4,$5::jsonb,NOW(),NOW())
		RETURNING id
	`, p.TenantID, source, strings.TrimSpace(p.Snapshot.ControlOneVersion), len(p.Snapshot.Packs), snapshotBytes).Scan(&id); err != nil {
		return nil, fmt.Errorf("insert content pack registry snapshot: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return s.GetContentPackRegistrySnapshot(ctx, id)
}

func (s *Store) ActiveContentPackRegistrySnapshot(ctx context.Context, tenantID uuid.UUID) (*ContentPackRegistrySnapshotRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	row := s.db.QueryRowContext(ctx, contentPackRegistrySnapshotSelectSQL+`
		WHERE tenant_id = $1 AND status = 'active'
		ORDER BY created_at DESC
		LIMIT 1
	`, tenantID)
	record, err := scanContentPackRegistrySnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return record, err
}

func (s *Store) GetContentPackRegistrySnapshot(ctx context.Context, id uuid.UUID) (*ContentPackRegistrySnapshotRecord, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("snapshot id is required")
	}
	row := s.db.QueryRowContext(ctx, contentPackRegistrySnapshotSelectSQL+` WHERE id = $1`, id)
	return scanContentPackRegistrySnapshot(row)
}

func (s *Store) ListContentPackRegistrySnapshots(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]ContentPackRegistrySnapshotRecord, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, 0, errors.New("tenant id is required")
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		return nil, 0, errors.New("offset must be non-negative")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM content_pack_registry_snapshots
		WHERE tenant_id = $1
	`, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count content pack registry snapshots: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, contentPackRegistrySnapshotSelectSQL+`
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query content pack registry snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]ContentPackRegistrySnapshotRecord, 0, limit)
	for rows.Next() {
		record, err := scanContentPackRegistrySnapshot(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

const contentPackRegistrySnapshotSelectSQL = `
	SELECT id, tenant_id, status, source, control_one_version, pack_count, snapshot, created_at, updated_at
	FROM content_pack_registry_snapshots
`

func scanContentPackRegistrySnapshot(row scanner) (*ContentPackRegistrySnapshotRecord, error) {
	var record ContentPackRegistrySnapshotRecord
	var snapshotRaw []byte
	if err := row.Scan(
		&record.ID,
		&record.TenantID,
		&record.Status,
		&record.Source,
		&record.ControlOneVersion,
		&record.PackCount,
		&snapshotRaw,
		&record.CreatedAt,
		&record.UpdatedAt,
	); err != nil {
		return nil, err
	}
	snapshot, err := decodeContentPackRegistrySnapshot(snapshotRaw)
	if err != nil {
		return nil, err
	}
	record.Snapshot = snapshot
	return &record, nil
}

func marshalContentPackRegistrySnapshot(snapshot contentpacks.RegistrySnapshot) ([]byte, error) {
	if snapshot.SchemaVersion != contentpacks.SchemaVersion {
		return nil, fmt.Errorf("unsupported content pack registry snapshot schema_version %d", snapshot.SchemaVersion)
	}
	if _, err := contentpacks.NewRegistryFromSnapshot(snapshot, ""); err != nil {
		return nil, err
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("marshal content pack registry snapshot: %w", err)
	}
	return data, nil
}

func decodeContentPackRegistrySnapshot(raw []byte) (contentpacks.RegistrySnapshot, error) {
	var snapshot contentpacks.RegistrySnapshot
	if len(raw) == 0 {
		return snapshot, fmt.Errorf("content pack registry snapshot is empty")
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return snapshot, fmt.Errorf("unmarshal content pack registry snapshot: %w", err)
	}
	if _, err := contentpacks.NewRegistryFromSnapshot(snapshot, ""); err != nil {
		return snapshot, err
	}
	return snapshot, nil
}
