package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// GetNodeByMachineID returns a node identified by its stable machine_id for the tenant.
// This is the preferred dedup path for enrollment — hostname is a fallback for legacy
// agents that don't send a machine_id.
func (s *Store) GetNodeByMachineID(ctx context.Context, tenantID uuid.UUID, machineID string) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	machineID = strings.TrimSpace(machineID)
	if tenantID == uuid.Nil || machineID == "" {
		return nil, errors.New("tenant id and machine id are required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, hostname, os, arch, public_ip, machine_id, state, created_at, updated_at
		FROM nodes
		WHERE tenant_id = $1 AND machine_id = $2
		LIMIT 1
	`, tenantID, machineID)

	var node Node
	if err := row.Scan(&node.ID, &node.TenantID, &node.Hostname, &node.OS, &node.Arch, &node.PublicIP, &node.MachineID, &node.State, &node.CreatedAt, &node.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get node by machine id: %w", err)
	}

	return &node, nil
}

// RetireNode marks a node as retired without deleting it, preserving audit history.
// Returns sql.ErrNoRows if the node does not exist.
func (s *Store) RetireNode(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("node id is required")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET state = $2, updated_at = $3
		WHERE id = $1
	`, id, NodeStateRetired, s.clock())
	if err != nil {
		return fmt.Errorf("retire node: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retire node rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}
