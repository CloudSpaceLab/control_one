//go:build darwin

package logs

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
	"go.uber.org/zap"
)

type unifiedLogCollector struct {
	cfg    config.LogSourceConfig
	logger *zap.Logger
}

func NewUnifiedLogCollector(cfg config.LogSourceConfig, logger *zap.Logger) (Collector, error) {
	if len(cfg.Paths) == 0 && strings.TrimSpace(cfg.Program) == "" {
		return nil, errors.New("unified log collector requires program or paths (predicates)")
	}
	return &unifiedLogCollector{cfg: cfg, logger: logger}, nil
}

func (c *unifiedLogCollector) Run(ctx context.Context, out chan<- RawLog) error {
	args := []string{"stream", "--style", "json"}
	if len(c.cfg.Paths) > 0 {
		args = append(args, c.cfg.Paths...)
	} else if c.cfg.Program != "" {
		args = append(args, "--predicate", "subsystem == '"+c.cfg.Program+"'")
	}

	cmd := exec.CommandContext(ctx, "/usr/bin/log", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	errCh := make(chan error, 2)

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			c.logger.Debug("unified log stderr", zap.String("line", scanner.Text()))
		}
		errCh <- scanner.Err()
	}()

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			entry := parseUnifiedLogJSON(scanner.Text())
			raw := RawLog{
				Timestamp: entry.Timestamp,
				Program:   entry.Subsystem,
				Source:    entry.Category,
				Message:   entry.Message,
				Severity:  entry.Level,
				Hostname:  entry.Host,
				Labels:    map[string]string{},
				Fields: map[string]any{
					"process": entry.Process,
					"pid":     entry.PID,
					"thread":  entry.ThreadID,
				},
			}
			for k, v := range c.cfg.Labels {
				raw.Labels[k] = v
			}
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case out <- raw:
			}
		}
		if err := scanner.Err(); err != nil {
			errCh <- err
		} else {
			errCh <- cmd.Wait()
		}
	}()

	for i := 0; i < 2; i++ {
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
	}
	return nil
}

type unifiedLogEntry struct {
	Timestamp time.Time
	Subsystem string
	Category  string
	Message   string
	Level     string
	Process   string
	PID       int
	ThreadID  int
	Host      string
}

func parseUnifiedLogJSON(line string) unifiedLogEntry {
	// Minimal parsing to avoid heavy dependencies.
	entry := unifiedLogEntry{
		Timestamp: time.Now().UTC(),
		Level:     "info",
	}
	fields := strings.Split(line, ",")
	for _, field := range fields {
		parts := strings.SplitN(field, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.Trim(strings.Trim(parts[0], "{}\""), " ")
		val := strings.Trim(strings.Trim(parts[1], "{}\""), " ")
		switch key {
		case "timestamp":
			if ts, err := time.Parse(time.RFC3339Nano, val); err == nil {
				entry.Timestamp = ts
			}
		case "subsystem":
			entry.Subsystem = val
		case "category":
			entry.Category = val
		case "eventMessage":
			entry.Message = val
		case "level":
			entry.Level = val
		case "process":
			entry.Process = val
		case "processID":
			if id, err := strconv.Atoi(val); err == nil {
				entry.PID = id
			}
		case "threadID":
			if id, err := strconv.Atoi(val); err == nil {
				entry.ThreadID = id
			}
		case "host":
			entry.Host = val
		}
	}
	return entry
}

func init() {
	RegisterCollectorFactory("unified", NewUnifiedLogCollector)
}
