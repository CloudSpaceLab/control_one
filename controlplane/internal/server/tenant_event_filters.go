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

// handleTenantEventFilters serves
//
//	GET  /api/v1/tenants/{id}/event-filters
//	PUT  /api/v1/tenants/{id}/event-filters
//
// Operators tune which events the agent captures and whether forensic mode
// is on. The agent receives the latest policy on its next heartbeat and
// reconfigures collectors at runtime — no restart.
func (s *Server) handleTenantEventFilters(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	// /api/v1/tenants/{id}/event-filters
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[1] != "event-filters" {
		http.NotFound(w, r)
		return
	}
	tenantID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		f, err := s.store.GetTenantEventFilters(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("get tenant event filters", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, f)
	case http.MethodPut:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var body struct {
			CaptureExternal         *bool     `json:"capture_external"`
			CaptureInternalSummary  *bool     `json:"capture_internal_summary"`
			CaptureListeningChanges *bool     `json:"capture_listening_changes"`
			CaptureFiles            *bool     `json:"capture_files"`
			CaptureDBQueries        *bool     `json:"capture_db_queries"`
			ThreatMatchFull         *bool     `json:"threat_match_full"`
			FilePathsWatch          *[]string `json:"file_paths_watch"`
			FileSizeMinBytes        *int64    `json:"file_size_min_bytes"`
			AllowlistCIDRs          *[]string `json:"allowlist_cidrs"`
			DenylistCIDRs           *[]string `json:"denylist_cidrs"`
			TrustedProxyCIDRs       *[]string `json:"trusted_proxy_cidrs"`
			DBQueryTextCapture      *bool     `json:"db_query_text_capture"`
			ForensicMode            *bool     `json:"forensic_mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		f, err := s.store.GetTenantEventFilters(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("get tenant event filters", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		applyTenantEventFilters(f, body)
		f.TenantID = tenantID
		if err := s.store.UpsertTenantEventFilters(r.Context(), *f); err != nil {
			http.Error(w, fmt.Sprintf("upsert failed: %v", err), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, f)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func applyTenantEventFilters(f *storage.TenantEventFilters, b struct {
	CaptureExternal         *bool     `json:"capture_external"`
	CaptureInternalSummary  *bool     `json:"capture_internal_summary"`
	CaptureListeningChanges *bool     `json:"capture_listening_changes"`
	CaptureFiles            *bool     `json:"capture_files"`
	CaptureDBQueries        *bool     `json:"capture_db_queries"`
	ThreatMatchFull         *bool     `json:"threat_match_full"`
	FilePathsWatch          *[]string `json:"file_paths_watch"`
	FileSizeMinBytes        *int64    `json:"file_size_min_bytes"`
	AllowlistCIDRs          *[]string `json:"allowlist_cidrs"`
	DenylistCIDRs           *[]string `json:"denylist_cidrs"`
	TrustedProxyCIDRs       *[]string `json:"trusted_proxy_cidrs"`
	DBQueryTextCapture      *bool     `json:"db_query_text_capture"`
	ForensicMode            *bool     `json:"forensic_mode"`
}) {
	if b.CaptureExternal != nil {
		f.CaptureExternal = *b.CaptureExternal
	}
	if b.CaptureInternalSummary != nil {
		f.CaptureInternalSummary = *b.CaptureInternalSummary
	}
	if b.CaptureListeningChanges != nil {
		f.CaptureListeningChanges = *b.CaptureListeningChanges
	}
	if b.CaptureFiles != nil {
		f.CaptureFiles = *b.CaptureFiles
	}
	if b.CaptureDBQueries != nil {
		f.CaptureDBQueries = *b.CaptureDBQueries
	}
	if b.ThreatMatchFull != nil {
		f.ThreatMatchFull = *b.ThreatMatchFull
	}
	if b.FilePathsWatch != nil {
		f.FilePathsWatch = *b.FilePathsWatch
	}
	if b.FileSizeMinBytes != nil {
		f.FileSizeMinBytes = *b.FileSizeMinBytes
	}
	if b.AllowlistCIDRs != nil {
		f.AllowlistCIDRs = *b.AllowlistCIDRs
	}
	if b.DenylistCIDRs != nil {
		f.DenylistCIDRs = *b.DenylistCIDRs
	}
	if b.TrustedProxyCIDRs != nil {
		f.TrustedProxyCIDRs = *b.TrustedProxyCIDRs
	}
	if b.DBQueryTextCapture != nil {
		f.DBQueryTextCapture = *b.DBQueryTextCapture
	}
	if b.ForensicMode != nil {
		f.ForensicMode = *b.ForensicMode
	}
}
