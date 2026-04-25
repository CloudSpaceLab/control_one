package mfa

import (
	"testing"
	"time"
)

func TestCodeStable(t *testing.T) {
	// RFC 6238 test vector: SHA1 key "12345678901234567890" (ASCII) at time 59s.
	secret := []byte("12345678901234567890")
	got := Code(secret, 1) // counter=1 since 59/30 = 1
	if len(got) != 6 {
		t.Fatalf("code should be 6 digits: %q", got)
	}
	if got != "287082" {
		t.Fatalf("RFC 6238 vector mismatch at T=59 counter=1: got %q, want %q", got, "287082")
	}
}

func TestVerifyAcceptsSkew(t *testing.T) {
	secret, err := GenerateSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code := Code(secret, uint64(now.Unix()/30))
	if !Verify(secret, code, now) {
		t.Fatal("current code should verify")
	}
	// 25s earlier should still verify (within skew=1 step)
	if !Verify(secret, code, now.Add(-25*time.Second)) {
		t.Fatal("code within skew window should verify")
	}
	// 90s earlier must not verify (outside skew)
	if Verify(secret, code, now.Add(-120*time.Second)) {
		t.Fatal("code outside skew window should not verify")
	}
}

func TestVerifyRejectsBadLength(t *testing.T) {
	secret, _ := GenerateSecret()
	if Verify(secret, "12345", time.Now()) {
		t.Fatal("5-digit code must reject")
	}
	if Verify(secret, "1234567", time.Now()) {
		t.Fatal("7-digit code must reject")
	}
}

func TestProvisioningURI(t *testing.T) {
	secret := []byte("sekretsekret20bytes!")
	uri := ProvisioningURI("ControlOne", "alice@example.com", secret)
	if len(uri) < 30 {
		t.Fatal("uri too short")
	}
	if uri[:len("otpauth://totp/")] != "otpauth://totp/" {
		t.Fatalf("wrong scheme: %q", uri)
	}
}
