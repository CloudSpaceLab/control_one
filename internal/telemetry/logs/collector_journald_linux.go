//go:build linux

package logs

import (
    "bufio"
    "context"
    "encoding/json"
    "errors"
    "fmt"
    "os/exec"
    "strconv"
    "strings"
    "time"

    "github.com/CloudSpaceLab/control_one/internal/config"
    "go.uber.org/zap"
)

type journaldCollector struct {
    cfg    config.LogSourceConfig
    logger *zap.Logger
}

func NewJournaldCollector(cfg config.LogSourceConfig, logger *zap.Logger) (Collector, error) {
    if len(cfg.JournalUnits) == 0 && strings.TrimSpace(cfg.Program) == "" {
        return nil, errors.New("journald collector requires program or journal_units")
    }
    return &journaldCollector{cfg: cfg, logger: logger}, nil
}

func (c *journaldCollector) Run(ctx context.Context, out chan<- RawLog) error {
    args := []string{"-f", "-o", "json"}
    for _, unit := range c.cfg.JournalUnits {
        if strings.TrimSpace(unit) != "" {
            args = append(args, "-u", unit)
        }
    }
    if c.cfg.Program != "" && len(c.cfg.JournalUnits) == 0 {
        args = append(args, fmt.Sprintf("SYSLOG_IDENTIFIER=%s", c.cfg.Program))
    }

    cmd := exec.CommandContext(ctx, "journalctl", args...)
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
            c.logger.Debug("journalctl stderr", zap.String("line", scanner.Text()))
        }
        if err := scanner.Err(); err != nil && ctx.Err() == nil {
            errCh <- err
        } else {
            errCh <- nil
        }
    }()

    go func() {
        defer close(errCh)
        scanner := bufio.NewScanner(stdout)
        for scanner.Scan() {
            line := scanner.Text()
            var evt journaldEvent
            if err := json.Unmarshal([]byte(line), &evt); err != nil {
                c.logger.Debug("journalctl decode", zap.Error(err))
                continue
            }

            raw := RawLog{
                Timestamp: evt.Timestamp(),
                Program:   evt.Program(c.cfg.Program),
                Source:    evt.Source(),
                Message:   evt.Message,
                Severity:  evt.Severity(),
                Hostname:  evt.Host,
                Labels:    map[string]string{},
                Fields: map[string]any{
                    "unit":      evt.Unit,
                    "priority":  evt.Priority,
                    "identifier": evt.Identifier,
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
        if err := scanner.Err(); err != nil && ctx.Err() == nil {
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

type journaldEvent struct {
    Message    string `json:"MESSAGE"`
    Priority   string `json:"PRIORITY"`
    Unit       string `json:"_SYSTEMD_UNIT"`
    Identifier string `json:"SYSLOG_IDENTIFIER"`
    Host       string `json:"_HOSTNAME"`
    RealtimeTS string `json:"__REALTIME_TIMESTAMP"`
}

func (e journaldEvent) Timestamp() time.Time {
    if ts, err := strconv.ParseInt(strings.TrimSpace(e.RealtimeTS), 10, 64); err == nil {
        return time.Unix(0, ts*int64(time.Microsecond))
    }
    return time.Now().UTC()
}

func (e journaldEvent) Severity() string {
    if pr, err := strconv.Atoi(strings.TrimSpace(e.Priority)); err == nil {
        switch pr {
        case 0:
            return "emergency"
        case 1:
            return "alert"
        case 2:
            return "critical"
        case 3:
            return "error"
        case 4:
            return "warn"
        case 5:
            return "notice"
        case 6:
            return "info"
        case 7:
            return "debug"
        }
    }
    return "info"
}

func (e journaldEvent) Program(defaultProgram string) string {
    if e.Identifier != "" {
        return e.Identifier
    }
    if e.Unit != "" {
        return e.Unit
    }
    return defaultProgram
}

func (e journaldEvent) Source() string {
    if e.Unit != "" {
        return e.Unit
    }
    return "journald"
}

func init() {
    RegisterCollectorFactory("journald", NewJournaldCollector)
}
