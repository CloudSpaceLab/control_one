package util

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/metrics"
)

// TestReadMachineIDShape verifies ReadMachineID is callable and returns either
// an empty string (unsupported/unavailable) or a non-empty identifier without
// panicking. We don't assert a specific value because the result is OS- and
// host-specific; CI runners may have different machine-ids.
func TestReadMachineIDShape(t *testing.T) {
	id, err := ReadMachineID()
	if err != nil {
		t.Fatalf("ReadMachineID returned error: %v", err)
	}

	// The ID is either empty (unsupported OS or missing source file) or
	// a reasonably-sized identifier. Empty is valid — callers fall back to
	// hostname-based dedup in that case.
	if id != "" && len(id) < 8 {
		t.Fatalf("suspiciously short machine id %q on %s", id, runtime.GOOS)
	}
}

// TestReadMachineIDStable verifies multiple consecutive calls return the same
// identifier. The function is expected to be pure with respect to machine
// state across the test run.
func TestReadMachineIDStable(t *testing.T) {
	first, err := ReadMachineID()
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, err := ReadMachineID()
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if first != second {
		t.Fatalf("machine id not stable: %q vs %q", first, second)
	}
}

func TestParseProcVMStatCounter(t *testing.T) {
	raw := []byte("pgpgin 12\noom_kill 7\npgpgout 42\n")
	value, ok := parseProcVMStatCounter(raw, "oom_kill")
	if !ok {
		t.Fatal("expected oom_kill counter to parse")
	}
	if value != 7 {
		t.Fatalf("unexpected oom_kill value: got %d, want 7", value)
	}

	if _, ok := parseProcVMStatCounter(raw, "does_not_exist"); ok {
		t.Fatal("unexpected counter parse for missing key")
	}
}

func TestCollectLinuxOOMEventsDelta(t *testing.T) {
	oldRead := readProcVMStat
	defer func() {
		readProcVMStat = oldRead
		resetOOMKillStateForTest(t)
	}()
	resetOOMKillStateForTest(t)

	samples := [][]byte{
		[]byte("oom_kill 3\n"),
		[]byte("oom_kill 5\n"),
		[]byte("oom_kill 5\n"),
		[]byte("oom_kill 1\n"),
	}
	index := 0
	readProcVMStat = func() ([]byte, error) {
		if index >= len(samples) {
			return samples[len(samples)-1], nil
		}
		raw := samples[index]
		index++
		return raw, nil
	}

	if value, ok := collectLinuxOOMEventsDelta(); ok {
		t.Fatalf("first sample should seed state without emitting, got value=%v", value)
	}
	if value, ok := collectLinuxOOMEventsDelta(); !ok || value != 2 {
		t.Fatalf("second sample delta mismatch: got value=%v ok=%v, want value=2 ok=true", value, ok)
	}
	if value, ok := collectLinuxOOMEventsDelta(); !ok || value != 0 {
		t.Fatalf("unchanged counter mismatch: got value=%v ok=%v, want value=0 ok=true", value, ok)
	}
	if value, ok := collectLinuxOOMEventsDelta(); !ok || value != 0 {
		t.Fatalf("counter reset mismatch: got value=%v ok=%v, want value=0 ok=true", value, ok)
	}
}

func TestParseWindowsHostHealthJSON(t *testing.T) {
	raw := []byte("notice\r\n{\"pagefile_usage_pct\":12.5,\"resource_exhaustion_events\":2,\"disk_queue_length\":3.25}")
	got, ok := parseWindowsHostHealthJSON(raw)
	if !ok {
		t.Fatal("expected Windows health JSON to parse")
	}
	if got["pagefile_usage_pct"] != 12.5 || got["resource_exhaustion_events"] != 2 || got["disk_queue_length"] != 3.25 {
		t.Fatalf("windows health values = %#v", got)
	}

	got, ok = parseWindowsHostHealthJSON([]byte(`{"pagefile_usage_pct":101,"resource_exhaustion_events":-1,"disk_queue_length":"nan"}`))
	if !ok || got["pagefile_usage_pct"] != 101 {
		t.Fatalf("expected valid non-negative counters only, got %#v ok=%v", got, ok)
	}
	if _, ok := got["resource_exhaustion_events"]; ok {
		t.Fatalf("negative event count should be ignored: %#v", got)
	}
}

func TestCollectWindowsHostHealthMetricSnapshot(t *testing.T) {
	oldRun := runHealthCommand
	defer func() {
		runHealthCommand = oldRun
		resetWindowsHostHealthStateForTest(t)
	}()
	resetWindowsHostHealthStateForTest(t)
	runHealthCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "powershell.exe" || len(args) == 0 {
			t.Fatalf("unexpected command %s %#v", name, args)
		}
		return []byte(`{"pagefile_usage_pct":87.5,"resource_exhaustion_events":1,"disk_queue_length":4}`), nil
	}

	got, samples := collectWindowsHostHealthMetricSnapshot(context.Background(), time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC))
	if got[metrics.MetricHostSwapUsedPct] != 87.5 ||
		got[metrics.MetricHostOOMEventsCount] != float64(1) ||
		got[metrics.MetricHostDiskQueueLength] != float64(4) {
		t.Fatalf("windows host metrics = %#v", got)
	}
	if len(samples) != 3 {
		t.Fatalf("windows host samples = %#v", samples)
	}
	for _, sample := range samples {
		if sample.Labels["collector"] == "" {
			t.Fatalf("windows sample missing collector label: %#v", sample)
		}
	}
}

