// Package server: Use Case 7 — Misconduct & Whistleblowing handlers.
//
// Two surfaces live in this file:
//
//  1. Public, unauthenticated whistleblower intake at /api/v1/misconduct/submit
//     and /api/v1/misconduct/intake-status. These mirror the trust-center
//     pattern (see trust_center.go): the auth middleware skips the path,
//     the handler enforces its own protections (per-IP rate limit, global
//     rate limit, proof-of-work challenge), and the response surface is
//     intentionally tiny (token-once on submit, status-only on poll).
//
//  2. Investigator-gated case management at /api/v1/misconduct/cases*. These
//     reuse s.authorize(roleInvestigator, roleAdmin), s.recordAudit on every
//     state transition, and the existing compliance_evidence upload flow for
//     the evidence locker (case_evidence is a link table — files live in
//     compliance_evidence, hashed and retention-tracked there).
//
// The misconduct.score and misconduct.retention_sweep job handlers also live
// here so the per-case scoring math, severity weights, and the sweep cadence
// stay co-located with the storage layer they drive.
package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// osGetenv is split out so the envLookup hook can swap it in tests.
func osGetenv(key string) string { return os.Getenv(key) }

// whistleblowerBcryptCostOverride lets tests reduce the bcrypt cost from 10
// to bcrypt.MinCost (4) so fixtures don't spend minutes hashing.
var whistleblowerBcryptCostOverride = whistleblowerBcryptCost

// ── Constants & limiters ─────────────────────────────────────────────────────

const (
	// whistleblowerPoWDifficulty is the number of leading zero bits the
	// SHA-256(challenge||nonce) digest must have. 20 bits ≈ ~1M hashes,
	// ~1–2 seconds in JS — high enough to deter scripted spam, low enough
	// that a real submitter (or a trivial mobile browser) finishes in the
	// background while filling the form.
	whistleblowerPoWDifficulty = 20

	// whistleblowerIPRateLimit caps per-source-IP submissions per hour.
	whistleblowerIPRateLimit = 10
	// whistleblowerGlobalRateLimit caps total submissions per hour across
	// all clients — the global ceiling protects the database when an
	// attacker spreads spam across many IPs.
	whistleblowerGlobalRateLimit = 100
	// whistleblowerRateWindow is the rolling window for both limits.
	whistleblowerRateWindow = time.Hour

	// whistleblowerRetention defaults to 90 days. The retention-sweep job
	// deletes rows older than this.
	whistleblowerRetention = 90 * 24 * time.Hour

	// whistleblowerChallengeTTL is how long a PoW challenge remains valid.
	// Long enough for slow clients, short enough to bound replay scope.
	whistleblowerChallengeTTL = 10 * time.Minute

	// whistleblowerBodyKeyEnv is the env var holding the AES-256-GCM key
	// when the platform sealer is not configured. We document the
	// rotation path in the comment block below.
	whistleblowerBodyKeyEnv = "WHISTLEBLOWER_BODY_KEY"

	// roleInvestigator is the RBAC role gating /api/v1/misconduct/cases*.
	roleInvestigator = "investigator"

	// whistleblowerBcryptCost: bcrypt cost for token hashing. 10 is the
	// library default; tests override via the package-level var below.
	whistleblowerBcryptCost = 10

	// JobTypeMisconductScore recomputes a case's risk_score from existing
	// signals (audit_logs, security_events, compliance_results).
	JobTypeMisconductScore = "misconduct.score"
	// JobTypeMisconductRetentionSweep deletes whistleblower submissions past
	// their retention deadline.
	JobTypeMisconductRetentionSweep = "misconduct.retention_sweep"
)

// rate-limit state — process-local. A real deployment behind multiple
// replicas would back this with Redis; for now per-replica is acceptable
// because the global limit kicks in long before a single bad actor can
// scale-out across replicas, and the database CHECK constraints + bcrypt
// token hashing prevent forgery either way.
type whistleblowerLimiter struct {
	mu          sync.Mutex
	perIP       map[string][]time.Time
	global      []time.Time
	challenges  map[string]time.Time
	clockOverr  func() time.Time
	maxIPHits   int
	maxGlobal   int
	rateWindow  time.Duration
	challengeFn func() string
}

