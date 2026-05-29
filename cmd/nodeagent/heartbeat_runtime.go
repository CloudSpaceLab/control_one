package main

import (
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"

	"github.com/CloudSpaceLab/control_one/internal/agentruntime"
	"github.com/CloudSpaceLab/control_one/internal/durablespool"
	"github.com/CloudSpaceLab/control_one/internal/eventstream"
)

type collectorStateReport struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	Backend       string `json:"backend,omitempty"`
	StartedAt     string `json:"started_at,omitempty"`
	BackoffReason string `json:"backoff_reason,omitempty"`
}

type collectorBudgetReport struct {
	Name          string  `json:"name"`
	MaxCPUPercent float64 `json:"max_cpu_percent,omitempty"`
	MaxDurationMS int64   `json:"max_duration_ms,omitempty"`
	BackoffAfter  int     `json:"backoff_after,omitempty"`
}

type agentSelfMetrics struct {
	RSSBytes              uint64 `json:"rss_bytes,omitempty"`
	HeapAllocBytes        uint64 `json:"heap_alloc_bytes,omitempty"`
	HeapSysBytes          uint64 `json:"heap_sys_bytes,omitempty"`
	Goroutines            int    `json:"goroutines"`
	EventStreamDropped    uint64 `json:"event_stream_dropped,omitempty"`
	EventSpoolRecords     int    `json:"event_spool_records,omitempty"`
	EventSpoolBytes       int64  `json:"event_spool_bytes,omitempty"`
	EventSpoolMaxBytes    int64  `json:"event_spool_max_bytes,omitempty"`
	EventSpoolDropped     uint64 `json:"event_spool_dropped,omitempty"`
	LogSpoolRecords       int    `json:"log_spool_records,omitempty"`
	LogSpoolBytes         int64  `json:"log_spool_bytes,omitempty"`
	LogSpoolMaxBytes      int64  `json:"log_spool_max_bytes,omitempty"`
	LogSpoolDropped       uint64 `json:"log_spool_dropped,omitempty"`
	NetflowSummaryBuckets int    `json:"netflow_summary_buckets,omitempty"`
	NetflowSummaryEvicted uint64 `json:"netflow_summary_evicted,omitempty"`
}

type agentSpoolRuntimeStats struct {
	Event durablespool.Stats
	Logs  durablespool.Stats
}

type heartbeatRuntimeState struct {
	mu sync.RWMutex

	settings       agentruntime.Settings
	eventStream    *eventstream.Stream
	collectorState func() []collectorStateReport
	netflowStats   func() (buckets int, evicted uint64)
	spoolStats     func() agentSpoolRuntimeStats
}

var heartbeatRuntime = &heartbeatRuntimeState{
	settings: agentruntime.ResolveForOS(agentruntime.ProfileAuto, runtime.GOOS),
}

func configureHeartbeatRuntime(settings agentruntime.Settings, es *eventstream.Stream, collectorState func() []collectorStateReport, netflowStats func() (int, uint64), spoolStats func() agentSpoolRuntimeStats) {
	heartbeatRuntime.mu.Lock()
	defer heartbeatRuntime.mu.Unlock()
	heartbeatRuntime.settings = settings
	heartbeatRuntime.eventStream = es
	heartbeatRuntime.collectorState = collectorState
	heartbeatRuntime.netflowStats = netflowStats
	heartbeatRuntime.spoolStats = spoolStats
}

func heartbeatRuntimeProfile() string {
	heartbeatRuntime.mu.RLock()
	defer heartbeatRuntime.mu.RUnlock()
	return heartbeatRuntime.settings.ActiveProfile
}

func heartbeatRuntimeCapabilities() []string {
	heartbeatRuntime.mu.RLock()
	defer heartbeatRuntime.mu.RUnlock()
	return append([]string(nil), heartbeatRuntime.settings.Capabilities...)
}

func heartbeatInventoryRefresh() time.Duration {
	heartbeatRuntime.mu.RLock()
	defer heartbeatRuntime.mu.RUnlock()
	return heartbeatRuntime.settings.InventoryRefresh
}

func heartbeatFirewallRefresh() time.Duration {
	heartbeatRuntime.mu.RLock()
	defer heartbeatRuntime.mu.RUnlock()
	return heartbeatRuntime.settings.FirewallRefresh
}

func heartbeatCollectorState() []collectorStateReport {
	heartbeatRuntime.mu.RLock()
	fn := heartbeatRuntime.collectorState
	heartbeatRuntime.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn()
}

func heartbeatCollectorBudgets() []collectorBudgetReport {
	heartbeatRuntime.mu.RLock()
	settings := heartbeatRuntime.settings
	heartbeatRuntime.mu.RUnlock()
	return []collectorBudgetReport{
		{Name: "procmon", MaxCPUPercent: 1.0, MaxDurationMS: settings.ProcmonInterval.Milliseconds(), BackoffAfter: 3},
		{Name: "netflow", MaxCPUPercent: 1.5, MaxDurationMS: settings.NetflowPollInterval.Milliseconds(), BackoffAfter: 3},
		{Name: "fileaccess", MaxCPUPercent: 2.0, MaxDurationMS: 5000, BackoffAfter: 3},
		{Name: "dbquery", MaxCPUPercent: 2.0, MaxDurationMS: 10000, BackoffAfter: 3},
		{Name: "inventory", MaxCPUPercent: 1.0, MaxDurationMS: 30000, BackoffAfter: 2},
	}
}

func collectAgentSelfMetrics() *agentSelfMetrics {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	out := &agentSelfMetrics{
		HeapAllocBytes: ms.HeapAlloc,
		HeapSysBytes:   ms.HeapSys,
		Goroutines:     runtime.NumGoroutine(),
	}

	if p, err := process.NewProcess(int32(os.Getpid())); err == nil {
		if mem, err := p.MemoryInfo(); err == nil && mem != nil {
			out.RSSBytes = mem.RSS
		}
	}

	heartbeatRuntime.mu.RLock()
	if heartbeatRuntime.eventStream != nil {
		out.EventStreamDropped = heartbeatRuntime.eventStream.Dropped()
	}
	statsFn := heartbeatRuntime.netflowStats
	spoolStatsFn := heartbeatRuntime.spoolStats
	heartbeatRuntime.mu.RUnlock()
	if statsFn != nil {
		out.NetflowSummaryBuckets, out.NetflowSummaryEvicted = statsFn()
	}
	if spoolStatsFn != nil {
		stats := spoolStatsFn()
		out.EventSpoolRecords = stats.Event.Records
		out.EventSpoolBytes = stats.Event.Bytes
		out.EventSpoolMaxBytes = stats.Event.MaxBytes
		out.EventSpoolDropped = stats.Event.DroppedRecords
		out.LogSpoolRecords = stats.Logs.Records
		out.LogSpoolBytes = stats.Logs.Bytes
		out.LogSpoolMaxBytes = stats.Logs.MaxBytes
		out.LogSpoolDropped = stats.Logs.DroppedRecords
	}
	return out
}

func firewallStateHash(st FirewallState) string {
	data, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return string(data)
}
