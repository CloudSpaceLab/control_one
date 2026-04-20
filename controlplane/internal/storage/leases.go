package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	pgconn "github.com/jackc/pgx/v5/pgconn"
)

// ErrLeaseHeld is returned from AcquireRemediationLease when another job
// already holds the lease for the node and the current time has not yet
// exceeded the lease expiry.
var ErrLeaseHeld = errors.New("remediation lease already held")

// RemediationLease captures the lease row used to serialise remediations per node.
type RemediationLease struct {
	NodeID     uuid.UUID
	TenantID   uuid.UUID
	JobID      uuid.UUID
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

// AcquireRemediationLease inserts a lease for (tenantID, nodeID, jobID) valid
// for ttl. If another live lease already exists for the same node, the call
// returns ErrLeaseHeld. Expired leases are cleaned up atomically before
// acquisition so a stale lease never blocks a new remediation.
func (s *Store) AcquireRemediationLease(ctx context.Context, tenantID, nodeID, jobID uuid.UUID, ttl time.Duration) (*RemediationLease, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	if nodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	if jobID == uuid.Nil {
		return nil, errors.New("job id is required")
	}
	if ttl <= 0 {
		return nil, errors.New("ttl must be positive")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	// Self-clean expired leases for this node before attempting to claim.
	if _, err = tx.ExecContext(ctx, `
		DELETE FROM remediation_leases
		WHERE node_id = $1 AND expires_at < NOW()
	`, nodeID); err != nil {
		return nil, fmt.Errorf("sweep expired leases: %w", err)
	}

	now := s.clock().UTC()
	expiresAt := now.Add(ttl)

	row := tx.QueryRowContext(ctx, `
		INSERT INTO remediation_leases (node_id, tenant_id, job_id, acquired_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (node_id) DO NOTHING
		RETURNING node_id, tenant_id, job_id, acquired_at, expires_at
	`, nodeID, tenantID, jobID, now, expiresAt)

	var lease RemediationLease
	if err = row.Scan(&lease.NodeID, &lease.TenantID, &lease.JobID, &lease.AcquiredAt, &lease.ExpiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLeaseHeld
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// unique violation (belt-and-suspenders; ON CONFLICT should handle it).
			return nil, ErrLeaseHeld
		}
		return nil, fmt.Errorf("insert remediation lease: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit lease: %w", err)
	}
	committed = true

	return &lease, nil
}

// ReleaseRemediationLease removes the lease for a node. Returns nil when no
// lease exists (release is idempotent by design).
func (s *Store) ReleaseRemediationLease(ctx context.Context, nodeID uuid.UUID) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	if nodeID == uuid.Nil {
		return errors.New("node id is required")
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM remediation_leases WHERE node_id = $1`, nodeID); err != nil {
		return fmt.Errorf("release remediation lease: %w", err)
	}
	return nil
}

// CountTenantLeases returns the number of currently-held (non-expired) leases
// belonging to the tenant. Expired rows are excluded from the count so the
// caller sees the real in-flight concurrency.
func (s *Store) CountTenantLeases(ctx context.Context, tenantID uuid.UUID) (int, error) {
	if s.db == nil {
		return 0, errors.New("store database not initialized")
	}
	if tenantID == uuid.Nil {
		return 0, errors.New("tenant id is required")
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM remediation_leases
		WHERE tenant_id = $1 AND expires_at > NOW()
	`, tenantID)

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("count tenant leases: %w", err)
	}
	return count, nil
}
