package server

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/mfa"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// MFA endpoints expose enrolment, verification, and step-up challenge
// issuance. Routes:
//
//   GET  /api/v1/mfa/factors                     — list factors for the caller
//   POST /api/v1/mfa/totp/enroll/begin           — generate secret + qr URI
//   POST /api/v1/mfa/totp/enroll/finish          — verify code + persist factor
//   POST /api/v1/mfa/webauthn/enroll/begin       — assemble registration challenge
//   POST /api/v1/mfa/webauthn/enroll/finish      — verify + persist credential
//   POST /api/v1/mfa/step-up/begin               — issue challenge for an action
//   POST /api/v1/mfa/step-up/verify              — submit code, return token
//   DELETE /api/v1/mfa/factors/{id}              — disable factor
//
// The verify endpoint returns a short-lived "step-up token" (5 min) the UI
// passes back via the X-Step-Up-Token header on the protected mutation. The
// authorize() helper enforces the token presence on routes added to the
// stepUpRequired set.

type mfaFactorResponse struct {
	ID         string  `json:"id"`
	FactorType string  `json:"factor_type"`
	Label      *string `json:"label,omitempty"`
	Enabled    bool    `json:"enabled"`
	CreatedAt  string  `json:"created_at"`
	LastUsedAt *string `json:"last_used_at,omitempty"`
}

func newMFAFactorResponse(f storage.MFAFactor) mfaFactorResponse {
	out := mfaFactorResponse{
		ID:         f.ID.String(),
		FactorType: f.FactorType,
		Enabled:    f.Enabled,
		CreatedAt:  formatTime(f.CreatedAt),
	}
	if f.Label.Valid {
		s := f.Label.String
		out.Label = &s
	}
	if f.LastUsedAt.Valid {
		s := formatTime(f.LastUsedAt.Time)
		out.LastUsedAt = &s
	}
	return out
}

