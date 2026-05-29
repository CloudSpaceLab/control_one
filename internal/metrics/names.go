// Package metrics is the single source of truth for telemetry metric names
// shared between the node agent (emitter) and the controlplane predictive
// engine (reader).
//
// Located at internal/metrics rather than controlplane/internal/metrics
// because Go's internal-package rule forbids the agent (rooted at the
// repo root) from importing controlplane/internal/*; a top-level internal/
// package is reachable from both sides.
package metrics

// Core/platform-emitted metric names. The node agent's CollectHostMetrics
// emits a subset of these keys on every tick depending on OS, kernel support,
// and optional probe configuration.
const (
	MetricCPUUsagePercent         = "cpu_usage_percent"
	MetricCPUCount                = "cpu_count"
	MetricMemoryUsedPercent       = "memory_used_percent"
	MetricMemoryTotalBytes        = "memory_total_bytes"
	MetricDiskUsagePercent        = "disk_usage_percent"
	MetricDiskTotalBytes          = "disk_total_bytes"
	MetricDiskUsedBytes           = "disk_used_bytes"
	MetricDiskFreeBytes           = "disk_free_bytes"
	MetricLoad1                   = "load1"
	MetricLoad5                   = "load5"
	MetricLoad15                  = "load15"
	MetricHostIowaitPct           = "host.iowait_pct"
	MetricHostSwapUsedPct         = "host.swap_used_pct"
	MetricHostLoadAvgRatio        = "host.load_avg_ratio"
	MetricHostOOMEventsCount      = "host.oom_events_count"
	MetricHostDiskQueueLength     = "host.disk_queue_length"
	MetricSmartReallocatedSectors = "smart.reallocated_sector_count"
	MetricSmartUncorrectableErrs  = "smart.uncorrectable_errors"
	MetricNetPacketLossPct        = "net.packet_loss_pct"
	MetricNetIcmpLatencyP99       = "net.icmp_latency_p99"
)

// CoreEmitted is the deterministic, ordered list of metric names the node agent
// may emit from its built-in host collector. Some are platform/config dependent
// and can be absent on a given tick.
var CoreEmitted = []string{
	MetricCPUUsagePercent,
	MetricCPUCount,
	MetricMemoryUsedPercent,
	MetricMemoryTotalBytes,
	MetricDiskUsagePercent,
	MetricDiskTotalBytes,
	MetricDiskUsedBytes,
	MetricDiskFreeBytes,
	MetricLoad1,
	MetricLoad5,
	MetricLoad15,
	MetricHostIowaitPct,
	MetricHostSwapUsedPct,
	MetricHostLoadAvgRatio,
	MetricHostOOMEventsCount,
	MetricHostDiskQueueLength,
	MetricSmartReallocatedSectors,
	MetricSmartUncorrectableErrs,
	MetricNetPacketLossPct,
	MetricNetIcmpLatencyP99,
}

// OptionalSignals is retained for predictive scoring compatibility. The
// current optional health metrics now live in CoreEmitted because collectors
// exist for them, though they still emit only when supported/configured.
var OptionalSignals = []string{}

// Units returns the unit suffix for a known metric name, or empty string if no
// unit applies (counts, dimensionless ratios).
func Units(name string) string {
	switch name {
	case MetricCPUUsagePercent, MetricMemoryUsedPercent, MetricDiskUsagePercent,
		MetricHostIowaitPct, MetricHostSwapUsedPct, MetricNetPacketLossPct:
		return "percent"
	case MetricMemoryTotalBytes, MetricDiskTotalBytes, MetricDiskUsedBytes, MetricDiskFreeBytes:
		return "bytes"
	case MetricNetIcmpLatencyP99:
		return "ms"
	case MetricSmartReallocatedSectors, MetricSmartUncorrectableErrs, MetricHostOOMEventsCount,
		MetricHostDiskQueueLength:
		return "count"
	case MetricHostLoadAvgRatio:
		return "ratio"
	default:
		return ""
	}
}
