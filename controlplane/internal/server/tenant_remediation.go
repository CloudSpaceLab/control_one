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
		var body storage.TenantRemediationConfig
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		body.TenantID = tenantID

		updated, err := s.store.UpsertTenantRemediationConfig(r.Context(), body)
		if err != nil {
			s.logger.Error("upsert remediation config", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, tenantID, "tenant.remediation_config.updated", "tenant", tenantID.String(), nil)
		writeJSON(w, http.StatusOK, updated)

	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}
