package auth

import (
	"context"
	"os"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/go-ldap/ldap/v3"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type ldapRoleFixture struct {
	global []string
	tenant []string
}

func (f *ldapRoleFixture) GetGlobalRoleState(context.Context, uuid.UUID) (storage.GlobalRoleState, error) {
	return storage.GlobalRoleState{Roles: f.global}, nil
}
func (f *ldapRoleFixture) ListTenantUserRoles(context.Context, uuid.UUID) ([]string, error) {
	return f.tenant, nil
}

// This test changes only an explicitly designated disposable directory group.
func TestLDAPDisposableDirectoryGroupChange(t *testing.T) {
	url := os.Getenv("CONTROLONE_DISPOSABLE_LDAP_URL")
	if url == "" {
		t.Skip("disposable LDAP fixture is required")
	}
	if url != "ldap://phase4-directory:1389" && url != "ldaps://phase4-directory:1636" {
		t.Fatal("only the explicitly named disposable LDAP fixture may be modified")
	}
	const admin = "cn=admin,dc=controlone,dc=invalid"
	const secret = "phase4-fixture-only-2026"
	const user = "uid=reviewer,ou=people,dc=controlone,dc=invalid"
	const group = "cn=patch-reviewers,ou=groups,dc=controlone,dc=invalid"
	provider, err := NewLDAPProvider(LDAPConfig{Enabled: true, URL: url, BindDN: admin, BindPassword: secret, UserBaseDN: "ou=people,dc=controlone,dc=invalid", GroupBaseDN: "ou=groups,dc=controlone,dc=invalid", GroupRoleMap: map[string]string{"patch-reviewers": "ciso"}, DefaultRole: "viewer"}, zap.NewNop())
	require.NoError(t, err)
	conn, err := ldap.DialURL(url)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.Bind(admin, secret))
	setMember := func(value string) error {
		req := ldap.NewModifyRequest(group, nil)
		req.Replace("member", []string{value})
		return conn.Modify(req)
	}
	require.NoError(t, setMember(user))
	defer func() { _ = setMember(user) }()
	first, err := provider.Authenticate(context.Background(), "reviewer@phase4.invalid", "phase4-fixture-user-2026")
	require.NoError(t, err)
	require.Equal(t, []string{"ciso"}, first.Roles)
	current, err := provider.ReadSessionRoles(context.Background(), "reviewer@phase4.invalid")
	require.NoError(t, err)
	require.Equal(t, []string{"ciso"}, current)
	roleStore := &ldapRoleFixture{global: []string{"ciso"}, tenant: []string{"viewer"}}
	guard := NewLDAPSessionVerifier(provider, roleStore)
	sessionUser := &storage.LocalUser{ID: uuid.New(), Email: "reviewer@phase4.invalid", AuthProvider: storage.AuthProviderLDAP}
	allowed, err := guard(context.Background(), sessionUser)
	require.NoError(t, err)
	require.Equal(t, []string{"ciso", "viewer"}, allowed)
	// Additional global assignments have unknown provenance and must not be
	// silently adopted or deleted by the conservative session guard.
	roleStore.global = []string{"ciso", "operator"}
	_, err = guard(context.Background(), sessionUser)
	require.ErrorContains(t, err, "LDAP roles changed")
	require.Equal(t, []string{"ciso", "operator"}, roleStore.global)
	roleStore.global = []string{"ciso"}

	require.NoError(t, setMember(admin))
	current, err = provider.ReadSessionRoles(context.Background(), "reviewer@phase4.invalid")
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, current)
	_, err = guard(context.Background(), sessionUser)
	require.ErrorContains(t, err, "LDAP roles changed")
	require.Equal(t, []string{"ciso"}, roleStore.global, "verification must never rewrite global assignments")
	require.Equal(t, []string{"viewer"}, roleStore.tenant, "verification must preserve tenant assignments")
	second, err := provider.Authenticate(context.Background(), "reviewer@phase4.invalid", "phase4-fixture-user-2026")
	require.NoError(t, err)
	require.Equal(t, []string{"viewer"}, second.Roles)
	require.NoError(t, setMember(user))
}
