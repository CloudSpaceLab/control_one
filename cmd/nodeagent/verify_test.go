package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// genTestKeyPair returns an ed25519 keypair plus a PEM-encoded copy of the
// public key written to disk under tmpDir. The returned pubPath is suitable
// for passing to verify-binary's --public-key / --public-key-file flags.
func genTestKeyPair(t *testing.T, tmpDir string) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	pubPath := filepath.Join(tmpDir, "pub.pem")
	if err := os.WriteFile(pubPath, pemBytes, 0o600); err != nil {
		t.Fatalf("write pubkey: %v", err)
	}
	return priv, pubPath
}

// signBinary writes random bytes to a file under tmpDir, signs sha256(file)
// with priv, and returns (binaryPath, base64-signature).
func signBinary(t *testing.T, tmpDir string, priv ed25519.PrivateKey) (string, string) {
	t.Helper()
	binaryPath := filepath.Join(tmpDir, "agent")
	body := []byte("fake-agent-binary-contents")
	if err := os.WriteFile(binaryPath, body, 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	digest := sha256.Sum256(body)
	sig := ed25519.Sign(priv, digest[:])
	return binaryPath, base64.StdEncoding.EncodeToString(sig)
}

// TestVerifyBinary_PublicKeyFileFlag asserts that --public-key-file is
// accepted as a synonym for --public-key. This is the contract the installer
// (and any pinned-key flow) depends on.
func TestVerifyBinary_PublicKeyFileFlag(t *testing.T) {
	tmpDir := t.TempDir()
	priv, pubPath := genTestKeyPair(t, tmpDir)
	binaryPath, sigB64 := signBinary(t, tmpDir, priv)

	if err := runVerifyBinary([]string{
		"--binary", binaryPath,
		"--signature", sigB64,
		"--public-key-file", pubPath,
	}); err != nil {
		t.Fatalf("--public-key-file should verify a valid signature: %v", err)
	}
}

// TestVerifyBinary_LegacyPublicKeyFlag asserts the older --public-key spelling
// still works. We don't want to break anything that already passes it.
func TestVerifyBinary_LegacyPublicKeyFlag(t *testing.T) {
	tmpDir := t.TempDir()
	priv, pubPath := genTestKeyPair(t, tmpDir)
	binaryPath, sigB64 := signBinary(t, tmpDir, priv)

	if err := runVerifyBinary([]string{
		"--binary", binaryPath,
		"--signature", sigB64,
		"--public-key", pubPath,
	}); err != nil {
		t.Fatalf("--public-key should still verify a valid signature: %v", err)
	}
}

// TestVerifyBinary_BothKeyFlagsAgree: when both spellings point at the same
// file we accept the call. Wrappers that defensively pass both shouldn't be
// punished.
func TestVerifyBinary_BothKeyFlagsAgree(t *testing.T) {
	tmpDir := t.TempDir()
	priv, pubPath := genTestKeyPair(t, tmpDir)
	binaryPath, sigB64 := signBinary(t, tmpDir, priv)

	if err := runVerifyBinary([]string{
		"--binary", binaryPath,
		"--signature", sigB64,
		"--public-key", pubPath,
		"--public-key-file", pubPath,
	}); err != nil {
		t.Fatalf("matching --public-key and --public-key-file should verify: %v", err)
	}
}

// TestVerifyBinary_KeyFlagsConflict: when the two spellings disagree we
// refuse rather than silently picking one. This is a usage error.
func TestVerifyBinary_KeyFlagsConflict(t *testing.T) {
	tmpDir := t.TempDir()
	_, pubPath := genTestKeyPair(t, tmpDir)
	binaryPath := filepath.Join(tmpDir, "agent")
	if err := os.WriteFile(binaryPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	otherPath := filepath.Join(tmpDir, "other.pem")
	if err := os.WriteFile(otherPath, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("write other: %v", err)
	}

	err := runVerifyBinary([]string{
		"--binary", binaryPath,
		"--signature", base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
		"--public-key", pubPath,
		"--public-key-file", otherPath,
	})
	if err == nil {
		t.Fatal("expected error when --public-key and --public-key-file disagree")
	}
	if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("error should mention conflict, got: %v", err)
	}
}

// TestVerifyBinary_NoKeySource: when neither --public-key, --public-key-file,
// nor --public-key-url is supplied we error out cleanly.
func TestVerifyBinary_NoKeySource(t *testing.T) {
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "agent")
	if err := os.WriteFile(binaryPath, []byte("x"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	err := runVerifyBinary([]string{
		"--binary", binaryPath,
		"--signature", base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)),
	})
	if err == nil {
		t.Fatal("expected error when no public-key source supplied")
	}
}
