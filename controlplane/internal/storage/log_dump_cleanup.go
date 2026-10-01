package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// FailControlPlaneLogDump records a bounded server-side capture failure.
func (s *Store) FailControlPlaneLogDump(ctx context.Context, tenantID, nodeID, dumpID uuid.UUID, message string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE agent_log_dumps
		SET status='failed', error=$4
		WHERE tenant_id=$1 AND node_id=$2 AND id=$3
		  AND source='control_plane' AND status='requested'`,
		tenantID, nodeID, dumpID, nullableText(message))
	if err != nil {
		return fmt.Errorf("fail control-plane log dump: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil
	}
	return nil
}

// DeleteLogDumpChunks removes chunk metadata after a completed artifact has
// been assembled. The final artifact and dump lifecycle record remain.
func (s *Store) DeleteLogDumpChunks(ctx context.Context, tenantID, nodeID, dumpID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM agent_log_dump_chunks
		WHERE tenant_id=$1 AND node_id=$2 AND dump_id=$3`, tenantID, nodeID, dumpID)
	if err != nil {
		return fmt.Errorf("delete log dump chunks: %w", err)
	}
	return nil
}

// FailClaimedLogDumpAndJob records an agent-side capture failure only while
// the caller still owns the active claim. Stale workers cannot fail a request
// after another agent process has taken over the lease.
func (s *Store) FailClaimedLogDumpAndJob(ctx context.Context, tenantID, nodeID, dumpID, jobID uuid.UUID, tokenSHA, message string, sourceAvailable *bool, sourceReason string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE agent_log_dumps SET
		status='failed', error=$6, source_available=$7, source_reason=$8,
		claim_token_sha256=NULL, claim_expires_at=NULL
		WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4
		  AND status='capturing' AND claim_token_sha256=$5
		  AND claim_expires_at>$9 AND expires_at>$9`,
		tenantID, nodeID, dumpID, jobID, tokenSHA, nullableText(message), sourceAvailable, nullableText(sourceReason), now)
	if err != nil {
		return fmt.Errorf("fail claimed log dump: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		var expiresAt time.Time
		var status string
		lookupErr := tx.QueryRowContext(ctx, `SELECT status, expires_at FROM agent_log_dumps
			WHERE tenant_id=$1 AND node_id=$2 AND id=$3 AND job_id=$4`,
			tenantID, nodeID, dumpID, jobID).Scan(&status, &expiresAt)
		if lookupErr != nil {
			return lookupErr
		}
		if !now.Before(expiresAt) || status == LogDumpStatusExpired {
			return ErrLogDumpExpired
		}
		return ErrLogDumpClaimInvalid
	}
	if err := setTerminalLogDumpJobTx(ctx, tx, jobID, JobStatusFailed, message, now); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

