package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
	"github.com/google/uuid"
)

// ReplaceNativePackageSnapshot makes the package rows and completeness evidence
// one commit. Legacy replacement invalidates this evidence under the same lock.
func (s *Store) ReplaceNativePackageSnapshot(ctx context.Context, nodeID uuid.UUID, items []packageinventory.Item, snapshot packageinventory.Snapshot, legacyHash string) error {
	if s.db == nil || nodeID == uuid.Nil {
		return errors.New("snapshot store/node required")
	}
	if err := snapshot.Validate(items, time.Now().UTC()); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT tenant_id FROM nodes WHERE id=$1 FOR UPDATE`, nodeID).Scan(&owner); err != nil {
		return err
	}
	var raw []byte
	var lastFullSync time.Time
	err = tx.QueryRowContext(ctx, `SELECT snapshot,last_full_sync FROM node_inventory_sync WHERE node_id=$1`, nodeID).Scan(&raw, &lastFullSync)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if snapshot.ObservedAt.Before(lastFullSync) {
		return ErrAssessmentSuperseded
	}
	var previous packageinventory.Snapshot
	if len(raw) > 0 && json.Unmarshal(raw, &previous) != nil {
		return errors.New("stored snapshot evidence invalid")
	}
	if !previous.ObservedAt.IsZero() && (snapshot.ObservedAt.Before(previous.ObservedAt) || snapshot.ObservedAt.Equal(previous.ObservedAt) && snapshot.SHA256 != previous.SHA256) {
		return ErrAssessmentSuperseded
	}
	if previous.Scope != "" && previous.Scope != snapshot.Scope {
		return errors.New("native collector scope changed; explicit recovery required")
	}
	if _,err=tx.ExecContext(ctx,`UPDATE nodes SET inventory_revision=inventory_revision+1 WHERE id=$1`,nodeID);err!=nil{return err}
	if _, err = tx.ExecContext(ctx, `DELETE FROM node_packages WHERE node_id=$1`, nodeID); err != nil {
		return err
	}
	for _, item := range items {
		if _, err = tx.ExecContext(ctx, `INSERT INTO node_packages(node_id,name,version,source,arch,observed_at) VALUES($1,$2,$3,$4,$5,$6)`, nodeID, item.Name, item.Version, item.Source, item.Arch, snapshot.ObservedAt); err != nil {
			return fmt.Errorf("save native package: %w", err)
		}
	}
	raw, err = json.Marshal(snapshot)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO node_inventory_sync(node_id,package_hash,package_count,last_full_sync,last_seen_at,snapshot)
 VALUES($1,$2,$3,$4,NOW(),$5) ON CONFLICT(node_id) DO UPDATE SET package_hash=$2,package_count=$3,last_full_sync=$4,last_seen_at=NOW(),snapshot=$5`, nodeID, legacyHash, len(items), snapshot.ObservedAt, raw)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetNativePackageSnapshot(ctx context.Context, nodeID uuid.UUID) (*packageinventory.Snapshot, error) {
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT snapshot FROM node_inventory_sync WHERE node_id=$1`, nodeID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) || err == nil && len(raw) == 0 {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var snapshot packageinventory.Snapshot
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func(s *Store)GetNodeInventoryRevision(ctx context.Context,nodeID uuid.UUID)(int64,error){var revision int64;err:=s.db.QueryRowContext(ctx,`SELECT inventory_revision FROM nodes WHERE id=$1`,nodeID).Scan(&revision);return revision,err}

func (s *Store) InvalidateNativePackageSnapshot(ctx context.Context, nodeID uuid.UUID, observed time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE id=$1 FOR UPDATE`, nodeID).Scan(&id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_inventory_sync SET snapshot=NULL WHERE node_id=$1 AND ($2::timestamptz='0001-01-01'::timestamptz OR snapshot IS NULL OR (snapshot->>'observed_at')::timestamptz<=$2)`, nodeID, observed)
	if err != nil {
		return err
	}
	return tx.Commit()
}
