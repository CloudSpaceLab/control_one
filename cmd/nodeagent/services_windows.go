//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// collectPlatformServices uses `netstat -ano -p TCP` and resolves PID→name
// via `tasklist /fo csv /nh`. The combination is universally available on
// supported Windows builds and avoids a hard dependency on PowerShell.
func collectPlatformServices() ([]ServiceInfo, error) {
	out, err := exec.Command("netstat", "-ano", "-p", "TCP").Output()
	if err != nil {
		return nil, fmt.Errorf("netstat: %w", err)
	}
	var listeners []ServiceInfo
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Expect: Proto LocalAddress ForeignAddress State PID
		if len(fields) < 5 {
			continue
		}
		if !strings.EqualFold(fields[0], "TCP") {
			continue
		}
		if !strings.EqualFold(fields[3], "LISTENING") {
			continue
		}
		addr, port, ok := splitWindowsEndpoint(fields[1])
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(fields[4])
		if err != nil {
			continue
		}
		listeners = append(listeners, ServiceInfo{
			ListenAddr: addr,
			Port:       port,
			PID:        pid,
		})
	}

	if len(listeners) == 0 {
		return nil, nil
	}
	pidNames := windowsPIDNameMap()
	for i := range listeners {
		if name, ok := pidNames[listeners[i].PID]; ok {
			listeners[i].Process = name
		}
	}
	return listeners, nil
}

// splitWindowsEndpoint accepts "0.0.0.0:135" and "[::]:445".
func splitWindowsEndpoint(s string) (string, int, bool) {
	colon := strings.LastIndex(s, ":")
	if colon < 0 || colon == len(s)-1 {
		return "", 0, false
	}
	addr := strings.Trim(s[:colon], "[]")
	port, err := strconv.Atoi(s[colon+1:])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	if addr == "" {
		addr = "0.0.0.0"
	}
	return addr, port, true
}

// windowsPIDNameMap shells `tasklist /fo csv /nh` once and parses the result
// into a pid→exe-name map. Failure returns an empty map — listeners still
// land, just without process names.
func windowsPIDNameMap() map[int]string {
	out := make(map[int]string)
	cmd := exec.Command("tasklist", "/fo", "csv", "/nh")
	bytes, err := cmd.Output()
	if err != nil {
		return out
	}
	scanner := bufio.NewScanner(strings.NewReader(string(bytes)))
	for scanner.Scan() {
		// "name.exe","1234","Services","0","12,345 K"
		parts := splitCSV(scanner.Text())
		if len(parts) < 2 {
			continue
		}
		pid, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		out[pid] = parts[0]
	}
	return out
}

// splitCSV is a minimal parser for tasklist's CSV — quoted, comma-separated,
// no embedded quotes. Cheaper and more predictable than encoding/csv here.
func splitCSV(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ',' && !inQuote:
			out = append(out, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	out = append(out, cur.String())
	return out
}
