package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"sync"
	"testing"
	"time"
)

func TestAuthenticationConcurrentFirstSignIn(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	subject := uuid.NewString()
	email := subject + "@fixture.invalid"
	var wg sync.WaitGroup
	start := make(chan struct{})
	type result struct {
		id  uuid.UUID
		err error
	}
	results := make(chan result, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			u, _, err := s.PersistAuthenticationRoles(ctx, "oidc:https://fixture.invalid", subject, email, "Concurrent reviewer", []string{"viewer"})
			r := result{err: err}
			if u != nil {
				r.id = u.ID
			}
			results <- r
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var expected uuid.UUID
	for r := range results {
		require.NoError(t, r.err)
		if expected == uuid.Nil {
			expected = r.id
		}
		require.Equal(t, expected, r.id)
	}
	var users, events int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE email=$1`, email).Scan(&users))
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM authentication_role_events WHERE user_id=$1`, expected).Scan(&events))
	require.Equal(t, 1, users)
	require.Equal(t, 1, events)
}

func TestAuthenticationRoleReconciliationDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	subject := uuid.NewString()
	u, roles, err := s.PersistAuthenticationRoles(ctx, "static", subject, "", "Isolated provider fixture", []string{"operator", "viewer"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"operator", "viewer"}, roles)
	require.NoError(t, s.SetUserRoles(ctx, u.ID, []string{"investigator"}))
	_, roles, err = s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"viewer"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"viewer", "investigator"}, roles)
	grants, err := s.ListGlobalRoleSources(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, grants, 2)
	var events int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM authentication_role_events WHERE user_id=$1`, u.ID).Scan(&events))
	require.Equal(t, 2, events)
	state, err := s.GetGlobalRoleState(ctx, u.ID)
	require.NoError(t, err)
	_, _, err = s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"viewer", "viewer"})
	require.NoError(t, err)
	again, err := s.GetGlobalRoleState(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, state.Revision, again.Revision)
	deadline := time.Now().Add(-time.Minute)
	_, err = s.db.ExecContext(ctx, `UPDATE global_role_sources SET expires_at=$2 WHERE source='authentication' AND assignment_id IN(SELECT id FROM user_roles WHERE user_id=$1)`, u.ID, deadline)
	require.NoError(t, err)
	_, roles, err = s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"viewer"})
	require.NoError(t, err)
	require.Equal(t, []string{"investigator"}, roles, "repeated claims must not renew an expired source")
	_, roles, err = s.PersistAuthenticationRoles(ctx, "static", subject, "", "", nil)
	require.NoError(t, err)
	require.Equal(t, []string{"investigator"}, roles)
	other, _, err := s.PersistAuthenticationRoles(ctx, "oidc:https://issuer.example.invalid", subject, "", "", []string{"viewer"})
	require.NoError(t, err)
	require.NotEqual(t, u.ID, other.ID, "issuer boundary must isolate same-subject identities")
	legacy, err := s.EnsureUser(ctx, uuid.NewString(), "", "")
	require.NoError(t, err)
	_, _, err = s.PersistAuthenticationRoles(ctx, "static", legacy.ExternalID, "", "", []string{"admin"})
	require.ErrorIs(t, err, ErrRoleSourceReviewRequired)
	_, err = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, u.ID)
	require.NoError(t, err)
	_, _, err = s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"admin"})
	require.Error(t, err)
	// Actual queued execution must not trust an old authentication snapshot.
	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer tx.Rollback()
	require.ErrorContains(t, automationRole(ctx, tx, uuid.New(), other.ID, "individual_approval"), "execution-time provider revalidation")
}
func TestAuthenticationRoleEventFailureRollsBack(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	subject := uuid.NewString()
	u, _, err := s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"operator"})
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `CREATE FUNCTION authentication_fixture_fault() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'isolated auth event failure'; END $$; CREATE TRIGGER authentication_fixture_fault BEFORE INSERT ON authentication_role_events FOR EACH ROW EXECUTE FUNCTION authentication_fixture_fault()`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(ctx, `DROP TRIGGER IF EXISTS authentication_fixture_fault ON authentication_role_events; DROP FUNCTION IF EXISTS authentication_fixture_fault()`)
	})
	_, _, err = s.PersistAuthenticationRoles(ctx, "static", subject, "", "", []string{"viewer"})
	require.Error(t, err)
	roles, err := s.ListUserRoles(ctx, u.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"operator"}, roles, "grant removal must roll back when audit persistence fails")
}
