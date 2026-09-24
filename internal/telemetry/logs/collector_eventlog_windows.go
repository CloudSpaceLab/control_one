//go:build windows

package logs

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/config"
	"go.uber.org/zap"
)

type eventLogCollector struct {
	cfg    config.LogSourceConfig
	logger *zap.Logger
}

func NewEventLogCollector(cfg config.LogSourceConfig, logger *zap.Logger) (Collector, error) {
	if len(cfg.EventChannels) == 0 && strings.TrimSpace(cfg.Program) == "" {
		return nil, errors.New("eventlog collector requires event_channels or program")
	}
	return &eventLogCollector{cfg: cfg, logger: logger}, nil
}

func (c *eventLogCollector) Run(ctx context.Context, out chan<- RawLog) error {
	channels := c.cfg.EventChannels
	if len(channels) == 0 {
		channels = []string{c.cfg.Program}
	}

	args := []string{"-NoProfile", "-Command"}
	script := buildPowerShellScript(channels)
	args = append(args, script)

	cmd := exec.CommandContext(ctx, "powershell.exe", args...)
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
			c.logger.Debug("eventlog stderr", zap.String("line", scanner.Text()))
		}
		errCh <- scanner.Err()
	}()

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			evt := parseEventLogJSON(line)
			raw := RawLog{
				Timestamp: evt.Timestamp,
				Program:   evt.Provider,
				Source:    evt.Channel,
				Message:   evt.Message,
				Severity:  evt.Level,
				Hostname:  evt.Computer,
				Labels:    map[string]string{},
				Fields: map[string]any{
					"event_id": evt.EventID,
					"keywords": evt.Keywords,
					"task":     evt.Task,
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

func buildPowerShellScript(channels []string) string {
	quoted := make([]string, 0, len(channels))
	for _, ch := range channels {
		ch = strings.TrimSpace(ch)
		if ch != "" {
			quoted = append(quoted, "'"+ch+"'")
		}
	}
	if len(quoted) == 0 {
		quoted = append(quoted, "'Application'")
	}
	// Poll so emitted JSON is written directly to the child stdout. PowerShell
	// event-job callbacks do not forward Write-Output to this pipe.
	// Probe each channel before entering the polling loop.  Previously all
	// Get-WinEvent errors were swallowed, so a service without Security-log
	// access looked healthy while silently collecting zero events.
	return "$channels = @(" + strings.Join(quoted, ",") + "); foreach ($channel in $channels) { try { @(Get-WinEvent -LogName $channel -MaxEvents 1 -ErrorAction Stop) | Out-Null } catch { [Console]::Error.WriteLine(('eventlog channel {0} unavailable: {1}' -f $channel, $_.Exception.Message)); exit 1 } }; $seen = @{}; while ($true) {" +
		"foreach ($channel in $channels) {" +
		"try { $events = @(Get-WinEvent -LogName $channel -MaxEvents 25 -ErrorAction Stop); foreach ($evt in ($events | Sort-Object RecordId)) { $key = ($channel + ':' + $evt.RecordId); if ($seen.ContainsKey($key)) { continue }; $seen[$key] = $true; $data = @{ 'Timestamp' = $evt.TimeCreated.ToUniversalTime().ToString('o'); 'Provider' = $evt.ProviderName; 'Channel' = $evt.LogName; 'Message' = $evt.FormatDescription(); 'Level' = $evt.LevelDisplayName; 'EventID' = $evt.Id; 'Computer' = $evt.MachineName; 'Keywords' = $evt.KeywordsDisplayNames; 'Task' = $evt.TaskDisplayName }; [Console]::Out.WriteLine(($data | ConvertTo-Json -Compress)) } } catch {} }" +
		"; Start-Sleep -Seconds 5 }"
}

type eventRecord struct {
	Timestamp time.Time
	Provider  string
	Channel   string
	Message   string
	Level     string
	EventID   int
	Computer  string
	Keywords  any
	Task      any
}

func parseEventLogJSON(line string) eventRecord {
	var evt eventRecord
	_ = json.Unmarshal([]byte(line), &evt)
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	if evt.Level == "" {
		evt.Level = "info"
	}
	return evt
}

func init() {
	RegisterCollectorFactory("eventlog", NewEventLogCollector)
}
