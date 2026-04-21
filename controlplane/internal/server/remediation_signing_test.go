package server

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

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

// writeCAKeyPair writes an ECDSA P-256 key + self-signed certificate to dir
// and returns their paths. Mirrors writeTestCA from cert_rotate_test.go but
// returns the private key as well so the test can compare the derived vs.
// file-loaded keys.
func writeCAKeyPair(t *testing.T, dir string) (certPath, keyPath string, priv *ecdsa.PrivateKey) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "controlone-signing-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	certPath = filepath.Join(dir, "ca.crt")
	keyPath = filepath.Join(dir, "ca.key")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
	return certPath, keyPath, priv
}

func newSigningServer(t *testing.T, caCertPath, caKeyPath string) *Server {
	t.Helper()
	return &Server{
		logger: zap.NewNop(),
		cfg: &config.Config{
			Enrollment: config.EnrollmentConfig{
				CAKeyFile:  caKeyPath,
				CACertFile: caCertPath,
			},
		},
	}
}

func TestRemediationSignerRoundTripWithCPCAKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath, keyPath, priv := writeCAKeyPair(t, dir)

	srv := newSigningServer(t, certPath, keyPath)
	signer := srv.remediationScriptSigner()
	require.NotNil(t, signer, "signer must be available when CA key is configured")

	sig, alg, err := signer("content", "linux", 2)
	require.NoError(t, err)
	require.NotEmpty(t, sig)
	require.Equal(t, remediation.SignatureAlgorithmECDSAP256SHA256, alg)

	// The signature must verify with the matching CA cert public key that
	// the Server hands to the engine.
	verifyKey := srv.remediationVerifyKey()
	require.NotNil(t, verifyKey)
	require.NoError(t, remediation.Verify(verifyKey, "content", "linux", 2, sig, alg))

	// And the derived public key matches the private key's public half.
	require.Equal(t, priv.PublicKey.X, verifyKey.X)
	require.Equal(t, priv.PublicKey.Y, verifyKey.Y)
}

func TestRemediationSignerMissingCAReturnsNil(t *testing.T) {
	t.Parallel()
	srv := &Server{
		logger: zap.NewNop(),
		cfg: &config.Config{
			Enrollment: config.EnrollmentConfig{},
		},
	}
	require.Nil(t, srv.remediationScriptSigner(), "no CA key => no signer (unsigned dev mode)")
	require.Nil(t, srv.remediationVerifyKey(), "no CA cert => no verify key")
}

func TestRemediationSignerKeyOnlyDerivesVerifyKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, keyPath, priv := writeCAKeyPair(t, dir)

	// Omit the cert path: loading should still succeed via derivation.
	srv := &Server{
		logger: zap.NewNop(),
		cfg: &config.Config{
			Enrollment: config.EnrollmentConfig{
				CAKeyFile: keyPath,
			},
		},
	}
	signer := srv.remediationScriptSigner()
	require.NotNil(t, signer)
	verifyKey := srv.remediationVerifyKey()
	require.NotNil(t, verifyKey)
	require.Equal(t, priv.PublicKey.X, verifyKey.X)
}
