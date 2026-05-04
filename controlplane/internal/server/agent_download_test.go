package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// writeTestBinary dumps a deterministic byte slice to disk so the server
// can compute a stable SHA-256 checksum + ed25519 signature for tests.
func writeTestBinary(t *testing.T, dir, osName, archName string, payload []byte) string {
	t.Helper()
	path := filepath.Join(dir, "controlone-agent-"+osName+"-"+archName)
	if err := os.WriteFile(path, payload, 0o755); err != nil {
		t.Fatalf("write test binary: %v", err)
	}
	return path
}

// writeSigningKeyPair writes a PEM-encoded PKCS#8 ed25519 private key and a
// PEM-encoded PKIX public key to a tmp dir and returns their paths.
func writeSigningKeyPair(t *testing.T, dir string) (string, string, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key: %v", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	privPath := filepath.Join(dir, "agent-sign.key")
	pubPath := filepath.Join(dir, "agent-sign.pub")
	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	return privPath, pubPath, pub
}

func newAgentTestServer(t *testing.T, binaryDir, signingKey, signingPub string) *Server {
	t.Helper()
	// AllowAnonymousBinary keeps the existing /agent/binary{,/manifest} test
	// fixtures working without each having to mint an enrollment token. The
	// dedicated token-enforcement tests below build their own server with the
	// flag off (the prod default) and a seeded fakeStore.
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Agent: config.AgentConfig{
			BinaryDir:            binaryDir,
			SigningKeyPath:       signingKey,
			SigningPublicKeyPath: signingPub,
			AllowAnonymousBinary: true,
		},
	}
	return New(zap.NewNop(), cfg, nil, nil)
}

// newAgentTestServerWithStore wires a fakeStore so token-enforcement tests can
// seed valid/expired/revoked enrollment tokens. AllowAnonymousBinary defaults
// to false so the binary + manifest handlers exercise the token path too.
func newAgentTestServerWithStore(t *testing.T, binaryDir string, store Store) *Server {
	t.Helper()
	cfg := &config.Config{
		HTTP:  config.HTTPConfig{Address: ":0"},
		Agent: config.AgentConfig{BinaryDir: binaryDir},
	}
	return New(zap.NewNop(), cfg, store, nil)
}

// seedEnrollmentToken inserts a token into the fakeStore keyed by the SHA-256
// hash of the raw value. Returns the raw token so the test can pass it as a
// query parameter and the seeded record so the test can mutate revocation /
// expiry attributes after creation.
func seedEnrollmentToken(t *testing.T, fs *fakeStore, mutate func(*storage.EnrollmentToken)) (raw string, rec storage.EnrollmentToken) {
	t.Helper()
	raw = "cot_test_" + uuid.New().String()
	h := sha256.Sum256([]byte(raw))
	hash := hex.EncodeToString(h[:])

	rec = storage.EnrollmentToken{
		ID:        uuid.New(),
		TenantID:  uuid.New(),
		Name:      "test-token",
		TokenHash: hash,
		MaxNodes:  0,
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}
	if mutate != nil {
		mutate(&rec)
	}

	if fs.enrollmentTokens == nil {
		fs.enrollmentTokens = make(map[string]storage.EnrollmentToken)
	}
	fs.enrollmentTokens[hash] = rec
	return raw, rec
}

func TestInstallScriptRenders(t *testing.T) {
	t.Parallel()

	// Each platform/mode permutation needs its own seeded token because the
	// handler now validates `?token=` against the store before rendering.
	store := &fakeStore{}
	rawToken, _ := seedEnrollmentToken(t, store, nil)

	cases := []struct {
		name            string
		extraQuery      string
		wantSubstring   string
		contentType     string
		wantDisposition string
	}{
		{
			name:            "bash default",
			extraQuery:      "",
			wantSubstring:   "controlone-agent",
			contentType:     "text/x-shellscript",
			wantDisposition: "install-agent.sh",
		},
		{
			name:            "powershell",
			extraQuery:      "&platform=windows",
			wantSubstring:   "$env:PROCESSOR_ARCHITECTURE",
			contentType:     "text/plain; charset=utf-8",
			wantDisposition: "install-agent.ps1",
		},
		{
			name:            "bash uninstall",
			extraQuery:      "&mode=uninstall",
			wantSubstring:   "uninstall",
			contentType:     "text/x-shellscript",
			wantDisposition: "uninstall-agent.sh",
		},
		{
			name:            "powershell uninstall",
			extraQuery:      "&platform=windows&mode=uninstall",
			wantSubstring:   "controlone-agent.exe",
			contentType:     "text/plain; charset=utf-8",
			wantDisposition: "uninstall-agent.ps1",
		},
	}

	srv := newAgentTestServerWithStore(t, t.TempDir(), store)

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			query := "token=" + rawToken + tc.extraQuery
			req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?"+query, nil)
			rec := httptest.NewRecorder()
			srv.handleAgentInstallScript(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != tc.contentType {
				t.Fatalf("Content-Type = %q, want %q", ct, tc.contentType)
			}
			if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, tc.wantDisposition) {
				t.Fatalf("Content-Disposition = %q, want to contain %q", disp, tc.wantDisposition)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
				t.Fatalf("Cache-Control = %q, want %q", cc, "no-store")
			}
			if pragma := rec.Header().Get("Pragma"); pragma != "no-cache" {
				t.Fatalf("Pragma = %q, want %q", pragma, "no-cache")
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.wantSubstring) {
				t.Fatalf("body missing %q; got first 200 chars: %s", tc.wantSubstring, firstN(body, 200))
			}
		})
	}
}

