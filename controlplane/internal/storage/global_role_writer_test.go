package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
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

func TestGlobalRoleSavePreservesDeadlinesAndAtomicAudit(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLocal, Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	user, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLocal, Password: uuid.NewString(), Roles: []string{"viewer"}})
	require.NoError(t, err)
	grants, err := s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	state, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	require.NoError(t, s.ChangeAdministratorRoleSource(ctx, admin.ID, user.ID, grants[0].AssignmentID, state.Revision, "set_expiry", "Bound temporary review access", &deadline))
	state, err = s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.NoError(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, user.ID, state.Revision, []string{"viewer", "operator"}))
	grants, err = s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	for _, g := range grants {
		if g.Role == "viewer" {
			require.True(t, deadline.Equal(*g.ExpiresAt))
		} else {
			require.Nil(t, g.ExpiresAt)
		}
	}
	var actor uuid.UUID
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT assigned_by FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND r.name='operator'`, user.ID).Scan(&actor))
	require.Equal(t, admin.ID, actor)
	var auditCount int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM global_role_change_events WHERE user_id=$1 AND actor_id=$2 AND jsonb_array_length(previous_grants)=1 AND jsonb_array_length(current_grants)=2`, user.ID, admin.ID).Scan(&auditCount))
	require.Equal(t, 1, auditCount)
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, user.ID, state.Revision, nil), ErrGovernanceConflict)
	// Internal replacement must preserve retained source expiry too.
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"viewer", "operator"}))
	grants, err = s.ListGlobalRoleSources(ctx, user.ID)
	require.NoError(t, err)
	for _, g := range grants {
		if g.Role == "viewer" {
			require.True(t, deadline.Equal(*g.ExpiresAt))
		}
	}
	// Reject the audit insert in this disposable database: no role change may commit.
	_, err = s.db.ExecContext(ctx, `CREATE FUNCTION p4_reject_role_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated audit failure'; END $$; CREATE TRIGGER p4_reject_role_audit BEFORE INSERT ON global_role_change_events FOR EACH ROW EXECUTE FUNCTION p4_reject_role_audit()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		s.db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS p4_reject_role_audit ON global_role_change_events; DROP FUNCTION IF EXISTS p4_reject_role_audit()`)
	})
	state, err = s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.Error(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, user.ID, state.Revision, []string{"ciso"}))
	after, err := s.GetGlobalRoleState(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, state, after)
}
