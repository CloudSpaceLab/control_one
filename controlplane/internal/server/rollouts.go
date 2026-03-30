package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type rolloutResponse struct {
	ID                string         `json:"id"`
	TemplateVersionID string         `json:"template_version_id"`
	TargetPercent     int            `json:"target_percent"`
	State             string         `json:"state"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	ScheduledFor      *string        `json:"scheduled_for,omitempty"`
	CompletedAt       *string        `json:"completed_at,omitempty"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type createRolloutRequest struct {
	TemplateVersionID string         `json:"template_version_id"`
	TargetPercent     int            `json:"target_percent"`
	State             *string        `json:"state,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	ScheduledFor      *string        `json:"scheduled_for,omitempty"`
}

type updateRolloutRequest struct {
	State         *string         `json:"state,omitempty"`
	TargetPercent *int            `json:"target_percent,omitempty"`
	Metadata      *map[string]any `json:"metadata,omitempty"`
	CompletedAt   *string         `json:"completed_at,omitempty"`
}

func (s *Server) handleTemplateRollouts(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListRollouts(w, r, templateID)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateRollout(w, r, templateID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleRolloutResource(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, rolloutID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetRollout(w, r, rolloutID)
	default:
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleCancelRollout(w http.ResponseWriter, r *http.Request, templateID uuid.UUID, rolloutID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	rollout, err := s.store.GetRollout(r.Context(), rolloutID)
	if err != nil {
		s.logger.Error("get rollout for cancel", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if rollout == nil {
		http.NotFound(w, r)
		return
	}

	if rollout.State == "completed" || rollout.State == "cancelled" {
		writeError(w, r, http.StatusConflict, "rollout cannot be cancelled in its current state")
		return
	}

	now := time.Now()
	params := storage.UpdateRolloutParams{
		State:       stringPtr("cancelled"),
		CompletedAt: &now,
	}

	updated, err := s.store.UpdateRollout(r.Context(), rolloutID, params)
	if err != nil {
		s.logger.Error("cancel rollout", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newRolloutResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "rollout.cancelled", "rollout", rolloutID.String(), map[string]any{
		"template_id": templateID.String(),
	})
}

func (s *Server) handleListRollouts(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	rollouts, total, err := s.store.ListRollouts(r.Context(), templateID, limit, offset)
	if err != nil {
		s.logger.Error("list rollouts", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	respItems := make([]rolloutResponse, 0, len(rollouts))
	for _, rollout := range rollouts {
		respItems = append(respItems, newRolloutResponse(rollout))
	}

	resp := paginatedResponse[rolloutResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateRollout(w http.ResponseWriter, r *http.Request, templateID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req createRolloutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	versionID, err := uuid.Parse(req.TemplateVersionID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid template_version_id")
		return
	}

	if req.TargetPercent < 0 || req.TargetPercent > 100 {
		writeError(w, r, http.StatusBadRequest, "target_percent must be between 0 and 100")
		return
	}

	state := "scheduled"
	if req.State != nil {
		state = strings.TrimSpace(*req.State)
		if state == "" {
			state = "scheduled"
		}
	}

	var scheduledFor *time.Time
	if req.ScheduledFor != nil {
		ts, err := time.Parse(time.RFC3339, *req.ScheduledFor)
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "invalid scheduled_for timestamp (use RFC3339)")
			return
		}
		scheduledFor = &ts
	}

	params := storage.CreateRolloutParams{
		TemplateVersionID: versionID,
		TargetPercent:    req.TargetPercent,
		State:            state,
		Metadata:         req.Metadata,
		ScheduledFor:     scheduledFor,
	}
	if params.Metadata == nil {
		params.Metadata = make(map[string]any)
	}

	created, err := s.store.CreateRollout(r.Context(), params)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("create rollout failed: %v", err))
		return
	}

	resp := newRolloutResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "rollout.created", "rollout", created.ID.String(), map[string]any{
		"template_id":         templateID.String(),
		"template_version_id": versionID.String(),
		"target_percent":      req.TargetPercent,
	})
}

func (s *Server) handleGetRollout(w http.ResponseWriter, r *http.Request, rolloutID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	rollout, err := s.store.GetRollout(r.Context(), rolloutID)
	if err != nil {
		s.logger.Error("get rollout", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if rollout == nil {
		http.NotFound(w, r)
		return
	}

	resp := newRolloutResponse(*rollout)
	writeJSON(w, http.StatusOK, resp)
}

func newRolloutResponse(r storage.TemplateRollout) rolloutResponse {
	resp := rolloutResponse{
		ID:                r.ID.String(),
		TemplateVersionID: r.TemplateVersionID.String(),
		TargetPercent:     r.TargetPercent,
		State:             r.State,
		Metadata:          r.Metadata,
		CreatedAt:         formatTime(r.CreatedAt),
		UpdatedAt:         formatTime(r.UpdatedAt),
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}
	if r.ScheduledFor.Valid {
		ts := r.ScheduledFor.Time.UTC().Format(time.RFC3339)
		resp.ScheduledFor = &ts
	}
	if r.CompletedAt.Valid {
		ts := r.CompletedAt.Time.UTC().Format(time.RFC3339)
		resp.CompletedAt = &ts
	}
	return resp
}

func stringPtr(s string) *string {
	return &s
}


