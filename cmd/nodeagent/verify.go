package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// runVerifyBinary implements the `controlone-agent verify-binary` subcommand
// used by the installer to confirm a downloaded binary before exec. It SHA-256
// hashes the binary, fetches the server's ed25519 public key, then verifies
// the provided signature against the hash.
//
// Flags:
//
//	--binary <path>           path to the binary to verify
//	--signature <b64|flag>    base64-encoded ed25519 signature over sha256(binary)
//	--signature-file <path>   alternative: read signature from file
//	--public-key <path>       alternative to --public-key-url: local PEM file
//	--public-key-url <url>    fetch PEM public key from the control plane
func runVerifyBinary(args []string) error {
	fs := flag.NewFlagSet("verify-binary", flag.ContinueOnError)
	binaryPath := fs.String("binary", "", "path to the binary to verify")
	signature := fs.String("signature", "", "base64 ed25519 signature")
	signatureFile := fs.String("signature-file", "", "path to a file containing the base64 signature")
	publicKeyFile := fs.String("public-key", "", "path to a local PEM-encoded ed25519 public key")
	publicKeyURL := fs.String("public-key-url", "", "URL to fetch the PEM-encoded public key from")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*binaryPath) == "" {
		return errors.New("--binary is required")
	}

	sigB64, err := readSignature(*signature, *signatureFile)
	if err != nil {
		return err
	}

	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(sigBytes) != ed25519.SignatureSize {
		return fmt.Errorf("signature has unexpected length %d (want %d)", len(sigBytes), ed25519.SignatureSize)
	}

	pub, err := loadVerifyPublicKey(*publicKeyFile, *publicKeyURL)
	if err != nil {
		return err
	}

	f, err := os.Open(*binaryPath) // #nosec G304 — caller-controlled installer path
	if err != nil {
		return fmt.Errorf("open binary: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash binary: %w", err)
	}
	digest := h.Sum(nil)

	if !ed25519.Verify(pub, digest, sigBytes) {
		return errors.New("signature verification failed")
	}
	fmt.Println("ok: signature verified")
	return nil
}

func readSignature(inline, path string) (string, error) {
	if strings.TrimSpace(inline) != "" {
		return strings.TrimSpace(inline), nil
	}
	if strings.TrimSpace(path) == "" {
		return "", errors.New("--signature or --signature-file is required")
	}
	data, err := os.ReadFile(path) // #nosec G304 — caller-controlled installer path
	if err != nil {
		return "", fmt.Errorf("read signature file: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func loadVerifyPublicKey(localFile, remoteURL string) (ed25519.PublicKey, error) {
	var pemBytes []byte

	switch {
	case strings.TrimSpace(localFile) != "":
		data, err := os.ReadFile(localFile) // #nosec G304 — caller-controlled installer path
		if err != nil {
			return nil, fmt.Errorf("read public key file: %w", err)
		}
		pemBytes = data
	case strings.TrimSpace(remoteURL) != "":
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(remoteURL) // #nosec G107 — installer-supplied URL
		if err != nil {
			return nil, fmt.Errorf("fetch public key: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("fetch public key: status %d", resp.StatusCode)
		}
		pemBytes, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read public key body: %w", err)
		}
	default:
		return nil, errors.New("--public-key or --public-key-url is required")
	}

	block, _ := pem.Decode(pemBytes)
	var keyDER []byte
	if block != nil {
		keyDER = block.Bytes
	} else {
		// Try raw base64 fallback.
		trimmed := bytes.TrimSpace(pemBytes)
		decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
		if err != nil {
			return nil, errors.New("unsupported public key encoding")
		}
		keyDER = decoded
	}

	if len(keyDER) == ed25519.PublicKeySize {
		return ed25519.PublicKey(keyDER), nil
	}

	pub, err := x509.ParsePKIXPublicKey(keyDER)
	if err != nil {
		return nil, fmt.Errorf("parse ed25519 public key: %w", err)
	}
	pk, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not ed25519")
	}
	return pk, nil
}
