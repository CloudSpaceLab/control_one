// Package metrics is the SINGLE SOURCE OF TRUTH for telemetry metric names
// shared between the node agent (emitter) and the controlplane predictive
// engine (reader).
//
// Located at internal/metrics rather than controlplane/internal/metrics
// because Go's internal-package rule forbids the agent (rooted at the
// repo root) from importing controlplane/internal/* — a top-level
// internal/ package is reachable from both sides.
//
// # Contract
//
// The agent emits exactly the metric names declared in CoreEmitted below.
// The predictive engine, the units map, and any other consumer of
// telemetry_metrics MUST import these constants rather than hard-coding
// strings. The TestMetricNamesContract assertion in this package's test file
// pins the agent + server lists together — drift fires the test.
//
// If a metric name needs to change or be added, update this file in the same
// commit that updates the producer. Do not introduce string literals in
// either internal/util/sysinfo.go or controlplane/internal/server/*.go for
// metric names — import from here instead.
//
// # Canonical list choice (2026-05-10, dispatch d-2026-05-10-010)
//
// PR #51 audit (docs/incomplete-features-and-bugs.md §1.1) found a 9-vs-7
// mismatch: the agent emitted 9 names (cpu_usage_percent, cpu_count,
// memory_*, disk_*, load1/5/15) and the predictive engine read 8 disjoint
// names (smart.*, host.iowait_pct/swap_used_pct/load_avg_ratio/oom_events_count,
// net.packet_loss_pct, net.icmp_latency_p99). The intersection was EMPTY,
// so calibration's min(samples) gate evaluated to 0 forever.
//
// Resolution: the currently-emitted host metric names are CANONICAL because
// they are what production nodes actually produce today. The 8 aspirational signals
// from healthSignalsCatalog are demoted to OptionalSignals — names that
// MAY arrive once the corresponding collectors ship (Linux PSI, smartctl,
// ICMP probe, /proc/vmstat oom_kill). The predictive engine treats Optional
// signals as "no penalty when absent, no calibration vote when absent" so
// rolling out collectors one at a time does not pin nodes in calibrating.
//
// Calibration is therefore gated on the intersection of CoreEmitted and the
// signals the predictive engine elects to consider — currently load1, which
// is sufficient to drive scoring while the optional signals come online.
package metrics

// Core emitted metric names. The node agent's CollectHostMetrics emits
// EXACTLY these keys. Adding a key here without updating the emitter (or
// vice versa) will trip TestMetricNamesContract.
const (
	MetricCPUUsagePercent   = "cpu_usage_percent"
	MetricCPUCount          = "cpu_count"
	MetricMemoryUsedPercent = "memory_used_percent"
	MetricMemoryTotalBytes  = "memory_total_bytes"
	MetricDiskUsagePercent  = "disk_usage_percent"
	MetricDiskTotalBytes    = "disk_total_bytes"
	MetricDiskUsedBytes     = "disk_used_bytes"
	MetricDiskFreeBytes     = "disk_free_bytes"
	MetricLoad1             = "load1"
	MetricLoad5             = "load5"
	MetricLoad15            = "load15"
)

// CoreEmitted is the deterministic, ordered list of metric names the node
// agent emits on every tick. Both the agent emitter and the server
// units/predictive code import this list.
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
}

// Optional / aspirational signals the predictive engine considers when
// available. These are NOT yet emitted by the agent; collectors are tracked
// in docs/incomplete-features-and-bugs.md §1.1 fix sketch (PSI iowait,
// swap, oom, load ratio, packet loss, ICMP latency, SMART). The predictive
// engine treats absence as "no penalty, no calibration vote" — see
// node_predictive.go scorePredictForTenant.
const (
	MetricHostIowaitPct           = "host.iowait_pct"
	MetricHostSwapUsedPct         = "host.swap_used_pct"
	MetricHostLoadAvgRatio        = "host.load_avg_ratio"
	MetricHostOOMEventsCount      = "host.oom_events_count"
	MetricSmartReallocatedSectors = "smart.reallocated_sector_count"
	MetricSmartUncorrectableErrs  = "smart.uncorrectable_errors"
	MetricNetPacketLossPct        = "net.packet_loss_pct"
	MetricNetIcmpLatencyP99       = "net.icmp_latency_p99"
)

// OptionalSignals enumerates the aspirational signals. The predictive
// engine iterates this list AND CoreEmitted; missing rows do not gate
// calibration.
var OptionalSignals = []string{
	MetricHostIowaitPct,
	MetricHostSwapUsedPct,
	MetricHostLoadAvgRatio,
	MetricHostOOMEventsCount,
	MetricSmartReallocatedSectors,
	MetricSmartUncorrectableErrs,
	MetricNetPacketLossPct,
	MetricNetIcmpLatencyP99,
}

// Units returns the unit suffix for a known metric name, or empty string
// if no unit applies (counts, dimensionless ratios). The server's
// metricUnits map should be derived from this.
func Units(name string) string {
	switch name {
	case MetricCPUUsagePercent, MetricMemoryUsedPercent, MetricDiskUsagePercent,
		MetricHostIowaitPct, MetricHostSwapUsedPct, MetricNetPacketLossPct:
		return "percent"
	case MetricMemoryTotalBytes, MetricDiskTotalBytes, MetricDiskUsedBytes, MetricDiskFreeBytes:
		return "bytes"
	case MetricNetIcmpLatencyP99:
		return "ms"
	case MetricSmartReallocatedSectors, MetricSmartUncorrectableErrs, MetricHostOOMEventsCount:
		return "count"
	case MetricHostLoadAvgRatio:
		return "ratio"
	default:
		return ""
	}
}
