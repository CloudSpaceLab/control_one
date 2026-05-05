//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// collectPlatformServices uses `ss` (preferred) and falls back to /proc
// parsing when ss is missing. Output for both producers is normalised into
// the same ServiceInfo slice. Errors are returned but the caller treats
// them as soft.
func collectPlatformServices() ([]ServiceInfo, error) {
	if _, err := exec.LookPath("ss"); err == nil {
		if svcs, err := collectServicesViaSS(); err == nil && len(svcs) > 0 {
			return svcs, nil
		}
		// fall through if ss returned nothing — /proc fallback below
	}
	return collectServicesViaProc()
}

// collectServicesViaSS shells out to:
//
//	ss -H -t -l -n -p
//
// -H drops the header, -t TCP only, -l listening only, -n numeric ports,
// -p attaches process info. Output sample:
//
//	LISTEN 0 511 0.0.0.0:80    *:*  users:(("nginx",pid=1234,fd=6))
func collectServicesViaSS() ([]ServiceInfo, error) {
	out, err := exec.Command("ss", "-H", "-t", "-l", "-n", "-p").Output()
	if err != nil {
		return nil, fmt.Errorf("ss: %w", err)
	}
	var services []ServiceInfo
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		// Need at least State Recv Send Local Peer (5). users:(...) is field
		// index 5 if present; absent for orphan listeners.
		if len(fields) < 5 {
			continue
		}
		addr, port, ok := splitListenEndpoint(fields[3])
		if !ok {
			continue
		}
		svc := ServiceInfo{ListenAddr: addr, Port: port}
		if len(fields) >= 6 {
			pid, name := parseSSUsers(fields[5])
			svc.PID = pid
			svc.Process = name
			if pid > 0 {
				if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
					svc.BinaryPath = exe
				}
			}
		}
		services = append(services, svc)
	}
	if err := scanner.Err(); err != nil {
		return services, fmt.Errorf("scan ss output: %w", err)
	}
	return services, nil
}

// parseSSUsers extracts the first (pid, command) tuple out of a string like
//
//	users:(("nginx",pid=1234,fd=6),("nginx",pid=1235,fd=6))
func parseSSUsers(s string) (int, string) {
	if !strings.HasPrefix(s, "users:") {
		return 0, ""
	}
	open := strings.Index(s, "((")
	if open < 0 {
		return 0, ""
	}
	rest := s[open+2:]
	close := strings.Index(rest, ")")
	if close < 0 {
		return 0, ""
	}
	tuple := rest[:close]
	parts := strings.Split(tuple, ",")
	if len(parts) < 2 {
		return 0, ""
	}
	name := strings.Trim(parts[0], `"`)
	var pid int
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if v, ok := strings.CutPrefix(p, "pid="); ok {
			if parsed, err := strconv.Atoi(v); err == nil {
				pid = parsed
				break
			}
		}
	}
	return pid, name
}

// splitListenEndpoint pulls (addr, port) out of "0.0.0.0:80", "[::]:443",
// "*:22", or "127.0.0.1:5432". Returns false on shapes we can't parse.
func splitListenEndpoint(s string) (string, int, bool) {
	colon := strings.LastIndex(s, ":")
	if colon < 0 || colon == len(s)-1 {
		return "", 0, false
	}
	addr := s[:colon]
	portStr := s[colon+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	addr = strings.Trim(addr, "[]")
	if addr == "*" || addr == "" {
		addr = "0.0.0.0"
	}
	return addr, port, true
}

// collectServicesViaProc is the last-resort fallback used on stripped
// container images that ship without iproute2. We read /proc/net/tcp[6] for
// listening sockets, then walk /proc/[pid]/net/tcp[6] to map sockets to
// processes.
func collectServicesViaProc() ([]ServiceInfo, error) {
	listeners, err := readProcNetListeners()
	if err != nil {
		return nil, err
	}
	if len(listeners) == 0 {
		return nil, nil
	}
	socketToPID := walkProcForSockets()
	for inode, ent := range listeners {
		if pid, ok := socketToPID[inode]; ok {
			ent.PID = pid
			if name, _ := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); len(name) > 0 {
				ent.Process = strings.TrimSpace(string(name))
			}
			if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid)); err == nil {
				ent.BinaryPath = exe
			}
			listeners[inode] = ent
		}
	}
	out := make([]ServiceInfo, 0, len(listeners))
	for _, ent := range listeners {
		out = append(out, ent)
	}
	return out, nil
}

func readProcNetListeners() (map[uint64]ServiceInfo, error) {
	files := []string{"/proc/net/tcp", "/proc/net/tcp6"}
	out := make(map[uint64]ServiceInfo)
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		scanner := bufio.NewScanner(f)
		first := true
		for scanner.Scan() {
			if first {
				first = false
				continue
			}
			line := strings.Fields(scanner.Text())
			// 0=sl 1=local 2=remote 3=state 4=tx_rx 5=tr 6=retrnsmt 7=uid 8=timeout 9=inode
			if len(line) < 10 {
				continue
			}
			if line[3] != "0A" { // TCP_LISTEN
				continue
			}
			addr, port, ok := parseProcNetEndpoint(line[1])
			if !ok {
				continue
			}
			inode, err := strconv.ParseUint(line[9], 10, 64)
			if err != nil {
				continue
			}
			out[inode] = ServiceInfo{ListenAddr: addr, Port: port}
		}
		_ = f.Close()
	}
	return out, nil
}

// parseProcNetEndpoint accepts "0100007F:1F90" (127.0.0.1:8080) or the
// IPv6 hex form. We render IPv4 as dotted quad and IPv6 as "::" since the
// graph only differentiates loopback vs. routable.
func parseProcNetEndpoint(s string) (string, int, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return "", 0, false
	}
	hex := parts[0]
	port64, err := strconv.ParseUint(parts[1], 16, 32)
	if err != nil {
		return "", 0, false
	}
	port := int(port64)
	switch len(hex) {
	case 8: // IPv4 little-endian
		var b [4]byte
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(hex[i*2:i*2+2], 16, 8)
			if err != nil {
				return "", 0, false
			}
			b[3-i] = byte(v)
		}
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3]), port, true
	case 32: // IPv6
		// /proc renders all-zero (any) as 32 zeros; we don't need the full
		// expansion in the knowledge graph, just enough to know it's v6.
		if strings.Trim(hex, "0") == "" {
			return "::", port, true
		}
		return "[v6]", port, true
	}
	return "", 0, false
}

// walkProcForSockets builds a map of socket-inode → owning pid by scanning
// every /proc/<pid>/fd. Restricted to numeric pid dirs we can list — read
// failures (other users' processes when running unprivileged) are silently
// skipped.
func walkProcForSockets() map[uint64]int {
	out := make(map[uint64]int)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if !strings.HasPrefix(target, "socket:[") {
				continue
			}
			inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"), 10, 64)
			if err != nil {
				continue
			}
			if _, exists := out[inode]; !exists {
				out[inode] = pid
			}
		}
	}
	return out
}
