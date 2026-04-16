package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/hooks"
	"github.com/CloudSpaceLab/control_one/internal/scanner"
	"github.com/CloudSpaceLab/control_one/internal/telemetry/logs"
)

// Service encapsulates telemetry operations towards the control plane.
type Service struct {
	client *api.Client
	log    *zap.Logger
	hooks  hooks.Publisher

	logsMu     sync.Mutex
	logsActive bool

	triggerMu sync.Mutex
	triggers  []*compiledTrigger
}

type compiledTrigger struct {
	cfg     config.LogTriggerConfig
	re      *regexp.Regexp
	lastRun time.Time
	runs    int
}

// SendHeartbeat notifies the control plane that the node is alive.
func (s *Service) SendHeartbeat(ctx context.Context, nodeID, heartbeatID string) {
	payload := map[string]any{
		"node_id": nodeID,
		"heartbeat": map[string]any{
			"id":        heartbeatID,
			"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}
	if err := s.postJSON(ctx, "/api/v1/heartbeat", payload); err != nil {
		s.log.Warn("send heartbeat failed", zap.Error(err))
	}
}

// New creates a telemetry service.
func New(client *api.Client, log *zap.Logger, hooks hooks.Publisher) *Service {
	return &Service{client: client, log: log, hooks: hooks}
}

// LoadTriggers compiles and stores log triggers.
func (s *Service) LoadTriggers(triggers []config.LogTriggerConfig) {
	s.triggerMu.Lock()
	defer s.triggerMu.Unlock()

	compiled := make([]*compiledTrigger, 0, len(triggers))
	for _, t := range triggers {
		re, err := regexp.Compile(t.Regex)
		if err != nil {
			s.log.Warn("log trigger regex compile failed", zap.String("id", t.ID), zap.String("regex", t.Regex), zap.Error(err))
			continue
		}
		compiled = append(compiled, &compiledTrigger{cfg: t, re: re})
	}
	s.triggers = compiled
	if len(compiled) > 0 {
		s.log.Info("log triggers configured", zap.Int("count", len(compiled)))
	}
}

// SendMetrics pushes periodic host metrics.
func (s *Service) SendMetrics(ctx context.Context, nodeID string, metrics map[string]any) {
	payload := map[string]any{
		"node_id":   nodeID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"metrics":   metrics,
	}
	if err := s.postJSON(ctx, "/api/v1/telemetry", payload); err != nil {
		s.log.Warn("send metrics failed", zap.Error(err))
	}
}

// SendCompliance reports compliance scan results.
func (s *Service) SendCompliance(ctx context.Context, nodeID string, results []scanner.Result) {
	summary := map[string]int{
		scanner.StatusCompliant:    0,
		scanner.StatusNonCompliant: 0,
		scanner.StatusError:        0,
	}
	for _, r := range results {
		summary[r.Status]++
	}
	payload := map[string]any{
		"node_id":   nodeID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"results":   results,
		"summary": map[string]any{
			"compliant":     summary[scanner.StatusCompliant],
			"non_compliant": summary[scanner.StatusNonCompliant],
			"error":         summary[scanner.StatusError],
			"total":         len(results),
		},
	}
	if err := s.postJSON(ctx, "/api/v1/compliance/report", payload); err != nil {
		s.log.Warn("send compliance failed", zap.Error(err))
	}
}

// StartLogCollection spins up collectors for configured log sources once.
func (s *Service) StartLogCollection(ctx context.Context, nodeID string, sources []config.LogSourceConfig) {
	s.logsMu.Lock()
	if s.logsActive {
		s.logsMu.Unlock()
		return
	}
	s.logsActive = true
	s.logsMu.Unlock()

	prepared := logs.PrepareSources(sources)

	for _, src := range prepared {
		source := src
		if source.Disabled {
			continue
		}

		bufferSize := source.BufferSize
		if bufferSize <= 0 {
			bufferSize = 128
		}
		rawCh := make(chan logs.RawLog, bufferSize)

		go s.runCollectorLoop(ctx, nodeID, source, rawCh)

		formatter := logs.GetFormatter(source.Formatter)
		go s.consumeLogs(ctx, nodeID, source, formatter, rawCh)
	}
}

func (s *Service) runCollectorLoop(ctx context.Context, nodeID string, source config.LogSourceConfig, out chan<- logs.RawLog) {
	defer close(out)
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	for {
		if ctx.Err() != nil {
			return
		}

		collector, err := logs.NewCollector(source, s.log)
		if err != nil {
			s.log.Warn("log collector init failed", zap.String("program", source.Program), zap.String("type", source.Type), zap.Error(err))
			s.publishHook(ctx, "telemetry.logs.collector.failed", nodeID, map[string]any{
				"program": source.Program,
				"type":    source.Type,
				"error":   err.Error(),
			})
			s.sleepWithBackoff(ctx, backoff)
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		if err := collector.Run(ctx, out); err != nil && ctx.Err() == nil {
			s.log.Warn("log collector exited", zap.String("program", source.Program), zap.Error(err))
			s.publishHook(ctx, "telemetry.logs.collector.exited", nodeID, map[string]any{
				"program": source.Program,
				"error":   err.Error(),
			})
			s.sleepWithBackoff(ctx, backoff)
			if backoff < maxBackoff {
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
			}
			continue
		}

		return
	}
}

func (s *Service) sleepWithBackoff(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (s *Service) consumeLogs(ctx context.Context, nodeID string, source config.LogSourceConfig, formatter logs.Formatter, rawCh <-chan logs.RawLog) {
	batch := make([]logs.StructuredLog, 0, source.BatchSize)
	flushInterval := source.FlushInterval
	if flushInterval <= 0 {
		flushInterval = time.Second * 5
	}
	flushTimer := time.NewTimer(flushInterval)
	defer flushTimer.Stop()

	flush := func(reason string) {
		if len(batch) == 0 {
			return
		}
		if err := s.sendLogBatch(ctx, nodeID, source, batch); err != nil {
			s.log.Warn("log batch send failed", zap.String("program", source.Program), zap.Int("count", len(batch)), zap.Error(err))
			s.publishHook(ctx, "telemetry.logs.failed", nodeID, map[string]any{
				"program": source.Program,
				"count":   len(batch),
				"error":   err.Error(),
			})
		} else {
			s.publishHook(ctx, "telemetry.logs.forwarded", nodeID, map[string]any{
				"program": source.Program,
				"count":   len(batch),
				"reason":  reason,
			})
		}
		batch = batch[:0]
	}

	resetTimer := func() {
		if !flushTimer.Stop() {
			select {
			case <-flushTimer.C:
			default:
			}
		}
		flushTimer.Reset(flushInterval)
	}

	for {
		select {
		case <-ctx.Done():
			flush("context_cancelled")
			return
		case raw, ok := <-rawCh:
			if !ok {
				flush("collector_closed")
				return
			}
			entry, err := formatter.Format(raw, source)
			if err != nil {
				s.log.Debug("log format failed", zap.String("program", source.Program), zap.Error(err))
				s.publishHook(ctx, "telemetry.logs.format.failed", nodeID, map[string]any{
					"program": source.Program,
					"error":   err.Error(),
				})
				continue
			}
			s.evaluateTriggers(ctx, nodeID, entry)
			batch = append(batch, entry)
			if source.BatchSize > 0 && len(batch) >= source.BatchSize {
				flush("batch_size")
				resetTimer()
			}
		case <-flushTimer.C:
			flush("interval")
			flushTimer.Reset(flushInterval)
		}
	}
}

func (s *Service) sendLogBatch(ctx context.Context, nodeID string, source config.LogSourceConfig, batch []logs.StructuredLog) error {
	entries := make([]map[string]any, 0, len(batch))
	for _, entry := range batch {
		entries = append(entries, entry.ToMap())
	}

	payload := map[string]any{
		"node_id":        nodeID,
		"program":        source.Program,
		"collector_type": source.Type,
		"count":          len(entries),
		"entries":        entries,
	}

	if len(source.Labels) > 0 {
		payload["labels"] = source.Labels
	}
	if len(source.Paths) > 0 {
		payload["paths"] = source.Paths
	}
	if len(source.JournalUnits) > 0 {
		payload["journal_units"] = source.JournalUnits
	}
	if len(source.EventChannels) > 0 {
		payload["event_channels"] = source.EventChannels
	}

	return s.postJSON(ctx, "/api/v1/logs", payload)
}

func (s *Service) publishHook(parent context.Context, eventID, nodeID string, payload map[string]any) {
	if s.hooks == nil {
		return
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if _, ok := payload["node_id"]; !ok {
		payload["node_id"] = nodeID
	}

	hCtx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	evt := &hooks.Event{
		EventID:   eventID,
		Source:    "telemetry",
		Subject:   nodeID,
		Payload:   payload,
		Metadata:  map[string]string{"component": "telemetry"},
		Timestamp: time.Now().UTC(),
	}
	if err := s.hooks.PublishEvent(hCtx, evt); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Debug("telemetry hook publish failed", zap.String("event_id", eventID), zap.Error(err))
	}
}

func (s *Service) postJSON(ctx context.Context, path string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telemetry payload: %w", err)
	}
	resp, err := s.client.Do(ctx, "POST", path, body)
	if err != nil {
		return fmt.Errorf("post telemetry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry request failed: status %d", resp.StatusCode)
	}

	return nil
}

func (s *Service) evaluateTriggers(ctx context.Context, nodeID string, entry logs.StructuredLog) {
	s.triggerMu.Lock()
	triggers := make([]*compiledTrigger, len(s.triggers))
	copy(triggers, s.triggers)
	s.triggerMu.Unlock()

	if len(triggers) == 0 {
		return
	}

	now := time.Now()
	for _, t := range triggers {
		if t == nil {
			continue
		}
		if strings.TrimSpace(t.cfg.Program) != "" && !strings.EqualFold(t.cfg.Program, entry.Program) {
			continue
		}
		if !t.re.MatchString(entry.Message) {
			continue
		}
		// cooldown and max-runs checks (shared by pointer, so guard with triggerMu)
		s.triggerMu.Lock()
		skip := false
		if t.cfg.MaxRuns > 0 && t.runs >= t.cfg.MaxRuns {
			skip = true
		}
		if !skip && t.cfg.Cooldown > 0 && !t.lastRun.IsZero() && now.Sub(t.lastRun) < t.cfg.Cooldown {
			skip = true
		}
		if !skip {
			t.runs++
			t.lastRun = now
		}
		s.triggerMu.Unlock()

		if skip {
			continue
		}

		if t.cfg.HooksEnabled {
			payload := map[string]any{
				"trigger_id": t.cfg.ID,
				"program":    entry.Program,
				"severity":   entry.Severity,
				"message":    entry.Message,
			}
			if len(entry.Labels) > 0 {
				payload["labels"] = entry.Labels
			}
			s.publishHook(ctx, "telemetry.logs.trigger.matched", nodeID, payload)
		}

		if t.cfg.ScriptsEnabled && strings.TrimSpace(t.cfg.Script) != "" {
			go s.runTriggerScript(ctx, nodeID, t.cfg, entry)
		}
	}
}

func (s *Service) runTriggerScript(parent context.Context, nodeID string, cfg config.LogTriggerConfig, entry logs.StructuredLog) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, cfg.Script) // #nosec G204 — script path is admin-configured
	output, err := cmd.CombinedOutput()

	payload := map[string]any{
		"trigger_id": cfg.ID,
		"program":    entry.Program,
		"message":    entry.Message,
		"exit_code":  cmd.ProcessState.ExitCode(),
		"output":     string(output),
	}
	if len(entry.Labels) > 0 {
		payload["labels"] = entry.Labels
	}
	if err != nil {
		s.log.Warn("log trigger script failed", zap.String("id", cfg.ID), zap.Error(err))
		s.publishHook(parent, "telemetry.logs.trigger.script.failed", nodeID, payload)
		return
	}
	s.publishHook(parent, "telemetry.logs.trigger.script.completed", nodeID, payload)
}
