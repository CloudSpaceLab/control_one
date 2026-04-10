package sessionrecording

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"go.uber.org/zap"
)

// SSHInterceptor handles SSH session recording via PAM or SSH wrapper
type SSHInterceptor struct {
	service *Service
	log     *zap.Logger
}

// NewSSHInterceptor creates a new SSH interceptor
func NewSSHInterceptor(service *Service, log *zap.Logger) *SSHInterceptor {
	return &SSHInterceptor{
		service: service,
		log:     log,
	}
}

// InterceptSSHSession starts recording for an SSH session
func (i *SSHInterceptor) InterceptSSHSession(ctx context.Context, userID, remoteAddr string) (string, error) {
	if i.service == nil || !i.service.cfg.Enabled || !i.service.cfg.RecordSSH {
		return "", nil
	}

	metadata := map[string]any{
		"remote_addr": remoteAddr,
		"protocol":    "ssh",
		"intercepted": true,
	}

	sessionID, err := i.service.StartSession(ctx, "ssh", userID, metadata)
	if err != nil {
		i.log.Warn("failed to start SSH session recording", zap.Error(err))
		return "", err
	}

	return sessionID, nil
}

// SetupPAMIntegration sets up PAM integration for automatic SSH session recording
func (i *SSHInterceptor) SetupPAMIntegration() error {
	if !i.service.cfg.Enabled || !i.service.cfg.RecordSSH {
		return nil
	}

	pamConfig := `
# Control One Session Recording PAM Configuration
# Add this to /etc/pam.d/sshd

session optional pam_exec.so seteuid /usr/local/bin/control-one-ssh-record %u %h %s
`

	configPath := "/etc/pam.d/control-one-ssh"
	if err := os.WriteFile(configPath, []byte(pamConfig), 0644); err != nil {
		return fmt.Errorf("write PAM config: %w", err)
	}

	i.log.Info("PAM configuration written", zap.String("path", configPath))
	return nil
}

// CreateSSHWrapperScript creates a wrapper script for SSH session recording
func (i *SSHInterceptor) CreateSSHWrapperScript() error {
	if !i.service.cfg.Enabled || !i.service.cfg.RecordSSH {
		return nil
	}

	wrapperScript := `#!/bin/bash
# Control One SSH Session Recording Wrapper
# This script wraps SSH sessions for recording

SESSION_ID=$(uuidgen)
USER_ID="$1"
REMOTE_ADDR="$2"

# Start session recording
/usr/local/bin/control-one-nodeagent record-start "$SESSION_ID" "$USER_ID" "$REMOTE_ADDR" ssh

# Execute original SSH command
exec "$@"

# Stop session recording on exit
EXIT_CODE=$?
/usr/local/bin/control-one-nodeagent record-stop "$SESSION_ID" $EXIT_CODE
exit $EXIT_CODE
`

	scriptPath := "/usr/local/bin/control-one-ssh-record"
	if err := os.WriteFile(scriptPath, []byte(wrapperScript), 0755); err != nil {
		return fmt.Errorf("write wrapper script: %w", err)
	}

	i.log.Info("SSH wrapper script created", zap.String("path", scriptPath))
	return nil
}

// SetupTerminalRecording sets up terminal session recording via shell hooks
func (i *SSHInterceptor) SetupTerminalRecording() error {
	if !i.service.cfg.Enabled || !i.service.cfg.RecordTerminal {
		return nil
	}

	shellHook := `
# Control One Terminal Session Recording Hook
if [ -n "$CONTROL_ONE_SESSION_ID" ]; then
    export TLOG_REC_SESSION="$CONTROL_ONE_SESSION_ID"
    if command -v tlog-rec >/dev/null 2>&1; then
        exec tlog-rec -w "$CONTROL_ONE_STORAGE_PATH/session-${CONTROL_ONE_SESSION_ID}.rec" "$SHELL"
    fi
fi
`

	hookPath := "/etc/profile.d/control-one-session.sh"
	if err := os.WriteFile(hookPath, []byte(shellHook), 0644); err != nil {
		return fmt.Errorf("write shell hook: %w", err)
	}

	i.log.Info("Terminal recording hook created", zap.String("path", hookPath))
	return nil
}

// RecordCommand records a single command execution
func (i *SSHInterceptor) RecordCommand(ctx context.Context, userID, command string, args []string) error {
	if i.service == nil || !i.service.cfg.Enabled || !i.service.cfg.RecordCommands {
		return nil
	}

	metadata := map[string]any{
		"command":     command,
		"args":        args,
		"recorded_at": time.Now().UTC(),
	}

	sessionID, err := i.service.StartSession(ctx, "command", userID, metadata)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	stopMetadata := map[string]any{
		"exit_code": exitCode,
		"output":    string(output),
	}

	if err := i.service.StopSession(ctx, sessionID); err != nil {
		i.log.Warn("failed to stop command recording session", zap.Error(err))
	} else {
		if err := i.service.NotifyControlPlane(ctx, sessionID, "command", userID, "stopped", stopMetadata); err != nil {
			i.log.Warn("failed to notify control plane of command recording", zap.Error(err))
		}
	}

	return nil
}

// CheckDependencies verifies that required recording tools are available
func (i *SSHInterceptor) CheckDependencies() error {
	if !i.service.cfg.Enabled {
		return nil
	}

	var missing []string

	switch strings.ToLower(i.service.cfg.Backend) {
	case "tlog":
		if _, err := exec.LookPath(i.service.cfg.TlogPath); err != nil {
			missing = append(missing, fmt.Sprintf("tlog (%s)", i.service.cfg.TlogPath))
		}
	case "auditx":
		if _, err := exec.LookPath(i.service.cfg.AuditxPath); err != nil {
			missing = append(missing, fmt.Sprintf("auditx (%s)", i.service.cfg.AuditxPath))
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required dependencies: %s", strings.Join(missing, ", "))
	}

	return nil
}
