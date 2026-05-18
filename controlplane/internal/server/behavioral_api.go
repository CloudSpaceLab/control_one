package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type behavioralBaselinePageStore interface {
	ListBehavioralBaselinesPage(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.BehavioralBaseline, int, error)
}

type ipBehaviorFindingPageStore interface {
	ListIPBehaviorFindings(context.Context, storage.IPBehaviorFindingFilter, int, int) ([]storage.IPBehaviorFinding, int, error)
}

type ipBehaviorFindingStatusStore interface {
	GetIPBehaviorFinding(context.Context, uuid.UUID) (*storage.IPBehaviorFinding, error)
	UpdateIPBehaviorFindingStatus(context.Context, uuid.UUID, string) (*storage.IPBehaviorFinding, error)
}

func (s *Server) handleBehavioralBaselines(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(behavioralBaselinePageStore)
	if !ok {
		http.Error(w, "behavioral baseline store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, nodeID, ok := optionalTenantNodeQuery(w, r)
	if !ok {
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, total, err := store.ListBehavioralBaselinesPage(r.Context(), tenantID, nodeID, limit, offset)
	if err != nil {
		s.logger.Warn("list behavioral baselines", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]behavioralBaselineResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, newBehavioralBaselineResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       resp,
		"pagination": newPaginationMeta(total, limit, offset, len(resp)),
	})
}

func (s *Server) handleBehavioralAnomalies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	store, ok := s.store.(ipBehaviorFindingPageStore)
	if !ok {
		http.Error(w, "behavioral anomaly store unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, _, ok := optionalTenantNodeQuery(w, r)
	if !ok {
		return
	}
	var resolved *bool
	if raw := strings.TrimSpace(r.URL.Query().Get("resolved")); raw != "" {
		v := strings.EqualFold(raw, "true") || raw == "1"
		resolved = &v
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rows, total, err := store.ListIPBehaviorFindings(r.Context(), storage.IPBehaviorFindingFilter{
		TenantID: tenantID,
		Resolved: resolved,
		SourceIP: strings.TrimSpace(r.URL.Query().Get("src_ip")),
	}, limit, offset)
	if err != nil {
		s.logger.Warn("list behavioral anomalies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.backfillIPBehaviorConfidenceAlerts(r.Context(), rows)
	resp := make([]behavioralAnomalyResponse, 0, len(rows))
	for _, row := range rows {
		resp = append(resp, newBehavioralAnomalyResponse(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       resp,
		"pagination": newPaginationMeta(total, limit, offset, len(resp)),
	})
}

func (s *Server) handleBehavioralAnomalyResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	store, ok := s.store.(ipBehaviorFindingStatusStore)
	if !ok {
		http.Error(w, "behavioral anomaly store unavailable", http.StatusServiceUnavailable)
		return
	}
	trimmed := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/behavioral/anomalies/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid anomaly id", http.StatusBadRequest)
		return
	}
	var status string
	switch parts[1] {
	case "suppress":
		status = "suppressed"
	case "resolve":
		status = "resolved"
	default:
		http.NotFound(w, r)
		return
	}
	existing, err := store.GetIPBehaviorFinding(r.Context(), id)
	if err != nil {
		s.logger.Warn("get behavioral anomaly", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if existing == nil {
		http.NotFound(w, r)
		return
	}
	updated, err := store.UpdateIPBehaviorFindingStatus(r.Context(), id, status)
	if err != nil {
		s.logger.Warn("update behavioral anomaly", zap.Error(err))
		http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, updated.TenantID, "behavioral.anomaly."+status, "ip_behavior_finding", updated.ID.String(), map[string]any{
		"previous_status": existing.Status,
		"source_ip":       updated.SourceIP.String,
		"country_code":    updated.CountryCode,
		"asn":             updated.ASN,
		"score":           updated.Score,
	})
	writeJSON(w, http.StatusOK, newBehavioralAnomalyResponse(*updated))
}

type behavioralBaselineResponse struct {
	ID          string  `json:"id"`
	TenantID    string  `json:"tenant_id"`
	NodeID      *string `json:"node_id,omitempty"`
	Metric      string  `json:"metric"`
	Window      string  `json:"window"`
	Mean        float64 `json:"mean"`
	Stddev      float64 `json:"stddev"`
	SampleCount int64   `json:"sample_count"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

func newBehavioralBaselineResponse(row storage.BehavioralBaseline) behavioralBaselineResponse {
	var nodeID *string
	if row.NodeID.Valid {
		v := row.NodeID.UUID.String()
		nodeID = &v
	}
	return behavioralBaselineResponse{
		ID:          row.ID.String(),
		TenantID:    row.TenantID.String(),
		NodeID:      nodeID,
		Metric:      firstNonEmptyIPBehavior(row.SignalType, row.Dimension),
		Window:      formatWindowDays(row.WindowDays),
		Mean:        baselineMean(row.Baseline),
		Stddev:      floatFromAny(row.Baseline["stddev"]),
		SampleCount: baselineResponseSampleCount(row.Baseline),
		CreatedAt:   row.ComputedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   row.ComputedAt.UTC().Format(time.RFC3339),
	}
}

type behavioralAnomalyResponse struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	BaselineID    string         `json:"baseline_id"`
	NodeID        *string        `json:"node_id,omitempty"`
	SourceIP      *string        `json:"source_ip,omitempty"`
	CountryCode   string         `json:"country_code,omitempty"`
	ASN           string         `json:"asn,omitempty"`
	Metric        string         `json:"metric"`
	Severity      string         `json:"severity"`
	Status        string         `json:"status"`
	Reason        string         `json:"reason"`
	ObservedValue float64        `json:"observed_value"`
	ZScore        float64        `json:"z_score"`
	Evidence      map[string]any `json:"evidence,omitempty"`
	Resolved      bool           `json:"resolved"`
	ResolvedAt    *string        `json:"resolved_at,omitempty"`
	CreatedAt     string         `json:"created_at"`
	LastSeenAt    string         `json:"last_seen_at"`
}

func newBehavioralAnomalyResponse(row storage.IPBehaviorFinding) behavioralAnomalyResponse {
	var nodeID *string
	if row.NodeID.Valid {
		v := row.NodeID.UUID.String()
		nodeID = &v
	}
	var resolvedAt *string
	if row.Status == "resolved" || row.Status == "suppressed" {
		v := row.UpdatedAt.UTC().Format(time.RFC3339)
		resolvedAt = &v
	}
	var sourceIP *string
	if row.SourceIP.Valid {
		v := row.SourceIP.String
		sourceIP = &v
	}
	return behavioralAnomalyResponse{
		ID:            row.ID.String(),
		TenantID:      row.TenantID.String(),
		BaselineID:    "",
		NodeID:        nodeID,
		SourceIP:      sourceIP,
		CountryCode:   row.CountryCode,
		ASN:           row.ASN,
		Metric:        firstNonEmptyIPBehavior(row.Category, "ip_behavior"),
		Severity:      row.Severity,
		Status:        row.Status,
		Reason:        row.Reason,
		ObservedValue: float64(row.Score),
		ZScore:        float64(row.Score) / 20,
		Evidence:      row.Evidence,
		Resolved:      row.Status == "resolved" || row.Status == "suppressed",
		ResolvedAt:    resolvedAt,
		CreatedAt:     row.FirstSeenAt.UTC().Format(time.RFC3339),
		LastSeenAt:    row.LastSeenAt.UTC().Format(time.RFC3339),
	}
}

func optionalTenantNodeQuery(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	var tenantID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("tenant_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return uuid.Nil, uuid.Nil, false
		}
		tenantID = parsed
	}
	var nodeID uuid.UUID
	if raw := strings.TrimSpace(r.URL.Query().Get("node_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return uuid.Nil, uuid.Nil, false
		}
		nodeID = parsed
	}
	return tenantID, nodeID, true
}

func baselineMean(m map[string]any) float64 {
	for _, key := range []string{"mean", "avg", "ewma"} {
		if v := floatFromAny(m[key]); v != 0 {
			return v
		}
	}
	for _, key := range []string{"request_count", "bytes_out"} {
		if v := nestedFloat(m, key, "avg"); v != 0 {
			return v
		}
	}
	if v := floatFromAny(m["dominant_ratio"]); v != 0 {
		return v
	}
	return 0
}

func baselineResponseSampleCount(m map[string]any) int64 {
	for _, key := range []string{"sample_count", "samples", "total_samples", "total_requests"} {
		if v := int64FromServerAny(m[key]); v != 0 {
			return v
		}
	}
	return 0
}

func formatWindowDays(days int) string {
	if days <= 0 {
		return ""
	}
	return strconv.Itoa(days) + "d"
}
