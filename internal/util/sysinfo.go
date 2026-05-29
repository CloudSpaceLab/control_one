package util

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"

	"github.com/CloudSpaceLab/control_one/internal/metrics"
)

// SystemInfo represents basic machine metadata collected at bootstrap.
type SystemInfo struct {
	Hostname    string
	OS          string
	Arch        string
	PublicIP    string
	Fingerprint string
}

var hostMetricState struct {
	mu                                 sync.Mutex
	cpuTimes                           *cpu.TimesStat
	oomKills                           *uint64
	windowsResourceExhaustionCheckedAt *time.Time
}

var readProcVMStat = func() ([]byte, error) {
	return os.ReadFile("/proc/vmstat")
}

var runHealthCommand = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type HostMetricOptions struct {
	HealthProbeTargets []string
	HealthProbeCount   int
	HealthProbeTimeout time.Duration
	DiskHealthDevices  []string
}

type HostMetricSample struct {
	Name   string
	Value  any
	Labels map[string]string
}

type HostMetricSnapshot struct {
	Metrics map[string]any
	Samples []HostMetricSample
}

// GatherSystemInfo collects basic host information for registration payloads.
func GatherSystemInfo() SystemInfo {
	hostname, _ := os.Hostname()
	hostInfo, _ := host.Info()

	return SystemInfo{
		Hostname:    hostname,
		OS:          hostInfo.Platform + " " + hostInfo.PlatformVersion,
		Arch:        runtime.GOARCH,
		PublicIP:    firstNonLoopbackIP(),
		Fingerprint: uuid.NewString(),
	}
}

// ReadMachineID returns a stable, OS-derived identifier for the current host.
// It is used by the enrollment flow to detect re-runs of the installer on the
// same machine and avoid creating duplicate node rows. The exact source is OS
// specific — see sysinfo_linux.go, sysinfo_darwin.go, sysinfo_windows.go.
//
// An empty string with no error is acceptable and means "unknown"; callers
// should fall back to hostname-based identification in that case.
func ReadMachineID() (string, error) {
	return readMachineID()
}

// CollectHostMetrics gathers lightweight host metrics for telemetry.
func CollectHostMetrics() map[string]any {
	return CollectHostMetricsWithOptions(HostMetricOptions{})
}

// CollectHostMetricsWithOptions gathers host metrics plus optional network and
// disk-health probes configured by the node agent.
func CollectHostMetricsWithOptions(opts HostMetricOptions) map[string]any {
	return CollectHostMetricSnapshotWithOptions(opts).Metrics
}

func CollectHostMetricSnapshot() HostMetricSnapshot {
	return CollectHostMetricSnapshotWithOptions(HostMetricOptions{})
}

// CollectHostMetricSnapshotWithOptions gathers host metrics plus optional
// labelled samples such as per-device SMART/NVMe health probes.
func CollectHostMetricSnapshotWithOptions(opts HostMetricOptions) HostMetricSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cpuPercent, _ := cpu.PercentWithContext(ctx, 0, false)
	cpuCounts, _ := cpu.CountsWithContext(ctx, true) // logical CPUs
	memStat, _ := mem.VirtualMemoryWithContext(ctx)
	swapStat, _ := mem.SwapMemoryWithContext(ctx)
	diskStat, _ := disk.UsageWithContext(ctx, "/")
	loadStat, _ := load.AvgWithContext(ctx)

	// Metric names imported from controlplane/internal/metrics — single
	// source of truth shared with the predictive engine.
	out := map[string]any{}
	var samples []HostMetricSample
	if len(cpuPercent) > 0 {
		out[metrics.MetricCPUUsagePercent] = cpuPercent[0]
	}
	if runtime.GOOS == "linux" {
		if iowait, ok := collectLinuxIOWaitPercent(ctx); ok {
			out[metrics.MetricHostIowaitPct] = iowait
		}
		if oomDelta, ok := collectLinuxOOMEventsDelta(); ok {
			out[metrics.MetricHostOOMEventsCount] = oomDelta
		}
	}
	if cpuCounts > 0 {
		out[metrics.MetricCPUCount] = float64(cpuCounts)
	}
	if memStat != nil {
		out[metrics.MetricMemoryTotalBytes] = memStat.Total
		out[metrics.MetricMemoryUsedPercent] = memStat.UsedPercent
	}
	if swapStat != nil {
		out[metrics.MetricHostSwapUsedPct] = swapStat.UsedPercent
	}
	if runtime.GOOS == "windows" {
		windowsMetrics, windowsSamples := collectWindowsHostHealthMetricSnapshot(ctx, time.Now().UTC())
		for key, value := range windowsMetrics {
			out[key] = value
		}
		samples = append(samples, windowsSamples...)
	}
	if diskStat != nil {
		out[metrics.MetricDiskUsagePercent] = diskStat.UsedPercent
		out[metrics.MetricDiskTotalBytes] = diskStat.Total
		out[metrics.MetricDiskUsedBytes] = diskStat.Used
		out[metrics.MetricDiskFreeBytes] = diskStat.Free
	}
	if loadStat != nil {
		out[metrics.MetricLoad1] = loadStat.Load1
		out[metrics.MetricLoad5] = loadStat.Load5
		out[metrics.MetricLoad15] = loadStat.Load15
		if cpuCounts > 0 {
			out[metrics.MetricHostLoadAvgRatio] = loadStat.Load1 / float64(cpuCounts)
		}
	}
	for key, value := range collectNetworkProbeMetrics(opts.HealthProbeTargets, opts.HealthProbeCount, opts.HealthProbeTimeout) {
		out[key] = value
	}
	diskHealthMetrics, diskHealthSamples := collectDiskHealthMetricSnapshot(opts.DiskHealthDevices)
	for key, value := range diskHealthMetrics {
		out[key] = value
	}
	samples = append(samples, diskHealthSamples...)

	return HostMetricSnapshot{Metrics: out, Samples: samples}
}