// scriptBodyMarker is a string we expect to appear in any successfully rendered
// install script body. Token-rejection tests use this as a negative assertion
// to guarantee the handler short-circuited before template execution.
const scriptBodyMarker = "controlone-agent"

func TestInstallScript_RejectsMissingToken(t *testing.T) {
	t.Parallel()

	srv := newAgentTestServerWithStore(t, t.TempDir(), &fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", cc, "no-store")
	}
	if strings.Contains(rec.Body.String(), scriptBodyMarker) {
		t.Fatalf("error response leaked install-script body: %s", rec.Body.String())
	}
}

func TestInstallScript_RejectsInvalidToken(t *testing.T) {
	t.Parallel()

	srv := newAgentTestServerWithStore(t, t.TempDir(), &fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?token=does-not-exist", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), scriptBodyMarker) {
		t.Fatalf("rejected request leaked script body: %s", rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want %q", cc, "no-store")
	}
}

func TestInstallScript_RejectsExpiredToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	raw, _ := seedEnrollmentToken(t, store, func(tok *storage.EnrollmentToken) {
		tok.ExpiresAt = time.Now().Add(-1 * time.Hour)
	})
	srv := newAgentTestServerWithStore(t, t.TempDir(), store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?token="+raw, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), scriptBodyMarker) {
		t.Fatalf("expired-token response leaked script body")
	}
}

func TestInstallScript_RejectsRevokedToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	raw, _ := seedEnrollmentToken(t, store, func(tok *storage.EnrollmentToken) {
		tok.RevokedAt = sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true}
	})
	srv := newAgentTestServerWithStore(t, t.TempDir(), store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?token="+raw, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), scriptBodyMarker) {
		t.Fatalf("revoked-token response leaked script body")
	}
}

