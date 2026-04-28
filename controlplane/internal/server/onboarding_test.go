package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/connect"
)

type stubRegistryConnector struct {
	probe *connect.Probe
	err   error
}

func (s *stubRegistryConnector) Name() connect.Protocol { return connect.ProtoSSH }
func (s *stubRegistryConnector) Test(_ context.Context, _ connect.Target) (*connect.Probe, error) {
	return s.probe, s.err
}

func TestOnboardingValidation(t *testing.T) {
	cases := []struct {
		name string
		body testConnectionRequest
		ok   bool
	}{
		{"missing host", testConnectionRequest{Protocol: connect.ProtoSSH, Username: "x", Auth: connect.AuthPassword, Password: "p"}, false},
		{"unknown protocol", testConnectionRequest{Protocol: "telnet", Host: "h"}, false},
		{"ssh password ok", testConnectionRequest{Protocol: connect.ProtoSSH, Host: "h", Username: "u", Auth: connect.AuthPassword, Password: "p"}, true},
		{"ssh password missing", testConnectionRequest{Protocol: connect.ProtoSSH, Host: "h", Username: "u", Auth: connect.AuthPassword}, false},
		{"key auth on winrm", testConnectionRequest{Protocol: connect.ProtoWinRM, Host: "h", Username: "u", Auth: connect.AuthPrivateKey, PrivateKey: "----"}, false},
		{"rdp no creds ok", testConnectionRequest{Protocol: connect.ProtoRDP, Host: "h"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateOnboardingRequest(c.body)
			if c.ok && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
			if !c.ok && err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestOnboardingTestConnectionRoleGate(t *testing.T) {
	body, _ := json.Marshal(testConnectionRequest{
		Protocol: connect.ProtoSSH,
		Host:     "10.0.0.1",
		Username: "ubuntu",
		Auth:     connect.AuthPassword,
		Password: "secret",
	})

	// viewer → 403
	srvViewer, _ := dashboardAdminHarness(t, "viewer", "viewer-token")
	r := connect.NewRegistry()
	r.Register(&stubRegistryConnector{probe: &connect.Probe{Reachable: true, OS: "linux"}})
	srvViewer.connectRegistryOverride = r

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/onboarding/test-connection", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer viewer-token")
	srvViewer.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("viewer expected 403, got %d body=%s", rec.Code, rec.Body.String())
	}

	// operator → 200, ok=true, probe.os=linux
	srvOp, _ := dashboardAdminHarness(t, "operator", "op-token")
	r2 := connect.NewRegistry()
	r2.Register(&stubRegistryConnector{probe: &connect.Probe{Reachable: true, OS: "linux", LatencyMs: 12}})
	srvOp.connectRegistryOverride = r2

	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/api/v1/onboarding/test-connection", strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer op-token")
	srvOp.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("operator expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out testConnectionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !out.OK || out.Probe == nil || out.Probe.OS != "linux" {
		t.Errorf("probe: %+v", out)
	}
}

func TestOnboardingProtocolsListsBuiltins(t *testing.T) {
	srv, _ := dashboardAdminHarness(t, "viewer", "viewer-token")

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/onboarding/protocols", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out onboardingProtocolsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	have := map[connect.Protocol]bool{}
	for _, p := range out.Protocols {
		have[p] = true
	}
	for _, want := range []connect.Protocol{connect.ProtoSSH, connect.ProtoWinRM, connect.ProtoRDP} {
		if !have[want] {
			t.Errorf("missing protocol %q", want)
		}
	}
}
