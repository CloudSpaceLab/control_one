package scanner

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
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
	RuleID    string            `json:"rule_id"`
	Status    string            `json:"status"`
	Details   string            `json:"details,omitempty"`
	CheckedAt time.Time         `json:"checked_at"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Runner interface {
	Run(ctx context.Context, rules []policy.Rule) ([]Result, error)
}

type Options struct {
	Timeout       time.Duration
	Shell         string
	MaxConcurrent int
	Env           map[string]string
}

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
	name, args := resolveShell(opts.Shell)
	return &BuiltinScanner{
		log:       log,
		opts:      opts,
		shellName: name,
		shellArgs: args,
	}
}

// Run evaluates each policy rule by executing the associated command using the configured shell.
func (b *BuiltinScanner) Run(ctx context.Context, rules []policy.Rule) ([]Result, error) {
	n := len(rules)
	results := make([]Result, n)
	if n == 0 {
		return results, nil
	}

	workers := b.opts.MaxConcurrent
	if workers <= 0 {
		workers = 1
	}
	if workers > n {
		workers = n
	}

	type job struct{ idx int }
	jobs := make(chan job)
	var wg sync.WaitGroup
	wg.Add(workers)

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			rule := rules[j.idx]
			r := Result{RuleID: rule.ID, CheckedAt: time.Now().UTC(), Metadata: map[string]string{
				"severity": rule.Severity,
				"version":  rule.Version,
			}}
			for k, v := range b.opts.Env {
				if r.Metadata == nil {
					r.Metadata = map[string]string{}
				}
				r.Metadata[k] = v
			}
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
			results[j.idx] = r
		}
	}

	for i := 0; i < workers; i++ {
		go worker()
	}
	for i := 0; i < n; i++ {
		select {
		case jobs <- job{idx: i}:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return results, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
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
