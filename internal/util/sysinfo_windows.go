//go:build windows

package util

import (
	"os/exec"
	"strings"
)

// readMachineID reads the MachineGuid from the Windows registry via reg.exe
// so we don't need an extra dependency for a single call. Returns ("", nil)
// if reg.exe is unavailable so callers can degrade gracefully.
func readMachineID() (string, error) {
	out, err := exec.Command("reg", "query", `HKLM\SOFTWARE\Microsoft\Cryptography`, "/v", "MachineGuid").Output()
	if err != nil {
		return "", nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "MachineGuid") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 3 {
			id := strings.TrimSpace(fields[len(fields)-1])
			if id != "" {
				return id, nil
			}
		}
	}
	return "", nil
}
