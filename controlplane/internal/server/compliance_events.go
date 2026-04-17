package server

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// Compliance event types emitted during evaluation.
const (
	EventComplianceCompleted = "compliance.evaluation.completed"
	EventComplianceFailure   = "compliance.failure"
	EventComplianceHighFail  = "compliance.failure.high"
	EventComplianceCritical  = "compliance.failure.critical"
)

// maxWebhookConcurrency caps the number of concurrent webhook deliveries.
const maxWebhookConcurrency = 10

// severityRank maps severity strings to numeric ranks for comparison.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// emitComplianceEvents dispatches webhook events based on compliance evaluation results.
// Deliveries happen in goroutines to avoid blocking the caller. Errors are logged but
// do not propagate since webhook delivery is best-effort.
func (s *Server) emitComplianceEvents(ctx context.Context, tenantID, nodeID uuid.UUID, results []compliance.Result, scanID string) {
	if s.store == nil {
		return
	}

	// Identify failures and determine the highest severity.
	var failedResults []map[string]any
	maxSev := 0
	maxSevName := ""
	for _, r := range results {
		if r.Passed {
			continue
		}
		failedResults = append(failedResults, map[string]any{
			"rule_id":    r.RuleID,
			"severity":   r.Severity,
			"details":    r.Details,
			"checked_at": r.CheckedAt.Format(time.RFC3339),
		})
		rank := severityRank[r.Severity]
		if rank > maxSev {
			maxSev = rank
			maxSevName = r.Severity
		}
	}

	// Build the event payload shared across event types.
	payload := map[string]any{
		"node_id":   nodeID.String(),
		"scan_id":   scanID,
		"total":     len(results),
		"failures":  failedResults,
		"severity":  maxSevName,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	// Determine which event types to emit.
	eventTypes := []string{EventComplianceCompleted}
	if len(failedResults) > 0 {
		eventTypes = append(eventTypes, EventComplianceFailure)
		if maxSev >= severityRank["high"] {
			eventTypes = append(eventTypes, EventComplianceHighFail)
		}
		if maxSev >= severityRank["critical"] {
			eventTypes = append(eventTypes, EventComplianceCritical)
		}
	}

	// Collect unique webhooks to deliver to, avoiding duplicate deliveries.
	type webhookEvent struct {
		webhook   storage.Webhook
		eventType string
	}
	var deliveries []webhookEvent
	seen := make(map[string]struct{}) // key: webhookID+eventType

	for _, eventType := range eventTypes {
		webhooks, err := s.store.ListWebhooksByEvent(ctx, tenantID, eventType)
		if err != nil {
			s.logger.Error("list webhooks for compliance event",
				zap.String("event_type", eventType),
				zap.Error(err),
			)
			continue
		}
		for _, wh := range webhooks {
			key := wh.ID.String() + ":" + eventType
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			deliveries = append(deliveries, webhookEvent{webhook: wh, eventType: eventType})
		}
	}

	if len(deliveries) == 0 {
		return
	}

	// Deliver webhooks concurrently with bounded concurrency.
	sem := make(chan struct{}, maxWebhookConcurrency)
	var wg sync.WaitGroup

	for _, d := range deliveries {
		wg.Add(1)
		sem <- struct{}{}
		go func(wh storage.Webhook, eventType string) {
			defer wg.Done()
			defer func() { <-sem }()

			s.deliverAndRecordCompliance(ctx, &wh, eventType, payload)
		}(d.webhook, d.eventType)
	}

	wg.Wait()
}

// deliverAndRecordCompliance delivers a webhook and records the delivery result.
func (s *Server) deliverAndRecordCompliance(ctx context.Context, webhook *storage.Webhook, eventType string, payload map[string]any) {
	success, statusCode, responseBody, err := s.deliverWebhook(webhook, eventType, payload)

	deliveryStatus := "success"
	if !success {
		deliveryStatus = "failed"
	}

	delivery := storage.WebhookDelivery{
		ID:            uuid.New(),
		WebhookID:     webhook.ID,
		EventType:     eventType,
		Status:        deliveryStatus,
		AttemptNumber: 1,
		RequestBody:   payload,
		CreatedAt:     time.Now().UTC(),
	}

	if statusCode > 0 {
		delivery.HTTPStatusCode = sql.NullInt64{Int64: int64(statusCode), Valid: true}
	}
	if responseBody != "" {
		delivery.ResponseBody = sql.NullString{String: responseBody, Valid: true}
	}
	if err != nil {
		delivery.ErrorMessage = sql.NullString{String: err.Error(), Valid: true}
		s.logger.Warn("compliance webhook delivery failed",
			zap.String("webhook_id", webhook.ID.String()),
			zap.String("event_type", eventType),
			zap.Error(err),
		)
	}

	now := time.Now().UTC()
	delivery.DeliveredAt = sql.NullTime{Time: now, Valid: true}

	if recordErr := s.store.RecordWebhookDelivery(ctx, delivery); recordErr != nil {
		s.logger.Error("record compliance webhook delivery",
			zap.String("webhook_id", webhook.ID.String()),
			zap.Error(recordErr),
		)
	}
}
