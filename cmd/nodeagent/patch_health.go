package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/packageinventory"
)

func collectPatchHealth() packageinventory.Health {
	h := packageinventory.Health{PackageAudit: "unknown", ServiceHealth: "unknown"}
	if runtime.GOOS != "linux" {
		h.ObservedAt = time.Now().UTC()
		return h
	}
	if raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
		h.BootID = strings.TrimSpace(string(raw))
	}
	run := func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).CombinedOutput()
	}
	if _, err := exec.LookPath("dpkg"); err == nil {
		if out, err := run("dpkg", "--audit"); err == nil && strings.TrimSpace(string(out)) == "" {
			h.PackageAudit = "healthy"
		} else {
			h.PackageAudit = "unhealthy"
		}
		if _, err := os.Stat("/run/reboot-required"); err == nil {
			required := true
			h.RebootRequired = &required
		} else if os.IsNotExist(err) {
			required := false
			h.RebootRequired = &required
		}
	}
	if _, err := exec.LookPath("systemctl"); err == nil {
		if out, err := run("systemctl", "is-system-running"); err == nil && strings.TrimSpace(string(out)) == "running" {
			h.ServiceHealth = "healthy"
		} else if strings.TrimSpace(string(out)) == "degraded" {
			h.ServiceHealth = "unhealthy"
		}
		if out, err := run("systemctl", "list-units", "--type=service", "--all", "--plain", "--no-legend", "--no-pager"); err == nil {
			h.ServicesComplete = true
			h.ServiceChecks = map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
					h.ServicesComplete = false
					continue
				}
				h.ServiceChecks[fields[0]] = fields[2]
				if fields[2] == "active" && fields[3] == "running" {
					h.ActiveServices = append(h.ActiveServices, fields[0])
				}
			}
		}
	}
	h.ConsumersComplete = patchConsumersCurrent()
	if !h.ConsumersComplete && h.ServiceHealth == "healthy" {
		h.ServiceHealth = "unknown"
	}
	h.ObservedAt = time.Now().UTC()
	return h
}

func patchConsumersCurrent() bool {
	started := time.Now()
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	total := 0
	for _, process := range processes {
		if time.Since(started) > 2*time.Second {
			return false
		}
		if _, err := strconv.Atoi(process.Name()); err != nil {
			continue
		}
		base := filepath.Join("/proc", process.Name())
		if executable, err := os.Readlink(filepath.Join(base, "exe")); err == nil {
			if strings.HasSuffix(executable, " (deleted)") {
				return false
			}
		} else if !os.IsNotExist(err) {
			return false
		}
		file, err := os.Open(filepath.Join(base, "maps"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false
		}
		maps, err := io.ReadAll(io.LimitReader(file, int64(64*1024*1024-total+1)))
		_ = file.Close()
		if err != nil {
			return false
		}
		total += len(maps)
		if total > 64*1024*1024 {
			return false
		}
		if strings.Contains(string(maps), " (deleted)") {
			return false
		}
	}
	return true
}
