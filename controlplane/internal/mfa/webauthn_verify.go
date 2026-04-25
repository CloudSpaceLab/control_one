package mfa

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// WebAuthnConfig packages the relying-party configuration the verifier needs.
// RPID is the bare hostname (no scheme), RPOrigin is the full origin string
// the browser sends in clientDataJSON.
type WebAuthnConfig struct {
	RPID     string
	RPName   string
	RPOrigin string
}

// NewWebAuthn returns a configured *webauthn.WebAuthn handle. Call once at
// startup and reuse — it is safe for concurrent use.
func NewWebAuthn(cfg WebAuthnConfig) (*webauthn.WebAuthn, error) {
	if strings.TrimSpace(cfg.RPID) == "" || strings.TrimSpace(cfg.RPOrigin) == "" {
		return nil, errors.New("rp_id and rp_origin required")
	}
	if cfg.RPName == "" {
		cfg.RPName = "Control One"
	}
	return webauthn.New(&webauthn.Config{
		RPID:          cfg.RPID,
		RPDisplayName: cfg.RPName,
		RPOrigins:     []string{cfg.RPOrigin},
	})
}

// WebAuthnUser is the minimal user identity the protocol needs. We populate
// it from a server.User row (id + email + display name + registered creds).
type WebAuthnUser struct {
	HandleBytes []byte
	Username    string
	DisplayName string
	Credentials []webauthn.Credential
}

func (u WebAuthnUser) WebAuthnID() []byte                         { return u.HandleBytes }
func (u WebAuthnUser) WebAuthnName() string                       { return u.Username }
func (u WebAuthnUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u WebAuthnUser) WebAuthnIcon() string                       { return "" }
func (u WebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

// VerifyAttestation validates a registration response against the challenge
// the server issued. Returns the new credential the caller should persist.
func VerifyAttestation(w *webauthn.WebAuthn, user WebAuthnUser, sessionData webauthn.SessionData, attestationJSON []byte) (*webauthn.Credential, error) {
	if w == nil {
		return nil, errors.New("webauthn handle nil")
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(attestationJSON)))
	if err != nil {
		return nil, fmt.Errorf("parse attestation: %w", err)
	}
	cred, err := w.CreateCredential(user, sessionData, parsed)
	if err != nil {
		return nil, fmt.Errorf("verify attestation: %w", err)
	}
	return cred, nil
}

// VerifyAssertion validates a login assertion. Returns the updated credential
// (with bumped sign count) so the caller persists the new sign count to
// detect cloned authenticators.
func VerifyAssertion(w *webauthn.WebAuthn, user WebAuthnUser, sessionData webauthn.SessionData, assertionJSON []byte) (*webauthn.Credential, error) {
	if w == nil {
		return nil, errors.New("webauthn handle nil")
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(assertionJSON)))
	if err != nil {
		return nil, fmt.Errorf("parse assertion: %w", err)
	}
	cred, err := w.ValidateLogin(user, sessionData, parsed)
	if err != nil {
		return nil, fmt.Errorf("verify assertion: %w", err)
	}
	return cred, nil
}

// EncodeCredential serializes a webauthn.Credential to bytes the storage layer
// can persist (via secrets sealer if desired). Symmetric with DecodeCredential.
func EncodeCredential(cred *webauthn.Credential) ([]byte, error) {
	if cred == nil {
		return nil, errors.New("nil credential")
	}
	// Use the protocol-stable JSON form. base64 the binary blobs.
	return protocol.URLEncodedBase64(cred.PublicKey).MarshalJSON()
}

// DecodeBase64 returns raw bytes for a base64url string (challenges, raw IDs).
func DecodeBase64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}
