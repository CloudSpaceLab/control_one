package storage

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestDirectoryRoleSourceReconciliationDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	user, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", DisplayName: "Disposable directory user", Provider: AuthProviderLDAP})
	require.NoError(t, err)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"viewer"}))
	// Explicitly simulate a pre-attribution legacy grant in the disposable fixture.
	_, err = s.db.ExecContext(ctx, `DELETE FROM global_role_sources WHERE assignment_id IN (SELECT id FROM user_roles WHERE user_id=$1 AND tenant_id IS NULL)`, user.ID)
	require.NoError(t, err)
	// Existing unsourced grants must never be silently interpreted as LDAP.
	require.ErrorIs(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, []string{"ciso"}), ErrRoleSourceReviewRequired)
	roles, err := s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles)
	beforeReview, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO global_role_sources(assignment_id,source) SELECT id,'administrator' FROM user_roles WHERE user_id=$1 AND tenant_id IS NULL`, user.ID)
	require.NoError(t, err)
	afterReview, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.NotEqual(t, beforeReview.Revision, afterReview.Revision, "changing provenance must invalidate the reviewed snapshot")
	// The same viewer assignment is independently granted by both sources.
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, []string{"viewer", "ciso"}))
	directoryRoles, err := s.ListDirectoryGlobalRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"ciso", "viewer"}, directoryRoles)
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM global_role_sources g JOIN user_roles ur ON ur.id=g.assignment_id JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND r.name='viewer'`, user.ID).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, nil))
	directoryRoles, err = s.ListDirectoryGlobalRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, directoryRoles)
	roles, err = s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles, "directory removal must retain the independent administrator grant")
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", DisplayName: "Disposable administrator", Provider: AuthProviderLocal, Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	state, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, user.ID, state.Revision, []string{"admin"}), ErrRoleSourceReviewRequired)
	roles, err = s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles, "unqualified editor must not erase source-aware grants")
	// An expired independent administrator grant must not inherit the
	// directory grant's permanent lifetime after membership is removed.
	beforeExpiry, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `UPDATE global_role_sources g SET expires_at=clock_timestamp()-interval '1 second' FROM user_roles ur WHERE g.assignment_id=ur.id AND ur.user_id=$1 AND g.source='administrator'`, user.ID)
	require.NoError(t, err)
	afterExpiry, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.NotEqual(t, beforeExpiry.Revision, afterExpiry.Revision)
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, []string{"viewer"}))
	roles, err = s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles, "active directory grant must survive the administrator deadline")
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, nil))
	roles, err = s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Empty(t, roles, "expired administrator grant must not be revived by directory reconciliation")
}
