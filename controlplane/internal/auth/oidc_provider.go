package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

// OIDCProvider performs production-grade OIDC ID token verification.
type OIDCProvider struct {
	cfg      config.OIDCConfig
	rbac     config.RBACConfig
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
}

// NewOIDCProvider constructs an OIDC helper with a cached verifier.
func NewOIDCProvider(ctx context.Context, oidcCfg config.OIDCConfig, rbacCfg config.RBACConfig) (*OIDCProvider, error) {
	if !oidcCfg.Enabled {
		return nil, errors.New("oidc disabled")
	}
	issuer := strings.TrimSpace(oidcCfg.IssuerURL)
	if issuer == "" {
		return nil, errors.New("oidc issuer url required")
	}
	clientID := strings.TrimSpace(oidcCfg.ClientID)
	if clientID == "" {
		return nil, errors.New("oidc client id required")
	}

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("init oidc provider: %w", err)
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: clientID})

	return &OIDCProvider{
		cfg:      oidcCfg,
		rbac:     rbacCfg,
		provider: provider,
		verifier: verifier,
	}, nil
}

// Verify validates an ID token and maps claims into a Principal.
func (p *OIDCProvider) Verify(ctx context.Context, rawToken string) (*Principal, error) {
	token := strings.TrimSpace(rawToken)
	if token == "" {
		return nil, errors.New("token required")
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	idToken, err := p.verifier.Verify(ctx, token)
	if err != nil {
		return nil, fmt.Errorf("verify oidc token: %w", err)
	}

	var rawClaims map[string]any
	if err := idToken.Claims(&rawClaims); err != nil {
		return nil, fmt.Errorf("parse oidc claims: %w", err)
	}

	audValues := extractStringSlice(rawClaims, "aud")
	if len(p.cfg.Audience) > 0 && !audAllowed(audValues, p.cfg.Audience) {
		return nil, fmt.Errorf("token audience not in allowed set")
	}

	subject := strings.TrimSpace(claimString(rawClaims, "sub"))
	if subject == "" {
		return nil, errors.New("token missing subject claim")
	}

	email := strings.TrimSpace(claimString(rawClaims, "email"))
	groups := p.resolveGroups(rawClaims)
	directRoles := extractStringSlice(rawClaims, "roles")

	principal := &Principal{
		Type:    "user",
		Subject: subject,
		Email:   email,
		Groups:  groups,
	}

	principal.Name = p.resolveDisplayName(rawClaims, subject, email)
	principal.Roles = p.resolveRoles(groups, directRoles)

	return principal, nil
}

func (p *OIDCProvider) resolveDisplayName(raw map[string]any, subject, email string) string {
	if claim := strings.TrimSpace(p.cfg.UsernameClaim); claim != "" {
		if val := strings.TrimSpace(claimString(raw, claim)); val != "" {
			return val
		}
	}

	fallbacks := []string{"preferred_username", "name"}
	for _, key := range fallbacks {
		if val := strings.TrimSpace(claimString(raw, key)); val != "" {
			return val
		}
	}

	if email != "" {
		return email
	}
	return subject
}

func (p *OIDCProvider) resolveGroups(raw map[string]any) []string {
	claimKey := strings.TrimSpace(p.cfg.GroupsClaim)
	if claimKey == "" {
		claimKey = "groups"
	}
	return extractStringSlice(raw, claimKey)
}

func (p *OIDCProvider) resolveRoles(groups, directRoles []string) []string {
	lookup := make(map[string]struct{})
	defaultRole := strings.TrimSpace(p.rbac.DefaultRole)
	if defaultRole == "" {
		defaultRole = "viewer"
	}
	lookup[defaultRole] = struct{}{}

	for _, role := range directRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		lookup[role] = struct{}{}
	}

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
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		roles = append(roles, role)
	}
	return roles
}

func audAllowed(tokenAud, allowed []string) bool {
	for _, aud := range tokenAud {
		for _, okAud := range allowed {
			if strings.EqualFold(strings.TrimSpace(aud), strings.TrimSpace(okAud)) {
				return true
			}
		}
	}
	return false
}

func claimString(raw map[string]any, key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if val, ok := raw[key]; ok {
		switch typed := val.(type) {
		case string:
			return typed
		case fmt.Stringer:
			return typed.String()
		case []any:
			if len(typed) > 0 {
				return fmt.Sprint(typed[0])
			}
		default:
			return fmt.Sprint(typed)
		}
	}
	return ""
}

func extractStringSlice(raw map[string]any, key string) []string {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	val, ok := raw[key]
	if !ok {
		return nil
	}

	switch typed := val.(type) {
	case []string:
		return sanitizeStrings(typed)
	case []any:
		collected := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			collected = append(collected, fmt.Sprint(item))
		}
		return sanitizeStrings(collected)
	case string:
		parts := strings.Split(typed, ",")
		return sanitizeStrings(parts)
	default:
		return sanitizeStrings([]string{fmt.Sprint(typed)})
	}
}

func sanitizeStrings(values []string) []string {
	filtered := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[strings.ToLower(trimmed)]; exists {
			continue
		}
		seen[strings.ToLower(trimmed)] = struct{}{}
		filtered = append(filtered, trimmed)
	}
	return filtered
}
