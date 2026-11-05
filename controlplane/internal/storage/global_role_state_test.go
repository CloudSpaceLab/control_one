package storage

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestGlobalRoleAuthorizedSnapshotAndAuthorityRaces(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	actor, e := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Isolated actor")
	require.NoError(t, e)
	target, e := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Isolated target")
	require.NoError(t, e)
	require.NoError(t, s.SetUserRoles(ctx, actor.ID, []string{"admin"}))
	require.NoError(t, s.SetUserRoles(ctx, target.ID, []string{"viewer"}))
	old, e := s.GetGlobalRoleState(ctx, target.ID)
	require.NoError(t, e)
	require.NoError(t, s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, old.Revision, []string{"operator"}))
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, old.Revision, []string{"ciso"}), ErrGovernanceConflict)
	// Even replacement with identical role names invalidates the old snapshot.
	state, e := s.GetGlobalRoleState(ctx, target.ID)
	require.NoError(t, e)
	require.NoError(t, s.SetUserRoles(ctx, target.ID, []string{"operator"}))
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, state.Revision, []string{"ciso"}), ErrGovernanceConflict)
	state, e = s.GetGlobalRoleState(ctx, target.ID)
	require.NoError(t, e)
	require.NoError(t, s.SetUserRoles(ctx, actor.ID, []string{"viewer"}))
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, state.Revision, []string{"ciso"}), ErrGlobalRoleAuthority)
	require.NoError(t, s.SetUserRoles(ctx, actor.ID, []string{"admin"}))
	// Wait for a target lock while the administrator's real assignment expires.
	tx, e := s.db.BeginTx(ctx, nil)
	require.NoError(t, e)
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, target.ID)
	require.NoError(t, e)
	_, e = s.db.ExecContext(ctx, `UPDATE user_roles SET expires_at=clock_timestamp()+interval '200 milliseconds' WHERE user_id=$1 AND tenant_id IS NULL`, actor.ID)
	require.NoError(t, e)
	result := make(chan error, 1)
	go func() {
		result <- s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, state.Revision, []string{"ciso"})
	}()
	_, e = tx.ExecContext(ctx, `SELECT pg_sleep(0.35)`)
	require.NoError(t, e)
	require.NoError(t, tx.Commit())
	require.ErrorIs(t, <-result, ErrGlobalRoleAuthority, "authority must use current time after lock waits, not transaction start")
	final, e := s.GetGlobalRoleState(ctx, target.ID)
	require.NoError(t, e)
	require.Equal(t, []string{"operator"}, final.Roles)
	// Revocation that is in progress before the save must win after its
	// transaction commits; an earlier handler check supplies no authority here.
	require.NoError(t, s.SetUserRoles(ctx, actor.ID, []string{"admin"}))
	revoke, e := s.db.BeginTx(ctx, nil)
	require.NoError(t, e)
	defer revoke.Rollback()
	_, e = revoke.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, actor.ID)
	require.NoError(t, e)
	_, e = revoke.ExecContext(ctx, `DELETE FROM user_roles WHERE user_id=$1 AND tenant_id IS NULL`, actor.ID)
	require.NoError(t, e)
	waiting := make(chan error, 1)
	go func() {
		waiting <- s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, final.Revision, []string{"ciso"})
	}()
	require.NoError(t, revoke.Commit())
	require.ErrorIs(t, <-waiting, ErrGlobalRoleAuthority)
	// Two reviewed drafts at the same revision must yield exactly one commit.
	require.NoError(t, s.SetUserRoles(ctx, actor.ID, []string{"admin"}))
	start, outcomes := make(chan struct{}), make(chan error, 2)
	for _, roles := range [][]string{{"viewer"}, {"operator"}} {
		go func(set []string) {
			<-start
			outcomes <- s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, final.Revision, set)
		}(roles)
	}
	close(start)
	successes, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-outcomes
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrGovernanceConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	final, e = s.GetGlobalRoleState(ctx, target.ID)
	require.NoError(t, e)
	// A disabled identity cannot save through a previously authenticated session.
	require.NoError(t, s.SetUserRoles(ctx, actor.ID, []string{"admin"}))
	_, e = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, actor.ID)
	require.NoError(t, e)
	require.ErrorIs(t, s.ReplaceGlobalRolesAuthorized(ctx, actor.ID, target.ID, final.Revision, []string{"ciso"}), ErrGlobalRoleAuthority)
}
