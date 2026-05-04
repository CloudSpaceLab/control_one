package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"time"

	"go.uber.org/zap"
)

// generateClientCertificate creates a TLS client certificate for a node.
// If CA cert/key files are configured, it signs against the CA. Otherwise it
// falls back to generating a self-signed certificate.
func (s *Server) generateClientCertificate(hostname, nodeID string) (certPEM, keyPEM, caCertPEM []byte, err error) {
	caKeyFile := s.cfg.Enrollment.CAKeyFile
	caCertFile := s.cfg.Enrollment.CACertFile

	if caKeyFile != "" && caCertFile != "" {
		return s.generateCASignedClientCert(caCertFile, caKeyFile, hostname, nodeID)
	}

	s.logger.Warn("enrollment CA not configured, generating self-signed client certificate",
		zap.String("hostname", hostname),
		zap.String("node_id", nodeID),
	)
	return generateSelfSignedClientCert(hostname, nodeID)
}

func (s *Server) generateCASignedClientCert(caCertFile, caKeyFile, hostname, nodeID string) (certPEM, keyPEM, caCertPEM []byte, err error) {
	// Load CA certificate
	caCertBytes, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read CA cert file: %w", err)
	}
	caCertBlock, _ := pem.Decode(caCertBytes)
	if caCertBlock == nil {
		return nil, nil, nil, fmt.Errorf("decode CA cert PEM: no PEM block found")
	}
	caCert, err := x509.ParseCertificate(caCertBlock.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse CA certificate: %w", err)
	}

	// Load CA key
	caKeyBytes, err := os.ReadFile(caKeyFile)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read CA key file: %w", err)
	}
	caKeyBlock, _ := pem.Decode(caKeyBytes)
	if caKeyBlock == nil {
		return nil, nil, nil, fmt.Errorf("decode CA key PEM: no PEM block found")
	}

	caKey, err := parsePrivateKey(caKeyBlock)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("parse CA private key: %w", err)
	}

	// Generate client ECDSA P-256 key
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate client key: %w", err)
	}

	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		serial = big.NewInt(now.UnixNano())
	}

	// CN must be the node UUID — the heartbeat handler enforces
	// strings.EqualFold(cn, nodeID.String()) so a node-A cert can't
	// touch node-B's row. Hostname goes into the Organization for
	// operator readability.
	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			// CN must match the node UUID so the server's heartbeat handler can
			// validate principal.Name == nodeID without any CA lookup.
			CommonName:   nodeID,
			Organization: []string{"Control One Agent"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, caCert, &clientKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create client certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyBytes, err := x509.MarshalECPrivateKey(clientKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal client key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	return certPEM, keyPEM, caCertBytes, nil
}

func generateSelfSignedClientCert(hostname, nodeID string) (certPEM, keyPEM, caCertPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate key: %w", err)
	}

	now := time.Now()
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		serial = big.NewInt(now.UnixNano())
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			// CN must match the node UUID — used by the heartbeat handler for
			// identity validation via X-SSL-Client-S-DN (nginx optional_no_ca).
			CommonName:   nodeID,
			Organization: []string{"Control One Agent"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true, // self-signed acts as its own CA
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("create self-signed certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal ecdsa key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})

	// Self-signed: the cert is also the CA cert
	caCertPEM = certPEM

	return certPEM, keyPEM, caCertPEM, nil
}

// parsePrivateKey attempts to parse a PEM block as various private key formats.
func parsePrivateKey(block *pem.Block) (interface{}, error) {
	if key, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("unsupported private key type: %s", block.Type)
}