func collectLinuxIOWaitPercent(ctx context.Context) (float64, bool) {
	times, err := cpu.TimesWithContext(ctx, false)
	if err != nil || len(times) == 0 {
		return 0, false
	}
	current := times[0]
	hostMetricState.mu.Lock()
	previous := hostMetricState.cpuTimes
	hostMetricState.cpuTimes = &current
	hostMetricState.mu.Unlock()
	if previous == nil {
		return 0, false
	}
	totalDelta := cpuTimesTotal(current) - cpuTimesTotal(*previous)
	if totalDelta <= 0 {
		return 0, false
	}
	iowaitDelta := current.Iowait - previous.Iowait
	if iowaitDelta < 0 {
		return 0, false
	}
	return (iowaitDelta / totalDelta) * 100, true
}

func cpuTimesTotal(t cpu.TimesStat) float64 {
	return t.User + t.System + t.Idle + t.Nice + t.Iowait + t.Irq + t.Softirq + t.Steal + t.Guest + t.GuestNice
}

func collectLinuxOOMEventsDelta() (float64, bool) {
	raw, err := readProcVMStat()
	if err != nil {
		return 0, false
	}
	current, ok := parseProcVMStatCounter(raw, "oom_kill")
	if !ok {
		return 0, false
	}

	hostMetricState.mu.Lock()
	previous := hostMetricState.oomKills
	snapshot := current
	hostMetricState.oomKills = &snapshot
	hostMetricState.mu.Unlock()

	if previous == nil {
		return 0, false
	}
	if current < *previous {
		return 0, true
	}
	return float64(current - *previous), true
}

func parseProcVMStatCounter(raw []byte, key string) (uint64, bool) {
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != key {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		return value, err == nil
	}
	return 0, false
}

func windowsHostHealthPowerShell(start time.Time) string {
	return fmt.Sprintf(`
$ErrorActionPreference = 'SilentlyContinue'
$result = [ordered]@{}
function Add-CounterValue([string]$key, [string]$path) {
  try {
    $sample = (Get-Counter -Counter $path -ErrorAction Stop).CounterSamples | Select-Object -First 1
    if ($null -ne $sample) {
      $result[$key] = [double]$sample.CookedValue
    }
  } catch {}
}
Add-CounterValue 'pagefile_usage_pct' '\Paging File(_Total)\%% Usage'
Add-CounterValue 'disk_queue_length' '\PhysicalDisk(_Total)\Current Disk Queue Length'
try {
  $start = [datetime]::Parse('%s', [Globalization.CultureInfo]::InvariantCulture, [Globalization.DateTimeStyles]::AssumeUniversal).ToLocalTime()
  $events = Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Microsoft-Windows-Resource-Exhaustion-Detector'; StartTime=$start} -ErrorAction SilentlyContinue
  $result['resource_exhaustion_events'] = @($events).Count
} catch {
  $result['resource_exhaustion_events'] = 0
}
$result | ConvertTo-Json -Compress
`, start.UTC().Format(time.RFC3339Nano))
}

