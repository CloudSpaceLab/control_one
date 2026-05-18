package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestNodeIsolationEndpointSetsTimedAirgap(t *testing.T) {
	srv, store := dashboardAdminHarness(t, "operator", "operator-token")
	tenantID := store.tenants[0].ID
	nodeID := uuid.New()
	store.nodes = []storage.Node{{
		ID:        nodeID,
		TenantID:  tenantID,
		Hostname:  "core-db-01",
		State:     storage.NodeStateActive,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Labels: map[string]any{
			"criticality": "core-banking",
		},
	}}
	body, _ := json.Marshal(nodeIsolationRequest{
		Mode:            isolationModeAirgapped,
		DurationSeconds: 900,
		Reason:          "maintenance isolation",
		AllowlistCIDRs:  []string{"10.0.0.0/8"},
	})
	req := httptestNewRequestWithBearer(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/isolation", body, "operator-token")
	rec := dashboardCallRequest(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	node, err := store.GetNode(t.Context(), nodeID)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}
	if got := node.Labels[isolationModeLabel]; got != isolationModeAirgapped {
		t.Fatalf("expected airgapped label, got %#v", node.Labels)
	}
	if got := node.Labels[isolationLocalOnlyLabel]; got != true {
		t.Fatalf("expected local-only label, got %#v", got)
	}
	if _, ok := node.Labels[isolationExpiresAtLabel].(string); !ok {
		t.Fatalf("expected expiry label, got %#v", node.Labels[isolationExpiresAtLabel])
	}

	body, _ = json.Marshal(nodeIsolationRequest{Mode: isolationModeOnline})
	req = httptestNewRequestWithBearer(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/isolation", body, "operator-token")
	rec = dashboardCallRequest(t, srv, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 clearing isolation got %d body=%s", rec.Code, rec.Body.String())
	}
	node, err = store.GetNode(t.Context(), nodeID)
	if err != nil {
		t.Fatalf("get node after clear: %v", err)
	}
	if _, ok := node.Labels[isolationModeLabel]; ok {
		t.Fatalf("expected isolation labels cleared, got %#v", node.Labels)
	}
	if node.Labels["criticality"] != "core-banking" {
		t.Fatalf("expected existing labels retained, got %#v", node.Labels)
	}
}

func httptestNewRequestWithBearer(method, path string, body []byte, token string) *http.Request {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func dashboardCallRequest(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}
