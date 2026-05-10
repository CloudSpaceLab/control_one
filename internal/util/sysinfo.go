package util

import (
	"context"
	"net"
	"os"
	"runtime"
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cpuPercent, _ := cpu.PercentWithContext(ctx, 0, false)
	cpuCounts, _ := cpu.CountsWithContext(ctx, true) // logical CPUs
	memStat, _ := mem.VirtualMemoryWithContext(ctx)
	diskStat, _ := disk.UsageWithContext(ctx, "/")
	loadStat, _ := load.AvgWithContext(ctx)

	// Metric names imported from controlplane/internal/metrics — single
	// source of truth shared with the predictive engine.
	out := map[string]any{}
	if len(cpuPercent) > 0 {
		out[metrics.MetricCPUUsagePercent] = cpuPercent[0]
	}
	if cpuCounts > 0 {
		out[metrics.MetricCPUCount] = float64(cpuCounts)
	}
	if memStat != nil {
		out[metrics.MetricMemoryTotalBytes] = memStat.Total
		out[metrics.MetricMemoryUsedPercent] = memStat.UsedPercent
	}
	if diskStat != nil {
		out[metrics.MetricDiskUsagePercent] = diskStat.UsedPercent
		out[metrics.MetricDiskTotalBytes] = diskStat.Total
	}
	if loadStat != nil {
		out[metrics.MetricLoad1] = loadStat.Load1
		out[metrics.MetricLoad5] = loadStat.Load5
		out[metrics.MetricLoad15] = loadStat.Load15
	}

	return out
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
