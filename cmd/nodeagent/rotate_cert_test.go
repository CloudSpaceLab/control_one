package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCertWithNotAfter drops a PEM-encoded cert with the supplied expiry into
// path so shouldRotateCert can read it.
func writeCertWithNotAfter(t *testing.T, path string, notAfter time.Time) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test-agent"},
		NotBefore:             notAfter.Add(-24 * time.Hour),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
}

func TestShouldRotateCertTrueWithinGrace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	now := time.Now()
	// NotAfter 10 days out, grace = 30d → should rotate.
	writeCertWithNotAfter(t, certPath, now.Add(10*24*time.Hour))
	if !shouldRotateCert(certPath, 30*24*time.Hour, now) {
		t.Fatalf("expected rotation to be required when cert expiry is inside grace window")
	}
}

func TestShouldRotateCertFalseOutsideGrace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "client.crt")
	now := time.Now()
	// NotAfter 60 days out, grace = 30d → should NOT rotate.
	writeCertWithNotAfter(t, certPath, now.Add(60*24*time.Hour))
	if shouldRotateCert(certPath, 30*24*time.Hour, now) {
		t.Fatalf("expected rotation to be skipped when cert expiry is outside grace window")
	}
}

func TestShouldRotateCertUnreadableReturnsTrue(t *testing.T) {
	t.Parallel()
	if shouldRotateCert("/does/not/exist/client.crt", 30*24*time.Hour, time.Now()) == false {
		t.Fatalf("expected rotation to be requested when cert file is missing")
	}
}

func TestResolveRotateCertOptionsAutoDiscovery(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	dataDir := t.TempDir()

	// Drop a minimal nodeagent.yaml so API URL discovery works.
	if err := os.WriteFile(filepath.Join(configDir, "nodeagent.yaml"),
		[]byte("api_url: https://cp.example.com\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	// Drop state.json so node_id discovery works.
	if err := os.WriteFile(filepath.Join(dataDir, "state.json"),
		[]byte(`{"node_id":"00000000-0000-0000-0000-000000000001"}`), 0o600); err != nil {
		t.Fatalf("write state: %v", err)
	}

	opts, err := resolveRotateCertOptions(rotateCertOptions{}, configDir, dataDir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if opts.APIURL != "https://cp.example.com" {
		t.Fatalf("api url = %q", opts.APIURL)
	}
	if opts.NodeID != "00000000-0000-0000-0000-000000000001" {
		t.Fatalf("node id = %q", opts.NodeID)
	}
	wantCert := filepath.Join(dataDir, "certs", "client.crt")
	if opts.CertFile != wantCert {
		t.Fatalf("cert path = %q, want %q", opts.CertFile, wantCert)
	}
}

func TestResolveRotateCertOptionsRequiresAPIURL(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	// Intentionally do not write a config — discovery must fail.
	_, err := resolveRotateCertOptions(rotateCertOptions{NodeID: "n"}, configDir, dataDir)
	if err == nil {
		t.Fatalf("expected error when api url cannot be discovered")
	}
}
