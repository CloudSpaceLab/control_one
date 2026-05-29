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

type tenantConnectorPolicyResponse struct {
	TenantID                 string   `json:"tenant_id"`
	AllowMediumRisk          bool     `json:"allow_medium_risk"`
	AllowHighRisk            bool     `json:"allow_high_risk"`
	AutoConnectPrograms      []string `json:"auto_connect_programs"`
	ApprovalRequiredPrograms []string `json:"approval_required_programs"`
	BlockedPrograms          []string `json:"blocked_programs"`
	UpdatedAt                string   `json:"updated_at,omitempty"`
}

type tenantConnectorPolicyPatch struct {
	AllowMediumRisk          *bool     `json:"allow_medium_risk"`
	AllowHighRisk            *bool     `json:"allow_high_risk"`
	AutoConnectPrograms      *[]string `json:"auto_connect_programs"`
	ApprovalRequiredPrograms *[]string `json:"approval_required_programs"`
	BlockedPrograms          *[]string `json:"blocked_programs"`
}

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
			CaptureExternal                   *bool     `json:"capture_external"`
			CaptureInternalSummary            *bool     `json:"capture_internal_summary"`
			CaptureListeningChanges           *bool     `json:"capture_listening_changes"`
			CaptureFiles                      *bool     `json:"capture_files"`
			CaptureDBQueries                  *bool     `json:"capture_db_queries"`
			ThreatMatchFull                   *bool     `json:"threat_match_full"`
			FilePathsWatch                    *[]string `json:"file_paths_watch"`
			FileSizeMinBytes                  *int64    `json:"file_size_min_bytes"`
			AllowlistCIDRs                    *[]string `json:"allowlist_cidrs"`
			DenylistCIDRs                     *[]string `json:"denylist_cidrs"`
			TrustedProxyCIDRs                 *[]string `json:"trusted_proxy_cidrs"`
			DBQueryTextCapture                *bool     `json:"db_query_text_capture"`
			ForensicMode                      *bool     `json:"forensic_mode"`
			ConnectorAutoConnectMediumRisk    *bool     `json:"connector_auto_connect_medium_risk"`
			ConnectorAutoConnectHighRisk      *bool     `json:"connector_auto_connect_high_risk"`
			ConnectorAutoConnectPrograms      *[]string `json:"connector_auto_connect_programs"`
			ConnectorApprovalRequiredPrograms *[]string `json:"connector_approval_required_programs"`
			ConnectorBlockedPrograms          *[]string `json:"connector_blocked_programs"`
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

