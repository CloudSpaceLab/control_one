package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type alertResponse struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	RuleID     *string        `json:"rule_id,omitempty"`
	NodeID     *string        `json:"node_id,omitempty"`
	Source     string         `json:"source"`
	Severity   string         `json:"severity"`
	Title      string         `json:"title"`
	Summary    *string        `json:"summary,omitempty"`
	State      string         `json:"state"`
	DedupKey   *string        `json:"dedup_key,omitempty"`
	Context    map[string]any `json:"context"`
	OpenedAt   string         `json:"opened_at"`
	AckedAt    *string        `json:"acked_at,omitempty"`
	AckedBy    *string        `json:"acked_by,omitempty"`
	ResolvedAt *string        `json:"resolved_at,omitempty"`
	ResolvedBy *string        `json:"resolved_by,omitempty"`
}

func newAlertResponse(a storage.Alert) alertResponse {
	out := alertResponse{
		ID:       a.ID.String(),
		TenantID: a.TenantID.String(),
		Source:   a.Source,
		Severity: a.Severity,
		Title:    a.Title,
		State:    a.State,
		Context:  a.Context,
		OpenedAt: formatTime(a.OpenedAt),
	}
	if out.Context == nil {
		out.Context = map[string]any{}
	}
	if a.RuleID.Valid {
		s := a.RuleID.UUID.String()
		out.RuleID = &s
	}
	if a.NodeID.Valid {
		s := a.NodeID.UUID.String()
		out.NodeID = &s
	}
	if a.Summary.Valid {
		s := a.Summary.String
		out.Summary = &s
	}
	if a.DedupKey.Valid {
		s := a.DedupKey.String
		out.DedupKey = &s
	}
	if a.AckedAt.Valid {
		s := formatTime(a.AckedAt.Time)
		out.AckedAt = &s
	}
	if a.AckedBy.Valid {
		s := a.AckedBy.UUID.String()
		out.AckedBy = &s
	}
	if a.ResolvedAt.Valid {
		s := formatTime(a.ResolvedAt.Time)
		out.ResolvedAt = &s
	}
	if a.ResolvedBy.Valid {
		s := a.ResolvedBy.UUID.String()
		out.ResolvedBy = &s
	}
	return out
}

type createAlertRequest struct {
	TenantID string         `json:"tenant_id"`
	RuleID   *string        `json:"rule_id"`
	NodeID   *string        `json:"node_id"`
	Source   string         `json:"source"`
	Severity string         `json:"severity"`
	Title    string         `json:"title"`
	Summary  string         `json:"summary"`
	DedupKey string         `json:"dedup_key"`
	Context  map[string]any `json:"context"`
}

func (s *Server) handleAlertsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListAlerts(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		s.handleCreateAlert(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAlertSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/alerts/")
	parts := strings.SplitN(rest, "/", 2)
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid alert id", http.StatusBadRequest)
		return
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		a, err := s.store.GetAlert(r.Context(), id)
		if err != nil {
			s.logger.Error("get alert", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if a == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newAlertResponse(*a))
	case "ack":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		userID := s.userIDForPrincipalCtx(r.Context(), principal)
		if err := s.store.AckAlert(r.Context(), id, userID); err != nil {
			http.Error(w, fmt.Sprintf("ack failed: %v", err), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "resolve":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		userID := s.userIDForPrincipalCtx(r.Context(), principal)
		if err := s.store.ResolveAlert(r.Context(), id, userID); err != nil {
			http.Error(w, fmt.Sprintf("resolve failed: %v", err), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := storage.AlertFilter{
		State:    strings.TrimSpace(r.URL.Query().Get("state")),
		Severity: strings.TrimSpace(r.URL.Query().Get("severity")),
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		f.TenantID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("node_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		f.NodeID = id
	}
	if v := strings.TrimSpace(r.URL.Query().Get("since")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid since", http.StatusBadRequest)
			return
		}
		f.Since = &t
	}
	alerts, total, err := s.store.ListAlerts(r.Context(), f, limit, offset)
	if err != nil {
		s.logger.Error("list alerts", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	items := make([]alertResponse, 0, len(alerts))
	for _, a := range alerts {
		items = append(items, newAlertResponse(a))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[alertResponse]{Data: items, Pagination: newPaginationMeta(total, limit, offset, len(items))})
}

func (s *Server) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	params := storage.CreateAlertParams{
		TenantID: tenantID,
		Source:   req.Source,
		Severity: req.Severity,
		Title:    req.Title,
		Summary:  req.Summary,
		DedupKey: req.DedupKey,
		Context:  req.Context,
	}
	if req.RuleID != nil && strings.TrimSpace(*req.RuleID) != "" {
		id, err := uuid.Parse(*req.RuleID)
		if err != nil {
			http.Error(w, "invalid rule_id", http.StatusBadRequest)
			return
		}
		params.RuleID = &id
	}
	if req.NodeID != nil && strings.TrimSpace(*req.NodeID) != "" {
		id, err := uuid.Parse(*req.NodeID)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		params.NodeID = &id
	}
	alert, err := s.store.CreateAlert(r.Context(), params)
	if err != nil {
		if errors.Is(err, storage.ErrAlertDeduped) {
			writeJSON(w, http.StatusOK, newAlertResponse(*alert))
			return
		}
		http.Error(w, fmt.Sprintf("create alert failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newAlertResponse(*alert))

	payload, _ := json.Marshal(map[string]any{
		"alert_id": alert.ID.String(),
		"severity": alert.Severity,
		"source":   alert.Source,
		"title":    alert.Title,
	})
	var nodeFilter *uuid.UUID
	if alert.NodeID.Valid {
		n := alert.NodeID.UUID
		nodeFilter = &n
	}
	s.publishEvent(eventbus.Event{
		Topic:    eventbus.TopicAlertOpened,
		TenantID: tenantID,
		NodeID:   nodeFilter,
		Payload:  payload,
	})
}

// userIDForPrincipalCtx resolves the user row id for the authenticated principal,
// or uuid.Nil if no user record is linked. Used for audit fields on ack/resolve.
func (s *Server) userIDForPrincipalCtx(ctx context.Context, principal *auth.Principal) uuid.UUID {
	if principal == nil || strings.TrimSpace(principal.Subject) == "" || s.store == nil {
		return uuid.Nil
	}
	user, err := s.store.GetUserByExternalID(ctx, principal.Subject)
	if err != nil || user == nil {
		return uuid.Nil
	}
	return user.ID
}
