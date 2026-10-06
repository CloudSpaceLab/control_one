package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/google/uuid"
)

type NetworkInventory struct {
	TargetID    uuid.UUID                `json:"target_id"`
	TenantID    uuid.UUID                `json:"tenant_id"`
	RefreshID   uuid.UUID                `json:"refresh_id"`
	State       string                   `json:"state"`
	AttemptedAt time.Time                `json:"attempted_at"`
	CompletedAt *time.Time               `json:"completed_at,omitempty"`
	Snapshot    *networkdevice.Inventory `json:"snapshot,omitempty"`
}
type NetworkInventoryConnection struct {
	TargetID   uuid.UUID
	TenantID   uuid.UUID
	Protocol   string
	Address    string
	Port       int
	Credential *ProviderCredential
}

var ErrInventoryBusy = errors.New("inventory refresh already in progress")

func (s *Store) GetNetworkInventoryConnection(ctx context.Context, id uuid.UUID, a TargetAccess) (*NetworkInventoryConnection, error) {
	if a.Permission != "targets.connect" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	var result NetworkInventoryConnection
	var credentialID uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT c.target_id,c.tenant_id,c.protocol,c.address,c.port,c.credential_id FROM network_target_connections c JOIN targets t ON t.id=c.target_id WHERE c.target_id=$1 AND t.lifecycle_state='active' AND `+targetAccessPredicate("c.tenant_id", 2, 3), id, a.UserID, a.Permission).Scan(&result.TargetID, &result.TenantID, &result.Protocol, &result.Address, &result.Port, &credentialID)
	if err != nil {
		return nil, err
	}
	result.Credential, err = s.GetNetworkCredential(ctx, credentialID, result.TenantID, a)
	if err != nil {
		return nil, err
	}
	if result.Credential == nil {
		return nil, sql.ErrNoRows
	}
	return &result, nil
}

func scanInventory(row rowScanner) (*NetworkInventory, error) {
	var result NetworkInventory
	var raw []byte
	err := row.Scan(&result.TargetID, &result.TenantID, &result.RefreshID, &result.State, &result.AttemptedAt, &result.CompletedAt, &raw)
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		result.Snapshot = &networkdevice.Inventory{}
		if err = json.Unmarshal(raw, result.Snapshot); err != nil {
			return nil, err
		}
	}
	return &result, nil
}

func (s *Store) GetNetworkInventory(ctx context.Context, id uuid.UUID, a TargetAccess) (*NetworkInventory, error) {
	if a.Permission != "targets.read" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	// An authorized target without a snapshot returns an explicit empty status.
	var tenant uuid.UUID
	err := s.db.QueryRowContext(ctx, `SELECT t.tenant_id FROM targets t WHERE t.id=$1 AND t.family='network_security' AND `+targetAccessPredicate("t.tenant_id", 2, 3), id, a.UserID, a.Permission).Scan(&tenant)
	if err != nil {
		return nil, err
	}
	result, err := scanInventory(s.db.QueryRowContext(ctx, `SELECT target_id,tenant_id,refresh_id,state,attempted_at,completed_at,snapshot FROM network_inventory WHERE target_id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return &NetworkInventory{TargetID: id, TenantID: tenant, State: "not_collected"}, nil
	}
	return result, err
}

func (s *Store) BeginNetworkInventory(ctx context.Context, id uuid.UUID, a TargetAccess) (*NetworkInventory, error) {
	if a.Permission != "targets.connect" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	result, err := scanInventory(s.db.QueryRowContext(ctx, `INSERT INTO network_inventory(target_id,tenant_id,refresh_id,state,attempted_at)
 SELECT c.target_id,c.tenant_id,$2,'refreshing',$3 FROM network_target_connections c JOIN targets t ON t.id=c.target_id WHERE c.target_id=$1 AND t.lifecycle_state='active' AND `+targetAccessPredicate("c.tenant_id", 4, 5)+`
 ON CONFLICT(target_id) DO UPDATE SET refresh_id=EXCLUDED.refresh_id,state='refreshing',attempted_at=EXCLUDED.attempted_at,completed_at=NULL
 WHERE network_inventory.state<>'refreshing' OR network_inventory.attempted_at < EXCLUDED.attempted_at-INTERVAL '2 minutes'
 RETURNING target_id,tenant_id,refresh_id,state,attempted_at,completed_at,snapshot`, id, uuid.New(), s.clock().UTC(), a.UserID, a.Permission))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInventoryBusy
	}
	return result, err
}

func (s *Store) FinishNetworkInventory(ctx context.Context, id, refreshID uuid.UUID, inventory networkdevice.Inventory) (*NetworkInventory, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var raw []byte
	if inventory.State == "inventory_ready" {
		raw, err = json.Marshal(inventory)
		if err != nil {
			return nil, err
		}
		if len(raw) > 4*1024*1024 {
			return nil, errors.New("inventory exceeds storage limit")
		}
	}
	result, err := scanInventory(tx.QueryRowContext(ctx, `UPDATE network_inventory SET state=$3,completed_at=$4,snapshot=COALESCE($5::jsonb,snapshot) WHERE target_id=$1 AND refresh_id=$2 AND state='refreshing' RETURNING target_id,tenant_id,refresh_id,state,attempted_at,completed_at,snapshot`, id, refreshID, inventory.State, s.clock().UTC(), raw))
	if err != nil {
		return nil, err
	}
	if inventory.State == "inventory_ready" {
		value := func(key string) string {
			f, ok := inventory.Facts[key]
			if !ok {
				return ""
			}
			v, _ := f.Value.(string)
			return v
		}
		_, err = tx.ExecContext(ctx, `UPDATE targets SET hostname=CASE WHEN $2<>'' THEN $2 ELSE hostname END,vendor=$3,model=$4,platform=$5,firmware=$6,serial=$7,collection_state='inventory_ready',reachability_state='reachable',last_observed_at=$8,last_successful_collection_at=$8 WHERE id=$1`, id, value("hostname"), value("vendor"), value("model"), value("platform"), value("firmware"), value("serial"), inventory.ObservedAt)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE targets SET collection_state=CASE WHEN last_successful_collection_at IS NOT NULL THEN 'stale' ELSE $2 END,reachability_state=CASE WHEN $2='unreachable' THEN 'unreachable' ELSE reachability_state END WHERE id=$1`, id, inventory.State)
	}
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}
