package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type pendingLogDumpStore interface {
	ListPendingNodeLogDumps(context.Context, uuid.UUID, time.Time, int) ([]storage.LogDump, error)
}

func (s *Server) appendPendingLogDumpActions(ctx context.Context, node *storage.Node, resp *heartbeatResponse) {
	if s == nil || s.store == nil || node == nil || resp == nil || !nodeAdvertisesCapability(node, agentLogDumpCapability) {
		return
	}
	store, ok := s.store.(pendingLogDumpStore)
	if !ok {
		return
	}
	pending, err := store.ListPendingNodeLogDumps(ctx, node.ID, s.logDumpNow(), 4)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.logger.Warn("list pending log dumps", zap.String("node_id", node.ID.String()), zap.Error(err))
		return
	}
	for _, dump := range pending {
		if !dump.JobID.Valid {
			continue
		}
		resp.PendingActions = append(resp.PendingActions,
			JobTypeLogDump+":"+dump.JobID.UUID.String()+":"+dump.ID.String())
	}
}
