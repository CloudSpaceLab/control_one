//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// collectPlatformServices uses `lsof -nP -iTCP -sTCP:LISTEN -F pcLn` which
// emits machine-readable per-record output — `p` pid, `c` command, `n`
// network endpoint. We parse it line-by-line (records are separated by
// repeated `p` headers).
func collectPlatformServices() ([]ServiceInfo, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcLn").Output()
	if err != nil {
		return nil, fmt.Errorf("lsof: %w", err)
	}
	var (
		services    []ServiceInfo
		currentPID  int
		currentName string
	)
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				currentPID = pid
				currentName = ""
			}
		case 'c':
			currentName = line[1:]
		case 'n':
			// e.g. "n*:8080" or "n127.0.0.1:5432" or "n[::1]:6443"
			endpoint := strings.TrimPrefix(line[1:], "->")
			addr, port, ok := splitListenEndpointDarwin(endpoint)
			if !ok {
				continue
			}
			services = append(services, ServiceInfo{
				PID:        currentPID,
				Process:    currentName,
				ListenAddr: addr,
				Port:       port,
			})
		}
	}
	return services, nil
}

func splitListenEndpointDarwin(s string) (string, int, bool) {
	colon := strings.LastIndex(s, ":")
	if colon < 0 || colon == len(s)-1 {
		return "", 0, false
	}
	addr := strings.Trim(s[:colon], "[]")
	if addr == "*" || addr == "" {
		addr = "0.0.0.0"
	}
	port, err := strconv.Atoi(s[colon+1:])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	return addr, port, true
}
