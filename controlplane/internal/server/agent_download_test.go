package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
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

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
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
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Agent: config.AgentConfig{
			BinaryDir:            binaryDir,
			SigningKeyPath:       signingKey,
			SigningPublicKeyPath: signingPub,
		},
	}
	return New(zap.NewNop(), cfg, nil, nil)
}

func TestInstallScriptRenders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		query           string
		wantSubstring   string
		contentType     string
		wantDisposition string
	}{
		{
			name:            "bash default",
			query:           "token=abc",
			wantSubstring:   "controlone-agent",
			contentType:     "text/x-shellscript",
			wantDisposition: "install-agent.sh",
		},
		{
			name:            "powershell",
			query:           "token=abc&platform=windows",
			wantSubstring:   "$env:PROCESSOR_ARCHITECTURE",
			contentType:     "text/plain; charset=utf-8",
			wantDisposition: "install-agent.ps1",
		},
		{
			name:            "bash uninstall",
			query:           "mode=uninstall",
			wantSubstring:   "uninstall",
			contentType:     "text/x-shellscript",
			wantDisposition: "uninstall-agent.sh",
		},
		{
			name:            "powershell uninstall",
			query:           "platform=windows&mode=uninstall",
			wantSubstring:   "controlone-agent.exe",
			contentType:     "text/plain; charset=utf-8",
			wantDisposition: "uninstall-agent.ps1",
		},
	}

	srv := newAgentTestServer(t, t.TempDir(), "", "")

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/install-script?"+tc.query, nil)
			rec := httptest.NewRecorder()
			srv.handleAgentInstallScript(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if ct := rec.Header().Get("Content-Type"); ct != tc.contentType {
				t.Fatalf("Content-Type = %q, want %q", ct, tc.contentType)
			}
			if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, tc.wantDisposition) {
				t.Fatalf("Content-Disposition = %q, want to contain %q", disp, tc.wantDisposition)
			}
			if body := rec.Body.String(); !strings.Contains(body, tc.wantSubstring) {
				t.Fatalf("body missing %q; got first 200 chars: %s", tc.wantSubstring, firstN(body, 200))
			}
		})
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
