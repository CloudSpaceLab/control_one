package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ---------- security events ----------

type securityEventResponse struct {
	ID        string         `json:"id"`
	TenantID  string         `json:"tenant_id"`
	NodeID    *string        `json:"node_id,omitempty"`
	EventType string         `json:"event_type"`
	Severity  string         `json:"severity"`
	Source    string         `json:"source"`
	Details   map[string]any `json:"details"`
	DedupKey  *string        `json:"dedup_key,omitempty"`
	FiredAt   string         `json:"fired_at"`
}

func newSecurityEventResponse(e storage.SecurityEvent) securityEventResponse {
	out := securityEventResponse{
		ID:        e.ID.String(),
		TenantID:  e.TenantID.String(),
		EventType: e.EventType,
		Severity:  e.Severity,
		Source:    e.Source,
		Details:   e.Details,
		FiredAt:   formatTime(e.FiredAt),
	}
	if out.Details == nil {
		out.Details = map[string]any{}
	}
	if e.NodeID.Valid {
		s := e.NodeID.UUID.String()
		out.NodeID = &s
	}
	if e.DedupKey.Valid {
		s := e.DedupKey.String
		out.DedupKey = &s
	}
	return out
}

type createSecurityEventRequest struct {
	TenantID  string         `json:"tenant_id"`
	NodeID    *string        `json:"node_id"`
	EventType string         `json:"event_type"`
	Severity  string         `json:"severity"`
	Source    string         `json:"source"`
	Details   map[string]any `json:"details"`
	DedupKey  string         `json:"dedup_key"`
}

func (s *Server) handleSecurityEventsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListSecurityEvents(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		s.handleCreateSecurityEvent(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListSecurityEvents(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	filter := storage.SecurityEventFilter{Severity: strings.TrimSpace(r.URL.Query().Get("severity"))}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		filter.TenantID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("node_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid since", http.StatusBadRequest)
			return
		}
		filter.Since = &t
	}
	events, total, err := s.store.ListSecurityEvents(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list security events", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]securityEventResponse, 0, len(events))
	for _, e := range events {
		items = append(items, newSecurityEventResponse(e))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[securityEventResponse]{Data: items, Pagination: newPaginationMeta(total, limit, offset, len(items))})
}

func (s *Server) handleCreateSecurityEvent(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createSecurityEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	var nodeID *uuid.UUID
	if req.NodeID != nil && strings.TrimSpace(*req.NodeID) != "" {
		nid, err := uuid.Parse(*req.NodeID)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		nodeID = &nid
	}
	ev, err := s.store.CreateSecurityEvent(r.Context(), storage.CreateSecurityEventParams{
		TenantID:  tenantID,
		NodeID:    nodeID,
		EventType: req.EventType,
		Severity:  req.Severity,
		Source:    req.Source,
		Details:   req.Details,
		DedupKey:  req.DedupKey,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create security event failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newSecurityEventResponse(*ev))

	payload, _ := json.Marshal(map[string]any{
		"event_id":   ev.ID.String(),
		"event_type": ev.EventType,
		"severity":   ev.Severity,
	})
	s.publishEvent(eventbus.Event{
		Topic:    eventbus.TopicSecurityEvent,
		TenantID: tenantID,
		NodeID:   nodeID,
		Payload:  payload,
	})
}

// ---------- health incidents ----------

type healthIncidentResponse struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	NodeID       *string        `json:"node_id,omitempty"`
	IncidentType string         `json:"incident_type"`
	Severity     string         `json:"severity"`
	Details      map[string]any `json:"details"`
	OpenedAt     string         `json:"opened_at"`
	ResolvedAt   *string        `json:"resolved_at,omitempty"`
}

func newHealthIncidentResponse(h storage.HealthIncident) healthIncidentResponse {
	out := healthIncidentResponse{
		ID:           h.ID.String(),
		TenantID:     h.TenantID.String(),
		IncidentType: h.IncidentType,
		Severity:     h.Severity,
		Details:      h.Details,
		OpenedAt:     formatTime(h.OpenedAt),
	}
	if out.Details == nil {
		out.Details = map[string]any{}
	}
	if h.NodeID.Valid {
		s := h.NodeID.UUID.String()
		out.NodeID = &s
	}
	if h.ResolvedAt.Valid {
		s := formatTime(h.ResolvedAt.Time)
		out.ResolvedAt = &s
	}
	return out
}

type createHealthIncidentRequest struct {
	TenantID     string         `json:"tenant_id"`
	NodeID       *string        `json:"node_id"`
	IncidentType string         `json:"incident_type"`
	Severity     string         `json:"severity"`
	Details      map[string]any `json:"details"`
	DedupKey     string         `json:"dedup_key"`
}

func (s *Server) handleHealthIncidentsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		s.handleCreateHealthIncident(w, r)
	default:
		w.Header().Set("Allow", "POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreateHealthIncident(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createHealthIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	var nodeID *uuid.UUID
	if req.NodeID != nil && strings.TrimSpace(*req.NodeID) != "" {
		nid, err := uuid.Parse(*req.NodeID)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		nodeID = &nid
	}
	hi, err := s.store.CreateHealthIncident(r.Context(), storage.CreateHealthIncidentParams{
		TenantID:     tenantID,
		NodeID:       nodeID,
		IncidentType: req.IncidentType,
		Severity:     req.Severity,
		Details:      req.Details,
		DedupKey:     req.DedupKey,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create health incident failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newHealthIncidentResponse(*hi))
	payload, _ := json.Marshal(map[string]any{"incident_id": hi.ID.String(), "severity": hi.Severity})
	s.publishEvent(eventbus.Event{
		Topic:    eventbus.TopicHealthIncident,
		TenantID: tenantID,
		NodeID:   nodeID,
		Payload:  payload,
	})
}

// ---------- rule triggers (ingest from agent) ----------

type createRuleTriggerRequest struct {
	TenantID string         `json:"tenant_id"`
	RuleID   string         `json:"rule_id"`
	RuleType string         `json:"rule_type"`
	NodeID   *string        `json:"node_id"`
	Severity string         `json:"severity"`
	Details  map[string]any `json:"details"`
}

func (s *Server) handleRuleTriggersCollection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleOperator); !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createRuleTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	ruleID, err := uuid.Parse(req.RuleID)
	if err != nil {
		http.Error(w, "invalid rule_id", http.StatusBadRequest)
		return
	}
	var nodeID *uuid.UUID
	if req.NodeID != nil && strings.TrimSpace(*req.NodeID) != "" {
		nid, err := uuid.Parse(*req.NodeID)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		nodeID = &nid
	}
	trig, err := s.store.CreateRuleTrigger(r.Context(), storage.CreateRuleTriggerParams{
		TenantID: tenantID,
		RuleID:   ruleID,
		RuleType: req.RuleType,
		NodeID:   nodeID,
		Severity: req.Severity,
		Details:  req.Details,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create rule trigger failed: %v", err), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = trig
	payload, _ := json.Marshal(map[string]any{"rule_id": ruleID.String(), "rule_type": req.RuleType, "severity": req.Severity})
	s.publishEvent(eventbus.Event{
		Topic:    eventbus.TopicRuleTriggered,
		TenantID: tenantID,
		NodeID:   nodeID,
		Payload:  payload,
	})
}
