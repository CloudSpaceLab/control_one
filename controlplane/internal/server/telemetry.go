package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
	"github.com/CloudSpaceLab/control_one/internal/metrics"
)

type telemetryMetricResponse struct {
	ID          string            `json:"id"`
	TenantID    *string           `json:"tenant_id,omitempty"`
	NodeID      *string           `json:"node_id,omitempty"`
	MetricName  string            `json:"metric_name"`
	MetricValue float64           `json:"metric_value"`
	MetricUnit  *string           `json:"metric_unit,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Timestamp   string            `json:"timestamp"`
	CreatedAt   string            `json:"created_at"`
}

type telemetryLogResponse struct {
	ID         string            `json:"id"`
	TenantID   *string           `json:"tenant_id,omitempty"`
	NodeID     *string           `json:"node_id,omitempty"`
	LogLevel   string            `json:"log_level"`
	LogMessage string            `json:"log_message"`
	LogSource  *string           `json:"log_source,omitempty"`
	LogProgram *string           `json:"log_program,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Timestamp  string            `json:"timestamp"`
	CreatedAt  string            `json:"created_at"`
}

// agentMetricsIngestRequest is the body shape posted by the agent's
// telemetry.SendMetrics every metricsInterval (default 60s):
//
//	{"node_id": "...", "timestamp": "RFC3339Nano", "metrics": {"cpu_usage_percent": 12.3, ...}}
//
// Labelled per-device samples can ride alongside aggregate metrics:
//
//	{"metric_samples":[{"name":"smart.reallocated_sector_count","value":2,"labels":{"device":"/dev/sda"}}]}
//
// Each metric becomes one row in telemetry_metrics. Tenant + node_id are
// resolved from the mTLS agent principal — the body's node_id is informational.
type agentMetricsIngestRequest struct {
	NodeID        string                     `json:"node_id"`
	Timestamp     string                     `json:"timestamp"`
	Metrics       map[string]any             `json:"metrics"`
	MetricSamples []agentMetricSampleRequest `json:"metric_samples,omitempty"`
}

type agentMetricSampleRequest struct {
	Name   string            `json:"name"`
	Value  any               `json:"value"`
	Labels map[string]string `json:"labels,omitempty"`
}

type agentLogIngestRequest struct {
	NodeID        string            `json:"node_id"`
	Program       string            `json:"program"`
	CollectorType string            `json:"collector_type"`
	Count         int               `json:"count"`
	Labels        map[string]string `json:"labels,omitempty"`
	Paths         []string          `json:"paths,omitempty"`
	JournalUnits  []string          `json:"journal_units,omitempty"`
	EventChannels []string          `json:"event_channels,omitempty"`
	ReplayKey     string            `json:"replay_key,omitempty"`
	Entries       []agentLogEntry   `json:"entries"`
}

type agentIngestReplayReceiptStore interface {
	GetAgentIngestReplayReceipt(context.Context, uuid.UUID, uuid.UUID, string, string) (*storage.AgentIngestReplayReceipt, error)
	UpsertAgentIngestReplayReceipt(context.Context, storage.UpsertAgentIngestReplayReceiptParams) error
}

