package telemetry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/telemetry/logs"
)

func TestLogReplayKeyStableAndBackfilled(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"node_id":        "node-1",
		"program":        "nginx",
		"collector_type": "file",
		"entries": []any{
			map[string]any{"timestamp": "2026-05-29T12:00:00Z", "message": "ok"},
		},
	}
	key := logReplayKey(payload)
	if key == "" || key != logReplayKey(payload) {
		t.Fatalf("logReplayKey is not stable: %q", key)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	withKey, err := ensureLogReplayKey(raw)
	if err != nil {
		t.Fatalf("ensureLogReplayKey: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(withKey, &decoded); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if decoded["replay_key"] != key {
		t.Fatalf("replay_key = %v, want %s", decoded["replay_key"], key)
	}
}

func TestSendMetricBatchIncludesLabelledSamples(t *testing.T) {
	t.Parallel()

	accepted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/telemetry" {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = r.Body.Close() }()
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		accepted <- payload
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	svc := New(client, zap.NewNop(), nil)
	svc.SendMetricBatch(context.Background(), "node-1", MetricBatch{
		Metrics: map[string]any{"cpu_usage_percent": 12.5},
		Samples: []MetricSample{{
			Name:   "smart.reallocated_sector_count",
			Value:  4,
			Labels: map[string]string{"device": "/dev/sda"},
		}},
	})

	select {
	case payload := <-accepted:
		if payload["node_id"] != "node-1" {
			t.Fatalf("node_id = %#v", payload["node_id"])
		}
		metrics, _ := payload["metrics"].(map[string]any)
		if metrics["cpu_usage_percent"] != 12.5 {
			t.Fatalf("metrics = %#v", metrics)
		}
		samples, _ := payload["metric_samples"].([]any)
		if len(samples) != 1 {
			t.Fatalf("metric_samples = %#v", payload["metric_samples"])
		}
		sample, _ := samples[0].(map[string]any)
		labels, _ := sample["labels"].(map[string]any)
		if sample["name"] != "smart.reallocated_sector_count" || labels["device"] != "/dev/sda" {
			t.Fatalf("sample payload = %#v", sample)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for metric payload")
	}
}

func TestConsumeLogsRetriesFailedBatch(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	accepted := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs" {
			http.NotFound(w, r)
			return
		}
		call := calls.Add(1)
		if call == 1 {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = r.Body.Close() }()
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Entries []map[string]any `json:"entries"`
		}
		_ = json.Unmarshal(body, &payload)
		accepted <- len(payload.Entries)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	svc := New(client, zap.NewNop(), nil)
	source := config.LogSourceConfig{
		Program:       "nginx",
		Type:          "file",
		BatchSize:     2,
		FlushInterval: 20 * time.Millisecond,
	}
	rawCh := make(chan logs.RawLog, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.consumeLogs(ctx, "node-1", source, logs.GetFormatter("default"), rawCh)

	rawCh <- logs.RawLog{Timestamp: time.Now(), Program: "nginx", Message: "first", Severity: "info"}
	rawCh <- logs.RawLog{Timestamp: time.Now(), Program: "nginx", Message: "second", Severity: "info"}

	select {
	case count := <-accepted:
		if count != 2 {
			t.Fatalf("retry sent %d entries, want 2", count)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for retry")
	}
	if calls.Load() < 2 {
		t.Fatalf("expected initial failure plus retry, got %d calls", calls.Load())
	}
}

func TestConsumeLogsCollectParsedOmitsRawMessage(t *testing.T) {
	t.Parallel()

	accepted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs" {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = r.Body.Close() }()
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Entries []map[string]any `json:"entries"`
		}
		_ = json.Unmarshal(body, &payload)
		if len(payload.Entries) > 0 {
			accepted <- payload.Entries[0]
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	svc := New(client, zap.NewNop(), nil)
	source := config.LogSourceConfig{
		Program:       "nginx",
		Type:          "file",
		Formatter:     "default",
		CollectMode:   config.LogCollectModeCollectParsed,
		BatchSize:     1,
		FlushInterval: 20 * time.Millisecond,
	}
	config.NormalizeLogSourceConfig(&source)
	rawCh := make(chan logs.RawLog, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.consumeLogs(ctx, "node-1", source, logs.GetFormatter("default"), rawCh)

	rawCh <- logs.RawLog{
		Timestamp: time.Now(),
		Program:   "nginx",
		Message:   `10.0.0.4 - alice [28/May/2026:12:00:00 +0000] "GET /customers/123 HTTP/1.1" 200`,
		Severity:  "info",
		Fields:    map[string]any{"status": 200.0, "path": "/customers/123"},
	}

	select {
	case entry := <-accepted:
		if entry["message"] == `10.0.0.4 - alice [28/May/2026:12:00:00 +0000] "GET /customers/123 HTTP/1.1" 200` {
			t.Fatalf("raw message was retained: %#v", entry)
		}
		if entry["message"] != "raw log omitted by collect_parsed" {
			t.Fatalf("message = %#v", entry["message"])
		}
		labels, _ := entry["labels"].(map[string]any)
		if labels["control_one.collect_mode"] != config.LogCollectModeCollectParsed || labels["control_one.raw_message_retained"] != "false" {
			t.Fatalf("labels = %#v", labels)
		}
		fields, _ := entry["fields"].(map[string]any)
		if fields["path"] != "/customers/123" {
			t.Fatalf("parsed fields were not retained: %#v", fields)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for parsed-only entry")
	}
}

func TestConsumeLogsReplaysDurableSpoolAfterOutage(t *testing.T) {
	t.Parallel()

	var accepting atomic.Bool
	accepted := make(chan int, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/logs" {
			http.NotFound(w, r)
			return
		}
		if !accepting.Load() {
			http.Error(w, "temporary outage", http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = r.Body.Close() }()
		body, _ := io.ReadAll(r.Body)
		var payload struct {
			Entries []map[string]any `json:"entries"`
		}
		_ = json.Unmarshal(body, &payload)
		accepted <- len(payload.Entries)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	svc := New(client, zap.NewNop(), nil)
	svc.WithDurability(DurabilityOptions{
		LogSpoolDir:      t.TempDir(),
		LogSpoolMaxBytes: 1 << 20,
	})
	source := config.LogSourceConfig{
		Program:       "nginx",
		Type:          "file",
		BatchSize:     1,
		FlushInterval: 20 * time.Millisecond,
	}
	rawCh := make(chan logs.RawLog, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go svc.consumeLogs(ctx, "node-1", source, logs.GetFormatter("default"), rawCh)

	rawCh <- logs.RawLog{Timestamp: time.Now(), Program: "nginx", Message: "first", Severity: "info"}
	time.Sleep(50 * time.Millisecond)
	accepting.Store(true)

	select {
	case count := <-accepted:
		if count != 1 {
			t.Fatalf("replayed %d entries, want 1", count)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for durable log spool replay")
	}
}

func TestTrimLogRetryBacklogDropsOldest(t *testing.T) {
	batch := []logs.StructuredLog{{Message: "old"}, {Message: "middle"}, {Message: "new"}}
	if dropped := trimLogRetryBacklog(&batch, 2); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if len(batch) != 2 || batch[0].Message != "middle" || batch[1].Message != "new" {
		t.Fatalf("unexpected retained batch: %#v", batch)
	}
}

func TestAddLogSourcesStartsOnlyNewSources(t *testing.T) {
	t.Parallel()

	logs.RegisterCollectorFactory("test-dynamic-add", func(cfg config.LogSourceConfig, logger *zap.Logger) (logs.Collector, error) {
		return blockingCollector{}, nil
	})
	svc := New(nil, zap.NewNop(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := []config.LogSourceConfig{{Program: "app-one", Type: "test-dynamic-add"}}
	if got := svc.AddLogSources(ctx, "node-1", first); got != 1 {
		t.Fatalf("first AddLogSources = %d, want 1", got)
	}
	if got := svc.AddLogSources(ctx, "node-1", first); got != 0 {
		t.Fatalf("duplicate AddLogSources = %d, want 0", got)
	}
	if got := svc.AddLogSources(ctx, "node-1", []config.LogSourceConfig{{Program: "app-two", Type: "test-dynamic-add"}}); got != 1 {
		t.Fatalf("second AddLogSources = %d, want 1", got)
	}
}

type blockingCollector struct{}

func (blockingCollector) Run(ctx context.Context, _ chan<- logs.RawLog) error {
	<-ctx.Done()
	return nil
}
