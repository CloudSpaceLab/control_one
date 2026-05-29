package server

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/internal/metrics"
)

func TestNewAgentMetricRowKeepsLabelledSamples(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	ts := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)

	row, ok := newAgentMetricRow(tenantID, nodeID, metrics.MetricSmartReallocatedSectors, float64(4), map[string]string{
		" device ": "/dev/sda ",
		"empty":    "",
	}, ts)
	if !ok {
		t.Fatal("expected row to be accepted")
	}
	if row.TenantID != tenantID || row.NodeID != nodeID || row.Timestamp != ts {
		t.Fatalf("row identity = %#v", row)
	}
	if row.MetricName != metrics.MetricSmartReallocatedSectors || row.MetricValue != 4 {
		t.Fatalf("metric row = %#v", row)
	}
	if row.MetricUnit == nil || *row.MetricUnit != "count" {
		t.Fatalf("metric unit = %#v", row.MetricUnit)
	}
	if row.Labels["device"] != "/dev/sda" {
		t.Fatalf("labels were not sanitized/preserved: %#v", row.Labels)
	}
	if _, ok := row.Labels["empty"]; ok {
		t.Fatalf("empty label should have been dropped: %#v", row.Labels)
	}
}

func TestNewAgentMetricRowRejectsNonNumericValues(t *testing.T) {
	if _, ok := newAgentMetricRow(uuid.New(), uuid.New(), "cpu_usage_percent", map[string]any{"value": 1}, nil, time.Now()); ok {
		t.Fatal("expected non-numeric metric value to be skipped")
	}
}
