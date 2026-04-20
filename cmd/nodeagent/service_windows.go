//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const (
	windowsServiceName        = "ControlOneAgent"
	windowsServiceDisplayName = "Control One Node Agent"
	windowsServiceDescription = "Control One endpoint agent: enrollment, compliance, and remediation."
)

// installService registers the Control One agent with the Windows Service
// Control Manager and starts it. If the service already exists its config is
// refreshed in place so repeated installs are idempotent.
func installService(configPath string) error {
	binaryPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve agent binary path: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect scm: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	cfg := mgr.Config{
		DisplayName:  windowsServiceDisplayName,
		Description:  windowsServiceDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}
	args := []string{"--config", configPath}

	if s, err := m.OpenService(windowsServiceName); err == nil {
		// Service already exists; refresh configuration so a re-run reflects
		// the latest binary path and config file, then (re)start it.
		defer func() { _ = s.Close() }()
		cfg.BinaryPathName = binaryPath
		if err := s.UpdateConfig(cfg); err != nil {
			return fmt.Errorf("update existing service config: %w", err)
		}
		return startServiceIfStopped(s)
	}

	s, err := m.CreateService(windowsServiceName, binaryPath, cfg, args...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Start(args...); err != nil {
		// ERROR_SERVICE_ALREADY_RUNNING is fine — treat as success so install
		// is idempotent even under the narrow race between CreateService and
		// Start from a previous run that crashed mid-flight.
		if !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return fmt.Errorf("start service: %w", err)
		}
	}

	return nil
}

// uninstallService stops the service (waiting briefly for the transition) and
// marks it for deletion from the SCM. A missing service is treated as a
// successful no-op so repeated uninstalls don't surface noisy errors. The CLI
// subcommand that exposes this to operators is wired up by the
// installer-hardening worktree (feat/installer-idempotent-signed); this
// worktree only contributes the platform primitive.
//
//nolint:unused // wired up from the `uninstall` subcommand in a sibling worktree
func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect scm: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	s, err := m.OpenService(windowsServiceName)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return nil
		}
		return fmt.Errorf("open service: %w", err)
	}
	defer func() { _ = s.Close() }()

	if _, err := s.Control(svc.Stop); err != nil &&
		!errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) &&
		!errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) {
		return fmt.Errorf("stop service: %w", err)
	}

	// Best-effort wait for the service to reach Stopped before deletion.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		status, err := s.Query()
		if err != nil || status.State == svc.Stopped {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

// startServiceIfStopped starts the named service when it is not already
// running. Used by the idempotent branch of installService.
func startServiceIfStopped(s *mgr.Service) error {
	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State == svc.Running || status.State == svc.StartPending {
		return nil
	}
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}
