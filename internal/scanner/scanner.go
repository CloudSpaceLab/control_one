package scanner

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/policy"
)

const (
	StatusCompliant    = "compliant"
	StatusNonCompliant = "non_compliant"
	StatusError        = "error"
)

// Result captures compliance evaluation outcome for a rule.
type Result struct {
	RuleID    string    `json:"rule_id"`
	Status    string    `json:"status"`
	Details   string    `json:"details,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// Runner defines the interface for compliance scanners.
type Runner interface {
	Run(ctx context.Context, rules []policy.Rule) ([]Result, error)
}

// Options tunes the builtin scanner runtime behavior.
type Options struct {
	Timeout time.Duration
	Shell   string
}

// BuiltinScanner executes rule checks using shell commands.
type BuiltinScanner struct {
	log       *zap.Logger
	opts      Options
	shellName string
	shellArgs []string
}

// NewBuiltinScanner creates the builtin scanner with provided options.
func NewBuiltinScanner(log *zap.Logger, opts Options) *BuiltinScanner {
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}

	shellName, shellArgs := resolveShell(opts.Shell)

	return &BuiltinScanner{
		log:       log,
		opts:      opts,
		shellName: shellName,
		shellArgs: shellArgs,
	}
}

// Run evaluates each policy rule by executing the associated command using the configured shell.
func (b *BuiltinScanner) Run(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	results := make([]Result, 0, len(rules))
	for _, rule := range rules {
		r := Result{RuleID: rule.ID, CheckedAt: time.Now().UTC()}
		cmdCtx := ctx
		var cancel context.CancelFunc
		if b.opts.Timeout > 0 {
			cmdCtx, cancel = context.WithTimeout(ctx, b.opts.Timeout)
		}

		output, err := b.runCommand(cmdCtx, rule.Check)
		if cancel != nil {
			cancel()
		}
		trimmed := strings.TrimSpace(output)
		if err == nil {
			r.Status = StatusCompliant
			r.Details = trimmed
		} else {
			if errors.Is(err, context.DeadlineExceeded) {
				r.Status = StatusError
				r.Details = "check timed out"
			} else if exitErr, ok := err.(*exec.ExitError); ok {
				r.Status = StatusNonCompliant
				r.Details = trimmed
				if r.Details == "" {
					r.Details = exitErr.Error()
				}
			} else {
				r.Status = StatusError
				r.Details = err.Error()
			}
		}
		b.log.Debug("check executed", zap.String("rule_id", rule.ID), zap.String("status", r.Status))
		results = append(results, r)
	}
	return results, nil
}

func (b *BuiltinScanner) runCommand(ctx context.Context, check string) (string, error) {
	if strings.TrimSpace(check) == "" {
		return "", errors.New("empty check command")
	}

	args := append([]string{}, b.shellArgs...)
	args = append(args, check)
	cmd := exec.CommandContext(ctx, b.shellName, args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func resolveShell(shellOverride string) (string, []string) {
	if strings.TrimSpace(shellOverride) != "" {
		tokens := strings.Fields(shellOverride)
		name := tokens[0]
		args := []string{}
		if len(tokens) > 1 {
			args = tokens[1:]
		}
		return name, args
	}

	if runtime.GOOS == "windows" {
		return "powershell.exe", []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command"}
	}
	return "/bin/sh", []string{"-c"}
}
