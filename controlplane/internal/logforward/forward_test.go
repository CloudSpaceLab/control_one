package logforward

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestValidatesArgs(t *testing.T) {
	if _, err := New(nil, &captureSink{}, nil, Options{}); err == nil {
		t.Fatal("nil source must error")
	}
	if _, err := New(&fakeSource{}, nil, nil, Options{}); err == nil {
		t.Fatal("nil sink must error")
	}
}
