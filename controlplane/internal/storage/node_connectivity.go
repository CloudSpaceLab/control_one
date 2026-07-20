package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodeConnectivityTest represents a pending or completed connectivity test for a node.
type NodeConnectivityTest struct {
	ID           uuid.UUID
	NodeID       uuid.UUID
	TenantID     uuid.UUID
	JobID        *uuid.UUID
	TargetIP     string
	TargetPort   int
	Protocol     string
	TimeoutMs    int
	Status       string
	Reachable    sql.NullBool
	ErrorMessage sql.NullString
	CreatedAt    time.Time
	CompletedAt  sql.NullTime
}

// CreateNodeConnectivityTest inserts a new connectivity test record.
func (s *Store) CreateNodeConnectivityTest(ctx context.Context, test NodeConnectivityTest) (*NodeConnectivityTest, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if test.ID == uuid.Nil {
		test.ID = uuid.New()
	}
	if test.Status == "" {
		test.Status = "pending"
	}
	if test.Protocol == "" {
		test.Protocol = "tcp"
	}
	if test.TimeoutMs <= 0 {
		test.TimeoutMs = 5000
	}

	var jobIDVal any
	if test.JobID != nil {
		jobIDVal = *test.JobID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO node_connectivity_tests
			(id, node_id, tenant_id, job_id, target_ip, target_port, protocol, timeout_ms, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, test.ID, test.NodeID, test.TenantID, jobIDVal,
		test.TargetIP, test.TargetPort, test.Protocol, test.TimeoutMs,
		test.Status, test.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("create connectivity test: %w", err)
	}
	return &test, nil
}

// ListPendingNodeConnectivityTests returns all pending connectivity tests for a node.
func (s *Store) ListPendingNodeConnectivityTests(ctx context.Context, nodeID uuid.UUID) ([]NodeConnectivityTest, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, node_id, tenant_id, job_id, target_ip, target_port, protocol,
		       timeout_ms, status, reachable, error_message, created_at, completed_at
		FROM node_connectivity_tests
		WHERE node_id = $1 AND status = 'pending'
		ORDER BY created_at ASC
		LIMIT 10
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list pending connectivity tests: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tests []NodeConnectivityTest
	for rows.Next() {
		var t NodeConnectivityTest
		if err := rows.Scan(&t.ID, &t.NodeID, &t.TenantID, &t.JobID,
			&t.TargetIP, &t.TargetPort, &t.Protocol, &t.TimeoutMs,
			&t.Status, &t.Reachable, &t.ErrorMessage, &t.CreatedAt, &t.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan connectivity test: %w", err)
		}
		tests = append(tests, t)
	}
	return tests, rows.Err()
}

// MarkNodeConnectivityTestRunning transitions a test to running status.
func (s *Store) MarkNodeConnectivityTestRunning(ctx context.Context, id uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_connectivity_tests SET status = 'running' WHERE id = $1 AND status = 'pending'
	`, id)
	return err
}

// MarkNodeConnectivityTestCompleted records the result of a connectivity test.
func (s *Store) MarkNodeConnectivityTestCompleted(ctx context.Context, id uuid.UUID, reachable bool, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_connectivity_tests
		SET status = 'succeeded', reachable = $2, error_message = $3, completed_at = NOW()
		WHERE id = $1
	`, id, reachable, nullString(errMsg))
	return err
}

// MarkNodeConnectivityTestFailed transitions a test to failed status.
func (s *Store) MarkNodeConnectivityTestFailed(ctx context.Context, id uuid.UUID, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_connectivity_tests
		SET status = 'failed', error_message = $2, completed_at = NOW()
		WHERE id = $1
	`, id, nullString(errMsg))
	return err
}

// MarkNodeConnectivityTestByJobCompleted records the result of a connectivity test by job_id.
func (s *Store) MarkNodeConnectivityTestByJobCompleted(ctx context.Context, jobID uuid.UUID, reachable bool, errMsg string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE node_connectivity_tests
		SET status = 'succeeded', reachable = $2, error_message = $3, completed_at = NOW()
		WHERE job_id = $1 AND status IN ('pending', 'running')
	`, jobID, reachable, nullString(errMsg))
	return err
}
