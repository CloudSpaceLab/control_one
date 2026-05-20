package doris

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// streamLoadOptions controls Doris Stream Load HTTP behaviour. Stream Load is
// the recommended bulk write path: rows are POST'd as JSON or CSV; the FE
// returns a per-batch status with row counts and error reports we surface as
// errors so callers can retry or alert.
type streamLoadOptions struct {
	Format          string // "json" (the only mode this writer uses)
	Strict          bool   // true → reject the whole batch on any row error
	Label           string // unique label for idempotent retries
	JSONPaths       []string
	StripOuterArray bool
	// Compress requests payloads with gzip when true. Doris Stream Load
	// honours `Content-Encoding: gzip`; saves ~80% on log-heavy traffic.
	Compress bool
}

// StreamLoadJSONOptions controls one explicit JSON Stream Load call.
type StreamLoadJSONOptions struct {
	// Label should be stable for durable replay. Doris deduplicates labels
	// for a bounded retention window, which lets callers safely retry after a
	// timeout without creating duplicate load jobs.
	Label    string
	Strict   bool
	Compress bool
}

// LoadStatus is the structured response Stream Load returns per request.
type LoadStatus struct {
	TxnID                int64  `json:"TxnId"`
	Label                string `json:"Label"`
	Status               string `json:"Status"`
	ExistingJobStatus    string `json:"ExistingJobStatus,omitempty"`
	Message              string `json:"Message,omitempty"`
	NumberTotalRows      int64  `json:"NumberTotalRows"`
	NumberLoadedRows     int64  `json:"NumberLoadedRows"`
	NumberFilteredRows   int64  `json:"NumberFilteredRows"`
	NumberUnselectedRows int64  `json:"NumberUnselectedRows"`
	LoadBytes            int64  `json:"LoadBytes"`
	LoadTimeMs           int64  `json:"LoadTimeMs"`
	ErrorURL             string `json:"ErrorURL,omitempty"`
}

// StreamLoadJSON posts rows to one Doris table and waits for the Stream Load
// response. Use this for journaled ingest where the caller must not mark a
// batch accepted until Doris has acknowledged it.
func (c *Client) StreamLoadJSON(ctx context.Context, table string, rows []map[string]any, opts StreamLoadJSONOptions) (*LoadStatus, error) {
	return c.streamLoad(ctx, table, rows, streamLoadOptions{
		Format:          "json",
		Strict:          opts.Strict,
		Label:           opts.Label,
		StripOuterArray: true,
		Compress:        opts.Compress,
	})
}

// streamLoad posts a JSON array of rows to Doris and returns the parsed
// LoadStatus. Caller is responsible for batching and retries. Compresses with
// gzip when payload exceeds the threshold or opts.Compress is set.
func (c *Client) streamLoad(ctx context.Context, table string, rows []map[string]any, opts streamLoadOptions) (*LoadStatus, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	endpoint := c.streamLoadURL(table)
	if endpoint == "" {
		return nil, errors.New("doris http endpoint not configured")
	}

	rawPayload, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("marshal rows: %w", err)
	}

	// Doris 2.1 rejects gzip-compressed JSON stream loads. Keep JSON payloads
	// uncompressed until the cluster supports them; CSV can still opt in later.
	useGzip := !strings.EqualFold(opts.Format, "json") && (opts.Compress || len(rawPayload) >= gzipThreshold)
	body, contentEncoding, err := maybeGzip(rawPayload, useGzip)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("format", "json")
	req.Header.Set("strip_outer_array", "true")
	if contentEncoding != "" {
		req.Header.Set("Content-Encoding", contentEncoding)
		// Doris also accepts a `compress_type` header explicitly.
		req.Header.Set("compress_type", contentEncoding)
	}
	if opts.Strict {
		req.Header.Set("strict_mode", "true")
	}
	if opts.Label != "" {
		req.Header.Set("label", opts.Label)
	}
	req.Header.Set("Expect", "100-continue")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stream load: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read stream load response: %w", err)
	}
	var status LoadStatus
	if err := json.Unmarshal(respBody, &status); err != nil {
		return nil, fmt.Errorf("decode stream load: %w (body=%s)", err, string(respBody))
	}
	if resp.StatusCode >= 400 || !streamLoadStatusAccepted(status) {
		return &status, fmt.Errorf("doris stream load %s: %s (loaded %d/%d, filtered %d, error_url=%s)",
			status.Status, status.Message, status.NumberLoadedRows, status.NumberTotalRows,
			status.NumberFilteredRows, status.ErrorURL)
	}
	return &status, nil
}

func streamLoadStatusAccepted(status LoadStatus) bool {
	switch strings.ToLower(strings.TrimSpace(status.Status)) {
	case "success", "publish timeout":
		return true
	case "label already exists":
		switch strings.ToLower(strings.TrimSpace(status.ExistingJobStatus)) {
		case "finished", "success", "publish timeout":
			return true
		}
	}
	return false
}

