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
	ID            string   `json:"id"`
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Description   *string  `json:"description,omitempty"`
	EventTypes    []string `json:"event_types"`
	WindowSeconds int      `json:"window_seconds"`
	Threshold     int      `json:"threshold"`
	Dimension     string   `json:"dimension"`
	Severity      string   `json:"severity"`
	Enabled       bool     `json:"enabled"`
	YAMLSpec      *string  `json:"yaml_spec,omitempty"`
	CreatedAt     string   `json:"created_at"`
}

func newCorrelationRuleResponse(r storage.CorrelationRule) correlationRuleResponse {
	out := correlationRuleResponse{
		ID: r.ID.String(), TenantID: r.TenantID.String(),
		Name: r.Name, EventTypes: r.EventTypes,
		WindowSeconds: r.WindowSeconds, Threshold: r.Threshold,
		Dimension: r.Dimension, Severity: r.Severity, Enabled: r.Enabled,
		CreatedAt: formatTime(r.CreatedAt),
	}
	if out.EventTypes == nil {
		out.EventTypes = []string{}
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
	TenantID      string   `json:"tenant_id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	EventTypes    []string `json:"event_types"`
	WindowSeconds int      `json:"window_seconds"`
	Threshold     int      `json:"threshold"`
	Dimension     string   `json:"dimension"`
	Severity      string   `json:"severity"`
	Enabled       *bool    `json:"enabled"`
	YAMLSpec      string   `json:"yaml_spec"`
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
		tenantID, err := uuid.Parse(req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		rule, err := s.store.CreateCorrelationRule(r.Context(), storage.CreateCorrelationRuleParams{
			TenantID:      tenantID,
			Name:          req.Name,
			Description:   req.Description,
			EventTypes:    req.EventTypes,
			WindowSeconds: req.WindowSeconds,
			Threshold:     req.Threshold,
			Dimension:     req.Dimension,
			Severity:      req.Severity,
			Enabled:       enabled,
			YAMLSpec:      req.YAMLSpec,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("create failed: %v", err), http.StatusBadRequest)
			return
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
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
