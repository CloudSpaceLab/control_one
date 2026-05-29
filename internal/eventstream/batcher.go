package eventstream

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/durablespool"
)

// Batcher drains a Stream and POSTs ndjson batches to the controlplane.
// Configuration knobs:
//
//	FlushInterval: 2s default. Forces a flush even when the batch is small,
//	               so latency-sensitive events (alerts, conn.open) reach
//	               the controlplane quickly.
//	FlushBytes:    256 KiB default. Forces a flush when the encoded batch
//	               hits this threshold so we never produce a 1+ MiB POST.
//	MaxRows:       5_000 default. Ceiling matches the server-side cap.
//	RetryBackoff:  exponential 0.5s → 30s on 5xx + 429.
type Batcher struct {
	client       *api.Client
	log          *zap.Logger
	stream       *Stream
	endpoint     string
	flushEvery   time.Duration
	flushBytes   int
	maxRows      int
	retryMin     time.Duration
	retryMax     time.Duration
	stamp        func(*Event)
	spool        *durablespool.Spool
	mu           sync.Mutex
	pending      []Event
	pendingBytes int
	stopCh       chan struct{}
	doneCh       chan struct{}
}

// BatcherOptions tune the batcher.
type BatcherOptions struct {
	Endpoint      string        // path on the controlplane, default "/api/v1/events/ingest"
	FlushInterval time.Duration // default 2s
	FlushBytes    int           // default 256 KiB
	MaxRows       int           // default 5000
	RetryMin      time.Duration // default 500ms
	RetryMax      time.Duration // default 30s
	// Stamp is called for every event before it is added to the pending
	// batch. Used to attach correlation IDs across streams.
	Stamp func(*Event)
	// SpoolDir enables disk-backed event replay for outage/restart safety.
	SpoolDir      string
	SpoolMaxBytes int64
}

// NewBatcher constructs a batcher; call Run to start the loop.
func NewBatcher(client *api.Client, stream *Stream, log *zap.Logger, opts BatcherOptions) *Batcher {
	if opts.Endpoint == "" {
		opts.Endpoint = "/api/v1/events/ingest"
	}
	if opts.FlushInterval <= 0 {
		opts.FlushInterval = 2 * time.Second
	}
	if opts.FlushBytes <= 0 {
		opts.FlushBytes = 256 * 1024
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = 5000
	}
	if opts.RetryMin <= 0 {
		opts.RetryMin = 500 * time.Millisecond
	}
	if opts.RetryMax <= 0 {
		opts.RetryMax = 30 * time.Second
	}
	b := &Batcher{
		client:     client,
		log:        log,
		stream:     stream,
		endpoint:   opts.Endpoint,
		flushEvery: opts.FlushInterval,
		flushBytes: opts.FlushBytes,
		maxRows:    opts.MaxRows,
		retryMin:   opts.RetryMin,
		retryMax:   opts.RetryMax,
		stamp:      opts.Stamp,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
	if strings.TrimSpace(opts.SpoolDir) != "" {
		spool, err := durablespool.New(durablespool.Options{
			Dir:      opts.SpoolDir,
			Prefix:   "eventstream",
			MaxBytes: opts.SpoolMaxBytes,
		})
		if err != nil {
			if log != nil {
				log.Warn("eventstream spool disabled", zap.String("dir", opts.SpoolDir), zap.Error(err))
			}
		} else {
			b.spool = spool
		}
	}
	return b
}

// Run drains until ctx is cancelled. Safe to run as a goroutine. On exit a
// final flush attempts to ship anything pending so a clean shutdown doesn't
// drop in-flight events.
func (b *Batcher) Run(ctx context.Context) {
	defer close(b.doneCh)
	t := time.NewTicker(b.flushEvery)
	defer t.Stop()
	b.drainSpool(ctx)
	for {
		select {
		case <-ctx.Done():
			b.flush(ctx)
			return
		case <-b.stopCh:
			b.flush(context.Background())
			return
		case <-t.C:
			b.drainSpool(ctx)
			b.flush(ctx)
		case ev, ok := <-b.stream.Out():
			if !ok {
				b.flush(ctx)
				return
			}
			b.append(ev, ctx)
		}
	}
}

// Stop signals the loop to drain + exit. Run() returns after the final
// flush. Idempotent.
func (b *Batcher) Stop() {
	select {
	case <-b.stopCh:
		return
	default:
	}
	close(b.stopCh)
	<-b.doneCh
}

func (b *Batcher) SpoolStats() (durablespool.Stats, error) {
	if b == nil || b.spool == nil {
		return durablespool.Stats{}, nil
	}
	return b.spool.Stats()
}

func (b *Batcher) append(ev Event, ctx context.Context) {
	if b.stamp != nil {
		b.stamp(&ev)
	}
	encoded, err := json.Marshal(ev)
	if err != nil {
		if b.log != nil {
			b.log.Warn("event encode", zap.Error(err))
		}
		return
	}
	b.mu.Lock()
	b.pending = append(b.pending, ev)
	b.pendingBytes += len(encoded) + 1 // newline
	overflow := len(b.pending) >= b.maxRows || b.pendingBytes >= b.flushBytes
	b.mu.Unlock()
	if overflow {
		b.flush(ctx)
	}
}

func (b *Batcher) flush(ctx context.Context) {
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	batch := b.pending
	b.pending = nil
	b.pendingBytes = 0
	b.mu.Unlock()

	body, err := encodeBatch(batch)
	if err != nil {
		if b.log != nil {
			b.log.Warn("encode batch", zap.Error(err))
		}
		return
	}
	if b.spool != nil {
		spooled := eventSpoolBatch{ReplayKey: eventReplayKey(batch), Events: batch}
		if _, err := b.spool.AppendJSON(spooled); err != nil {
			if b.log != nil {
				b.log.Warn("eventstream spool append failed; posting batch directly", zap.Int("rows", len(batch)), zap.Error(err))
			}
			if err := b.postWithRetry(ctx, body, spooled.ReplayKey); err != nil && b.log != nil {
				b.log.Warn("ingest post failed",
					zap.Int("rows", len(batch)), zap.Error(err))
			}
			return
		}
		b.drainSpool(ctx)
		return
	}
	if err := b.postWithRetry(ctx, body, eventReplayKey(batch)); err != nil && b.log != nil {
		b.log.Warn("ingest post failed",
			zap.Int("rows", len(batch)), zap.Error(err))
	}
}

type eventSpoolBatch struct {
	ReplayKey string  `json:"replay_key,omitempty"`
	Events    []Event `json:"events"`
}

func (b *Batcher) drainSpool(ctx context.Context) {
	if b == nil || b.spool == nil || ctx.Err() != nil {
		return
	}
	records, err := b.spool.Records()
	if err != nil {
		if b.log != nil {
			b.log.Warn("eventstream spool list failed", zap.Error(err))
		}
		return
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return
		}
		data, err := b.spool.Read(record)
		if err != nil {
			if b.log != nil {
				b.log.Warn("eventstream spool read failed", zap.String("path", record.Path), zap.Error(err))
			}
			return
		}
		var batch eventSpoolBatch
		if err := json.Unmarshal(data, &batch); err != nil || len(batch.Events) == 0 {
			if b.log != nil {
				b.log.Warn("eventstream spool record invalid; dropping", zap.String("path", record.Path), zap.Error(err))
			}
			_ = b.spool.Delete(record)
			continue
		}
		body, err := encodeBatch(batch.Events)
		if err != nil {
			if b.log != nil {
				b.log.Warn("eventstream spool encode failed; dropping", zap.String("path", record.Path), zap.Error(err))
			}
			_ = b.spool.Delete(record)
			continue
		}
		replayKey := strings.TrimSpace(batch.ReplayKey)
		if replayKey == "" {
			replayKey = eventReplayKey(batch.Events)
		}
		if err := b.postWithRetry(ctx, body, replayKey); err != nil {
			if b.log != nil {
				b.log.Warn("eventstream spool replay failed", zap.String("path", record.Path), zap.Int("rows", len(batch.Events)), zap.Error(err))
			}
			return
		}
		if err := b.spool.Delete(record); err != nil {
			if b.log != nil {
				b.log.Warn("eventstream spool delete failed", zap.String("path", record.Path), zap.Error(err))
			}
			return
		}
	}
}

