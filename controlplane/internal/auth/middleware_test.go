package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestMiddlewareStaticTokenAuthentication(t *testing.T) {
	store := &fakeIdentityStore{}
	cfg := config.AuthConfig{
		RBAC: config.RBACConfig{DefaultRole: "viewer"},
		OIDC: config.OIDCConfig{
			StaticTokens: map[string]config.StaticPrincipalConfig{
				"local-dev-token": {
					Subject: "user-123",
					Email:   "dev@example.com",
					Name:    " Dev User ",
					Roles:   []string{" admin ", "ADMIN", ""},
					Groups:  []string{" team-a ", "team-a"},
				},
			},
		},
	}

	mw := NewMiddleware(zap.NewNop(), false, cfg, store)

	req := httptest.NewRequest("GET", "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer local-dev-token")

	principal, err := mw.authenticate(req)
	if err != nil {
		t.Fatalf("expected authentication to succeed: %v", err)
	}
	if principal == nil {
		t.Fatalf("expected principal, got nil")
	}

	if principal.Subject != "user-123" {
		t.Fatalf("expected subject user-123 got %s", principal.Subject)
	}
	if principal.Email != "dev@example.com" {
		t.Fatalf("expected email propagated, got %s", principal.Email)
	}
	if principal.Name != "Dev User" {
		t.Fatalf("expected trimmed name, got %q", principal.Name)
	}

	expectedRoles := []string{"admin"}
	if !equalSlices(principal.Roles, expectedRoles) {
		t.Fatalf("expected roles %v got %v", expectedRoles, principal.Roles)
	}
	expectedGroups := []string{"team-a"}
	if !equalSlices(principal.Groups, expectedGroups) {
		t.Fatalf("expected groups %v got %v", expectedGroups, principal.Groups)
	}

	if !store.ensureUserCalled {
		t.Fatalf("expected EnsureUser to be called")
	}
	if !equalSlices(store.assignedRoles, expectedRoles) {
		t.Fatalf("expected assigned roles %v got %v", expectedRoles, store.assignedRoles)
	}
}

func TestMiddlewareStaticTokenUsesStoredRoles(t *testing.T) {
	store := &fakeIdentityStore{
		rolesReturn: []string{"operator", "admin"},
	}
	cfg := config.AuthConfig{
		RBAC: config.RBACConfig{DefaultRole: "viewer"},
		OIDC: config.OIDCConfig{
			StaticTokens: map[string]config.StaticPrincipalConfig{
				"fallback-token": {
					Subject: "user-456",
				},
			},
		},
	}

	mw := NewMiddleware(zap.NewNop(), false, cfg, store)

	req := httptest.NewRequest("GET", "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer fallback-token")

	principal, err := mw.authenticate(req)
	if err != nil {
		t.Fatalf("expected authentication to succeed: %v", err)
	}

	if !equalSlices(store.assignedRoles, []string{"viewer"}) {
		t.Fatalf("expected default role assigned, got %v", store.assignedRoles)
	}

	expectedRoles := []string{"operator", "admin"}
	if !equalSlices(principal.Roles, expectedRoles) {
		t.Fatalf("expected roles to reflect stored assignments %v got %v", expectedRoles, principal.Roles)
	}
}

type fakeIdentityStore struct {
	userID           uuid.UUID
	ensureUserCalled bool
	assignedRoles    []string
	rolesReturn      []string
}

func (f *fakeIdentityStore) EnsureUser(_ context.Context, externalID, email, displayName string) (*storage.User, error) {
	f.ensureUserCalled = true
	if f.userID == uuid.Nil {
		f.userID = uuid.New()
	}
	return &storage.User{ID: f.userID, ExternalID: externalID}, nil
}

func (f *fakeIdentityStore) AssignRolesToUser(_ context.Context, userID uuid.UUID, roles []string) error {
	f.assignedRoles = append([]string{}, roles...)
	if f.userID == uuid.Nil {
		f.userID = userID
	}
	return nil
}

func (f *fakeIdentityStore) ListUserRoles(_ context.Context, userID uuid.UUID) ([]string, error) {
	if len(f.rolesReturn) == 0 {
		return nil, nil
	}
	return append([]string{}, f.rolesReturn...), nil
}

func (f *fakeIdentityStore) GetUserByExternalID(_ context.Context, externalID string) (*storage.User, error) {
	if f.userID == uuid.Nil {
		return nil, nil
	}
	return &storage.User{ID: f.userID, ExternalID: externalID}, nil
}

func (f *fakeIdentityStore) ValidateSessionToken(_ context.Context, token string) (*storage.Session, *storage.LocalUser, error) {
	return nil, nil, nil
}

func (f *fakeIdentityStore) ValidateNodeToken(_ context.Context, token string) (*storage.Node, error) {
	return nil, nil
}

func TestMiddlewareRejectsOpaqueBearerWhenOIDCDisabled(t *testing.T) {
	store := &fakeIdentityStore{}
	cfg := config.AuthConfig{
		OIDC: config.OIDCConfig{
			Enabled: false,
		},
		RBAC: config.RBACConfig{DefaultRole: "viewer"},
	}

	mw := NewMiddleware(zap.NewNop(), false, cfg, store)

	req := httptest.NewRequest("GET", "/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer some-opaque-token")

	principal, err := mw.authenticate(req)
	if err == nil {
		t.Fatalf("expected opaque token to be rejected when OIDC disabled, got principal %+v", principal)
	}
	if principal != nil {
		t.Fatalf("expected nil principal, got %+v", principal)
	}
}

func TestMiddlewareBypassesCollectorSelfServiceOnlyWithCollectorCredential(t *testing.T) {
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, &fakeIdentityStore{})
	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-1/heartbeat?tenant_id="+uuid.NewString(), nil)
	req.Header.Set("X-ControlOne-Collector-Token", storage.ContentPackEdgeCollectorTokenPrefix+"test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("collector credential bypass status=%d called=%v", rec.Code, called)
	}

	called = false
	req = httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-1/heartbeat?tenant_id="+uuid.NewString(), nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || called {
		t.Fatalf("missing collector credential status=%d called=%v, want 401 and not called", rec.Code, called)
	}
}

func TestMiddlewareAllowsPublicTrustCenterTenantLookup(t *testing.T) {
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, &fakeIdentityStore{})
	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trust/Tenant%20A", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || !called {
		t.Fatalf("public trust lookup status=%d called=%v, want 204 and called", rec.Code, called)
	}
}

func TestMiddlewareKeepsTrustCenterAdminCollectionsAuthenticated(t *testing.T) {
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, &fakeIdentityStore{})
	called := false
	handler := mw.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	adminPaths := []string{
		"/api/v1/trust/subprocessors",
		"/api/v1/trust/certifications",
		"/api/v1/trust/faq",
		"/api/v1/trust/incidents",
	}
	for _, path := range adminPaths {
		t.Run(path, func(t *testing.T) {
			called = false
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized || called {
				t.Fatalf("admin trust path status=%d called=%v, want 401 and not called", rec.Code, called)
			}
		})
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
