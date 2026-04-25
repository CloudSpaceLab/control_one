package sshca

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestGenerate(t *testing.T) {
	kp, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if kp.PublicKey == nil {
		t.Fatal("nil public key")
	}
	if kp.PublicKeyAuthorizedKey == "" {
		t.Fatal("empty authorized_keys line")
	}
}

func TestSignAndVerifyUserCert(t *testing.T) {
	ca, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	userPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	userSSHPub, err := ssh.NewPublicKey(userPub)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := SignUserCert(SignUserCertParams{
		CAPrivate:  ca.PrivateKey,
		UserPubKey: userSSHPub,
		KeyID:      "alice",
		Principals: []string{"alice", "root"},
		Serial:     1,
		ValidFor:   5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Verify using CertChecker.
	checker := ssh.CertChecker{
		IsUserAuthority: func(auth ssh.PublicKey) bool {
			return string(auth.Marshal()) == string(ca.PublicKey.Marshal())
		},
	}
	if err := checker.CheckCert("alice", cert.(*ssh.Certificate)); err != nil {
		t.Fatalf("cert should verify: %v", err)
	}
	if err := checker.CheckCert("bob", cert.(*ssh.Certificate)); err == nil {
		t.Fatal("cert should not verify for unauthorized principal")
	}
}

func TestSignRequiresPrincipals(t *testing.T) {
	ca, _ := Generate()
	userPub, _, _ := ed25519.GenerateKey(rand.Reader)
	userSSHPub, _ := ssh.NewPublicKey(userPub)
	if _, err := SignUserCert(SignUserCertParams{CAPrivate: ca.PrivateKey, UserPubKey: userSSHPub, KeyID: "x"}); err == nil {
		t.Fatal("should reject no principals")
	}
}

func TestSignCapsValidity(t *testing.T) {
	ca, _ := Generate()
	userPub, _, _ := ed25519.GenerateKey(rand.Reader)
	userSSHPub, _ := ssh.NewPublicKey(userPub)
	cert, err := SignUserCert(SignUserCertParams{
		CAPrivate: ca.PrivateKey, UserPubKey: userSSHPub, KeyID: "x",
		Principals: []string{"a"}, ValidFor: 72 * time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	sshCert := cert.(*ssh.Certificate)
	dur := time.Until(time.Unix(int64(sshCert.ValidBefore), 0))
	if dur > 25*time.Hour {
		t.Fatalf("validity not capped: %v", dur)
	}
}
