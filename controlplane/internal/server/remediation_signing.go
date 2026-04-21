package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/remediation"
)

// remediationSigning bundles the CP CA keys used to sign remediation scripts
// on write and verify them on exec. The private key is used by the storage
// layer signer; the public key is embedded into the remediation engine so
// every worker-driven script exec re-verifies before spawning the process.
//
// Both are loaded once from cfg.Enrollment.{CAKeyFile,CACertFile} at first
// use; if either file is missing the fields stay nil and the CP runs in
// "unsigned" dev mode. Production configs with `enrollment.ca_*_file` set
// will always produce real signatures.
type remediationSigning struct {
	privateKey *ecdsa.PrivateKey
	publicKey  *ecdsa.PublicKey
	loadErr    error
}

// remediationSigningMaterial lazily loads and caches the CP CA keys.
func (s *Server) remediationSigningMaterial() *remediationSigning {
	s.remediationSigningOnce.Do(func() {
		s.remediationSigning = s.loadRemediationSigning()
	})
	return s.remediationSigning
}

func (s *Server) loadRemediationSigning() *remediationSigning {
	out := &remediationSigning{}

	keyPath := strings.TrimSpace(s.cfg.Enrollment.CAKeyFile)
	certPath := strings.TrimSpace(s.cfg.Enrollment.CACertFile)

	if keyPath == "" && certPath == "" {
		s.logger.Warn("remediation signing disabled: enrollment CA key + cert not configured; scripts will be written unsigned and executed without verification")
		return out
	}

	if keyPath != "" {
		priv, err := loadECDSAPrivateKey(keyPath)
		if err != nil {
			s.logger.Warn("remediation signing: CA key load failed; scripts will be written unsigned",
				zap.String("path", keyPath), zap.Error(err))
			out.loadErr = err
		} else {
			out.privateKey = priv
		}
	}

	if certPath != "" {
		pub, err := loadECDSAPublicKeyFromCert(certPath)
		if err != nil {
			s.logger.Warn("remediation signing: CA cert load failed; engine will run with verification disabled",
				zap.String("path", certPath), zap.Error(err))
			if out.loadErr == nil {
				out.loadErr = err
			}
		} else {
			out.publicKey = pub
		}
	}

	// If we have a private key but no public key file, derive the public key
	// from the private — the CA-signed cert is the canonical source but the
	// matching public key is always available on the key pair itself.
	if out.privateKey != nil && out.publicKey == nil {
		out.publicKey = &out.privateKey.PublicKey
	}

	if out.privateKey != nil && out.publicKey != nil {
		s.logger.Info("remediation signing active: scripts will be signed on write and verified on exec")
	}
	return out
}

// remediationScriptSigner returns a ScriptSignerFunc bound to the CP CA key.
// Nil when the CA key is not available, which lets the storage layer write
// unsigned rows rather than rejecting every create call during dev.
func (s *Server) remediationScriptSigner() storage.ScriptSignerFunc {
	mat := s.remediationSigningMaterial()
	if mat == nil || mat.privateKey == nil {
		return nil
	}
	return func(content, platform string, version int) (string, string, error) {
		return remediation.Sign(mat.privateKey, content, platform, version)
	}
}

// remediationVerifyKey returns the CP CA public key used by the remediation
// engine before exec. Nil in unsigned dev mode.
func (s *Server) remediationVerifyKey() *ecdsa.PublicKey {
	mat := s.remediationSigningMaterial()
	if mat == nil {
		return nil
	}
	return mat.publicKey
}

// backfillRemediationSignatures signs any pre-Sprint-3 rows whose signature
// is still NULL. Safe to call repeatedly; unsigned rows are only produced
// when the CA key is unavailable so the signer call is a no-op in that case.
func (s *Server) backfillRemediationSignatures(ctx context.Context) {
	if s.store == nil {
		return
	}
	signer := s.remediationScriptSigner()
	if signer == nil {
		return
	}
	concrete, ok := s.store.(interface {
		BackfillRemediationScriptSignatures(context.Context, storage.ScriptSignerFunc) (int, error)
	})
	if !ok {
		s.logger.Debug("store does not support remediation signature backfill; skipping")
		return
	}
	signed, err := concrete.BackfillRemediationScriptSignatures(ctx, signer)
	if err != nil {
		s.logger.Warn("remediation signature backfill failed", zap.Error(err))
		return
	}
	if signed > 0 {
		s.logger.Info("remediation signature backfill complete", zap.Int("signed", signed))
	}
}

// loadECDSAPrivateKey parses a PEM-encoded ECDSA private key in either
// SEC1 ("EC PRIVATE KEY") or PKCS#8 form. Rejects RSA/ED25519 so signature
// algorithm stays consistent with the enrollment CA.
func loadECDSAPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path) // #nosec G304 — admin-supplied path
	if err != nil {
		return nil, fmt.Errorf("read ca key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("decode ca key PEM: no PEM block")
	}
	if ec, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return ec, nil
	}
	if pk8, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if ec, ok := pk8.(*ecdsa.PrivateKey); ok {
			return ec, nil
		}
		return nil, errors.New("ca private key is not ECDSA")
	}
	return nil, errors.New("unsupported CA private key encoding")
}

// loadECDSAPublicKeyFromCert reads a PEM-encoded X.509 certificate and
// returns its ECDSA public key.
func loadECDSAPublicKeyFromCert(path string) (*ecdsa.PublicKey, error) {
	data, err := os.ReadFile(path) // #nosec G304 — admin-supplied path
	if err != nil {
		return nil, fmt.Errorf("read ca cert: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("decode ca cert PEM: no PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse ca cert: %w", err)
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, errors.New("ca cert public key is not ECDSA")
	}
	return pub, nil
}

