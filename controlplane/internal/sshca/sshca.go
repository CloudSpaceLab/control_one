// Package sshca provides a minimal SSH certificate authority. It generates
// ed25519 key pairs for a tenant and signs short-lived user certificates that
// can be used to connect to nodes whose sshd trusts the tenant's CA public key.
//
// This package is intentionally small: it covers key generation, cert signing,
// and serialization. Persistence is delegated to storage.Store. Encryption at
// rest uses the same secretbox sealer that wraps provider credentials.
package sshca

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// Keypair holds a freshly generated CA key.
type Keypair struct {
	PublicKey              ssh.PublicKey
	PrivateKey             ed25519.PrivateKey
	PublicKeyAuthorizedKey string // OpenSSH authorized_keys line
}

// Generate returns a new ed25519 keypair suitable for use as an SSH CA.
func Generate() (*Keypair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("wrap ssh public key: %w", err)
	}
	return &Keypair{
		PublicKey:              sshPub,
		PrivateKey:             priv,
		PublicKeyAuthorizedKey: string(ssh.MarshalAuthorizedKey(sshPub)),
	}, nil
}

// SignUserCertParams captures the inputs needed to sign a user certificate.
type SignUserCertParams struct {
	CAPrivate  ed25519.PrivateKey
	UserPubKey ssh.PublicKey
	KeyID      string   // human identifier (e.g. "alice@access-request-abc")
	Principals []string // SSH login names the cert is valid for
	Serial     uint64
	ValidFor   time.Duration // capped at 24h
	Extensions map[string]string
}

// SignUserCert returns the cert in authorized_keys wire format (base64 cert
// with leading "ssh-ed25519-cert-v01@openssh.com " prefix).
func SignUserCert(p SignUserCertParams) (ssh.PublicKey, error) {
	if p.CAPrivate == nil || p.UserPubKey == nil {
		return nil, errors.New("ca_private and user_pub_key required")
	}
	if len(p.Principals) == 0 {
		return nil, errors.New("principals required")
	}
	if p.ValidFor <= 0 {
		p.ValidFor = 30 * time.Minute
	}
	if p.ValidFor > 24*time.Hour {
		p.ValidFor = 24 * time.Hour
	}
	if p.Extensions == nil {
		p.Extensions = map[string]string{
			"permit-pty": "",
		}
	}

	signer, err := ssh.NewSignerFromKey(p.CAPrivate)
	if err != nil {
		return nil, fmt.Errorf("ssh signer: %w", err)
	}

	now := time.Now().UTC()
	cert := &ssh.Certificate{
		Key:             p.UserPubKey,
		CertType:        ssh.UserCert,
		KeyId:           p.KeyID,
		ValidPrincipals: p.Principals,
		Serial:          p.Serial,
		ValidAfter:      uint64(now.Add(-30 * time.Second).Unix()),
		ValidBefore:     uint64(now.Add(p.ValidFor).Unix()),
		Permissions: ssh.Permissions{
			Extensions: p.Extensions,
		},
	}
	if err := cert.SignCert(rand.Reader, signer); err != nil {
		return nil, fmt.Errorf("sign cert: %w", err)
	}
	return cert, nil
}

// MarshalCert returns the OpenSSH authorized_keys representation of a cert.
func MarshalCert(cert ssh.PublicKey) string {
	return string(ssh.MarshalAuthorizedKey(cert))
}