const gzipThreshold = 10 * 1024 // 10 KiB

func maybeGzip(payload []byte, use bool) (io.Reader, string, error) {
	if !use {
		return bytes.NewReader(payload), "", nil
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(payload); err != nil {
		_ = zw.Close()
		return nil, "", fmt.Errorf("gzip stream load payload: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, "", fmt.Errorf("gzip close: %w", err)
	}
	return &buf, "gzip", nil
}

// MetricsReporter receives per-flush observations so the caller can wire them
// to Prometheus, logs, or anywhere else. All fields are best-effort; nil
// implementations are no-ops.
type MetricsReporter interface {
	ObserveFlush(table string, rows int, bytes int, dur time.Duration, err error)
	ObserveQueueDepth(table string, depth int)
}

// nopReporter is the default when MetricsReporter is not configured.
type nopReporter struct{}

func (nopReporter) ObserveFlush(string, int, int, time.Duration, error) {}
func (nopReporter) ObserveQueueDepth(string, int)                       {}

// Writer batches rows per table and flushes on size or interval. One Writer
// per process; safe for concurrent calls.
type Writer struct {
	c              *Client
	mu             sync.Mutex
	pending        map[string][]map[string]any
	flushInterval  time.Duration
	maxBatchRows   int
	maxPendingRows int
	closed         atomic.Bool
	stopCh         chan struct{}
	doneCh         chan struct{}
	flushWG        sync.WaitGroup
	onError        func(table string, err error)
	metrics        MetricsReporter
	hostname       string
	nonce          string
	pid            int
	labelCounter   atomic.Uint64
	healthyAtomic  atomic.Bool
	cond           *sync.Cond
	compress       bool
}

// WriterOptions tune the writer.
type WriterOptions struct {
	FlushInterval  time.Duration // default 2s
	MaxBatchRows   int           // default 5000
	MaxPendingRows int           // default 50_000 (per table); above this Enqueue blocks
	Compress       bool          // force gzip for every payload (auto-detect when false)
	OnError        func(table string, err error)
	Metrics        MetricsReporter
}

// NewWriter starts the flush loop. Caller owns Close().
func NewWriter(c *Client, opts WriterOptions) *Writer {
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 2 * time.Second
	}
	if opts.MaxBatchRows <= 0 {
		opts.MaxBatchRows = 5000
	}
	if opts.MaxPendingRows <= 0 {
		opts.MaxPendingRows = 50_000
	}
	if opts.Metrics == nil {
		opts.Metrics = nopReporter{}
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "controlplane"
	}
	// 32-bit random nonce stamped into every label so two replicas that share
	// a hostname (rolling restart, blue/green with sticky DNS) cannot collide
	// on (table, label) inside Doris's 3-day dedup window.
	var nonceBytes [4]byte
	_, _ = rand.Read(nonceBytes[:])
	w := &Writer{
		c:              c,
		pending:        make(map[string][]map[string]any),
		flushInterval:  opts.FlushInterval,
		maxBatchRows:   opts.MaxBatchRows,
		maxPendingRows: opts.MaxPendingRows,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
		onError:        opts.OnError,
		metrics:        opts.Metrics,
		hostname:       host,
		pid:            os.Getpid(),
		nonce:          hex.EncodeToString(nonceBytes[:]),
		compress:       opts.Compress,
	}
	w.cond = sync.NewCond(&w.mu)
	w.healthyAtomic.Store(true)
	go w.loop()
	return w
}

// ErrWriterClosed is returned by Enqueue after Close().
var ErrWriterClosed = errors.New("doris writer closed")

// ErrBackpressure is returned by EnqueueNonBlocking when the per-table buffer
// is at its cap. Callers should drop or retry later.
var ErrBackpressure = errors.New("doris writer backpressure")

// Enqueue appends rows to the per-table buffer. Blocks when the table buffer
// has hit MaxPendingRows so producers naturally back off if Doris is slow or
// unavailable. Auto-flushes when the table hits MaxBatchRows.
func (w *Writer) Enqueue(table string, rows []map[string]any) error {
	return w.enqueue(table, rows, true)
}

// EnqueueNonBlocking is like Enqueue but returns ErrBackpressure instead of
// blocking when the buffer is full. Use from latency-sensitive callers (e.g.
// HTTP handlers); fall back to a journal write on backpressure.
func (w *Writer) EnqueueNonBlocking(table string, rows []map[string]any) error {
	return w.enqueue(table, rows, false)
}

func (w *Writer) enqueue(table string, rows []map[string]any, block bool) error {
	if w == nil || w.c == nil {
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	w.mu.Lock()
	// closed must be re-checked under the lock so Close() — which sets the
	// flag and then waits on flushWG — cannot race with a flushWG.Add() that
	// happens after Wait() has already returned.
	if w.closed.Load() {
		w.mu.Unlock()
		return ErrWriterClosed
	}
	for len(w.pending[table]) >= w.maxPendingRows {
		if !block {
			w.mu.Unlock()
			return ErrBackpressure
		}
		w.cond.Wait()
		if w.closed.Load() {
			w.mu.Unlock()
			return ErrWriterClosed
		}
	}
	w.pending[table] = append(w.pending[table], rows...)
	depth := len(w.pending[table])
	overflow := depth >= w.maxBatchRows
	w.metrics.ObserveQueueDepth(table, depth)
	if overflow {
		batch := w.pending[table]
		w.pending[table] = nil
		// Add to the waitgroup before releasing the lock so Close() — which
		// sets closed under the same lock — sees a coherent counter when it
		// transitions to Wait().
		w.flushWG.Add(1)
		w.cond.Broadcast()
		w.mu.Unlock()
		go func() {
			defer w.flushWG.Done()
			w.flushTable(table, batch)
		}()
		return nil
	}
	w.mu.Unlock()
	return nil
}

// Healthy reports whether the most recent flush succeeded. Use as a signal in
// the ingest path to journal-and-retry instead of trying every batch.
func (w *Writer) Healthy() bool {
	if w == nil {
		return false
	}
	return w.healthyAtomic.Load()
}

// Close flushes pending batches then stops the loop. Safe to call multiple
// times; subsequent calls are no-ops.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	if !w.closed.CompareAndSwap(false, true) {
		return nil
	}
	w.mu.Lock()
	w.cond.Broadcast() // unblock any waiting Enqueue callers
	w.mu.Unlock()
	close(w.stopCh)
	<-w.doneCh
	w.flushWG.Wait()
	return nil
}