func (s *Server) handleMFAFactors(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	switch r.Method {
	case http.MethodGet:
		factors, err := s.store.ListMFAFactors(r.Context(), userID)
		if err != nil {
			s.logger.Error("list mfa factors", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		out := make([]mfaFactorResponse, 0, len(factors))
		for _, f := range factors {
			out = append(out, newMFAFactorResponse(f))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out})
	default:
		w.Header().Set("Allow", "GET")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMFAFactorSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	id, err := uuid.Parse(strings.TrimPrefix(r.URL.Path, "/api/v1/mfa/factors/"))
	if err != nil {
		http.Error(w, "invalid factor id", http.StatusBadRequest)
		return
	}
	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	factor, err := s.store.GetMFAFactor(r.Context(), id)
	if err != nil || factor == nil {
		http.NotFound(w, r)
		return
	}
	uid := s.userIDForPrincipalCtx(r.Context(), principal)
	if factor.UserID != uid {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	if err := s.store.DisableMFAFactor(r.Context(), id); err != nil {
		s.logger.Error("disable mfa", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- TOTP enrolment ----------------------------------------------------

type totpBeginResponse struct {
	FactorID         string `json:"factor_id"`
	Secret           string `json:"secret"`
	ProvisioningURI  string `json:"provisioning_uri"`
}

type totpFinishRequest struct {
	FactorID string `json:"factor_id"`
	Code     string `json:"code"`
	Label    string `json:"label"`
}

func (s *Server) handleTOTPEnrollBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.sealer == nil {
		http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	secret, err := mfa.GenerateSecret()
	if err != nil {
		http.Error(w, fmt.Sprintf("gen secret: %v", err), http.StatusInternalServerError)
		return
	}
	sealed, nonce, err := s.sealer.Seal(secret)
	if err != nil {
		http.Error(w, "seal failed", http.StatusInternalServerError)
		return
	}
	label := s.cfg.HTTP.Address
	if label == "" {
		label = "ControlOne"
	}
	uri := mfa.ProvisioningURI("ControlOne", principal.Email, secret)

	factor, err := s.store.CreateMFAFactor(r.Context(), storage.CreateMFAFactorParams{
		UserID:       userID,
		FactorType:   "totp",
		SecretSealed: sealed,
		Nonce:        nonce,
		Label:        "pending",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("persist factor: %v", err), http.StatusInternalServerError)
		return
	}
	// Disable until verified — finish endpoint flips enabled when the user
	// proves possession with a valid code.
	_ = s.store.DisableMFAFactor(r.Context(), factor.ID)

	writeJSON(w, http.StatusOK, totpBeginResponse{
		FactorID:        factor.ID.String(),
		Secret:          mfa.SecretToBase32(secret),
		ProvisioningURI: uri,
	})
}

func (s *Server) handleTOTPEnrollFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.sealer == nil {
		http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
		return
	}
	var req totpFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	id, err := uuid.Parse(req.FactorID)
	if err != nil {
		http.Error(w, "invalid factor_id", http.StatusBadRequest)
		return
	}
	factor, err := s.store.GetMFAFactor(r.Context(), id)
	if err != nil || factor == nil {
		http.NotFound(w, r)
		return
	}
	uid := s.userIDForPrincipalCtx(r.Context(), principal)
	if factor.UserID != uid {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	secret, err := s.sealer.Open(factor.SecretSealed, factor.Nonce)
	if err != nil {
		http.Error(w, "unseal failed", http.StatusInternalServerError)
		return
	}
	if !mfa.Verify(secret, req.Code, time.Now()) {
		http.Error(w, "invalid totp code", http.StatusUnauthorized)
		return
	}
	// Activate the factor with the user-supplied label (or a sensible default).
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "Authenticator app"
	}
	if err := s.store.EnableMFAFactor(r.Context(), id, label); err != nil {
		s.logger.Error("enable mfa factor", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if err := s.store.RecordMFAUse(r.Context(), id, factor.SignCount+1); err != nil {
		s.logger.Warn("record mfa use", zap.Error(err))
	}
	s.recordAudit(r.Context(), principal, uuid.Nil, "mfa.totp.enroll", "mfa_factor", factor.ID.String(), nil)
	writeJSON(w, http.StatusOK, map[string]any{"factor_id": factor.ID.String(), "verified": true})
}

// --- step-up challenge -------------------------------------------------

type stepUpBeginRequest struct {
	Action     string `json:"action"`
	ResourceID string `json:"resource_id"`
}

type stepUpBeginResponse struct {
	ChallengeID string `json:"challenge_id"`
	Action      string `json:"action"`
	ExpiresAt   string `json:"expires_at"`
	FactorTypes []string `json:"factor_types"`
}

type stepUpVerifyRequest struct {
	ChallengeID string `json:"challenge_id"`
	FactorID    string `json:"factor_id"`
	Code        string `json:"code"`
}

type stepUpVerifyResponse struct {
	StepUpToken string `json:"step_up_token"`
	ExpiresAt   string `json:"expires_at"`
}

func (s *Server) handleStepUpBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	var req stepUpBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Action) == "" {
		http.Error(w, "action required", http.StatusBadRequest)
		return
	}
	factors, err := s.store.ListMFAFactors(r.Context(), userID)
	if err != nil || len(factors) == 0 {
		http.Error(w, "no mfa factors enrolled", http.StatusForbidden)
		return
	}
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		http.Error(w, "challenge gen", http.StatusInternalServerError)
		return
	}
	stored, err := s.store.CreateStepUpChallenge(r.Context(), userID, req.Action, req.ResourceID, challenge, 5*time.Minute)
	if err != nil {
		http.Error(w, fmt.Sprintf("persist challenge: %v", err), http.StatusInternalServerError)
		return
	}
	types := make([]string, 0, len(factors))
	seen := map[string]bool{}
	for _, f := range factors {
		if !seen[f.FactorType] {
			seen[f.FactorType] = true
			types = append(types, f.FactorType)
		}
	}
	writeJSON(w, http.StatusOK, stepUpBeginResponse{
		ChallengeID: stored.ID.String(),
		Action:      req.Action,
		ExpiresAt:   formatTime(stored.ExpiresAt),
		FactorTypes: types,
	})
}

func (s *Server) handleStepUpVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	if s.sealer == nil {
		http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	var req stepUpVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	chID, err := uuid.Parse(req.ChallengeID)
	if err != nil {
		http.Error(w, "invalid challenge_id", http.StatusBadRequest)
		return
	}
	factorID, err := uuid.Parse(req.FactorID)
	if err != nil {
		http.Error(w, "invalid factor_id", http.StatusBadRequest)
		return
	}
	challenge, err := s.store.ConsumeStepUpChallenge(r.Context(), chID)
	if err != nil {
		http.Error(w, fmt.Sprintf("consume challenge: %v", err), http.StatusInternalServerError)
		return
	}
	if challenge == nil || challenge.UserID != userID {
		http.Error(w, "challenge not found or expired", http.StatusForbidden)
		return
	}
	factor, err := s.store.GetMFAFactor(r.Context(), factorID)
	if err != nil || factor == nil || factor.UserID != userID {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}

	switch factor.FactorType {
	case "totp":
		secret, err := s.sealer.Open(factor.SecretSealed, factor.Nonce)
		if err != nil {
			http.Error(w, "unseal failed", http.StatusInternalServerError)
			return
		}
		if !mfa.Verify(secret, req.Code, time.Now()) {
			http.Error(w, "invalid code", http.StatusUnauthorized)
			return
		}
	case "webauthn":
		if s.webauthn == nil {
			http.Error(w, "webauthn not configured", http.StatusServiceUnavailable)
			return
		}
		// The challenge.Challenge column holds a JSON-encoded webauthn
		// SessionData blob (see handleWebAuthnStepUpBegin). Reconstruct it
		// and validate the assertion the browser submitted in req.Code (as
		// the assertion JSON, base64url encoded so it survives this DTO).
		raw, err := base64.RawURLEncoding.DecodeString(req.Code)
		if err != nil {
			http.Error(w, "assertion must be base64url(json)", http.StatusBadRequest)
			return
		}
		var session webauthnlib.SessionData
		if err := json.Unmarshal(challenge.Challenge, &session); err != nil {
			http.Error(w, "stored session corrupt", http.StatusInternalServerError)
			return
		}
		creds, lerr := s.loadWebAuthnCredentials(r.Context(), userID)
		if lerr != nil {
			s.logger.Error("load webauthn creds", zap.Error(lerr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		user := mfa.WebAuthnUser{
			HandleBytes: userID[:],
			Username:    principal.Email,
			DisplayName: principal.Name,
			Credentials: creds,
		}
		parsed, perr := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(raw)))
		if perr != nil {
			http.Error(w, fmt.Sprintf("parse assertion: %v", perr), http.StatusBadRequest)
			return
		}
		updated, verr := s.webauthn.ValidateLogin(user, session, parsed)
		if verr != nil {
			http.Error(w, fmt.Sprintf("verify assertion: %v", verr), http.StatusUnauthorized)
			return
		}
		// Persist the bumped sign count to detect cloned authenticators.
		if updated != nil {
			_ = s.store.RecordMFAUse(r.Context(), factor.ID, int64(updated.Authenticator.SignCount))
		}
	default:
		http.Error(w, "unknown factor type", http.StatusBadRequest)
		return
	}

	_ = s.store.RecordMFAUse(r.Context(), factor.ID, factor.SignCount+1)
	token, expiresAt, err := s.issueStepUpToken(userID, challenge.Action)
	if err != nil {
		http.Error(w, fmt.Sprintf("issue token: %v", err), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, uuid.Nil, "auth.step_up.verify", "step_up", challenge.ID.String(), map[string]any{
		"action":   challenge.Action,
		"factor":   factor.FactorType,
	})
	writeJSON(w, http.StatusOK, stepUpVerifyResponse{StepUpToken: token, ExpiresAt: formatTime(expiresAt)})
}

// issueStepUpToken returns an opaque base64 token tying (user, action) to an
// expiry. It is HMAC-signed by the secrets sealer so verification needs no
// extra DB lookup; callers verify via verifyStepUpToken.
func (s *Server) issueStepUpToken(userID uuid.UUID, action string) (string, time.Time, error) {
	expires := time.Now().UTC().Add(5 * time.Minute)
	payload := fmt.Sprintf("%s|%s|%d", userID.String(), action, expires.Unix())
	sealed, nonce, err := s.sealer.Seal([]byte(payload))
	if err != nil {
		return "", expires, err
	}
	combined := append(nonce, sealed...)
	return base64.RawURLEncoding.EncodeToString(combined), expires, nil
}

// verifyStepUpToken decodes a step-up token and returns (userID, action) if
// valid + unexpired. Used by middleware on sensitive routes.
func (s *Server) verifyStepUpToken(token string) (uuid.UUID, string, bool) {
	if s.sealer == nil {
		return uuid.Nil, "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) < 12 {
		return uuid.Nil, "", false
	}
	nonce := raw[:12]
	sealed := raw[12:]
	payload, err := s.sealer.Open(sealed, nonce)
	if err != nil {
		return uuid.Nil, "", false
	}
	parts := strings.SplitN(string(payload), "|", 3)
	if len(parts) != 3 {
		return uuid.Nil, "", false
	}
	uid, err := uuid.Parse(parts[0])
	if err != nil {
		return uuid.Nil, "", false
	}
	expSec := 0
	for _, c := range parts[2] {
		if c < '0' || c > '9' {
			return uuid.Nil, "", false
		}
		expSec = expSec*10 + int(c-'0')
	}
	if time.Now().UTC().Unix() > int64(expSec) {
		return uuid.Nil, "", false
	}
	return uid, parts[1], true
}

// loadWebAuthnCredentials returns every enabled WebAuthn credential for a
// user in the shape go-webauthn expects. The credential blob lives in
// secret_sealed; we unwrap with the sealer + decode JSON.
func (s *Server) loadWebAuthnCredentials(ctx context.Context, userID uuid.UUID) ([]webauthnlib.Credential, error) {
	factors, err := s.store.ListMFAFactors(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]webauthnlib.Credential, 0, len(factors))
	for _, f := range factors {
		if f.FactorType != "webauthn" || !f.Enabled {
			continue
		}
		plain, err := s.sealer.Open(f.SecretSealed, f.Nonce)
		if err != nil {
			s.logger.Warn("webauthn unseal cred", zap.Error(err))
			continue
		}
		var cred webauthnlib.Credential
		if err := json.Unmarshal(plain, &cred); err != nil {
			s.logger.Warn("webauthn decode cred", zap.Error(err))
			continue
		}
		out = append(out, cred)
	}
	return out, nil
}

// --- WebAuthn registration ceremony ---

func (s *Server) handleWebAuthnEnrollBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.webauthn == nil {
		http.Error(w, "webauthn not configured", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	creds, _ := s.loadWebAuthnCredentials(r.Context(), userID)
	user := mfa.WebAuthnUser{
		HandleBytes: userID[:],
		Username:    principal.Email,
		DisplayName: principal.Name,
		Credentials: creds,
	}
	options, sessionData, err := s.webauthn.BeginRegistration(user)
	if err != nil {
		http.Error(w, fmt.Sprintf("begin registration: %v", err), http.StatusInternalServerError)
		return
	}
	sd, err := json.Marshal(sessionData)
	if err != nil {
		http.Error(w, "encode session", http.StatusInternalServerError)
		return
	}
	stored, err := s.store.CreateStepUpChallenge(r.Context(), userID, "webauthn.register", "", sd, 5*time.Minute)
	if err != nil {
		http.Error(w, fmt.Sprintf("persist challenge: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": stored.ID.String(),
		"options":      options,
	})
}

type webauthnFinishRequest struct {
	ChallengeID string          `json:"challenge_id"`
	Label       string          `json:"label"`
	Attestation json.RawMessage `json:"attestation"`
}

func (s *Server) handleWebAuthnEnrollFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.webauthn == nil {
		http.Error(w, "webauthn not configured", http.StatusServiceUnavailable)
		return
	}
	if s.sealer == nil {
		http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	var req webauthnFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	chID, err := uuid.Parse(req.ChallengeID)
	if err != nil {
		http.Error(w, "invalid challenge_id", http.StatusBadRequest)
		return
	}
	challenge, err := s.store.ConsumeStepUpChallenge(r.Context(), chID)
	if err != nil || challenge == nil || challenge.UserID != userID || challenge.Action != "webauthn.register" {
		http.Error(w, "challenge not found or expired", http.StatusForbidden)
		return
	}
	var session webauthnlib.SessionData
	if err := json.Unmarshal(challenge.Challenge, &session); err != nil {
		http.Error(w, "stored session corrupt", http.StatusInternalServerError)
		return
	}
	creds, _ := s.loadWebAuthnCredentials(r.Context(), userID)
	user := mfa.WebAuthnUser{
		HandleBytes: userID[:],
		Username:    principal.Email,
		DisplayName: principal.Name,
		Credentials: creds,
	}
	parsed, perr := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(req.Attestation)))
	if perr != nil {
		http.Error(w, fmt.Sprintf("parse attestation: %v", perr), http.StatusBadRequest)
		return
	}
	cred, verr := s.webauthn.CreateCredential(user, session, parsed)
	if verr != nil {
		http.Error(w, fmt.Sprintf("verify attestation: %v", verr), http.StatusBadRequest)
		return
	}
	credBytes, err := json.Marshal(cred)
	if err != nil {
		http.Error(w, "encode credential", http.StatusInternalServerError)
		return
	}
	sealed, nonce, err := s.sealer.Seal(credBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf("seal credential: %v", err), http.StatusInternalServerError)
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = "Security key"
	}
	factor, err := s.store.CreateMFAFactor(r.Context(), storage.CreateMFAFactorParams{
		UserID:         userID,
		FactorType:     "webauthn",
		Label:          label,
		SecretSealed:   sealed,
		Nonce:          nonce,
		WebAuthnCredID: base64.RawURLEncoding.EncodeToString(cred.ID),
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("persist factor: %v", err), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, uuid.Nil, "mfa.webauthn.enroll", "mfa_factor", factor.ID.String(), nil)
	writeJSON(w, http.StatusOK, map[string]any{"factor_id": factor.ID.String(), "verified": true})
}

// --- WebAuthn step-up assertion ceremony ---

// handleWebAuthnStepUpBegin issues the assertion challenge and stores the
// SessionData blob the verifier needs. Returns the options the browser feeds
// to navigator.credentials.get(...).
func (s *Server) handleWebAuthnStepUpBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.webauthn == nil {
		http.Error(w, "webauthn not configured", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID == uuid.Nil {
		http.Error(w, "user not registered", http.StatusForbidden)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	var req struct {
		Action     string `json:"action"`
		ResourceID string `json:"resource_id"`
	}
	if len(body) > 0 {
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
	}
	if strings.TrimSpace(req.Action) == "" {
		http.Error(w, "action required", http.StatusBadRequest)
		return
	}
	creds, err := s.loadWebAuthnCredentials(r.Context(), userID)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if len(creds) == 0 {
		http.Error(w, "no webauthn factors enrolled", http.StatusForbidden)
		return
	}
	user := mfa.WebAuthnUser{
		HandleBytes: userID[:],
		Username:    principal.Email,
		DisplayName: principal.Name,
		Credentials: creds,
	}
	options, sessionData, err := s.webauthn.BeginLogin(user)
	if err != nil {
		http.Error(w, fmt.Sprintf("begin login: %v", err), http.StatusInternalServerError)
		return
	}
	sd, err := json.Marshal(sessionData)
	if err != nil {
		http.Error(w, "encode session", http.StatusInternalServerError)
		return
	}
	stored, err := s.store.CreateStepUpChallenge(r.Context(), userID, req.Action, req.ResourceID, sd, 2*time.Minute)
	if err != nil {
		http.Error(w, fmt.Sprintf("persist challenge: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": stored.ID.String(),
		"action":       req.Action,
		"expires_at":   formatTime(stored.ExpiresAt),
		"options":      options,
	})
}

// requireStepUp returns true when the request has a valid X-Step-Up-Token
// header for the given action. Mutating handlers call this before performing
// sensitive operations (delete tenant, approve high-risk request, rotate CA).
func (s *Server) requireStepUp(w http.ResponseWriter, r *http.Request, principal *auth.Principal, action string) bool {
	token := strings.TrimSpace(r.Header.Get("X-Step-Up-Token"))
	if token == "" {
		http.Error(w, "step-up required", http.StatusPreconditionRequired)
		return false
	}
	uid, tokenAction, ok := s.verifyStepUpToken(token)
	if !ok {
		http.Error(w, "step-up invalid", http.StatusUnauthorized)
		return false
	}
	if tokenAction != action {
		http.Error(w, "step-up scope mismatch", http.StatusForbidden)
		return false
	}
	expected := s.userIDForPrincipalCtx(r.Context(), principal)
	if expected != uuid.Nil && uid != expected {
		http.Error(w, "step-up user mismatch", http.StatusForbidden)
		return false
	}
	return true
}
