package policy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/api"
	"go.uber.org/zap"
)

func TestPolicyTrustRejectsMissingAndMalformedConfiguredKeys(t *testing.T) {
	for _, kind := range []string{"missing", "malformed"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.pem")
			if kind == "malformed" {
				if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewSyncer(nil, zap.NewNop(), Options{PublicKeyPath: path}); err == nil {
				t.Fatal("configured trust must fail closed")
			}
		})
	}
}

func TestPolicyTrustVerifiesFetchAndCache(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, wrongPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "unsigned", "tampered-rule", "tampered-version", "wrong-key", "malformed-signature"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			keyPath := filepath.Join(root, "policy.pem")
			der, err := x509.MarshalPKIXPublicKey(pub)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600); err != nil {
				t.Fatal(err)
			}
			set := PolicySet{Version: "1", Policies: []Rule{{ID: "trust-fixture", Version: "1", Check: "fixture-original"}}}
			payload, err := json.Marshal(struct {
				Policies []Rule `json:"policies"`
				Version  string `json:"version,omitempty"`
			}{set.Policies, set.Version})
			if err != nil {
				t.Fatal(err)
			}
			signer := priv
			trustedSet := PolicySet{Policies: set.Policies, Version: set.Version, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, payload))}
			originalCache, err := json.Marshal(trustedSet)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "wrong-key" {
				signer = wrongPriv
			}
			set.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(signer, payload))
			switch scenario {
			case "unsigned":
				set.Signature = ""
			case "tampered-rule":
				set.Policies[0].Check = "fixture-altered"
			case "tampered-version":
				set.Version = "2"
			case "malformed-signature":
				set.Signature = "not base64!"
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(set) }))
			defer srv.Close()
			client, err := api.NewClient(srv.URL, "", "", "", "")
			if err != nil {
				t.Fatal(err)
			}
			syncer, err := NewSyncer(client, zap.NewNop(), Options{PolicyDir: root, PublicKeyPath: keyPath})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(syncer.cachePath(), originalCache, 0600); err != nil {
				t.Fatal(err)
			}
			_, err = syncer.FetchAndPersist(context.Background(), "isolated-fixture")
			if scenario == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				rules, err := syncer.LoadCached()
				if err != nil || len(rules) != 1 {
					t.Fatalf("valid cache: %v %v", rules, err)
				}
			} else {
				if err == nil {
					t.Fatal("untrusted fetched policy accepted")
				}
				after, readErr := os.ReadFile(syncer.cachePath())
				if readErr != nil || !bytes.Equal(after, originalCache) {
					t.Fatal("rejected fetch replaced cached policy")
				}
				if rules, err := syncer.LoadCached(); err != nil || len(rules) != 1 || rules[0].Check != "fixture-original" {
					t.Fatal("last trusted cached policy is not available after rejected fetch")
				}
				encoded, err := json.Marshal(set)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(syncer.cachePath(), encoded, 0600); err != nil {
					t.Fatal(err)
				}
				if rules, err := syncer.LoadCached(); err == nil || rules != nil {
					t.Fatal("untrusted cached policy accepted")
				}
			}
		})
	}
}