type agentLogEntry struct {
	Timestamp        string            `json:"timestamp"`
	Program          string            `json:"program"`
	Message          string            `json:"message"`
	Severity         string            `json:"severity"`
	OriginalSeverity string            `json:"original_severity,omitempty"`
	Source           string            `json:"source,omitempty"`
	Hostname         string            `json:"hostname,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Fields           map[string]any    `json:"fields,omitempty"`
}

// metricUnits maps the well-known agent metric names emitted by
// internal/util/sysinfo.go (CollectHostMetrics) to a unit string. Anything
// unmapped is stored with no unit — the read endpoint just round-trips it.
//
// Both the names and the units are derived from the single source of truth
// at controlplane/internal/metrics. Adding or renaming a metric MUST happen
// in that package; TestMetricNamesContract pins agent + server together.
var metricUnits = func() map[string]string {
	m := make(map[string]string, len(metrics.CoreEmitted))
	for _, name := range metrics.CoreEmitted {
		m[name] = metrics.Units(name)
	}
	return m
}()

// handleTelemetryIngest accepts agent-emitted host metrics (CPU, memory,
// disk, load averages) and persists them into telemetry_metrics.
//
//	POST /api/v1/telemetry
//
// Auth: agent mTLS principal required (same shape as /api/v1/events/ingest).
func (s *Server) handleTelemetryIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil || principal.Type != "agent" {
		http.Error(w, "agent principal required", http.StatusForbidden)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	tenantID, nodeID, err := s.tenantNodeForAgent(r.Context(), principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // 64 KiB is plenty for one host's gauges
	defer func() { _ = r.Body.Close() }()

	var body agentMetricsIngestRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	ts := time.Now().UTC()
	if strings.TrimSpace(body.Timestamp) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, body.Timestamp); err == nil {
			ts = parsed
		} else if parsed, err := time.Parse(time.RFC3339, body.Timestamp); err == nil {
			ts = parsed
		}
	}

	rows := make([]storage.CreateTelemetryMetricParams, 0, len(body.Metrics)+len(body.MetricSamples))
	for name, raw := range body.Metrics {
		if row, ok := newAgentMetricRow(tenantID, nodeID, name, raw, nil, ts); ok {
			rows = append(rows, row)
		}
	}
	for _, sample := range body.MetricSamples {
		if row, ok := newAgentMetricRow(tenantID, nodeID, sample.Name, sample.Value, sample.Labels, ts); ok {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if err := s.store.CreateTelemetryMetrics(r.Context(), rows); err != nil {
		s.logger.Error("ingest telemetry metrics",
			zap.Error(err),
			zap.String("node_id", nodeID.String()),
			zap.Int("rows", len(rows)),
		)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// handleAgentLivenessHeartbeat is a 200-noop on POST /api/v1/heartbeat. The
// agent's telemetry layer (internal/telemetry/telemetry.go SendHeartbeat)
// posts here every metricsInterval as a redundant liveness ping; the real
// liveness signal comes from POST /api/v1/nodes/{id}/heartbeat. Without
// this stub the redundant call generates a 404-per-minute log line per
// node. Method-only check; auth is handled by the wrap-around middleware.
func (s *Server) handleAgentLivenessHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLogIngest accepts the agent's structured log batch payload on
// POST /api/v1/logs. It persists queryable telemetry_logs and derives
// normalized log.line/web.request events for the unified event pipeline.
func (s *Server) handleLogIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil || principal.Type != "agent" {
		http.Error(w, "agent principal required", http.StatusForbidden)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, nodeID, err := s.tenantNodeForAgent(r.Context(), principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	defer func() { _ = r.Body.Close() }()
	var body agentLogIngestRequest
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	replayKey, err := sanitizeReplayKey(body.ReplayKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if replayKey != "" {
		if receiptStore, ok := s.store.(agentIngestReplayReceiptStore); ok {
			receipt, err := receiptStore.GetAgentIngestReplayReceipt(r.Context(), tenantID, nodeID, "logs", replayKey)
			if err != nil {
				s.logger.Warn("lookup log ingest replay receipt", zap.Error(err), zap.String("replay_key", replayKey))
			} else if receipt != nil && strings.EqualFold(receipt.Status, "accepted") {
				writeAgentIngestReplayReceipt(w, receipt)
				return
			}
		}
	}
	if len(body.Entries) == 0 {
		http.Error(w, "entries required", http.StatusBadRequest)
		return
	}
	if len(body.Entries) > 5000 {
		http.Error(w, "batch exceeds 5000 log entries", http.StatusRequestEntityTooLarge)
		return
	}

	ipCache := map[string]map[string]any{}
	trustedProxyCIDRs := s.tenantTrustedProxyCIDRs(r.Context(), tenantID)
	logRows := make([]storage.CreateTelemetryLogParams, 0, len(body.Entries))
	events := make([]IngestedEvent, 0, len(body.Entries)*2)
	for i := range body.Entries {
		entry := &body.Entries[i]
		ts := parseLogTimestamp(entry.Timestamp)
		program := firstNonEmpty(entry.Program, body.Program, "generic")
		labels := mergeStringLabels(body.Labels, entry.Labels)
		source := firstNonEmpty(entry.Source, firstString(body.Paths), firstString(body.JournalUnits), firstString(body.EventChannels), body.CollectorType)
		level := firstNonEmpty(entry.Severity, "info")
		logRows = append(logRows, storage.CreateTelemetryLogParams{
			TenantID:   tenantID,
			NodeID:     nodeID,
			LogLevel:   level,
			LogMessage: entry.Message,
			LogSource:  stringPtrOrNil(source),
			LogProgram: stringPtrOrNil(program),
			Labels:     labels,
			Timestamp:  ts,
		})

		details := map[string]any{
			"program":        program,
			"collector_type": body.CollectorType,
			"source":         source,
			"hostname":       entry.Hostname,
			"labels":         labels,
			"fields":         entry.Fields,
		}
		if entry.OriginalSeverity != "" {
			details["original_severity"] = entry.OriginalSeverity
		}
		correlationID := fmt.Sprintf("log:%s:%d:%s", nodeID, ts.UnixNano(), hashString(entry.Message))
		logLine := IngestedEvent{
			Type:          "log.line",
			TS:            ts,
			NodeID:        nodeID.String(),
			TenantID:      tenantID.String(),
			Severity:      level,
			CorrelationID: correlationID,
			Message:       entry.Message,
			Details:       details,
			DedupKey:      fmt.Sprintf("log.line:%s:%s:%d", nodeID, program, ts.UnixNano()),
		}
		if ip := firstNonEmpty(fieldString(entry.Fields, "remote_ip"), fieldString(entry.Fields, "client"), fieldString(entry.Fields, "src_ip")); net.ParseIP(ip) != nil {
			logLine.SrcIP = ip
		}
		events = append(events, logLine)
		if webEvent, ok := s.webRequestFromLog(r.Context(), tenantID, nodeID, program, source, labels, entry, ipCache, trustedProxyCIDRs); ok {
			events = append(events, webEvent)
		}
	}

	ingest := s.eventIngestService()
	eventBatch, err := ingest.recordLogDerivedBatch(r.Context(), tenantID, nodeID, events, replayKey)
	if err != nil {
		s.logger.Error("journal log-derived events", zap.Error(err), zap.Int("events", len(events)))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if err := s.store.CreateTelemetryLogs(r.Context(), logRows); err != nil {
		s.logger.Error("ingest telemetry logs", zap.Error(err), zap.Int("rows", len(logRows)))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.persistContentPackSourceRuntimeStateFromAgentLogs(r.Context(), tenantID, nodeID, body)
	if !eventBatch.Duplicate {
		eventBatch.DorisStatus, eventBatch.Status, err = ingest.complete(r.Context(), eventBatch.ID, tenantID, nodeID, events)
		if err != nil {
			s.logger.Warn("fanout log-derived events", zap.Error(err), zap.String("doris_status", eventBatch.DorisStatus))
		}
	}
	resp := map[string]any{
		"rows":                len(logRows),
		"events":              len(events),
		"doris_status":        eventBatch.DorisStatus,
		"event_ingest_status": eventBatch.Status,
	}
	if eventBatch.ID != uuid.Nil {
		resp["event_batch_id"] = eventBatch.ID.String()
	}
	if replayKey != "" {
		resp["replay_key"] = replayKey
		if receiptStore, ok := s.store.(agentIngestReplayReceiptStore); ok {
			raw, _ := json.Marshal(resp)
			if err := receiptStore.UpsertAgentIngestReplayReceipt(r.Context(), storage.UpsertAgentIngestReplayReceiptParams{
				TenantID:  tenantID,
				NodeID:    nodeID,
				Endpoint:  "logs",
				ReplayKey: replayKey,
				Status:    "accepted",
				Response:  raw,
			}); err != nil {
				s.logger.Warn("record log ingest replay receipt", zap.Error(err), zap.String("replay_key", replayKey))
			}
		}
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func writeAgentIngestReplayReceipt(w http.ResponseWriter, receipt *storage.AgentIngestReplayReceipt) {
	resp := map[string]any{}
	if receipt != nil && len(receipt.Response) > 0 {
		_ = json.Unmarshal(receipt.Response, &resp)
	}
	if resp == nil {
		resp = map[string]any{}
	}
	resp["duplicate"] = true
	if receipt != nil && strings.TrimSpace(receipt.ReplayKey) != "" {
		resp["replay_key"] = strings.TrimSpace(receipt.ReplayKey)
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func logDerivedEventReplayKey(replayKey string) string {
	replayKey = strings.TrimSpace(replayKey)
	if replayKey == "" {
		return ""
	}
	return "logs-events:" + hashString(replayKey)
}

func newAgentMetricRow(tenantID, nodeID uuid.UUID, name string, raw any, labels map[string]string, ts time.Time) (storage.CreateTelemetryMetricParams, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return storage.CreateTelemetryMetricParams{}, false
	}
	val, ok := metricToFloat(raw)
	if !ok {
		// Skip values we can't coerce; the agent might add string-typed
		// metrics later and we don't want one bad row to drop the batch.
		return storage.CreateTelemetryMetricParams{}, false
	}
	row := storage.CreateTelemetryMetricParams{
		TenantID:    tenantID,
		NodeID:      nodeID,
		MetricName:  name,
		MetricValue: val,
		Labels:      sanitizeMetricLabels(labels),
		Timestamp:   ts,
	}
	if unit, known := metricUnits[name]; known && unit != "" {
		u := unit
		row.MetricUnit = &u
	}
	return row, true
}

func sanitizeMetricLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// metricToFloat coerces a JSON-decoded value (already float64 from
// encoding/json) into a float64. Bools coerce to 0/1 for forward compat.
func metricToFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

func (s *Server) persistContentPackSourceRuntimeStateFromAgentLogs(ctx context.Context, tenantID, nodeID uuid.UUID, body agentLogIngestRequest) {
	if s == nil || s.store == nil || tenantID == uuid.Nil || nodeID == uuid.Nil || len(body.Entries) == 0 {
		return
	}
	store, ok := s.store.(contentPackSourceRuntimeStateStore)
	if !ok || store == nil {
		return
	}

	labels := mergeStringLabels(body.Labels, nil)
	for _, entry := range body.Entries {
		labels = mergeStringLabels(labels, entry.Labels)
	}
	logSource := firstNonEmpty(firstString(body.Paths), firstString(body.JournalUnits), firstString(body.EventChannels), body.CollectorType)
	entryProgram := ""
	for _, entry := range body.Entries {
		if entryProgram = strings.TrimSpace(entry.Program); entryProgram != "" {
			break
		}
	}
	sourceID := strings.TrimSpace(firstNonEmpty(
		labels["control_one.content_pack_source_id"],
		labels["content_pack_source_id"],
		labels["source_id"],
		labels["parser_profile"],
		body.Program,
		entryProgram,
		logSource,
		body.CollectorType,
	))
	if sourceID == "" {
		return
	}

	now := time.Now().UTC()
	latestEventAt := time.Time{}
	parsedCount := int64(0)
	for _, entry := range body.Entries {
		ts := parseLogTimestamp(entry.Timestamp)
		if latestEventAt.IsZero() || ts.After(latestEventAt) {
			latestEventAt = ts
		}
		if len(entry.Fields) > 0 || strings.EqualFold(labels["control_one.collect_mode"], "collect_parsed") {
			parsedCount++
		}
	}
	if latestEventAt.IsZero() {
		latestEventAt = now
	}
	var lastParsedAt *time.Time
	if parsedCount > 0 {
		t := latestEventAt
		lastParsedAt = &t
	}
	lastHealthAt := now
	stateLabels := map[string]string{
		"program":                           strings.TrimSpace(body.Program),
		"collector_type":                    strings.TrimSpace(body.CollectorType),
		"source":                            strings.TrimSpace(logSource),
		"collect_mode":                      strings.TrimSpace(labels["control_one.collect_mode"]),
		"control_one.metrics_semantics":     "delta",
		contentPackCollectionOwnerLabel:     contentPackCollectionOwnerNodeAgent,
		contentPackCollectionIdentityLabel:  contentPackAgentLogCollectionIdentity(nodeID, sourceID, labels),
		"control_one.collection_log_source": strings.TrimSpace(logSource),
	}
	for _, key := range []string{
		"control_one.source_proposal_id",
		"control_one.source_proposal_external_id",
		"control_one.content_pack_source_id",
		"control_one.collect_mode",
		"control_one.raw_message_retained",
	} {
		if value := strings.TrimSpace(labels[key]); value != "" {
			stateLabels[key] = value
		}
	}
	state := contentpacks.SourceRuntimeState{
		SourceInstanceID: contentPackProposalSourceInstanceID(nodeID, sourceID),
		SourceID:         sourceID,
		DisplayName:      strings.TrimSpace(firstNonEmpty(body.Program, entryProgram, sourceID)),
		NodeID:           nodeID.String(),
		CollectorID:      nodeID.String(),
		CollectorMode:    contentpacks.CollectorNodeFileLog,
		ParserID:         sourceID,
		CoverageState:    contentpacks.CoverageState(contentpacks.CoverageCollecting),
		ApprovalRequired: strings.TrimSpace(labels["control_one.source_proposal_id"]) != "",
		ApprovalID:       strings.TrimSpace(labels["control_one.source_proposal_id"]),
		LastEventAt:      &latestEventAt,
		LastParsedAt:     lastParsedAt,
		LastHealthAt:     &lastHealthAt,
		Metrics: contentpacks.SourceRuntimeMetrics{
			EventsReceived: int64(len(body.Entries)),
			EventsParsed:   parsedCount,
		},
		Labels:    stateLabels,
		UpdatedAt: now,
	}
	if _, err := store.UpsertContentPackSourceRuntimeState(ctx, storage.UpsertContentPackSourceRuntimeStateParams{
		TenantID: tenantID,
		State:    state,
	}); err != nil {
		s.logger.Warn("persist source runtime state from agent log ingest",
			zap.Error(err),
			zap.String("source_id", sourceID),
			zap.String("node_id", nodeID.String()),
		)
	}
}

func (s *Server) handleTelemetryMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	filter := storage.TelemetryMetricFilter{TenantID: tenantID}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if _, err := s.ensureNodeInTenant(r.Context(), tenantID, parsed); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		filter.NodeID = parsed
	}

	if metricParam := strings.TrimSpace(r.URL.Query().Get("metric_name")); metricParam != "" {
		filter.MetricName = metricParam
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	metrics, total, err := s.store.ListTelemetryMetrics(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list telemetry metrics", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]telemetryMetricResponse, 0, len(metrics))
	for _, metric := range metrics {
		respItems = append(respItems, newTelemetryMetricResponse(metric))
	}

	resp := paginatedResponse[telemetryMetricResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTelemetryLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tenantID, ok := s.requireTenantAccessFromQuery(w, r, principal, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	filter := storage.TelemetryLogFilter{TenantID: tenantID}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		if _, err := s.ensureNodeInTenant(r.Context(), tenantID, parsed); err != nil {
			http.Error(w, err.Error(), http.StatusForbidden)
			return
		}
		filter.NodeID = parsed
	}

	if levelParam := strings.TrimSpace(r.URL.Query().Get("log_level")); levelParam != "" {
		filter.LogLevel = levelParam
	}

	if sourceParam := strings.TrimSpace(r.URL.Query().Get("log_source")); sourceParam != "" {
		filter.LogSource = sourceParam
	}

	if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
		ts, err := time.Parse(time.RFC3339, sinceParam)
		if err != nil {
			http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Since = &ts
	}

	if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
		ts, err := time.Parse(time.RFC3339, untilParam)
		if err != nil {
			http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
			return
		}
		filter.Until = &ts
	}

	if q := strings.TrimSpace(r.URL.Query().Get("search")); q != "" {
		filter.Search = q
	}
	if q := strings.TrimSpace(r.URL.Query().Get("regex")); q != "" {
		filter.Regex = q
	}

	logs, total, err := s.store.ListTelemetryLogs(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list telemetry logs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]telemetryLogResponse, 0, len(logs))
	for _, log := range logs {
		respItems = append(respItems, newTelemetryLogResponse(log))
	}

	resp := paginatedResponse[telemetryLogResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleTelemetryNodeSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/telemetry/nodes/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) < 2 {
		http.NotFound(w, r)
		return
	}

	nodeID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get telemetry node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}
	if !s.requireTenantAccess(w, r, principal, node.TenantID, roleViewer, roleOperator, roleAdmin) {
		return
	}

	if segments[1] == "metrics" {
		filter := storage.TelemetryMetricFilter{TenantID: node.TenantID, NodeID: nodeID}

		if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
			parsed, err := uuid.Parse(tenantParam)
			if err != nil {
				http.Error(w, "invalid tenant_id", http.StatusBadRequest)
				return
			}
			if parsed != node.TenantID {
				http.Error(w, "node is outside requested tenant", http.StatusForbidden)
				return
			}
		}

		if metricParam := strings.TrimSpace(r.URL.Query().Get("metric_name")); metricParam != "" {
			filter.MetricName = metricParam
		}

		if sinceParam := strings.TrimSpace(r.URL.Query().Get("since")); sinceParam != "" {
			ts, err := time.Parse(time.RFC3339, sinceParam)
			if err != nil {
				http.Error(w, "invalid since timestamp (use RFC3339)", http.StatusBadRequest)
				return
			}
			filter.Since = &ts
		}

		if untilParam := strings.TrimSpace(r.URL.Query().Get("until")); untilParam != "" {
			ts, err := time.Parse(time.RFC3339, untilParam)
			if err != nil {
				http.Error(w, "invalid until timestamp (use RFC3339)", http.StatusBadRequest)
				return
			}
			filter.Until = &ts
		}

		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		metrics, total, err := s.store.ListTelemetryMetrics(r.Context(), filter, limit, offset)
		if err != nil {
			s.logger.Error("list node telemetry metrics", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}

		respItems := make([]telemetryMetricResponse, 0, len(metrics))
		for _, metric := range metrics {
			respItems = append(respItems, newTelemetryMetricResponse(metric))
		}

		resp := paginatedResponse[telemetryMetricResponse]{
			Data:       respItems,
			Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	http.NotFound(w, r)
}

// handleLegacyHeartbeat accepts POST /api/v1/heartbeat from agent telemetry
// service. The payload is acknowledged but not persisted — node liveness is
// tracked via POST /api/v1/nodes/:id/heartbeat which drives the state machine.
func newTelemetryMetricResponse(m storage.TelemetryMetric) telemetryMetricResponse {
	resp := telemetryMetricResponse{
		ID:          m.ID.String(),
		MetricName:  m.MetricName,
		MetricValue: m.MetricValue,
		Labels:      m.Labels,
		Timestamp:   formatTime(m.Timestamp),
		CreatedAt:   formatTime(m.CreatedAt),
	}
	if m.TenantID != uuid.Nil {
		tid := m.TenantID.String()
		resp.TenantID = &tid
	}
	if m.NodeID != uuid.Nil {
		nid := m.NodeID.String()
		resp.NodeID = &nid
	}
	if m.MetricUnit.Valid {
		unit := m.MetricUnit.String
		resp.MetricUnit = &unit
	}
	if resp.Labels == nil {
		resp.Labels = make(map[string]string)
	}
	return resp
}

func newTelemetryLogResponse(l storage.TelemetryLog) telemetryLogResponse {
	resp := telemetryLogResponse{
		ID:         l.ID.String(),
		LogLevel:   l.LogLevel,
		LogMessage: l.LogMessage,
		Labels:     l.Labels,
		Timestamp:  formatTime(l.Timestamp),
		CreatedAt:  formatTime(l.CreatedAt),
	}
	if l.TenantID != uuid.Nil {
		tid := l.TenantID.String()
		resp.TenantID = &tid
	}
	if l.NodeID != uuid.Nil {
		nid := l.NodeID.String()
		resp.NodeID = &nid
	}
	if l.LogSource.Valid {
		source := l.LogSource.String
		resp.LogSource = &source
	}
	if l.LogProgram.Valid {
		program := l.LogProgram.String
		resp.LogProgram = &program
	}
	if resp.Labels == nil {
		resp.Labels = make(map[string]string)
	}
	return resp
}

func (s *Server) webRequestFromLog(ctx context.Context, tenantID, nodeID uuid.UUID, program, source string, labels map[string]string, entry *agentLogEntry, ipCache map[string]map[string]any, trustedProxies []string) (IngestedEvent, bool) {
	fields := entry.Fields
	if fields == nil {
		return IngestedEvent{}, false
	}
	xffChain := firstNonEmpty(fieldString(fields, "xff_chain"), fieldString(fields, "x_forwarded_for"), fieldString(fields, "xff"))
	clientDecision, ok := resolveWebRequestClientIP(
		firstNonEmpty(fieldString(fields, "remote_ip"), fieldString(fields, "client"), fieldString(fields, "src_ip")),
		fieldString(fields, "real_client_ip"),
		xffChain,
		trustedProxies,
	)
	if !ok {
		return IngestedEvent{}, false
	}
	remoteIP := clientDecision.ClientIP
	status := fieldInt(fields, "status")
	if status == 0 {
		status = fieldInt(fields, "status_code")
	}
	if status == 0 {
		return IngestedEvent{}, false
	}
	request := fieldString(fields, "request")
	method, path := splitHTTPReq(request)
	if method == "" {
		method = fieldString(fields, "method")
	}
	if path == "" {
		path = firstNonEmpty(fieldString(fields, "path"), fieldString(fields, "uri"), fieldString(fields, "request_uri"))
	}
	bytesOut := fieldInt64(fields, "bytes")
	if bytesOut == 0 {
		bytesOut = fieldInt64(fields, "body_bytes_sent")
	}
	durationMS := durationMillis(fields)
	refHost := referrerHost(fieldString(fields, "referrer"))
	ua := fieldString(fields, "user_agent")
	if ua == "" {
		ua = fieldString(fields, "agent")
	}
	pathTemplate := pathTemplate(path)
	details := map[string]any{
		"webserver_kind":   strings.ToLower(program),
		"parser_profile":   strings.ToLower(program),
		"source_file":      source,
		"socket_ip":        clientDecision.SocketIP,
		"client_ip_source": clientDecision.Source,
		"method":           method,
		"path":             path,
		"path_template":    pathTemplate,
		"path_hash":        hashString(path),
		"status_code":      status,
		"status_family":    fmt.Sprintf("%dxx", status/100),
		"bytes_out":        bytesOut,
		"request":          request,
		"referrer_host":    refHost,
		"user_agent_hash":  hashString(ua),
		"trusted_proxy":    clientDecision.TrustedProxy,
		"labels":           labels,
	}
	if clientDecision.RejectedXFF {
		details["xff_spoof_rejected"] = true
	}
	if len(trustedProxies) > 0 {
		details["trusted_proxy_cidrs_configured"] = len(trustedProxies)
	}
	for _, key := range []string{"server_group", "app", "vhost", "environment", "criticality"} {
		if v := strings.TrimSpace(labels[key]); v != "" {
			details[key] = v
		}
	}
	if clientDecision.XFFChain != "" {
		details["xff_chain"] = clientDecision.XFFChain
	}
	if upstream := fieldString(fields, "upstream_status"); upstream != "" {
		details["upstream_status"] = upstream
	}
	copyFirstWebRequestField(details, fields, "request_id", "request_id", "x_request_id", "http_x_request_id")
	copyFirstWebRequestField(details, fields, "correlation_id", "correlation_id", "x_correlation_id", "http_x_correlation_id")
	copyFirstWebRequestField(details, fields, "response_request_id", "response_request_id", "sent_http_x_request_id", "res_x_request_id", "upstream_http_x_request_id")
	copyFirstWebRequestField(details, fields, "response_correlation_id", "response_correlation_id", "sent_http_x_correlation_id", "res_x_correlation_id")
	copyFirstWebRequestField(details, fields, "traceparent", "traceparent", "http_traceparent")
	copyFirstWebRequestField(details, fields, "upstream_response_time", "upstream_response_time")
	copyFirstWebRequestField(details, fields, "proxy_host", "proxy_host")
	copyFirstWebRequestField(details, fields, "frontend", "frontend", "haproxy_frontend")
	copyFirstWebRequestField(details, fields, "backend", "backend", "haproxy_backend")
	copyFirstWebRequestField(details, fields, "upstream_server", "server", "upstream_server")
	copyFirstWebRequestField(details, fields, "termination_state", "termination_state")
	copyFirstWebRequestField(details, fields, "captured_request_headers", "captured_request_headers")
	copyFirstWebRequestField(details, fields, "captured_response_headers", "captured_response_headers")
	copyFirstWebRequestField(details, fields, "tls_sni", "tls_sni", "sni")
	if bytesIn := firstPositiveFieldInt64(fields, "request_bytes", "bytes_in", "body_bytes_received"); bytesIn > 0 {
		details["bytes_in"] = bytesIn
	}
	if port := firstPositiveFieldInt64(fields, "local_port", "server_port", "remote_port"); port > 0 {
		details["port"] = port
	}
	enrich := s.lookupIPBehaviorEnrichment(ctx, tenantID, remoteIP, ipCache)
	for k, v := range enrich {
		details[k] = v
	}
	threatScore := 0
	if v, ok := enrich["reputation_score"].(int); ok {
		threatScore = v
	}
	threatFeed := firstThreatFeedName(enrich["threat_feeds"])
	ts := parseLogTimestamp(entry.Timestamp)
	return IngestedEvent{
		Type:          "web.request",
		TS:            ts,
		NodeID:        nodeID.String(),
		TenantID:      tenantID.String(),
		Severity:      severityFromHTTPStatusCode(status),
		CorrelationID: fmt.Sprintf("log:%s:%d:%s", nodeID, ts.UnixNano(), hashString(entry.Message)),
		ProcessName:   strings.ToLower(program),
		SrcIP:         remoteIP,
		BytesOut:      bytesOut,
		DurationMS:    durationMS,
		ThreatFeed:    threatFeed,
		ThreatScore:   threatScore,
		Message:       fmt.Sprintf("%d %s %s", status, method, path),
		Details:       details,
		DedupKey:      fmt.Sprintf("web.request:%s:%s:%d:%s", nodeID, remoteIP, ts.UnixNano(), hashString(entry.Message)),
	}, true
}

type webRequestClientIPDecision struct {
	ClientIP     string
	SocketIP     string
	XFFChain     string
	Source       string
	TrustedProxy bool
	RejectedXFF  bool
}

func (s *Server) tenantTrustedProxyCIDRs(ctx context.Context, tenantID uuid.UUID) []string {
	if s == nil || s.store == nil || tenantID == uuid.Nil {
		return nil
	}
	filters, err := s.store.GetTenantEventFilters(ctx, tenantID)
	if err != nil || filters == nil {
		return nil
	}
	return filters.TrustedProxyCIDRs
}

func resolveWebRequestClientIP(socketIP, realClientIP, xffChain string, trustedProxyCIDRs []string) (webRequestClientIPDecision, bool) {
	socket := cleanHeaderIP(socketIP)
	realClient := cleanHeaderIP(realClientIP)
	source := "socket"
	if socket == "" {
		socket = realClient
		source = "real_client_ip_fallback"
	}
	if socket == "" || net.ParseIP(socket) == nil {
		return webRequestClientIPDecision{}, false
	}
	decision := webRequestClientIPDecision{
		ClientIP: socket,
		SocketIP: socket,
		XFFChain: strings.TrimSpace(xffChain),
		Source:   source,
	}
	xffIPs := validHeaderIPChain(xffChain)
	trustedSocket := ipInCIDRList(socket, trustedProxyCIDRs)
	if trustedSocket {
		if chosen := clientIPFromTrustedProxyChain(socket, xffIPs, trustedProxyCIDRs); chosen != "" {
			decision.ClientIP = chosen
			decision.Source = "xff_chain"
			decision.TrustedProxy = true
			return decision, true
		}
		if realClient != "" && realClient != socket {
			decision.ClientIP = realClient
			decision.Source = "real_client_ip"
			decision.TrustedProxy = true
			return decision, true
		}
	}
	if len(xffIPs) > 0 || (realClient != "" && realClient != socket && source != "real_client_ip_fallback") {
		decision.RejectedXFF = true
	}
	return decision, true
}

func clientIPFromTrustedProxyChain(socket string, xffIPs []string, trustedProxyCIDRs []string) string {
	if len(xffIPs) == 0 {
		return ""
	}
	// Walk right-to-left through the proxy chain, trusting only the configured
	// suffix. This avoids accepting user-supplied leftmost spoof values.
	for i := len(xffIPs) - 1; i >= 0; i-- {
		if !ipInCIDRList(xffIPs[i], trustedProxyCIDRs) {
			return xffIPs[i]
		}
	}
	if cleanHeaderIP(socket) != "" && !ipInCIDRList(socket, trustedProxyCIDRs) {
		return cleanHeaderIP(socket)
	}
	return xffIPs[0]
}

func validHeaderIPChain(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if ip := cleanHeaderIP(part); ip != "" {
			out = append(out, ip)
		}
	}
	return out
}

func cleanHeaderIP(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "unknown") {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(raw), "for=") {
		raw = strings.TrimSpace(raw[4:])
	}
	raw = strings.Trim(raw, `"' `)
	if ip := net.ParseIP(strings.Trim(raw, "[]")); ip != nil {
		return ip.String()
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		if ip := net.ParseIP(strings.Trim(host, "[]")); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func ipInCIDRList(ipValue string, cidrs []string) bool {
	ip := net.ParseIP(strings.TrimSpace(ipValue))
	if ip == nil {
		return false
	}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if candidate := net.ParseIP(raw); candidate != nil && candidate.Equal(ip) {
			return true
		}
		_, network, err := net.ParseCIDR(raw)
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func (s *Server) lookupIPBehaviorEnrichment(ctx context.Context, tenantID uuid.UUID, ip string, cache map[string]map[string]any) map[string]any {
	if cache == nil {
		cache = map[string]map[string]any{}
	}
	if cached, ok := cache[ip]; ok {
		return cached
	}
	out := map[string]any{}
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsPrivate() || parsed.IsLoopback() || parsed.IsLinkLocalUnicast() {
		cache[ip] = out
		return out
	}
	if root := s.offlineContentRootDir(); root != "" {
		if enriched, ok, err := offlinebundle.LookupIP(root, ip); err == nil && ok && enriched != nil {
			out["country"] = enriched.Country
			out["country_code"] = enriched.CountryCode
			out["region"] = enriched.Region
			out["asn"] = enriched.ASN
			out["isp"] = enriched.ISP
			out["usage_type"] = enriched.UsageType
			out["is_tor"] = enriched.IsTor
			out["reputation_score"] = enriched.ReputationScore
			out["content_source"] = enriched.Source
			out["content_bundle_id"] = enriched.BundleID
			out["content_bundle_version"] = enriched.BundleVersion
			out["content_version"] = enriched.ContentVersion
			out["content_stale"] = enriched.Stale
			if len(enriched.ThreatFeeds) > 0 {
				out["threat_feeds"] = enriched.ThreatFeeds
			}
			s.mergeThreatIntelIntoBehaviorEnrichment(tenantID, parsed, out)
			cache[ip] = out
			return out
		}
	}
	if s.ipIntel != nil {
		lookupCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		if enriched, ok, err := s.ipIntel.LookupCached(lookupCtx, ip); err == nil && ok && enriched != nil {
			out["country"] = enriched.Geo.Country
			out["country_code"] = enriched.Geo.CountryCode
			out["region"] = enriched.Geo.Region
			out["asn"] = enriched.Geo.ASN
			out["isp"] = firstNonEmpty(enriched.Geo.ISP, enriched.Geo.Org)
			out["usage_type"] = enriched.UsageType
			out["is_tor"] = enriched.IsTor
			out["reputation_score"] = enriched.ReputationScore
			out["content_source"] = enriched.Source
			if len(enriched.ThreatFeeds) > 0 {
				out["threat_feeds"] = enriched.ThreatFeeds
			}
		}
	}
	s.mergeThreatIntelIntoBehaviorEnrichment(tenantID, parsed, out)
	cache[ip] = out
	return out
}

func (s *Server) mergeThreatIntelIntoBehaviorEnrichment(tenantID uuid.UUID, ip net.IP, out map[string]any) {
	if out == nil || ip == nil {
		return
	}
	matches := s.threatIntelIPMatches(tenantID, ip)
	if len(matches) == 0 {
		return
	}
	hits, _, score := threatIndicatorsToEnrichment(matches)
	if len(hits) > 0 {
		out["threat_feeds"] = mergeBehaviorThreatFeeds(out["threat_feeds"], hits)
	}
	if current, ok := out["reputation_score"].(int); !ok || score > current {
		out["reputation_score"] = score
	}
}

func mergeBehaviorThreatFeeds(existing any, extra []ipintel.ThreatFeedHit) []ipintel.ThreatFeedHit {
	base := []ipintel.ThreatFeedHit{}
	switch feeds := existing.(type) {
	case []ipintel.ThreatFeedHit:
		base = append(base, feeds...)
	case []offlinebundle.ThreatFeedHit:
		for _, feed := range feeds {
			base = append(base, ipintel.ThreatFeedHit{Feed: feed.Feed, Severity: feed.Severity})
		}
	}
	return mergeIPThreatFeedHits(base, extra)
}

func firstThreatFeedName(value any) string {
	switch feeds := value.(type) {
	case []ipintel.ThreatFeedHit:
		if len(feeds) > 0 {
			return feeds[0].Feed
		}
	case []offlinebundle.ThreatFeedHit:
		if len(feeds) > 0 {
			return feeds[0].Feed
		}
	}
	return ""
}

func parseLogTimestamp(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Now().UTC()
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05.000", "2006-01-02 15:04:05"} {
		if ts, err := time.Parse(layout, raw); err == nil {
			return ts.UTC()
		}
	}
	return time.Now().UTC()
}

func mergeStringLabels(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	for k, v := range b {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func stringPtrOrNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	v := strings.TrimSpace(s)
	return &v
}

func firstString(values []string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fieldString(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	if v, ok := fields[key]; ok {
		switch x := v.(type) {
		case string:
			return strings.TrimSpace(x)
		case fmt.Stringer:
			return strings.TrimSpace(x.String())
		}
	}
	return ""
}

func fieldInt(fields map[string]any, key string) int {
	return int(fieldInt64(fields, key))
}

func fieldInt64(fields map[string]any, key string) int64 {
	if fields == nil {
		return 0
	}
	v, ok := fields[key]
	if !ok {
		return 0
	}
	switch x := v.(type) {
	case int:
		return int64(x)
	case int64:
		return x
	case float64:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		if x == "-" {
			return 0
		}
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	}
	return 0
}

func copyFirstWebRequestField(dst, fields map[string]any, dstKey string, sourceKeys ...string) {
	for _, key := range sourceKeys {
		if v := fieldString(fields, key); v != "" && v != "-" {
			dst[dstKey] = v
			return
		}
	}
}

func firstPositiveFieldInt64(fields map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if v := fieldInt64(fields, key); v > 0 {
			return v
		}
	}
	return 0
}

func durationMillis(fields map[string]any) int64 {
	for _, key := range []string{"duration_ms", "request_time_ms", "duration"} {
		if v := fieldInt64(fields, key); v > 0 {
			return v
		}
	}
	if us := fieldInt64(fields, "duration_us"); us > 0 {
		return us / 1000
	}
	if sec := fieldString(fields, "request_time"); sec != "" {
		f, _ := strconv.ParseFloat(sec, 64)
		return int64(f * 1000)
	}
	return 0
}

func splitHTTPReq(req string) (string, string) {
	parts := strings.Fields(req)
	if len(parts) < 2 {
		return "", ""
	}
	return strings.ToUpper(parts[0]), parts[1]
}

func pathTemplate(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if u, err := url.Parse(path); err == nil && u.Path != "" {
		path = u.Path
	}
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		if seg == "" {
			continue
		}
		if _, err := strconv.ParseInt(seg, 10, 64); err == nil || looksLikeUUID(seg) {
			segments[i] = ":id"
		}
	}
	return strings.Join(segments, "/")
}

func looksLikeUUID(s string) bool {
	return len(s) == 36 && strings.Count(s, "-") == 4
}

func referrerHost(raw string) string {
	if raw == "" || raw == "-" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func severityFromHTTPStatusCode(status int) string {
	switch {
	case status >= 500:
		return "error"
	case status >= 400:
		return "warning"
	case status >= 300:
		return "notice"
	default:
		return "info"
	}
}
