package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

const (
	contentPackCollectorDefaultKind = "otel"
	contentPackCollectorStatusOK    = "healthy"
	contentPackCollectorStatusWarn  = "degraded"
	contentPackApplyStatusDeployed  = "deployed"
	contentPackApplyStatusFailed    = "failed"
)

type contentPackEdgeCollectorOptions struct {
	TenantID         string
	CollectorID      string
	Kind             string
	Version          string
	Token            string
	ConfigPath       string
	StateFile        string
	MetricsEndpoint  string
	MetricsTimeout   time.Duration
	PollInterval     time.Duration
	ApplyTimeout     time.Duration
	ValidateCommand  []string
	ReloadCommand    []string
	SuperviseCommand []string
}

type contentPackEdgeCollectorWrapper struct {
	client *api.Client
	log    *zap.Logger
	opts   contentPackEdgeCollectorOptions
	state  contentPackEdgeCollectorState
	proc   *contentPackCollectorProcess
}

type contentPackEdgeCollectorState struct {
	CandidateID          string `json:"candidate_id,omitempty"`
	DesiredConfigVersion string `json:"desired_config_version,omitempty"`
	RunningConfigVersion string `json:"running_config_version,omitempty"`
	LastApplyAt          string `json:"last_apply_at,omitempty"`
	LastError            string `json:"last_error,omitempty"`
}

type contentPackDesiredConfigResponse struct {
	TenantID      string           `json:"tenant_id"`
	CollectorID   string           `json:"collector_id"`
	CandidateID   string           `json:"candidate_id"`
	ConfigVersion string           `json:"config_version"`
	GeneratedAt   string           `json:"generated_at"`
	QueuedAt      string           `json:"queued_at,omitempty"`
	Sources       []map[string]any `json:"sources,omitempty"`
	Warnings      []string         `json:"warnings,omitempty"`
	YAML          string           `json:"yaml"`
}

func newContentPackEdgeCollectorWrapper(client *api.Client, log *zap.Logger, opts contentPackEdgeCollectorOptions) *contentPackEdgeCollectorWrapper {
	if log == nil {
		log = zap.NewNop()
	}
	opts.TenantID = strings.TrimSpace(opts.TenantID)
	opts.CollectorID = strings.TrimSpace(opts.CollectorID)
	opts.Kind = strings.TrimSpace(strings.ToLower(opts.Kind))
	if opts.Kind == "" {
		opts.Kind = contentPackCollectorDefaultKind
	}
	opts.Version = strings.TrimSpace(opts.Version)
	opts.Token = strings.TrimSpace(opts.Token)
	opts.ConfigPath = strings.TrimSpace(opts.ConfigPath)
	opts.StateFile = strings.TrimSpace(opts.StateFile)
	opts.MetricsEndpoint = strings.TrimSpace(opts.MetricsEndpoint)
	if opts.MetricsTimeout <= 0 {
		opts.MetricsTimeout = 3 * time.Second
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 30 * time.Second
	}
	if opts.ApplyTimeout <= 0 {
		opts.ApplyTimeout = 30 * time.Second
	}
	return &contentPackEdgeCollectorWrapper{
		client: client,
		log:    log.Named("content-pack-collector"),
		opts:   opts,
		proc:   newContentPackCollectorProcess(log.Named("content-pack-collector").Named("process")),
	}
}

func (w *contentPackEdgeCollectorWrapper) Validate() error {
	switch {
	case w == nil:
		return errors.New("content pack collector wrapper is nil")
	case w.client == nil:
		return errors.New("api client is required")
	case strings.TrimSpace(w.opts.TenantID) == "":
		return errors.New("tenant id is required")
	case strings.TrimSpace(w.opts.CollectorID) == "":
		return errors.New("collector id is required")
	case strings.TrimSpace(w.opts.Token) == "":
		return errors.New("collector token is required")
	case strings.TrimSpace(w.opts.ConfigPath) == "":
		return errors.New("collector config path is required")
	case strings.TrimSpace(w.opts.StateFile) == "":
		return errors.New("collector state file is required")
	default:
		return nil
	}
}

