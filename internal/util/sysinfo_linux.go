//go:build linux

package util

import (
	"os"
	"strings"
)

// readMachineID reads the Linux systemd/dbus machine-id, falling back between
// the two standard locations. Returns ("", nil) if neither file exists so
// callers can degrade gracefully to hostname-based identification.
func readMachineID() (string, error) {
	candidates := []string{
		"/etc/machine-id",
		"/var/lib/dbus/machine-id",
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path) // #nosec G304 — fixed whitelist
		if err != nil {
			continue
		}
		id := strings.TrimSpace(string(data))
		if id != "" {
			return id, nil
		}
	}
	return "", nil
}
