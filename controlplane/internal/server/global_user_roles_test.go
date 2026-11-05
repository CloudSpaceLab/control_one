package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type globalRoleBoundaryStore struct {
	*tenantRoleTestStore
	writes         int
	reviewRequired bool
}

func (s *globalRoleBoundaryStore) ListUserRoles(context.Context, uuid.UUID) ([]string, error) {
	return []string{"viewer", "ciso"}, nil // Effective roles include a tenant grant.
}
func (s *globalRoleBoundaryStore) ListGlobalUserRoles(context.Context, uuid.UUID) ([]string, error) {
	return []string{"viewer"}, nil
}
func (s *globalRoleBoundaryStore) SetUserRoles(ctx context.Context, user uuid.UUID, roles []string) error {
	s.writes++
	return s.fakeStore.SetUserRoles(ctx, user, roles)
}

func TestGlobalUserEditorDoesNotRoundTripTenantAssignments(t *testing.T) {
	actor, target := uuid.New(), uuid.New()
	base := newPatchTestStore(uuid.New(), uuid.New())
	principal := &auth.Principal{Type: "user", Subject: "global-editor-admin", Roles: []string{"admin"}}
	base.users = map[string]*storage.User{principal.Subject: {ID: actor, ExternalID: principal.Subject}}
	user := storage.User{ID: target, ExternalID: "existing-reviewer"}
	base.usersByID = map[uuid.UUID]*storage.User{target: &user}
	base.userList = []storage.User{user}
	store := &globalRoleBoundaryStore{tenantRoleTestStore: &tenantRoleTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}}, global: true}}
	srv := newPatchTestServer(store)
	call := func(method, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest(method, "/api/v1/users/"+target.String(), bytes.NewBufferString(body)), principal)
		switch method {
		case "GET":
			srv.handleGetUser(rec, req, target)
		case "PATCH":
			srv.handleUpdateUserRoles(rec, req, target)
		default:
			srv.handleListUsers(rec, req)
		}
		return rec
	}
	for _, method := range []string{"LIST", "GET", "PATCH"} {
		rec := call(method, `{"roles":["viewer"],"role_scope":"global","expected_revision":"fixture-role-revision"}`)
		require.Equal(t, 200, rec.Code, rec.Body.String())
		var result userResponse
		if method == "LIST" {
			var page paginatedResponse[userResponse]
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &page))
			require.Len(t, page.Data, 1)
			result = page.Data[0]
		} else {
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
		}
		require.Equal(t, "global", result.RoleScope)
		require.Equal(t, []string{"viewer"}, result.Roles)
	}
	require.Equal(t, 1, store.writes)
	for _, body := range []string{`{"roles":["viewer","ciso"]}`, `{"roles":["viewer"],"role_scope":"tenant"}`} {
		require.Equal(t, 400, call("PATCH", body).Code)
	}
	require.Equal(t, 1, store.writes, "unscoped legacy or tenant writes must fail before mutation")
	require.Equal(t, 409, call("PATCH", `{"roles":["ciso"],"role_scope":"global","expected_revision":"stale"}`).Code)
	require.Equal(t, 400, call("PATCH", `{"roles":["ciso"],"role_scope":"global"}`).Code)
	require.Equal(t, 1, store.writes, "conflicting and missing revisions must not mutate")
	store.reviewRequired = true
	denied := call("PATCH", `{"roles":["admin"],"role_scope":"global","expected_revision":"fixture-role-revision"}`)
	require.Equal(t, 409, denied.Code)
	require.Contains(t, denied.Body.String(), "independently managed role sources")
	require.Equal(t, 1, store.writes)

}

func (s *globalRoleBoundaryStore) GetGlobalRoleState(ctx context.Context, user uuid.UUID) (storage.GlobalRoleState, error) {
	roles, err := s.ListGlobalUserRoles(ctx, user)
	return storage.GlobalRoleState{Roles: roles, Revision: "fixture-role-revision"}, err
}
func (s *globalRoleBoundaryStore) ReplaceGlobalRolesAuthorized(ctx context.Context, actor, target uuid.UUID, expected string, roles []string) error {
	if s.reviewRequired {
		return storage.ErrRoleSourceReviewRequired
	}
	if expected != "fixture-role-revision" {
		return storage.ErrGovernanceConflict
	}
	return s.SetUserRoles(ctx, target, roles)
}
