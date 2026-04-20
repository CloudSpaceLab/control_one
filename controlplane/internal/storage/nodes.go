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

// GetNodeCertHistory returns every certificate history row for a node ordered by
// issuance time (oldest first). Used by audits and tests to walk the replacement
// chain.
func (s *Store) GetNodeCertHistory(ctx context.Context, nodeID uuid.UUID) ([]NodeCertHistory, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_id, serial, issued_at, revoked_at, replaced_by
		FROM node_certificate_history
		WHERE node_id = $1
		ORDER BY issued_at ASC
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node cert history: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var history []NodeCertHistory
	for rows.Next() {
		var h NodeCertHistory
		if err := rows.Scan(&h.ID, &h.NodeID, &h.Serial, &h.IssuedAt, &h.RevokedAt, &h.ReplacedBy); err != nil {
			return nil, fmt.Errorf("scan node cert history row: %w", err)
		}
		history = append(history, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node cert history: %w", err)
	}
	return history, nil
}

// LatestNodeCertHistory returns the most recently issued (and not yet replaced)
// history row for a node, or (nil, nil) if none exist.
func (s *Store) LatestNodeCertHistory(ctx context.Context, nodeID uuid.UUID) (*NodeCertHistory, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, node_id, serial, issued_at, revoked_at, replaced_by
		FROM node_certificate_history
		WHERE node_id = $1 AND replaced_by IS NULL
		ORDER BY issued_at DESC
		LIMIT 1
	`, nodeID)

	var h NodeCertHistory
	if err := row.Scan(&h.ID, &h.NodeID, &h.Serial, &h.IssuedAt, &h.RevokedAt, &h.ReplacedBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest node cert history: %w", err)
	}
	return &h, nil
}

// RotateNodeCertificate atomically updates nodes.cert_serial + cert_rotated_at
// and inserts a new node_certificate_history row for the issued serial. When a
// previous unreplaced row exists, it is updated with replaced_by pointing at
// the new row so the lineage stays queryable. The returned NodeCertHistory
// row is the freshly inserted one.
func (s *Store) RotateNodeCertificate(ctx context.Context, nodeID uuid.UUID, serial string) (*NodeCertHistory, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	serial = strings.TrimSpace(serial)
	if serial == "" {
		return nil, errors.New("serial is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin rotate cert tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Verify the node exists before touching history.
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id = $1)`, nodeID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check node exists: %w", err)
	}
	if !exists {
		return nil, sql.ErrNoRows
	}

	now := s.clock().UTC()

	// Insert the new history row first so we have an id to chain from the predecessor.
	newID := uuid.New()
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO node_certificate_history (id, node_id, serial, issued_at)
		VALUES ($1, $2, $3, $4)
	`, newID, nodeID, serial, now); err != nil {
		return nil, fmt.Errorf("insert node cert history: %w", err)
	}

	// Chain: any prior unreplaced row becomes replaced_by = newID and revoked_at = now.
	if _, err := tx.ExecContext(ctx, `
		UPDATE node_certificate_history
		SET replaced_by = $2, revoked_at = $3
		WHERE node_id = $1 AND replaced_by IS NULL AND id <> $2
	`, nodeID, newID, now); err != nil {
		return nil, fmt.Errorf("chain replaced node cert history: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE nodes
		SET cert_serial = $2, cert_rotated_at = $3, updated_at = $3
		WHERE id = $1
	`, nodeID, serial, now); err != nil {
		return nil, fmt.Errorf("update node cert metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit rotate cert: %w", err)
	}
	committed = true

	return &NodeCertHistory{
		ID:       newID,
		NodeID:   nodeID,
		Serial:   serial,
		IssuedAt: now,
	}, nil
}
