package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type ldapFixture struct {
	user  *auth.LDAPUser
	calls int
}

func (l *ldapFixture) Authenticate(context.Context, string, string) (*auth.LDAPUser, error) {
	l.calls++
	return l.user, nil
}

type ldapAccountFixture struct {
	*fakeStore
	existing    *storage.LocalUser
	provisioned *storage.LocalUser
	syncErr     error
	assigned    []string
	syncCalls   int
}

func (f *ldapAccountFixture) GetLocalUserByEmail(context.Context, string) (*storage.LocalUser, error) {
	return f.existing, nil
}
func (f *ldapAccountFixture) CreateLocalUser(_ context.Context, p storage.CreateLocalUserParams) (*storage.LocalUser, error) {
	if len(p.Roles) != 0 {
		return nil, errors.New("unexpected role assignment during provision")
	}
	return f.provisioned, nil
}
func (f *ldapAccountFixture) ReconcileDirectoryRoleSources(_ context.Context, _ uuid.UUID, roles []string) error {
	f.syncCalls++
	f.assigned = append([]string{}, roles...)
	return f.syncErr
}

func TestLDAPSignInRequiresCurrentAccountAndSuccessfulRoleSync(t *testing.T) {
	for _, tc := range []struct {
		name                                  string
		disabledExisting, disabledProvisioned bool
		roles                                 []string
		syncErr                               error
		wantErr                               bool
		wantBind, wantSync                    int
	}{
		{name: "active synchronized", roles: []string{"viewer"}, wantBind: 1, wantSync: 1},
		{name: "empty roles clear old grants", roles: []string{}, wantBind: 1, wantSync: 1},
		{name: "sync failure denies session", roles: []string{"viewer"}, syncErr: errors.New("sync failed"), wantErr: true, wantBind: 1, wantSync: 1},
		{name: "disabled existing account", disabledExisting: true, wantErr: true},
		{name: "disabled provisioned account", disabledProvisioned: true, wantErr: true, wantBind: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := &storage.LocalUser{ID: uuid.New(), AuthProvider: storage.AuthProviderLDAP, Email: "reviewer@example.invalid"}
			created := &storage.LocalUser{ID: existing.ID, AuthProvider: storage.AuthProviderLDAP, Email: "reviewer@example.invalid"}
			if tc.disabledExisting {
				existing.DisabledAt = sql.NullTime{Time: time.Now(), Valid: true}
			}
			if tc.disabledProvisioned {
				created.DisabledAt = sql.NullTime{Time: time.Now(), Valid: true}
			}
			store := &ldapAccountFixture{fakeStore: &fakeStore{}, existing: existing, provisioned: created, syncErr: tc.syncErr}
			ldap := &ldapFixture{user: &auth.LDAPUser{Email: "reviewer@example.invalid", Roles: tc.roles}}
			srv := &Server{store: store, ldapProvider: ldap}
			_, _, err := srv.authenticateOperator(context.Background(), "reviewer@example.invalid", "fixture-password")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.roles, store.assigned)
			}
			require.Equal(t, tc.wantBind, ldap.calls)
			require.Equal(t, tc.wantSync, store.syncCalls)
		})
	}
}

func TestLDAPLoginRejectsExistingProviderAndIdentityMismatch(t *testing.T) {
	for _, tc := range []struct {
		name, provider, returned string
		wantBind                 int
	}{
		{"existing OIDC identity", storage.AuthProviderOIDC, "reviewer@example.invalid", 0},
		{"different directory email", storage.AuthProviderLDAP, "other@example.invalid", 1},
		{"empty directory email", storage.AuthProviderLDAP, "", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := &storage.LocalUser{ID: uuid.New(), AuthProvider: tc.provider, Email: "reviewer@example.invalid"}
			store := &ldapAccountFixture{fakeStore: &fakeStore{}, existing: existing, provisioned: existing}
			ldap := &ldapFixture{user: &auth.LDAPUser{Email: tc.returned, Roles: []string{"admin"}}}
			srv := &Server{store: store, ldapProvider: ldap}
			_, _, err := srv.authenticateOperator(context.Background(), "reviewer@example.invalid", "fixture-password")
			require.ErrorIs(t, err, storage.ErrInvalidCredentials)
			require.Equal(t, tc.wantBind, ldap.calls)
			require.Zero(t, store.syncCalls, "identity mismatch must never change roles")
		})
	}
}
