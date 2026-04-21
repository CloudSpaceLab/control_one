package remediation

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
)

// VerificationFailedReason is the constant status token reported to the CP
// when the engine refuses to exec a script because signature verification
// failed. Callers (the worker-side job closure) use this to distinguish
// "script ran and failed" from "script never ran because it was tampered".
const VerificationFailedReason = "verification_failed"

// Script represents a remediation script to execute.
type Script struct {
	RuleID             string
	Platform           string
	Version            int
	ScriptType         string
	ScriptContent      string
	Checksum           string
	Signature          string
	SignatureAlgorithm string
}

// Result captures the outcome of remediation execution.
type Result struct {
	RuleID     string
	Success    bool
	// VerificationFailed indicates that the script was refused before exec
	// because its CP CA signature was missing, tampered, or used an
	// unsupported algorithm. When true, Success is false and no command ran.
	VerificationFailed bool
	Output             string
	Error              string
	ExecutedAt         time.Time
	Duration           time.Duration
}

// Engine executes remediation scripts for compliance violations.
type Engine struct {
	log              *zap.Logger
	timeout          time.Duration
	env              map[string]string
	verifyKey        *ecdsa.PublicKey
	requireSignature bool
}

// Options configures the remediation engine.
type Options struct {
	Timeout time.Duration
	Env     map[string]string
	// VerifyKey is the CP CA public key that authenticates remediation script
	// signatures. When set, the engine verifies Script.Signature before exec.
	// Leaving it nil disables verification (dev-friendly fallback) unless
	// RequireSignature is set.
	VerifyKey *ecdsa.PublicKey
	// RequireSignature forces the engine to refuse any script without a valid
	// signature, even when VerifyKey is nil (which makes verification
	// impossible and so hard-fails every script — useful in prod where a
	// missing CA key is a misconfiguration we want to catch loudly).
	RequireSignature bool
}

// NewEngine creates a new remediation engine.
func NewEngine(log *zap.Logger, opts Options) *Engine {
	if opts.Timeout <= 0 {
		opts.Timeout = 5 * time.Minute
	}
	return &Engine{
		log:              log,
		timeout:          opts.Timeout,
		env:              opts.Env,
		verifyKey:        opts.VerifyKey,
		requireSignature: opts.RequireSignature,
	}
}

// Execute runs a remediation script and returns the result. Before the script
// is exec'd the signature (if any) is verified against the configured CP CA
// public key. A verification failure short-circuits — the command is never
// spawned and Result.VerificationFailed is set so the caller can mark the
// job `verification_failed` rather than letting the node run a tampered
// script.
func (e *Engine) Execute(ctx context.Context, script Script) (*Result, error) {
	if strings.TrimSpace(script.ScriptContent) == "" {
		return nil, errors.New("script content is empty")
	}

	start := time.Now()
	result := &Result{
		RuleID:     script.RuleID,
		ExecutedAt: start,
	}

	if err := e.verifyScript(script); err != nil {
		result.VerificationFailed = true
		result.Success = false
		result.Error = fmt.Sprintf("%s: %v", VerificationFailedReason, err)
		result.Duration = time.Since(start)
		e.log.Warn("remediation script signature verification failed; refusing to exec",
			zap.String("rule_id", script.RuleID),
			zap.String("platform", script.Platform),
			zap.Int("version", script.Version),
			zap.Error(err),
		)
		return result, fmt.Errorf("%s: %w", VerificationFailedReason, err)
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

// verifyScript checks the script signature against the CP CA public key.
// Returns nil when verification is disabled AND not required (dev mode),
// nil on a valid signature, and a wrapped error on any mismatch.
//
// Policy matrix:
//
//	VerifyKey  RequireSignature  Has signature   -> Action
//	nil        false             any             -> skip (dev mode)
//	nil        true              any             -> fail (misconfig)
//	set        any               no              -> fail (refuse unsigned)
//	set        any               yes-valid       -> pass
//	set        any               yes-invalid     -> fail
func (e *Engine) verifyScript(script Script) error {
	if e.verifyKey == nil {
		if e.requireSignature {
			return errors.New("CP CA verify key not configured but signature required")
		}
		return nil
	}
	return Verify(e.verifyKey, script.ScriptContent, script.Platform, script.Version, script.Signature, script.SignatureAlgorithm)
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
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}