func TestInstallScript_RejectsExhaustedToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	raw, _ := seedEnrollmentToken(t, store, func(tok *storage.EnrollmentToken) {
		tok.MaxNodes = 1
		tok.NodesEnrolled = 1
	})
	srv := newAgentTestServerWithStore(t, t.TempDir(), store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?token="+raw, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestInstallScript_AcceptsValidToken(t *testing.T) {
	t.Parallel()

	store := &fakeStore{}
	raw, _ := seedEnrollmentToken(t, store, nil)
	srv := newAgentTestServerWithStore(t, t.TempDir(), store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?token="+raw, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentInstallScript(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/x-shellscript" {
		t.Fatalf("Content-Type = %q, want text/x-shellscript", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if !strings.Contains(rec.Body.String(), scriptBodyMarker) {
		t.Fatalf("body missing expected marker; got first 200 chars: %s", firstN(rec.Body.String(), 200))
	}
	// The rendered script must embed the operator-supplied token verbatim so
	// the agent can call /api/v1/enroll without manual intervention.
	if !strings.Contains(rec.Body.String(), raw) {
		t.Fatalf("rendered script did not embed the operator token")
	}
}

func TestAgentBinary_RejectsInvalidToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestBinary(t, dir, "linux", "amd64", []byte("payload"))

	srv := newAgentTestServerWithStore(t, dir, &fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary?os=linux&arch=amd64&token=bogus", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinary(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}

func TestAgentBinary_RejectsMissingToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestBinary(t, dir, "linux", "amd64", []byte("payload"))
	srv := newAgentTestServerWithStore(t, dir, &fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary?os=linux&arch=amd64", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinary(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAgentBinaryManifest_RejectsInvalidToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestBinary(t, dir, "linux", "amd64", []byte("payload"))
	srv := newAgentTestServerWithStore(t, dir, &fakeStore{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary/manifest?os=linux&arch=amd64&token=bogus", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinaryManifest(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rec.Code, rec.Body.String())
	}
}

func TestAgentBinary_AcceptsValidToken(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	payload := []byte("real-binary")
	writeTestBinary(t, dir, "linux", "amd64", payload)
	store := &fakeStore{}
	raw, _ := seedEnrollmentToken(t, store, nil)
	srv := newAgentTestServerWithStore(t, dir, store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary?os=linux&arch=amd64&token="+raw, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
}

func TestAgentBinaryReturnsSignedHeadersWhenKeyConfigured(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	payload := []byte("fake-agent-binary-content")
	writeTestBinary(t, dir, "linux", "amd64", payload)

	privPath, pubPath, pubKey := writeSigningKeyPair(t, dir)
	srv := newAgentTestServer(t, dir, privPath, pubPath)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary?os=linux&arch=amd64", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	checksum := rec.Header().Get("X-ControlOne-Checksum")
	if !strings.HasPrefix(checksum, "sha256-") {
		t.Fatalf("missing or malformed sha256 checksum header: %q", checksum)
	}
	hexHash := strings.TrimPrefix(checksum, "sha256-")
	sum := sha256.Sum256(payload)
	if hexHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("checksum mismatch: got %s, want %s", hexHash, hex.EncodeToString(sum[:]))
	}

	sigHdr := rec.Header().Get("X-ControlOne-Signature")
	if !strings.HasPrefix(sigHdr, "ed25519-") {
		t.Fatalf("missing or malformed signature header: %q", sigHdr)
	}
	sigB64 := strings.TrimPrefix(sigHdr, "ed25519-")
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(pubKey, sum[:], sigBytes) {
		t.Fatal("signature did not verify against the configured public key")
	}
}

func TestAgentBinaryServesUnsignedWhenKeyMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	payload := []byte("unsigned-binary")
	writeTestBinary(t, dir, "linux", "arm64", payload)
	srv := newAgentTestServer(t, dir, "", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary?os=linux&arch=arm64", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinary(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("X-ControlOne-Checksum"); !strings.HasPrefix(got, "sha256-") {
		t.Fatalf("checksum header missing; got %q", got)
	}
	if got := rec.Header().Get("X-ControlOne-Signature"); got != "" {
		t.Fatalf("expected no signature header, got %q", got)
	}
}

func TestAgentBinaryManifest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	payload := []byte("manifest-test-binary")
	writeTestBinary(t, dir, "darwin", "arm64", payload)
	privPath, pubPath, pubKey := writeSigningKeyPair(t, dir)
	srv := newAgentTestServer(t, dir, privPath, pubPath)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary/manifest?os=darwin&arch=arm64", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinaryManifest(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}

	var manifest binaryManifest
	if err := json.NewDecoder(rec.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	sum := sha256.Sum256(payload)
	if manifest.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("manifest sha256 mismatch: got %s", manifest.SHA256)
	}
	if manifest.SizeBytes != int64(len(payload)) {
		t.Fatalf("manifest size mismatch: got %d want %d", manifest.SizeBytes, len(payload))
	}

	sigBytes, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil {
		t.Fatalf("decode manifest signature: %v", err)
	}
	if !ed25519.Verify(pubKey, sum[:], sigBytes) {
		t.Fatal("manifest signature did not verify")
	}
	if manifest.PublicKeyFingerprint == "" {
		t.Fatal("expected public_key_fingerprint in manifest")
	}
}

func TestAgentPublicKeyEndpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeTestBinary(t, dir, "linux", "amd64", []byte("x"))
	privPath, pubPath, pubKey := writeSigningKeyPair(t, dir)
	srv := newAgentTestServer(t, dir, privPath, pubPath)

	t.Run("returns pem when configured", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/public-key", nil)
		rec := httptest.NewRecorder()
		srv.handleAgentPublicKey(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		body, _ := io.ReadAll(rec.Body)
		block, _ := pem.Decode(body)
		if block == nil {
			t.Fatal("response body is not a PEM block")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			t.Fatalf("parse public key: %v", err)
		}
		asEd25519, ok := parsed.(ed25519.PublicKey)
		if !ok {
			t.Fatalf("public key is not ed25519: %T", parsed)
		}
		if !asEd25519.Equal(pubKey) {
			t.Fatal("served public key does not match configured key")
		}
	})

	t.Run("returns 404 when unconfigured", func(t *testing.T) {
		unsigned := newAgentTestServer(t, dir, "", "")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/public-key", nil)
		rec := httptest.NewRecorder()
		unsigned.handleAgentPublicKey(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", rec.Code)
		}
	})
}

func TestAgentBinaryRejectsMissingOSArch(t *testing.T) {
	t.Parallel()

	srv := newAgentTestServer(t, t.TempDir(), "", "")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/binary", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBinary(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func firstN(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}
