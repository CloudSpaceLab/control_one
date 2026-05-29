package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/durablespool"
	"github.com/CloudSpaceLab/control_one/internal/eventstream"
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
	logSources map[string]struct{}

	triggerMu sync.Mutex
	triggers  []*compiledTrigger

	spike  *logSpikeDetector
	stream *eventstream.Stream

	logSpool     *durablespool.Spool
	logCursorDir string
}

type DurabilityOptions struct {
	LogSpoolDir      string
	LogSpoolMaxBytes int64
	LogCursorDir     string
}

// WithEventStream wires the agent's eventstream into the telemetry service
// so log.spike (and future telemetry-emitted events) flow into Doris/UI
// rather than only the local hooks subsystem. Idempotent.
func (s *Service) WithEventStream(es *eventstream.Stream) {
	if s == nil {
		return
	}
	s.stream = es
}

func (s *Service) WithDurability(opts DurabilityOptions) {
	if s == nil {
		return
	}
	s.logCursorDir = strings.TrimSpace(opts.LogCursorDir)
	if strings.TrimSpace(opts.LogSpoolDir) == "" {
		return
	}
	spool, err := durablespool.New(durablespool.Options{
		Dir:      opts.LogSpoolDir,
		Prefix:   "logs",
		MaxBytes: opts.LogSpoolMaxBytes,
	})
	if err != nil {
		s.log.Warn("log spool disabled", zap.String("dir", opts.LogSpoolDir), zap.Error(err))
		return
	}
	s.logSpool = spool
}

