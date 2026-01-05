package remediation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Script represents a remediation script to execute.
type Script struct {
	RuleID       string
	Platform     string
	ScriptType   string
	ScriptContent string
	Checksum     string
}

// Result captures the outcome of remediation execution.
type Result struct {
	RuleID      string
	Success     bool
	Output      string
	Error       string
	ExecutedAt  time.Time
	Duration    time.Duration
}

// Engine executes remediation scripts for compliance violations.
type Engine struct {
	log     *zap.Logger
	timeout time.Duration
	env     map[string]string
}

// Options configures the remediation engine.
type Options struct {
	Timeout time.Duration
	Env     map[string]string
}

// NewEngine creates a new remediation engine.
func NewEngine(log *zap.Logger, opts Options) *Engine {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	return &Engine{
		log:     log,
		timeout: opts.Timeout,
		env:     opts.Env,
	}
}

// Execute runs a remediation script and returns the result.
func (e *Engine) Execute(ctx context.Context, script Script) (*Result, error) {
	if strings.TrimSpace(script.ScriptContent) == "" {
		return nil, errors.New("script content is empty")
	}

	start := time.Now()
	result := &Result{
		RuleID:     script.RuleID,
		ExecutedAt: start,
	}

	cmdCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	var cmd *exec.Cmd
	var err error

	switch strings.ToLower(script.ScriptType) {
	case "shell", "bash", "sh":
		cmd, err = e.buildShellCommand(cmdCtx, script.ScriptContent)
	case "powershell", "ps1":
		cmd, err = e.buildPowerShellCommand(cmdCtx, script.ScriptContent)
	case "ansible", "ansible-playbook", "playbook":
		cmd, err = e.buildAnsibleCommand(cmdCtx, script.ScriptContent)
	default:
		return nil, fmt.Errorf("unsupported script type: %s", script.ScriptType)
	}

	if err != nil {
		result.Error = err.Error()
		result.Duration = time.Since(start)
		return result, err
	}

	if e.env != nil {
		cmd.Env = os.Environ()
		for k, v := range e.env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	output, execErr := cmd.CombinedOutput()
	result.Duration = time.Since(start)
	result.Output = string(output)

	if execErr != nil {
		if errors.Is(execErr, context.DeadlineExceeded) {
			result.Error = "remediation script timed out"
		} else {
			result.Error = execErr.Error()
		}
		result.Success = false
		e.log.Warn("remediation script failed",
			zap.String("rule_id", script.RuleID),
			zap.String("script_type", script.ScriptType),
			zap.Error(execErr),
			zap.Duration("duration", result.Duration),
		)
		return result, nil
	}

	result.Success = true
	e.log.Info("remediation script executed successfully",
		zap.String("rule_id", script.RuleID),
		zap.String("script_type", script.ScriptType),
		zap.Duration("duration", result.Duration),
	)

	return result, nil
}

func (e *Engine) buildShellCommand(ctx context.Context, content string) (*exec.Cmd, error) {
	if runtime.GOOS == "windows" {
		return nil, errors.New("shell scripts not supported on Windows, use powershell")
	}

	shell := "/bin/bash"
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if _, err := exec.LookPath("bash"); err != nil {
			shell = "/bin/sh"
		}
	}

	cmd := exec.CommandContext(ctx, shell, "-c", content)
	return cmd, nil
}

func (e *Engine) buildPowerShellCommand(ctx context.Context, content string) (*exec.Cmd, error) {
	if runtime.GOOS != "windows" {
		if _, err := exec.LookPath("pwsh"); err != nil {
			if _, err := exec.LookPath("powershell"); err != nil {
				return nil, errors.New("powershell not available on this platform")
			}
			cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", content)
			return cmd, nil
		}
		cmd := exec.CommandContext(ctx, "pwsh", "-NoProfile", "-NonInteractive", "-Command", content)
		return cmd, nil
	}

	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", content)
	return cmd, nil
}

func (e *Engine) buildAnsibleCommand(ctx context.Context, content string) (*exec.Cmd, error) {
	ansiblePath := e.findAnsible()
	if ansiblePath == "" {
		return nil, errors.New("ansible-playbook not available")
	}

	playbookFile, err := e.writeTempPlaybook(content)
	if err != nil {
		return nil, fmt.Errorf("write temp playbook: %w", err)
	}

	cmd := exec.CommandContext(ctx, ansiblePath, playbookFile,
		"--connection", "local",
		"--inventory", "localhost,")

	return cmd, nil
}

func (e *Engine) findAnsible() string {
	paths := []string{
		"ansible-playbook",
		"/usr/bin/ansible-playbook",
		"/usr/local/bin/ansible-playbook",
	}
	for _, path := range paths {
		if _, err := exec.LookPath(path); err == nil {
			return path
		}
	}
	return ""
}

func (e *Engine) writeTempPlaybook(content string) (string, error) {
	tmpFile, err := os.CreateTemp("", "remediation-playbook-*.yml")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

