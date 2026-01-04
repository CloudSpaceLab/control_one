package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ContextKey represents authentication values stored in the request context.
type ContextKey string

const (
	// ContextKeyPrincipal stores the authenticated principal in request context.
	ContextKeyPrincipal ContextKey = "controlone.principal"
)

// Principal captures the caller identity.
type Principal struct {
	Type    string
	Name    string
	Subject string
	Email   string
	Roles   []string
	Groups  []string
}

// PrincipalFromContext extracts the authenticated principal from request context.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	if ctx == nil {
		return nil, false
	}
	val := ctx.Value(ContextKeyPrincipal)
	if principal, ok := val.(*Principal); ok {
		return principal, true
	}
	return nil, false
}

// Middleware performs basic authentication checks for incoming HTTP requests.
type Middleware struct {
	log              *zap.Logger
	requireClientTLS bool
	publicPaths      map[string]struct{}

	authCfg     config.AuthConfig
	oidcOnce    sync.Once
	oidcInitErr error
	oidc        *OIDCProvider

	store IdentityStore
}

// IdentityStore captures the persistence operations required by the middleware.
type IdentityStore interface {
	EnsureUser(ctx context.Context, externalID, email, displayName string) (*storage.User, error)
	AssignRolesToUser(ctx context.Context, userID uuid.UUID, roles []string) error
	ListUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error)
	GetUserByExternalID(ctx context.Context, externalID string) (*storage.User, error)
}

// NewMiddleware creates an auth middleware with optional client TLS enforcement.
func NewMiddleware(log *zap.Logger, requireClientTLS bool, authCfg config.AuthConfig, store IdentityStore) *Middleware {
	return &Middleware{
		log:              log.Named("auth"),
		requireClientTLS: requireClientTLS,
		authCfg:          authCfg,
		store:            store,
		publicPaths: map[string]struct{}{
			"/healthz": {},
			"/metrics": {},
		},
	}
}

// Wrap decorates the provided handler with authentication.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := m.publicPaths[r.URL.Path]; ok {
			next.ServeHTTP(w, r)
			return
		}

		principal, err := m.authenticate(r)
		if err != nil {
			m.log.Warn("request unauthenticated",
				zap.String("path", r.URL.Path),
				zap.Error(err),
			)
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
			return
		}

		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ContextKeyPrincipal, principal)))
	})
}

func (m *Middleware) authenticate(r *http.Request) (*Principal, error) {
	if r.TLS != nil && len(r.TLS.PeerCertificates) > 0 {
		cert := r.TLS.PeerCertificates[0]
		return &Principal{
			Type:    "agent",
			Name:    cert.Subject.CommonName,
			Subject: cert.Subject.String(),
			Roles:   []string{"agent"},
		}, nil
	}

	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		token := strings.TrimSpace(authz[7:])
		if token != "" {
			if principal, ok := m.staticPrincipal(token); ok {
				return m.persistPrincipal(r.Context(), principal), nil
			}
			if m.authCfg.OIDC.Enabled {
				provider, err := m.ensureOIDCProvider(r.Context())
				if err != nil {
					return nil, err
				}
				principal, err := provider.Verify(r.Context(), token)
				if err != nil {
					return nil, err
				}
				principal = m.persistPrincipal(r.Context(), principal)
				return principal, nil
			}
			// Reject opaque bearer tokens when OIDC is disabled and no static token matched.
			return nil, http.ErrNoCookie
		}
	}

	if m.requireClientTLS {
		return nil, http.ErrNoCookie
	}

	return nil, http.ErrNoCookie
}

func (m *Middleware) ensureOIDCProvider(ctx context.Context) (*OIDCProvider, error) {
	m.oidcOnce.Do(func() {
		if !m.authCfg.OIDC.Enabled {
			return
		}
		provider, err := NewOIDCProvider(ctx, m.authCfg.OIDC, m.authCfg.RBAC)
		if err != nil {
			m.oidcInitErr = err
			return
		}
		m.oidc = provider
	})

	if m.oidcInitErr != nil {
		return nil, m.oidcInitErr
	}
	if m.oidc == nil {
		return nil, errors.New("oidc provider not initialized")
	}
	return m.oidc, nil
}

func (m *Middleware) defaultRole() string {
	role := strings.TrimSpace(m.authCfg.RBAC.DefaultRole)
	if role == "" {
		return "viewer"
	}
	return role
}

func (m *Middleware) persistPrincipal(ctx context.Context, principal *Principal) *Principal {
	if principal == nil || m.store == nil {
		return principal
	}
	if principal.Subject == "" {
		return principal
	}

	user, err := m.store.EnsureUser(ctx, principal.Subject, principal.Email, principal.Name)
	if err != nil {
		m.log.Warn("ensure user failed", zap.Error(err))
		return principal
	}

	roles := principal.Roles
	if len(roles) == 0 {
		roles = []string{m.defaultRole()}
	}
	if err := m.store.AssignRolesToUser(ctx, user.ID, roles); err != nil {
		m.log.Warn("assign roles failed", zap.Error(err), zap.Strings("roles", roles))
		return principal
	}

	storedRoles, err := m.store.ListUserRoles(ctx, user.ID)
	if err != nil {
		m.log.Warn("list user roles failed", zap.Error(err))
		return principal
	}
	if len(storedRoles) > 0 {
		principal.Roles = storedRoles
	}
	return principal
}

func (m *Middleware) staticPrincipal(token string) (*Principal, bool) {
	if len(m.authCfg.OIDC.StaticTokens) == 0 {
		return nil, false
	}

	cfg, ok := m.authCfg.OIDC.StaticTokens[token]
	if !ok {
		return nil, false
	}

	subject := strings.TrimSpace(cfg.Subject)
	if subject == "" {
		subject = token
	}

	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = subject
	}

	roles := sanitizeStringsLocal(cfg.Roles)
	if len(roles) == 0 {
		roles = []string{m.defaultRole()}
	}

	groups := sanitizeStringsLocal(cfg.Groups)
	principal := &Principal{
		Type:    "user",
		Name:    name,
		Subject: subject,
		Email:   strings.TrimSpace(cfg.Email),
		Roles:   roles,
		Groups:  groups,
	}

	return principal, true
}

func sanitizeStringsLocal(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	var deduped []string
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, trimmed)
	}
	return deduped
}
