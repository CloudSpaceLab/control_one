package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	for _, src := range sources {
		source := src
		if source.Disabled {
			continue
		}

		collector, err := logs.NewCollector(source, s.log)
		if err != nil {
			s.log.Warn("log collector init failed", zap.String("program", source.Program), zap.String("type", source.Type), zap.Error(err))
			s.publishHook(ctx, "telemetry.logs.collector.failed", nodeID, map[string]any{
				"program": source.Program,
				"type":    source.Type,
				"error":   err.Error(),
			})
			continue
		}

		bufferSize := source.BufferSize
		if bufferSize <= 0 {
			bufferSize = 128
		}
		rawCh := make(chan logs.RawLog, bufferSize)

		go func(program string) {
			defer close(rawCh)
			if err := collector.Run(ctx, rawCh); err != nil && ctx.Err() == nil {
				s.log.Warn("log collector exited", zap.String("program", program), zap.Error(err))
				s.publishHook(ctx, "telemetry.logs.collector.exited", nodeID, map[string]any{
					"program": program,
					"error":   err.Error(),
				})
			}
		}(source.Program)

		formatter := logs.GetFormatter(source.Formatter)
		go s.consumeLogs(ctx, nodeID, source, formatter, rawCh)
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
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("telemetry request failed: status %d", resp.StatusCode)
	}

	return nil
}
