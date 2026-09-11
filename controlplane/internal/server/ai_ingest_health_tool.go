package server

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type aiIngestBacklogProvider interface {
	EventIngestBacklogForTenant(context.Context, uuid.UUID) (storage.EventIngestBacklogSummary, error)
	ListEventIngestBacklogBatches(context.Context, uuid.UUID, int) ([]storage.EventIngestBatch, error)
}

type dorisIngestHealthToolResponse struct {
	TenantID            string                      `json:"tenant_id"`
	Status              string                      `json:"status"`
	AnalyticsMode       string                      `json:"analytics_mode"`
	AnalyticsStatus     string                      `json:"analytics_status"`
	WarehouseStatus     string                      `json:"warehouse_status"`
	WarehouseConfigured bool                        `json:"warehouse_configured"`
	DorisStatus         string                      `json:"doris_status"`
	DorisConfigured     bool                        `json:"doris_configured"`
	WriterHealthy       bool                        `json:"writer_healthy"`
	PendingBatches      int64                       `json:"pending_batches"`
	PendingRows         int64                       `json:"pending_rows"`
	DueBatches          int64                       `json:"due_batches"`
	RetryingBatches     int64                       `json:"retrying_batches"`
	FailedBatches       int64                       `json:"failed_batches"`
	MaxRetryCount       int                         `json:"max_retry_count"`
	OldestPendingAt     *string                     `json:"oldest_pending_at,omitempty"`
	NextAttemptAt       *string                     `json:"next_attempt_at,omitempty"`
	LastErrorAt         *string                     `json:"last_error_at,omitempty"`
	LastErrorMessage    string                      `json:"last_error_message,omitempty"`
	Evidence            []dorisIngestHealthEvidence `json:"evidence"`
	Citations           []dorisIngestHealthCitation `json:"citations"`
	Guardrails          []string                    `json:"guardrails"`
	GeneratedAt         string                      `json:"generated_at"`
}

type dorisIngestHealthEvidence struct {
	ID             string  `json:"id"`
	BatchID        string  `json:"batch_id"`
	Status         string  `json:"status"`
	DorisStatus    string  `json:"doris_status,omitempty"`
	NodeID         string  `json:"node_id,omitempty"`
	Rows           int     `json:"rows"`
	SizeBytes      int64   `json:"size_bytes"`
	RetryCount     int     `json:"retry_count"`
	ReceivedAt     string  `json:"received_at"`
	LastAttemptAt  *string `json:"last_attempt_at,omitempty"`
	NextAttemptAt  *string `json:"next_attempt_at,omitempty"`
	LastErrorAt    *string `json:"last_error_at,omitempty"`
	ErrorMessage   string  `json:"error_message,omitempty"`
	SourceRecordID string  `json:"source_record_id"`
}

type dorisIngestHealthCitation struct {
	ID             string `json:"id"`
	Kind           string `json:"kind"`
	Table          string `json:"table"`
	SourceRecordID string `json:"source_record_id"`
}

func dorisIngestHealthToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 25, "description": "Maximum cited backlog batch rows to include"},
		},
	}
}

func (s *Server) runDorisIngestHealthTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runIngestHealthToolNamed(ctx, tc, input, "doris_ingest_health")
}

func (s *Server) runIngestHealthTool(ctx context.Context, tc aiToolContext, input map[string]any) (aiToolExecution, error) {
	return s.runIngestHealthToolNamed(ctx, tc, input, "ingest_health")
}

func (s *Server) runIngestHealthToolNamed(ctx context.Context, tc aiToolContext, input map[string]any, toolName string) (aiToolExecution, error) {
	if s == nil || s.store == nil {
		return aiToolExecution{}, fmt.Errorf("event ingest backlog unavailable")
	}
	provider, ok := s.store.(aiIngestBacklogProvider)
	if !ok {
		return aiToolExecution{}, fmt.Errorf("event ingest backlog unavailable")
	}
	limit := intFromToolInput(input, "limit")
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	summary, err := provider.EventIngestBacklogForTenant(ctx, tc.TenantID)
	if err != nil {
		return aiToolExecution{}, err
	}
	batches, err := provider.ListEventIngestBacklogBatches(ctx, tc.TenantID, limit)
	if err != nil {
		return aiToolExecution{}, err
	}
	resp := s.newDorisIngestHealthToolResponse(ctx, tc.TenantID, summary, batches)
	return aiToolExecution{
		Citation: llm.Citation{
			Tool:   toolName,
			Label:  "event ingest backlog",
			Detail: fmt.Sprintf("%d pending batches, %d failed batches", summary.PendingBatches, summary.FailedBatches),
		},
		Payload: resp,
	}, nil
}

