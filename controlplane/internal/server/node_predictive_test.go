package server

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/metrics"
)

func TestHealthPredictScoresDiskUsageSurgeBeforeCritical(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			Hostname: "bank-laravel-app-01",
		}},
		nodeHealthScores: map[uuid.UUID]storage.NodeHealthScore{},
	}
	for i := 0; i < healthCalibrationMinSamples; i++ {
		store.telemetryMetrics = append(store.telemetryMetrics, storage.CreateTelemetryMetricParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			MetricName:  metrics.MetricLoad1,
			MetricValue: 0.8,
			Timestamp:   now.Add(-time.Duration(i) * time.Minute),
		})
	}
	store.telemetryMetrics = append(store.telemetryMetrics,
		storage.CreateTelemetryMetricParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			MetricName:  metrics.MetricDiskUsagePercent,
			MetricValue: 55,
			Timestamp:   now.Add(-90 * time.Minute),
		},
		storage.CreateTelemetryMetricParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			MetricName:  metrics.MetricDiskUsagePercent,
			MetricValue: 76,
			Timestamp:   now.Add(-time.Minute),
		},
	)
	srv := &Server{store: store, logger: zap.NewNop()}

	scored, err := srv.scorePredictForTenant(context.Background(), tenantID, healthSignalsCatalog())
	if err != nil {
		t.Fatalf("score predict: %v", err)
	}
	if scored != 1 {
		t.Fatalf("scored nodes = %d, want 1", scored)
	}
	score, err := store.GetNodeHealthScore(context.Background(), nodeID)
	if err != nil {
		t.Fatalf("get score: %v", err)
	}
	if score == nil {
		t.Fatal("expected stored node health score")
	}
	if score.Score != 70 || score.RiskLevel != "medium" {
		t.Fatalf("score = %+v, want 70/medium", score)
	}
	breakdown, ok := score.Components["breakdown"].(map[string]int)
	if !ok {
		t.Fatalf("breakdown type = %T, value=%#v", score.Components["breakdown"], score.Components["breakdown"])
	}
	if got := breakdown["disk_usage_surge"]; got != -30 {
		t.Fatalf("disk surge penalty = %d, want -30 in %#v", got, breakdown)
	}
	if primary := score.Components["primary_component"]; primary != "disk_usage_surge" {
		t.Fatalf("primary_component = %#v, want disk_usage_surge", primary)
	}
}
