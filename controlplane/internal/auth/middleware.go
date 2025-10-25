package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
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
}

// NewMiddleware creates an auth middleware with optional client TLS enforcement.
func NewMiddleware(log *zap.Logger, requireClientTLS bool, authCfg config.AuthConfig) *Middleware {
	return &Middleware{
		log:              log.Named("auth"),
		requireClientTLS: requireClientTLS,
		authCfg:          authCfg,
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
			if m.authCfg.OIDC.Enabled {
				provider, err := m.ensureOIDCProvider(r.Context())
				if err != nil {
					return nil, err
				}
				principal, err := provider.Verify(r.Context(), token)
				if err != nil {
					return nil, err
				}
				return principal, nil
			}
			return &Principal{
				Type:    "user",
				Name:    "bearer",
				Subject: token,
				Roles:   []string{m.defaultRole()},
			}, nil
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
