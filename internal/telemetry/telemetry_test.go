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

func TestTrimLogRetryBacklogDropsOldest(t *testing.T) {
	batch := []logs.StructuredLog{{Message: "old"}, {Message: "middle"}, {Message: "new"}}
	if dropped := trimLogRetryBacklog(&batch, 2); dropped != 1 {
		t.Fatalf("dropped = %d, want 1", dropped)
	}
	if len(batch) != 2 || batch[0].Message != "middle" || batch[1].Message != "new" {
		t.Fatalf("unexpected retained batch: %#v", batch)
	}
}
