package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type aiOperatorStore interface {
	CreateAIInvestigation(context.Context, storage.CreateAIInvestigationParams) (*storage.AIInvestigation, error)
	GetAIInvestigation(context.Context, uuid.UUID) (*storage.AIInvestigation, error)
	ListAIInvestigations(context.Context, storage.ListAIInvestigationsFilter, int, int) ([]storage.AIInvestigation, int, error)
	CreateAIOperatorProposal(context.Context, storage.CreateAIOperatorProposalParams) (*storage.AIOperatorProposal, error)
	ListAIOperatorProposals(context.Context, storage.ListAIOperatorProposalsFilter, int, int) ([]storage.AIOperatorProposal, int, error)
}

func (s *Server) aiOperatorBackend() aiOperatorStore {
	if s == nil || s.store == nil {
		return nil
	}
	if backend, ok := s.store.(aiOperatorStore); ok {
		return backend
	}
	return nil
}

func (s *Server) persistAnomalyInvestigations(ctx context.Context, tenantID, fallbackNodeID uuid.UUID, anomalies []IngestedEvent) {
	backend := s.aiOperatorBackend()
	if backend == nil || len(anomalies) == 0 {
		return
	}
	for i := range anomalies {
		ev := anomalies[i]
		nodeID := fallbackNodeID
		if parsed, err := uuid.Parse(strings.TrimSpace(ev.NodeID)); err == nil {
			nodeID = parsed
		}
		evidence, err := json.Marshal(ev)
		if err != nil {
			s.logger.Warn("marshal anomaly investigation evidence", zap.Error(err), zap.String("event_type", ev.Type))
			continue
		}
		_, err = backend.CreateAIInvestigation(ctx, storage.CreateAIInvestigationParams{
			TenantID:         tenantID,
			NodeID:           nodeID,
			TriggerType:      "anomaly",
			TriggerEventType: ev.Type,
			TriggerDedupKey:  firstNonEmpty(ev.DedupKey, ev.CorrelationID, ev.Type),
			Severity:         ev.Severity,
			Summary:          firstNonEmpty(ev.Message, ev.Type),
			Evidence:         evidence,
			Status:           storage.AIInvestigationStatusOpen,
		})
		if err != nil {
			s.logger.Warn("persist anomaly investigation", zap.Error(err), zap.String("event_type", ev.Type), zap.String("dedup_key", ev.DedupKey))
		}
	}
}

func approvalRouteForOperatorAction(action string) (string, string) {
	normalized := strings.ToLower(strings.TrimSpace(action))
	switch {
	case strings.Contains(normalized, "patch"):
		return "patch", "/api/v1/patch/approvals"
	case strings.Contains(normalized, "remediation"),
		strings.Contains(normalized, "remediate"),
		strings.Contains(normalized, "script"),
		strings.Contains(normalized, "rollback"),
		strings.Contains(normalized, "rotate"),
		strings.Contains(normalized, "truncate"),
		strings.Contains(normalized, "kill"),
		strings.Contains(normalized, "restart"):
		return "remediation", "/api/v1/remediation/approvals"
	default:
		return "manual", ""
	}
}
