package fileaccess

import (
	"sync"
	"time"
)

// bucketAggregator aggregates per (pid, path, op) within a fixed window so
// the collector emits one row per file per process per window instead of
// per-syscall. Compiles get-noisy paths down to a single event.
type bucketAggregator struct {
	mu      sync.Mutex
	window  time.Duration
	buckets map[bucketKey]*bucketAccum
}

type bucketKey struct {
	pid    int
	path   string
	op     string
	bucket int64 // unix seconds, truncated to window
}

type bucketAccum struct {
	bytes    int64
	count    int
	process  string
	user     string
	startedAt time.Time
	endedAt  time.Time
	corr     string
}

func newBucketAggregator(window time.Duration) *bucketAggregator {
	if window <= 0 {
		window = 5 * time.Second
	}
	return &bucketAggregator{window: window, buckets: make(map[bucketKey]*bucketAccum)}
}

func (b *bucketAggregator) add(ev FileEvent) {
	if ev.StartedAt.IsZero() {
		ev.StartedAt = time.Now().UTC()
	}
	if ev.EndedAt.IsZero() {
		ev.EndedAt = ev.StartedAt
	}
	bucket := ev.StartedAt.Truncate(b.window).Unix()
	key := bucketKey{pid: ev.PID, path: ev.Path, op: ev.Op, bucket: bucket}
	b.mu.Lock()
	defer b.mu.Unlock()
	a, ok := b.buckets[key]
	if !ok {
		a = &bucketAccum{
			startedAt: ev.StartedAt,
			endedAt:   ev.EndedAt,
			process:   ev.Process,
			user:      ev.User,
			corr:      ev.CorrelationID,
		}
		b.buckets[key] = a
	}
	a.count += max1(ev.OpCount)
	a.bytes += ev.Bytes
	if ev.EndedAt.After(a.endedAt) {
		a.endedAt = ev.EndedAt
	}
	if a.corr == "" && ev.CorrelationID != "" {
		a.corr = ev.CorrelationID
	}
}

// drain returns and removes buckets older than the window.
func (b *bucketAggregator) drain(now time.Time) []FileEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	cutoff := now.Add(-b.window).Truncate(b.window).Unix()
	out := []FileEvent{}
	for k, a := range b.buckets {
		if k.bucket > cutoff {
			continue
		}
		out = append(out, FileEvent{
			Op:           k.op,
			Path:         k.path,
			PID:          k.pid,
			Process:      a.process,
			User:         a.user,
			Bytes:        a.bytes,
			OpCount:      a.count,
			StartedAt:    a.startedAt,
			EndedAt:      a.endedAt,
			CorrelationID: a.corr,
		})
		delete(b.buckets, k)
	}
	return out
}

func max1(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}