func (s *Service) LogSpoolStats() (durablespool.Stats, error) {
	if s == nil || s.logSpool == nil {
		return durablespool.Stats{}, nil
	}
	return s.logSpool.Stats()
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
	return &Service{client: client, log: log, hooks: hooks, spike: newLogSpikeDetector()}
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

type MetricSample struct {
	Name   string            `json:"name"`
	Value  any               `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

type MetricBatch struct {
	Metrics map[string]any
	Samples []MetricSample
}

// SendMetrics pushes periodic host metrics.
func (s *Service) SendMetrics(ctx context.Context, nodeID string, metrics map[string]any) {
	s.SendMetricBatch(ctx, nodeID, MetricBatch{Metrics: metrics})
}

// SendMetricBatch pushes aggregate host metrics plus optional labelled samples.
func (s *Service) SendMetricBatch(ctx context.Context, nodeID string, batch MetricBatch) {
	payload := map[string]any{
		"node_id":   nodeID,
		"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
		"metrics":   batch.Metrics,
	}
	if len(batch.Samples) > 0 {
		payload["metric_samples"] = batch.Samples
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

// StartLogCollection spins up collectors for configured log sources.
func (s *Service) StartLogCollection(ctx context.Context, nodeID string, sources []config.LogSourceConfig) {
	if s.AddLogSources(ctx, nodeID, sources) == 0 {
		s.logsMu.Lock()
		active := s.logsActive
		s.logsMu.Unlock()
		if !active {
			s.log.Info("log collection not started: no explicit log sources configured")
		}
	}
}

// AddLogSources starts collectors for sources that are not already active. It
// is intentionally additive: approvals can hot-add collection without tearing
// down existing file cursors or disrupting in-flight batches.
func (s *Service) AddLogSources(ctx context.Context, nodeID string, sources []config.LogSourceConfig) int {
	prepared := logs.PrepareSources(sources)
	if len(prepared) == 0 {
		return 0
	}

	toStart := make([]config.LogSourceConfig, 0, len(prepared))
	s.logsMu.Lock()
	if s.logSources == nil {
		s.logSources = map[string]struct{}{}
	}
	for _, src := range prepared {
		if src.Disabled || !config.LogCollectModeStartsCollector(src.CollectMode) {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(src.Type), "file") && strings.TrimSpace(src.CursorStateFile) == "" {
			src.CursorStateFile = s.defaultLogCursorFile(src)
		}
		key := logSourceRuntimeKey(src)
		if _, exists := s.logSources[key]; exists {
			continue
		}
		s.logSources[key] = struct{}{}
		toStart = append(toStart, src)
	}
	if len(toStart) > 0 {
		s.logsActive = true
	}
	s.logsMu.Unlock()

	for _, src := range toStart {
		source := src

		bufferSize := source.BufferSize
		if bufferSize <= 0 {
			bufferSize = 128
		}
		rawCh := make(chan logs.RawLog, bufferSize)

		go s.runCollectorLoop(ctx, nodeID, source, rawCh)

		formatter := logs.GetFormatter(source.Formatter)
		go s.consumeLogs(ctx, nodeID, source, formatter, rawCh)
	}
	return len(toStart)
}

func (s *Service) defaultLogCursorFile(source config.LogSourceConfig) string {
	dir := strings.TrimSpace(s.logCursorDir)
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(logSourceRuntimeKey(source)))
	return filepath.Join(dir, hex.EncodeToString(sum[:])+".json")
}

func logSourceRuntimeKey(source config.LogSourceConfig) string {
	parts := []string{
		strings.ToLower(strings.TrimSpace(source.Program)),
		strings.ToLower(strings.TrimSpace(source.Type)),
		strings.ToLower(strings.TrimSpace(source.Formatter)),
		config.NormalizeLogCollectMode(source.CollectMode),
		joinLogSourceStrings(source.Paths),
		joinLogSourceStrings(source.JournalUnits),
		joinLogSourceStrings(source.EventChannels),
	}
	return strings.Join(parts, "|")
}

func joinLogSourceStrings(values []string) string {
	if len(values) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" {
			normalized = append(normalized, value)
		}
	}
	sort.Strings(normalized)
	return strings.Join(normalized, ",")
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
	maxRetained := maxRetainedLogBatch(source.BatchSize)
	flushInterval := source.FlushInterval
	if flushInterval <= 0 {
		flushInterval = time.Second * 5
	}
	flushTimer := time.NewTimer(flushInterval)
	defer flushTimer.Stop()

	flush := func(reason string) {
		if len(batch) == 0 {
			if err := s.drainLogSpool(ctx); err != nil {
				s.log.Warn("log spool replay failed", zap.Error(err))
			}
			return
		}
		persisted, err := s.sendLogBatch(ctx, nodeID, source, batch)
		if err != nil {
			s.log.Warn("log batch send failed", zap.String("program", source.Program), zap.Int("count", len(batch)), zap.Error(err))
			s.publishHook(ctx, "telemetry.logs.failed", nodeID, map[string]any{
				"program": source.Program,
				"count":   len(batch),
				"error":   err.Error(),
			})
			if persisted {
				batch = batch[:0]
			}
			return
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
			entry = applyLogCollectMode(entry, source)
			s.evaluateTriggers(ctx, nodeID, entry)
			batch = append(batch, entry)
			if dropped := trimLogRetryBacklog(&batch, maxRetained); dropped > 0 {
				s.publishHook(ctx, "telemetry.logs.dropped", nodeID, map[string]any{
					"program": source.Program,
					"count":   dropped,
					"reason":  "retry_backlog_full",
				})
			}
			if spiked, baseline, current := s.spike.Record(source.Program, int64(len(entry.Message)), time.Now()); spiked {
				details := map[string]any{
					"program":            source.Program,
					"current_bytes_min":  current,
					"baseline_bytes_min": baseline,
					"ratio":              float64(current) / float64(baseline+1),
				}
				s.publishHook(ctx, "log.spike", nodeID, details)
				if s.stream != nil {
					s.stream.Publish(eventstream.Event{
						Type:     "log.spike",
						TS:       time.Now(),
						NodeID:   nodeID,
						Severity: "warning",
						Message:  source.Program,
						Details:  details,
						DedupKey: fmt.Sprintf("log.spike:%s:%d", source.Program, time.Now().Unix()/300),
					})
				}
			}
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

func maxRetainedLogBatch(batchSize int) int {
	if batchSize <= 0 {
		return 1000
	}
	max := batchSize * 4
	if max < batchSize {
		max = batchSize
	}
	if max > 5000 {
		max = 5000
	}
	return max
}

func trimLogRetryBacklog(batch *[]logs.StructuredLog, maxRetained int) int {
	if batch == nil || maxRetained <= 0 || len(*batch) <= maxRetained {
		return 0
	}
	dropped := len(*batch) - maxRetained
	copy((*batch)[0:], (*batch)[dropped:])
	*batch = (*batch)[:maxRetained]
	return dropped
}

func applyLogCollectMode(entry logs.StructuredLog, source config.LogSourceConfig) logs.StructuredLog {
	mode := config.NormalizeLogCollectMode(source.CollectMode)
	if entry.Labels == nil {
		entry.Labels = map[string]string{}
	}
	entry.Labels["control_one.collect_mode"] = mode
	switch mode {
	case config.LogCollectModeCollectParsed:
		entry.Message = "raw log omitted by collect_parsed"
		entry.Labels["control_one.raw_message_retained"] = "false"
	case config.LogCollectModeCollectRaw:
		entry.Labels["control_one.raw_message_retained"] = "true"
	}
	return entry
}

func (s *Service) sendLogBatch(ctx context.Context, nodeID string, source config.LogSourceConfig, batch []logs.StructuredLog) (bool, error) {
	payload := s.logBatchPayload(nodeID, source, batch)
	if s.logSpool != nil {
		if _, err := s.logSpool.AppendJSON(payload); err != nil {
			return false, fmt.Errorf("persist log batch: %w", err)
		}
		if err := s.drainLogSpool(ctx); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, s.postJSON(ctx, "/api/v1/logs", payload)
}

func (s *Service) logBatchPayload(nodeID string, source config.LogSourceConfig, batch []logs.StructuredLog) map[string]any {
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
	payload["replay_key"] = logReplayKey(payload)

	return payload
}

func (s *Service) drainLogSpool(ctx context.Context) error {
	if s == nil || s.logSpool == nil || ctx.Err() != nil {
		return nil
	}
	records, err := s.logSpool.Records()
	if err != nil {
		return fmt.Errorf("list log spool: %w", err)
	}
	for _, record := range records {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		body, err := s.logSpool.Read(record)
		if err != nil {
			return fmt.Errorf("read log spool: %w", err)
		}
		if !json.Valid(body) {
			_ = s.logSpool.Delete(record)
			continue
		}
		body, err = ensureLogReplayKey(body)
		if err != nil {
			return fmt.Errorf("prepare log replay key: %w", err)
		}
		if err := s.postJSONBytes(ctx, "/api/v1/logs", body); err != nil {
			return err
		}
		if err := s.logSpool.Delete(record); err != nil {
			return fmt.Errorf("delete log spool: %w", err)
		}
	}
	return nil
}

func logReplayKey(payload map[string]any) string {
	material := make(map[string]any, len(payload))
	for key, value := range payload {
		if key == "replay_key" {
			continue
		}
		material[key] = value
	}
	raw, err := json.Marshal(material)
	if err != nil {
		raw = []byte(fmt.Sprintf("%#v", material))
	}
	sum := sha256.Sum256(raw)
	return "logs:" + hex.EncodeToString(sum[:])
}

func ensureLogReplayKey(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	if raw, ok := payload["replay_key"].(string); ok && strings.TrimSpace(raw) != "" {
		return body, nil
	}
	payload["replay_key"] = logReplayKey(payload)
	return json.Marshal(payload)
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
	return s.postJSONBytes(ctx, path, body)
}

func (s *Service) postJSONBytes(ctx context.Context, path string, body []byte) error {
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
