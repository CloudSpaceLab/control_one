package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ---------- port rules ----------

type portRuleResponse struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	PolicyID      *string        `json:"policy_id,omitempty"`
	Name          string         `json:"name"`
	Port          int            `json:"port"`
	Protocol      string         `json:"protocol"`
	ExpectedState string         `json:"expected_state"`
	TargetLabels  map[string]any `json:"target_labels"`
	Severity      string         `json:"severity"`
	Action        string         `json:"action"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

func newPortRuleResponse(r storage.PortMonitoringRule) portRuleResponse {
	out := portRuleResponse{
		ID:            r.ID.String(),
		TenantID:      r.TenantID.String(),
		Name:          r.Name,
		Port:          r.Port,
		Protocol:      r.Protocol,
		ExpectedState: r.ExpectedState,
		TargetLabels:  r.TargetLabels,
		Severity:      r.Severity,
		Action:        r.Action,
		Enabled:       r.Enabled,
		CreatedAt:     formatTime(r.CreatedAt),
		UpdatedAt:     formatTime(r.UpdatedAt),
	}
	if out.TargetLabels == nil {
		out.TargetLabels = map[string]any{}
	}
	if r.PolicyID.Valid {
		s := r.PolicyID.UUID.String()
		out.PolicyID = &s
	}
	return out
}

type createPortRuleRequest struct {
	TenantID      string         `json:"tenant_id"`
	PolicyID      *string        `json:"policy_id"`
	Name          string         `json:"name"`
	Port          int            `json:"port"`
	Protocol      string         `json:"protocol"`
	ExpectedState string         `json:"expected_state"`
	TargetLabels  map[string]any `json:"target_labels"`
	Severity      string         `json:"severity"`
	Action        string         `json:"action"`
	Enabled       *bool          `json:"enabled"`
}

type updatePortRuleRequest struct {
	Name          *string         `json:"name"`
	Port          *int            `json:"port"`
	Protocol      *string         `json:"protocol"`
	ExpectedState *string         `json:"expected_state"`
	TargetLabels  *map[string]any `json:"target_labels"`
	Severity      *string         `json:"severity"`
	Action        *string         `json:"action"`
	Enabled       *bool           `json:"enabled"`
}

func (s *Server) handlePortRulesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListPortRules(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreatePortRule(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handlePortRuleSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/rules/port/")
	idStr := strings.SplitN(rest, "/", 2)[0]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid rule id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		rule, err := s.store.GetPortRule(r.Context(), id)
		if err != nil {
			s.logger.Error("get port rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if rule == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newPortRuleResponse(*rule))
	case http.MethodPatch, http.MethodPut:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var req updatePortRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		updated, err := s.store.UpdatePortRule(r.Context(), id, storage.UpdatePortRuleParams(req))
		if err != nil {
			http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, newPortRuleResponse(*updated))
		s.publishRuleUpdated(updated.TenantID, "port")
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		rule, _ := s.store.GetPortRule(r.Context(), id)
		if err := s.store.DeletePortRule(r.Context(), id); err != nil {
			s.logger.Error("delete port rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		if rule != nil {
			s.publishRuleUpdated(rule.TenantID, "port")
		}
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListPortRules(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.PortRuleFilter{}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("policy_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid policy_id", http.StatusBadRequest)
			return
		}
		filter.PolicyID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("enabled")); v != "" {
		b := parseBoolQuery(v)
		filter.Enabled = &b
	}
	rules, total, err := s.store.ListPortRules(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list port rules", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]portRuleResponse, 0, len(rules))
	for _, rr := range rules {
		items = append(items, newPortRuleResponse(rr))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[portRuleResponse]{Data: items, Pagination: newPaginationMeta(total, limit, offset, len(items))})
}

func (s *Server) handleCreatePortRule(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createPortRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	var policyID *uuid.UUID
	if req.PolicyID != nil && strings.TrimSpace(*req.PolicyID) != "" {
		pid, err := uuid.Parse(*req.PolicyID)
		if err != nil {
			http.Error(w, "invalid policy_id", http.StatusBadRequest)
			return
		}
		policyID = &pid
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule, err := s.store.CreatePortRule(r.Context(), storage.CreatePortRuleParams{
		TenantID:      tenantID,
		PolicyID:      policyID,
		Name:          req.Name,
		Port:          req.Port,
		Protocol:      req.Protocol,
		ExpectedState: req.ExpectedState,
		TargetLabels:  req.TargetLabels,
		Severity:      req.Severity,
		Action:        req.Action,
		Enabled:       enabled,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create port rule failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newPortRuleResponse(*rule))
	s.publishRuleUpdated(tenantID, "port")
}

// ---------- log rules ----------

type logRuleResponse struct {
	ID            string         `json:"id"`
	TenantID      string         `json:"tenant_id"`
	PolicyID      *string        `json:"policy_id,omitempty"`
	Name          string         `json:"name"`
	LogSource     string         `json:"log_source"`
	Pattern       string         `json:"pattern"`
	Severity      string         `json:"severity"`
	WindowSeconds int            `json:"window_seconds"`
	Threshold     int            `json:"threshold"`
	Action        string         `json:"action"`
	TargetLabels  map[string]any `json:"target_labels"`
	Enabled       bool           `json:"enabled"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

