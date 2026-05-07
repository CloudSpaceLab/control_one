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
)

// Supported provider identifiers for credentials + hypervisor hosts.
const (
	ProviderAWS     = "aws"
	ProviderAzure   = "azure"
	ProviderVMware  = "vmware"
	ProviderLibvirt = "libvirt"
)

var supportedProviders = map[string]struct{}{
	ProviderAWS:     {},
	ProviderAzure:   {},
	ProviderVMware:  {},
	ProviderLibvirt: {},
}

type providerCredentialRequest struct {
	TenantID uuid.UUID      `json:"tenant_id"`
	Provider string         `json:"provider"`
	Name     string         `json:"name"`
	Config   map[string]any `json:"config"`
}

type providerCredentialResponse struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Provider  string  `json:"provider"`
	Name      string  `json:"name"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
	RotatedAt *string `json:"rotated_at,omitempty"`
}

type providerCredentialRotateRequest struct {
	Config map[string]any `json:"config"`
}

type providerCredentialListResponse struct {
	Items      []providerCredentialResponse `json:"items"`
	Pagination paginationMeta               `json:"pagination"`
}

func (s *Server) handleProviderCredentialsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.listProviderCredentials(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.createProviderCredential(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleProviderCredentialSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/provider-credentials/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	parts := strings.Split(trimmed, "/")
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid credential id", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			if _, ok := s.authorize(w, r, roleAdmin); !ok {
				return
			}
			s.getProviderCredential(w, r, id)
		case http.MethodDelete:
			if _, ok := s.authorize(w, r, roleAdmin); !ok {
				return
			}
			s.deleteProviderCredential(w, r, id)
		default:
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodDelete)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}

	if len(parts) == 2 && parts[1] == "rotate" {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.rotateProviderCredential(w, r, id)
		return
	}

	http.NotFound(w, r)
}

func (s *Server) createProviderCredential(w http.ResponseWriter, r *http.Request) {
	if s.sealer == nil {
		http.Error(w, "secrets encryption is not configured", http.StatusServiceUnavailable)
		return
	}

	var req providerCredentialRequest
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
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if len(req.Config) == 0 {
		http.Error(w, "config must not be empty", http.StatusBadRequest)
		return
	}

	plaintext, err := json.Marshal(req.Config)
	if err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusBadRequest)
		return
	}

	ciphertext, nonce, err := s.sealer.Seal(plaintext)
	if err != nil {
		s.logger.Error("seal provider credential", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	cred, err := s.store.CreateProviderCredential(r.Context(), storage.CreateProviderCredentialParams{
		TenantID:        req.TenantID,
		Provider:        provider,
		Name:            name,
		ConfigEncrypted: ciphertext,
		Nonce:           nonce,
	})
	if err != nil {
		s.logger.Error("store provider credential", zap.Error(err))
		http.Error(w, "create provider credential", http.StatusInternalServerError)
		return
	}

	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, cred.TenantID, "provider_credential.created", "provider_credential", cred.ID.String(), map[string]any{
		"provider": provider,
		"name":     name,
	})

	writeJSON(w, http.StatusCreated, providerCredentialToResponse(cred))
}

func (s *Server) rotateProviderCredential(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if s.sealer == nil {
		http.Error(w, "secrets encryption is not configured", http.StatusServiceUnavailable)
		return
	}
	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	existing, err := s.store.GetProviderCredential(r.Context(), id)
	if err != nil {
		s.logger.Error("get provider credential", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if existing == nil || existing.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	var req providerCredentialRotateRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Config) == 0 {
		http.Error(w, "config must not be empty", http.StatusBadRequest)
		return
	}
	plaintext, err := json.Marshal(req.Config)
	if err != nil {
		http.Error(w, "encode config: "+err.Error(), http.StatusBadRequest)
		return
	}
	ciphertext, nonce, err := s.sealer.Seal(plaintext)
	if err != nil {
		s.logger.Error("seal provider credential rotate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	cred, err := s.store.UpdateProviderCredential(r.Context(), id, storage.UpdateProviderCredentialParams{
		ConfigEncrypted: ciphertext,
		Nonce:           nonce,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("rotate provider credential", zap.Error(err))
		http.Error(w, "rotate provider credential", http.StatusInternalServerError)
		return
	}
	if cred == nil {
		http.NotFound(w, r)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, cred.TenantID, "provider_credential.rotated", "provider_credential", cred.ID.String(), nil)
	writeJSON(w, http.StatusOK, providerCredentialToResponse(cred))
}

func (s *Server) listProviderCredentials(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	provider := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("provider")))

	creds, total, err := s.store.ListProviderCredentials(r.Context(), tenantID, provider, limit, offset)
	if err != nil {
		s.logger.Error("list provider credentials", zap.Error(err))
		http.Error(w, "list provider credentials", http.StatusInternalServerError)
		return
	}
	items := make([]providerCredentialResponse, 0, len(creds))
	for i := range creds {
		items = append(items, providerCredentialToResponse(&creds[i]))
	}
	writeJSON(w, http.StatusOK, providerCredentialListResponse{
		Items:      items,
		Pagination: paginationMeta{Total: total, Limit: limit, Offset: offset},
	})
}

func (s *Server) getProviderCredential(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	cred, err := s.store.GetProviderCredential(r.Context(), id)
	if err != nil {
		s.logger.Error("get provider credential", zap.Error(err))
		http.Error(w, "get provider credential", http.StatusInternalServerError)
		return
	}
	if cred == nil || cred.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, providerCredentialToResponse(cred))
}

func (s *Server) deleteProviderCredential(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	cred, err := s.store.GetProviderCredential(r.Context(), id)
	if err != nil {
		s.logger.Error("get provider credential", zap.Error(err))
		http.Error(w, "delete provider credential", http.StatusInternalServerError)
		return
	}
	if cred == nil || cred.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteProviderCredential(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("delete provider credential", zap.Error(err))
		http.Error(w, "delete provider credential", http.StatusInternalServerError)
		return
	}
	principal, _ := auth.PrincipalFromContext(r.Context())
	s.recordAudit(r.Context(), principal, cred.TenantID, "provider_credential.deleted", "provider_credential", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// openProviderCredential returns the decrypted config blob for consumption by
// provisioning adapters. Never expose this over HTTP.
func (s *Server) openProviderCredential(cred *storage.ProviderCredential) (map[string]any, error) {
	if s.sealer == nil {
		return nil, errors.New("secrets encryption not configured")
	}
	if cred == nil || len(cred.ConfigEncrypted) == 0 {
		return nil, errors.New("credential has no sealed config")
	}
	plaintext, err := s.sealer.Open(cred.ConfigEncrypted, cred.Nonce)
	if err != nil {
		return nil, fmt.Errorf("open provider credential: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(plaintext, &out); err != nil {
		return nil, fmt.Errorf("decode provider credential: %w", err)
	}
	return out, nil
}

func providerCredentialToResponse(c *storage.ProviderCredential) providerCredentialResponse {
	resp := providerCredentialResponse{
		ID:        c.ID.String(),
		TenantID:  c.TenantID.String(),
		Provider:  c.Provider,
		Name:      c.Name,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.RotatedAt.Valid {
		ts := c.RotatedAt.Time.UTC().Format(time.RFC3339)
		resp.RotatedAt = &ts
	}
	return resp
}
