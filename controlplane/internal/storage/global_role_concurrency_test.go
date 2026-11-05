package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestGlobalRoleReplacementSerializesOnUser(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	user, err := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Isolated role concurrency")
	require.NoError(t, err)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{"viewer"}))

	// An empty assignment set still has a stable lock boundary. Hold that row
	// independently to prove a replacement cannot bypass the serialization.
	tx, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, user.ID)
	require.NoError(t, err)
	blockedCtx, stopBlocked := context.WithTimeout(ctx, 150*time.Millisecond)
	err = s.SetUserRoles(blockedCtx, user.ID, []string{"ciso"})
	stopBlocked()
	require.Error(t, err, "replacement must wait for the account lock")
	require.NoError(t, tx.Commit())
	roles, err := s.ListGlobalUserRoles(ctx, user.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, roles, "cancelled replacement must leave the previous roles intact")

	for round := 0; round < 6; round++ {
		// Exercise both an empty baseline and an existing assignment baseline.
		if round%2 == 0 {
			require.NoError(t, s.SetUserRoles(ctx, user.ID, nil))
		}
		start, outcomes := make(chan struct{}), make(chan error, 2)
		for _, replacement := range [][]string{{"viewer", "operator"}, {"ciso"}} {
			go func(set []string) {
				<-start
				outcomes <- s.SetUserRoles(ctx, user.ID, set)
			}(replacement)
		}
		close(start)
		require.NoError(t, <-outcomes)
		require.NoError(t, <-outcomes)
		roles, err = s.ListGlobalUserRoles(ctx, user.ID)
		require.NoError(t, err)
		require.True(t, len(roles) == 1 && roles[0] == "ciso" || len(roles) == 2 && roles[0] == "operator" && roles[1] == "viewer", "result must be one whole replacement, got %v", roles)
	}
	require.ErrorIs(t, s.SetUserRoles(ctx, uuid.New(), []string{"viewer"}), sql.ErrNoRows)
}
