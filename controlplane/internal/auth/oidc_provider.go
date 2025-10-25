package auth

import (
	"context"
	"errors"
	"strings"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// OIDCProvider is a lightweight scaffold for verifying OIDC ID tokens.
type OIDCProvider struct {
	cfg  config.OIDCConfig
	rbac config.RBACConfig
}

// NewOIDCProvider constructs an OIDC provider helper. The current implementation
// offers minimal validation and should be extended with real OIDC verification
// in later phases.
func NewOIDCProvider(_ context.Context, oidcCfg config.OIDCConfig, rbacCfg config.RBACConfig) (*OIDCProvider, error) {
	if !oidcCfg.Enabled {
		return nil, errors.New("oidc disabled")
	}
	if strings.TrimSpace(oidcCfg.IssuerURL) == "" {
		return nil, errors.New("oidc issuer url required")
	}
	if strings.TrimSpace(oidcCfg.ClientID) == "" {
		return nil, errors.New("oidc client id required")
	}

	return &OIDCProvider{cfg: oidcCfg, rbac: rbacCfg}, nil
}

// Verify performs placeholder token validation and should be replaced with
// actual OIDC signature and claim checks. It returns a Principal seeded with
// default RBAC roles to allow subsequent middleware development.
func (p *OIDCProvider) Verify(_ context.Context, rawToken string) (*Principal, error) {
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return nil, errors.New("token required")
	}

	return &Principal{
		Type:    "user",
		Name:    p.cfg.ClientID,
		Subject: token,
		Roles:   p.resolveRoles(nil),
		Groups:  nil,
	}, nil
}

func (p *OIDCProvider) resolveRoles(groups []string) []string {
	lookup := make(map[string]struct{})
	defaultRole := strings.TrimSpace(p.rbac.DefaultRole)
	if defaultRole == "" {
		defaultRole = "viewer"
	}
	lookup[defaultRole] = struct{}{}

	for role, mappedGroups := range p.rbac.RoleMappings {
		for _, group := range groups {
			for _, candidate := range mappedGroups {
				if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(group)) {
					lookup[strings.TrimSpace(role)] = struct{}{}
				}
			}
		}
	}

	roles := make([]string, 0, len(lookup))
	for role := range lookup {
		if role == "" {
			continue
		}
		roles = append(roles, role)
	}
	return roles
}
