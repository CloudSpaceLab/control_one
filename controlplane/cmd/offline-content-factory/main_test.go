package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
)

func TestOfflineContentFactoryBuildsSelfVerifyingBundle(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	dir := t.TempDir()
	contentRoot := filepath.Join(dir, "content-root")
	if err := os.MkdirAll(filepath.Join(contentRoot, "content"), 0o755); err != nil {
		t.Fatalf("mkdir content root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(contentRoot, "content", "feed.json"), []byte(`{"schema_version":1}`), 0o644); err != nil {
		t.Fatalf("write content: %v", err)
	}
	manifest := offlinebundle.Manifest{
		SchemaVersion: 1,
		BundleID:      "factory-test",
		Version:       "2026.05.29",
		Sequence:      1,
		IssuedAt:      time.Date(2026, 5, 29, 10, 0, 0, 0, time.UTC),
		ExpiresAt:     time.Now().UTC().Add(24 * time.Hour),
		Contents: []offlinebundle.ContentFile{{
			Type:    "ip_enrichment",
			Name:    "default",
			Version: "2026.05.29",
			Path:    "content/feed.json",
		}},
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	keyPath := writeFactoryTestPrivateKey(t, dir, priv)
	outPath := filepath.Join(dir, "bundle.tar.gz")

	var stdout bytes.Buffer
	if err := run([]string{
		"--manifest", manifestPath,
		"--content-root", contentRoot,
		"--private-key", keyPath,
		"--out", outPath,
		"--print-public-key",
	}, &stdout, ioDiscard{}); err != nil {
		t.Fatalf("run factory: %v", err)
	}
	if !strings.Contains(stdout.String(), "public_key_fingerprint") || !strings.Contains(stdout.String(), "BEGIN PUBLIC KEY") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}

	bundle, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer func() { _ = bundle.Close() }()
	verified, err := offlinebundle.VerifyArchive(bundle, offlinebundle.ImportOptions{PublicKey: pub, Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("verify output: %v", err)
	}
	if verified.Manifest.Contents[0].SHA256 == "" {
		t.Fatalf("factory did not stamp content sha: %+v", verified.Manifest.Contents[0])
	}

	rawManifest := readBundleMember(t, outPath, offlinebundle.ManifestPath)
	rawSig := readBundleMember(t, outPath, offlinebundle.SignaturePath)
	sig, err := base64.StdEncoding.DecodeString(string(rawSig))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(pub, rawManifest, sig) {
		t.Fatal("signature does not verify against archived manifest bytes")
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func writeFactoryTestPrivateKey(t *testing.T, dir string, priv ed25519.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	path := filepath.Join(dir, "offline-content.key")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	return path
}

func readBundleMember(t *testing.T, bundlePath, name string) []byte {
	t.Helper()
	f, err := os.Open(bundlePath)
	if err != nil {
		t.Fatalf("open bundle: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("open gzip: %v", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if hdr.Name != name {
			continue
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(tr); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return buf.Bytes()
	}
}