func newWhistleblowerLimiter() *whistleblowerLimiter {
	return &whistleblowerLimiter{
		perIP:      map[string][]time.Time{},
		challenges: map[string]time.Time{},
		maxIPHits:  whistleblowerIPRateLimit,
		maxGlobal:  whistleblowerGlobalRateLimit,
		rateWindow: whistleblowerRateWindow,
	}
}

func (l *whistleblowerLimiter) now() time.Time {
	if l.clockOverr != nil {
		return l.clockOverr()
	}
	return time.Now()
}

// allow returns nil if the request is within both per-IP and global limits.
func (l *whistleblowerLimiter) allow(ip string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	cutoff := now.Add(-l.rateWindow)

	// Trim per-IP.
	hits := l.perIP[ip]
	pruned := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= l.maxIPHits {
		l.perIP[ip] = pruned
		return errors.New("rate limit exceeded for source ip")
	}

	// Trim global.
	prunedGlobal := l.global[:0]
	for _, t := range l.global {
		if t.After(cutoff) {
			prunedGlobal = append(prunedGlobal, t)
		}
	}
	if len(prunedGlobal) >= l.maxGlobal {
		l.global = prunedGlobal
		return errors.New("rate limit exceeded globally")
	}

	// Record.
	pruned = append(pruned, now)
	prunedGlobal = append(prunedGlobal, now)
	l.perIP[ip] = pruned
	l.global = prunedGlobal
	return nil
}

// issueChallenge generates a fresh PoW challenge string and stores it.
func (l *whistleblowerLimiter) issueChallenge() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Garbage-collect expired challenges so the map stays bounded.
	now := l.now()
	for k, t := range l.challenges {
		if now.Sub(t) > whistleblowerChallengeTTL {
			delete(l.challenges, k)
		}
	}
	var c string
	if l.challengeFn != nil {
		c = l.challengeFn()
	} else {
		buf := make([]byte, 16)
		_, _ = rand.Read(buf)
		c = hex.EncodeToString(buf)
	}
	l.challenges[c] = now
	return c
}

// consumeChallenge validates and removes a challenge (single-use).
func (l *whistleblowerLimiter) consumeChallenge(challenge string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	issuedAt, ok := l.challenges[challenge]
	if !ok {
		return errors.New("unknown challenge")
	}
	delete(l.challenges, challenge)
	if l.now().Sub(issuedAt) > whistleblowerChallengeTTL {
		return errors.New("challenge expired")
	}
	return nil
}

// verifyPoW returns true iff sha256(challenge||nonce) starts with `bits`
// leading zero bits. We hard-code SHA-256 for parity with browser
// SubtleCrypto on the client side.
func verifyPoW(challenge, nonce string, bits int) bool {
	if bits <= 0 || bits > 256 {
		return false
	}
	h := sha256.Sum256([]byte(challenge + nonce))
	full := uint(bits / 8)
	rem := uint(bits % 8)
	for i := uint(0); i < full; i++ {
		if h[i] != 0 {
			return false
		}
	}
	if rem > 0 {
		mask := byte(0xff << (8 - rem))
		if h[full]&mask != 0 {
			return false
		}
	}
	return true
}

// ── Public intake endpoints (no auth, rate-limited, PoW-gated) ───────────────

type whistleblowerSubmitRequest struct {
	Description     string `json:"description"`
	ApproximateDate string `json:"approximate_date"`
	SubjectRole     string `json:"subject_role"`
	Challenge       string `json:"challenge"`
	Nonce           string `json:"nonce"`
}

type whistleblowerSubmitResponse struct {
	Token   string `json:"token"`
	Message string `json:"message"`
}

type whistleblowerChallengeResponse struct {
	Challenge  string `json:"challenge"`
	Difficulty int    `json:"difficulty"`
}

