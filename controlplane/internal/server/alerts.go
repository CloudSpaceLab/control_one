package server

import (
	"context"
	"database/sql"
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
	ID          string                    `json:"id"`
	TenantID    string                    `json:"tenant_id"`
	RuleID      *string                   `json:"rule_id,omitempty"`
	NodeID      *string                   `json:"node_id,omitempty"`
	Source      string                    `json:"source"`
	Severity    string                    `json:"severity"`
	Title       string                    `json:"title"`
	Summary     *string                   `json:"summary,omitempty"`
	State       string                    `json:"state"`
	DedupKey    *string                   `json:"dedup_key,omitempty"`
	Context     map[string]any            `json:"context"`
	OpenedAt    string                    `json:"opened_at"`
	AckedAt     *string                   `json:"acked_at,omitempty"`
	AckedBy     *string                   `json:"acked_by,omitempty"`
	ResolvedAt  *string                   `json:"resolved_at,omitempty"`
	ResolvedBy  *string                   `json:"resolved_by,omitempty"`
	Disposition *alertDispositionResponse `json:"disposition,omitempty"`
}

type alertDispositionResponse struct {
	Value         string  `json:"value"`
	Reason        string  `json:"reason,omitempty"`
	UpdatedAt     string  `json:"updated_at,omitempty"`
	UpdatedBy     string  `json:"updated_by,omitempty"`
	SuppressUntil *string `json:"suppress_until,omitempty"`
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
	if disposition := alertDispositionFromContext(a.Context); disposition != nil {
		out.Disposition = disposition
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

type alertDispositionRequest struct {
	Disposition   string `json:"disposition"`
	Reason        string `json:"reason,omitempty"`
	SuppressUntil string `json:"suppress_until,omitempty"`
}

func (s *Server) handleAlertsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.handleListAlerts(w, r, principal)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		s.handleCreateAlert(w, r, principal)
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
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		a, ok := s.requireAlertTenantAccess(w, r, principal, id, roleViewer, roleOperator, roleInvestigator, roleAdmin)
		if !ok {
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
		if _, ok := s.requireAlertTenantAccess(w, r, principal, id, roleOperator, roleAdmin); !ok {
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
		if _, ok := s.requireAlertTenantAccess(w, r, principal, id, roleOperator, roleAdmin); !ok {
			return
		}
		userID := s.userIDForPrincipalCtx(r.Context(), principal)
		if err := s.store.ResolveAlert(r.Context(), id, userID); err != nil {
			http.Error(w, fmt.Sprintf("resolve failed: %v", err), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "disposition":
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleOperator)
		if !ok {
			return
		}
		if _, ok := s.requireAlertTenantAccess(w, r, principal, id, roleOperator, roleAdmin); !ok {
			return
		}
		var req alertDispositionRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		disposition := strings.ToLower(strings.TrimSpace(req.Disposition))
		if !validAlertDisposition(disposition) {
			http.Error(w, "invalid disposition", http.StatusBadRequest)
			return
		}
		reason := strings.TrimSpace(req.Reason)
		if reason == "" {
			http.Error(w, "reason is required", http.StatusBadRequest)
			return
		}
		var suppressUntil *time.Time
		if value := strings.TrimSpace(req.SuppressUntil); value != "" {
			parsed, err := time.Parse(time.RFC3339, value)
			if err != nil {
				http.Error(w, "invalid suppress_until", http.StatusBadRequest)
				return
			}
			parsed = parsed.UTC()
			if !parsed.After(time.Now().UTC()) {
				http.Error(w, "suppress_until must be in the future", http.StatusBadRequest)
				return
			}
			suppressUntil = &parsed
		}
		if disposition == "suppressed" && suppressUntil == nil {
			http.Error(w, "suppress_until is required for suppressed disposition", http.StatusBadRequest)
			return
		}
		userID := s.userIDForPrincipalCtx(r.Context(), principal)
		alert, err := s.store.UpdateAlertDisposition(r.Context(), id, storage.UpdateAlertDispositionParams{
			Disposition:   disposition,
			Reason:        reason,
			SuppressUntil: suppressUntil,
			By:            userID,
			At:            time.Now().UTC(),
		})
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				http.NotFound(w, r)
				return
			}
			http.Error(w, fmt.Sprintf("update disposition failed: %v", err), http.StatusBadRequest)
			return
		}
		s.recordAudit(r.Context(), principal, alert.TenantID, "alert.disposition_updated", "alert", alert.ID.String(), map[string]any{
			"disposition": disposition,
			"reason":      reason,
		})
		writeJSON(w, http.StatusOK, newAlertResponse(*alert))
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) requireAlertTenantAccess(w http.ResponseWriter, r *http.Request, principal *auth.Principal, id uuid.UUID, roles ...string) (*storage.Alert, bool) {
	a, err := s.store.GetAlert(r.Context(), id)
	if err != nil {
		s.logger.Error("get alert", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return nil, false
	}
	if a == nil {
		http.NotFound(w, r)
		return nil, false
	}
	if !s.requireTenantAccess(w, r, principal, a.TenantID, roles...) {
		return nil, false
	}
	return a, true
}

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
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
	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tid, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	f.TenantID = tid
	if !s.requireTenantAccess(w, r, principal, tid, roleViewer, roleOperator, roleInvestigator, roleAdmin) {
		return
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

func (s *Server) handleCreateAlert(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
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
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleAdmin) {
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
		node, err := s.store.GetNode(r.Context(), id)
		if err != nil {
			s.logger.Error("get alert node", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if node == nil || node.TenantID != tenantID {
			http.Error(w, "node does not belong to tenant", http.StatusBadRequest)
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

func validAlertDisposition(disposition string) bool {
	switch strings.ToLower(strings.TrimSpace(disposition)) {
	case "true_positive", "false_positive", "benign_positive", "accepted_risk", "suppressed", "resolved":
		return true
	default:
		return false
	}
}

func alertDispositionFromContext(contextMap map[string]any) *alertDispositionResponse {
	raw := metadataMap(contextMap["disposition"])
	if len(raw) == 0 {
		return nil
	}
	value := strings.TrimSpace(detailsString(raw, "value", ""))
	if value == "" {
		return nil
	}
	resp := &alertDispositionResponse{
		Value:     value,
		Reason:    strings.TrimSpace(detailsString(raw, "reason", "")),
		UpdatedAt: strings.TrimSpace(detailsString(raw, "updated_at", "")),
		UpdatedBy: strings.TrimSpace(detailsString(raw, "updated_by", "")),
	}
	if suppressUntil := strings.TrimSpace(detailsString(raw, "suppress_until", "")); suppressUntil != "" {
		resp.SuppressUntil = &suppressUntil
	}
	return resp
}

// userIDForPrincipalCtx resolves the user row id for the authenticated principal,
// or uuid.Nil if no user record is linked. Used for audit fields on ack/resolve.
func (s *Server) userIDForPrincipalCtx(ctx context.Context, principal *auth.Principal) uuid.UUID {
	if principal == nil || strings.TrimSpace(principal.Subject) == "" || s.store == nil {
		return uuid.Nil
	}
	if id, err := uuid.Parse(strings.TrimSpace(principal.Subject)); err == nil && id != uuid.Nil {
		if user, err := s.store.GetUser(ctx, id); err == nil && user != nil {
			return user.ID
		}
	}
	user, err := s.store.GetUserByExternalID(ctx, principal.Subject)
	if err != nil || user == nil {
		return uuid.Nil
	}
	return user.ID
}
