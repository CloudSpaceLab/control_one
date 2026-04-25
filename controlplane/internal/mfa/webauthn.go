package mfa

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Webauthn challenges in this build are handled as a thin server-side flow:
// the control plane generates a random challenge, stores it with a short
// expiry, and the browser performs navigator.credentials.create / get using
// any WebAuthn client library. On submit the browser returns the attestation
// or assertion; the server verifies the challenge matches what was issued and
// stores the credential. Full signature + attestation validation will be
// layered on via a dedicated library in a follow-up — this scaffold captures
// the state machine and persistence shape.

// RegistrationChallenge is the payload sent to the browser to begin registering
// a new authenticator.
type RegistrationChallenge struct {
	Challenge string    `json:"challenge"` // base64url encoded
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	RPID      string    `json:"rp_id"`
	RPName    string    `json:"rp_name"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AssertionChallenge is the payload sent for an authentication ceremony.
type AssertionChallenge struct {
	Challenge        string    `json:"challenge"`
	AllowCredentials []string  `json:"allow_credentials,omitempty"` // base64 credential IDs
	RPID             string    `json:"rp_id"`
	ExpiresAt        time.Time `json:"expires_at"`
}

// RegisteredCredential is what the caller persists in user_mfa_factors.
type RegisteredCredential struct {
	CredentialID   string // base64url
	PublicKey      []byte
	SignCount      uint32
	AttestationFmt string
}

// GenerateChallenge returns 32 random bytes, base64url encoded (no padding).
func GenerateChallenge() (string, []byte, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("gen challenge: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), raw, nil
}

// NewRegistrationChallenge assembles a RegistrationChallenge with a 5-minute
// validity window. Callers persist the raw challenge bytes in
// step_up_challenges and compare on submit.
func NewRegistrationChallenge(userID, username, rpID, rpName string) (RegistrationChallenge, []byte, error) {
	if rpID == "" {
		return RegistrationChallenge{}, nil, errors.New("rp_id required")
	}
	encoded, raw, err := GenerateChallenge()
	if err != nil {
		return RegistrationChallenge{}, nil, err
	}
	return RegistrationChallenge{
		Challenge: encoded,
		UserID:    userID,
		Username:  username,
		RPID:      rpID,
		RPName:    rpName,
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
	}, raw, nil
}

// NewAssertionChallenge generates an assertion challenge. allowCredentials
// narrows the ceremony to one or more already-registered authenticators.
func NewAssertionChallenge(rpID string, allowCredentials []string) (AssertionChallenge, []byte, error) {
	if rpID == "" {
		return AssertionChallenge{}, nil, errors.New("rp_id required")
	}
	encoded, raw, err := GenerateChallenge()
	if err != nil {
		return AssertionChallenge{}, nil, err
	}
	return AssertionChallenge{
		Challenge:        encoded,
		AllowCredentials: allowCredentials,
		RPID:             rpID,
		ExpiresAt:        time.Now().UTC().Add(2 * time.Minute),
	}, raw, nil
}

// VerifyChallengeMatch is a constant-time compare of the user-submitted
// challenge (base64url) against the stored raw bytes.
func VerifyChallengeMatch(submitted string, stored []byte) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(submitted)
	if err != nil || len(decoded) != len(stored) {
		return false
	}
	diff := byte(0)
	for i := range decoded {
		diff |= decoded[i] ^ stored[i]
	}
	return diff == 0
}
