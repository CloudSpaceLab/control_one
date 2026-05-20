package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type tenantAccessFakeStore struct {
	fakeStore
	allowed    bool
	seenUser   uuid.UUID
	seenTenant uuid.UUID
	seenRoles  []string
}

func (f *tenantAccessFakeStore) UserHasTenantRole(_ context.Context, userID, tenantID uuid.UUID, roles []string) (bool, error) {
	f.seenUser = userID
	f.seenTenant = tenantID
	f.seenRoles = append([]string(nil), roles...)
	return f.allowed, nil
}

func TestRequireTenantAccessUsesPersistedTenantRoles(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tenantID := uuid.New()
	store := &tenantAccessFakeStore{
		allowed: true,
		fakeStore: fakeStore{users: map[string]*storage.User{
			"oidc-subject": {ID: userID, ExternalID: "oidc-subject"},
		}},
	}
	srv := &Server{store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/query?tenant_id="+tenantID.String(), nil)
	rec := httptest.NewRecorder()
	principal := &auth.Principal{Type: "user", Subject: "oidc-subject", Roles: []string{roleViewer}}

	if !srv.requireTenantAccess(rec, req, principal, tenantID, roleViewer, roleAdmin) {
		t.Fatalf("expected tenant access to pass: %s", rec.Body.String())
	}
	if store.seenUser != userID || store.seenTenant != tenantID {
		t.Fatalf("tenant gate checked %s/%s, want %s/%s", store.seenUser, store.seenTenant, userID, tenantID)
	}
	if len(store.seenRoles) != 2 || store.seenRoles[0] != roleViewer || store.seenRoles[1] != roleAdmin {
		t.Fatalf("unexpected checked roles: %v", store.seenRoles)
	}
}

func TestRequireTenantAccessRejectsUnassignedTenant(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tenantID := uuid.New()
	store := &tenantAccessFakeStore{
		allowed: false,
		fakeStore: fakeStore{users: map[string]*storage.User{
			"oidc-subject": {ID: userID, ExternalID: "oidc-subject"},
		}},
	}
	srv := &Server{store: store}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/query?tenant_id="+tenantID.String(), nil)
	rec := httptest.NewRecorder()
	principal := &auth.Principal{Type: "user", Subject: "oidc-subject", Roles: []string{roleViewer}}

	if srv.requireTenantAccess(rec, req, principal, tenantID, roleViewer) {
		t.Fatal("expected tenant access to fail")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
}