func newLogRuleResponse(r storage.LogMonitoringRule) logRuleResponse {
	out := logRuleResponse{
		ID:            r.ID.String(),
		TenantID:      r.TenantID.String(),
		Name:          r.Name,
		LogSource:     r.LogSource,
		Pattern:       r.Pattern,
		Severity:      r.Severity,
		WindowSeconds: r.WindowSeconds,
		Threshold:     r.Threshold,
		Action:        r.Action,
		TargetLabels:  r.TargetLabels,
		Enabled:       r.Enabled,
		CreatedAt:     formatTime(r.CreatedAt),
		UpdatedAt:     formatTime(r.UpdatedAt),
	}
	if out.TargetLabels == nil {
		out.TargetLabels = map[string]any{}
	}
	if r.PolicyID.Valid {
		s := r.PolicyID.UUID.String()
		out.PolicyID = &s
	}
	return out
}

type createLogRuleRequest struct {
	TenantID      string         `json:"tenant_id"`
	PolicyID      *string        `json:"policy_id"`
	Name          string         `json:"name"`
	LogSource     string         `json:"log_source"`
	Pattern       string         `json:"pattern"`
	Severity      string         `json:"severity"`
	WindowSeconds int            `json:"window_seconds"`
	Threshold     int            `json:"threshold"`
	Action        string         `json:"action"`
	TargetLabels  map[string]any `json:"target_labels"`
	Enabled       *bool          `json:"enabled"`
}

type updateLogRuleRequest struct {
	Name          *string         `json:"name"`
	LogSource     *string         `json:"log_source"`
	Pattern       *string         `json:"pattern"`
	Severity      *string         `json:"severity"`
	WindowSeconds *int            `json:"window_seconds"`
	Threshold     *int            `json:"threshold"`
	Action        *string         `json:"action"`
	TargetLabels  *map[string]any `json:"target_labels"`
	Enabled       *bool           `json:"enabled"`
}

func (s *Server) handleLogRulesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListLogRules(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateLogRule(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogRuleSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/rules/log/")
	idStr := strings.SplitN(rest, "/", 2)[0]
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid rule id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		rule, err := s.store.GetLogRule(r.Context(), id)
		if err != nil {
			s.logger.Error("get log rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if rule == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newLogRuleResponse(*rule))
	case http.MethodPatch, http.MethodPut:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var req updateLogRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		updated, err := s.store.UpdateLogRule(r.Context(), id, storage.UpdateLogRuleParams(req))
		if err != nil {
			http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, newLogRuleResponse(*updated))
		s.publishRuleUpdated(updated.TenantID, "log")
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		rule, _ := s.store.GetLogRule(r.Context(), id)
		if err := s.store.DeleteLogRule(r.Context(), id); err != nil {
			s.logger.Error("delete log rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		if rule != nil {
			s.publishRuleUpdated(rule.TenantID, "log")
		}
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListLogRules(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.LogRuleFilter{LogSource: strings.TrimSpace(r.URL.Query().Get("log_source"))}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("policy_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid policy_id", http.StatusBadRequest)
			return
		}
		filter.PolicyID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("enabled")); v != "" {
		b := parseBoolQuery(v)
		filter.Enabled = &b
	}
	rules, total, err := s.store.ListLogRules(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list log rules", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]logRuleResponse, 0, len(rules))
	for _, rr := range rules {
		items = append(items, newLogRuleResponse(rr))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[logRuleResponse]{Data: items, Pagination: newPaginationMeta(total, limit, offset, len(items))})
}

func (s *Server) handleCreateLogRule(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createLogRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	var policyID *uuid.UUID
	if req.PolicyID != nil && strings.TrimSpace(*req.PolicyID) != "" {
		pid, err := uuid.Parse(*req.PolicyID)
		if err != nil {
			http.Error(w, "invalid policy_id", http.StatusBadRequest)
			return
		}
		policyID = &pid
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	rule, err := s.store.CreateLogRule(r.Context(), storage.CreateLogRuleParams{
		TenantID:      tenantID,
		PolicyID:      policyID,
		Name:          req.Name,
		LogSource:     req.LogSource,
		Pattern:       req.Pattern,
		Severity:      req.Severity,
		WindowSeconds: req.WindowSeconds,
		Threshold:     req.Threshold,
		Action:        req.Action,
		TargetLabels:  req.TargetLabels,
		Enabled:       enabled,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create log rule failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newLogRuleResponse(*rule))
	s.publishRuleUpdated(tenantID, "log")
}

// publishRuleUpdated emits a realtime event so subscribed agents refetch rules.
func (s *Server) publishRuleUpdated(tenantID uuid.UUID, ruleType string) {
	if s == nil || s.eventBus == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{"rule_type": ruleType})
	s.eventBus.Publish(eventbus.Event{
		Topic:    eventbus.TopicRuleTriggered,
		TenantID: tenantID,
		Payload:  payload,
	})
	// Also emit policy.updated so existing policy-sync paths refetch.
	payload2, _ := json.Marshal(map[string]any{"action": "rule_change", "rule_type": ruleType})
	s.eventBus.Publish(eventbus.Event{
		Topic:    eventbus.TopicPolicyUpdated,
		TenantID: tenantID,
		Payload:  payload2,
	})
}
