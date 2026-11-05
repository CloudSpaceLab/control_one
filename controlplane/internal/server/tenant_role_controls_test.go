package server

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type tenantRoleTestStore struct {
	*governanceTestStore
	global     bool
	changes    int
	readActor  uuid.UUID
	readManage bool
}

func (s *tenantRoleTestStore) HasCurrentGlobalAdmin(context.Context, uuid.UUID) (bool, error) {
	return s.global, nil
}
func (s *tenantRoleTestStore) GetTenantRoleControls(_ context.Context, _ uuid.UUID, actor uuid.UUID, manage bool) (storage.TenantRoleControlPage, error) {
	s.readActor = actor
	s.readManage = manage
	return storage.TenantRoleControlPage{Assignments: []storage.TenantRoleAssignment{}, History: []storage.TenantRoleEvent{}}, nil
}
func (s *tenantRoleTestStore) SaveTenantRoleControl(context.Context, uuid.UUID, uuid.UUID, storage.TenantRoleControlChange) error {
	s.changes++
	return nil
}

func TestTenantRoleControlAPIUsesAuthenticatedIdentityAndGlobalAuthority(t *testing.T) {
	tenant, node, actor := uuid.New(), uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	principal := &auth.Principal{Type: "user", Subject: "access-test", Roles: []string{roleViewer, roleAdmin}}
	base.users = map[string]*storage.User{principal.Subject: {ID: actor, ExternalID: principal.Subject}}
	store := &tenantRoleTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleViewer}}
	srv := newPatchTestServer(store)
	call := func(method, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/tenant-role-controls?tenant_id="+tenant.String()+"&user_id="+uuid.NewString(), bytes.NewBufferString(body))
		srv.handleTenantRoleControls(rec, withPrincipal(req, principal))
		return rec
	}
	require.Equal(t, 200, call("GET", "").Code)
	require.Equal(t, actor, store.readActor)
	require.False(t, store.readManage)
	require.Equal(t, 403, call("POST", `{}`).Code)
	require.Zero(t, store.changes)
	store.global = true
	require.Equal(t, 200, call("GET", "").Code)
	require.True(t, store.readManage)
	require.Equal(t, 400, call("POST", `{"unknown":true}`).Code)
	require.Equal(t, 200, call("POST", `{"action":"revoke"}`).Code)
	require.Equal(t, 1, store.changes)
	store.allowed = false
	require.Equal(t, 403, call("GET", "").Code)
	store.allowed = true
	principal.Type = "agent"
	require.Equal(t, 403, call("GET", "").Code)
}

func TestGlobalRoleManagementRequiresCurrentGlobalAuthority(t *testing.T) {
	tenant, actor := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, uuid.New())
	principal := &auth.Principal{Type: "user", Subject: "global-authority-test", Roles: []string{roleAdmin}}
	base.users = map[string]*storage.User{principal.Subject: {ID: actor, ExternalID: principal.Subject}}
	store := &tenantRoleTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}}}
	srv := newPatchTestServer(store)
	check := func() int {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest("GET", "/api/v1/roles", nil), principal)
		_, _ = srv.authorizeGlobalRoleAdmin(rec, req)
		return rec.Code
	}
	require.Equal(t, 403, check(), "a token role cannot replace current global authority")
	store.global = true
	require.Equal(t, 200, check())
	principal.Type = "agent"
	require.Equal(t, 403, check(), "an agent cannot administer human access")
	principal.Type = "user"
	srv = newPatchTestServer(struct{ Store }{base})
	require.Equal(t, 503, check(), "missing global authority verification must fail closed")
}
