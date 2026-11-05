package server

import (
	"bytes"
	"context"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

type roleReviewAPIStore struct {
	*globalRoleBoundaryStore
	reviews   int
	reviewErr error
}

func (s *roleReviewAPIStore) ListGlobalRoleSources(context.Context, uuid.UUID) ([]storage.GlobalRoleSource, error) {
	return []storage.GlobalRoleSource{}, nil
}
func (s *roleReviewAPIStore) ReviewLegacyRoleSource(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string) error {
	if s.reviewErr != nil {
		return s.reviewErr
	}
	s.reviews++
	return nil
}
func (s *roleReviewAPIStore) ChangeAdministratorRoleSource(ctx context.Context, actor, user, assignment uuid.UUID, expected, action, reason string, expiry *time.Time) error {
	return s.ReviewLegacyRoleSource(ctx, actor, user, assignment, expected, reason)
}
func TestGlobalRoleSourceReviewAPI(t *testing.T) {
	actor, target := uuid.New(), uuid.New()
	base := newPatchTestStore(uuid.New(), uuid.New())
	principal := &auth.Principal{Type: "user", Subject: "role-source-admin", Roles: []string{"admin"}}
	base.users = map[string]*storage.User{principal.Subject: {ID: actor, ExternalID: principal.Subject}}
	store := &roleReviewAPIStore{globalRoleBoundaryStore: &globalRoleBoundaryStore{tenantRoleTestStore: &tenantRoleTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}}, global: true}}}
	srv := newPatchTestServer(store)
	call := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := withPrincipal(httptest.NewRequest("POST", "/api/v1/users/"+target.String()+"/role-sources", bytes.NewBufferString(body)), principal)
		srv.handleUserSubroutes(rec, req)
		return rec
	}
	require.Equal(t, 400, call(`{"reason":"short"}`).Code)
	require.Zero(t, store.reviews)
	body := `{"assignment_id":"` + uuid.NewString() + `","expected_revision":"reviewed","reason":"Reviewed existing authorization"}`
	store.reviewErr = storage.ErrGovernanceConflict
	require.Equal(t, 409, call(body).Code)
	require.Zero(t, store.reviews)
	store.reviewErr = nil
	require.Equal(t, 200, call(body).Code)
	require.Equal(t, 1, store.reviews)
	changeBody := `{"assignment_id":"` + uuid.NewString() + `","expected_revision":"reviewed","reason":"Remove independent administrator grant","action":"revoke"}`
	require.Equal(t, 200, call(changeBody).Code)
	require.Equal(t, 2, store.reviews)
	store.reviewErr = storage.ErrRoleSourceReviewRequired
	require.Equal(t, 409, call(changeBody).Code)
	require.Equal(t, 2, store.reviews)
	store.reviewErr = nil
	store.global = false
	require.Equal(t, 403, call(body).Code)
	require.Equal(t, 2, store.reviews)
}
