//go:build !linux && !darwin && !windows

package main

import (
	"fmt"
	"runtime"
)

func init() {
	uninstallServiceHook = uninstallService
}

// installService registers the Control One agent as a managed service on the
// host. This fallback is compiled on platforms that do not ship a concrete
// service-manager integration (anything other than linux/darwin/windows).
func installService(_ string) error {
	return fmt.Errorf("service install is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
}

// uninstallService removes the previously installed Control One agent service
// registration. Symmetric with installService; fallback returns an explicit
// error on unsupported platforms.
func uninstallService() error {
	return fmt.Errorf("service uninstall is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
}
