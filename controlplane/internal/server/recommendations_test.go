package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// TestRecommendationsBridgeWritesPortObservations is the closure test for
// bugs §1.3.
//
// Before this fix the code path was:
//
//	agent POST /nodes/<id>/services
//	  -> ReplaceNodeServices  (writes node_services rows)
//	-- nothing else --
//
// Result: port_observations had zero writers in the codebase, the
// diagnostic SQL "SELECT count(*) FROM port_observations" always returned
// 0, and the Recommendations tab was permanently empty for every tenant.
//
// The test exercises the full ingest -> bridge path with a synthetic
// service inventory and asserts:
//  1. After a single agent ingest, the fake store has > 0 port_observations
//     rows (mirroring the diagnostic SQL).
//  2. After enough ingest cycles to clear the 50-sample / 95%-dominant
//     threshold, handleRecommendations returns at least one recommendation
//     with non-empty evidence — proving the aggregator actually runs
//     against the bridged rows.
func TestRecommendationsBridgeWritesPortObservations(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "recos-tenant", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "recos-host",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "test-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	body := nodeServicesRequest{
		Services: []nodeServiceItem{
			{PID: 100, Process: "nginx", Port: 443, ServiceKind: "https"},
			{PID: 200, Process: "sshd", Port: 22, ServiceKind: "ssh"},
			{PID: 300, Process: "postgres", Port: 5432, ServiceKind: "postgresql"},
		},
	}
	performIngest(t, srv, nodeID, body)

	if got := len(store.portObservations); got == 0 {
		t.Fatalf("port_observations row count = 0 after ingest; bridge not wired (bugs §1.3)")
	}
	if got, want := len(store.portObservations), len(body.Services); got != want {
		t.Fatalf("port_observations row count = %d, want one per service (%d)", got, want)
	}
	for _, obs := range store.portObservations {
		if obs.TenantID != tenantID {
			t.Errorf("observation tenant = %s, want %s", obs.TenantID, tenantID)
		}
		if obs.NodeID == nil || *obs.NodeID != nodeID {
			t.Errorf("observation node_id missing or mismatched (want %s, got %v)", nodeID, obs.NodeID)
		}
		if obs.State != "open" {
			t.Errorf("observation state = %q, want open (listening service)", obs.State)
		}
		if obs.Protocol == "" {
			t.Errorf("observation protocol is empty")
		}
	}

	// Drive the aggregator over the 50-sample / 95%-dominant gate.
	// Each ingest writes one row per service, so each (port, protocol)
	// pair needs >= 50 cycles to clear the minSamples threshold. We do
	// 59 more (60 total including the first) so every port crosses the
	// gate with a 100%-dominant "open" state.
	for i := 0; i < 59; i++ {
		performIngest(t, srv, nodeID, body)
	}
	if got := len(store.portObservations); got < 60*len(body.Services) {
		t.Fatalf("port_observations row count = %d, want >= %d to clear minSamples per port", got, 60*len(body.Services))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/recommendations?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, &auth.Principal{
		Type:  "user",
		Name:  "viewer@example.com",
		Roles: []string{"viewer"},
	})
	rec := httptest.NewRecorder()
	srv.handleRecommendations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("recommendations status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []recommendationResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Data) == 0 {
		t.Fatalf("recommendations returned 0 rules; bridge wrote rows but aggregator did not surface them")
	}
	for _, r := range resp.Data {
		if r.Kind != "port_rule" {
			t.Errorf("rec kind = %q, want port_rule", r.Kind)
		}
		if samples, ok := r.Evidence["samples"]; !ok || samples == nil {
			t.Errorf("rec evidence missing samples: %+v", r.Evidence)
		}
	}
}

// TestBridgePortObservationsRejectsInvalidPort guards the silent-skip path:
// out-of-range ports in the agent payload must NOT generate rows.
func TestBridgePortObservationsRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "recos-tenant", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	body := nodeServicesRequest{
		Services: []nodeServiceItem{
			{PID: 1, Process: "x", Port: 0, ServiceKind: "http"},
			{PID: 2, Process: "y", Port: 70000, ServiceKind: "http"},
			{PID: 3, Process: "nginx", Port: 80, ServiceKind: "http"},
		},
	}
	performIngest(t, srv, nodeID, body)

	if got, want := len(store.portObservations), 1; got != want {
		t.Fatalf("port_observations rows = %d, want %d (only valid ports persisted)", got, want)
	}
	if store.portObservations[0].Port != 80 {
		t.Errorf("persisted port = %d, want 80", store.portObservations[0].Port)
	}
}

// TestPortStateFromServiceClassification verifies the probe-status -> state
// mapping used by the bridge.
func TestPortStateFromServiceClassification(t *testing.T) {
	t.Parallel()

	ok := 200
	bad := 502
	zero := 0
	cases := []struct {
		name string
		svc  storage.NodeService
		want string
	}{
		{"no probe", storage.NodeService{}, "open"},
		{"healthy probe", storage.NodeService{ProbeStatus: &ok}, "open"},
		{"5xx probe", storage.NodeService{ProbeStatus: &bad}, "filtered"},
		{"zero probe", storage.NodeService{ProbeStatus: &zero}, "filtered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portStateFromService(tc.svc); got != tc.want {
				t.Fatalf("portStateFromService(%+v) = %q, want %q", tc.svc, got, tc.want)
			}
		})
	}
}

// performIngest runs one agent inventory POST against handleNodeServicesIngest
// with an mTLS principal whose CN matches nodeID.
func performIngest(t *testing.T, srv *Server, nodeID uuid.UUID, body nodeServicesRequest) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%s/services", nodeID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleNodeServicesIngest(rec, req, nodeID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ingest status = %d, want 204; body = %s", rec.Code, rec.Body.String())
	}
}
