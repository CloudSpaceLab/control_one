package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPinnedReleaseVerificationUsesExternalTrust(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Linux/macOS OpenSSL bootstrap path")
	}
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("OpenSSL unavailable")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wrongPub, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "tampered", "attacker-bundle", "unsigned"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			binaryPath := filepath.Join(root, "controlone-agent")
			binary := []byte("#!/bin/sh\ntouch \"$EXECUTED_MARKER\"\nexit 0\n")
			digest := sha256.Sum256(binary)
			signer := priv
			if scenario == "attacker-bundle" {
				signer = wrongPriv
			}
			signature := base64.StdEncoding.EncodeToString(ed25519.Sign(signer, digest[:]))
			if scenario == "tampered" {
				binary = append(binary, []byte("# altered bytes\n")...)
			}
			if scenario == "unsigned" {
				signature = ""
			}
			if err := os.WriteFile(binaryPath, binary, 0700); err != nil {
				t.Fatal(err)
			}
			for name, key := range map[string]ed25519.PublicKey{"pinned.pem": pub, "signing-public-key.pem": wrongPub} {
				der, err := x509.MarshalPKIXPublicKey(key)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name), pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-c", pinnedReleaseVerificationBash+`verify_pinned_release "$BINARY" "$SIGNATURE" "$PINNED_KEY"`)
			cmd.Env = append(os.Environ(), "BINARY="+binaryPath, "SIGNATURE="+signature, "PINNED_KEY="+filepath.Join(root, "pinned.pem"))
			output, err := cmd.CombinedOutput()
			if scenario == "valid" {
				if err != nil {
					t.Fatalf("valid pinned signature: %v %s", err, output)
				}
				return
			}
			if err == nil {
				t.Fatal("untrusted release accepted")
			}
			manifest, _ := json.Marshal(map[string]string{"signature": signature})
			for name, data := range map[string][]byte{"install-offline.sh": []byte(installOfflineScriptTemplate), "manifest.json": manifest, "config.yaml": []byte("isolated-fixture"), "controlone-agent.sig": []byte(signature)} {
				if err := os.WriteFile(filepath.Join(root, name), data, 0700); err != nil {
					t.Fatal(err)
				}
			}
			marker := filepath.Join(root, "downloaded-code-executed")
			cmd = exec.Command("bash", filepath.Join(root, "install-offline.sh"), "--install-dir", filepath.Join(root, "install"), "--config-dir", filepath.Join(root, "config"), "--no-service")
			cmd.Env = append(os.Environ(), "CONTROLONE_SIGNING_PUBLIC_KEY="+filepath.Join(root, "pinned.pem"), "EXECUTED_MARKER="+marker)
			output, err = cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("untrusted installer completed: %s", output)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatal("untrusted downloaded code executed before verification")
			}
			if _, err := os.Stat(filepath.Join(root, "install")); !os.IsNotExist(err) {
				t.Fatal("untrusted release installed")
			}
		})
	}
}

// Exercise the existing generated offline installer in an isolated directory.
// The stub only tests exit handling; cryptography is tested by bundle/policy tests.
func TestOfflineInstallerStopsWhenSignatureVerifierFails(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("bash fixture runs on the Linux development runtime")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	for _, scenario := range []struct{ name, omitted, reason string }{
		{"verifier-failure", "", "Signature verification failed"},
		{"missing-signature", "controlone-agent.sig", "Bundled signature missing"},
		{"missing-key", "signing-public-key.pem", "Bundled signing public key missing"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			binary := []byte("#!/bin/sh\nexit 7\n")
			for name, body := range map[string][]byte{"controlone-agent": binary, "controlone-agent.sig": []byte("fixture-signature"), "signing-public-key.pem": []byte("fixture-key"), "config.yaml": []byte("fixture"), "manifest.json": []byte(`{"signature":"fixture-signature"}`), "install-offline.sh": []byte(installOfflineScriptTemplate)} {
				if name == scenario.omitted {
					continue
				}
				if err := os.WriteFile(filepath.Join(root, name), body, 0700); err != nil {
					t.Fatal(err)
				}
			}
			installDir := filepath.Join(root, "not-installed")
			configDir := filepath.Join(root, "not-configured")
			cmd := exec.Command(bash, filepath.Join(root, "install-offline.sh"), "--install-dir", installDir, "--config-dir", configDir, "--no-service")
			output, err := cmd.CombinedOutput()
			if err == nil || !strings.Contains(string(output), scenario.reason) {
				t.Fatalf("must stop on verifier failure: %v %s", err, output)
			}
			if _, err := os.Stat(installDir); !os.IsNotExist(err) {
				t.Fatal("failed verification created install directory")
			}
			if _, err := os.Stat(configDir); !os.IsNotExist(err) {
				t.Fatal("failed verification wrote configuration")
			}
		})
	}
}
