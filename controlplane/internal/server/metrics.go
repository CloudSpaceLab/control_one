package server

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	metricsStatusSuccess = "success"
	metricsStatusFailure = "failure"
	metricsStatusError   = "error"
)

var (
	metricsOnce             sync.Once
	jobEnqueuedTotal        *prometheus.CounterVec
	jobExecutionTotal       *prometheus.CounterVec
	jobExecutionDuration    *prometheus.HistogramVec
	jobExecutionConcurrency *prometheus.GaugeVec
)

func initServerMetrics() {
	metricsOnce.Do(func() {
		jobEnqueuedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "controlone",
			Subsystem: "jobs",
			Name:      "enqueued_total",
			Help:      "Total number of jobs successfully enqueued by type.",
		}, []string{"job_type"})

		jobExecutionTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "controlone",
			Subsystem: "jobs",
			Name:      "execution_total",
			Help:      "Total number of job executions by type and outcome.",
		}, []string{"job_type", "outcome"})

		jobExecutionDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "controlone",
			Subsystem: "jobs",
			Name:      "execution_duration_seconds",
			Help:      "Job execution duration in seconds.",
			Buckets:   prometheus.DefBuckets,
		}, []string{"job_type"})

		jobExecutionConcurrency = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "controlone",
			Subsystem: "jobs",
			Name:      "inflight",
			Help:      "Number of jobs currently executing by type.",
		}, []string{"job_type"})

		prometheus.MustRegister(
			jobEnqueuedTotal,
			jobExecutionTotal,
			jobExecutionDuration,
			jobExecutionConcurrency,
		)
	})
}

func metricsRecordJobQueued(jobType string) {
	initServerMetrics()
	jobEnqueuedTotal.WithLabelValues(jobType).Inc()
}

func metricsTrackJob(jobType string) func(outcome string) {
	initServerMetrics()
	jobExecutionConcurrency.WithLabelValues(jobType).Inc()
	started := time.Now()

	return func(outcome string) {
		jobExecutionConcurrency.WithLabelValues(jobType).Dec()
		jobExecutionDuration.WithLabelValues(jobType).Observe(time.Since(started).Seconds())
		jobExecutionTotal.WithLabelValues(jobType, outcome).Inc()
	}
}