// handleTenantConnectorPolicy serves
//
//	GET  /api/v1/tenants/{id}/connector-policy
//	PUT  /api/v1/tenants/{id}/connector-policy
//
// This is the operator-facing view over the connector subset of tenant event
// filters. The same stored row is still delivered to agents through heartbeat.
func (s *Server) handleTenantConnectorPolicy(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[1] != "connector-policy" {
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
			s.logger.Error("get tenant connector policy", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, tenantConnectorPolicyResponseFromFilters(f))
	case http.MethodPut:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		var body tenantConnectorPolicyPatch
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		f, err := s.store.GetTenantEventFilters(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("get tenant connector policy", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		applyTenantConnectorPolicyPatch(f, body)
		f.TenantID = tenantID
		if err := s.store.UpsertTenantEventFilters(r.Context(), *f); err != nil {
			http.Error(w, fmt.Sprintf("upsert failed: %v", err), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, tenantID, "tenant.connector_policy.update", "tenant", tenantID.String(), map[string]any{
			"allow_medium_risk":          f.ConnectorAutoConnectMediumRisk,
			"allow_high_risk":            f.ConnectorAutoConnectHighRisk,
			"auto_connect_programs":      sanitizeStringSlice(f.ConnectorAutoConnectPrograms, 128),
			"approval_required_programs": sanitizeStringSlice(f.ConnectorApprovalRequiredPrograms, 128),
			"blocked_programs":           sanitizeStringSlice(f.ConnectorBlockedPrograms, 128),
		})
		writeJSON(w, http.StatusOK, tenantConnectorPolicyResponseFromFilters(f))
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func tenantConnectorPolicyResponseFromFilters(f *storage.TenantEventFilters) tenantConnectorPolicyResponse {
	if f == nil {
		return tenantConnectorPolicyResponse{}
	}
	resp := tenantConnectorPolicyResponse{
		TenantID:                 f.TenantID.String(),
		AllowMediumRisk:          f.ConnectorAutoConnectMediumRisk,
		AllowHighRisk:            f.ConnectorAutoConnectHighRisk,
		AutoConnectPrograms:      sanitizeStringSlice(f.ConnectorAutoConnectPrograms, 128),
		ApprovalRequiredPrograms: sanitizeStringSlice(f.ConnectorApprovalRequiredPrograms, 128),
		BlockedPrograms:          sanitizeStringSlice(f.ConnectorBlockedPrograms, 128),
	}
	if !f.UpdatedAt.IsZero() {
		resp.UpdatedAt = f.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

func applyTenantConnectorPolicyPatch(f *storage.TenantEventFilters, body tenantConnectorPolicyPatch) {
	if f == nil {
		return
	}
	if body.AllowMediumRisk != nil {
		f.ConnectorAutoConnectMediumRisk = *body.AllowMediumRisk
	}
	if body.AllowHighRisk != nil {
		f.ConnectorAutoConnectHighRisk = *body.AllowHighRisk
	}
	if body.AutoConnectPrograms != nil {
		f.ConnectorAutoConnectPrograms = sanitizeStringSlice(*body.AutoConnectPrograms, 128)
	}
	if body.ApprovalRequiredPrograms != nil {
		f.ConnectorApprovalRequiredPrograms = sanitizeStringSlice(*body.ApprovalRequiredPrograms, 128)
	}
	if body.BlockedPrograms != nil {
		f.ConnectorBlockedPrograms = sanitizeStringSlice(*body.BlockedPrograms, 128)
	}
}

func applyTenantEventFilters(f *storage.TenantEventFilters, b struct {
	CaptureExternal                   *bool     `json:"capture_external"`
	CaptureInternalSummary            *bool     `json:"capture_internal_summary"`
	CaptureListeningChanges           *bool     `json:"capture_listening_changes"`
	CaptureFiles                      *bool     `json:"capture_files"`
	CaptureDBQueries                  *bool     `json:"capture_db_queries"`
	ThreatMatchFull                   *bool     `json:"threat_match_full"`
	FilePathsWatch                    *[]string `json:"file_paths_watch"`
	FileSizeMinBytes                  *int64    `json:"file_size_min_bytes"`
	AllowlistCIDRs                    *[]string `json:"allowlist_cidrs"`
	DenylistCIDRs                     *[]string `json:"denylist_cidrs"`
	TrustedProxyCIDRs                 *[]string `json:"trusted_proxy_cidrs"`
	DBQueryTextCapture                *bool     `json:"db_query_text_capture"`
	ForensicMode                      *bool     `json:"forensic_mode"`
	ConnectorAutoConnectMediumRisk    *bool     `json:"connector_auto_connect_medium_risk"`
	ConnectorAutoConnectHighRisk      *bool     `json:"connector_auto_connect_high_risk"`
	ConnectorAutoConnectPrograms      *[]string `json:"connector_auto_connect_programs"`
	ConnectorApprovalRequiredPrograms *[]string `json:"connector_approval_required_programs"`
	ConnectorBlockedPrograms          *[]string `json:"connector_blocked_programs"`
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
	if b.ConnectorAutoConnectMediumRisk != nil {
		f.ConnectorAutoConnectMediumRisk = *b.ConnectorAutoConnectMediumRisk
	}
	if b.ConnectorAutoConnectHighRisk != nil {
		f.ConnectorAutoConnectHighRisk = *b.ConnectorAutoConnectHighRisk
	}
	if b.ConnectorAutoConnectPrograms != nil {
		f.ConnectorAutoConnectPrograms = *b.ConnectorAutoConnectPrograms
	}
	if b.ConnectorApprovalRequiredPrograms != nil {
		f.ConnectorApprovalRequiredPrograms = *b.ConnectorApprovalRequiredPrograms
	}
	if b.ConnectorBlockedPrograms != nil {
		f.ConnectorBlockedPrograms = *b.ConnectorBlockedPrograms
	}
}
