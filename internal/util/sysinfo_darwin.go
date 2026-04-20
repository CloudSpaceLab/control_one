//go:build darwin

package util

import (
	"os/exec"
	"strings"
)

// readMachineID reads the macOS IOPlatformUUID from ioreg. Returns ("", nil)
// if ioreg is unavailable so callers can degrade gracefully.
func readMachineID() (string, error) {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return "", nil
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "IOPlatformUUID") {
			continue
		}
		// Format: "IOPlatformUUID" = "XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX"
		parts := strings.Split(line, "=")
		if len(parts) != 2 {
			continue
		}
		id := strings.TrimSpace(parts[1])
		id = strings.Trim(id, "\"")
		if id != "" {
			return id, nil
		}
	}
	return "", nil
}
