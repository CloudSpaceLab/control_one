package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type retentionPolicyResponse struct {
	ID            string         `json:"id"`
	TenantID      *string        `json:"tenant_id,omitempty"`
	PolicyName    string         `json:"policy_name"`
	DataType      string         `json:"data_type"`
	RetentionDays int            `json:"retention_days"`
	Enabled       bool           `json:"enabled"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type createRetentionPolicyRequest struct {
	TenantID      *string        `json:"tenant_id,omitempty"`
	PolicyName    string         `json:"policy_name"`
	DataType      string         `json:"data_type"`
	RetentionDays int            `json:"retention_days"`
	Enabled       *bool          `json:"enabled,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type retentionCleanupResponse struct {
	DeletedRows int64  `json:"deleted_rows"`
	DataType    string `json:"data_type"`
	TenantID    string `json:"tenant_id,omitempty"`
}

func (s *Server) handleRetentionPoliciesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListRetentionPolicies(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateRetentionPolicy(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListRetentionPolicies(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	policies, total, err := s.store.ListRetentionPolicies(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Error("list retention policies", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]retentionPolicyResponse, 0, len(policies))
	for _, policy := range policies {
		respItems = append(respItems, newRetentionPolicyResponse(policy))
	}

	resp := paginatedResponse[retentionPolicyResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateRetentionPolicy(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	var req createRetentionPolicyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.PolicyName) == "" {
		http.Error(w, "policy_name is required", http.StatusBadRequest)
		return
	}
	if req.RetentionDays <= 0 {
		http.Error(w, "retention_days must be positive", http.StatusBadRequest)
		return
	}
	dataType := strings.ToLower(strings.TrimSpace(req.DataType))
	if dataType != "metrics" && dataType != "logs" && dataType != "both" {
		http.Error(w, "data_type must be metrics, logs, or both", http.StatusBadRequest)
		return
	}

	var tenantID *uuid.UUID
	if req.TenantID != nil {
		parsed, err := uuid.Parse(*req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = &parsed
	}

	params := storage.CreateRetentionPolicyParams{
		TenantID:      tenantID,
		PolicyName:    req.PolicyName,
		DataType:      dataType,
		RetentionDays: req.RetentionDays,
		Enabled:       req.Enabled,
		Metadata:      req.Metadata,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}

	created, err := s.store.CreateRetentionPolicy(r.Context(), params)
	if err != nil {
		s.logger.Error("create retention policy", zap.Error(err))
		http.Error(w, fmt.Sprintf("create retention policy failed: %v", err), http.StatusBadRequest)
		return
	}

	resp := newRetentionPolicyResponse(*created)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *Server) handleRetentionCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	dataType := strings.TrimSpace(r.URL.Query().Get("data_type"))
	if dataType == "" {
		dataType = "both"
	}
	if dataType != "metrics" && dataType != "logs" && dataType != "both" {
		http.Error(w, "data_type must be metrics, logs, or both", http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	deletedRows, err := s.store.DeleteExpiredTelemetry(r.Context(), tenantID, dataType)
	if err != nil {
		s.logger.Error("delete expired telemetry", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := retentionCleanupResponse{
		DeletedRows: deletedRows,
		DataType:    dataType,
	}
	if tenantID != uuid.Nil {
		tid := tenantID.String()
		resp.TenantID = tid
	}

	writeJSON(w, http.StatusOK, resp)
}

func newRetentionPolicyResponse(policy storage.TelemetryRetentionPolicy) retentionPolicyResponse {
	resp := retentionPolicyResponse{
		ID:            policy.ID.String(),
		PolicyName:    policy.PolicyName,
		DataType:      policy.DataType,
		RetentionDays: policy.RetentionDays,
		Enabled:       policy.Enabled,
		Metadata:      policy.Metadata,
		CreatedAt:     formatTime(policy.CreatedAt),
		UpdatedAt:     formatTime(policy.UpdatedAt),
	}
	if policy.TenantID.Valid {
		tid := policy.TenantID.UUID.String()
		resp.TenantID = &tid
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	return resp
}
