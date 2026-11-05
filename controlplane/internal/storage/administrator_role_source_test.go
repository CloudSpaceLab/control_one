package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAdministratorRoleSourceChangeDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLocal, Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	user, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLDAP})
	require.NoError(t, err)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"viewer"}))
	// Explicitly simulate a pre-attribution legacy grant in the disposable fixture.
	_, err = s.db.ExecContext(ctx, `DELETE FROM global_role_sources WHERE assignment_id IN (SELECT id FROM user_roles WHERE user_id=$1 AND tenant_id IS NULL)`, user.ID)
	require.NoError(t, err)
	grants, err := s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	state, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.NoError(t, s.ReviewLegacyRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, state.Revision, "Reviewed existing viewer authorization"))
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, []string{"viewer"}))
	state, err = s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	expiry := time.Now().UTC().Add(time.Hour)
	require.ErrorIs(t, s.ChangeAdministratorRoleSource(ctx, user.ID, user.ID, grants[0].AssignmentID, state.Revision, "set_expiry", "Unauthorized viewer change", &expiry), ErrGlobalRoleAuthority)
	require.NoError(t, s.ChangeAdministratorRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, state.Revision, "set_expiry", "Time bound administrator grant", &expiry))
	require.ErrorIs(t, s.ChangeAdministratorRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, state.Revision, "revoke", "Stale administrator revocation", nil), ErrGovernanceConflict)
	current, err := s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, current, 2)
	for _, grant := range current {
		if grant.Source == "directory" {
			require.Nil(t, grant.ExpiresAt)
		} else {
			require.NotNil(t, grant.ExpiresAt)
			require.WithinDuration(t, expiry, *grant.ExpiresAt, time.Millisecond)
		}
	}
	state, err = s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.NoError(t, s.ChangeAdministratorRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, state.Revision, "revoke", "Remove administrator grant only", nil))
	roles, err := s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles)
	current, err = s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, current, 1)
	require.Equal(t, "directory", current[0].Source)
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, nil))
	roles, err = s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, roles)
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM global_role_source_reviews WHERE user_id=$1`, user.ID).Scan(&count))
	require.Equal(t, 3, count)
}
