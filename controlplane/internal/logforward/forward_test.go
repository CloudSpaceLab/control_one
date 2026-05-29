package logforward

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeSource struct {
	mu      sync.Mutex
	batches [][]LogRecord
	exhaust int32
}

func (f *fakeSource) Fetch(_ context.Context, cursor time.Time, _ int) ([]LogRecord, time.Time, error) {
	if atomic.LoadInt32(&f.exhaust) > 0 {
		return nil, cursor, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.batches) == 0 {
		atomic.StoreInt32(&f.exhaust, 1)
		return nil, cursor, nil
	}
	batch := f.batches[0]
	f.batches = f.batches[1:]
	next := cursor.Add(time.Minute)
	return batch, next, nil
}

type captureSink struct {
	mu  sync.Mutex
	got [][]LogRecord
}

func (c *captureSink) Name() string { return "capture" }
func (c *captureSink) Push(_ context.Context, r []LogRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, r)
	return nil
}

func TestForwarderPushesBatch(t *testing.T) {
	src := &fakeSource{
		batches: [][]LogRecord{
			{{Message: "a"}, {Message: "b"}},
		},
	}
	sink := &captureSink{}
	fwd, err := New(src, sink, nil, Options{Interval: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	fwd.Run(ctx)
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.got) != 1 || len(sink.got[0]) != 2 {
		t.Fatalf("unexpected pushes: %+v", sink.got)
	}
}

func TestLokiSinkSerialization(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		got = string(buf)
		w.WriteHeader(200)
	}))
	defer server.Close()
	sink := NewLokiSink(server.URL, "tenant-x")
	err := sink.Push(context.Background(), []LogRecord{{
		Timestamp: time.Now(), Level: "info", Message: "hello", Source: "auth",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 {
		t.Fatal("server got no body")
	}
}

func TestElasticSinkSerialization(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer server.Close()
	sink := NewElasticSink(server.URL, "")
	if err := sink.Push(context.Background(), []LogRecord{{Message: "hi"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSplunkHECSinkSerialization(t *testing.T) {
	var gotBody string
	var gotAuth string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := NewSplunkHECSink(server.URL, "token-123", "control-one", "controlone:telemetry", "main")
	err := sink.Push(context.Background(), []LogRecord{{
		Timestamp: time.Unix(1710000000, 123000000).UTC(),
		Level:     "warn",
		Message:   "blocked login",
		Source:    "ad",
		Program:   "wec",
		NodeID:    "node-1",
		TenantID:  "tenant-a",
		Labels: map[string]string{
			"attack":  "T1110",
			"channel": "Security",
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Splunk token-123" {
		t.Fatalf("unexpected Authorization header %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected Content-Type header %q", gotContentType)
	}
	lines := strings.Split(strings.TrimSpace(gotBody), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one HEC event, got %d lines: %q", len(lines), gotBody)
	}
	var event map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatal(err)
	}
	if event["host"] != "node-1" || event["source"] != "ad" || event["sourcetype"] != "controlone:telemetry" || event["index"] != "main" {
		t.Fatalf("unexpected HEC envelope: %#v", event)
	}
	gotTime, ok := event["time"].(float64)
	if !ok || math.Abs(gotTime-1710000000.123) > 0.000001 {
		t.Fatalf("unexpected HEC event time: %#v", event["time"])
	}
	payload, ok := event["event"].(map[string]any)
	if !ok {
		t.Fatalf("HEC event payload missing: %#v", event["event"])
	}
	if payload["message"] != "blocked login" || payload["tenant_id"] != "tenant-a" || payload["source"] != "ad" {
		t.Fatalf("unexpected HEC event payload: %#v", payload)
	}
	fields, ok := event["fields"].(map[string]any)
	if !ok {
		t.Fatalf("HEC fields missing: %#v", event["fields"])
	}
	if fields["tenant_id"] != "tenant-a" || fields["node_id"] != "node-1" || fields["label.attack"] != "T1110" {
		t.Fatalf("unexpected HEC fields: %#v", fields)
	}
}

func TestAzureMonitorSinkSerialization(t *testing.T) {
	var gotBody string
	var gotAuth string
	var gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		gotBody = string(raw)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sink := NewAzureMonitorSink(server.URL, "aad-token")
	err := sink.Push(context.Background(), []LogRecord{{
		Timestamp: time.Date(2026, 5, 29, 12, 0, 0, 123, time.UTC),
		Level:     "warn",
		Message:   "blocked login",
		Source:    "wec",
		Program:   "windows",
		NodeID:    "node-1",
		TenantID:  "tenant-a",
		Labels:    map[string]string{"channel": "Security"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer aad-token" {
		t.Fatalf("unexpected Authorization header %q", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("unexpected Content-Type header %q", gotContentType)
	}
	var payload []map[string]any
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload) != 1 || payload[0]["Message"] != "blocked login" || payload[0]["TenantId"] != "tenant-a" {
		t.Fatalf("unexpected Azure Monitor payload: %#v", payload)
	}
	if payload[0]["TimeGenerated"] == "" {
		t.Fatalf("TimeGenerated missing: %#v", payload[0])
	}
}

func TestNewSinkFromConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  SinkConfig
		want string
	}{
		{name: "loki", cfg: SinkConfig{Kind: "loki", URL: "http://loki.example"}, want: "loki"},
		{name: "elastic", cfg: SinkConfig{Kind: "elastic", URL: "http://elastic.example/_bulk"}, want: "elasticsearch"},
		{name: "splunk", cfg: SinkConfig{Kind: "splunk_hec", URL: "http://splunk.example/services/collector", Token: "token"}, want: "splunk_hec"},
		{name: "sentinel", cfg: SinkConfig{Kind: "sentinel", URL: "http://azure.example/dataCollectionRules/dcr/streams/Custom-ControlOne", Token: "token"}, want: "azure_monitor"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sink, err := NewSinkFromConfig(tt.cfg)
			if err != nil {
				t.Fatal(err)
			}
			if sink.Name() != tt.want {
				t.Fatalf("sink name = %q, want %q", sink.Name(), tt.want)
			}
		})
	}

	for _, cfg := range []SinkConfig{
		{Kind: "loki"},
		{Kind: "elasticsearch"},
		{Kind: "splunk_hec", URL: "http://splunk.example/services/collector"},
		{Kind: "sentinel", URL: "http://azure.example/dataCollectionRules/dcr/streams/Custom-ControlOne"},
		{Kind: "unknown", URL: "http://example"},
	} {
		if _, err := NewSinkFromConfig(cfg); err == nil {
			t.Fatalf("expected config error for %+v", cfg)
		}
	}
}

func TestValidatesArgs(t *testing.T) {
	if _, err := New(nil, &captureSink{}, nil, Options{}); err == nil {
		t.Fatal("nil source must error")
	}
	if _, err := New(&fakeSource{}, nil, nil, Options{}); err == nil {
		t.Fatal("nil sink must error")
	}
}
