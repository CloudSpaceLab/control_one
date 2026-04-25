package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MFAFactor represents a single registered authenticator for a user. The
// secret_sealed bytes are unwrapped via the sealer; this layer stores them
// raw so multiple factor types share the same column shape.
type MFAFactor struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	FactorType     string
	Label          sql.NullString
	SecretSealed   []byte
	Nonce          []byte
	WebAuthnCredID sql.NullString
	SignCount      int64
	Enabled        bool
	CreatedAt      time.Time
	LastUsedAt     sql.NullTime
}

type CreateMFAFactorParams struct {
	UserID         uuid.UUID
	FactorType     string
	Label          string
	SecretSealed   []byte
	Nonce          []byte
	WebAuthnCredID string
}

func (s *Store) CreateMFAFactor(ctx context.Context, p CreateMFAFactorParams) (*MFAFactor, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.UserID == uuid.Nil {
		return nil, errors.New("user_id required")
	}
	if p.FactorType != "totp" && p.FactorType != "webauthn" {
		return nil, errors.New("factor_type must be totp or webauthn")
	}
	if len(p.SecretSealed) == 0 {
		return nil, errors.New("secret required")
	}
	id := uuid.New()
	var labelArg, credArg any
	if strings.TrimSpace(p.Label) != "" {
		labelArg = p.Label
	}
	if strings.TrimSpace(p.WebAuthnCredID) != "" {
		credArg = p.WebAuthnCredID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO user_mfa_factors (id, user_id, factor_type, label, secret_sealed, nonce, webauthn_cred_id, sign_count, enabled, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 0, true, $8)
	`, id, p.UserID, p.FactorType, labelArg, p.SecretSealed, p.Nonce, credArg, s.clock())
	if err != nil {
		return nil, fmt.Errorf("insert mfa factor: %w", err)
	}
	return s.GetMFAFactor(ctx, id)
}

func (s *Store) GetMFAFactor(ctx context.Context, id uuid.UUID) (*MFAFactor, error) {
	row := s.db.QueryRowContext(ctx, mfaFactorSelectSQL+` WHERE id = $1`, id)
	return scanMFAFactor(row)
}

func (s *Store) ListMFAFactors(ctx context.Context, userID uuid.UUID) ([]MFAFactor, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	rows, err := s.db.QueryContext(ctx, mfaFactorSelectSQL+` WHERE user_id = $1 AND enabled = true ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []MFAFactor
	for rows.Next() {
		f, err := scanMFAFactor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (s *Store) DisableMFAFactor(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.ExecContext(ctx, `UPDATE user_mfa_factors SET enabled = false WHERE id = $1`, id)
	return err
}

// EnableMFAFactor flips enabled=true and (optionally) sets a human-readable
// label. Used at the end of TOTP / WebAuthn enrolment after the user proves
// possession of the factor.
func (s *Store) EnableMFAFactor(ctx context.Context, id uuid.UUID, label string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	var labelArg any
	if strings.TrimSpace(label) != "" {
		labelArg = strings.TrimSpace(label)
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_mfa_factors
		   SET enabled = true,
		       label   = COALESCE($2, label)
		 WHERE id = $1
	`, id, labelArg)
	return err
}

func (s *Store) RecordMFAUse(ctx context.Context, id uuid.UUID, signCount int64) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE user_mfa_factors SET last_used_at = NOW(), sign_count = $2 WHERE id = $1
	`, id, signCount)
	return err
}

const mfaFactorSelectSQL = `
	SELECT id, user_id, factor_type, label, secret_sealed, nonce, webauthn_cred_id, sign_count, enabled, created_at, last_used_at
	FROM user_mfa_factors
`

func scanMFAFactor(sc scanner) (*MFAFactor, error) {
	var f MFAFactor
	if err := sc.Scan(&f.ID, &f.UserID, &f.FactorType, &f.Label, &f.SecretSealed, &f.Nonce, &f.WebAuthnCredID, &f.SignCount, &f.Enabled, &f.CreatedAt, &f.LastUsedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

// ---------- step_up_challenges ----------

type StepUpChallenge struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Action     string
	ResourceID sql.NullString
	Challenge  []byte
	Consumed   bool
	ExpiresAt  time.Time
	CreatedAt  time.Time
	ConsumedAt sql.NullTime
}

func (s *Store) CreateStepUpChallenge(ctx context.Context, userID uuid.UUID, action, resourceID string, challenge []byte, ttl time.Duration) (*StepUpChallenge, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if userID == uuid.Nil || action == "" || len(challenge) == 0 {
		return nil, errors.New("user_id, action, challenge required")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	expires := time.Now().UTC().Add(ttl)
	id := uuid.New()
	var resArg any
	if resourceID != "" {
		resArg = resourceID
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO step_up_challenges (id, user_id, action, resource_id, challenge, consumed, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, false, $6, NOW())
	`, id, userID, action, resArg, challenge, expires)
	if err != nil {
		return nil, fmt.Errorf("insert step up challenge: %w", err)
	}
	return &StepUpChallenge{ID: id, UserID: userID, Action: action, Challenge: challenge, ExpiresAt: expires}, nil
}

// ConsumeStepUpChallenge marks the challenge consumed in one query so two
// concurrent verifications cannot both succeed.
func (s *Store) ConsumeStepUpChallenge(ctx context.Context, id uuid.UUID) (*StepUpChallenge, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	row := s.db.QueryRowContext(ctx, `
		UPDATE step_up_challenges
		SET consumed = true, consumed_at = NOW()
		WHERE id = $1 AND consumed = false AND expires_at > NOW()
		RETURNING id, user_id, action, resource_id, challenge, consumed, expires_at, created_at, consumed_at
	`, id)
	var c StepUpChallenge
	if err := row.Scan(&c.ID, &c.UserID, &c.Action, &c.ResourceID, &c.Challenge, &c.Consumed, &c.ExpiresAt, &c.CreatedAt, &c.ConsumedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}
