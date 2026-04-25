package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/sessionrecording"
)

// handleSessionParsed opens the session artifact, decodes it into normalized
// events via sessionrecording.Parse, and optionally filters by command search.
// Streaming huge recordings through memory is fine for the go-live MVP — the
// API layer already caps artifact sizes at tens of MB.
func (s *Server) handleSessionParsed(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	events, err := s.loadSessionEvents(r, sessionID)
	if err != nil {
		s.writeSessionParseError(w, err)
		return
	}
	if search := strings.TrimSpace(r.URL.Query().Get("search")); search != "" {
		events = sessionrecording.SearchCommands(events, search)
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session_id": sessionID.String(),
		"data":       events,
		"count":      len(events),
	})
}

// handleSessionTranscript returns the output-only portion as text/plain, useful
// for download and indexing in log-forwarding pipelines.
func (s *Server) handleSessionTranscript(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	events, err := s.loadSessionEvents(r, sessionID)
	if err != nil {
		s.writeSessionParseError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(sessionrecording.Transcript(events)))
}

func (s *Server) loadSessionEvents(r *http.Request, sessionID uuid.UUID) ([]sessionrecording.Event, error) {
	if s.store == nil {
		return nil, errStoreUnavailable
	}
	session, err := s.store.GetSessionRecording(r.Context(), sessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, errSessionNotFound
	}
	if !session.ArtifactPath.Valid {
		return nil, errSessionNoArtifact
	}
	f, err := os.Open(session.ArtifactPath.String)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return sessionrecording.Parse(f)
}

var (
	errStoreUnavailable  = errors.New("storage unavailable")
	errSessionNotFound   = errors.New("session not found")
	errSessionNoArtifact = errors.New("session has no artifact to parse")
)

func (s *Server) writeSessionParseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSessionNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, errSessionNoArtifact):
		http.Error(w, err.Error(), http.StatusConflict)
	case errors.Is(err, errStoreUnavailable):
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
	default:
		if s.logger != nil {
			s.logger.Error("session parse failed", zap.Error(err))
		}
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}
