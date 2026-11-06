package server

import (
	"context"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type globalAuditStore struct {
	*globalRoleBoundaryStore
	calls  int
	filter storage.AuditLogFilter
}

func (s *globalAuditStore) ListAuditLogs(_ context.Context, f storage.AuditLogFilter, _, _ int) ([]storage.AuditLog, int, error) {
	s.calls++
	s.filter = f
	return []storage.AuditLog{}, 0, nil
}
func TestGlobalIdentityAuditRequiresCurrentGlobalAdmin(t *testing.T) {
	actor := uuid.New()
	base := newPatchTestStore(uuid.New(), uuid.New())
	p := &auth.Principal{Type: "user", Subject: "audit-admin", Roles: []string{"admin"}}
	base.users = map[string]*storage.User{p.Subject: {ID: actor, ExternalID: p.Subject}}
	store := &globalAuditStore{globalRoleBoundaryStore: &globalRoleBoundaryStore{tenantRoleTestStore: &tenantRoleTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}}, global: true}}}
	srv := newPatchTestServer(store)
	call := func(query string) int {
		w := httptest.NewRecorder()
		srv.handleAuditCollection(w, withPrincipal(httptest.NewRequest("GET", "/api/v1/audit?"+query, nil), p))
		return w.Code
	}
	require.Equal(t, 200, call("scope=global_identity&action=user.roles.change"))
	require.True(t, store.filter.GlobalIdentity)
	require.Equal(t, uuid.Nil, store.filter.TenantID)
	require.Equal(t, "user.roles.change", store.filter.Action)
	require.Equal(t, 400, call("scope=global_identity&tenant_id="+uuid.NewString()))
	require.Equal(t, 400, call("scope=other"))
	require.Equal(t, 1, store.calls)
	store.global = false
	require.Equal(t, 403, call("scope=global_identity"))
	require.Equal(t, 1, store.calls)
	p.Roles = []string{"operator"}
	require.Equal(t, 403, call("scope=global_identity"))
	require.Equal(t, 1, store.calls)
}
