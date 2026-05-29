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
)

// AgentIngestReplayReceipt is a compact, persistent idempotency receipt for
// compatibility ingest paths that do not yet own the canonical event journal.
type AgentIngestReplayReceipt struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	NodeID    uuid.UUID
	Endpoint  string
	ReplayKey string
	Status    string
	Response  json.RawMessage
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpsertAgentIngestReplayReceiptParams struct {
	TenantID  uuid.UUID
	NodeID    uuid.UUID
	Endpoint  string
	ReplayKey string
	Status    string
	Response  json.RawMessage
}

func (s *Store) GetAgentIngestReplayReceipt(ctx context.Context, tenantID, nodeID uuid.UUID, endpoint, replayKey string) (*AgentIngestReplayReceipt, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	endpoint = strings.TrimSpace(endpoint)
	replayKey = strings.TrimSpace(replayKey)
	if tenantID == uuid.Nil || nodeID == uuid.Nil || endpoint == "" || replayKey == "" {
		return nil, errors.New("tenant_id, node_id, endpoint, and replay_key required")
	}
	var r AgentIngestReplayReceipt
	err := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, node_id, endpoint, replay_key, status, response_json, created_at, updated_at
		FROM agent_ingest_replay_receipts
		WHERE tenant_id = $1 AND node_id = $2 AND endpoint = $3 AND replay_key = $4
	`, tenantID, nodeID, endpoint, replayKey).Scan(
		&r.ID, &r.TenantID, &r.NodeID, &r.Endpoint, &r.ReplayKey, &r.Status,
		&r.Response, &r.CreatedAt, &r.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) UpsertAgentIngestReplayReceipt(ctx context.Context, p UpsertAgentIngestReplayReceiptParams) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	endpoint := strings.TrimSpace(p.Endpoint)
	replayKey := strings.TrimSpace(p.ReplayKey)
	status := strings.TrimSpace(p.Status)
	if status == "" {
		status = "accepted"
	}
	if p.TenantID == uuid.Nil || p.NodeID == uuid.Nil || endpoint == "" || replayKey == "" {
		return errors.New("tenant_id, node_id, endpoint, and replay_key required")
	}
	response := p.Response
	if len(response) == 0 {
		response = json.RawMessage(`{}`)
	}
	if !json.Valid(response) {
		return fmt.Errorf("response_json is not valid JSON")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_ingest_replay_receipts (
			id, tenant_id, node_id, endpoint, replay_key, status, response_json, created_at, updated_at
		)
		VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6, NOW(), NOW())
		ON CONFLICT (tenant_id, node_id, endpoint, replay_key) DO UPDATE
		   SET status = EXCLUDED.status,
		       response_json = EXCLUDED.response_json,
		       updated_at = NOW()
	`, p.TenantID, p.NodeID, endpoint, replayKey, status, response)
	return err
}
