package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// handleTenantRemediationConfig serves
//
//	GET /api/v1/tenants/{id}/remediation-config
//	PUT /api/v1/tenants/{id}/remediation-config
func (s *Server) handleTenantRemediationConfig(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/tenants/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) < 1 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid tenant id", http.StatusBadRequest)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleAdmin) {
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, err := s.store.GetTenantRemediationConfig(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("get remediation config", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, cfg)

	case http.MethodPut:
		current, err := s.store.GetTenantRemediationConfig(r.Context(), tenantID)
		if err != nil {
			s.logger.Error("get remediation config before update", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		var body tenantRemediationConfigUpdate
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		applyTenantRemediationConfigUpdate(current, body)
		current.TenantID = tenantID

		updated, err := s.store.UpsertTenantRemediationConfig(r.Context(), *current)
		if err != nil {
			s.logger.Error("upsert remediation config", zap.Error(err))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.recordAudit(r.Context(), principal, tenantID, "tenant.remediation_config.updated", "tenant", tenantID.String(), map[string]any{
			"auto_block_enabled":                 updated.AutoBlockEnabled,
			"auto_block_min_confidence":          updated.AutoBlockMinConfidence,
			"default_ip_block_scope":             updated.DefaultIPBlockScope,
			"default_ip_block_ttl_seconds":       updated.DefaultIPBlockTTLSeconds,
			"require_corroborating_threat_intel": updated.RequireCorroboratingThreatIntel,
		})
		writeJSON(w, http.StatusOK, updated)

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

type tenantRemediationConfigUpdate struct {
	MinApprovalSeverity             *string
	ChangeWindows                   *[]storage.ChangeWindow
	CriticalOverride                *bool
	CircuitBreakerWindowMin         *int
	CircuitBreakerFailPct           *int
	CircuitBreakerMinSamples        *int
	AutoBlockEnabled                *bool
	AutoBlockMinConfidence          *int
	DefaultIPBlockScope             *string
	DefaultIPBlockTTLSeconds        *int
	RequireCorroboratingThreatIntel *bool
	PatchRequiresApproval           *bool
}

func applyTenantRemediationConfigUpdate(cfg *storage.TenantRemediationConfig, update tenantRemediationConfigUpdate) {
	if cfg == nil {
		return
	}
	if update.MinApprovalSeverity != nil {
		cfg.MinApprovalSeverity = *update.MinApprovalSeverity
	}
	if update.ChangeWindows != nil {
		cfg.ChangeWindows = *update.ChangeWindows
	}
	if update.CriticalOverride != nil {
		cfg.CriticalOverride = *update.CriticalOverride
	}
	if update.CircuitBreakerWindowMin != nil {
		cfg.CircuitBreakerWindowMin = *update.CircuitBreakerWindowMin
	}
	if update.CircuitBreakerFailPct != nil {
		cfg.CircuitBreakerFailPct = *update.CircuitBreakerFailPct
	}
	if update.CircuitBreakerMinSamples != nil {
		cfg.CircuitBreakerMinSamples = *update.CircuitBreakerMinSamples
	}
	if update.AutoBlockEnabled != nil {
		cfg.AutoBlockEnabled = *update.AutoBlockEnabled
	}
	if update.AutoBlockMinConfidence != nil {
		cfg.AutoBlockMinConfidence = *update.AutoBlockMinConfidence
	}
	if update.DefaultIPBlockScope != nil {
		cfg.DefaultIPBlockScope = *update.DefaultIPBlockScope
	}
	if update.DefaultIPBlockTTLSeconds != nil {
		cfg.DefaultIPBlockTTLSeconds = *update.DefaultIPBlockTTLSeconds
	}
	if update.RequireCorroboratingThreatIntel != nil {
		cfg.RequireCorroboratingThreatIntel = *update.RequireCorroboratingThreatIntel
	}
	if update.PatchRequiresApproval != nil {
		cfg.PatchRequiresApproval = *update.PatchRequiresApproval
	}
}
