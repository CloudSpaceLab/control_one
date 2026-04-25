package server

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
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