func collectWindowsHostHealthMetricSnapshot(ctx context.Context, now time.Time) (map[string]any, []HostMetricSample) {
	start := nextWindowsResourceExhaustionStart(now)
	args := []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", windowsHostHealthPowerShell(start)}
	var raw []byte
	for _, command := range []string{"powershell.exe", "powershell", "pwsh"} {
		out, err := runHealthCommand(ctx, command, args...)
		if err == nil || len(out) > 0 {
			raw = out
			break
		}
	}
	values, ok := parseWindowsHostHealthJSON(raw)
	if !ok {
		return nil, nil
	}
	out := map[string]any{}
	var samples []HostMetricSample
	if value, ok := values["pagefile_usage_pct"]; ok {
		value = clampPercent(value)
		out[metrics.MetricHostSwapUsedPct] = value
		samples = append(samples, HostMetricSample{
			Name:  metrics.MetricHostSwapUsedPct,
			Value: value,
			Labels: map[string]string{
				"collector": "windows_perfcounter",
				"counter":   `\Paging File(_Total)\% Usage`,
			},
		})
	}
	if value, ok := values["resource_exhaustion_events"]; ok {
		out[metrics.MetricHostOOMEventsCount] = value
		samples = append(samples, HostMetricSample{
			Name:  metrics.MetricHostOOMEventsCount,
			Value: value,
			Labels: map[string]string{
				"collector": "windows_eventlog",
				"log_name":  "System",
				"provider":  "Microsoft-Windows-Resource-Exhaustion-Detector",
			},
		})
	}
	if value, ok := values["disk_queue_length"]; ok {
		out[metrics.MetricHostDiskQueueLength] = value
		samples = append(samples, HostMetricSample{
			Name:  metrics.MetricHostDiskQueueLength,
			Value: value,
			Labels: map[string]string{
				"collector": "windows_perfcounter",
				"counter":   `\PhysicalDisk(_Total)\Current Disk Queue Length`,
			},
		})
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, samples
}

func nextWindowsResourceExhaustionStart(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	hostMetricState.mu.Lock()
	previous := hostMetricState.windowsResourceExhaustionCheckedAt
	snapshot := now.UTC()
	hostMetricState.windowsResourceExhaustionCheckedAt = &snapshot
	hostMetricState.mu.Unlock()
	if previous == nil {
		return snapshot.Add(-1 * time.Hour)
	}
	return previous.UTC()
}

func parseWindowsHostHealthJSON(raw []byte) (map[string]float64, bool) {
	text := strings.TrimSpace(string(raw))
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, false
	}
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(text[start : end+1]))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return nil, false
	}
	out := map[string]float64{}
	for _, key := range []string{"pagefile_usage_pct", "resource_exhaustion_events", "disk_queue_length"} {
		value, ok := numericValue(payload[key])
		if !ok || !validHealthMetricValue(value) {
			continue
		}
		out[key] = value
	}
	return out, len(out) > 0
}

func validHealthMetricValue(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func clampPercent(value float64) float64 {
	switch {
	case value < 0:
		return 0
	case value > 100:
		return 100
	default:
		return value
	}
}

func collectNetworkProbeMetrics(targets []string, count int, timeout time.Duration) map[string]any {
	if len(targets) == 0 {
		return nil
	}
	if count <= 0 {
		count = 4
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	var (
		seen         bool
		worstLoss    float64
		worstLatency float64
	)
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target == "" || strings.HasPrefix(target, "-") {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout+time.Second)
		raw, _ := runHealthCommand(ctx, "ping", pingArgs(target, count, timeout)...)
		cancel()
		loss, latency, ok := parsePingOutput(raw)
		if !ok {
			continue
		}
		seen = true
		if loss > worstLoss {
			worstLoss = loss
		}
		if latency > worstLatency {
			worstLatency = latency
		}
	}
	if !seen {
		return nil
	}
	return map[string]any{
		metrics.MetricNetPacketLossPct:  worstLoss,
		metrics.MetricNetIcmpLatencyP99: worstLatency,
	}
}

func pingArgs(target string, count int, timeout time.Duration) []string {
	if runtime.GOOS == "windows" {
		return []string{"-n", strconv.Itoa(count), "-w", strconv.Itoa(int(timeout.Milliseconds())), target}
	}
	waitSeconds := int(timeout.Seconds())
	if waitSeconds <= 0 {
		waitSeconds = 1
	}
	return []string{"-c", strconv.Itoa(count), "-W", strconv.Itoa(waitSeconds), target}
}

var (
	packetLossPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)([0-9]+(?:\.[0-9]+)?)%\s*packet loss`),
		regexp.MustCompile(`(?i)\(([0-9]+(?:\.[0-9]+)?)%\s*loss\)`),
	}
	latencyPattern = regexp.MustCompile(`(?i)time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)
)

func parsePingOutput(raw []byte) (float64, float64, bool) {
	text := string(raw)
	loss, lossOK := parsePacketLoss(text)
	var maxLatency float64
	var latencyOK bool
	for _, match := range latencyPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			continue
		}
		latencyOK = true
		if value > maxLatency {
			maxLatency = value
		}
	}
	return loss, maxLatency, lossOK || latencyOK
}

