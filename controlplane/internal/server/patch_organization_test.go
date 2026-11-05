package server

import (
	"bytes"
	"context"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

type organizationTestStore struct {
	*operationsTestStore
	organizationWrites int
}

func (s *organizationTestStore) ListPatchOrganizationPeople(context.Context, uuid.UUID) ([]storage.PatchReviewRecipient, error) {
	return []storage.PatchReviewRecipient{}, nil
}

func (s *organizationTestStore) GetPatchOrganizationPolicy(context.Context, uuid.UUID) (*storage.PatchOrganizationPolicy, error) {
	return nil, nil
}
func (s *organizationTestStore) ListPatchOrganizationHistory(context.Context, uuid.UUID) ([]storage.PatchOrganizationPolicy, error) {
	return []storage.PatchOrganizationPolicy{}, nil
}
func (s *organizationTestStore) SavePatchOrganizationPolicy(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ int64, _ string, _ patchgovernance.OrganizationPolicy) (storage.PatchOrganizationPolicy, error) {
	s.organizationWrites++
	return storage.PatchOrganizationPolicy{}, nil
}

func TestPatchOrganizationAPIAuthorityAndStrictInput(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	principal := &auth.Principal{Type: "user", Subject: "org-admin", Roles: []string{roleViewer, roleOperator, roleAdmin}}
	base.users = map[string]*storage.User{principal.Subject: {ID: uuid.New(), ExternalID: principal.Subject}}
	store := &organizationTestStore{operationsTestStore: &operationsTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleOperator}}}
	srv := newPatchTestServer(store)
	call := func(method, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/patch/organization-policy?tenant_id="+tenant.String(), bytes.NewBufferString(body))
		srv.handlePatchOrganizationPolicy(rec, withPrincipal(req, principal))
		return rec
	}
	require.Equal(t, 403, call("POST", `{}`).Code)
	require.Zero(t, store.organizationWrites)
	require.Equal(t, 200, call("GET", "").Code)
	store.requireRole = roleAdmin
	require.Equal(t, 400, call("POST", `{"unknown":true}`).Code)
	require.Equal(t, 400, call("POST", `{} {}`).Code)
	require.Zero(t, store.organizationWrites)
	require.Equal(t, 405, call("DELETE", "").Code)
	store.requireRole = roleCISO
	principal.Roles = []string{roleCISO}
	require.Equal(t, 200, call("GET", "").Code)
	require.Equal(t, 403, call("POST", `{"action":"draft"}`).Code)
	require.Equal(t, 403, call("POST", `{"action":"submit"}`).Code)
	require.Equal(t, 200, call("POST", `{"action":"approve"}`).Code)
	require.Equal(t, 1, store.organizationWrites)
	store.allowed = false
	require.Equal(t, 403, call("GET", "").Code)
	store.allowed = true
	principal.Type = "agent"
	require.Equal(t, 403, call("GET", "").Code)
}
