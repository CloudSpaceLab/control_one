package eventstream

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/durablespool"
)

func TestBatcherBoundsRejectedResponseDiagnostic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad event type: "+strings.Repeat("x", 3000), http.StatusBadRequest)
	}))
	defer srv.Close()
	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b := &Batcher{client: client, endpoint: "/api/v1/events/ingest"}
	_, success, err := b.postOnce(context.Background(), []byte("unused"), "")
	if success || err == nil || !strings.Contains(err.Error(), "bad event type") || len(err.Error()) > 1100 {
		t.Fatalf("success=%v error=%v", success, err)
	}
}

func TestBatcherQuarantinesExpiredBatchAndReplaysFreshBatch(t *testing.T) {
	var accepted []Event
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		accepted, err = decodeEventRequest(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(202)
	}))
	defer srv.Close()
	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b := NewBatcher(client, NewStream(8), zap.NewNop(), BatcherOptions{SpoolDir: t.TempDir()})
	_, err = b.spool.AppendJSON(eventSpoolBatch{Events: []Event{{Type: "conn.open", TS: time.Now().Add(-8 * 24 * time.Hour)}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.spool.AppendJSON(eventSpoolBatch{Events: []Event{{Type: "conn.close", TS: time.Now()}}})
	if err != nil {
		t.Fatal(err)
	}
	b.drainSpool(context.Background())
	stats, err := b.SpoolStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Records != 0 || stats.QuarantinedRecords != 1 || len(accepted) != 1 || accepted[0].Type != "conn.close" {
		t.Fatalf("stats=%+v accepted=%+v", stats, accepted)
	}
}

func TestBatcherReplaysDiskSpoolAfterOutage(t *testing.T) {
	t.Parallel()

	var accept atomic.Bool
	var requests atomic.Int32
	var gotReplayKey atomic.Value
	accepted := make(chan []Event, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/events/ingest" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		if !accept.Load() {
			http.Error(w, "outage", http.StatusServiceUnavailable)
			return
		}
		gotReplayKey.Store(r.Header.Get("X-ControlOne-Replay-Key"))
		events, err := decodeEventRequest(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		accepted <- events
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client, err := api.NewClient(srv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	spoolDir := t.TempDir()
	stream := NewStream(4)
	batcher := NewBatcher(client, stream, zap.NewNop(), BatcherOptions{
		FlushInterval: 10 * time.Millisecond,
		MaxRows:       1,
		RetryMin:      time.Millisecond,
		RetryMax:      time.Millisecond,
		SpoolDir:      spoolDir,
		SpoolMaxBytes: 1 << 20,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go batcher.Run(ctx)

	stream.Publish(Event{Type: "proc.exec", NodeID: "node-1", Message: "whoami"})
	waitForEventRequestAttempt(t, &requests)
	batcher.Stop()

	spool, err := durablespool.New(durablespool.Options{Dir: spoolDir, Prefix: "eventstream"})
	if err != nil {
		t.Fatalf("durablespool.New: %v", err)
	}
	records, err := spool.Records()
	if err != nil {
		t.Fatalf("spool Records: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("expected failed batch to remain in disk spool")
	}

	accept.Store(true)
	replayBatcher := NewBatcher(client, NewStream(1), zap.NewNop(), BatcherOptions{
		FlushInterval: 10 * time.Millisecond,
		RetryMin:      time.Millisecond,
		RetryMax:      time.Millisecond,
		SpoolDir:      spoolDir,
		SpoolMaxBytes: 1 << 20,
	})
	replayCtx, replayCancel := context.WithCancel(context.Background())
	defer replayCancel()
	go replayBatcher.Run(replayCtx)
	defer replayBatcher.Stop()

	select {
	case events := <-accepted:
		if len(events) != 1 || events[0].Type != "proc.exec" || events[0].NodeID != "node-1" {
			t.Fatalf("replayed events = %#v", events)
		}
		if key, _ := gotReplayKey.Load().(string); key == "" {
			t.Fatal("expected replay key header on spooled event replay")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for spool replay")
	}
}

func TestEventReplayKeyStableForSameBatch(t *testing.T) {
	t.Parallel()

	events := []Event{{
		Type:    "proc.exec",
		TS:      time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC),
		NodeID:  "node-1",
		Message: "whoami",
		Details: map[string]any{
			"b": "two",
			"a": "one",
		},
	}}
	if got, want := eventReplayKey(events), eventReplayKey(events); got == "" || got != want {
		t.Fatalf("eventReplayKey = %q, want stable %q", got, want)
	}
}

func waitForEventRequestAttempt(t *testing.T, requests *atomic.Int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if requests.Load() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for first event ingest attempt")
}

func decodeEventRequest(body io.Reader) ([]Event, error) {
	zr, err := gzip.NewReader(body)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	scanner := bufio.NewScanner(zr)
	var events []Event
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
