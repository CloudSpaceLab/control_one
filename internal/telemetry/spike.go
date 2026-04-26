package telemetry

import (
	"sync"
	"time"
)

// logSpikeDetector flags when a log source's byte rate explodes vs its
// rolling 30-minute baseline. Emits at most once per cooldown window so a
// sustained spike doesn't drown the eventbus.
//
// Algorithm:
//   - 30 one-minute buckets, ring-indexed by (unix_min % 30).
//   - Current rate = newest full minute's bytes.
//   - Baseline = mean of the prior 29 buckets (drops zeros to avoid
//     making early-process spikes look infinite).
//   - Spike = current ≥ ratio * baseline AND baseline ≥ MinBytesPerMin.
type logSpikeDetector struct {
	mu        sync.Mutex
	sources   map[string]*spikeWindow
	ratio     float64       // default 10×
	minBytes  int64         // floor on baseline so noise doesn't fire
	cooldown  time.Duration // suppression window per source
}

type spikeWindow struct {
	buckets    [30]int64
	bucketMin  [30]int64 // unix minute that owns this slot
	lastSpike  time.Time
	lastBucket int64
}

func newLogSpikeDetector() *logSpikeDetector {
	return &logSpikeDetector{
		sources:  map[string]*spikeWindow{},
		ratio:    10.0,
		minBytes: 4096,
		cooldown: 5 * time.Minute,
	}
}

// Record adds n bytes to source's current minute. Returns (true, baseline,
// current) when a fresh spike crossed the threshold; otherwise (false, ...).
func (d *logSpikeDetector) Record(source string, n int64, now time.Time) (bool, int64, int64) {
	if d == nil || n <= 0 {
		return false, 0, 0
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	w, ok := d.sources[source]
	if !ok {
		w = &spikeWindow{}
		d.sources[source] = w
	}

	min := now.Unix() / 60
	idx := min % 30
	if w.bucketMin[idx] != min {
		w.buckets[idx] = 0
		w.bucketMin[idx] = min
	}
	w.buckets[idx] += n

	// Don't evaluate until we cross into a new minute — current minute is
	// still accumulating so its rate reading is misleadingly low.
	if min == w.lastBucket {
		return false, 0, 0
	}
	w.lastBucket = min

	current := w.buckets[(min-1+30)%30] // previous full minute
	var sum int64
	var count int64
	for i := int64(0); i < 29; i++ {
		slot := (min - 2 - i + 30) % 30
		if w.bucketMin[slot] == 0 {
			continue
		}
		sum += w.buckets[slot]
		count++
	}
	if count == 0 || current == 0 {
		return false, 0, current
	}
	baseline := sum / count
	if baseline < d.minBytes {
		return false, baseline, current
	}
	if float64(current) < d.ratio*float64(baseline) {
		return false, baseline, current
	}
	if !w.lastSpike.IsZero() && now.Sub(w.lastSpike) < d.cooldown {
		return false, baseline, current
	}
	w.lastSpike = now
	return true, baseline, current
}
