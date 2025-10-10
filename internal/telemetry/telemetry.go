package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/scanner"
)

// Service encapsulates telemetry operations towards the control plane.
type Service struct {
	client *api.Client
	log    *zap.Logger
}

// New creates a telemetry service.
func New(client *api.Client, log *zap.Logger) *Service {
	return &Service{client: client, log: log}
}

// SendMetrics pushes periodic host metrics.
func (s *Service) SendMetrics(ctx context.Context, nodeID string, metrics map[string]any) {
	payload := map[string]any{
		"node_id":   nodeID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"metrics":   metrics,
	}
	if err := s.postJSON(ctx, "/api/v1/telemetry", payload); err != nil {
		s.log.Warn("send metrics failed", zap.Error(err))
	}
}

// SendCompliance reports compliance scan results.
func (s *Service) SendCompliance(ctx context.Context, nodeID string, results []scanner.Result) {
    summary := map[string]int{
        scanner.StatusCompliant:    0,
        scanner.StatusNonCompliant: 0,
        scanner.StatusError:        0,
    }
    for _, r := range results {
        summary[r.Status]++
    }
	payload := map[string]any{
		"node_id":   nodeID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"results":   results,
		"summary": map[string]any{
			"compliant":     summary[scanner.StatusCompliant],
			"non_compliant": summary[scanner.StatusNonCompliant],
			"error":         summary[scanner.StatusError],
			"total":         len(results),
		},
	}
	if err := s.postJSON(ctx, "/api/v1/compliance/report", payload); err != nil {
		s.log.Warn("send compliance failed", zap.Error(err))
	}
}

// SendHeartbeat notifies the control plane that the node is alive.
func (s *Service) SendHeartbeat(ctx context.Context, nodeID, heartbeatID string) {
	payload := map[string]any{
		"node_id": nodeID,
		"heartbeat": map[string]any{
			"id":        heartbeatID,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if err := s.postJSON(ctx, "/api/v1/heartbeat", payload); err != nil {
		s.log.Warn("send heartbeat failed", zap.Error(err))
	}
}

func (s *Service) postJSON(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telemetry payload: %w", err)
	}

	resp, err := s.client.Do(ctx, "POST", path, body)
	if err != nil {
		return fmt.Errorf("post telemetry: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry request failed: status %d", resp.StatusCode)
	}

	return nil
}
