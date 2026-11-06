package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAuthenticationIdentityMigrationReviewedAndAudited(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLocal, Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	subject := uuid.NewString()
	legacy, err := s.EnsureUser(ctx, subject, uuid.NewString()+"@example.invalid", "Legacy review fixture")
	require.NoError(t, err)
	require.NoError(t, s.AssignRolesToUser(ctx, legacy.ID, []string{"operator"}))
	_, err = s.db.ExecContext(ctx, `UPDATE global_role_sources SET expires_at=clock_timestamp()+interval '1 hour' WHERE assignment_id IN(SELECT id FROM user_roles WHERE user_id=$1)`, legacy.ID)
	require.NoError(t, err)
	before, err := s.ListGlobalRoleSources(ctx, legacy.ID)
	require.NoError(t, err)
	state, err := s.GetGlobalRoleState(ctx, legacy.ID)
	require.NoError(t, err)
	_, _, err = s.PersistAuthenticationRoles(ctx, "static", subject, legacy.Email.String, "", []string{"viewer"})
	require.ErrorIs(t, err, ErrRoleSourceReviewRequired)
	require.ErrorIs(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, legacy.ID, "stale", "static", subject, "Reviewed provider migration"), ErrGovernanceConflict)
	require.ErrorIs(t, s.MigrateAuthenticationIdentity(ctx, legacy.ID, legacy.ID, state.Revision, "static", subject, "Unauthorized migration request"), ErrGlobalRoleAuthority)
	require.ErrorIs(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, legacy.ID, state.Revision, "static", "different", "Reviewed wrong subject"), ErrRoleSourceReviewRequired)
	require.NoError(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, legacy.ID, state.Revision, "static", subject, "Approved provider identity; retain temporary operator access"))
	after, err := s.ListGlobalRoleSources(ctx, legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "administrator", after[0].Source)
	require.True(t, before[0].ExpiresAt.Equal(*after[0].ExpiresAt))
	migrated, roles, err := s.PersistAuthenticationRoles(ctx, "static", subject, legacy.Email.String, "", []string{"viewer"})
	require.NoError(t, err)
	require.Equal(t, legacy.ID, migrated.ID)
	require.ElementsMatch(t, []string{"operator", "viewer"}, roles)
	_, roles, err = s.PersistAuthenticationRoles(ctx, "static", subject, legacy.Email.String, "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"operator"}, roles)
	logs, total, err := s.ListAuditLogs(ctx, AuditLogFilter{GlobalIdentity: true, Action: "user.authentication_identity.migrate", ResourceID: legacy.ID.String()}, 10, 0)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, admin.ID, logs[0].ActorID)
	require.Contains(t, logs[0].Metadata, "previous_grants")
	_, total, err = s.ListAuditLogs(ctx, AuditLogFilter{TenantID: uuid.New()}, 10, 0)
	require.NoError(t, err)
	require.Zero(t, total)
	require.ErrorIs(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, admin.ID, state.Revision, "static", subject, "Do not migrate own password account"), ErrGlobalRoleAuthority)
	eligible, err := s.AuthenticationMigrationEligible(ctx, admin.ID)
	require.NoError(t, err)
	require.False(t, eligible)
}
func TestAuthenticationIdentityMigrationAuditFailureRollsBack(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	subject := uuid.NewString()
	u, err := s.EnsureUser(ctx, subject, "", "")
	require.NoError(t, err)
	require.NoError(t, s.AssignRolesToUser(ctx, u.ID, []string{"viewer"}))
	state, err := s.GetGlobalRoleState(ctx, u.ID)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `CREATE FUNCTION migration_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated audit rejection';END $$;CREATE TRIGGER migration_audit_fault BEFORE INSERT ON authentication_identity_migrations FOR EACH ROW EXECUTE FUNCTION migration_audit_fault()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		s.db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS migration_audit_fault ON authentication_identity_migrations;DROP FUNCTION IF EXISTS migration_audit_fault()`)
	})
	require.Error(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, u.ID, state.Revision, "static", subject, "Reviewed rollback failure injection"))
	original, err := s.GetUser(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, subject, original.ExternalID)
	after, err := s.GetGlobalRoleState(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, state, after)
}

func TestAuthenticationIdentityMigrationCollisionAndDisabledAuthority(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	subject := uuid.NewString()
	existing, _, err := s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"viewer"})
	require.NoError(t, err)
	legacy, err := s.EnsureUser(ctx, subject, "", "")
	require.NoError(t, err)
	require.NoError(t, s.AssignRolesToUser(ctx, legacy.ID, []string{"viewer"}))
	state, err := s.GetGlobalRoleState(ctx, legacy.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, legacy.ID, state.Revision, "static", subject, "Do not merge an existing bound identity"), ErrGovernanceConflict)
	require.NotEqual(t, existing.ID, legacy.ID)
	otherSubject := uuid.NewString()
	other, err := s.EnsureUser(ctx, otherSubject, "", "")
	require.NoError(t, err)
	state, err = s.GetGlobalRoleState(ctx, other.ID)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, admin.ID)
	require.NoError(t, err)
	require.ErrorIs(t, s.MigrateAuthenticationIdentity(ctx, admin.ID, other.ID, state.Revision, "static", otherSubject, "Disabled administrators cannot migrate"), ErrGlobalRoleAuthority)
	ldap, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Provider: AuthProviderLDAP})
	require.NoError(t, err)
	eligible, err := s.AuthenticationMigrationEligible(ctx, ldap.ID)
	require.NoError(t, err)
	require.False(t, eligible)
}

func TestAuthenticationIdentityMigrationConcurrentDrafts(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	admin, err := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", Password: uuid.NewString(), Roles: []string{"admin"}})
	require.NoError(t, err)
	subject := uuid.NewString()
	u, err := s.EnsureUser(ctx, subject, "", "")
	require.NoError(t, err)
	require.NoError(t, s.AssignRolesToUser(ctx, u.ID, []string{"viewer"}))
	state, err := s.GetGlobalRoleState(ctx, u.ID)
	require.NoError(t, err)
	start, results := make(chan struct{}), make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			results <- s.MigrateAuthenticationIdentity(ctx, admin.ID, u.ID, state.Revision, "static", subject, "Concurrent reviewed identity migration")
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrGovernanceConflict)
		}
	}
	require.Equal(t, 1, successes)
	_, err = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, u.ID)
	require.NoError(t, err)
	eligible, err := s.AuthenticationMigrationEligible(ctx, u.ID)
	require.NoError(t, err)
	require.False(t, eligible)
}
