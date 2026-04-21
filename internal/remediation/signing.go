package remediation

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// SignatureAlgorithmECDSAP256SHA256 is the canonical algorithm identifier for
// signatures produced by the CP CA key (ECDSA P-256 + SHA-256). New signers
// must set this verbatim; verifiers reject unknown algorithms to prevent
// downgrade attacks.
const SignatureAlgorithmECDSAP256SHA256 = "ecdsa-p256-sha256"

// ErrMissingSignature is returned by Verify when the script has no signature
// attached but verification is required.
var ErrMissingSignature = errors.New("remediation: script signature missing")

// ErrUnsupportedSignatureAlgorithm is returned by Verify when the script's
// algorithm identifier is not one we know how to check.
var ErrUnsupportedSignatureAlgorithm = errors.New("remediation: unsupported signature algorithm")

// ErrSignatureMismatch is returned by Verify when the signature does not
// match the canonical bytes of (content, platform, version).
var ErrSignatureMismatch = errors.New("remediation: script signature verification failed")

// CanonicalSigningInput returns the bytes that are hashed and signed for a
// given remediation script. Both signer and verifier MUST use this exact
// function so the two sides compute the same digest. The canonical form is
// the content, platform, and integer version joined by newline — the same
// (content, platform, version) tuple that uniquely identifies a script row
// in the database (rule_id is a stable key but not part of the signed payload
// so rule renames do not invalidate shipped signatures).
func CanonicalSigningInput(content, platform string, version int) []byte {
	var b strings.Builder
	b.WriteString(content)
	b.WriteByte('\n')
	b.WriteString(normalizePlatform(platform))
	b.WriteByte('\n')
	b.WriteString(strconv.Itoa(version))
	return []byte(b.String())
}

// normalizePlatform mirrors storage.CreateRemediationScript which defaults an
// empty platform to "all". Keeping the logic here means signatures survive
// round-trips even if callers forget to set the default themselves.
func normalizePlatform(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "all"
	}
	return p
}

// Sign produces a base64 DER-encoded ECDSA signature over the SHA-256 digest
// of the canonical signing input. The signer MUST use the CP CA private key
// so node agents (and the worker that executes on their behalf) can verify
// against the embedded CP CA public key.
func Sign(priv *ecdsa.PrivateKey, content, platform string, version int) (string, string, error) {
	if priv == nil {
		return "", "", errors.New("remediation: sign requires a private key")
	}
	digest := sha256.Sum256(CanonicalSigningInput(content, platform, version))
	derSig, err := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	if err != nil {
		return "", "", fmt.Errorf("remediation: sign script: %w", err)
	}
	return base64.StdEncoding.EncodeToString(derSig), SignatureAlgorithmECDSAP256SHA256, nil
}

// Verify checks a base64 DER-encoded ECDSA signature against the canonical
// digest of (content, platform, version) using the CP CA public key. The
// algorithm identifier is required to match the expected alg exactly —
// unrecognized algs are refused so a future alg upgrade cannot be downgraded
// by a malicious CP-side writer.
func Verify(pub *ecdsa.PublicKey, content, platform string, version int, signature, algorithm string) error {
	if pub == nil {
		return errors.New("remediation: verify requires a public key")
	}
	if strings.TrimSpace(signature) == "" {
		return ErrMissingSignature
	}
	alg := strings.TrimSpace(algorithm)
	if alg == "" {
		// Empty alg means "legacy unsigned row pre-Sprint-3" — treat as missing
		// so callers can decide whether to fail closed or open.
		return ErrMissingSignature
	}
	if alg != SignatureAlgorithmECDSAP256SHA256 {
		return fmt.Errorf("%w: %s", ErrUnsupportedSignatureAlgorithm, alg)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("remediation: decode signature: %w", err)
	}

	digest := sha256.Sum256(CanonicalSigningInput(content, platform, version))
	if !ecdsa.VerifyASN1(pub, digest[:], sigBytes) {
		return ErrSignatureMismatch
	}
	return nil
}
