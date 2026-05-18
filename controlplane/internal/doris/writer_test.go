package doris

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestStreamLoadBuildsRequest(t *testing.T) {
	var (
		gotMethod   string
		gotPath     string
		gotBody     []byte
		gotAuth     string
		gotEncoding string
		mu          sync.Mutex
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotEncoding = r.Header.Get("Content-Encoding")
		body, _ := io.ReadAll(r.Body)
		gotBody = body
		_ = json.NewEncoder(w).Encode(LoadStatus{
			Status: "Success", NumberLoadedRows: 2, NumberTotalRows: 2,
		})
	}))
	defer server.Close()

	c := &Client{
		cfg:  Config{HTTPEndpoint: server.URL, Database: "controlone", User: "u", Password: "p"},
		db:   nil,
		http: &http.Client{Timeout: 5 * time.Second},
	}
	rows := []map[string]any{
		{"tenant_id": "t1", "message": "hello"},
		{"tenant_id": "t1", "message": "world"},
	}
	st, err := c.streamLoad(context.Background(), "telemetry_logs", rows, streamLoadOptions{Format: "json", Label: "test-1", Compress: true})
	if err != nil {
		t.Fatal(err)
	}
	if st.NumberLoadedRows != 2 {
		t.Fatalf("want 2 loaded, got %d", st.NumberLoadedRows)
	}
	if gotMethod != http.MethodPut {
		t.Fatalf("want PUT, got %s", gotMethod)
	}
	if gotPath != "/api/controlone/telemetry_logs/_stream_load" {
		t.Fatalf("unexpected path %s", gotPath)
	}
	if gotAuth == "" {
		t.Fatal("auth header missing")
	}
	if gotEncoding != "" {
		t.Fatalf("json stream loads must stay uncompressed, got Content-Encoding=%q", gotEncoding)
	}
	if len(gotBody) < 10 {
		t.Fatalf("body too short: %s", string(gotBody))
	}
}

func TestStreamLoadFailsOnNonSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(LoadStatus{Status: "Fail", Message: "bad row", NumberFilteredRows: 5})
	}))
	defer server.Close()
	c := &Client{
		cfg:  Config{HTTPEndpoint: server.URL, Database: "co"},
		http: &http.Client{Timeout: 5 * time.Second},
	}
	if _, err := c.streamLoad(context.Background(), "x", []map[string]any{{"a": 1}}, streamLoadOptions{}); err == nil {
		t.Fatal("expected error on failure status")
	}
}

func TestWriterFlushesOnInterval(t *testing.T) {
	var pushed int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var rows []map[string]any
		_ = json.Unmarshal(body, &rows)
		pushed += len(rows)
		_ = json.NewEncoder(w).Encode(LoadStatus{Status: "Success", NumberLoadedRows: int64(len(rows))})
	}))
	defer server.Close()
	c := &Client{cfg: Config{HTTPEndpoint: server.URL, Database: "co"}, http: &http.Client{Timeout: 5 * time.Second}}

	w := NewWriter(c, WriterOptions{FlushInterval: 100 * time.Millisecond})
	if err := w.Enqueue("x", []map[string]any{{"a": 1}, {"a": 2}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	time.Sleep(250 * time.Millisecond)
	_ = w.Close()
	if pushed != 2 {
		t.Fatalf("want 2 pushed, got %d", pushed)
	}
}

func TestWriterFlushesOnSize(t *testing.T) {
	var pushed int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var rows []map[string]any
		_ = json.Unmarshal(body, &rows)
		pushed += len(rows)
		_ = json.NewEncoder(w).Encode(LoadStatus{Status: "Success", NumberLoadedRows: int64(len(rows))})
	}))
	defer server.Close()
	c := &Client{cfg: Config{HTTPEndpoint: server.URL, Database: "co"}, http: &http.Client{Timeout: 5 * time.Second}}
	w := NewWriter(c, WriterOptions{FlushInterval: time.Hour, MaxBatchRows: 3})
	if err := w.Enqueue("x", []map[string]any{{"a": 1}, {"a": 2}, {"a": 3}}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = w.Close()
	if pushed != 3 {
		t.Fatalf("want 3, got %d", pushed)
	}
}
