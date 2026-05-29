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

// Threat-feed CRUD lets operators manage which IP/abuse data sources the
// platform consumes. Built-in sources (Spamhaus, FireHOL, Tor) need only a
// name; AbuseIPDB can use a sealed API key to refresh its upstream blocklist
// but request-path checks read the downloaded local snapshot; OTX still needs
// an API key. Custom feeds accept any URL serving line-delimited or
// Spamhaus-format text.

type threatFeedResponse struct {
	ID                 string  `json:"id"`
	TenantID           string  `json:"tenant_id"`
	Name               string  `json:"name"`
	FeedType           string  `json:"feed_type"`
	URL                *string `json:"url,omitempty"`
	HasAPIKey          bool    `json:"has_api_key"`
	ScoreFloor         int     `json:"score_floor"`
	RefreshSeconds     int     `json:"refresh_seconds"`
	Category           *string `json:"category,omitempty"`
	Enabled            bool    `json:"enabled"`
	LastStatus         *string `json:"last_status,omitempty"`
	LastError          *string `json:"last_error,omitempty"`
	LastIndicatorCount int     `json:"last_indicator_count"`
	LastRefreshedAt    *string `json:"last_refreshed_at,omitempty"`
	CreatedAt          string  `json:"created_at"`
	UpdatedAt          string  `json:"updated_at"`
}

func newThreatFeedResponse(f storage.ThreatFeed) threatFeedResponse {
	out := threatFeedResponse{
		ID:                 f.ID.String(),
		TenantID:           f.TenantID.String(),
		Name:               f.Name,
		FeedType:           f.FeedType,
		HasAPIKey:          len(f.APIKeySealed) > 0,
		ScoreFloor:         f.ScoreFloor,
		RefreshSeconds:     f.RefreshSeconds,
		Enabled:            f.Enabled,
		LastIndicatorCount: f.LastIndicatorCount,
		CreatedAt:          formatTime(f.CreatedAt),
		UpdatedAt:          formatTime(f.UpdatedAt),
	}
	if f.URL.Valid {
		s := f.URL.String
		out.URL = &s
	}
	if f.Category.Valid {
		s := f.Category.String
		out.Category = &s
	}
	if f.LastStatus.Valid {
		s := f.LastStatus.String
		out.LastStatus = &s
	}
	if f.LastError.Valid {
		s := f.LastError.String
		out.LastError = &s
	}
	if f.LastRefreshedAt.Valid {
		s := formatTime(f.LastRefreshedAt.Time)
		out.LastRefreshedAt = &s
	}
	return out
}

type createThreatFeedRequest struct {
	TenantID       string `json:"tenant_id"`
	Name           string `json:"name"`
	FeedType       string `json:"feed_type"`
	URL            string `json:"url"`
	APIKey         string `json:"api_key"`
	ScoreFloor     int    `json:"score_floor"`
	RefreshSeconds int    `json:"refresh_seconds"`
	Category       string `json:"category"`
	Enabled        *bool  `json:"enabled"`
}

type updateThreatFeedRequest struct {
	Name           *string `json:"name"`
	URL            *string `json:"url"`
	APIKey         *string `json:"api_key"`
	ClearAPIKey    bool    `json:"clear_api_key"`
	ScoreFloor     *int    `json:"score_floor"`
	RefreshSeconds *int    `json:"refresh_seconds"`
	Category       *string `json:"category"`
	Enabled        *bool   `json:"enabled"`
}

func (s *Server) handleThreatFeedsCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.listThreatFeeds(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.createThreatFeed(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listThreatFeeds(w http.ResponseWriter, r *http.Request) {
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	feeds, err := s.store.ListThreatFeeds(r.Context(), storage.ThreatFeedFilter{TenantID: tenantID})
	if err != nil {
		s.logger.Error("list threat feeds", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	out := make([]threatFeedResponse, 0, len(feeds))
	for _, f := range feeds {
		out = append(out, newThreatFeedResponse(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (s *Server) createThreatFeed(w http.ResponseWriter, r *http.Request) {
	var req createThreatFeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	if req.FeedType == "" || req.Name == "" {
		http.Error(w, "name and feed_type required", http.StatusBadRequest)
		return
	}
	if req.FeedType == "otx" && strings.TrimSpace(req.APIKey) == "" {
		http.Error(w, "api_key required for "+req.FeedType, http.StatusBadRequest)
		return
	}

	var sealed, nonce []byte
	if strings.TrimSpace(req.APIKey) != "" {
		if s.sealer == nil {
			http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
			return
		}
		var serr error
		sealed, nonce, serr = s.sealer.Seal([]byte(req.APIKey))
		if serr != nil {
			http.Error(w, fmt.Sprintf("seal api_key: %v", serr), http.StatusInternalServerError)
			return
		}
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	score := req.ScoreFloor
	if score == 0 {
		score = 50
	}
	refresh := req.RefreshSeconds
	if refresh == 0 {
		refresh = 3600
	}

	feed, err := s.store.CreateThreatFeed(r.Context(), storage.CreateThreatFeedParams{
		TenantID: tenantID, Name: req.Name, FeedType: req.FeedType,
		URL: req.URL, APIKeySealed: sealed, Nonce: nonce,
		ScoreFloor: score, RefreshSeconds: refresh, Category: req.Category, Enabled: enabled,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newThreatFeedResponse(*feed))
}

func (s *Server) handleThreatFeedSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/threat-feeds/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid feed id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		feed, err := s.store.GetThreatFeed(r.Context(), id)
		if err != nil {
			s.logger.Error("get threat feed", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if feed == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newThreatFeedResponse(*feed))
	case http.MethodPatch, http.MethodPut:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.updateThreatFeed(w, r, id)
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		if err := s.store.DeleteThreatFeed(r.Context(), id); err != nil {
			http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) updateThreatFeed(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req updateThreatFeedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	params := storage.UpdateThreatFeedParams{
		Name:           req.Name,
		URL:            req.URL,
		ScoreFloor:     req.ScoreFloor,
		RefreshSeconds: req.RefreshSeconds,
		Category:       req.Category,
		Enabled:        req.Enabled,
		ClearAPIKey:    req.ClearAPIKey,
	}
	if req.APIKey != nil && strings.TrimSpace(*req.APIKey) != "" {
		if s.sealer == nil {
			http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
			return
		}
		sealed, nonce, err := s.sealer.Seal([]byte(*req.APIKey))
		if err != nil {
			http.Error(w, fmt.Sprintf("seal: %v", err), http.StatusInternalServerError)
			return
		}
		params.APIKeySealed = sealed
		params.Nonce = nonce
	}
	updated, err := s.store.UpdateThreatFeed(r.Context(), id, params)
	if err != nil {
		http.Error(w, fmt.Sprintf("update failed: %v", err), http.StatusBadRequest)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, newThreatFeedResponse(*updated))
}
