package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestHandleLogIngestReturnsReplayReceiptForDuplicateKey(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	replayKey := "logs:test-replay-key"
	store := &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: tenantID}}}
	srv := &Server{store: store, logger: zap.NewNop()}
	raw := []byte(fmt.Sprintf(`{
		"node_id":%q,
		"program":"nginx",
		"collector_type":"file",
		"count":1,
		"replay_key":%q,
		"entries":[{
			"timestamp":"%s",
			"program":"nginx",
			"message":"GET /health 200",
			"severity":"info",
			"fields":{"status":200,"path":"/health","remote_ip":"203.0.113.10"}
		}]
	}`, nodeID.String(), replayKey, time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC).Format(time.RFC3339)))

	post := func() map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/logs", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req = withPrincipal(req, &auth.Principal{
			Type:    "agent",
			Name:    nodeID.String(),
			Subject: nodeID.String(),
			Roles:   []string{"agent"},
		})
		rec := httptest.NewRecorder()
		srv.handleLogIngest(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("log ingest status = %d, body = %s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		return resp
	}

	first := post()
	second := post()
	if first["replay_key"] != replayKey || second["replay_key"] != replayKey {
		t.Fatalf("responses did not preserve replay key: first=%v second=%v", first, second)
	}
	if second["duplicate"] != true {
		t.Fatalf("duplicate response missing receipt marker: %v", second)
	}
	if len(store.telemetryLogs) != 1 {
		t.Fatalf("telemetry log rows = %d, want 1", len(store.telemetryLogs))
	}
	if len(store.eventIngestRecords) != 1 {
		t.Fatalf("event ingest records = %d, want 1", len(store.eventIngestRecords))
	}
	derived := store.eventIngestRecords[0]
	if derived.Rows != 2 || derived.ReplayKey == "" || derived.ReplayKey == replayKey {
		t.Fatalf("derived event journal params = %#v", derived)
	}
	if first["event_batch_id"] == "" || first["event_ingest_status"] != "accepted" {
		t.Fatalf("first response missing derived event receipt: %v", first)
	}
}

func TestHandleTelemetryIngestPersistsLabelledMetricSamples(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: tenantID}}}
	srv := &Server{store: store, logger: zap.NewNop()}
	raw := []byte(fmt.Sprintf(`{
		"node_id":%q,
		"timestamp":"2026-05-29T12:00:00Z",
		"metrics":{"cpu_usage_percent":12.5},
		"metric_samples":[
			{"name":"smart.reallocated_sector_count","value":4,"labels":{"device":" /dev/sda ","collector":"smartctl","empty":""}},
			{"name":"host.disk_queue_length","value":3,"labels":{"collector":"windows_perfcounter"}}
		]
	}`, nodeID.String()))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/telemetry", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, &auth.Principal{
		Type:    "agent",
		Name:    nodeID.String(),
		Subject: nodeID.String(),
		Roles:   []string{"agent"},
	})
	rec := httptest.NewRecorder()
	srv.handleTelemetryIngest(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("telemetry ingest status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if len(store.telemetryMetrics) != 3 {
		t.Fatalf("telemetry metric rows = %#v", store.telemetryMetrics)
	}
	var smartRow, queueRow storage.CreateTelemetryMetricParams
	for _, row := range store.telemetryMetrics {
		switch row.MetricName {
		case "smart.reallocated_sector_count":
			smartRow = row
		case "host.disk_queue_length":
			queueRow = row
		}
	}
	if smartRow.MetricValue != 4 || smartRow.Labels["device"] != "/dev/sda" || smartRow.Labels["empty"] != "" {
		t.Fatalf("smart row = %#v", smartRow)
	}
	if queueRow.MetricValue != 3 || queueRow.Labels["collector"] != "windows_perfcounter" {
		t.Fatalf("queue row = %#v", queueRow)
	}
	if queueRow.MetricUnit == nil || *queueRow.MetricUnit != "count" {
		t.Fatalf("queue row unit = %#v", queueRow.MetricUnit)
	}
}
