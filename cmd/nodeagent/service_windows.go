//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// runAsWindowsService bridges the installed executable to the Windows Service
// Control Manager. The service host launches the normal agent worker as a
// child process so the existing CLI path remains identical for interactive
// runs and service runs.
func runAsWindowsService() bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}
	args := append([]string(nil), os.Args[1:]...)
	err = svc.Run(windowsServiceName, &agentService{args: args})
	if err != nil {
		fmt.Fprintf(os.Stderr, "windows service failed: %v\n", err)
	}
	return true
}

type agentService struct {
	args []string
	cmd  *exec.Cmd
}

func (s *agentService) Execute(_ []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	s.svcStart()
	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for c := range r {
		switch c.Cmd {
		case svc.Interrogate:
			changes <- c.CurrentStatus
		case svc.Stop, svc.Shutdown:
			changes <- svc.Status{State: svc.StopPending}
			s.svcStop()
			return false, 0
		}
	}
	return false, 0
}

func (s *agentService) svcStart() {
	path, err := os.Executable()
	if err != nil {
		return
	}
	s.cmd = exec.Command(path, s.args...)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr
	_ = s.cmd.Start()
}

func (s *agentService) svcStop() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
		_, _ = s.cmd.Process.Wait()
	}
}

const (
	windowsServiceName        = "ControlOneAgent"
	windowsServiceDisplayName = "Control One Node Agent"
	windowsServiceDescription = "Control One endpoint agent: enrollment, compliance, and remediation."
)

func init() {
	uninstallServiceHook = uninstallService
}

// installService registers the Control One agent with the Windows Service
// Control Manager and starts it. If the service already exists its config is
// refreshed in place so repeated installs are idempotent.
func installService(configPath string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect scm: %w", err)
	}
	defer func() { _ = m.Disconnect() }()

	// Open and stop an existing service before replacing its executable. Windows
	// keeps the running image file locked, so copying over it while the old
	// process is alive fails with ERROR_SHARING_VIOLATION.
	s, openErr := m.OpenService(windowsServiceName)
	if openErr == nil {
		defer func() { _ = s.Close() }()
		if err := stopService(s); err != nil {
			return fmt.Errorf("stop existing service: %w", err)
		}
	} else if !errors.Is(openErr, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("open existing service: %w", openErr)
	}

	binaryPath, err := ensureInstalledBinary()
	if err != nil {
		return fmt.Errorf("resolve agent binary path: %w", err)
	}

	cfg := mgr.Config{
		DisplayName:  windowsServiceDisplayName,
		Description:  windowsServiceDescription,
		StartType:    mgr.StartAutomatic,
		ErrorControl: mgr.ErrorNormal,
	}
	args := []string{"--config", configPath}

	if s != nil {
		// Service already exists; refresh configuration so a re-run reflects
		// the latest binary path and config file, then (re)start it.
		existing, err := s.Config()
		if err != nil {
			return fmt.Errorf("read existing service config: %w", err)
		}
		// UpdateConfig passes every service field to ChangeServiceConfig. Keep
		// the fields Windows assigned when the service was created (especially
		// ServiceType and SidType); zero values are rejected as ERROR_INVALID_PARAMETER.
		// Include the config path in the command line so re-enrollment cannot
		// retain a stale platform-default path such as
		// \\etc\\control-one\\nodeagent.yaml.
		cfg = refreshedWindowsServiceConfig(existing, binaryPath, configPath)
		if err := s.UpdateConfig(cfg); err != nil {
			return fmt.Errorf("update existing service config: %w", err)
		}
		return restartService(s)
	}

	created, err := m.CreateService(windowsServiceName, binaryPath, cfg, args...)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer func() { _ = created.Close() }()

	if err := created.Start(args...); err != nil {
		// ERROR_SERVICE_ALREADY_RUNNING is fine — treat as success so install
		// is idempotent even under the narrow race between CreateService and
		// Start from a previous run that crashed mid-flight.
		if !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
			return fmt.Errorf("start service: %w", err)
		}
	}

	return nil
}

// refreshedWindowsServiceConfig preserves SCM-owned fields from an existing
// service while updating the executable, config path, and display metadata.
// mgr.Service.UpdateConfig forwards all fields to ChangeServiceConfig, so
// constructing a partial Config would turn valid values such as ServiceType
// into zeroes and produce ERROR_INVALID_PARAMETER on re-enrollment.
func refreshedWindowsServiceConfig(existing mgr.Config, binaryPath, configPath string) mgr.Config {
	existing.BinaryPathName = fmt.Sprintf(`"%s" --config "%s"`, binaryPath, configPath)
	existing.DisplayName = windowsServiceDisplayName
	existing.Description = windowsServiceDescription
	existing.StartType = mgr.StartAutomatic
	existing.ErrorControl = mgr.ErrorNormal
	return existing
}

// ensureInstalledBinary makes service enrollment independent of the directory
// from which the bootstrap executable was launched.  Bootstrap commands are
// commonly run from a download or temporary directory; SCM must instead point
// at a stable, administrator-owned location that survives cleanup of that
// directory and subsequent upgrades.
func ensureInstalledBinary() (string, error) {
	source, err := os.Executable()
	if err != nil {
		return "", err
	}
	programFiles := os.Getenv("ProgramFiles")
	if programFiles == "" {
		programFiles = `C:\Program Files`
	}
	target := filepath.Join(programFiles, "Control One", "ControlOneAgent", "controlone-agent.exe")
	if same, err := filepath.Abs(source); err == nil {
		if dst, dstErr := filepath.Abs(target); dstErr == nil && strings.EqualFold(same, dst) {
			return target, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", fmt.Errorf("create install directory: %w", err)
	}
	in, err := os.Open(source)
	if err != nil {
		return "", fmt.Errorf("open bootstrap binary: %w", err)
	}
	defer in.Close()
	out, err := os.Create(target)
	if err != nil {
		return "", fmt.Errorf("create installed binary: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return "", fmt.Errorf("copy installed binary: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", fmt.Errorf("close installed binary: %w", err)
	}
	return target, nil
}

// uninstallService stops the service (waiting briefly for the transition) and
// marks it for deletion from the SCM. A missing service is treated as a
// successful no-op so repeated uninstalls don't surface noisy errors.
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

// restartService makes a re-enrollment take effect immediately. Updating the
// SCM record alone does not replace a process that is already running from the
// previous binary, so an existing service must be stopped and started again.
func restartService(s *mgr.Service) error {
	if err := stopService(s); err != nil {
		return err
	}
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

func stopService(s *mgr.Service) error {
	status, err := s.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State != svc.Stopped {
		if _, err := s.Control(svc.Stop); err != nil &&
			!errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) &&
			!errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) {
			return fmt.Errorf("stop service: %w", err)
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			status, err = s.Query()
			if err != nil {
				return fmt.Errorf("query service while stopping: %w", err)
			}
			if status.State == svc.Stopped {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if status.State != svc.Stopped {
			return fmt.Errorf("service did not stop")
		}
	}
	return nil
}
