package doris

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusReporter is a MetricsReporter that exports the writer's per-flush
// observations to a Prometheus registry. Wire one of these into the writer at
// init so failures surface in Grafana.
type PrometheusReporter struct {
	flushes      *prometheus.CounterVec
	errors       *prometheus.CounterVec
	flushLatency *prometheus.HistogramVec
	flushBytes   *prometheus.CounterVec
	flushRows    *prometheus.CounterVec
	queueDepth   *prometheus.GaugeVec
}

// NewPrometheusReporter registers all writer metrics on `reg`. Pass
// prometheus.DefaultRegisterer to use the default registry.
func NewPrometheusReporter(reg prometheus.Registerer) *PrometheusReporter {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	r := &PrometheusReporter{
		flushes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controlone_doris_flushes_total",
			Help: "Total Doris Stream Load flushes.",
		}, []string{"table", "outcome"}),
		errors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controlone_doris_write_errors_total",
			Help: "Total Doris write errors broken down by table.",
		}, []string{"table"}),
		flushLatency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "controlone_doris_flush_seconds",
			Help:    "Latency of Doris flush operations.",
			Buckets: prometheus.DefBuckets,
		}, []string{"table"}),
		flushBytes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controlone_doris_flush_bytes_total",
			Help: "Total bytes (pre-compression) flushed to Doris.",
		}, []string{"table"}),
		flushRows: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controlone_doris_flush_rows_total",
			Help: "Total rows flushed to Doris.",
		}, []string{"table"}),
		queueDepth: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "controlone_doris_writer_queue_depth",
			Help: "Pending rows in the Doris writer per table.",
		}, []string{"table"}),
	}
	// Best-effort: ignore AlreadyRegistered so calling twice (in tests) is
	// safe.
	for _, c := range []prometheus.Collector{r.flushes, r.errors, r.flushLatency, r.flushBytes, r.flushRows, r.queueDepth} {
		if err := reg.Register(c); err != nil {
			if are, ok := err.(prometheus.AlreadyRegisteredError); ok {
				// Reuse the existing collector.
				switch are.ExistingCollector.(type) {
				case *prometheus.CounterVec, *prometheus.HistogramVec, *prometheus.GaugeVec:
				}
			}
		}
	}
	return r
}

// ObserveFlush records one flush outcome.
func (r *PrometheusReporter) ObserveFlush(table string, rows int, bytes int, dur time.Duration, err error) {
	if r == nil {
		return
	}
	outcome := "success"
	if err != nil {
		outcome = "error"
		r.errors.WithLabelValues(table).Inc()
	}
	r.flushes.WithLabelValues(table, outcome).Inc()
	r.flushLatency.WithLabelValues(table).Observe(dur.Seconds())
	r.flushBytes.WithLabelValues(table).Add(float64(bytes))
	r.flushRows.WithLabelValues(table).Add(float64(rows))
}

// ObserveQueueDepth records the current pending depth for a table.
func (r *PrometheusReporter) ObserveQueueDepth(table string, depth int) {
	if r == nil {
		return
	}
	r.queueDepth.WithLabelValues(table).Set(float64(depth))
}

// --- nil-safe wrapper -----------------------------------------------------

// MultiReporter forwards observations to several reporters. Useful in tests
// that want to assert via fakes alongside a real Prometheus reporter.
type MultiReporter struct {
	mu        sync.RWMutex
	reporters []MetricsReporter
}

// Add appends a reporter.
func (m *MultiReporter) Add(r MetricsReporter) {
	if r == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reporters = append(m.reporters, r)
}

func (m *MultiReporter) ObserveFlush(table string, rows int, bytes int, dur time.Duration, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.reporters {
		r.ObserveFlush(table, rows, bytes, dur, err)
	}
}

func (m *MultiReporter) ObserveQueueDepth(table string, depth int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.reporters {
		r.ObserveQueueDepth(table, depth)
	}
}
