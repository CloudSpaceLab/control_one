//go:build !linux && !windows && !darwin

package main

// collectPlatformServices is a no-op on platforms where we don't have a
// listener-enumeration tool wired in. The agent silently reports an empty
// services list rather than refusing to compile.
func collectPlatformServices() ([]ServiceInfo, error) {
	return nil, nil
}