func (w *Writer) loop() {
	defer close(w.doneCh)
	t := time.NewTicker(w.flushInterval)
	defer t.Stop()
	for {
		select {
		case <-w.stopCh:
			w.flushAll()
			return
		case <-t.C:
			w.flushAll()
		}
	}
}

func (w *Writer) flushAll() {
	w.mu.Lock()
	if len(w.pending) == 0 {
		w.mu.Unlock()
		return
	}
	snapshot := w.pending
	w.pending = make(map[string][]map[string]any, len(snapshot))
	w.cond.Broadcast()
	w.mu.Unlock()

	for table, rows := range snapshot {
		if len(rows) == 0 {
			continue
		}
		w.flushWG.Add(1)
		go func(tbl string, batch []map[string]any) {
			defer w.flushWG.Done()
			w.flushTable(tbl, batch)
		}(table, rows)
	}
}

func (w *Writer) flushTable(table string, rows []map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	label := w.makeLabel(table)
	start := time.Now()
	_, err := w.c.streamLoad(ctx, table, rows, streamLoadOptions{
		Format:          "json",
		StripOuterArray: true,
		Label:           label,
		Compress:        w.compress,
	})
	dur := time.Since(start)
	// Approximate payload bytes with the marshaled length (good enough for
	// Prometheus histograms).
	bytesEstimate := approxBytes(rows)
	w.metrics.ObserveFlush(table, len(rows), bytesEstimate, dur, err)
	if err != nil {
		w.healthyAtomic.Store(false)
		if w.onError != nil {
			w.onError(table, err)
		}
		return
	}
	w.healthyAtomic.Store(true)
}

func approxBytes(rows []map[string]any) int {
	// Cheap upper-bound: estimate 250 B per row pre-compression. Avoid a real
	// json.Marshal to keep flush hot-path light.
	return len(rows) * 250
}

// makeLabel composes a Stream Load label that is unique across replicas.
// Doris dedups by (table, label) within ~3 days; we want one unique label per
// flush so our retries on transient errors can reuse the same label and let
// Doris's built-in dedup absorb duplicates safely.
//
// Format: co-{hostname}-{pid}-{nonce}-{nanos}-{counter}-{table}
// {nonce} is a per-Writer random hex string so two replicas that happen to
// share a hostname (and even a pid in container restart edge cases) still
// emit non-colliding labels.
func (w *Writer) makeLabel(table string) string {
	n := time.Now().UnixNano()
	c := w.labelCounter.Add(1)
	host := strings.ReplaceAll(w.hostname, ".", "-")
	return fmt.Sprintf("co-%s-%d-%s-%d-%d-%s", host, w.pid, w.nonce, n, c, sanitizeLabel(table))
}

func sanitizeLabel(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-':
			return r
		default:
			return '-'
		}
	}, s)
}

// LabelFor returns a deterministic Stream Load label so retries hit the same
// transaction id and Doris dedupes via its built-in label store.
func LabelFor(table string, batchID string) string {
	return strings.ReplaceAll(fmt.Sprintf("%s-%s", table, batchID), "/", "-")
}
