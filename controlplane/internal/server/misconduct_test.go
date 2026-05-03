package server

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// solveTestPoW finds a nonce satisfying sha256(challenge||nonce) zero-prefix
// for the given bit difficulty. Used to drive the public submit endpoint
// in tests without actually crunching through whistleblowerPoWDifficulty
// (≈1M iterations) every run.
func solveTestPoW(challenge string, bits int) string {
	for i := 0; i < 1<<24; i++ {
		nonce := fmt.Sprintf("%x", i)
		if verifyPoW(challenge, nonce, bits) {
			return nonce
		}
	}
	return ""
}

func newMisconductTestServer(t *testing.T) (*Server, *fakeStore) {
	t.Helper()
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("investigator", "inv-token"),
	}
	store := &fakeStore{
		userRoles: map[uuid.UUID][]string{},
	}
	srv := New(logger, cfg, store, &stubQueue{})
	srv.auditAsync = false
	// Use bcrypt MinCost in tests so iterating every row in the status
	// handler doesn't slow the suite to a crawl.
	whistleblowerBcryptCostOverride = bcrypt.MinCost
	t.Cleanup(func() { whistleblowerBcryptCostOverride = whistleblowerBcryptCost })
	// Use the existing platform sealer (configured via cfg.Secrets.EncryptionKey)
	// when available; otherwise fall through to env. For tests we set a
	// deterministic 32-byte key via the env var path.
	t.Setenv(whistleblowerBodyKeyEnv, hex.EncodeToString(make([]byte, 32)))
	return srv, store
}

// TestPoWSelfCheck guards the verifyPoW helper because the rest of the
// suite depends on it producing the same answer the JS client will compute.
func TestPoWSelfCheck(t *testing.T) {
	if !verifyPoW("zero", solveTestPoW("zero", 8), 8) {
		t.Fatal("self-check failed: solveTestPoW returned a nonce verifyPoW rejects")
	}
	if verifyPoW("zero", "wrong", 8) {
		t.Fatal("verifyPoW accepted a known-bad nonce")
	}
}

func TestVerifyPoWBitness(t *testing.T) {
	for _, bits := range []int{4, 12} {
		challenge := "deadbeef"
		nonce := solveTestPoW(challenge, bits)
		if nonce == "" {
			t.Fatalf("could not solve %d-bit PoW", bits)
		}
		if !verifyPoW(challenge, nonce, bits) {
			t.Fatalf("verify failed for %d-bit", bits)
		}
		if verifyPoW(challenge, "", bits) {
			t.Fatalf("verify accepted empty nonce for %d-bit", bits)
		}
	}
}

// TestWhistleblowerEndToEnd runs the full submit -> intake-status loop
// against the public routes. It uses a low-cost bcrypt + an injected,
// low-difficulty challenge so the test is fast.
func TestWhistleblowerEndToEnd(t *testing.T) {
	srv, _ := newMisconductTestServer(t)
	lim := srv.ensureWhistleblowerLimiter()

	// Solve a challenge at the real difficulty up front. 20 bits is ~1M
	// hashes; on a modern CPU this is ~100–300ms.
	challenge := lim.issueChallenge()
	nonce := solveTestPoW(challenge, whistleblowerPoWDifficulty)
	if nonce == "" {
		t.Fatal("could not solve real-difficulty PoW")
	}

	body, _ := json.Marshal(map[string]string{
		"description":      "Manager pressured staff to falsify reports.",
		"approximate_date": "2026-04-30",
		"subject_role":     "manager",
		"challenge":        challenge,
		"nonce":            nonce,
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.1:1234"
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("submit got %d: %s", rec.Code, rec.Body.String())
	}
	var submitResp whistleblowerSubmitResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &submitResp); err != nil {
		t.Fatalf("decode submit response: %v", err)
	}
	if submitResp.Token == "" {
		t.Fatal("submit response missing token")
	}

	// Check status — should be `received`.
	statusBody, _ := json.Marshal(map[string]string{"token": submitResp.Token})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/intake-status", bytes.NewReader(statusBody))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status got %d: %s", rec.Code, rec.Body.String())
	}
	var statusResp intakeStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if statusResp.Status != "received" {
		t.Fatalf("status got %s, want received", statusResp.Status)
	}
	// Verify the response body contains exactly {"status":"received"} —
	// no extra fields. The trim is to drop the trailing newline added by
	// json.Encoder.
	got := strings.TrimSpace(rec.Body.String())
	if got != `{"status":"received"}` {
		t.Fatalf("status body has extra fields: %q", got)
	}
}