func TestParsePingOutput(t *testing.T) {
	loss, latency, ok := parsePingOutput([]byte(`4 packets transmitted, 3 received, 25% packet loss, time 3004ms
64 bytes from 1.1.1.1: icmp_seq=1 ttl=58 time=10.4 ms
64 bytes from 1.1.1.1: icmp_seq=2 ttl=58 time=16.8 ms
64 bytes from 1.1.1.1: icmp_seq=3 ttl=58 time=12.1 ms`))
	if !ok || loss != 25 || latency != 16.8 {
		t.Fatalf("linux ping parse = loss %v latency %v ok %v", loss, latency, ok)
	}

	loss, latency, ok = parsePingOutput([]byte(`Packets: Sent = 4, Received = 4, Lost = 0 (0% loss),
Approximate round trip times in milli-seconds:
Reply from 1.1.1.1: bytes=32 time=9ms TTL=57
Reply from 1.1.1.1: bytes=32 time=11ms TTL=57`))
	if !ok || loss != 0 || latency != 11 {
		t.Fatalf("windows ping parse = loss %v latency %v ok %v", loss, latency, ok)
	}
}

func TestCollectNetworkProbeMetrics(t *testing.T) {
	oldRun := runHealthCommand
	defer func() { runHealthCommand = oldRun }()
	runHealthCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "ping" || len(args) == 0 {
			t.Fatalf("unexpected command %s %#v", name, args)
		}
		return []byte("4 packets transmitted, 4 received, 0% packet loss\n64 bytes: time=7.2 ms\n64 bytes: time=9.1 ms\n"), nil
	}
	got := collectNetworkProbeMetrics([]string{"1.1.1.1"}, 2, time.Second)
	if got[metrics.MetricNetPacketLossPct] != float64(0) || got[metrics.MetricNetIcmpLatencyP99] != 9.1 {
		t.Fatalf("network metrics = %#v", got)
	}
}

func TestParseSmartctlJSON(t *testing.T) {
	raw := []byte(`{
		"ata_smart_attributes": {
			"table": [
				{"name":"Reallocated_Sector_Ct","raw":{"value":2}},
				{"name":"Offline_Uncorrectable","raw":{"value":1}}
			]
		}
	}`)
	got, ok := parseSmartctlJSON(raw)
	if !ok || got[metrics.MetricSmartReallocatedSectors] != 2 || got[metrics.MetricSmartUncorrectableErrs] != 1 {
		t.Fatalf("ata smart parse = %#v ok=%v", got, ok)
	}

	raw = []byte(`{"nvme_smart_health_information_log":{"media_errors":3}}`)
	got, ok = parseSmartctlJSON(raw)
	if !ok || got[metrics.MetricSmartUncorrectableErrs] != 3 {
		t.Fatalf("nvme smart parse = %#v ok=%v", got, ok)
	}
}

func TestCollectDiskHealthMetrics(t *testing.T) {
	oldRun := runHealthCommand
	defer func() { runHealthCommand = oldRun }()
	runHealthCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "smartctl" || len(args) != 3 || args[2] != "/dev/sda" {
			t.Fatalf("unexpected command %s %#v", name, args)
		}
		return []byte(`{"ata_smart_attributes":{"table":[{"name":"Reallocated_Sector_Ct","raw":{"value":4}},{"name":"Reported_Uncorrect","raw":{"value":2}}]}}`), nil
	}
	got := collectDiskHealthMetrics([]string{"/dev/sda"})
	if got[metrics.MetricSmartReallocatedSectors] != float64(4) || got[metrics.MetricSmartUncorrectableErrs] != float64(2) {
		t.Fatalf("disk metrics = %#v", got)
	}
}

func TestCollectDiskHealthMetricSnapshotLabelsPerDevice(t *testing.T) {
	oldRun := runHealthCommand
	defer func() { runHealthCommand = oldRun }()
	runHealthCommand = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "smartctl" || len(args) != 3 {
			t.Fatalf("unexpected command %s %#v", name, args)
		}
		return []byte(`{"device":{"name":"/dev/sda","type":"sat"},"model_name":"Samsung SSD","serial_number":"S123","ata_smart_attributes":{"table":[{"name":"Reallocated_Sector_Ct","raw":{"value":4}},{"name":"Reported_Uncorrect","raw":{"value":2}}]}}`), nil
	}

	aggregate, samples := collectDiskHealthMetricSnapshot([]string{"/dev/sda"})
	if aggregate[metrics.MetricSmartReallocatedSectors] != float64(4) || aggregate[metrics.MetricSmartUncorrectableErrs] != float64(2) {
		t.Fatalf("aggregate disk metrics = %#v", aggregate)
	}
	if len(samples) != 2 {
		t.Fatalf("expected two labelled samples, got %#v", samples)
	}
	for _, sample := range samples {
		if sample.Labels["device"] != "/dev/sda" || sample.Labels["collector"] != "smartctl" {
			t.Fatalf("sample labels = %#v", sample)
		}
		if sample.Labels["device_type"] != "sat" || sample.Labels["model_name"] != "Samsung SSD" || sample.Labels["serial_number"] != "S123" {
			t.Fatalf("sample hardware labels = %#v", sample)
		}
	}
}

func resetOOMKillStateForTest(t *testing.T) {
	t.Helper()
	hostMetricState.mu.Lock()
	hostMetricState.oomKills = nil
	hostMetricState.mu.Unlock()
}

func resetWindowsHostHealthStateForTest(t *testing.T) {
	t.Helper()
	hostMetricState.mu.Lock()
	hostMetricState.windowsResourceExhaustionCheckedAt = nil
	hostMetricState.mu.Unlock()
}