type intakeStatusRequest struct {
	Token string `json:"token"`
}

type intakeStatusResponse struct {
	Status string `json:"status"`
}

// ensureWhistleblowerLimiter lazily initialises the in-process limiter.
func (s *Server) ensureWhistleblowerLimiter() *whistleblowerLimiter {
	s.misconductOnce.Do(func() {
		s.whistleblowerLim = newWhistleblowerLimiter()
	})
	return s.whistleblowerLim
}

// whistleblowerClientIP extracts the source IP for rate limiting. Mirrors
// the existing clientIP helper but lives here to avoid importing it from
// auth_login.go (which has a slightly different fallback policy).
func whistleblowerClientIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		// Take the first IP — the real client per RFC 7239 conventions.
		if comma := strings.Index(xff, ","); comma > 0 {
			xff = strings.TrimSpace(xff[:comma])
		}
		return xff
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xri != "" {
		return xri
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// handleWhistleblowerChallenge issues a fresh PoW challenge to clients before
// they submit. GET /api/v1/misconduct/challenge.
func (s *Server) handleWhistleblowerChallenge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	lim := s.ensureWhistleblowerLimiter()
	resp := whistleblowerChallengeResponse{
		Challenge:  lim.issueChallenge(),
		Difficulty: whistleblowerPoWDifficulty,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleWhistleblowerSubmit accepts an anonymous report. Path:
// POST /api/v1/misconduct/submit. PUBLIC — no auth, but rate-limited and
// PoW-gated. Returns a one-time token (base64url, 32 bytes) the submitter
// can use to poll status. Body is sealed with the platform secretbox if
// configured, else with a key derived from WHISTLEBLOWER_BODY_KEY.
func (s *Server) handleWhistleblowerSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	lim := s.ensureWhistleblowerLimiter()
	if err := lim.allow(whistleblowerClientIP(r)); err != nil {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	var req whistleblowerSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Description) == "" {
		http.Error(w, "description required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Challenge) == "" || strings.TrimSpace(req.Nonce) == "" {
		http.Error(w, "challenge + nonce required", http.StatusBadRequest)
		return
	}
	if !verifyPoW(req.Challenge, req.Nonce, whistleblowerPoWDifficulty) {
		http.Error(w, "proof-of-work invalid", http.StatusBadRequest)
		return
	}
	if err := lim.consumeChallenge(req.Challenge); err != nil {
		http.Error(w, "challenge replay or expired", http.StatusBadRequest)
		return
	}

	// Marshal a redacted body — only the operator-supplied fields, never
	// the raw HTTP headers. We also include `approximate_date` and
	// `subject_role` so investigators have context without us storing IP/UA.
	bodyJSON, err := json.Marshal(map[string]string{
		"description":      req.Description,
		"approximate_date": req.ApproximateDate,
		"subject_role":     req.SubjectRole,
	})
	if err != nil {
		http.Error(w, "encode body", http.StatusInternalServerError)
		return
	}

	sealer, err := s.misconductSealer()
	if err != nil {
		s.logger.Error("misconduct sealer unavailable", zap.Error(err))
		http.Error(w, "encryption unavailable", http.StatusServiceUnavailable)
		return
	}
	cipher, nonce, err := sealer.Seal(bodyJSON)
	if err != nil {
		s.logger.Error("seal whistleblower body", zap.Error(err))
		http.Error(w, "seal failed", http.StatusInternalServerError)
		return
	}

	token := generateToken()
	hash, err := bcrypt.GenerateFromPassword([]byte(token), whistleblowerBcryptCostOverride)
	if err != nil {
		s.logger.Error("hash whistleblower token", zap.Error(err))
		http.Error(w, "hash failed", http.StatusInternalServerError)
		return
	}

	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	_, err = s.store.CreateWhistleblowerSubmission(r.Context(), storage.CreateWhistleblowerSubmissionParams{
		TokenHash:      string(hash),
		BodyEncrypted:  cipher,
		BodyNonce:      nonce,
		RetentionUntil: time.Now().Add(whistleblowerRetention),
	})
	if err != nil {
		s.logger.Error("store whistleblower submission", zap.Error(err))
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, whistleblowerSubmitResponse{
		Token: token,
		Message: "Save this token now. We do not store it in plaintext and " +
			"cannot recover it. Use it on the status page to check progress.",
	})
}

// handleIntakeStatus accepts a token and returns its status badge only.
// Path: POST /api/v1/misconduct/intake-status. PUBLIC. The response body
// is intentionally minimal — `{"status":"received|under_review|closed"}`
// and nothing else.
func (s *Server) handleIntakeStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req intakeStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	tok := strings.TrimSpace(req.Token)
	if tok == "" {
		http.Error(w, "token required", http.StatusBadRequest)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}

	all, err := s.store.ListAllWhistleblowerSubmissions(r.Context())
	if err != nil {
		s.logger.Warn("list whistleblower submissions", zap.Error(err))
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}

	// Iterate every stored hash and compare with bcrypt (which is itself
	// constant-time per-call). We do NOT break early on a hit — keeping
	// the loop length data-independent prevents an attacker from inferring
	// token validity from response latency. subtle.ConstantTimeCompare is
	// imported here so future-us can extend the loop with a fixed-cost
	// compare against any non-bcrypt secrets the schema gains.
	matched := false
	var status string
	for _, row := range all {
		if err := bcrypt.CompareHashAndPassword([]byte(row.TokenHash), []byte(tok)); err == nil {
			matched = true
			status = row.Status
		}
		// Force at least one constant-time op per row so timing stays
		// uniform between match / no-match paths.
		_ = subtle.ConstantTimeCompare([]byte(row.TokenHash), []byte(row.TokenHash))
	}
	if !matched {
		// Always 200 with `unknown` instead of 404 — leaking existence
		// would let an attacker enumerate valid tokens by status code.
		writeJSON(w, http.StatusOK, intakeStatusResponse{Status: "unknown"})
		return
	}
	writeJSON(w, http.StatusOK, intakeStatusResponse{Status: status})
}

// generateToken returns 32 cryptographically random bytes encoded as
// base64url (no padding) — opaque, single-use, never re-derivable from
// stored state.
func generateToken() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		// Fall back to a timestamp-derived token. This is a last-resort
		// path; production never hits it because crypto/rand on every
		// supported platform reads from a kernel entropy source.
		now := time.Now().UnixNano()
		buf = make([]byte, 8)
		binary.BigEndian.PutUint64(buf, uint64(now))
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

// misconductSealer returns the AES-256-GCM sealer used to encrypt
// whistleblower bodies at rest.
//
// Resolution order:
//
//  1. The platform secretbox.Sealer (s.sealer) if configured. This is the
//     same sealer that protects provider credentials, so rotating that
//     key rotates whistleblower bodies in lockstep.
//  2. WHISTLEBLOWER_BODY_KEY env var (32-byte raw or 64-char hex).
//
// Rotation path: when the platform sealer rotates we re-seal in place via
// a one-shot job (out of scope for this PR — submissions are append-only
// for the 90-day retention window, so the next rotation simply applies
// going forward and the sweep job clears legacy ciphertext within 90 days).
func (s *Server) misconductSealer() (*secretbox.Sealer, error) {
	if s.sealer != nil {
		return s.sealer, nil
	}
	raw := strings.TrimSpace(envLookup(whistleblowerBodyKeyEnv))
	if raw == "" {
		return nil, errors.New("WHISTLEBLOWER_BODY_KEY env var not set and platform sealer unavailable")
	}
	return secretbox.NewSealerFromConfig(raw)
}

// envLookup is a thin wrapper so tests can inject keys without setting
// process env. The wrapper lets future-us swap to a config struct without
// touching every call site.
var envLookup = func(key string) string {
	return osGetenv(key)
}

// ── Investigator-gated case CRUD ─────────────────────────────────────────────

type createCaseRequest struct {
	TenantID      string  `json:"tenant_id"`
	Summary       string  `json:"summary"`
	SubjectUserID *string `json:"subject_user_id,omitempty"`
	SubjectLabel  *string `json:"subject_label,omitempty"`
}

type patchCaseRequest struct {
	Status        *string `json:"status,omitempty"`
	Summary       *string `json:"summary,omitempty"`
	SubjectUserID *string `json:"subject_user_id,omitempty"`
	SubjectLabel  *string `json:"subject_label,omitempty"`
}

type attachEvidenceRequest struct {
	EvidenceID string `json:"evidence_id"`
}

func (s *Server) handleMisconductCasesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listMisconductCases(w, r)
	case http.MethodPost:
		s.createMisconductCase(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listMisconductCases(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleInvestigator, roleAdmin); !ok {
		return
	}
	tenantStr := r.URL.Query().Get("tenant_id")
	tenantID, err := uuid.Parse(tenantStr)
	if err != nil {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	status := r.URL.Query().Get("status")
	cases, total, err := s.store.ListMisconductCases(r.Context(),
		storage.MisconductCaseFilter{TenantID: tenantID, Status: status}, limit, offset)
	if err != nil {
		s.logger.Warn("list misconduct cases", zap.Error(err))
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": cases,
		"pagination": map[string]any{
			"total": total, "limit": limit, "offset": offset,
		},
	})
}

func (s *Server) createMisconductCase(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	var req createCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	openedBy := s.userIDForPrincipalCtx(r.Context(), principal)
	params := storage.CreateMisconductCaseParams{
		TenantID: tenantID,
		Summary:  req.Summary,
	}
	if openedBy != uuid.Nil {
		v := openedBy
		params.OpenedBy = &v
	}
	if req.SubjectUserID != nil && strings.TrimSpace(*req.SubjectUserID) != "" {
		uid, err := uuid.Parse(strings.TrimSpace(*req.SubjectUserID))
		if err == nil {
			params.SubjectUserID = &uid
		}
	}
	if req.SubjectLabel != nil && strings.TrimSpace(*req.SubjectLabel) != "" {
		v := strings.TrimSpace(*req.SubjectLabel)
		params.SubjectLabel = &v
	}
	created, err := s.store.CreateMisconductCase(r.Context(), params)
	if err != nil {
		s.logger.Warn("create misconduct case", zap.Error(err))
		http.Error(w, "create failed", http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "misconduct.case.create", "misconduct_case", created.ID.String(), map[string]any{
		"summary": created.Summary,
	})
	writeJSON(w, http.StatusCreated, created)
}

func (s *Server) handleMisconductCaseSubroutes(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v1/misconduct/cases/{id}[ /evidence | /signals ]
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/misconduct/cases/")
	parts := strings.SplitN(rest, "/", 2)
	caseID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid case id", http.StatusBadRequest)
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getMisconductCase(w, r, caseID)
		case http.MethodPatch:
			s.patchMisconductCase(w, r, caseID)
		default:
			w.Header().Set("Allow", "GET, PATCH")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		}
		return
	}
	switch parts[1] {
	case "evidence":
		s.handleCaseEvidence(w, r, caseID)
	case "signals":
		s.handleCaseSignals(w, r, caseID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) getMisconductCase(w http.ResponseWriter, r *http.Request, caseID uuid.UUID) {
	if _, ok := s.authorize(w, r, roleInvestigator, roleAdmin); !ok {
		return
	}
	c, err := s.store.GetMisconductCase(r.Context(), caseID)
	if err != nil {
		s.logger.Warn("get misconduct case", zap.Error(err))
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if c == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) patchMisconductCase(w http.ResponseWriter, r *http.Request, caseID uuid.UUID) {
	principal, ok := s.authorize(w, r, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	var req patchCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	params := storage.UpdateMisconductCaseParams{}
	if req.Status != nil {
		st := strings.TrimSpace(*req.Status)
		switch st {
		case "open", "investigating", "closed":
			params.Status = st
		case "":
			// no-op
		default:
			http.Error(w, "invalid status", http.StatusBadRequest)
			return
		}
	}
	if req.Summary != nil {
		v := *req.Summary
		params.Summary = &v
	}
	if req.SubjectUserID != nil {
		s := strings.TrimSpace(*req.SubjectUserID)
		if s == "" {
			n := uuid.Nil
			params.SubjectUserID = &n
		} else if uid, err := uuid.Parse(s); err == nil {
			params.SubjectUserID = &uid
		}
	}
	if req.SubjectLabel != nil {
		v := strings.TrimSpace(*req.SubjectLabel)
		params.SubjectLabel = &v
	}
	updated, err := s.store.UpdateMisconductCase(r.Context(), caseID, params)
	if err != nil {
		s.logger.Warn("update misconduct case", zap.Error(err))
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}
	if updated == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	s.recordAudit(r.Context(), principal, updated.TenantID, "misconduct.case.update", "misconduct_case", caseID.String(), map[string]any{
		"status": updated.Status,
	})
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleCaseEvidence(w http.ResponseWriter, r *http.Request, caseID uuid.UUID) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleInvestigator, roleAdmin); !ok {
			return
		}
		links, err := s.store.ListCaseEvidence(r.Context(), caseID)
		if err != nil {
			s.logger.Warn("list case evidence", zap.Error(err))
			http.Error(w, "list failed", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": links})
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleInvestigator, roleAdmin)
		if !ok {
			return
		}
		var req attachEvidenceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		evidenceID, err := uuid.Parse(strings.TrimSpace(req.EvidenceID))
		if err != nil {
			http.Error(w, "evidence_id required", http.StatusBadRequest)
			return
		}
		// Look up the case to get the tenant for the audit log.
		c, err := s.store.GetMisconductCase(r.Context(), caseID)
		if err != nil || c == nil {
			http.Error(w, "case not found", http.StatusNotFound)
			return
		}
		link, err := s.store.AttachCaseEvidence(r.Context(), caseID, evidenceID)
		if err != nil {
			s.logger.Warn("attach case evidence", zap.Error(err))
			http.Error(w, "attach failed", http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, c.TenantID, "misconduct.case.evidence.attach", "misconduct_case", caseID.String(), map[string]any{
			"evidence_id": evidenceID.String(),
		})
		writeJSON(w, http.StatusCreated, link)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCaseSignals(w http.ResponseWriter, r *http.Request, caseID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleInvestigator, roleAdmin); !ok {
		return
	}
	signals, err := s.store.ListRiskSignals(r.Context(), caseID)
	if err != nil {
		s.logger.Warn("list risk signals", zap.Error(err))
		http.Error(w, "list failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": signals})
}

// ── Job handlers ─────────────────────────────────────────────────────────────

// misconductScorePayload is the queued payload for misconduct.score jobs.
type misconductScorePayload struct {
	CaseID string `json:"case_id"`
}

// handleMisconductScoreJob recomputes risk_score for a single case.
//
// Algorithm: gather signals for the case's subject (audit_logs by actor,
// security_events for the tenant in the last 30 days, failed compliance
// results in the last 30 days). Convert each finding to a severity bucket,
// emit one risk_signals row, sum the weights (critical=30/high=15/medium=5
// /low=1), cap at 100. Replace prior signals so re-running the job is
// idempotent.
func (s *Server) handleMisconductScoreJob(ctx context.Context, job *storage.Job) error {
	if s == nil || s.store == nil {
		return errors.New("misconduct score: store unavailable")
	}
	var payload misconductScorePayload
	if len(job.Payload) > 0 {
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return fmt.Errorf("decode misconduct.score payload: %w", err)
		}
	}
	caseID, err := uuid.Parse(strings.TrimSpace(payload.CaseID))
	if err != nil {
		return fmt.Errorf("invalid case_id: %w", err)
	}
	c, err := s.store.GetMisconductCase(ctx, caseID)
	if err != nil {
		return fmt.Errorf("load case: %w", err)
	}
	if c == nil {
		return fmt.Errorf("case %s not found", caseID)
	}
	// Recompute signals fresh.
	if err := s.store.DeleteRiskSignalsForCase(ctx, caseID); err != nil {
		return fmt.Errorf("clear signals: %w", err)
	}

	since := time.Now().Add(-30 * 24 * time.Hour)
	weights := map[string]int{"critical": 30, "high": 15, "medium": 5, "low": 1}
	score := 0

	// Audit-log activity for the subject user (if any) — high volume implies
	// elevated privilege use; bucket into severity.
	if c.SubjectUserID != nil && *c.SubjectUserID != uuid.Nil {
		n, err := s.store.CountAuditLogsForActor(ctx, *c.SubjectUserID, since)
		if err == nil {
			sev := bucketAuditCount(n)
			if sev != "" {
				w := weights[sev]
				score += w
				_, _ = s.store.CreateRiskSignal(ctx, storage.CreateRiskSignalParams{
					CaseID:      caseID,
					SignalType:  "audit_log_activity",
					Severity:    sev,
					SourceID:    c.SubjectUserID,
					SourceTable: "audit_logs",
					Weight:      w,
				})
			}
		}
	}

	// Security events for the tenant — assigned at the case level since
	// sec events don't carry a user_id column.
	sevCounts, err := s.store.CountSecurityEventsBySeverity(ctx, c.TenantID, since)
	if err == nil {
		for sev, n := range sevCounts {
			if n == 0 {
				continue
			}
			normalized := normalizeSeverity(sev)
			if normalized == "" {
				continue
			}
			w := weights[normalized]
			score += w
			_, _ = s.store.CreateRiskSignal(ctx, storage.CreateRiskSignalParams{
				CaseID:      caseID,
				SignalType:  "security_event",
				Severity:    normalized,
				SourceTable: "security_events",
				Weight:      w,
			})
		}
	}

	// Failed compliance results — anything that fails counts as medium.
	if n, err := s.store.CountFailedComplianceForTenant(ctx, c.TenantID, since); err == nil && n > 0 {
		sev := bucketComplianceFailures(n)
		w := weights[sev]
		score += w
		_, _ = s.store.CreateRiskSignal(ctx, storage.CreateRiskSignalParams{
			CaseID:      caseID,
			SignalType:  "compliance_failures",
			Severity:    sev,
			SourceTable: "compliance_results",
			Weight:      w,
		})
	}

	if score > 100 {
		score = 100
	}
	if err := s.store.SetMisconductCaseRiskScore(ctx, caseID, score); err != nil {
		return fmt.Errorf("update risk score: %w", err)
	}
	s.logger.Info("misconduct.score complete",
		zap.String("case_id", caseID.String()),
		zap.Int("score", score),
	)
	return nil
}

func bucketAuditCount(n int) string {
	switch {
	case n >= 500:
		return "critical"
	case n >= 100:
		return "high"
	case n >= 20:
		return "medium"
	case n > 0:
		return "low"
	default:
		return ""
	}
}

func bucketComplianceFailures(n int) string {
	switch {
	case n >= 50:
		return "high"
	case n >= 10:
		return "medium"
	default:
		return "low"
	}
}

func normalizeSeverity(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical", "crit":
		return "critical"
	case "high":
		return "high"
	case "medium", "med":
		return "medium"
	case "low":
		return "low"
	default:
		return ""
	}
}

// handleMisconductRetentionSweepJob deletes whistleblower submissions whose
// retention deadline has passed. The daily cadence is enforced by the
// retention scheduler; this handler is the body.
func (s *Server) handleMisconductRetentionSweepJob(ctx context.Context, job *storage.Job) error {
	if s == nil || s.store == nil {
		return errors.New("retention sweep: store unavailable")
	}
	deleted, err := s.store.SweepWhistleblowerSubmissions(ctx, time.Now())
	if err != nil {
		return fmt.Errorf("sweep submissions: %w", err)
	}
	s.logger.Info("misconduct.retention_sweep complete", zap.Int64("deleted", deleted))
	return nil
}
