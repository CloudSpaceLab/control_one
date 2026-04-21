package remediation

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func newTestCAKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	return k
}

func TestSignVerifyRoundTrip(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)

	content := "#!/bin/bash\nsystemctl disable telnet\n"
	sig, alg, err := Sign(priv, content, "linux", 3)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if sig == "" {
		t.Fatal("sign returned empty signature")
	}
	if alg != SignatureAlgorithmECDSAP256SHA256 {
		t.Fatalf("unexpected algorithm: %s", alg)
	}

	if err := Verify(&priv.PublicKey, content, "linux", 3, sig, alg); err != nil {
		t.Fatalf("verify round-trip: %v", err)
	}
}

func TestSignDeterministicAlgorithm(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	sig, alg, err := Sign(priv, "x", "all", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if alg != SignatureAlgorithmECDSAP256SHA256 {
		t.Fatalf("algorithm mismatch: got %s", alg)
	}
	if _, err := base64.StdEncoding.DecodeString(sig); err != nil {
		t.Fatalf("signature not base64: %v", err)
	}
}

func TestVerifyRejectsTamperedContent(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	sig, alg, err := Sign(priv, "original", "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	err = Verify(&priv.PublicKey, "tampered", "linux", 1, sig, alg)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch, got: %v", err)
	}
}

func TestVerifyRejectsPlatformMismatch(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	sig, alg, err := Sign(priv, "payload", "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Same content, different platform — must not verify.
	err = Verify(&priv.PublicKey, "payload", "windows", 1, sig, alg)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch on platform swap, got: %v", err)
	}
}

func TestVerifyRejectsVersionMismatch(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	sig, alg, err := Sign(priv, "payload", "linux", 5)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	// Same content + platform, bumped version — signature must not validate,
	// preventing version-rollback attacks.
	err = Verify(&priv.PublicKey, "payload", "linux", 6, sig, alg)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch on version swap, got: %v", err)
	}
}

func TestVerifyRejectsWrongCAKey(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	other := newTestCAKey(t)

	sig, alg, err := Sign(priv, "payload", "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	err = Verify(&other.PublicKey, "payload", "linux", 1, sig, alg)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("expected ErrSignatureMismatch for wrong CA, got: %v", err)
	}
}

func TestVerifyMissingSignature(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	if err := Verify(&priv.PublicKey, "x", "linux", 1, "", SignatureAlgorithmECDSAP256SHA256); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("expected ErrMissingSignature, got: %v", err)
	}
	// Empty algorithm also treated as missing.
	sig, _, err := Sign(priv, "x", "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := Verify(&priv.PublicKey, "x", "linux", 1, sig, ""); !errors.Is(err, ErrMissingSignature) {
		t.Fatalf("expected ErrMissingSignature on empty alg, got: %v", err)
	}
}

func TestVerifyUnsupportedAlgorithm(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	sig, _, err := Sign(priv, "x", "linux", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	err = Verify(&priv.PublicKey, "x", "linux", 1, sig, "rsa-pss-sha512")
	if !errors.Is(err, ErrUnsupportedSignatureAlgorithm) {
		t.Fatalf("expected ErrUnsupportedSignatureAlgorithm, got: %v", err)
	}
}

func TestVerifyCorruptedBase64(t *testing.T) {
	t.Parallel()
	priv := newTestCAKey(t)
	err := Verify(&priv.PublicKey, "x", "linux", 1, "!!!not-base64!!!", SignatureAlgorithmECDSAP256SHA256)
	if err == nil || !strings.Contains(err.Error(), "decode signature") {
		t.Fatalf("expected decode signature error, got: %v", err)
	}
}

func TestCanonicalInputNormalizesPlatform(t *testing.T) {
	t.Parallel()
	// Empty platform should normalize to "all" on both sides.
	priv := newTestCAKey(t)
	sig, alg, err := Sign(priv, "content", "", 1)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := Verify(&priv.PublicKey, "content", "all", 1, sig, alg); err != nil {
		t.Fatalf("verify with explicit 'all' after signing with empty: %v", err)
	}
	if err := Verify(&priv.PublicKey, "content", "   ", 1, sig, alg); err != nil {
		t.Fatalf("verify with whitespace platform: %v", err)
	}
}

func TestSignRequiresPrivateKey(t *testing.T) {
	t.Parallel()
	if _, _, err := Sign(nil, "x", "all", 1); err == nil {
		t.Fatal("expected error when signing with nil private key")
	}
}

func TestVerifyRequiresPublicKey(t *testing.T) {
	t.Parallel()
	err := Verify(nil, "x", "all", 1, "sig", SignatureAlgorithmECDSAP256SHA256)
	if err == nil || !strings.Contains(err.Error(), "public key") {
		t.Fatalf("expected public key error, got: %v", err)
	}
}
