package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGlobalRoleWritersAttributeAndPreserveSources(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	user, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLDAP})
	require.NoError(t, err)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"viewer"}))
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, []string{"ciso"}))
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"operator"}))
	roles, err := s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ciso", "operator"}, roles)
	require.NoError(t, s.AssignRolesToUser(ctx, user.ID, []string{"investigator"}))
	before, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.NoError(t, s.AssignRolesToUser(ctx, user.ID, []string{"investigator"}))
	after, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, before.Revision, after.Revision)
	grants, err := s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, grants, 3)
	origins := map[string]string{}
	for _, g := range grants {
		origins[g.Role] = g.Source
	}
	require.Equal(t, map[string]string{"ciso": "directory", "operator": "administrator", "investigator": "provisioning"}, origins)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, nil))
	roles, err = s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"ciso", "investigator"}, roles)
	local, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Provider: AuthProviderLocal, Roles: []string{"viewer"}})
	require.NoError(t, err)
	grants, err = s.ListGlobalRoleSources(ctx, local.ID)
	require.NoError(t, err)
	require.Equal(t, "administrator", grants[0].Source)
	seed, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Provider: AuthProviderLocal, Roles: []string{"viewer"}, RoleSource: "provisioning"})
	require.NoError(t, err)
	grants, err = s.ListGlobalRoleSources(ctx, seed.ID)
	require.NoError(t, err)
	require.Equal(t, "provisioning", grants[0].Source)
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Provider: AuthProviderLocal, Roles: []string{"admin"}})
	require.NoError(t, err)
	localState, err := s.GetGlobalRoleState(ctx, local.ID)
	require.NoError(t, err)
	require.NoError(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, local.ID, localState.Revision, []string{"viewer"}))
	grants, err = s.ListGlobalRoleSources(ctx, local.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, "administrator", grants[0].Source)
	seedState, err := s.GetGlobalRoleState(ctx, seed.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, seed.ID, seedState.Revision, []string{"admin"}), ErrRoleSourceReviewRequired)
	grants, err = s.ListGlobalRoleSources(ctx, seed.ID)
	require.NoError(t, err)
	require.Equal(t, "provisioning", grants[0].Source)
	_, err = s.db.ExecContext(ctx, `DELETE FROM global_role_sources WHERE assignment_id IN(SELECT id FROM user_roles WHERE user_id=$1)`, local.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.AssignRolesToUser(ctx, local.ID, []string{"viewer"}), ErrRoleSourceReviewRequired)
	require.ErrorIs(t, s.SetUserRoles(ctx, local.ID, []string{"operator"}), ErrRoleSourceReviewRequired)
	roles, err = s.ListGlobalUserRoles(ctx, local.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles)
}