func (w *contentPackEdgeCollectorWrapper) Backend() string {
	if w == nil || strings.TrimSpace(w.opts.Kind) == "" {
		return "content-pack-collector"
	}
	return "content-pack-" + strings.TrimSpace(w.opts.Kind)
}

func (w *contentPackEdgeCollectorWrapper) Run(ctx context.Context) {
	if err := w.Validate(); err != nil {
		w.log.Warn("content pack collector wrapper disabled", zap.Error(err))
		return
	}
	if err := w.loadState(); err != nil {
		w.log.Warn("load content pack collector state", zap.Error(err))
	}
	if err := w.syncOnce(ctx); err != nil && ctx.Err() == nil {
		w.log.Debug("content pack collector sync failed", zap.Error(err))
	}

	ticker := time.NewTicker(w.opts.PollInterval)
	defer ticker.Stop()
	defer w.stopSupervisedCollector()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.syncOnce(ctx); err != nil && ctx.Err() == nil {
				w.log.Debug("content pack collector sync failed", zap.Error(err))
			}
		}
	}
}

func (w *contentPackEdgeCollectorWrapper) syncOnce(ctx context.Context) error {
	if err := w.Validate(); err != nil {
		return err
	}
	desired, err := w.fetchDesiredConfig(ctx)
	if err != nil {
		w.state.LastError = err.Error()
		_ = w.saveState()
		_ = w.sendHeartbeat(ctx)
		return err
	}
	if desired != nil {
		w.state.CandidateID = strings.TrimSpace(desired.CandidateID)
		w.state.DesiredConfigVersion = strings.TrimSpace(desired.ConfigVersion)
		if w.state.DesiredConfigVersion != "" && w.state.DesiredConfigVersion != w.state.RunningConfigVersion {
			if applyErr := w.applyAndReport(ctx, *desired); applyErr != nil {
				w.state.LastError = applyErr.Error()
				_ = w.saveState()
				_ = w.sendHeartbeat(ctx)
				return applyErr
			}
		} else if w.state.DesiredConfigVersion != "" {
			w.state.LastError = ""
		}
	}
	if err := w.ensureSupervisedCollector(ctx); err != nil {
		w.state.LastError = err.Error()
		_ = w.saveState()
		_ = w.sendHeartbeat(ctx)
		return err
	}
	if err := w.saveState(); err != nil {
		w.log.Debug("save content pack collector state", zap.Error(err))
	}
	return w.sendHeartbeat(ctx)
}

func (w *contentPackEdgeCollectorWrapper) fetchDesiredConfig(ctx context.Context) (*contentPackDesiredConfigResponse, error) {
	resp, err := w.doCollectorRequest(ctx, http.MethodGet, "desired-config", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("desired config status %d: %s", resp.StatusCode, readResponseSnippet(resp.Body))
	}
	var desired contentPackDesiredConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&desired); err != nil {
		return nil, fmt.Errorf("decode desired config: %w", err)
	}
	if strings.TrimSpace(desired.ConfigVersion) == "" {
		return nil, errors.New("desired config missing config_version")
	}
	if strings.TrimSpace(desired.YAML) == "" {
		return nil, errors.New("desired config missing yaml")
	}
	return &desired, nil
}

func (w *contentPackEdgeCollectorWrapper) applyAndReport(ctx context.Context, desired contentPackDesiredConfigResponse) error {
	err := w.applyDesiredConfig(ctx, desired)
	status := contentPackApplyStatusDeployed
	errMsg := ""
	if err != nil {
		status = contentPackApplyStatusFailed
		errMsg = err.Error()
	}
	if reportErr := w.reportApplyResult(ctx, desired.ConfigVersion, status, errMsg); reportErr != nil {
		if err != nil {
			return fmt.Errorf("%w; report apply result: %v", err, reportErr)
		}
		return reportErr
	}
	if err != nil {
		return err
	}
	w.state.RunningConfigVersion = strings.TrimSpace(desired.ConfigVersion)
	w.state.LastApplyAt = time.Now().UTC().Format(time.RFC3339Nano)
	w.state.LastError = ""
	return nil
}

