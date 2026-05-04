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
		       last_seen_at, first_scan_at, labels, agent_version,
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

// ResetNodeForReenrollment atomically resets a node to enrollment_pending and
// clears last_seen_at + first_scan_at so the enrollment gate runs from scratch.
// Called when an existing node in enrollment_failed or retired state re-enrolls.
func (s *Store) ResetNodeForReenrollment(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("node id is required")
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET state         = $2,
		    last_seen_at  = NULL,
		    first_scan_at = NULL,
		    updated_at    = $3
		WHERE id = $1
	`, id, NodeStateEnrollmentPending, s.clock())
	if err != nil {
		return fmt.Errorf("reset node for re-enrollment: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("reset node for re-enrollment rows affected: %w", err)
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
		          last_seen_at, first_scan_at, labels, agent_version,
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
		          last_seen_at, first_scan_at, labels, agent_version,
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
		       last_seen_at, first_scan_at, labels, agent_version,
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
			&lastSeen, &firstScan, &labelsBytes, &n.AgentVersion,
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

// UpdateNodeAgentVersion persists the agent version string reported by the
// agent in heartbeat payloads. Idempotent — identical version strings are
// a no-op for updated_at to avoid spurious update timestamps.
func (s *Store) UpdateNodeAgentVersion(ctx context.Context, id uuid.UUID, version string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if id == uuid.Nil {
		return errors.New("node id is required")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE nodes
		SET agent_version = $2, updated_at = NOW()
		WHERE id = $1 AND (agent_version IS DISTINCT FROM $2)
	`, id, version)
	return err
}

// GetPendingAgentUpdateJob returns the first queued agent.update job whose
// payload targets the given nodeID, or nil if no such job exists.
func (s *Store) GetPendingAgentUpdateJob(ctx context.Context, nodeID uuid.UUID) (*Job, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, type, status, payload, retries, max_retries,
		       scheduled_at, started_at, finished_at, created_at, updated_at
		FROM jobs
		WHERE type = 'agent.update'
		  AND status = 'queued'
		  AND payload->>'node_id' = $1
		ORDER BY created_at ASC
		LIMIT 1
	`, nodeID.String())

	var (
		job       Job
		tenant    sql.NullString
		scheduled sql.NullTime
		started   sql.NullTime
		finished  sql.NullTime
	)
	if err := row.Scan(
		&job.ID, &tenant, &job.Type, &job.Status, &job.Payload,
		&job.Retries, &job.MaxRetries,
		&scheduled, &started, &finished,
		&job.CreatedAt, &job.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pending agent update job: %w", err)
	}
	if tenant.Valid {
		job.TenantID, _ = uuid.Parse(tenant.String)
	}
	if scheduled.Valid {
		t := scheduled.Time
		job.ScheduledAt = &t
	}
	if started.Valid {
		t := started.Time
		job.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time
		job.FinishedAt = &t
	}
	return &job, nil
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
