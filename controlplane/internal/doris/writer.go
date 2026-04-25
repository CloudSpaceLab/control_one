package doris

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// streamLoadOptions controls Doris Stream Load HTTP behaviour. Stream Load is
// the recommended bulk write path: rows are POST'd as JSON or CSV; the FE
// returns a per-batch status with row counts and error reports we surface as
// errors so callers can retry or alert.
type streamLoadOptions struct {
	Format         string // "json" (the only mode this writer uses)
	Strict         bool   // true → reject the whole batch on any row error
	Label          string // unique label for idempotent retries
	JSONPaths      []string
	StripOuterArray bool
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

// streamLoad posts a JSON array of rows to Doris and returns the parsed
// LoadStatus. Caller is responsible for batching and retries.
func (c *Client) streamLoad(ctx context.Context, table string, rows []map[string]any, opts streamLoadOptions) (*LoadStatus, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	endpoint := c.streamLoadURL(table)
	if endpoint == "" {
		return nil, errors.New("doris http endpoint not configured")
	}

	payload, err := json.Marshal(rows)
	if err != nil {
		return nil, fmt.Errorf("marshal rows: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.Password)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("format", "json")
	req.Header.Set("strip_outer_array", "true")
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read stream load response: %w", err)
	}
	var status LoadStatus
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, fmt.Errorf("decode stream load: %w (body=%s)", err, string(body))
	}
	if resp.StatusCode >= 400 || (status.Status != "Success" && status.Status != "Publish Timeout") {
		return &status, fmt.Errorf("doris stream load %s: %s (loaded %d/%d, filtered %d, error_url=%s)",
			status.Status, status.Message, status.NumberLoadedRows, status.NumberTotalRows,
			status.NumberFilteredRows, status.ErrorURL)
	}
	return &status, nil
}

// Writer batches rows per table and flushes on size or interval. One Writer
// per process; safe for concurrent calls.
type Writer struct {
	c             *Client
	mu            sync.Mutex
	pending       map[string][]map[string]any
	flushBytes    int
	flushInterval time.Duration
	maxBatchRows  int
	closed        bool
	stopCh        chan struct{}
	doneCh        chan struct{}
	onError       func(table string, err error)
}

// WriterOptions tune the writer.
type WriterOptions struct {
	FlushInterval time.Duration            // default 2s
	MaxBatchRows  int                      // default 5000
	OnError       func(table string, err error)
}

// NewWriter starts the flush loop. Caller owns Close().
func NewWriter(c *Client, opts WriterOptions) *Writer {
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 2 * time.Second
	}
	if opts.MaxBatchRows <= 0 {
		opts.MaxBatchRows = 5000
	}
	w := &Writer{
		c:             c,
		pending:       make(map[string][]map[string]any),
		flushInterval: opts.FlushInterval,
		maxBatchRows:  opts.MaxBatchRows,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		onError:       opts.OnError,
	}
	go w.loop()
	return w
}

// Enqueue appends rows to the per-table buffer. Auto-flushes when the table
// hits MaxBatchRows.
func (w *Writer) Enqueue(table string, rows []map[string]any) {
	if w == nil || w.c == nil || len(rows) == 0 {
		return
	}
	w.mu.Lock()
	w.pending[table] = append(w.pending[table], rows...)
	overflow := len(w.pending[table]) >= w.maxBatchRows
	if overflow {
		batch := w.pending[table]
		w.pending[table] = nil
		w.mu.Unlock()
		w.flushTable(table, batch)
		return
	}
	w.mu.Unlock()
}

// Close flushes pending batches then stops the loop. Safe to call multiple
// times; subsequent calls are no-ops.
func (w *Writer) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.stopCh)
	w.mu.Unlock()
	<-w.doneCh
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
	w.mu.Unlock()

	for table, rows := range snapshot {
		if len(rows) == 0 {
			continue
		}
		w.flushTable(table, rows)
	}
}

func (w *Writer) flushTable(table string, rows []map[string]any) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	label := fmt.Sprintf("co-%s-%d", table, time.Now().UnixNano())
	_, err := w.c.streamLoad(ctx, table, rows, streamLoadOptions{
		Format:          "json",
		StripOuterArray: true,
		Label:           label,
	})
	if err != nil && w.onError != nil {
		w.onError(table, err)
	}
}

// LabelFor returns a deterministic Stream Load label so retries hit the same
// transaction id and Doris dedupes via its built-in label store.
func LabelFor(table string, batchID string) string {
	return strings.ReplaceAll(fmt.Sprintf("%s-%s", table, batchID), "/", "-")
}
