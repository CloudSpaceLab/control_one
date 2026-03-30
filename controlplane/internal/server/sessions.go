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

type sessionRecordingResponse struct {
	ID              string                 `json:"id"`
	NodeID          string                 `json:"node_id"`
	UserID          *string                `json:"user_id,omitempty"`
	SessionType     string                 `json:"session_type"`
	StartedAt       string                 `json:"started_at"`
	EndedAt         *string                `json:"ended_at,omitempty"`
	DurationSeconds *int64                 `json:"duration_seconds,omitempty"`
	Status          string                 `json:"status"`
	Metadata        map[string]any         `json:"metadata,omitempty"`
	ArtifactPath    *string                `json:"artifact_path,omitempty"`
	ArtifactSizeBytes *int64               `json:"artifact_size_bytes,omitempty"`
	Checksum        *string                `json:"checksum,omitempty"`
	CreatedAt       string                 `json:"created_at"`
	UpdatedAt       string                 `json:"updated_at"`
}

type createSessionRecordingRequest struct {
	NodeID      string                 `json:"node_id"`
	UserID      *string                `json:"user_id,omitempty"`
	SessionType string                 `json:"session_type"`
	StartedAt   *string                `json:"started_at,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Metadata    map[string]any         `json:"metadata,omitempty"`
}

type updateSessionRecordingRequest struct {
	EndedAt         *string                `json:"ended_at,omitempty"`
	DurationSeconds *int64                 `json:"duration_seconds,omitempty"`
	Status          *string                `json:"status,omitempty"`
	ArtifactPath    *string                `json:"artifact_path,omitempty"`
	ArtifactSizeBytes *int64               `json:"artifact_size_bytes,omitempty"`
	Checksum        *string                `json:"checksum,omitempty"`
	Metadata        map[string]any         `json:"metadata,omitempty"`
}

func (s *Server) handleSessionsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListSessions(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		s.handleCreateSession(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleSessionSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	segments := strings.Split(trimmed, "/")
	sessionID, err := uuid.Parse(segments[0])
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid session id")
		return
	}

	if len(segments) == 1 {
		s.handleSessionResource(w, r, sessionID)
		return
	}

	if len(segments) == 2 {
		switch segments[1] {
		case "replay":
			s.handleSessionReplay(w, r, sessionID)
			return
		case "download":
			s.handleSessionDownload(w, r, sessionID)
			return
		case "events":
			s.handleSessionEvents(w, r, sessionID)
			return
		}
	}

	http.NotFound(w, r)
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	params := storage.ListSessionRecordingsParams{}

	if nodeIDStr := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeIDStr != "" {
		if nodeID, err := uuid.Parse(nodeIDStr); err == nil {
			params.NodeID = &nodeID
		}
	}
	if userIDStr := strings.TrimSpace(r.URL.Query().Get("user_id")); userIDStr != "" {
		params.UserID = &userIDStr
	}
	if sessionType := strings.TrimSpace(r.URL.Query().Get("session_type")); sessionType != "" {
		params.SessionType = &sessionType
	}
	if status := strings.TrimSpace(r.URL.Query().Get("status")); status != "" {
		params.Status = &status
	}
	if startTimeStr := strings.TrimSpace(r.URL.Query().Get("start_time")); startTimeStr != "" {
		if startTime, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			params.StartTime = &startTime
		}
	}
	if endTimeStr := strings.TrimSpace(r.URL.Query().Get("end_time")); endTimeStr != "" {
		if endTime, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			params.EndTime = &endTime
		}
	}

	sessions, total, err := s.store.ListSessionRecordings(r.Context(), params, limit, offset)
	if err != nil {
		s.logger.Error("list session recordings", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	respItems := make([]sessionRecordingResponse, 0, len(sessions))
	for _, session := range sessions {
		respItems = append(respItems, newSessionRecordingResponse(session))
	}

	resp := paginatedResponse[sessionRecordingResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	var req createSessionRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	if strings.TrimSpace(req.NodeID) == "" {
		writeError(w, r, http.StatusBadRequest, "node_id is required")
		return
	}

	nodeID, err := uuid.Parse(req.NodeID)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid node_id")
		return
	}

	startedAt := time.Now()
	if req.StartedAt != nil {
		if parsed, err := time.Parse(time.RFC3339, *req.StartedAt); err == nil {
			startedAt = parsed
		}
	}

	sessionType := req.SessionType
	if sessionType == "" {
		sessionType = "terminal"
	}

	status := req.Status
	if status == "" {
		status = "active"
	}

	params := storage.CreateSessionRecordingParams{
		NodeID:      nodeID,
		UserID:      req.UserID,
		SessionType: sessionType,
		StartedAt:   startedAt,
		Status:      status,
		Metadata:    req.Metadata,
	}

	created, err := s.store.CreateSessionRecording(r.Context(), params)
	if err != nil {
		s.logger.Error("create session recording", zap.Error(err))
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("create session recording failed: %v", err))
		return
	}

	resp := newSessionRecordingResponse(*created)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "session.created", "session", created.ID.String(), map[string]any{
		"node_id":      nodeID.String(),
		"session_type": sessionType,
	})
}

func (s *Server) handleSessionResource(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleGetSession(w, r, sessionID)
	case http.MethodPatch:
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		s.handleUpdateSession(w, r, sessionID)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch}, ", "))
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
	}
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	session, err := s.store.GetSessionRecording(r.Context(), sessionID)
	if err != nil {
		s.logger.Error("get session recording", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}
	if session == nil {
		http.NotFound(w, r)
		return
	}

	resp := newSessionRecordingResponse(*session)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	principal, ok := s.authorize(w, r, roleOperator)
	if !ok {
		return
	}

	var req updateSessionRecordingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	params := storage.UpdateSessionRecordingParams{}

	if req.EndedAt != nil {
		if endedAt, err := time.Parse(time.RFC3339, *req.EndedAt); err == nil {
			params.EndedAt = &endedAt
		}
	}
	params.DurationSeconds = req.DurationSeconds
	params.Status = req.Status
	params.ArtifactPath = req.ArtifactPath
	params.ArtifactSizeBytes = req.ArtifactSizeBytes
	params.Checksum = req.Checksum
	params.Metadata = req.Metadata

	updated, err := s.store.UpdateSessionRecording(r.Context(), sessionID, params)
	if err != nil {
		s.logger.Error("update session recording", zap.Error(err))
		writeError(w, r, http.StatusBadRequest, fmt.Sprintf("update session recording failed: %v", err))
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	resp := newSessionRecordingResponse(*updated)
	writeJSON(w, http.StatusOK, resp)

	s.recordAudit(r.Context(), principal, uuid.Nil, "session.updated", "session", sessionID.String(), map[string]any{})
}

func (s *Server) handleSessionReplay(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	session, err := s.store.GetSessionRecording(r.Context(), sessionID)
	if err != nil || session == nil {
		http.NotFound(w, r)
		return
	}

	if !session.ArtifactPath.Valid {
		writeError(w, r, http.StatusNotFound, "session artifact not available")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=session-%s.replay", sessionID.String()))
	http.ServeFile(w, r, session.ArtifactPath.String)
}

func (s *Server) handleSessionDownload(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	session, err := s.store.GetSessionRecording(r.Context(), sessionID)
	if err != nil || session == nil {
		http.NotFound(w, r)
		return
	}

	if !session.ArtifactPath.Valid {
		writeError(w, r, http.StatusNotFound, "session artifact not available")
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=session-%s.tar.gz", sessionID.String()))
	http.ServeFile(w, r, session.ArtifactPath.String)
}

func (s *Server) handleSessionEvents(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, r, http.StatusMethodNotAllowed, http.StatusText(http.StatusMethodNotAllowed))
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	if s.store == nil {
		writeError(w, r, http.StatusServiceUnavailable, "storage unavailable")
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		writeError(w, r, http.StatusBadRequest, err.Error())
		return
	}

	events, total, err := s.store.ListSessionEvents(r.Context(), sessionID, limit, offset)
	if err != nil {
		s.logger.Error("list session events", zap.Error(err))
		writeError(w, r, http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
		return
	}

	type eventResponse struct {
		ID        string                 `json:"id"`
		SessionID string                 `json:"session_id"`
		EventType string                 `json:"event_type"`
		Timestamp string                 `json:"timestamp"`
		Data      map[string]any         `json:"data,omitempty"`
		CreatedAt string                 `json:"created_at"`
	}

	respItems := make([]eventResponse, 0, len(events))
	for _, event := range events {
		respItems = append(respItems, eventResponse{
			ID:        event.ID.String(),
			SessionID: event.SessionID.String(),
			EventType: event.EventType,
			Timestamp: formatTime(event.Timestamp),
			Data:      event.Data,
			CreatedAt: formatTime(event.CreatedAt),
		})
	}

	resp := paginatedResponse[eventResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func newSessionRecordingResponse(session storage.SessionRecording) sessionRecordingResponse {
	resp := sessionRecordingResponse{
		ID:          session.ID.String(),
		NodeID:      session.NodeID.String(),
		SessionType: session.SessionType,
		StartedAt:   formatTime(session.StartedAt),
		Status:      session.Status,
		Metadata:    session.Metadata,
		CreatedAt:   formatTime(session.CreatedAt),
		UpdatedAt:   formatTime(session.UpdatedAt),
	}

	if session.UserID.Valid {
		resp.UserID = &session.UserID.String
	}
	if session.EndedAt.Valid {
		endedAt := formatNullTime(session.EndedAt)
		resp.EndedAt = endedAt
	}
	if session.DurationSeconds.Valid {
		resp.DurationSeconds = &session.DurationSeconds.Int64
	}
	if session.ArtifactPath.Valid {
		resp.ArtifactPath = &session.ArtifactPath.String
	}
	if session.ArtifactSizeBytes.Valid {
		resp.ArtifactSizeBytes = &session.ArtifactSizeBytes.Int64
	}
	if session.Checksum.Valid {
		resp.Checksum = &session.Checksum.String
	}
	if resp.Metadata == nil {
		resp.Metadata = make(map[string]any)
	}

	return resp
}