func (s *Server) newDorisIngestHealthToolResponse(ctx context.Context, tenantID uuid.UUID, summary storage.EventIngestBacklogSummary, batches []storage.EventIngestBatch) dorisIngestHealthToolResponse {
	analyticsMode := effectiveAnalyticsMode(nil)
	if s != nil {
		analyticsMode = effectiveAnalyticsMode(s.cfg)
	}
	dorisConfigured := s != nil && (s.dorisClient != nil || s.dorisWriter != nil)
	writerHealthy := s == nil || s.dorisWriter == nil || s.dorisWriter.Healthy()
	dorisStatus := "unconfigured"
	warehouseStatus := "disabled"
	if analyticsMode == analyticsModeOLAP {
		warehouseStatus = "unconfigured"
	}
	if dorisConfigured {
		dorisStatus = "ok"
		warehouseStatus = "ok"
		if !writerHealthy {
			dorisStatus = "degraded"
			warehouseStatus = "degraded"
		}
		if s.dorisClient != nil {
			pingCtx, cancel := context.WithTimeout(ctx, time.Second)
			if err := s.dorisClient.Ping(pingCtx); err != nil {
				dorisStatus = "degraded"
				warehouseStatus = "degraded"
			}
			cancel()
		}
	}

	status := "ok"
	if summary.PendingBatches > 0 || summary.FailedBatches > 0 || warehouseStatus == "degraded" {
		status = "degraded"
	}
	if analyticsMode == analyticsModeOLAP && !dorisConfigured && summary.PendingBatches > 0 {
		status = "down"
	}
	resp := dorisIngestHealthToolResponse{
		TenantID:            tenantID.String(),
		Status:              status,
		AnalyticsMode:       analyticsMode,
		AnalyticsStatus:     status,
		WarehouseStatus:     warehouseStatus,
		WarehouseConfigured: dorisConfigured,
		DorisStatus:         dorisStatus,
		DorisConfigured:     dorisConfigured,
		WriterHealthy:       writerHealthy,
		PendingBatches:      summary.PendingBatches,
		PendingRows:         summary.PendingRows,
		DueBatches:          summary.DueBatches,
		RetryingBatches:     summary.RetryingBatches,
		FailedBatches:       summary.FailedBatches,
		MaxRetryCount:       summary.MaxRetryCount,
		OldestPendingAt:     formatNullTimePtr(summary.OldestPendingAt),
		NextAttemptAt:       formatNullTimePtr(summary.NextAttemptAt),
		LastErrorAt:         formatNullTimePtr(summary.LastErrorAt),
		Evidence:            []dorisIngestHealthEvidence{},
		Citations:           []dorisIngestHealthCitation{},
		Guardrails: []string{
			"admin-gated because ingest replay status is operational platform health",
			"tenant scoped to the authenticated AI request tenant",
			"raw event payload bytes are never returned by this tool",
			"pending or retrying batches mean analytic investigations can be incomplete until replay drains",
		},
		GeneratedAt: formatTime(time.Now().UTC()),
	}
	if summary.LastErrorMessage.Valid {
		resp.LastErrorMessage = summary.LastErrorMessage.String
	}
	for _, batch := range batches {
		if batch.ID == uuid.Nil {
			continue
		}
		sourceRecordID := "event_ingest_batches:" + batch.ID.String()
		citationID := "ingest-backlog:" + batch.ID.String()
		ev := dorisIngestHealthEvidence{
			ID:             citationID,
			BatchID:        batch.ID.String(),
			Status:         batch.Status,
			Rows:           batch.Rows,
			SizeBytes:      batch.SizeBytes,
			RetryCount:     batch.RetryCount,
			ReceivedAt:     formatTime(batch.ReceivedAt.UTC()),
			LastAttemptAt:  formatNullTimePtr(batch.LastAttemptAt),
			NextAttemptAt:  formatNullTimePtr(batch.NextAttemptAt),
			LastErrorAt:    formatNullTimePtr(batch.LastErrorAt),
			SourceRecordID: sourceRecordID,
		}
		if batch.NodeID.Valid {
			ev.NodeID = batch.NodeID.UUID.String()
		}
		if batch.DorisStatus.Valid {
			ev.DorisStatus = batch.DorisStatus.String
		}
		if batch.ErrorMessage.Valid {
			ev.ErrorMessage = batch.ErrorMessage.String
		}
		resp.Evidence = append(resp.Evidence, ev)
		resp.Citations = append(resp.Citations, dorisIngestHealthCitation{
			ID:             citationID,
			Kind:           "event_ingest_batch",
			Table:          "event_ingest_batches",
			SourceRecordID: sourceRecordID,
		})
	}
	return resp
}
