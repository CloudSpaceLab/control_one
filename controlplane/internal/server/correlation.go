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

type correlationRuleResponse struct {
	ID                 string                           `json:"id"`
	TenantID           string                           `json:"tenant_id"`
	Name               string                           `json:"name"`
	Description        *string                          `json:"description,omitempty"`
	EventTypes         []string                         `json:"event_types"`
	EventType          string                           `json:"event_type"`
	WindowSeconds      int                              `json:"window_seconds"`
	Threshold          int                              `json:"threshold"`
	Dimension          string                           `json:"dimension"`
	GroupBy            []string                         `json:"group_by"`
	SuppressionSeconds int                              `json:"suppression_seconds"`
	Conditions         []storage.CorrelationCondition   `json:"conditions"`
	ConditionGroups    [][]storage.CorrelationCondition `json:"condition_groups"`
	DistinctField      string                           `json:"distinct_field"`
	Severity           string                           `json:"severity"`
	Enabled            bool                             `json:"enabled"`
	YAMLSpec           *string                          `json:"yaml_spec,omitempty"`
	CreatedAt          string                           `json:"created_at"`
	UpdatedAt          string                           `json:"updated_at"`
}

func newCorrelationRuleResponse(r storage.CorrelationRule) correlationRuleResponse {
	out := correlationRuleResponse{
		ID: r.ID.String(), TenantID: r.TenantID.String(),
		Name: r.Name, EventTypes: r.EventTypes,
		WindowSeconds: r.WindowSeconds, Threshold: r.Threshold,
		EventType: r.EventType, Dimension: r.Dimension, GroupBy: r.GroupBy,
		SuppressionSeconds: r.SuppressionSeconds, Conditions: r.Conditions, ConditionGroups: r.ConditionGroups, DistinctField: r.DistinctField, Severity: r.Severity, Enabled: r.Enabled,
		CreatedAt: formatTime(r.CreatedAt), UpdatedAt: formatTime(r.UpdatedAt),
	}
	if out.EventTypes == nil {
		out.EventTypes = []string{}
	}
	if out.GroupBy == nil {
		out.GroupBy = []string{}
	}
	if out.Conditions == nil {
		out.Conditions = []storage.CorrelationCondition{}
	}
	if out.ConditionGroups == nil {
		out.ConditionGroups = [][]storage.CorrelationCondition{}
	}
	if r.Description.Valid {
		s := r.Description.String
		out.Description = &s
	}
	if r.YAMLSpec.Valid {
		s := r.YAMLSpec.String
		out.YAMLSpec = &s
	}
	return out
}

type createCorrelationRuleRequest struct {
	TenantID           string                           `json:"tenant_id"`
	Name               string                           `json:"name"`
	Description        string                           `json:"description"`
	EventTypes         []string                         `json:"event_types"`
	EventType          string                           `json:"event_type"`
	WindowSeconds      int                              `json:"window_seconds"`
	Threshold          int                              `json:"threshold"`
	Dimension          string                           `json:"dimension"`
	GroupBy            []string                         `json:"group_by"`
	SuppressionSeconds int                              `json:"suppression_seconds"`
	Conditions         []storage.CorrelationCondition   `json:"conditions"`
	ConditionGroups    [][]storage.CorrelationCondition `json:"condition_groups"`
	DistinctField      string                           `json:"distinct_field"`
	Severity           string                           `json:"severity"`
	Enabled            *bool                            `json:"enabled"`
	YAMLSpec           string                           `json:"yaml_spec"`
}

var correlationTopics = map[string]bool{
	"security.event": true, "events.anomaly": true, "rule.triggered": true,
	"compliance.fired": true, "health.incident": true, "remediation.applied": true,
}
var correlationDimensions = map[string]bool{
	"node_id": true, "tenant_id": true, "src_ip": true, "user_name": true, "correlation_id": true,
}
var correlationSeverities = map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
var correlationConditionFields = map[string]bool{
	"src_ip": true, "dst_ip": true, "src_port": true, "dst_port": true, "protocol": true,
	"user_name": true, "auth_result": true, "status_code": true, "http_method": true,
	"path": true, "source": true, "severity": true,
}
var correlationConditionOperators = map[string]bool{
	"eq": true, "neq": true, "contains": true, "gt": true, "gte": true, "lt": true, "lte": true,
}

