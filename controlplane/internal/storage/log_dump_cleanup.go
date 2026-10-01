package storage

import (
	"context"
	"fmt"

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
