package worker

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricsBackendMemory = "memory"
	metricsBackendAsynq  = "asynq"

	metricsStatusSuccess = "success"
	metricsStatusFailure = "failure"
	metricsStatusError   = "error"
)

var (
	metricsOnce                 sync.Once
	workerTaskEnqueueTotal      *prometheus.CounterVec
	workerTaskExecutionTotal    *prometheus.CounterVec
	workerTaskExecutionDuration *prometheus.HistogramVec
	workerTaskInflight          *prometheus.GaugeVec
	workerQueueDepth            *prometheus.GaugeVec
	workerBackendUp             *prometheus.GaugeVec
)

func initWorkerMetrics() {
	metricsOnce.Do(func() {
		workerTaskEnqueueTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "controlone",
			Subsystem: "worker",
			Name:      "task_enqueue_total",
			Help:      "Total number of task enqueue attempts segmented by backend and outcome.",
		}, []string{"backend", "outcome"})

		workerTaskExecutionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "controlone",
			Subsystem: "worker",
			Name:      "task_execution_total",
			Help:      "Total number of task executions segmented by backend and outcome.",
		}, []string{"backend", "outcome"})

		workerTaskExecutionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "controlone",
			Subsystem: "worker",
			Name:      "task_execution_duration_seconds",
			Help:      "Duration of task executions in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"backend"})

		workerTaskInflight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "controlone",
			Subsystem: "worker",
			Name:      "tasks_inflight",
			Help:      "Number of tasks currently executing per backend.",
		}, []string{"backend"})

		workerQueueDepth = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "controlone",
			Subsystem: "worker",
			Name:      "queue_depth",
			Help:      "Current queue depth per worker backend.",
		}, []string{"backend"})

		workerBackendUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "controlone",
			Subsystem: "worker",
			Name:      "backend_up",
			Help:      "Indicates whether a worker backend is active (1) or stopped (0).",
		}, []string{"backend"})

		prometheus.MustRegister(
			workerTaskEnqueueTotal,
			workerTaskExecutionTotal,
			workerTaskExecutionDuration,
			workerTaskInflight,
			workerQueueDepth,
			workerBackendUp,
		)
	})
}

func metricsRecordBackendState(backend string, up bool) {
	if backend == "" {
		return
	}
	initWorkerMetrics()
	var value float64
	if up {
		value = 1
	}
	workerBackendUp.WithLabelValues(backend).Set(value)
	if backend == metricsBackendMemory && !up {
		workerQueueDepth.WithLabelValues(backend).Set(0)
	}
}

func metricsRecordEnqueueResult(backend, outcome string) {
	if backend == "" {
		return
	}
	initWorkerMetrics()
	workerTaskEnqueueTotal.WithLabelValues(backend, outcome).Inc()
}

func metricsRecordQueueDepth(backend string, depth int) {
	if backend == "" {
		return
	}
	initWorkerMetrics()
	workerQueueDepth.WithLabelValues(backend).Set(float64(depth))
}

func metricsTrackWorkerTask(backend string) func(string) {
	if backend == "" {
		return func(string) {}
	}
	initWorkerMetrics()
	workerTaskInflight.WithLabelValues(backend).Inc()
	started := time.Now()

	return func(outcome string) {
		workerTaskInflight.WithLabelValues(backend).Dec()
		workerTaskExecutionDuration.WithLabelValues(backend).Observe(time.Since(started).Seconds())
		workerTaskExecutionTotal.WithLabelValues(backend, outcome).Inc()
	}
}