func TestSubmitRejectsWithoutPoW(t *testing.T) {
	srv, _ := newMisconductTestServer(t)
	body, _ := json.Marshal(map[string]string{
		"description":      "Reportable.",
		"approximate_date": "2026-04-15",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "10.0.0.2:1234"
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 missing PoW got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSubmitPerIPRateLimit(t *testing.T) {
	srv, _ := newMisconductTestServer(t)
	lim := srv.ensureWhistleblowerLimiter()
	// Pre-fill the per-IP slot for our test IP up to the limit.
	for i := 0; i < whistleblowerIPRateLimit; i++ {
		if err := lim.allow("203.0.113.42"); err != nil {
			t.Fatalf("unexpected throttle on hit %d: %v", i, err)
		}
	}
	// Now the next request from this IP should be rejected before the
	// PoW check runs.
	body, _ := json.Marshal(map[string]string{
		"description": "blocked",
		"challenge":   "x",
		"nonce":       "y",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.42:80"
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 got %d", rec.Code)
	}
}

func TestSubmitGlobalRateLimit(t *testing.T) {
	srv, _ := newMisconductTestServer(t)
	lim := srv.ensureWhistleblowerLimiter()
	// Saturate global counter from many distinct IPs.
	for i := 0; i < whistleblowerGlobalRateLimit; i++ {
		if err := lim.allow(fmt.Sprintf("198.51.100.%d", i)); err != nil {
			t.Fatalf("unexpected throttle on hit %d: %v", i, err)
		}
	}
	body, _ := json.Marshal(map[string]string{
		"description": "blocked",
		"challenge":   "x",
		"nonce":       "y",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/submit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// Fresh source IP — global limit must still kick in.
	req.RemoteAddr = "192.0.2.99:80"
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 from global limit got %d", rec.Code)
	}
}

func TestIntakeStatusUnknownToken(t *testing.T) {
	srv, _ := newMisconductTestServer(t)
	body, _ := json.Marshal(map[string]string{"token": "no-such-token"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/intake-status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 unknown got %d", rec.Code)
	}
	got := strings.TrimSpace(rec.Body.String())
	if got != `{"status":"unknown"}` {
		t.Fatalf("unknown response leaked details: %q", got)
	}
}

func TestInvestigatorOnlyRoutes(t *testing.T) {
	srv, _ := newMisconductTestServer(t)
	// /api/v1/misconduct/cases requires investigator|admin. A viewer must 403.
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "viewer-token"),
	}
	store := &fakeStore{userRoles: map[uuid.UUID][]string{}}
	srv2 := New(logger, cfg, store, &stubQueue{})
	srv2.auditAsync = false
	_ = srv

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/misconduct/cases?tenant_id="+uuid.New().String(), nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	srv2.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for viewer got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestInvestigatorCaseCreate(t *testing.T) {
	srv, store := newMisconductTestServer(t)
	tenantID := uuid.New()
	body, _ := json.Marshal(map[string]any{
		"tenant_id": tenantID.String(),
		"summary":   "Coworker reported retaliation",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/misconduct/cases",
		bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer inv-token")
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create case got %d: %s", rec.Code, rec.Body.String())
	}
	var c storage.MisconductCase
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if c.TenantID != tenantID || c.Status != "open" {
		t.Fatalf("unexpected case: %+v", c)
	}
	if len(store.auditLogs) == 0 {
		t.Fatal("expected audit log on case create")
	}
}

func TestRetentionSweepJobDeletesExpired(t *testing.T) {
	srv, store := newMisconductTestServer(t)
	// Seed two submissions: one expired, one fresh.
	expired := storage.WhistleblowerSubmission{
		ID:             uuid.New(),
		TokenHash:      "h1",
		SubmittedAt:    time.Now().Add(-100 * 24 * time.Hour),
		RetentionUntil: time.Now().Add(-time.Hour),
		Status:         "received",
	}
	fresh := storage.WhistleblowerSubmission{
		ID:             uuid.New(),
		TokenHash:      "h2",
		SubmittedAt:    time.Now(),
		RetentionUntil: time.Now().Add(89 * 24 * time.Hour),
		Status:         "received",
	}
	store.whistleblowerSubs = []storage.WhistleblowerSubmission{expired, fresh}

	if err := srv.handleMisconductRetentionSweepJob(context.Background(),
		&storage.Job{ID: uuid.New()}); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(store.whistleblowerSubs) != 1 || store.whistleblowerSubs[0].ID != fresh.ID {
		t.Fatalf("sweep did not delete expired row: %+v", store.whistleblowerSubs)
	}
}

func TestScoreJobCapsAt100(t *testing.T) {
	srv, store := newMisconductTestServer(t)
	tenantID := uuid.New()
	subjectID := uuid.New()
	c, err := store.CreateMisconductCase(context.Background(), storage.CreateMisconductCaseParams{
		TenantID:      tenantID,
		Summary:       "test",
		SubjectUserID: &subjectID,
	})
	if err != nil {
		t.Fatalf("create case: %v", err)
	}

	payload, _ := json.Marshal(misconductScorePayload{CaseID: c.ID.String()})
	if err := srv.handleMisconductScoreJob(context.Background(), &storage.Job{
		ID:       uuid.New(),
		TenantID: tenantID,
		Type:     JobTypeMisconductScore,
		Payload:  payload,
	}); err != nil {
		t.Fatalf("score: %v", err)
	}

	updated, err := store.GetMisconductCase(context.Background(), c.ID)
	if err != nil {
		t.Fatalf("get case: %v", err)
	}
	if updated.RiskScore < 0 || updated.RiskScore > 100 {
		t.Fatalf("score out of range: %d", updated.RiskScore)
	}
}

// authWithTokens is shared with the existing server tests.
//
// The investigator-token alias is registered with the investigator role so
// the new handlers' RBAC gate finds a match. We keep the helper next to the
// other test fixtures rather than duplicating its body inline.
