package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/api"
)

// TestHeartbeatE2E_RealHTTPRoundTrip verifies the heartbeat works end-to-end
// over real HTTP: the agent's api.Client (the same one cmd/nodeagent uses)
// POSTs a heartbeat to the real Server.handleNodeHeartbeat handler against
// a fakeStore. Proves the contract beyond the per-side unit tests.
//
// The auth.Middleware needs an mTLS handshake to extract a Principal from
// the cert CN; httptest doesn't model mTLS, so we wrap the real handler in
// a tiny test middleware that injects the agent Principal exactly the way
// the real middleware would after a successful mTLS handshake. Everything
// downstream (URL parsing, body decode, store calls, response encode) is
// the real production code path.
func TestHeartbeatE2E_RealHTTPRoundTrip(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "e2e-host",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	cn := nodeID.String()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/nodes/", func(w http.ResponseWriter, r *http.Request) {
		principal := &auth.Principal{
			Type:    "agent",
			Name:    cn,
			Subject: "CN=" + cn,
			Roles:   []string{"agent"},
		}
		r = r.WithContext(context.WithValue(r.Context(), auth.ContextKeyPrincipal, principal))
		srv.handleNodeResource(w, r)
	})
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	client, err := api.NewClient(httpSrv.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}

	body := heartbeatRequest{
		AgentVersion:  "v0.0.0-e2e-test (linux/amd64)",
		PackageHash:   "deadbeef",
		PackageCount:  3,
		KernelVersion: "6.5.0-test",
		OSVersion:     "Ubuntu 24.04 LTS",
		FirewallState: &heartbeatFirewallState{
			Type:    "ufw",
			Enabled: true,
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	resp, err := client.Do(context.Background(), http.MethodPost,
		"/api/v1/nodes/"+nodeID.String()+"/heartbeat", bodyBytes)
	if err != nil {
		t.Fatalf("client.Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body: %s", resp.StatusCode, respBody)
	}

	var ack heartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&ack); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if store.nodes[0].LastSeenAt == nil {
		t.Fatal("nodes[0].LastSeenAt is nil — TouchNodeHeartbeat was not called")
	}
	if ack.NodeID != nodeID.String() {
		t.Errorf("ack.NodeID = %q, want %q", ack.NodeID, nodeID.String())
	}
	if ack.State == "" {
		t.Error("ack.State is empty")
	}
	if ack.LastSeenAt == "" {
		t.Error("ack.LastSeenAt is empty")
	}

	t.Logf("E2E OK: %d-byte heartbeat → real HTTP → handler → store; ack: state=%s last_seen=%s",
		len(bodyBytes), ack.State, ack.LastSeenAt)
}

// TestEnrollE2E_PolicyPublicKeyRoundTrips verifies the new
// enrollResponse.policy.public_key_pem field round-trips correctly over
// real HTTP — the wiring this PR adds. Two cases: when no key is configured
// the agent receives an empty string; when one is configured the bytes
// arrive verbatim.
func TestEnrollE2E_PolicyPublicKeyRoundTrips(t *testing.T) {
	enroll := func(t *testing.T, srv *Server) string {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/enroll", srv.handleEnroll)
		httpSrv := httptest.NewServer(mux)
		defer httpSrv.Close()

		client, err := api.NewClient(httpSrv.URL, "", "", "", "")
		if err != nil {
			t.Fatalf("api.NewClient: %v", err)
		}

		body, _ := json.Marshal(map[string]any{
			"token":      "cot_test_enroll_token_value",
			"hostname":   "e2e-host",
			"machine_id": uuid.NewString(),
		})
		resp, err := client.Do(context.Background(), http.MethodPost, "/api/v1/enroll", body)
		if err != nil {
			t.Fatalf("client.Do: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			t.Fatalf("enroll status = %d; body: %s", resp.StatusCode, raw)
		}
		var er enrollResponse
		if err := json.Unmarshal(raw, &er); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return er.Policy.PublicKeyPEM
	}

	t.Run("case_no_key_configured", func(t *testing.T) {
		srv, _, _ := setupEnrollmentServer(t)
		// cfg.Policy.PublicKeyFile defaults to "" — server returns empty PEM.
		got := enroll(t, srv)
		if got != "" {
			t.Errorf("policy.public_key_pem = %q, want empty", got)
		}
	})

	t.Run("case_key_configured", func(t *testing.T) {
		srv, _, _ := setupEnrollmentServer(t)
		fakePEM := "-----BEGIN PUBLIC KEY-----\nFAKEKEYBYTES\n-----END PUBLIC KEY-----\n"
		path := filepath.Join(t.TempDir(), "policy_pub.pem")
		if err := os.WriteFile(path, []byte(fakePEM), 0o600); err != nil {
			t.Fatalf("write key: %v", err)
		}
		srv.cfg.Policy.PublicKeyFile = path

		got := enroll(t, srv)
		if !strings.Contains(got, "FAKEKEYBYTES") {
			t.Errorf("policy.public_key_pem = %q, want PEM containing FAKEKEYBYTES", got)
		}
	})
}
