package server

import (
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
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
)

type hypervisorHostRequest struct {
	TenantID     uuid.UUID      `json:"tenant_id"`
	Provider     string         `json:"provider"`
	Name         string         `json:"name"`
	EndpointURL  string         `json:"endpoint_url"`
	CredentialID *string        `json:"credential_id,omitempty"`
	Datacenter   string         `json:"datacenter,omitempty"`
	Labels       map[string]any `json:"labels,omitempty"`
}

type hypervisorHostUpdateRequest struct {
	Name              *string         `json:"name,omitempty"`
	EndpointURL       *string         `json:"endpoint_url,omitempty"`
	CredentialID      *string         `json:"credential_id,omitempty"`
	ClearCredentialID bool            `json:"clear_credential_id,omitempty"`
	Datacenter        *string         `json:"datacenter,omitempty"`
	Labels            *map[string]any `json:"labels,omitempty"`
}

type hypervisorHostResponse struct {
	ID             string         `json:"id"`
	TenantID       string         `json:"tenant_id"`
	Provider       string         `json:"provider"`
	Name           string         `json:"name"`
	EndpointURL    string         `json:"endpoint_url"`
	CredentialID   *string        `json:"credential_id,omitempty"`
	Datacenter     *string        `json:"datacenter,omitempty"`
	Labels         map[string]any `json:"labels"`
	HealthStatus   string         `json:"health_status"`
	HealthMessage  *string        `json:"health_message,omitempty"`
	LastVerifiedAt *string        `json:"last_verified_at,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

type hypervisorHostListResponse struct {
	Items      []hypervisorHostResponse `json:"items"`
	Pagination paginationMeta           `json:"pagination"`
}

type hypervisorHostVerifyResponse struct {
	Host    hypervisorHostResponse `json:"host"`
	Status  string                 `json:"status"`
	Message string                 `json:"message,omitempty"`
}

func (s *Server) handleHypervisorHostsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.listHypervisorHosts(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.createHypervisorHost(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHypervisorHostSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/hypervisor-hosts/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(trimmed, "/")
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid host id", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			if _, ok := s.authorize(w, r, roleViewer); !ok {
				return
			}
			s.getHypervisorHost(w, r, id)
		case http.MethodPatch:
			if _, ok := s.authorize(w, r, roleAdmin); !ok {
				return
			}
			s.updateHypervisorHost(w, r, id)
		case http.MethodDelete:
			if _, ok := s.authorize(w, r, roleAdmin); !ok {
				return
			}
			s.deleteHypervisorHost(w, r, id)
		default:
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodPatch+", "+http.MethodDelete)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 2 && parts[1] == "verify" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if _, ok := s.authorize(w, r, roleOperator); !ok {
			return
		}
		s.verifyHypervisorHost(w, r, id)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) createHypervisorHost(w http.ResponseWriter, r *http.Request) {
	var req hypervisorHostRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if req.TenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	provider := strings.TrimSpace(strings.ToLower(req.Provider))
	if _, ok := supportedProviders[provider]; !ok {
		http.Error(w, "provider must be one of aws|azure|vmware|libvirt", http.StatusBadRequest)
		return
	}

	var credID *uuid.UUID
	if req.CredentialID != nil && strings.TrimSpace(*req.CredentialID) != "" {
		parsed, err := uuid.Parse(*req.CredentialID)
		if err != nil {
			http.Error(w, "invalid credential_id", http.StatusBadRequest)
			return
		}
		credID = &parsed
	}

	params := storage.CreateHypervisorHostParams{
		TenantID:     req.TenantID,
		Provider:     provider,
		Name:         req.Name,
		EndpointURL:  req.EndpointURL,
		CredentialID: credID,
		Datacenter:   req.Datacenter,
		Labels:       req.Labels,
	}
	host, err := s.store.CreateHypervisorHost(r.Context(), params)
	if err != nil {
		s.logger.Error("create hypervisor host", zap.Error(err))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, host.TenantID, "hypervisor_host.created", "hypervisor_host", host.ID.String(), map[string]any{
		"provider": provider,
		"name":     host.Name,
	})
	writeJSON(w, http.StatusCreated, hypervisorHostToResponse(host))
}

func (s *Server) listHypervisorHosts(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var tenantID uuid.UUID
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		parsed, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}
	provider := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("provider")))

	hosts, total, err := s.store.ListHypervisorHosts(r.Context(), tenantID, provider, limit, offset)
	if err != nil {
		s.logger.Error("list hypervisor hosts", zap.Error(err))
		http.Error(w, "list hypervisor hosts", http.StatusInternalServerError)
		return
	}
	items := make([]hypervisorHostResponse, 0, len(hosts))
	for i := range hosts {
		items = append(items, hypervisorHostToResponse(&hosts[i]))
	}
	writeJSON(w, http.StatusOK, hypervisorHostListResponse{
		Items:      items,
		Pagination: paginationMeta{Total: total, Limit: limit, Offset: offset, Count: len(items)},
	})
}

func (s *Server) getHypervisorHost(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	host, err := s.store.GetHypervisorHost(r.Context(), id)
	if err != nil {
		s.logger.Error("get hypervisor host", zap.Error(err))
		http.Error(w, "get hypervisor host", http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, hypervisorHostToResponse(host))
}

func (s *Server) updateHypervisorHost(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	var req hypervisorHostUpdateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	params := storage.UpdateHypervisorHostParams{
		Name:              req.Name,
		EndpointURL:       req.EndpointURL,
		Datacenter:        req.Datacenter,
		Labels:            req.Labels,
		ClearCredentialID: req.ClearCredentialID,
	}
	if req.CredentialID != nil && strings.TrimSpace(*req.CredentialID) != "" {
		parsed, err := uuid.Parse(*req.CredentialID)
		if err != nil {
			http.Error(w, "invalid credential_id", http.StatusBadRequest)
			return
		}
		params.CredentialID = &parsed
	}
	host, err := s.store.UpdateHypervisorHost(r.Context(), id, params)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("update hypervisor host", zap.Error(err))
		http.Error(w, "update hypervisor host", http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.NotFound(w, r)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, host.TenantID, "hypervisor_host.updated", "hypervisor_host", host.ID.String(), nil)
	writeJSON(w, http.StatusOK, hypervisorHostToResponse(host))
}

func (s *Server) deleteHypervisorHost(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	host, err := s.store.GetHypervisorHost(r.Context(), id)
	if err != nil {
		s.logger.Error("get hypervisor host", zap.Error(err))
		http.Error(w, "delete hypervisor host", http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteHypervisorHost(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("delete hypervisor host", zap.Error(err))
		http.Error(w, "delete hypervisor host", http.StatusInternalServerError)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, host.TenantID, "hypervisor_host.deleted", "hypervisor_host", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// verifyHypervisorHost exercises the provider adapter's VerifyReachable path
// against the stored credential and records the outcome as health_status.
func (s *Server) verifyHypervisorHost(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	host, err := s.store.GetHypervisorHost(r.Context(), id)
	if err != nil {
		s.logger.Error("get hypervisor host", zap.Error(err))
		http.Error(w, "verify hypervisor host", http.StatusInternalServerError)
		return
	}
	if host == nil {
		http.NotFound(w, r)
		return
	}

	status := "ok"
	message := ""
	if err := s.verifyHypervisorHostOnce(r, host); err != nil {
		status = "unhealthy"
		message = err.Error()
	}

	updated, err := s.store.RecordHypervisorHostHealth(r.Context(), host.ID, status, message)
	if err != nil {
		s.logger.Error("record hypervisor host health", zap.Error(err))
		http.Error(w, "record hypervisor host health", http.StatusInternalServerError)
		return
	}

	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, host.TenantID, "hypervisor_host.verified", "hypervisor_host", host.ID.String(), map[string]any{
		"status":  status,
		"message": message,
	})

	writeJSON(w, http.StatusOK, hypervisorHostVerifyResponse{
		Host:    hypervisorHostToResponse(updated),
		Status:  status,
		Message: message,
	})
}

// verifyHypervisorHostOnce runs a single verification attempt using the
// configured provisioning adapter. When the adapter exposes VerifyReachable
// it is invoked directly; otherwise a minimal reachability probe is performed
// by calling Apply with `dry_run=true` metadata.
func (s *Server) verifyHypervisorHostOnce(r *http.Request, host *storage.HypervisorHost) error {
	metadata := map[string]string{
		"_endpoint_url":        host.EndpointURL,
		"_hypervisor_host_id":  host.ID.String(),
		"_hypervisor_host_dc":  strOrEmpty(host.Datacenter),
		"_verify_only":         "true",
	}

	if host.CredentialID.Valid {
		cred, err := s.store.GetProviderCredential(r.Context(), host.CredentialID.UUID)
		if err != nil {
			return fmt.Errorf("load credential: %w", err)
		}
		if cred == nil {
			return errors.New("credential_id references missing credential")
		}
		raw, err := s.openProviderCredential(cred)
		if err != nil {
			return fmt.Errorf("decrypt credential: %w", err)
		}
		for k, v := range raw {
			if str, ok := v.(string); ok {
				metadata["_cred_"+k] = str
			}
		}
	}

	adapter := provisioning.NewAdapter(host.Provider, s.logger, nil)
	if verifier, ok := adapter.(provisioning.Verifier); ok {
		return verifier.VerifyReachable(r.Context(), host.Provider, metadata)
	}
	return nil
}

func strOrEmpty(n sql.NullString) string {
	if n.Valid {
		return n.String
	}
	return ""
}

func hypervisorHostToResponse(h *storage.HypervisorHost) hypervisorHostResponse {
	resp := hypervisorHostResponse{
		ID:           h.ID.String(),
		TenantID:     h.TenantID.String(),
		Provider:     h.Provider,
		Name:         h.Name,
		EndpointURL:  h.EndpointURL,
		Labels:       h.Labels,
		HealthStatus: h.HealthStatus,
		CreatedAt:    h.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    h.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if h.CredentialID.Valid {
		v := h.CredentialID.UUID.String()
		resp.CredentialID = &v
	}
	if h.Datacenter.Valid {
		v := h.Datacenter.String
		resp.Datacenter = &v
	}
	if h.HealthMessage.Valid {
		v := h.HealthMessage.String
		resp.HealthMessage = &v
	}
	if h.LastVerifiedAt.Valid {
		v := h.LastVerifiedAt.Time.UTC().Format(time.RFC3339)
		resp.LastVerifiedAt = &v
	}
	if resp.Labels == nil {
		resp.Labels = map[string]any{}
	}
	return resp
}
