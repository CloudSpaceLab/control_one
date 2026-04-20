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
		SELECT id, tenant_id, hostname, os, arch, public_ip, machine_id, state,
		       last_seen_at, first_scan_at, labels,
		       created_at, updated_at
		FROM nodes
		WHERE tenant_id = $1 AND machine_id = $2
		LIMIT 1
	`, tenantID, machineID)

	return scanNodeRow(row)
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

// SetNodeState transitions a node into the given lifecycle state. It is a
// low-level primitive used by the heartbeat/first-scan state machine + by the
// enrollment-pending reaper job. Callers are responsible for ensuring the
// target state is legal for the current state (the database will reject any
// value not listed in the nodes_state_check CHECK constraint).
func (s *Store) SetNodeState(ctx context.Context, id uuid.UUID, state string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("node id is required")
	}
	state = strings.TrimSpace(state)
	if state == "" {
		return errors.New("state is required")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET state = $2, updated_at = $3
		WHERE id = $1
	`, id, state, s.clock())
	if err != nil {
		return fmt.Errorf("set node state: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set node state rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// TouchNodeHeartbeat bumps nodes.last_seen_at to now. Called from the heartbeat
// endpoint. Returns the refreshed node so callers can inspect state/first_scan_at
// atomically without a second query — the mTLS heartbeat handler uses that
// snapshot to decide whether the node is ready to flip enrollment_pending ->
// active. Returns sql.ErrNoRows if the node does not exist.
func (s *Store) TouchNodeHeartbeat(ctx context.Context, id uuid.UUID) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("node id is required")
	}

	now := s.clock()
	row := s.db.QueryRowContext(ctx, `
		UPDATE nodes
		SET last_seen_at = $2, updated_at = $2
		WHERE id = $1
		RETURNING id, tenant_id, hostname, os, arch, public_ip, machine_id, state,
		          last_seen_at, first_scan_at, labels,
		          created_at, updated_at
	`, id, now)

	node, err := scanNodeRow(row)
	if err != nil {
		return nil, fmt.Errorf("touch heartbeat: %w", err)
	}
	if node == nil {
		return nil, sql.ErrNoRows
	}
	return node, nil
}

// MarkNodeFirstScan records the first_scan_at timestamp the first time a
// compliance result lands for a node. Subsequent calls are no-ops — we only
// stamp it once so the enrollment gate doesn't re-trigger on every scan.
// Returns the refreshed node (or sql.ErrNoRows if it does not exist).
func (s *Store) MarkNodeFirstScan(ctx context.Context, id uuid.UUID) (*Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return nil, errors.New("node id is required")
	}

	now := s.clock()
	// COALESCE preserves the existing timestamp so this is idempotent: the
	// first call writes `now`, every subsequent call is a no-op that still
	// returns the full row.
	row := s.db.QueryRowContext(ctx, `
		UPDATE nodes
		SET first_scan_at = COALESCE(first_scan_at, $2), updated_at = $2
		WHERE id = $1
		RETURNING id, tenant_id, hostname, os, arch, public_ip, machine_id, state,
		          last_seen_at, first_scan_at, labels,
		          created_at, updated_at
	`, id, now)

	node, err := scanNodeRow(row)
	if err != nil {
		return nil, fmt.Errorf("mark first scan: %w", err)
	}
	if node == nil {
		return nil, sql.ErrNoRows
	}
	return node, nil
}

// UpdateNodeLabels replaces the labels JSONB blob on a node atomically.
// A nil/empty map writes '{}' (never NULL) so downstream JSONB operators
// don't need NULL-safety. Returns sql.ErrNoRows if the node does not exist.
func (s *Store) UpdateNodeLabels(ctx context.Context, id uuid.UUID, labels map[string]any) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("node id is required")
	}

	payload := []byte("{}")
	if len(labels) > 0 {
		marshalled, err := json.Marshal(labels)
		if err != nil {
			return fmt.Errorf("marshal node labels: %w", err)
		}
		payload = marshalled
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET labels = $2, updated_at = $3
		WHERE id = $1
	`, id, payload, s.clock())
	if err != nil {
		return fmt.Errorf("update node labels: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update node labels rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListEnrollmentPendingNodesOlderThan returns nodes still stuck in
// enrollment_pending whose created_at is older than `cutoff`. This is the
// reaper query: the worker dispatcher scans this set every minute and flips
// survivors to enrollment_failed. time.Time is passed directly because we
// want caller-supplied clock injection in tests.
func (s *Store) ListEnrollmentPendingNodesOlderThan(ctx context.Context, cutoff time.Time) ([]Node, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, hostname, os, arch, public_ip, machine_id, state,
		       last_seen_at, first_scan_at, labels,
		       created_at, updated_at
		FROM nodes
		WHERE state = $1 AND created_at < $2
		ORDER BY created_at ASC
	`, NodeStateEnrollmentPending, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list pending nodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Node
	for rows.Next() {
		var (
			n           Node
			lastSeen    sql.NullTime
			firstScan   sql.NullTime
			labelsBytes []byte
		)
		if err := rows.Scan(
			&n.ID, &n.TenantID, &n.Hostname,
			&n.OS, &n.Arch, &n.PublicIP, &n.MachineID, &n.State,
			&lastSeen, &firstScan, &labelsBytes,
			&n.CreatedAt, &n.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending node: %w", err)
		}
		if lastSeen.Valid {
			t := lastSeen.Time
			n.LastSeenAt = &t
		}
		if firstScan.Valid {
			t := firstScan.Time
			n.FirstScanAt = &t
		}
		if len(labelsBytes) > 0 {
			var labels map[string]any
			if err := json.Unmarshal(labelsBytes, &labels); err != nil {
				return nil, fmt.Errorf("unmarshal pending labels: %w", err)
			}
			n.Labels = labels
		}
		if n.Labels == nil {
			n.Labels = map[string]any{}
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending nodes: %w", err)
	}

	return out, nil
}
