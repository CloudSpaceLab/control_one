package executionauthority

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCurrentAuthorityContract(t *testing.T) {
	for _, scenario := range []string{"allow", "revoked", "inactive", "roles_removed", "wrong_nonce", "wrong_subject", "wrong_tenant", "wrong_boundary", "wrong_reference", "wrong_authority", "stale", "future", "expired", "long_lifetime", "missing_id", "unknown_field", "trailing_json", "oversized", "refused", "redirect", "untrusted_tls"} {
		t.Run(scenario, func(t *testing.T) {
			seen := map[string]bool{}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
					t.Error("missing service authentication")
				}
				var req Request
				if json.NewDecoder(r.Body).Decode(&req) != nil {
					t.Error("invalid request")
					return
				}
				if len(req.Nonce) != 64 || seen[req.Nonce] {
					t.Error("nonce reused")
				}
				seen[req.Nonce] = true
				d := Decision{Request: req, DecisionID: "fixture-1", Active: true, Allowed: true, Roles: []string{"operator"}, ObservedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(8 * time.Second)}
				switch scenario {
				case "revoked":
					d.Allowed = false
				case "inactive":
					d.Active = false
				case "roles_removed":
					d.Roles = []string{"viewer"}
				case "wrong_nonce":
					d.Nonce = "old-nonce"
				case "wrong_subject":
					d.Subject = "someone-else"
				case "wrong_tenant":
					d.TenantID = "other"
				case "wrong_boundary":
					d.Boundary = "dispatch"
				case "wrong_reference":
					d.Reference = "another-job"
				case "wrong_authority":
					d.Authority = "oidc:https://other.invalid"
				case "stale":
					d.ObservedAt = time.Now().Add(-time.Minute)
				case "future":
					d.ObservedAt = time.Now().Add(time.Minute)
				case "expired":
					d.ExpiresAt = time.Now().Add(-time.Second)
				case "long_lifetime":
					d.ExpiresAt = time.Now().Add(time.Hour)
				case "missing_id":
					d.DecisionID = ""
				case "refused":
					w.WriteHeader(403)
					return
				case "redirect":
					http.Redirect(w, r, "https://other.invalid", 302)
					return
				case "oversized":
					_, _ = w.Write([]byte(strings.Repeat("x", 65537)))
					return
				}
				raw, _ := json.Marshal(d)
				if scenario == "unknown_field" {
					raw = append(raw[:len(raw)-1], []byte(`,"extra":true}`)...)
				}
				_, _ = w.Write(raw)
				if scenario == "trailing_json" {
					_, _ = w.Write([]byte(`{}`))
				}
			}))
			defer server.Close()
			dir := t.TempDir()
			credential := filepath.Join(dir, "credential")
			ca := filepath.Join(dir, "ca.pem")
			if err := os.WriteFile(credential, []byte(strings.Repeat("x", 32)), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "untrusted_tls" {
				ca = ""
			}
			client, err := New(Config{Enabled: true, Endpoint: server.URL, CredentialFile: credential, CAFile: ca}, "https://issuer.invalid")
			if err != nil {
				t.Fatal(err)
			}
			request := Request{Authority: "oidc:https://issuer.invalid", Subject: "subject-1", UserID: "user-1", TenantID: "tenant-1", Boundary: "start", Purpose: "individual_approval", Reference: "job-1"}
			_, err = client.Check(context.Background(), request)
			if scenario == "allow" {
				if err != nil {
					t.Fatal(err)
				}
				if _, err = client.Check(context.Background(), request); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("unsafe decision accepted")
			}
		})
	}
}

func TestCurrentAuthorityConfigurationAndCancellation(t *testing.T) {
	if client, err := New(Config{}, ""); err != nil || client != nil {
		t.Fatal("disabled contract must remain unavailable")
	}
	for _, endpoint := range []string{"http://127.0.0.1/check", "https://user:secret@example.invalid/check", "https://example.invalid/check?token=x", "https://example.invalid/#fragment", "/relative"} {
		if _, err := New(Config{Enabled: true, Endpoint: endpoint}, "https://issuer.invalid"); err == nil {
			t.Fatal("invalid endpoint accepted", endpoint)
		}
	}
	var client *Client
	if _, err := client.Check(context.Background(), Request{}); err == nil {
		t.Fatal("nil adapter allowed")
	}
}

func TestCurrentAuthorityDeadlineAndCancelledCaller(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer func() { close(release); server.Close() }()
	client := &Client{endpoint: server.URL, credential: strings.Repeat("x", 32), authority: "oidc:https://issuer.invalid", http: server.Client()}
	client.http.Timeout = 40 * time.Millisecond
	request := Request{Authority: client.authority, Subject: "subject", UserID: "user", TenantID: "tenant", Boundary: "start", Purpose: "individual_approval", Reference: "job"}
	if _, err := client.Check(context.Background(), request); err == nil {
		t.Fatal("timeout allowed execution")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Check(ctx, request); err == nil {
		t.Fatal("cancelled caller allowed execution")
	}
}
