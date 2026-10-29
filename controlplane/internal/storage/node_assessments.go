package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodeAssessment records an attempt, not a declaration that the host is clean.
// Evidence contains the inventory/feed identities, limitations and outcome.
type NodeAssessment struct {
	ID                  uuid.UUID       `json:"id"`
	TenantID            uuid.UUID       `json:"tenant_id"`
	NodeID              uuid.UUID       `json:"node_id"`
	StartedAt           time.Time       `json:"started_at"`
	CompletedAt         time.Time       `json:"completed_at"`
	Status              string          `json:"status"`
	InventoryGeneration string          `json:"inventory_generation"`
	Evidence            json.RawMessage `json:"evidence"`
}

func (s *Store) RecordNodeAssessment(ctx context.Context, a NodeAssessment) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	return recordNodeAssessment(ctx, s.db, a)
}

func recordNodeAssessment(ctx context.Context, db assessmentExecer, a NodeAssessment) error {
	if a.ID == uuid.Nil || a.TenantID == uuid.Nil || a.NodeID == uuid.Nil || a.StartedAt.IsZero() || a.CompletedAt.Before(a.StartedAt) || !json.Valid(a.Evidence) {
		return errors.New("invalid assessment evidence")
	}
	// Verify ownership within the insert; a node belonging to another tenant
	// cannot acquire evidence under the caller's tenant.
	result, err := db.ExecContext(ctx, `INSERT INTO node_assessments
  (id,tenant_id,node_id,started_at,completed_at,status,inventory_generation,evidence)
  SELECT $1,$2,$3,$4,$5,$6,$7,$8::jsonb FROM nodes WHERE id=$3 AND tenant_id=$2`,
		a.ID, a.TenantID, a.NodeID, a.StartedAt, a.CompletedAt, a.Status, a.InventoryGeneration, []byte(a.Evidence))
	if err != nil {
		return fmt.Errorf("record node assessment: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("assessment node/tenant mismatch")
	}
	return nil
}

func (s *Store) GetLatestNodeAssessment(ctx context.Context, tenantID, nodeID uuid.UUID) (*NodeAssessment, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	var a NodeAssessment
	err := s.db.QueryRowContext(ctx, `SELECT id,tenant_id,node_id,started_at,completed_at,status,inventory_generation,evidence
  FROM node_assessments WHERE tenant_id=$1 AND node_id=$2 ORDER BY started_at DESC,id DESC LIMIT 1`, tenantID, nodeID).
		Scan(&a.ID, &a.TenantID, &a.NodeID, &a.StartedAt, &a.CompletedAt, &a.Status, &a.InventoryGeneration, &a.Evidence)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read node assessment: %w", err)
	}
	return &a, nil
}
