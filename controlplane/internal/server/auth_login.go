package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// loginRequest is the body of POST /api/v1/auth/login. We accept the same
// shape regardless of provider (local / ldap) — the server picks the path
// based on the matched user's auth_provider column or falls through to LDAP
// for unknown emails when LDAP is enabled.
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token       string    `json:"token"`
	ExpiresAt   time.Time `json:"expires_at"`
	UserID      string    `json:"user_id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name,omitempty"`
	Roles       []string  `json:"roles"`
	Permissions []string  `json:"permissions"`
}

// handleAuthLogin authenticates an operator and returns an opaque session
// token. The token is sha256-hashed before storage so a DB leak doesn't
// hand attackers live sessions.
func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var body loginRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	body.Email = strings.TrimSpace(body.Email)
	if body.Email == "" || body.Password == "" {
		http.Error(w, "email + password required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, roles, err := s.authenticateOperator(ctx, body.Email, body.Password)
	if err != nil {
		// Generic 401 — internal log carries the real reason.
		s.logger.Info("login failed",
			zap.String("email", body.Email),
			zap.String("reason", err.Error()),
			zap.String("remote_ip", clientIP(r)),
		)
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	sess, err := s.store.IssueSession(ctx, user.ID, 12*time.Hour, r.UserAgent(), clientIP(r))
	if err != nil {
		s.logger.Error("issue session", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := s.store.MarkLoginSuccess(ctx, user.ID); err != nil {
		s.logger.Warn("mark login success", zap.Error(err))
	}

	perms, err := s.store.GetUserPermissions(ctx, user.ID)
	if err != nil {
		s.logger.Warn("get user permissions", zap.Error(err))
		perms = []string{}
	}

	writeJSON(w, http.StatusOK, loginResponse{
		Token:       sess.Token,
		ExpiresAt:   sess.ExpiresAt,
		UserID:      user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Roles:       roles,
		Permissions: perms,
	})
}

// authenticateOperator dispatches to local password verify, LDAP bind, or
// errors — in that order. Local always tried first because it's cheap; LDAP
// only when local fails AND ldap is configured.
func (s *Server) authenticateOperator(ctx context.Context, email, password string) (*storage.LocalUser, []string, error) {
	// 1. Local user with bcrypt hash.
	local, err := s.store.GetLocalUserByEmail(ctx, email)
	if err != nil {
		return nil, nil, err
	}
	if local != nil && local.AuthProvider == storage.AuthProviderLocal {
		verified, err := s.store.VerifyLocalUserPassword(ctx, email, password)
		if err != nil {
			return nil, nil, err
		}
		roles, _ := s.store.ListUserRoles(ctx, verified.ID)
		return verified, roles, nil
	}

	// 2. LDAP — either an existing ldap-provisioned user, or a brand-new
	// directory user that we auto-provision on first successful bind.
	if s.ldapProvider != nil {
		ldapUser, err := s.ldapProvider.Authenticate(ctx, email, password)
		if err != nil {
			return nil, nil, err
		}
		// Provision-on-first-login: create a `ldap`-provider user record
		// the first time we see them. Subsequent logins update last_login.
		emailLower := strings.ToLower(ldapUser.Email)
		acct, err := s.store.CreateLocalUser(ctx, storage.CreateLocalUserParams{
			Email:       emailLower,
			DisplayName: ldapUser.DisplayName,
			Provider:    storage.AuthProviderLDAP,
			Roles:       ldapUser.Roles,
		})
		if err != nil {
			return nil, nil, err
		}
		// Refresh role assignment on every login so directory changes
		// take effect within one heartbeat.
		if len(ldapUser.Roles) > 0 {
			_ = s.store.SetUserRoles(ctx, acct.ID, ldapUser.Roles)
		}
		return acct, ldapUser.Roles, nil
	}

	if local != nil {
		// User exists but is in a non-local provider and LDAP is off.
		return nil, nil, errors.New("user provider has no enabled login path")
	}
	return nil, nil, storage.ErrInvalidCredentials
}

// handleAuthLogout revokes the caller's session. Idempotent — returns 200
// even when the token is already revoked or unknown.
func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	token := bearerFromHeader(r.Header.Get("Authorization"))
	if token == "" {
		w.WriteHeader(http.StatusOK)
		return
	}
	sess, _, err := s.store.ValidateSessionToken(r.Context(), token)
	if err == nil && sess != nil {
		_ = s.store.RevokeSession(r.Context(), sess.ID)
	}
	w.WriteHeader(http.StatusOK)
}

// handleAuthMe returns the current authenticated user's profile +
// permissions. UI calls this on every page load to render role-gated nav.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	resp := map[string]any{
		"subject":      principal.Subject,
		"email":        principal.Email,
		"display_name": principal.Name,
		"roles":        principal.Roles,
		"groups":       principal.Groups,
		"type":         principal.Type,
	}
	if principal.Type == "user" {
		// Pull fresh permissions from DB so admin role changes take effect
		// on the next /me call without re-login.
		if local, err := s.store.GetLocalUserByEmail(r.Context(), principal.Email); err == nil && local != nil {
			perms, _ := s.store.GetUserPermissions(r.Context(), local.ID)
			resp["permissions"] = perms
			resp["user_id"] = local.ID.String()
			resp["auth_provider"] = local.AuthProvider
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func bearerFromHeader(h string) string {
	h = strings.TrimSpace(h)
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}

func clientIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.Index(xf, ","); i >= 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
