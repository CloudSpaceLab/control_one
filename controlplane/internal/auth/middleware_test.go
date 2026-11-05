package auth

import (
	"context"
	"errors"
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

func TestMiddlewareBearerAuthenticationTakesPrecedenceOverForwardedClientCertificate(t *testing.T) {
	store := &fakeIdentityStore{}
	cfg := config.AuthConfig{
		OIDC: config.OIDCConfig{
			StaticTokens: map[string]config.StaticPrincipalConfig{
				"browser-token": {
					Subject: "browser-user",
					Roles:   []string{"admin"},
				},
			},
		},
	}
	mw := NewMiddleware(zap.NewNop(), false, cfg, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer browser-token")
	req.Header.Set("X-SSL-Client-S-DN", "CN=installed-browser-certificate")

	principal, err := mw.authenticate(req)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if principal.Type != "user" || principal.Subject != "browser-user" {
		t.Fatalf("principal=%+v, want bearer user", principal)
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

func TestMiddlewarePrefersBearerSessionOverForwardedClientCertificate(t *testing.T) {
	userID := uuid.New()
	store := &fakeIdentityStore{
		rolesReturn:  []string{"admin"},
		sessionToken: "browser-session-token",
		session:      &storage.Session{},
		sessionUser: &storage.LocalUser{
			ID:          userID,
			Email:       "admin@local",
			DisplayName: "Default Admin",
		},
	}
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer browser-session-token")
	req.Header.Set("X-SSL-Client-S-DN", "CN=browser-certificate,O=Example Corp")

	principal, err := mw.authenticate(req)
	if err != nil {
		t.Fatalf("expected authenticated browser session: %v", err)
	}
	if principal.Type != "user" {
		t.Fatalf("principal type = %q, want user", principal.Type)
	}
	if principal.Subject != userID.String() {
		t.Fatalf("principal subject = %q, want %q", principal.Subject, userID)
	}
	if !equalSlices(principal.Roles, []string{"admin"}) {
		t.Fatalf("principal roles = %v, want [admin]", principal.Roles)
	}
}

type fakeIdentityStore struct {
	ensureErr, assignErr, listErr error
	emptyRoles                    bool
	userID                        uuid.UUID
	ensureUserCalled              bool
	assignedRoles                 []string
	rolesReturn                   []string
	sessionToken                  string
	session                       *storage.Session
	sessionUser                   *storage.LocalUser
}

func (f *fakeIdentityStore) EnsureUser(_ context.Context, externalID, email, displayName string) (*storage.User, error) {
	f.ensureUserCalled = true
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	if f.userID == uuid.Nil {
		f.userID = uuid.New()
	}
	return &storage.User{ID: f.userID, ExternalID: externalID}, nil
}

func (f *fakeIdentityStore) AssignRolesToUser(_ context.Context, userID uuid.UUID, roles []string) error {
	if f.assignErr != nil {
		return f.assignErr
	}
	f.assignedRoles = append([]string{}, roles...)
	if f.userID == uuid.Nil {
		f.userID = userID
	}
	return nil
}

func (f *fakeIdentityStore) ListUserRoles(_ context.Context, userID uuid.UUID) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	if f.emptyRoles {
		return nil, nil
	}
	if len(f.rolesReturn) == 0 {
		return append([]string{}, f.assignedRoles...), nil
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
	if token == f.sessionToken {
		return f.session, f.sessionUser, nil
	}
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

func TestLDAPSessionVerifierRejectsMissingOrFailedDirectoryEvidence(t *testing.T) {
	store := &fakeIdentityStore{sessionToken: "fixture-session", session: &storage.Session{}, sessionUser: &storage.LocalUser{ID: uuid.New(), Email: "reviewer@phase4.invalid", AuthProvider: storage.AuthProviderLDAP}, rolesReturn: []string{"ciso"}}
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)
	req.Header.Set("Authorization", "Bearer fixture-session")
	_, err := mw.authenticate(req)
	if err == nil {
		t.Fatal("missing LDAP verifier must deny")
	}
	mw.SetLDAPSessionVerifier(func(context.Context, *storage.LocalUser) ([]string, error) {
		return nil, errors.New("directory unavailable")
	})
	_, err = mw.authenticate(req)
	if err == nil {
		t.Fatal("failed directory verification must deny")
	}
	recorder := httptest.NewRecorder()
	mw.Wrap(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("directory failure reached protected handler") })).ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized || recorder.Header().Get("X-ControlOne-Auth-Reason") != "directory_verification_required" {
		t.Fatalf("unexpected response: %d %q", recorder.Code, recorder.Header().Get("X-ControlOne-Auth-Reason"))
	}

	mw.SetLDAPSessionVerifier(func(context.Context, *storage.LocalUser) ([]string, error) { return []string{"viewer"}, nil })
	p, err := mw.authenticate(req)
	if err != nil {
		t.Fatal(err)
	}
	if !equalSlices(p.Roles, []string{"viewer"}) {
		t.Fatalf("must use verified roles, got %v", p.Roles)
	}
}

func TestProvisioningPersistenceFailsClosed(t *testing.T) {
	for _, stage := range []string{"identity", "assignment", "readback", "empty"} {
		t.Run(stage, func(t *testing.T) {
			store := &fakeIdentityStore{}
			fault := errors.New("isolated persistence failure")
			switch stage {
			case "identity":
				store.ensureErr = fault
			case "assignment":
				store.assignErr = fault
			case "readback":
				store.listErr = fault
			case "empty":
				store.emptyRoles = true
			}
			mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, store)
			p, err := mw.persistPrincipal(context.Background(), &Principal{Subject: "fixture", Roles: []string{"admin"}})
			if stage == "empty" {
				if err != nil || len(p.Roles) != 0 {
					t.Fatalf("empty effective grants retained authority: %v %v", p, err)
				}
				return
			}
			if !errors.Is(err, fault) || p != nil {
				t.Fatalf("persistence failure must deny principal: %v %v", p, err)
			}
		})
	}
}

func (f *fakeIdentityStore) PersistAuthenticationRoles(ctx context.Context, authority, subject, email, name string, roles []string) (*storage.User, []string, error) {
	u, err := f.EnsureUser(ctx, subject, email, name)
	if err != nil {
		return nil, nil, err
	}
	if err = f.AssignRolesToUser(ctx, u.ID, roles); err != nil {
		return nil, nil, err
	}
	effective, err := f.ListUserRoles(ctx, u.ID)
	return u, effective, err
}

func TestStaticTokenIdentityDoesNotExposeCredential(t *testing.T) {
	const secret = "isolated-token-not-for-production"
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{OIDC: config.OIDCConfig{StaticTokens: map[string]config.StaticPrincipalConfig{secret: {Roles: []string{"viewer"}}}}}, &fakeIdentityStore{})
	p, ok := mw.staticPrincipal(secret)
	if !ok || p.Subject == secret || p.Name == secret {
		t.Fatal("token leaked into principal identity")
	}
}

func TestProvisioningRequiresPersistenceAndSubject(t *testing.T) {
	mw := NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, nil)
	if p, err := mw.persistPrincipal(context.Background(), &Principal{Subject: "fixture", Roles: []string{"admin"}}); err == nil || p != nil {
		t.Fatal("missing persistence accepted claimed authority")
	}
	mw = NewMiddleware(zap.NewNop(), false, config.AuthConfig{}, &fakeIdentityStore{})
	if p, err := mw.persistPrincipal(context.Background(), &Principal{Roles: []string{"admin"}}); err == nil || p != nil {
		t.Fatal("empty subject accepted claimed authority")
	}
}
