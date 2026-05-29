package server

import (
	"bytes"
	"encoding/json"
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

func TestTenantConnectorPolicyAPIUpdatesEventFilterBackedPolicy(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "bank", CreatedAt: now}},
		eventFilters: map[uuid.UUID]storage.TenantEventFilters{
			tenantID: storage.DefaultTenantEventFilters(tenantID),
		},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	body := []byte(`{
		"allow_medium_risk": true,
		"auto_connect_programs": ["postgresql", "postgresql", " "],
		"approval_required_programs": ["nginx"],
		"blocked_programs": ["temenos-t24"]
	}`)
	putReq := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/"+tenantID.String()+"/connector-policy", bytes.NewReader(body))
	putReq = withPrincipal(putReq, &auth.Principal{Type: "user", Name: "admin@example.com", Subject: "admin@example.com", Roles: []string{"admin"}})
	putRec := httptest.NewRecorder()
	srv.handleTenantResource(putRec, putReq)
	if putRec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200; body = %s", putRec.Code, putRec.Body.String())
	}
	var putResp tenantConnectorPolicyResponse
	if err := json.Unmarshal(putRec.Body.Bytes(), &putResp); err != nil {
		t.Fatalf("decode PUT response: %v", err)
	}
	if !putResp.AllowMediumRisk || putResp.AllowHighRisk {
		t.Fatalf("risk flags = %#v", putResp)
	}
	if len(putResp.AutoConnectPrograms) != 1 || putResp.AutoConnectPrograms[0] != "postgresql" {
		t.Fatalf("auto programs = %#v", putResp.AutoConnectPrograms)
	}
	if len(putResp.ApprovalRequiredPrograms) != 1 || putResp.ApprovalRequiredPrograms[0] != "nginx" {
		t.Fatalf("approval programs = %#v", putResp.ApprovalRequiredPrograms)
	}
	if len(putResp.BlockedPrograms) != 1 || putResp.BlockedPrograms[0] != "temenos-t24" {
		t.Fatalf("blocked programs = %#v", putResp.BlockedPrograms)
	}

	stored := store.eventFilters[tenantID]
	if !stored.CaptureFiles || !stored.CaptureDBQueries {
		t.Fatalf("connector policy update should not erase event filter defaults: %#v", stored)
	}
	if !stored.ConnectorAutoConnectMediumRisk || len(stored.ConnectorAutoConnectPrograms) != 1 {
		t.Fatalf("stored connector policy = %#v", stored)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+tenantID.String()+"/connector-policy", nil)
	getReq = withPrincipal(getReq, &auth.Principal{Type: "user", Name: "viewer@example.com", Roles: []string{"viewer"}})
	getRec := httptest.NewRecorder()
	srv.handleTenantResource(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200; body = %s", getRec.Code, getRec.Body.String())
	}
	var getResp tenantConnectorPolicyResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if getResp.TenantID != tenantID.String() || !getResp.AllowMediumRisk || len(getResp.AutoConnectPrograms) != 1 {
		t.Fatalf("GET connector policy = %#v", getResp)
	}
}
