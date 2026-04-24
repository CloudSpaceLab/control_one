// Package secretbox provides authenticated symmetric encryption for provider
// credentials at rest. Ciphertext and nonce are stored separately in the
// database; a single 32-byte key (AES-256-GCM) is held in process memory and
// loaded from config.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Sealer seals plaintext and unseals ciphertext using AES-256-GCM.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer returns a Sealer backed by the provided 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secretbox key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm aead: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// NewSealerFromConfig accepts hex- or raw-encoded key material. A blank value
// returns (nil, nil) so callers can decide whether to fall back to plaintext
// (dev) or refuse to start (prod).
func NewSealerFromConfig(raw string) (*Sealer, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if len(raw) == 64 { // hex
		key, err := hex.DecodeString(raw)
		if err != nil {
			return nil, fmt.Errorf("decode hex key: %w", err)
		}
		return NewSealer(key)
	}
	return NewSealer([]byte(raw))
}

// Seal encrypts plaintext and returns (ciphertext, nonce, error).
// The nonce is randomly generated per call.
func (s *Sealer) Seal(plaintext []byte) ([]byte, []byte, error) {
	if s == nil {
		return nil, nil, errors.New("secretbox sealer not configured")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := s.aead.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Open decrypts ciphertext using the supplied nonce.
func (s *Sealer) Open(ciphertext, nonce []byte) ([]byte, error) {
	if s == nil {
		return nil, errors.New("secretbox sealer not configured")
	}
	if len(nonce) != s.aead.NonceSize() {
		return nil, fmt.Errorf("nonce length mismatch: want %d got %d", s.aead.NonceSize(), len(nonce))
	}
	plaintext, err := s.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}
