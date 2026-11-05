package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLegacyRoleSourceReviewDedicatedPostgres(t *testing.T) {
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
	_, err = s.db.ExecContext(ctx, `UPDATE user_roles SET expires_at=clock_timestamp()+interval '1 hour' WHERE user_id=$1`, user.ID)
	require.NoError(t, err)
	grants, err := s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.Equal(t, "legacy_unknown", grants[0].Source)
	before, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.ReviewLegacyRoleSource(ctx, user.ID, user.ID, grants[0].AssignmentID, before.Revision, "Explicit isolated ownership review"), ErrGlobalRoleAuthority)
	require.ErrorIs(t, s.ReviewLegacyRoleSource(ctx, admin.ID, user.ID, uuid.New(), before.Revision, "Explicit isolated ownership review"), ErrGovernanceConflict)
	require.NoError(t, s.ReviewLegacyRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, before.Revision, "Explicit isolated ownership review"))
	after, err := s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, after, 1)
	require.Equal(t, "administrator", after[0].Source)
	require.Equal(t, grants[0].ExpiresAt, after[0].ExpiresAt)
	require.ErrorIs(t, s.ReviewLegacyRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, before.Revision, "Repeated stale ownership review"), ErrGovernanceConflict)
	var count int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM global_role_source_reviews WHERE user_id=$1 AND actor_id=$2`, user.ID, admin.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, []string{"ciso"}))
	require.NoError(t, s.ReconcileDirectoryRoleSources(ctx, user.ID, nil))
	roles, err := s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles)
}