func (w *contentPackEdgeCollectorWrapper) applyDesiredConfig(ctx context.Context, desired contentPackDesiredConfigResponse) error {
	if got := contentPackRenderedConfigVersion(desired.YAML); got != strings.TrimSpace(desired.ConfigVersion) {
		return fmt.Errorf("desired config version mismatch: got %s want %s", got, strings.TrimSpace(desired.ConfigVersion))
	}
	if err := os.MkdirAll(filepath.Dir(w.opts.ConfigPath), 0o750); err != nil {
		return fmt.Errorf("create collector config dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(w.opts.ConfigPath), ".control-one-otel-*.yaml")
	if err != nil {
		return fmt.Errorf("create temp collector config: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.WriteString(desired.YAML); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp collector config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp collector config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp collector config: %w", err)
	}

	validateEnv := w.commandEnv(desired, tmpPath)
	if err := runContentPackCollectorCommand(ctx, w.opts.ApplyTimeout, w.opts.ValidateCommand, validateEnv); err != nil {
		return fmt.Errorf("validate collector config: %w", err)
	}
	if err := replaceFile(tmpPath, w.opts.ConfigPath); err != nil {
		return fmt.Errorf("install collector config: %w", err)
	}
	removeTmp = false

	if len(trimCommand(w.opts.SuperviseCommand)) > 0 {
		if err := w.restartSupervisedCollector(ctx, desired); err != nil {
			return fmt.Errorf("restart collector process: %w", err)
		}
	} else {
		reloadEnv := w.commandEnv(desired, w.opts.ConfigPath)
		if err := runContentPackCollectorCommand(ctx, w.opts.ApplyTimeout, w.opts.ReloadCommand, reloadEnv); err != nil {
			return fmt.Errorf("reload collector: %w", err)
		}
	}
	return nil
}

func (w *contentPackEdgeCollectorWrapper) reportApplyResult(ctx context.Context, configVersion, status, errMsg string) error {
	body, err := json.Marshal(map[string]string{
		"config_version": strings.TrimSpace(configVersion),
		"status":         strings.TrimSpace(status),
		"error":          strings.TrimSpace(errMsg),
	})
	if err != nil {
		return err
	}
	resp, err := w.doCollectorRequest(ctx, http.MethodPost, "apply-result", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("apply-result status %d: %s", resp.StatusCode, readResponseSnippet(resp.Body))
	}
	return nil
}

func (w *contentPackEdgeCollectorWrapper) sendHeartbeat(ctx context.Context) error {
	status := contentPackCollectorStatusOK
	processHealth, processOK := w.processHealth()
	receiverHealth, receiverOK, metricsErr := w.scrapeReceiverMetrics(ctx)
	if strings.TrimSpace(w.state.LastError) != "" ||
		(strings.TrimSpace(w.state.DesiredConfigVersion) != "" &&
			strings.TrimSpace(w.state.RunningConfigVersion) != "" &&
			w.state.DesiredConfigVersion != w.state.RunningConfigVersion) ||
		!processOK ||
		!receiverOK ||
		metricsErr != nil {
		status = contentPackCollectorStatusWarn
	}
	wrapperHealth := map[string]any{
		"state":                  status,
		"config_path":            w.opts.ConfigPath,
		"state_file":             w.opts.StateFile,
		"metrics_endpoint":       w.opts.MetricsEndpoint,
		"last_apply_at":          w.state.LastApplyAt,
		"desired_config_version": w.state.DesiredConfigVersion,
		"running_config_version": w.state.RunningConfigVersion,
		"candidate_id":           w.state.CandidateID,
		"validate_command":       len(w.opts.ValidateCommand) > 0,
		"reload_command":         len(w.opts.ReloadCommand) > 0,
		"supervise_command":      len(trimCommand(w.opts.SuperviseCommand)) > 0,
		"process":                processHealth,
	}
	if metricsErr != nil {
		wrapperHealth["metrics_error"] = metricsErr.Error()
	}
	health := map[string]any{
		"wrapper": wrapperHealth,
	}
	if len(receiverHealth) > 0 {
		health["receivers"] = receiverHealth
	}
	body, err := json.Marshal(map[string]any{
		"kind":                   w.opts.Kind,
		"version":                w.opts.Version,
		"status":                 status,
		"desired_config_version": w.state.DesiredConfigVersion,
		"running_config_version": w.state.RunningConfigVersion,
		"health":                 health,
		"last_error":             w.state.LastError,
	})
	if err != nil {
		return err
	}
	resp, err := w.doCollectorRequest(ctx, http.MethodPost, "heartbeat", body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("collector heartbeat status %d: %s", resp.StatusCode, readResponseSnippet(resp.Body))
	}
	return nil
}

func (w *contentPackEdgeCollectorWrapper) doCollectorRequest(ctx context.Context, method, action string, body []byte) (*http.Response, error) {
	headers := map[string]string{
		"Authorization":                 "Bearer " + w.opts.Token,
		"X-ControlOne-Collector-Token":  w.opts.Token,
		"X-ControlOne-Collector-ID":     w.opts.CollectorID,
		"X-ControlOne-Collector-Kind":   w.opts.Kind,
		"X-ControlOne-Collector-Source": "nodeagent-wrapper",
	}
	return w.client.DoWithHeaders(ctx, method, w.collectorPath(action), body, headers)
}

func (w *contentPackEdgeCollectorWrapper) collectorPath(action string) string {
	return "/api/v1/content-packs/collectors/" +
		url.PathEscape(w.opts.CollectorID) +
		"/" +
		strings.Trim(strings.TrimSpace(action), "/") +
		"?tenant_id=" +
		url.QueryEscape(w.opts.TenantID)
}

func (w *contentPackEdgeCollectorWrapper) commandEnv(desired contentPackDesiredConfigResponse, configPath string) map[string]string {
	return map[string]string{
		"C1_COLLECTOR_CONFIG":     configPath,
		"C1_CONFIG_VERSION":       strings.TrimSpace(desired.ConfigVersion),
		"C1_CANDIDATE_ID":         strings.TrimSpace(desired.CandidateID),
		"C1_TENANT_ID":            strings.TrimSpace(w.opts.TenantID),
		"C1_COLLECTOR_ID":         strings.TrimSpace(w.opts.CollectorID),
		"C1_COLLECTOR_KIND":       strings.TrimSpace(w.opts.Kind),
		"C1_COLLECTOR_STATE_FILE": strings.TrimSpace(w.opts.StateFile),
	}
}

func (w *contentPackEdgeCollectorWrapper) ensureSupervisedCollector(ctx context.Context) error {
	if w == nil || len(trimCommand(w.opts.SuperviseCommand)) == 0 || strings.TrimSpace(w.state.RunningConfigVersion) == "" {
		return nil
	}
	desired := contentPackDesiredConfigResponse{
		CandidateID:   w.state.CandidateID,
		ConfigVersion: w.state.RunningConfigVersion,
	}
	env := w.commandEnv(desired, w.opts.ConfigPath)
	return w.proc.Ensure(ctx, w.opts.SuperviseCommand, env)
}

func (w *contentPackEdgeCollectorWrapper) restartSupervisedCollector(ctx context.Context, desired contentPackDesiredConfigResponse) error {
	if w == nil || len(trimCommand(w.opts.SuperviseCommand)) == 0 {
		return nil
	}
	env := w.commandEnv(desired, w.opts.ConfigPath)
	return w.proc.Restart(ctx, w.opts.SuperviseCommand, env)
}

func (w *contentPackEdgeCollectorWrapper) stopSupervisedCollector() {
	if w == nil || w.proc == nil {
		return
	}
	w.proc.Stop()
}

func (w *contentPackEdgeCollectorWrapper) processHealth() (map[string]any, bool) {
	if w == nil || len(trimCommand(w.opts.SuperviseCommand)) == 0 || w.proc == nil {
		return map[string]any{"managed": false}, true
	}
	snapshot := w.proc.Snapshot()
	if strings.TrimSpace(w.state.RunningConfigVersion) == "" {
		snapshot["state"] = "waiting_for_config"
		return snapshot, true
	}
	if boolFromAny(snapshot["running"]) {
		snapshot["state"] = "running"
		return snapshot, true
	}
	snapshot["state"] = "stopped"
	return snapshot, false
}

func (w *contentPackEdgeCollectorWrapper) scrapeReceiverMetrics(ctx context.Context) (map[string]any, bool, error) {
	if w == nil || strings.TrimSpace(w.opts.MetricsEndpoint) == "" {
		return nil, true, nil
	}
	timeout := w.opts.MetricsTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(callCtx, http.MethodGet, w.opts.MetricsEndpoint, nil)
	if err != nil {
		return nil, false, err
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf("metrics endpoint status %d: %s", resp.StatusCode, readResponseSnippet(resp.Body))
	}
	limited := io.LimitReader(resp.Body, 2<<20)
	return parseContentPackCollectorPrometheusMetrics(limited, time.Now().UTC())
}

type contentPackCollectorProcess struct {
	mu           sync.Mutex
	log          *zap.Logger
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	expectedStop bool
	pid          int
	startedAt    time.Time
	exitedAt     time.Time
	lastError    string
	restarts     int
}

func newContentPackCollectorProcess(log *zap.Logger) *contentPackCollectorProcess {
	if log == nil {
		log = zap.NewNop()
	}
	return &contentPackCollectorProcess{log: log}
}

func (p *contentPackCollectorProcess) Ensure(ctx context.Context, command []string, env map[string]string) error {
	if p == nil {
		return nil
	}
	command = trimCommand(command)
	if len(command) == 0 {
		return nil
	}
	p.mu.Lock()
	running := p.cmd != nil
	previouslyStarted := !p.startedAt.IsZero()
	p.mu.Unlock()
	if running {
		return nil
	}
	return p.start(ctx, command, env, previouslyStarted)
}

func (p *contentPackCollectorProcess) Restart(ctx context.Context, command []string, env map[string]string) error {
	if p == nil {
		return nil
	}
	command = trimCommand(command)
	if len(command) == 0 {
		return nil
	}
	p.mu.Lock()
	wasRunning := p.cmd != nil
	p.mu.Unlock()
	p.StopAndWait(5 * time.Second)
	return p.start(ctx, command, env, wasRunning)
}

func (p *contentPackCollectorProcess) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	cancel := p.cancel
	if p.cmd != nil {
		p.expectedStop = true
	}
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (p *contentPackCollectorProcess) StopAndWait(timeout time.Duration) {
	p.Stop()
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		p.mu.Lock()
		running := p.cmd != nil
		p.mu.Unlock()
		if !running || time.Now().After(deadline) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (p *contentPackCollectorProcess) Snapshot() map[string]any {
	if p == nil {
		return map[string]any{"managed": false}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := map[string]any{
		"managed":       true,
		"running":       p.cmd != nil,
		"pid":           p.pid,
		"restart_count": p.restarts,
		"last_error":    p.lastError,
	}
	if !p.startedAt.IsZero() {
		out["started_at"] = p.startedAt.UTC().Format(time.RFC3339Nano)
	}
	if !p.exitedAt.IsZero() {
		out["exited_at"] = p.exitedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func (p *contentPackCollectorProcess) start(ctx context.Context, command []string, env map[string]string, restart bool) error {
	expanded := expandCommand(command, env)
	if len(expanded) == 0 {
		return nil
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, expanded[0], expanded[1:]...)
	cmd.Env = append(os.Environ(), mapToEnv(env)...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		p.mu.Lock()
		p.lastError = err.Error()
		p.exitedAt = time.Now().UTC()
		p.cmd = nil
		p.cancel = nil
		p.pid = 0
		p.mu.Unlock()
		return err
	}
	p.mu.Lock()
	p.cmd = cmd
	p.cancel = cancel
	p.expectedStop = false
	p.pid = cmd.Process.Pid
	p.startedAt = time.Now().UTC()
	p.lastError = ""
	if restart {
		p.restarts++
	}
	p.mu.Unlock()
	p.log.Info("managed collector process started", zap.Int("pid", cmd.Process.Pid), zap.String("command", expanded[0]))
	go p.wait(childCtx, cmd)
	return nil
}

func (p *contentPackCollectorProcess) wait(ctx context.Context, cmd *exec.Cmd) {
	err := cmd.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != cmd {
		return
	}
	expectedStop := p.expectedStop || ctx.Err() != nil
	p.cmd = nil
	p.cancel = nil
	p.pid = 0
	p.exitedAt = time.Now().UTC()
	if expectedStop {
		p.lastError = ""
		p.expectedStop = false
		return
	}
	if err != nil {
		p.lastError = err.Error()
	} else {
		p.lastError = "collector process exited"
	}
	p.log.Warn("managed collector process exited", zap.Error(err))
}

func (w *contentPackEdgeCollectorWrapper) loadState() error {
	if strings.TrimSpace(w.opts.StateFile) == "" {
		return nil
	}
	data, err := os.ReadFile(w.opts.StateFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return json.Unmarshal(data, &w.state)
}

func (w *contentPackEdgeCollectorWrapper) saveState() error {
	if strings.TrimSpace(w.opts.StateFile) == "" {
		return nil
	}
	return writeJSONFileAtomic(w.opts.StateFile, w.state, 0o600)
}

func contentPackRenderedConfigVersion(yaml string) string {
	sum := sha256.Sum256([]byte(yaml))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func parseContentPackCollectorPrometheusMetrics(r io.Reader, now time.Time) (map[string]any, bool, error) {
	if r == nil {
		return nil, true, nil
	}
	receivers := map[string]map[string]any{}
	var globalQueueDepth int64
	var globalRetryCount int64
	var globalDropped int64
	var globalErrors int64
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		name, labels, value, ok := parsePrometheusMetricLine(scanner.Text())
		if !ok {
			continue
		}
		switch name {
		case "otelcol_receiver_accepted_log_records",
			"otelcol_receiver_accepted_metric_points",
			"otelcol_receiver_accepted_spans",
			"otelcol_receiver_accepted_log_records_total",
			"otelcol_receiver_accepted_metric_points_total",
			"otelcol_receiver_accepted_spans_total":
			receiverID := firstNonEmptyMetricLabel(labels, "receiver", "scraper")
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_received", value, now)
		case "otelcol_receiver_refused_log_records",
			"otelcol_receiver_refused_metric_points",
			"otelcol_receiver_refused_spans",
			"otelcol_receiver_refused_log_records_total",
			"otelcol_receiver_refused_metric_points_total",
			"otelcol_receiver_refused_spans_total":
			receiverID := firstNonEmptyMetricLabel(labels, "receiver", "scraper")
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_dropped", value, now)
		case "otelcol_scraper_scraped_metric_points",
			"otelcol_scraper_scraped_metric_points_total":
			receiverID := firstNonEmptyMetricLabel(labels, "receiver", "scraper")
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_received", value, now)
		case "otelcol_scraper_errored_metric_points",
			"otelcol_scraper_errored_metric_points_total":
			receiverID := firstNonEmptyMetricLabel(labels, "receiver", "scraper")
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_dropped", value, now)
		case "otelcol_exporter_queue_size", "otelcol_exporter_queue_capacity":
			if value > globalQueueDepth {
				globalQueueDepth = value
			}
		case "otelcol_exporter_send_failed_log_records",
			"otelcol_exporter_send_failed_metric_points",
			"otelcol_exporter_send_failed_spans",
			"otelcol_exporter_send_failed_log_records_total",
			"otelcol_exporter_send_failed_metric_points_total",
			"otelcol_exporter_send_failed_spans_total":
			if value > globalRetryCount {
				globalRetryCount = value
			}
		case "component_received_events_total":
			receiverID := vectorReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_received", value, now)
		case "component_sent_events_total":
			receiverID := vectorReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_parsed", value, now)
		case "component_discarded_events_total":
			receiverID := vectorReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_dropped", value, now)
		case "component_errors_total":
			receiverID := vectorReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "retry_count", value, now)
			addReceiverLastError(receivers, receiverID, "vector component errors reported", now)
		case "source_buffer_utilization_level", "source_buffer_utilization_mean":
			receiverID := vectorReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "queue_depth", value, now)
		case "fluentbit_input_records_total":
			receiverID := fluentBitInputReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "events_received", value, now)
		case "fluentbit_input_ring_buffer_retries_total",
			"fluentbit_input_ring_buffer_retry_failures_total":
			receiverID := fluentBitInputReceiverID(labels)
			if receiverID == "" {
				continue
			}
			addReceiverMetric(receivers, receiverID, "retry_count", value, now)
		case "fluentbit_output_dropped_records_total":
			globalDropped += value
		case "fluentbit_output_errors_total",
			"fluentbit_output_retries_total",
			"fluentbit_output_retries_failed_total",
			"fluentbit_output_retried_records_total":
			globalErrors += value
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, err
	}
	out := make(map[string]any, len(receivers))
	ok := true
	for receiverID, values := range receivers {
		if globalQueueDepth > 0 {
			values["queue_depth"] = globalQueueDepth
		}
		if globalRetryCount > 0 {
			values["retry_count"] = globalRetryCount
		}
		if globalDropped > 0 {
			current, _ := int64FromAny(values["events_dropped"])
			values["events_dropped"] = current + globalDropped
		}
		if globalErrors > 0 {
			current, _ := int64FromAny(values["retry_count"])
			values["retry_count"] = current + globalErrors
			if values["last_error"] == nil {
				values["last_error"] = "collector output errors or retries reported"
			}
		}
		state := contentPackReceiverMetricState(values)
		values["state"] = state
		if state == "backpressured" || state == "parser_failed" {
			ok = false
		}
		out[receiverID] = values
	}
	return out, ok, nil
}

func parsePrometheusMetricLine(line string) (string, map[string]string, int64, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil, 0, false
	}
	nameEnd := strings.IndexAny(line, "{ \t")
	if nameEnd < 0 {
		return "", nil, 0, false
	}
	name := strings.TrimSpace(line[:nameEnd])
	if name == "" || strings.HasSuffix(name, "_bucket") || strings.HasSuffix(name, "_sum") || strings.HasSuffix(name, "_count") {
		return "", nil, 0, false
	}
	rest := strings.TrimSpace(line[nameEnd:])
	labels := map[string]string{}
	if strings.HasPrefix(rest, "{") {
		end := strings.Index(rest, "}")
		if end < 0 {
			return "", nil, 0, false
		}
		labels = parsePrometheusLabels(rest[1:end])
		rest = strings.TrimSpace(rest[end+1:])
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", nil, 0, false
	}
	parsed, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return "", nil, 0, false
	}
	if parsed < 0 {
		parsed = 0
	}
	rounded := int64(parsed)
	if parsed > 0 && rounded == 0 {
		rounded = 1
	}
	return name, labels, rounded, true
}

func parsePrometheusLabels(raw string) map[string]string {
	out := map[string]string{}
	for len(raw) > 0 {
		raw = strings.TrimLeft(raw, " \t,")
		if raw == "" {
			break
		}
		eq := strings.Index(raw, "=")
		if eq <= 0 {
			break
		}
		key := strings.TrimSpace(raw[:eq])
		raw = strings.TrimLeft(raw[eq+1:], " \t")
		if !strings.HasPrefix(raw, `"`) {
			comma := strings.Index(raw, ",")
			if comma < 0 {
				out[key] = strings.TrimSpace(raw)
				break
			}
			out[key] = strings.TrimSpace(raw[:comma])
			raw = raw[comma+1:]
			continue
		}
		value, rest := parsePrometheusQuotedLabel(raw)
		if key != "" {
			out[key] = value
		}
		raw = rest
	}
	return out
}

func parsePrometheusQuotedLabel(raw string) (string, string) {
	var b strings.Builder
	escaped := false
	for i := 1; i < len(raw); i++ {
		ch := raw[i]
		if escaped {
			switch ch {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(ch)
			}
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '"':
			return b.String(), raw[i+1:]
		default:
			b.WriteByte(ch)
		}
	}
	return b.String(), ""
}

func firstNonEmptyMetricLabel(labels map[string]string, keys ...string) string {
	for _, key := range keys {
		value := strings.TrimSpace(labels[key])
		if value != "" {
			return value
		}
	}
	return ""
}

func vectorReceiverID(labels map[string]string) string {
	id := firstNonEmptyMetricLabel(labels, "component_id", "component_name", "component")
	if id == "" {
		return ""
	}
	if strings.Contains(id, "/") {
		return id
	}
	return "vector/" + id
}

func fluentBitInputReceiverID(labels map[string]string) string {
	id := firstNonEmptyMetricLabel(labels, "name", "input", "plugin")
	if id == "" {
		return ""
	}
	if strings.Contains(id, "/") {
		return id
	}
	return "fluentbit/" + id
}

func addReceiverMetric(receivers map[string]map[string]any, receiverID, key string, value int64, now time.Time) {
	receiverID = strings.TrimSpace(receiverID)
	if receiverID == "" {
		return
	}
	item := receivers[receiverID]
	if item == nil {
		item = map[string]any{
			"receiver_id":    receiverID,
			"last_health_at": now.UTC().Format(time.RFC3339Nano),
		}
		sourceID := sourceIDFromReceiverID(receiverID)
		if sourceID != "" {
			item["source_id"] = sourceID
		}
		receivers[receiverID] = item
	}
	current, _ := int64FromAny(item[key])
	item[key] = current + value
}

func addReceiverLastError(receivers map[string]map[string]any, receiverID, message string, now time.Time) {
	receiverID = strings.TrimSpace(receiverID)
	message = strings.TrimSpace(message)
	if receiverID == "" || message == "" {
		return
	}
	item := receivers[receiverID]
	if item == nil {
		addReceiverMetric(receivers, receiverID, "retry_count", 0, now)
		item = receivers[receiverID]
	}
	if item != nil {
		item["last_error"] = message
	}
}

func contentPackReceiverMetricState(values map[string]any) string {
	dropped, _ := int64FromAny(values["events_dropped"])
	queueDepth, _ := int64FromAny(values["queue_depth"])
	retries, _ := int64FromAny(values["retry_count"])
	received, _ := int64FromAny(values["events_received"])
	switch {
	case dropped > 0 || queueDepth > 0 || retries > 0:
		return "backpressured"
	case received > 0:
		return "collecting"
	default:
		return "deployed"
	}
}

func sourceIDFromReceiverID(receiverID string) string {
	receiverID = strings.TrimSpace(receiverID)
	if receiverID == "" {
		return ""
	}
	const marker = "/controlone."
	lower := strings.ToLower(receiverID)
	idx := strings.Index(lower, marker)
	if idx >= 0 {
		return strings.TrimSpace(receiverID[idx+1:])
	}
	if idx := strings.Index(receiverID, "/"); idx >= 0 && idx+1 < len(receiverID) {
		return strings.TrimSpace(receiverID[idx+1:])
	}
	return receiverID
}

func int64FromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case string:
		if parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

func runContentPackCollectorCommand(ctx context.Context, timeout time.Duration, command []string, env map[string]string) error {
	command = trimCommand(command)
	if len(command) == 0 {
		return nil
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	expanded := expandCommand(command, env)
	cmd := exec.CommandContext(callCtx, expanded[0], expanded[1:]...)
	cmd.Env = append(os.Environ(), mapToEnv(env)...)
	out, err := cmd.CombinedOutput()
	if callCtx.Err() != nil {
		return callCtx.Err()
	}
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", expanded[0], err, truncateString(strings.TrimSpace(string(out)), 2000))
	}
	return nil
}

func trimCommand(command []string) []string {
	out := make([]string, 0, len(command))
	for _, part := range command {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func expandCommand(command []string, env map[string]string) []string {
	out := make([]string, len(command))
	for i, part := range command {
		out[i] = os.Expand(part, func(key string) string {
			return env[key]
		})
	}
	return out
}

func mapToEnv(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for k, v := range values {
		out = append(out, k+"="+v)
	}
	return out
}

func boolFromAny(value any) bool {
	got, _ := value.(bool)
	return got
}

func writeJSONFileAtomic(path string, value any, perm os.FileMode) error {
	if perm == 0 {
		perm = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".control-one-state-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()
	enc := json.NewEncoder(tmp)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmpPath, path); err != nil {
		return err
	}
	removeTmp = false
	return nil
}

func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}

func readResponseSnippet(r io.Reader) string {
	if r == nil {
		return ""
	}
	data, _ := io.ReadAll(io.LimitReader(r, 512))
	return strings.TrimSpace(string(data))
}

func truncateString(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
