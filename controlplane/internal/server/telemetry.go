package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
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
// Each metric becomes one row in telemetry_metrics. Tenant + node_id are
// resolved from the mTLS agent principal — the body's node_id is informational.
type agentMetricsIngestRequest struct {
	NodeID    string         `json:"node_id"`
	Timestamp string         `json:"timestamp"`
	Metrics   map[string]any `json:"metrics"`
	Samples   []metricSample `json:"samples"`
}

type metricSample struct {
	Name   string            `json:"name"`
	Value  any               `json:"value"`
	Unit   string            `json:"unit"`
	Labels map[string]string `json:"labels"`
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

	rows := make([]storage.CreateTelemetryMetricParams, 0, len(body.Metrics)+len(body.Samples))
	for name, raw := range body.Metrics {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		val, ok := metricToFloat(raw)
		if !ok {
			// Skip values we can't coerce; the agent might add string-typed
			// metrics later and we don't want one bad row to drop the batch.
			continue
		}
		row := storage.CreateTelemetryMetricParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			MetricName:  name,
			MetricValue: val,
			Timestamp:   ts,
		}
		if unit, known := metricUnits[name]; known && unit != "" {
			u := unit
			row.MetricUnit = &u
		}
		rows = append(rows, row)
	}
	for _, sample := range body.Samples {
		name := strings.TrimSpace(sample.Name)
		if name == "" {
			continue
		}
		val, ok := metricToFloat(sample.Value)
		if !ok {
			continue
		}
		row := storage.CreateTelemetryMetricParams{
			TenantID:    tenantID,
			NodeID:      nodeID,
			MetricName:  name,
			MetricValue: val,
			Labels:      sample.Labels,
			Timestamp:   ts,
		}
		if unit := strings.TrimSpace(sample.Unit); unit != "" {
			row.MetricUnit = &unit
		} else if unit, known := metricUnits[name]; known && unit != "" {
			u := unit
			row.MetricUnit = &u
		}
		rows = append(rows, row)
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

func (s *Server) handleTelemetryMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
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

	filter := storage.TelemetryMetricFilter{}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
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

	if _, ok := s.authorize(w, r, roleViewer); !ok {
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

	filter := storage.TelemetryLogFilter{}

	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = parsed
	}

	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		parsed, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
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

	if segments[1] == "metrics" {
		filter := storage.TelemetryMetricFilter{NodeID: nodeID}

		if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
			parsed, err := uuid.Parse(tenantParam)
			if err != nil {
				http.Error(w, "invalid tenant_id", http.StatusBadRequest)
				return
			}
			filter.TenantID = parsed
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
func (s *Server) handleLegacyHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	// Drain body so the connection is reusable.
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

// handleLegacyTelemetry accepts POST /api/v1/telemetry from agent telemetry
// service. The payload is acknowledged but not currently persisted.
func (s *Server) handleLegacyTelemetry(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	_, _ = io.Copy(io.Discard, r.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

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
