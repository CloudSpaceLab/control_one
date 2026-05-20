package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type hypervisorTenantStore struct {
	fakeStore
	allowedTenants map[uuid.UUID]bool
	hosts          map[uuid.UUID]*storage.HypervisorHost
	credentials    map[uuid.UUID]*storage.ProviderCredential
}

func (s *hypervisorTenantStore) UserHasTenantRole(_ context.Context, _ uuid.UUID, tenantID uuid.UUID, _ []string) (bool, error) {
	return s.allowedTenants[tenantID], nil
}

func (s *hypervisorTenantStore) GetProviderCredential(_ context.Context, id uuid.UUID) (*storage.ProviderCredential, error) {
	if cred, ok := s.credentials[id]; ok {
		copy := *cred
		return &copy, nil
	}
	return nil, nil
}

func (s *hypervisorTenantStore) CreateHypervisorHost(_ context.Context, params storage.CreateHypervisorHostParams) (*storage.HypervisorHost, error) {
	if s.hosts == nil {
		s.hosts = map[uuid.UUID]*storage.HypervisorHost{}
	}
	host := &storage.HypervisorHost{
		ID:           uuid.New(),
		TenantID:     params.TenantID,
		Provider:     params.Provider,
		Name:         params.Name,
		EndpointURL:  params.EndpointURL,
		Labels:       params.Labels,
		HealthStatus: "unknown",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if params.CredentialID != nil {
		host.CredentialID = uuid.NullUUID{UUID: *params.CredentialID, Valid: true}
	}
	if params.Datacenter != "" {
		host.Datacenter = sql.NullString{String: params.Datacenter, Valid: true}
	}
	s.hosts[host.ID] = host
	copy := *host
	return &copy, nil
}

func (s *hypervisorTenantStore) GetHypervisorHost(_ context.Context, id uuid.UUID) (*storage.HypervisorHost, error) {
	if host, ok := s.hosts[id]; ok {
		copy := *host
		return &copy, nil
	}
	return nil, nil
}

func (s *hypervisorTenantStore) ListHypervisorHosts(_ context.Context, tenantID uuid.UUID, provider string, limit, offset int) ([]storage.HypervisorHost, int, error) {
	var filtered []storage.HypervisorHost
	for _, host := range s.hosts {
		if host.TenantID != tenantID {
			continue
		}
		if provider != "" && host.Provider != provider {
			continue
		}
		filtered = append(filtered, *host)
	}
	total := len(filtered)
	if offset > total {
		return []storage.HypervisorHost{}, total, nil
	}
	if limit > 0 && offset+limit < total {
		return filtered[offset : offset+limit], total, nil
	}
	return filtered[offset:], total, nil
}

func (s *hypervisorTenantStore) UpdateHypervisorHost(_ context.Context, id uuid.UUID, params storage.UpdateHypervisorHostParams) (*storage.HypervisorHost, error) {
	host, ok := s.hosts[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	if params.Name != nil {
		host.Name = *params.Name
	}
	if params.EndpointURL != nil {
		host.EndpointURL = *params.EndpointURL
	}
	if params.ClearCredentialID {
		host.CredentialID = uuid.NullUUID{}
	} else if params.CredentialID != nil {
		host.CredentialID = uuid.NullUUID{UUID: *params.CredentialID, Valid: true}
	}
	if params.Datacenter != nil {
		host.Datacenter = sql.NullString{String: *params.Datacenter, Valid: *params.Datacenter != ""}
	}
	if params.Labels != nil {
		host.Labels = *params.Labels
	}
	host.UpdatedAt = time.Now()
	copy := *host
	return &copy, nil
}

func (s *hypervisorTenantStore) RecordHypervisorHostHealth(_ context.Context, id uuid.UUID, status, message string) (*storage.HypervisorHost, error) {
	host, ok := s.hosts[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	host.HealthStatus = status
	host.HealthMessage = sql.NullString{String: message, Valid: message != ""}
	host.LastVerifiedAt = sql.NullTime{Time: time.Now(), Valid: true}
	host.UpdatedAt = time.Now()
	copy := *host
	return &copy, nil
}

func (s *hypervisorTenantStore) DeleteHypervisorHost(_ context.Context, id uuid.UUID) error {
	if _, ok := s.hosts[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.hosts, id)
	return nil
}

func newHypervisorTenantServer(store *hypervisorTenantStore, role, token string) *Server {
	return New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens(role, token),
	}, store, &stubQueue{})
}

func seededHypervisorHost(id, tenantID uuid.UUID, provider string) *storage.HypervisorHost {
	now := time.Now()
	return &storage.HypervisorHost{
		ID:           id,
		TenantID:     tenantID,
		Provider:     provider,
		Name:         "hv-" + id.String(),
		EndpointURL:  "https://hypervisor.example",
		Labels:       map[string]any{},
		HealthStatus: "unknown",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestHypervisorHostsListRequiresTenantAccess(t *testing.T) {
	allowedTenant := uuid.New()
	blockedTenant := uuid.New()
	allowedHost := uuid.New()
	blockedHost := uuid.New()
	store := &hypervisorTenantStore{
		fakeStore: fakeStore{userRoles: map[uuid.UUID][]string{}},
		allowedTenants: map[uuid.UUID]bool{
			allowedTenant: true,
		},
		hosts: map[uuid.UUID]*storage.HypervisorHost{
			allowedHost: seededHypervisorHost(allowedHost, allowedTenant, ProviderAWS),
			blockedHost: seededHypervisorHost(blockedHost, blockedTenant, ProviderAWS),
		},
	}
	srv := newHypervisorTenantServer(store, "viewer", "hypervisor-viewer")

	rec := callTenantGatedServer(t, srv, http.MethodGet, "/api/v1/hypervisor-hosts?tenant_id="+blockedTenant.String(), "hypervisor-viewer", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected blocked tenant list to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callTenantGatedServer(t, srv, http.MethodGet, "/api/v1/hypervisor-hosts?tenant_id="+allowedTenant.String(), "hypervisor-viewer", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected allowed tenant list to return 200, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHypervisorHostResourceRoutesRequireResourceTenantAccess(t *testing.T) {
	allowedTenant := uuid.New()
	blockedTenant := uuid.New()
	hostID := uuid.New()
	store := &hypervisorTenantStore{
		fakeStore:      fakeStore{userRoles: map[uuid.UUID][]string{}},
		allowedTenants: map[uuid.UUID]bool{allowedTenant: true},
		hosts: map[uuid.UUID]*storage.HypervisorHost{
			hostID: seededHypervisorHost(hostID, blockedTenant, ProviderAWS),
		},
	}
	srv := newHypervisorTenantServer(store, "admin", "hypervisor-admin")

	rec := callTenantGatedServer(t, srv, http.MethodGet, "/api/v1/hypervisor-hosts/"+hostID.String()+"?tenant_id="+blockedTenant.String(), "hypervisor-admin", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant get to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callTenantGatedServer(t, srv, http.MethodPatch, "/api/v1/hypervisor-hosts/"+hostID.String(), "hypervisor-admin", map[string]any{"name": "blocked"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant update to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callTenantGatedServer(t, srv, http.MethodPost, "/api/v1/hypervisor-hosts/"+hostID.String()+"/verify", "hypervisor-admin", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected cross-tenant verify to return 403, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHypervisorHostCredentialReferencesMustStayInTenant(t *testing.T) {
	tenantID := uuid.New()
	otherTenantID := uuid.New()
	hostID := uuid.New()
	credentialID := uuid.New()
	store := &hypervisorTenantStore{
		fakeStore:      fakeStore{userRoles: map[uuid.UUID][]string{}},
		allowedTenants: map[uuid.UUID]bool{tenantID: true},
		hosts: map[uuid.UUID]*storage.HypervisorHost{
			hostID: seededHypervisorHost(hostID, tenantID, ProviderAWS),
		},
		credentials: map[uuid.UUID]*storage.ProviderCredential{
			credentialID: {
				ID:        credentialID,
				TenantID:  otherTenantID,
				Provider:  ProviderAWS,
				Name:      "other-tenant-aws",
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		},
	}
	srv := newHypervisorTenantServer(store, "admin", "hypervisor-admin")

	body := map[string]any{
		"tenant_id":     tenantID.String(),
		"provider":      ProviderAWS,
		"name":          "bad-credential",
		"endpoint_url":  "https://hypervisor.example",
		"credential_id": credentialID.String(),
	}
	rec := callTenantGatedServer(t, srv, http.MethodPost, "/api/v1/hypervisor-hosts", "hypervisor-admin", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-tenant credential create to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}

	rec = callTenantGatedServer(t, srv, http.MethodPatch, "/api/v1/hypervisor-hosts/"+hostID.String(), "hypervisor-admin", map[string]any{
		"credential_id": credentialID.String(),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected cross-tenant credential update to return 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !errors.Is(store.DeleteHypervisorHost(context.Background(), uuid.New()), sql.ErrNoRows) {
		t.Fatalf("hypervisor store delete should preserve not-found semantics")
	}
}
