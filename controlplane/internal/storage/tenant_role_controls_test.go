package storage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestTenantRoleControlsExpiryRevocationAndIsolation(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, e := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, e)
	other, e := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, e)
	admin, e := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Global access manager")
	require.NoError(t, e)
	require.NoError(t, s.SetUserRoles(ctx, admin.ID, []string{"admin"}))
	target, e := s.CreateLocalUser(ctx, CreateLocalUserParams{Email: uuid.NewString() + "@example.invalid", DisplayName: "Existing reviewer", Password: uuid.NewString(), Provider: AuthProviderLocal, Roles: []string{"viewer"}})
	require.NoError(t, e)
	change := TenantRoleControlChange{UserID: target.ID, Role: "ciso", Action: "grant", ExpiresAt: time.Now().UTC().Add(time.Hour), Reason: "Isolated, time-limited independent security review"}
	var group sync.WaitGroup
	out := make(chan error, 2)
	for i := 0; i < 2; i++ {
		group.Add(1)
		go func() { defer group.Done(); out <- s.SaveTenantRoleControl(ctx, tenant.ID, admin.ID, change) }()
	}
	group.Wait()
	close(out)
	successes, conflicts := 0, 0
	for err := range out {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, ErrGovernanceConflict)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	allowed, e := s.UserHasTenantRole(ctx, target.ID, tenant.ID, []string{"ciso"})
	require.NoError(t, e)
	require.True(t, allowed)
	allowed, e = s.UserHasTenantRole(ctx, target.ID, other.ID, []string{"ciso"})
	require.NoError(t, e)
	require.False(t, allowed)
	roles, e := s.ListUserRoles(ctx, target.ID)
	require.NoError(t, e)
	require.Contains(t, roles, "ciso")
	require.Contains(t, roles, "viewer")
	globalRoles, e := s.ListGlobalUserRoles(ctx, target.ID)
	require.NoError(t, e)
	require.Equal(t, []string{"viewer"}, globalRoles)
	// Saving the global editor's unchanged selection must neither promote nor
	// delete the independently managed tenant CISO assignment.
	reviewed, e := s.GetGlobalRoleState(ctx, target.ID)
	require.NoError(t, e)
	require.NoError(t, s.ReplaceGlobalRolesAuthorized(ctx, admin.ID, target.ID, reviewed.Revision, globalRoles))
	roles, e = s.ListUserRoles(ctx, target.ID)
	require.NoError(t, e)
	require.Contains(t, roles, "ciso")
	permissions, e := s.GetUserPermissions(ctx, target.ID)
	require.NoError(t, e)
	require.NotEmpty(t, permissions)
	// Only a global administrator may delegate; a scoped/global reviewer cannot.
	change.ExpectedRevision = 1
	require.Error(t, s.SaveTenantRoleControl(ctx, tenant.ID, target.ID, change))
	unauthorized := change
	unauthorized.UserID = admin.ID
	require.ErrorContains(t, s.SaveTenantRoleControl(ctx, tenant.ID, target.ID, unauthorized), "current global administrator required")
	elevated := change
	elevated.Role = "admin"
	require.Error(t, s.SaveTenantRoleControl(ctx, tenant.ID, admin.ID, elevated))
	expired := change
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	require.Error(t, s.SaveTenantRoleControl(ctx, tenant.ID, admin.ID, expired))
	change.Action = "revoke"
	require.NoError(t, s.SaveTenantRoleControl(ctx, tenant.ID, admin.ID, change))
	roles, e = s.ListUserRoles(ctx, target.ID)
	require.NoError(t, e)
	require.NotContains(t, roles, "ciso")
	require.Contains(t, roles, "viewer", "global assignment must survive scoped revocation")
	allowed, e = s.UserHasTenantRole(ctx, target.ID, tenant.ID, []string{"ciso"})
	require.NoError(t, e)
	require.False(t, allowed)
	page, e := s.GetTenantRoleControls(ctx, tenant.ID, target.ID, false)
	require.NoError(t, e)
	require.Len(t, page.History, 2)
	require.EqualValues(t, 2, page.Revision)
	for _, a := range page.Assignments {
		require.Equal(t, target.ID, a.UserID)
	}
	// Issue a normal session and then prove a naturally expiring assignment is
	// omitted by the same role read used by session authentication.
	session, e := s.IssueSession(ctx, target.ID, time.Hour, "isolated-test", "127.0.0.1")
	require.NoError(t, e)
	change.Action = "grant"
	change.ExpectedRevision = 2
	change.Role = "operator"
	change.ExpiresAt = time.Now().UTC().Add(time.Second)
	require.NoError(t, s.SaveTenantRoleControl(ctx, tenant.ID, admin.ID, change))
	time.Sleep(1100 * time.Millisecond)
	_, account, e := s.ValidateSessionToken(ctx, session.Token)
	require.NoError(t, e)
	require.Equal(t, target.ID, account.ID)
	roles, e = s.ListUserRoles(ctx, target.ID)
	require.NoError(t, e)
	require.NotContains(t, roles, "operator")
	require.Contains(t, roles, "viewer")
	// Read-only callers cannot widen their view by passing another user ID at API.
	otherPage, e := s.GetTenantRoleControls(ctx, other.ID, target.ID, false)
	require.NoError(t, e)
	require.Empty(t, otherPage.History)
	var globalAdmin int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM user_roles ur JOIN roles r ON r.id=ur.role_id WHERE ur.user_id=$1 AND ur.tenant_id IS NULL AND r.name='admin'`, target.ID).Scan(&globalAdmin))
	require.Zero(t, globalAdmin)
	// Disabled accounts cannot retain effective authority or receive a new grant.
	_, e = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=NOW() WHERE id=$1`, target.ID)
	require.NoError(t, e)
	_, _, e = s.ValidateSessionToken(ctx, session.Token)
	require.ErrorIs(t, e, ErrUserDisabled, "the existing signed-in session must stop working when the account is disabled")
	roles, e = s.ListUserRoles(ctx, target.ID)
	require.NoError(t, e)
	require.Empty(t, roles)
	permissions, e = s.GetUserPermissions(ctx, target.ID)
	require.NoError(t, e)
	require.Empty(t, permissions)
	allowed, e = s.UserHasTenantRole(ctx, target.ID, tenant.ID, []string{"viewer", "operator"})
	require.NoError(t, e)
	require.False(t, allowed)
	change.ExpectedRevision = 3
	change.ExpiresAt = time.Now().UTC().Add(time.Hour)
	require.ErrorContains(t, s.SaveTenantRoleControl(ctx, tenant.ID, admin.ID, change), "active existing account required")
	_, e = s.db.ExecContext(ctx, `UPDATE users SET disabled_at=NOW() WHERE id=$1`, admin.ID)
	require.NoError(t, e)
	allowed, e = s.HasCurrentGlobalAdmin(ctx, admin.ID)
	require.NoError(t, e)
	require.False(t, allowed)
}
