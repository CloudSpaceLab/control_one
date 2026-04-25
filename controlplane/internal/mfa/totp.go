// Package mfa implements RFC 6238 TOTP (30-second step, 6-digit code, SHA1).
// This module does not persist anything; storage is handled by the caller.
package mfa

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	defaultDigits = 6
	defaultStep   = 30 * time.Second
	defaultSkew   = 1 // accept one step before and one after
)

// GenerateSecret returns 20 random bytes — the standard size for an RFC 6238
// TOTP secret. Callers should store the raw bytes (sealed) and surface the
// base32 form only during enrolment.
func GenerateSecret() ([]byte, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return nil, fmt.Errorf("generate totp secret: %w", err)
	}
	return buf, nil
}

// SecretToBase32 returns the RFC-standard base32 (no padding) form of a secret.
func SecretToBase32(secret []byte) string {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	return enc.EncodeToString(secret)
}

// ProvisioningURI returns the otpauth:// URL that authenticator apps consume
// via QR scan.
func ProvisioningURI(issuer, accountName string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", SecretToBase32(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	label := url.PathEscape(issuer + ":" + accountName)
	return "otpauth://totp/" + label + "?" + v.Encode()
}

// Code returns the 6-digit code for a specific counter value.
func Code(secret []byte, counter uint64) string {
	mac := hmac.New(sha1.New, secret)
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < defaultDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", defaultDigits, bin%mod)
}

// Verify returns true if the supplied code matches the current step or any
// step within the skew window. Use constant-time comparison to avoid leaking
// how close an attacker's guess was.
func Verify(secret []byte, userCode string, at time.Time) bool {
	code := strings.TrimSpace(userCode)
	if len(code) != defaultDigits {
		return false
	}
	counter := uint64(at.Unix() / int64(defaultStep.Seconds()))
	for skew := -int64(defaultSkew); skew <= int64(defaultSkew); skew++ {
		c := int64(counter) + skew
		if c < 0 {
			continue
		}
		want := Code(secret, uint64(c))
		if hmac.Equal([]byte(want), []byte(code)) {
			return true
		}
	}
	return false
}

// Errors exposed to callers.
var (
	ErrInvalidCode = errors.New("invalid totp code")
)
