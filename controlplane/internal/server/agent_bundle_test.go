package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
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

// newBundleTestServer constructs a Server whose binary dir holds a single fake
// agent binary, whose store recognises a single pre-seeded enrollment token,
// and whose enrollment CA is wired up. Returns the server and the plaintext
// token string so the caller can feed it into the request.
func newBundleTestServer(t *testing.T, osName, archName, tokenHash, plaintextToken string) (*Server, string) {
	t.Helper()

	dir := t.TempDir()
	payload := []byte("offline-fake-agent-binary")
	writeTestBinary(t, dir, osName, archName, payload)

	privPath, pubPath, _ := writeSigningKeyPair(t, dir)

	// A tiny CA PEM lets us exercise the ca.pem bundle entry.
	caCert, caKey := writeTestCA(t, dir)

	store := &fakeStore{
		enrollmentTokens: map[string]storage.EnrollmentToken{
			tokenHash: {
				ID:        uuid.New(),
				TenantID:  uuid.New(),
				Name:      "bundle-token",
				TokenHash: tokenHash,
				MaxNodes:  5,
				ExpiresAt: time.Now().Add(24 * time.Hour),
				CreatedAt: time.Now(),
			},
		},
	}
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Agent: config.AgentConfig{
			BinaryDir:            dir,
			SigningKeyPath:       privPath,
			SigningPublicKeyPath: pubPath,
		},
		Enrollment: config.EnrollmentConfig{
			CACertFile: caCert,
			CAKeyFile:  caKey,
		},
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	return srv, plaintextToken
}

// hashToken re-implements the server-side token hashing to avoid exporting the
// helper just for tests.
func hashToken(t *testing.T, token string) string {
	t.Helper()
	h := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(h[:])
}

// readBundle extracts a tarball from rec.Body and returns a map keyed by entry
// name. We strip the `controlone-bundle/` prefix so assertions can reference
// files by short name.
func readBundle(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	tr := tar.NewReader(gr)
	out := make(map[string][]byte)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar reader: %v", err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read tar entry %s: %v", header.Name, err)
		}
		name := strings.TrimPrefix(header.Name, "controlone-bundle/")
		out[name] = data
	}
	return out
}

func TestAgentBundleRejectsMissingToken(t *testing.T) {
	t.Parallel()

	token := "cot_bundle_aa"
	srv, _ := newBundleTestServer(t, "linux", "amd64", hashToken(t, token), token)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/bundle?os=linux&arch=amd64", nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBundle(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", rec.Code, rec.Body.String())
	}
}

func TestAgentBundleRejectsRevokedToken(t *testing.T) {
	t.Parallel()

	token := "cot_bundle_revoked"
	hash := hashToken(t, token)
	srv, _ := newBundleTestServer(t, "linux", "amd64", hash, token)

	// Manually revoke the seeded token.
	store := srv.store.(*fakeStore)
	store.mu.Lock()
	rec := store.enrollmentTokens[hash]
	rec.RevokedAt = sql.NullTime{Time: time.Now(), Valid: true}
	store.enrollmentTokens[hash] = rec
	store.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/bundle?os=linux&arch=amd64&token="+token, nil)
	rr := httptest.NewRecorder()
	srv.handleAgentBundle(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", rr.Code, rr.Body.String())
	}
}

func TestAgentBundleStreamsTarballWithExpectedEntries(t *testing.T) {
	t.Parallel()

	token := "cot_bundle_happy"
	srv, plaintext := newBundleTestServer(t, "linux", "amd64", hashToken(t, token), token)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/bundle?os=linux&arch=amd64&token="+plaintext, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBundle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("Content-Type = %q, want application/gzip", ct)
	}
	if !strings.HasPrefix(rec.Header().Get("X-ControlOne-Checksum"), "sha256-") {
		t.Fatalf("missing X-ControlOne-Checksum header")
	}
	if format := rec.Header().Get("X-ControlOne-Bundle-Format"); format != "controlone-bundle-v1" {
		t.Fatalf("bundle-format = %q, want controlone-bundle-v1", format)
	}

	entries := readBundle(t, rec.Body.Bytes())

	// Must contain: binary, manifest, config, install script, ca.pem, signature.
	required := []string{"controlone-agent", "manifest.json", "config.yaml", "install-offline.sh", "ca.pem", "controlone-agent.sig"}
	for _, name := range required {
		if _, ok := entries[name]; !ok {
			t.Fatalf("bundle missing entry %q; have %v", name, keysOf(entries))
		}
	}

	// Manifest should validate as JSON and reference the binary's hash.
	var manifest bundleManifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.OS != "linux" || manifest.Arch != "amd64" {
		t.Fatalf("manifest OS/arch = %s/%s", manifest.OS, manifest.Arch)
	}
	if manifest.BundleFormat != "controlone-bundle-v1" {
		t.Fatalf("manifest bundle_format = %q", manifest.BundleFormat)
	}
	if manifest.SHA256 == "" {
		t.Fatalf("manifest sha256 empty")
	}

	// Install script should reference offline-specific entrypoints.
	if !bytes.Contains(entries["install-offline.sh"], []byte("Control One Agent Offline Installer")) {
		t.Fatalf("install-offline.sh missing offline marker")
	}

	// Config should carry the provided token + CP URL.
	if !bytes.Contains(entries["config.yaml"], []byte(plaintext)) {
		t.Fatalf("config.yaml missing embedded token; body=%s", string(entries["config.yaml"]))
	}
}

func TestAgentBundleWindowsYieldsPowerShellInstaller(t *testing.T) {
	t.Parallel()

	token := "cot_bundle_win"
	srv, plaintext := newBundleTestServer(t, "windows", "amd64", hashToken(t, token), token)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/bundle?os=windows&arch=amd64&token="+plaintext, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBundle(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}

	entries := readBundle(t, rec.Body.Bytes())

	if _, ok := entries["install-offline.ps1"]; !ok {
		t.Fatalf("expected install-offline.ps1 entry; have %v", keysOf(entries))
	}
	if _, ok := entries["controlone-agent.exe"]; !ok {
		t.Fatalf("expected controlone-agent.exe entry; have %v", keysOf(entries))
	}
}

func TestAgentBundleRequiresBinaryForRequestedPlatform(t *testing.T) {
	t.Parallel()

	token := "cot_bundle_missing"
	srv, plaintext := newBundleTestServer(t, "linux", "amd64", hashToken(t, token), token)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent/bundle?os=darwin&arch=arm64&token="+plaintext, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBundle(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestAgentBundleOnlyAcceptsGet(t *testing.T) {
	t.Parallel()

	token := "cot_bundle_method"
	srv, plaintext := newBundleTestServer(t, "linux", "amd64", hashToken(t, token), token)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agent/bundle?os=linux&arch=amd64&token="+plaintext, nil)
	rec := httptest.NewRecorder()
	srv.handleAgentBundle(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// keysOf returns a sorted-ish slice of map keys for test error messages.
func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Sanity check: writeTestBinary actually writes into os.TempDir so we notice
// any path drift quickly.
func TestWriteTestBinarySanity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeTestBinary(t, dir, "linux", "amd64", []byte("x"))
	if _, err := os.Stat(filepath.Join(dir, "controlone-agent-linux-amd64")); err != nil {
		t.Fatalf("expected binary at %s: %v", filepath.Join(dir, "controlone-agent-linux-amd64"), err)
	}
}