func validateCorrelationRuleRequest(req *createCorrelationRuleRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.EventType = strings.TrimSpace(req.EventType)
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(req.EventTypes) != 1 || !correlationTopics[req.EventTypes[0]] {
		return fmt.Errorf("one supported event category is required")
	}
	if req.WindowSeconds < 1 || req.WindowSeconds > 86400 {
		return fmt.Errorf("window_seconds must be between 1 and 86400")
	}
	if req.Threshold < 1 || req.Threshold > 1000000 {
		return fmt.Errorf("threshold must be between 1 and 1000000")
	}
	if len(req.GroupBy) == 0 && req.Dimension != "" {
		req.GroupBy = []string{req.Dimension}
	}
	if len(req.GroupBy) == 0 || len(req.GroupBy) > 3 {
		return fmt.Errorf("group_by must contain between 1 and 3 fields")
	}
	seen := map[string]bool{}
	for _, field := range req.GroupBy {
		if !correlationDimensions[field] {
			return fmt.Errorf("unsupported group_by field %q", field)
		}
		if seen[field] {
			return fmt.Errorf("duplicate group_by field %q", field)
		}
		seen[field] = true
	}
	req.Dimension = req.GroupBy[0]
	if !correlationSeverities[req.Severity] {
		return fmt.Errorf("severity must be low, medium, high, or critical")
	}
	if req.SuppressionSeconds < 0 || req.SuppressionSeconds > 604800 {
		return fmt.Errorf("suppression_seconds must be between 0 and 604800")
	}
	if len(req.Conditions) > 10 {
		return fmt.Errorf("conditions cannot contain more than 10 entries")
	}
	for i := range req.Conditions {
		if err := validateCorrelationCondition(&req.Conditions[i], fmt.Sprintf("condition %d", i+1)); err != nil {
			return err
		}
	}
	if len(req.ConditionGroups) > 10 {
		return fmt.Errorf("condition_groups cannot contain more than 10 groups")
	}
	for groupIndex := range req.ConditionGroups {
		if len(req.ConditionGroups[groupIndex]) == 0 || len(req.ConditionGroups[groupIndex]) > 10 {
			return fmt.Errorf("condition group %d must contain between 1 and 10 entries", groupIndex+1)
		}
		for conditionIndex := range req.ConditionGroups[groupIndex] {
			label := fmt.Sprintf("condition group %d entry %d", groupIndex+1, conditionIndex+1)
			if err := validateCorrelationCondition(&req.ConditionGroups[groupIndex][conditionIndex], label); err != nil {
				return err
			}
		}
	}
	req.DistinctField = strings.TrimSpace(req.DistinctField)
	if req.DistinctField != "" && !correlationConditionFields[req.DistinctField] {
		return fmt.Errorf("unsupported distinct_field %q", req.DistinctField)
	}
	return nil
}

func validateCorrelationCondition(condition *storage.CorrelationCondition, label string) error {
	condition.Field = strings.TrimSpace(condition.Field)
	condition.Operator = strings.TrimSpace(condition.Operator)
	if !correlationConditionFields[condition.Field] {
		return fmt.Errorf("unsupported condition field %q", condition.Field)
	}
	if !correlationConditionOperators[condition.Operator] {
		return fmt.Errorf("unsupported condition operator %q", condition.Operator)
	}
	if condition.Value == nil || strings.TrimSpace(fmt.Sprint(condition.Value)) == "" {
		return fmt.Errorf("%s value is required", label)
	}
	return nil
}

func correlationParams(tenantID uuid.UUID, req createCorrelationRuleRequest, enabled bool) storage.CreateCorrelationRuleParams {
	return storage.CreateCorrelationRuleParams{TenantID: tenantID, Name: req.Name, Description: req.Description,
		EventTypes: req.EventTypes, EventType: req.EventType, WindowSeconds: req.WindowSeconds,
		Threshold: req.Threshold, Dimension: req.Dimension, GroupBy: req.GroupBy,
		SuppressionSeconds: req.SuppressionSeconds, Conditions: req.Conditions, ConditionGroups: req.ConditionGroups, DistinctField: req.DistinctField, Severity: req.Severity, Enabled: enabled, YAMLSpec: req.YAMLSpec}
}

func (s *Server) handleCorrelationRulesCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		tenantID, err := requiredTenantID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		rules, err := s.store.ListCorrelationRules(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("list correlation rules", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		resp := make([]correlationRuleResponse, 0, len(rules))
		for _, rr := range rules {
			resp = append(resp, newCorrelationRuleResponse(rr))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": resp})
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var req createCorrelationRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		if err := validateCorrelationRuleRequest(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tenantID, err := uuid.Parse(req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rule, err := s.store.CreateCorrelationRule(r.Context(), correlationParams(tenantID, req, enabled))
		if err != nil {
			http.Error(w, fmt.Sprintf("create failed: %v", err), http.StatusBadRequest)
			return
		}
		if s.correlationEng != nil {
			s.correlationEng.InvalidateCache(tenantID)
		}
		writeJSON(w, http.StatusCreated, newCorrelationRuleResponse(*rule))
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCorrelationRuleSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/correlation-rules/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		rr, err := s.store.GetCorrelationRule(r.Context(), tenantID, id)
		if err != nil {
			s.logger.Error("get correlation rule", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if rr == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newCorrelationRuleResponse(*rr))
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		if err := s.store.DeleteCorrelationRule(r.Context(), tenantID, id); err != nil {
			http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusBadRequest)
			return
		}
		if s.correlationEng != nil {
			s.correlationEng.InvalidateCache(tenantID)
		}
		w.WriteHeader(http.StatusNoContent)
	case http.MethodPut:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var req createCorrelationRuleRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		if err := validateCorrelationRuleRequest(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rule, err := s.store.UpdateCorrelationRule(r.Context(), tenantID, id, correlationParams(tenantID, req, enabled))
		if err != nil {
			http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusBadRequest)
			return
		}
		if rule == nil {
			http.NotFound(w, r)
			return
		}
		if s.correlationEng != nil {
			s.correlationEng.InvalidateCache(tenantID)
		}
		writeJSON(w, http.StatusOK, newCorrelationRuleResponse(*rule))
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
