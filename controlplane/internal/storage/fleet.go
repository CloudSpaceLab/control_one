package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// FleetEnrollmentResult stores the outcome of provisioning a single host
// during a fleet enrollment job.
type FleetEnrollmentResult struct {
	ID           uuid.UUID
	JobID        uuid.UUID
	Host         string
	Port         int
	Success      bool
	NodeID       *uuid.UUID
	ErrorMessage sql.NullString
	SSHOutput    sql.NullString
	DurationMs   sql.NullInt32
	CreatedAt    time.Time
}

// CreateFleetEnrollmentResult inserts a single fleet enrollment result.
func (s *Store) CreateFleetEnrollmentResult(ctx context.Context, r *FleetEnrollmentResult) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if r == nil {
		return errors.New("result cannot be nil")
	}
	if r.JobID == uuid.Nil {
		return errors.New("job_id is required")
	}
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.clock()
	}

	var nodeID any
	if r.NodeID != nil && *r.NodeID != uuid.Nil {
		nodeID = *r.NodeID
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO fleet_enrollment_results (id, job_id, host, port, success, node_id, error_message, ssh_output, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, r.ID, r.JobID, r.Host, r.Port, r.Success, nodeID, r.ErrorMessage, r.SSHOutput, r.DurationMs, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert fleet enrollment result: %w", err)
	}

	return nil
}

// ListFleetEnrollmentResults returns all enrollment results for a given job.
func (s *Store) ListFleetEnrollmentResults(ctx context.Context, jobID uuid.UUID) ([]FleetEnrollmentResult, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if jobID == uuid.Nil {
		return nil, errors.New("job_id is required")
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, job_id, host, port, success, node_id, error_message, ssh_output, duration_ms, created_at
		FROM fleet_enrollment_results
		WHERE job_id = $1
		ORDER BY created_at ASC
	`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query fleet enrollment results: %w", err)
	}
	defer rows.Close()

	var results []FleetEnrollmentResult
	for rows.Next() {
		var r FleetEnrollmentResult
		var nodeID sql.NullString
		if err := rows.Scan(&r.ID, &r.JobID, &r.Host, &r.Port, &r.Success, &nodeID, &r.ErrorMessage, &r.SSHOutput, &r.DurationMs, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan fleet enrollment result: %w", err)
		}
		if nodeID.Valid {
			parsed, err := uuid.Parse(nodeID.String)
			if err == nil {
				r.NodeID = &parsed
			}
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fleet enrollment results: %w", err)
	}

	return results, nil
}
