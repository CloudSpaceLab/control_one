// Package logforward pushes telemetry_logs rows out to an external sink
// (Loki or Elasticsearch) so operators can use their existing search + alert
// stack. It is sink-agnostic: callers supply a Sink implementation; the
// forwarder handles batching, backoff, and checkpointing.
package logforward

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

// LogRecord is the minimal shape the forwarder cares about. Adapters inside
// the package translate this to Loki streams or ES bulk payloads.
type LogRecord struct {
	Timestamp time.Time
	Level     string
	Message   string
	Source    string
	Program   string
	NodeID    string
	TenantID  string
	Labels    map[string]string
}

// Sink is the destination backend. Push must accept any size batch (the
// forwarder will already have enforced MaxBatchSize when building the slice).
type Sink interface {
	Push(ctx context.Context, records []LogRecord) error
	Name() string
}

// Source pulls the next batch of records newer than cursor and returns the
// records plus a new cursor the forwarder can persist between ticks. Empty
// results (len == 0) are the signal to sleep until the next tick.
type Source interface {
	Fetch(ctx context.Context, cursor time.Time, limit int) ([]LogRecord, time.Time, error)
}

// Options controls the forwarder loop.
type Options struct {
	Interval      time.Duration // min: 1s; default 5s
	MaxBatchSize  int           // default 500
	InitialCursor time.Time     // where to start reading from
}

// Forwarder loops source -> sink until ctx is cancelled.
type Forwarder struct {
	src  Source
	sink Sink
	log  *zap.Logger
	opts Options
}

func New(src Source, sink Sink, log *zap.Logger, opts Options) (*Forwarder, error) {
	if src == nil || sink == nil {
		return nil, errors.New("source and sink required")
	}
	if opts.Interval <= 0 {
		opts.Interval = 5 * time.Second
	}
	if opts.Interval < time.Second {
		opts.Interval = time.Second
	}
	if opts.MaxBatchSize <= 0 {
		opts.MaxBatchSize = 500
	}
	return &Forwarder{src: src, sink: sink, log: log, opts: opts}, nil
}

// Run blocks until ctx is cancelled. Error paths log + back off rather than
// aborting the forwarder — operators can still push again from the checkpoint.
func (f *Forwarder) Run(ctx context.Context) {
	cursor := f.opts.InitialCursor
	if cursor.IsZero() {
		cursor = time.Now().UTC()
	}
	backoff := f.opts.Interval
	for {
		if ctx.Err() != nil {
			return
		}
		records, next, err := f.src.Fetch(ctx, cursor, f.opts.MaxBatchSize)
		if err != nil {
			if f.log != nil {
				f.log.Warn("log forwarder fetch", zap.Error(err), zap.String("sink", f.sink.Name()))
			}
			f.sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		if len(records) == 0 {
			f.sleep(ctx, f.opts.Interval)
			continue
		}
		if err := f.sink.Push(ctx, records); err != nil {
			if f.log != nil {
				f.log.Warn("log forwarder push", zap.Error(err), zap.String("sink", f.sink.Name()))
			}
			f.sleep(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		cursor = next
		backoff = f.opts.Interval
	}
}

func (f *Forwarder) sleep(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C:
	}
}

func nextBackoff(cur time.Duration) time.Duration {
	next := cur * 2
	if next > 30*time.Second {
		next = 30 * time.Second
	}
	return next
}
