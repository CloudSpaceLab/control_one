//go:build !linux && !darwin && !windows

package main

import (
	"fmt"
	"runtime"
)

// installService registers the Control One agent as a managed service on the
// host. This fallback is compiled on platforms that do not ship a concrete
// service-manager integration (anything other than linux/darwin/windows).
func installService(_ string) error {
	return fmt.Errorf("service install is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
}

// uninstallService removes the previously installed Control One agent service
// registration. Symmetric with installService; fallback returns an explicit
// error on unsupported platforms. The CLI subcommand that exposes this to
// operators is wired up by the installer-hardening worktree
// (feat/installer-idempotent-signed); this worktree only contributes the
// platform primitive.
//
//nolint:unused // wired up from the `uninstall` subcommand in a sibling worktree
func uninstallService() error {
	return fmt.Errorf("service uninstall is not supported on %s/%s", runtime.GOOS, runtime.GOARCH)
}
