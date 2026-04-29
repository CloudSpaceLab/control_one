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
	// ValidateSessionToken resolves a bearer token issued by /auth/login.
	// Returns nil + nil + nil when no row matches; returns the session +
	// user + nil on a valid hit; bumps last_used_at as a side-effect.
	ValidateSessionToken(ctx context.Context, token string) (*storage.Session, *storage.LocalUser, error)
}

// NewMiddleware creates an auth middleware with optional client TLS enforcement.
func NewMiddleware(log *zap.Logger, requireClientTLS bool, authCfg config.AuthConfig, store IdentityStore) *Middleware {
	return &Middleware{
		log:              log.Named("auth"),
		requireClientTLS: requireClientTLS,
		authCfg:          authCfg,
		store:            store,
		publicPaths: map[string]struct{}{
			"/healthz":                       {},
			"/metrics":                       {},
			"/api/v1/enroll":                 {},
			"/api/v1/register":               {},
			"/api/v1/auth/login":             {},
			"/api/v1/auth/logout":            {},
			// Agent install-script is authenticated by the token query param
			// inside the handler itself; the middleware must not block it.
			"/api/v1/agent/install-script":   {},
			// Binary endpoints are token-validated inside the handler.
			"/api/v1/agent/binary":           {},
			"/api/v1/agent/binary/manifest":  {},
			"/api/v1/agent/public-key":       {},
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

	// When the controlplane runs behind nginx (TLS terminated at the edge),
	// nginx forwards the client cert subject DN via X-SSL-Client-S-DN.
	// nginx always overwrites this header from the TLS negotiation result, so
	// it cannot be forged by an external caller — an absent cert yields "".
	if dn := strings.TrimSpace(r.Header.Get("X-SSL-Client-S-DN")); dn != "" {
		if cn := extractCertCN(dn); cn != "" {
			return &Principal{
				Type:    "agent",
				Name:    cn,
				Subject: dn,
				Roles:   []string{"agent"},
			}, nil
		}
	}

	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		token := strings.TrimSpace(authz[7:])
		if token != "" {
			// 1. Try login session token first — local + LDAP users.
			if m.store != nil {
				if sess, u, err := m.store.ValidateSessionToken(r.Context(), token); err == nil && sess != nil && u != nil {
					roles, _ := m.store.ListUserRoles(r.Context(), u.ID)
					return &Principal{
						Type:    "user",
						Name:    u.DisplayName,
						Subject: u.ID.String(),
						Email:   u.Email,
						Roles:   roles,
					}, nil
				}
			}
			// 2. Static admin/operator tokens from config.
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
		// Dedupe — ListUserRoles may surface the same role twice (e.g.
		// global "admin" + tenant-scoped "admin"). The downstream RBAC
		// gate doesn't care, but the JSON response (`roles` array) shows
		// up duplicated in clients without the dedupe.
		seen := make(map[string]struct{}, len(storedRoles))
		uniq := make([]string, 0, len(storedRoles))
		for _, r := range storedRoles {
			if _, ok := seen[r]; ok {
				continue
			}
			seen[r] = struct{}{}
			uniq = append(uniq, r)
		}
		principal.Roles = uniq
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

// extractCertCN parses the CN component from an X.509 Subject DN string as
// forwarded by nginx via $ssl_client_s_dn (RFC 4514 format: "CN=val,O=org,…"
// or legacy OpenSSL slash format "/CN=val/O=org").
func extractCertCN(dn string) string {
	// RFC 4514: comma-separated, attributes in "key=value" pairs.
	for _, part := range strings.Split(dn, ",") {
		part = strings.TrimSpace(part)
		upper := strings.ToUpper(part)
		if strings.HasPrefix(upper, "CN=") {
			return strings.TrimSpace(part[3:])
		}
	}
	// OpenSSL legacy slash format: /CN=value/O=...
	upper := strings.ToUpper(dn)
	if idx := strings.Index(upper, "CN="); idx >= 0 {
		val := dn[idx+3:]
		if end := strings.IndexAny(val, "/,"); end >= 0 {
			val = val[:end]
		}
		return strings.TrimSpace(val)
	}
	return ""
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
