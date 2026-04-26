package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// AuthProvider names which path produced the user record. Single source of
// truth so the UI / audit log can show users where their account came from.
const (
	AuthProviderLocal = "local"
	AuthProviderLDAP  = "ldap"
	AuthProviderOIDC  = "oidc"
)

// LocalUser is the row shape for an admin-provisioned operator. Differs
// from the generic User by carrying the password fields and provider.
type LocalUser struct {
	ID                uuid.UUID
	Email             string
	DisplayName       string
	AuthProvider      string
	PasswordChangedAt sql.NullTime
	LastLoginAt       sql.NullTime
	DisabledAt        sql.NullTime
	CreatedAt         time.Time
}

// CreateLocalUserParams is the input for admin-provisioned account
// creation. Password is the cleartext; we bcrypt it before insert.
type CreateLocalUserParams struct {
	Email       string
	DisplayName string
	Password    string
	Provider    string // "local" or "ldap"
	Roles       []string
}

// CreateLocalUser inserts a new operator record. Sets bcrypt(password) when
// provider=local; leaves password_hash NULL for ldap (LDAP bind validates
// each login). Idempotent on email — returns existing row if it already
// exists.
func (s *Store) CreateLocalUser(ctx context.Context, p CreateLocalUserParams) (*LocalUser, error) {
	if s.db == nil {
		return nil, errors.New("storage not initialized")
	}
	email := strings.ToLower(strings.TrimSpace(p.Email))
	if email == "" {
		return nil, errors.New("email required")
	}
	provider := p.Provider
	if provider == "" {
		provider = AuthProviderLocal
	}

	var hashStr sql.NullString
	if provider == AuthProviderLocal {
		if p.Password == "" {
			return nil, errors.New("password required for local provider")
		}
		hashed, err := bcrypt.GenerateFromPassword([]byte(p.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		hashStr = sql.NullString{String: string(hashed), Valid: true}
	}

	// external_id is required (NOT NULL) by the existing users table; we
	// use the same email for local accounts so re-bootstraps converge.
	externalID := provider + ":" + email

	const ins = `
INSERT INTO users (id, external_id, email, display_name, password_hash, auth_provider, password_changed_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW())
ON CONFLICT (external_id) DO UPDATE SET
    email                = EXCLUDED.email,
    display_name         = COALESCE(EXCLUDED.display_name, users.display_name),
    password_hash        = COALESCE(EXCLUDED.password_hash, users.password_hash),
    auth_provider        = EXCLUDED.auth_provider,
    password_changed_at  = CASE WHEN EXCLUDED.password_hash IS NOT NULL THEN NOW() ELSE users.password_changed_at END
RETURNING id, COALESCE(email,''), COALESCE(display_name,''), auth_provider,
          password_changed_at, last_login_at, disabled_at, created_at;`
	id := uuid.New()
	var u LocalUser
	if err := s.db.QueryRowContext(ctx, ins, id, externalID, email, p.DisplayName, hashStr, provider).
		Scan(&u.ID, &u.Email, &u.DisplayName, &u.AuthProvider,
			&u.PasswordChangedAt, &u.LastLoginAt, &u.DisabledAt, &u.CreatedAt); err != nil {
		return nil, err
	}

	// Best-effort role assignment.
	if len(p.Roles) > 0 {
		if err := s.AssignRolesToUser(ctx, u.ID, p.Roles); err != nil {
			return &u, err
		}
	}
	return &u, nil
}

// SetUserPassword updates an existing user's bcrypt hash. For password
// resets / rotations.
func (s *Store) SetUserPassword(ctx context.Context, userID uuid.UUID, newPassword string) error {
	if newPassword == "" {
		return errors.New("password required")
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
UPDATE users
   SET password_hash = $1, password_changed_at = NOW()
 WHERE id = $2`, string(hashed), userID)
	return err
}

// SetUserDisabled flips disabled_at. Disabled users can't log in; existing
// sessions remain valid until they expire (revoke separately for kick).
func (s *Store) SetUserDisabled(ctx context.Context, userID uuid.UUID, disabled bool) error {
	if disabled {
		_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled_at = NOW() WHERE id = $1 AND disabled_at IS NULL`, userID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE users SET disabled_at = NULL WHERE id = $1`, userID)
	return err
}

// VerifyLocalUserPassword constant-time compares email+password against the
// stored bcrypt hash. Returns the user record on success. Does NOT update
// last_login_at — call MarkLoginSuccess after the session is issued.
//
// All error returns are deliberately the same generic message
// ("invalid credentials") so probing is hard. The caller logs the
// distinct failures internally.
func (s *Store) VerifyLocalUserPassword(ctx context.Context, email, password string) (*LocalUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	const q = `
SELECT id, COALESCE(email,''), COALESCE(display_name,''), auth_provider,
       password_hash, password_changed_at, last_login_at, disabled_at, created_at
FROM users
WHERE LOWER(email) = $1
LIMIT 1`
	var u LocalUser
	var hash sql.NullString
	err := s.db.QueryRowContext(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.AuthProvider,
		&hash, &u.PasswordChangedAt, &u.LastLoginAt, &u.DisabledAt, &u.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if u.DisabledAt.Valid {
		return nil, ErrUserDisabled
	}
	if u.AuthProvider != AuthProviderLocal {
		// Caller routes LDAP / OIDC users to their respective providers
		// before reaching this function, so hitting this branch means a
		// local-login attempt against a non-local account.
		return nil, ErrInvalidCredentials
	}
	if !hash.Valid || hash.String == "" {
		return nil, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}
	return &u, nil
}

// MarkLoginSuccess bumps last_login_at after a successful auth.
func (s *Store) MarkLoginSuccess(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = NOW() WHERE id = $1`, userID)
	return err
}

// GetLocalUserByEmail looks up an operator. Returns nil + nil when no
// row exists (not an error — caller routes by provider).
func (s *Store) GetLocalUserByEmail(ctx context.Context, email string) (*LocalUser, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	const q = `
SELECT id, COALESCE(email,''), COALESCE(display_name,''), auth_provider,
       password_changed_at, last_login_at, disabled_at, created_at
FROM users WHERE LOWER(email) = $1 LIMIT 1`
	var u LocalUser
	err := s.db.QueryRowContext(ctx, q, email).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.AuthProvider,
		&u.PasswordChangedAt, &u.LastLoginAt, &u.DisabledAt, &u.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// Session is the row stored per opaque login token. token_hash =
// sha256(token); the cleartext is returned to the client only at issue.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Token      string // populated only on Issue
	IssuedAt   time.Time
	ExpiresAt  time.Time
	LastUsedAt time.Time
	UserAgent  string
	IPAddress  string
}

// IssueSession creates a new login session. ttl defaults to 12h if zero.
// userAgent + ip are best-effort — we just store them for audit.
func (s *Store) IssueSession(ctx context.Context, userID uuid.UUID, ttl time.Duration, userAgent, ip string) (*Session, error) {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	tokenBytes := uuid.New().String() + "-" + uuid.New().String() // 72 chars
	hashed := sha256Hex(tokenBytes)
	id := uuid.New()
	now := time.Now().UTC()
	expires := now.Add(ttl)
	var ipArg interface{}
	if ip != "" {
		ipArg = ip
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO user_sessions (id, user_id, token_hash, issued_at, expires_at, last_used_at, user_agent, ip_address)
VALUES ($1, $2, $3, $4, $5, $4, $6, $7)`,
		id, userID, hashed, now, expires, userAgent, ipArg)
	if err != nil {
		return nil, err
	}
	return &Session{
		ID: id, UserID: userID, Token: tokenBytes,
		IssuedAt: now, ExpiresAt: expires, LastUsedAt: now,
		UserAgent: userAgent, IPAddress: ip,
	}, nil
}

// ValidateSessionToken hashes the supplied token and looks up the session.
// Bumps last_used_at when valid. Returns nil + nil when no row matches.
func (s *Store) ValidateSessionToken(ctx context.Context, token string) (*Session, *LocalUser, error) {
	if token == "" {
		return nil, nil, nil
	}
	hashed := sha256Hex(token)
	const q = `
SELECT s.id, s.user_id, s.issued_at, s.expires_at, s.last_used_at,
       u.id, COALESCE(u.email,''), COALESCE(u.display_name,''), u.auth_provider,
       u.password_changed_at, u.last_login_at, u.disabled_at, u.created_at
FROM user_sessions s
JOIN users u ON u.id = s.user_id
WHERE s.token_hash = $1
  AND s.revoked_at IS NULL
  AND s.expires_at > NOW()
LIMIT 1`
	var sess Session
	var u LocalUser
	err := s.db.QueryRowContext(ctx, q, hashed).Scan(
		&sess.ID, &sess.UserID, &sess.IssuedAt, &sess.ExpiresAt, &sess.LastUsedAt,
		&u.ID, &u.Email, &u.DisplayName, &u.AuthProvider,
		&u.PasswordChangedAt, &u.LastLoginAt, &u.DisabledAt, &u.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if u.DisabledAt.Valid {
		return nil, nil, ErrUserDisabled
	}
	// Bump last_used_at; ignore the error so transient write failures
	// don't deny the request.
	_, _ = s.db.ExecContext(ctx, `UPDATE user_sessions SET last_used_at = NOW() WHERE id = $1`, sess.ID)
	return &sess, &u, nil
}

// RevokeSession marks the session revoked. Idempotent.
func (s *Store) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = NOW() WHERE id = $1 AND revoked_at IS NULL`,
		sessionID)
	return err
}

// RevokeAllSessionsForUser is the kick-user button. Used post-disable +
// post-password-change.
func (s *Store) RevokeAllSessionsForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = NOW() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID)
	return err
}

// PurgeExpiredSessions deletes rows older than `expires_at + grace`. Run
// from a worker once a day.
func (s *Store) PurgeExpiredSessions(ctx context.Context, grace time.Duration) (int64, error) {
	if grace <= 0 {
		grace = 7 * 24 * time.Hour
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM user_sessions WHERE expires_at < NOW() - $1::interval`,
		grace.String())
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Sentinel errors. Caller maps both to HTTP 401 so probing yields nothing.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("account disabled")
)

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