func eventReplayKey(events []Event) string {
	raw, err := json.Marshal(events)
	if err != nil {
		raw = []byte(fmt.Sprintf("%#v", events))
	}
	sum := sha256.Sum256(raw)
	return "eventstream:" + hex.EncodeToString(sum[:])
}

func encodeBatch(events []Event) ([]byte, error) {
	var raw bytes.Buffer
	enc := json.NewEncoder(&raw)
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return nil, fmt.Errorf("encode event %d: %w", i, err)
		}
	}
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		_ = zw.Close()
		return nil, fmt.Errorf("gzip encode: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip close: %w", err)
	}
	return gz.Bytes(), nil
}

func (b *Batcher) postWithRetry(ctx context.Context, body []byte, replayKey string) error {
	delay := b.retryMin
	for attempt := 0; attempt < 6; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		retryAfter, ok, err := b.postOnce(ctx, body, replayKey)
		if ok {
			return nil
		}
		if retryAfter > 0 {
			delay = retryAfter
		}
		if err != nil && b.log != nil {
			b.log.Debug("ingest retry", zap.Int("attempt", attempt+1), zap.Duration("delay", delay), zap.Error(err))
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		delay *= 2
		if delay > b.retryMax {
			delay = b.retryMax
		}
	}
	return fmt.Errorf("ingest exhausted retries")
}

// postOnce returns (retryAfter, success, err). retryAfter is the server-hinted
// delay (from `Retry-After`); success is true on 2xx; err carries non-fatal
// transport / 5xx errors.
func (b *Batcher) postOnce(ctx context.Context, body []byte, replayKey string) (time.Duration, bool, error) {
	if b.client == nil {
		return 0, false, fmt.Errorf("api client nil")
	}
	headers := map[string]string{
		"Content-Type":     "application/x-ndjson",
		"Content-Encoding": "gzip",
	}
	if replayKey = strings.TrimSpace(replayKey); replayKey != "" {
		headers["X-ControlOne-Replay-Key"] = replayKey
	}
	resp, err := b.client.DoWithHeaders(ctx, http.MethodPost, b.endpoint, body, headers)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return 0, true, nil
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		ra := parseRetryAfter(resp.Header.Get("Retry-After"))
		return ra, false, fmt.Errorf("ingest status %d", resp.StatusCode)
	}
	return 0, false, fmt.Errorf("ingest status %d (non-retryable)", resp.StatusCode)
}

func parseRetryAfter(s string) time.Duration {
	if s == "" {
		return 0
	}
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return time.Duration(n * float64(time.Second))
	}
	if t, err := http.ParseTime(s); err == nil {
		d := time.Until(t)
		if d > 0 {
			return d
		}
	}
	return 0
}