func parsePacketLoss(text string) (float64, bool) {
	for _, pattern := range packetLossPatterns {
		match := pattern.FindStringSubmatch(text)
		if len(match) < 2 {
			continue
		}
		value, err := strconv.ParseFloat(match[1], 64)
		return value, err == nil
	}
	return 0, false
}

func collectDiskHealthMetrics(devices []string) map[string]any {
	out, _ := collectDiskHealthMetricSnapshot(devices)
	return out
}

func collectDiskHealthMetricSnapshot(devices []string) (map[string]any, []HostMetricSample) {
	if len(devices) == 0 {
		return nil, nil
	}
	var (
		seenReallocated bool
		seenUncorrected bool
		reallocated     float64
		uncorrected     float64
		samples         []HostMetricSample
	)
	for _, device := range devices {
		device = strings.TrimSpace(device)
		if device == "" || strings.HasPrefix(device, "-") {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		raw, _ := runHealthCommand(ctx, "smartctl", "-j", "-A", device)
		cancel()
		diskMetrics, ok := parseSmartctlJSON(raw)
		if !ok {
			continue
		}
		labels := smartctlSampleLabels(raw, device)
		for _, name := range []string{metrics.MetricSmartReallocatedSectors, metrics.MetricSmartUncorrectableErrs} {
			value, ok := diskMetrics[name]
			if !ok {
				continue
			}
			samples = append(samples, HostMetricSample{
				Name:   name,
				Value:  value,
				Labels: labels,
			})
		}
		if value, ok := diskMetrics[metrics.MetricSmartReallocatedSectors]; ok {
			seenReallocated = true
			reallocated += value
		}
		if value, ok := diskMetrics[metrics.MetricSmartUncorrectableErrs]; ok {
			seenUncorrected = true
			uncorrected += value
		}
	}
	out := map[string]any{}
	if seenReallocated {
		out[metrics.MetricSmartReallocatedSectors] = reallocated
	}
	if seenUncorrected {
		out[metrics.MetricSmartUncorrectableErrs] = uncorrected
	}
	if len(out) == 0 && len(samples) == 0 {
		return nil, nil
	}
	return out, samples
}

func smartctlSampleLabels(raw []byte, fallbackDevice string) map[string]string {
	labels := map[string]string{
		"collector": "smartctl",
		"scope":     "device",
		"device":    strings.TrimSpace(fallbackDevice),
	}
	if !json.Valid(raw) {
		return labels
	}
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return labels
	}
	if device, ok := payload["device"].(map[string]any); ok {
		setSmartctlLabel(labels, "device", fmtAny(device["name"]))
		setSmartctlLabel(labels, "device_type", fmtAny(device["type"]))
		setSmartctlLabel(labels, "device_protocol", fmtAny(device["protocol"]))
	}
	for _, key := range []string{"model_name", "model_family", "serial_number", "firmware_version"} {
		setSmartctlLabel(labels, key, fmtAny(payload[key]))
	}
	if status, ok := payload["smart_status"].(map[string]any); ok {
		setSmartctlLabel(labels, "smart_passed", fmtAny(status["passed"]))
	}
	return labels
}

func setSmartctlLabel(labels map[string]string, key, value string) {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	labels[key] = value
}

func parseSmartctlJSON(raw []byte) (map[string]float64, bool) {
	if !json.Valid(raw) {
		return nil, false
	}
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return nil, false
	}
	out := map[string]float64{}
	if table, ok := nestedList(payload, "ata_smart_attributes", "table"); ok {
		for _, item := range table {
			attr, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := strings.ToLower(strings.TrimSpace(fmtAny(attr["name"])))
			rawValue, ok := nestedNumber(attr, "raw", "value")
			if !ok {
				continue
			}
			switch {
			case strings.Contains(name, "reallocated"):
				out[metrics.MetricSmartReallocatedSectors] += rawValue
			case strings.Contains(name, "uncorrectable") || strings.Contains(name, "uncorrect"):
				out[metrics.MetricSmartUncorrectableErrs] += rawValue
			}
		}
	}
	if nvme, ok := payload["nvme_smart_health_information_log"].(map[string]any); ok {
		if value, ok := numericValue(nvme["media_errors"]); ok {
			out[metrics.MetricSmartUncorrectableErrs] += value
		}
	}
	return out, len(out) > 0
}

func nestedList(root map[string]any, first, second string) ([]any, bool) {
	child, ok := root[first].(map[string]any)
	if !ok {
		return nil, false
	}
	list, ok := child[second].([]any)
	return list, ok
}

func nestedNumber(root map[string]any, first, second string) (float64, bool) {
	child, ok := root[first].(map[string]any)
	if !ok {
		return 0, false
	}
	return numericValue(child[second])
}

func numericValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		f, err := typed.Float64()
		return f, err == nil
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func fmtAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonLoopbackIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			return ip.String()
		}
	}
	return ""
}
