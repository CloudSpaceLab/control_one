package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
)

type LDAPSessionRoleStore interface {
	GetGlobalRoleState(context.Context, uuid.UUID) (storage.GlobalRoleState, error)
	ListTenantUserRoles(context.Context, uuid.UUID) ([]string, error)
}
type LDAPSessionRoleReader interface {
	ReadSessionRoles(context.Context, string) ([]string, error)
}

// NewLDAPSessionVerifier checks current directory evidence on every request.
// Global role provenance is not yet available, so differences block the
// session; this deliberately does not rewrite any global or tenant grant.
func NewLDAPSessionVerifier(directory LDAPSessionRoleReader, store LDAPSessionRoleStore) func(context.Context, *storage.LocalUser) ([]string, error) {
	return func(ctx context.Context, user *storage.LocalUser) ([]string, error) {
		if directory == nil || store == nil || user == nil {
			return nil, errors.New("LDAP session verification unavailable")
		}
		current, err := directory.ReadSessionRoles(ctx, user.Email)
		if err != nil {
			return nil, err
		}
		state, err := store.GetGlobalRoleState(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		normalize := func(in []string) map[string]bool {
			out := make(map[string]bool)
			for _, v := range in {
				out[strings.ToLower(strings.TrimSpace(v))] = true
			}
			return out
		}
		directoryRoles := state.Roles
		if sourced, ok := store.(interface {
			ListDirectoryGlobalRoles(context.Context, uuid.UUID) ([]string, error)
		}); ok {
			directoryRoles, err = sourced.ListDirectoryGlobalRoles(ctx, user.ID)
			if err != nil {
				return nil, err
			}
		}
		expected, stored := normalize(current), normalize(directoryRoles)
		if len(expected) != len(stored) {
			return nil, errors.New("LDAP roles changed or cannot be reconciled; sign in again")
		}
		for role := range expected {
			if !stored[role] {
				return nil, errors.New("LDAP roles changed or cannot be reconciled; sign in again")
			}
		}
		tenantRoles, err := store.ListTenantUserRoles(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		return append(state.Roles, tenantRoles...), nil
	}
}
