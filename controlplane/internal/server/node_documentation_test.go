package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestNodeDocumentationEndpointsComposeNodeState(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	observed := time.Date(2026, 5, 12, 8, 0, 0, 0, time.UTC)
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Bank Ops"}},
		nodes: []storage.Node{{
			ID:           nodeID,
			TenantID:     tenantID,
			Hostname:     "core-api-01",
			OS:           sql.NullString{String: "linux", Valid: true},
			Arch:         sql.NullString{String: "amd64", Valid: true},
			PublicIP:     sql.NullString{String: "203.0.113.10", Valid: true},
			State:        storage.NodeStateActive,
			AgentVersion: sql.NullString{String: "1.2.3", Valid: true},
		}},
		nodeServices: map[uuid.UUID][]storage.NodeService{
			nodeID: {{
				NodeID:      nodeID,
				TenantID:    tenantID,
				Process:     "nginx",
				ListenAddr:  "0.0.0.0",
				Port:        443,
				ServiceKind: "https",
			}},
		},
		firewallStates: map[uuid.UUID]storage.NodeFirewallState{
			nodeID: {
				NodeID:       nodeID,
				FirewallType: "ufw",
				Enabled:      true,
				Rules:        []storage.FirewallRule{{Action: "allow", Direction: "in", Protocol: "tcp", Port: "443"}},
				ObservedAt:   observed,
			},
		},
		nodeHealthScores: map[uuid.UUID]storage.NodeHealthScore{
			nodeID: {
				NodeID:     nodeID,
				Score:      82,
				RiskLevel:  "low",
				Components: map[string]any{"load1_high": 0},
				ComputedAt: observed,
			},
		},
		alerts: []storage.Alert{{
			ID:       uuid.New(),
			TenantID: tenantID,
			NodeID:   uuid.NullUUID{UUID: nodeID, Valid: true},
			Severity: "high",
			Title:    "CPU spike",
			State:    "open",
			OpenedAt: observed,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("viewer", "viewer-token"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/documentation", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET documentation status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body nodeDocumentationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode documentation response: %v", err)
	}
	if body.Node.ID != nodeID.String() || body.Node.Hostname != "core-api-01" {
		t.Fatalf("unexpected node summary: %+v", body.Node)
	}
	if body.Firewall == nil || body.Firewall.FirewallType != "ufw" || !body.Firewall.Enabled {
		t.Fatalf("firewall state missing: %+v", body.Firewall)
	}
	if body.Health == nil || body.Health.Score != 82 || body.Health.RiskLevel != "low" {
		t.Fatalf("health score missing: %+v", body.Health)
	}
	if len(body.Services) != 1 || body.Services[0].Port != 443 {
		t.Fatalf("services missing: %+v", body.Services)
	}
	if len(body.RecentAlerts) != 1 || body.RecentAlerts[0].Title != "CPU spike" {
		t.Fatalf("alerts missing: %+v", body.RecentAlerts)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/documentation.md", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET documentation.md status=%d body=%s", rec.Code, rec.Body.String())
	}
	md := rec.Body.String()
	for _, want := range []string{"# Node documentation - core-api-01", "Firewall", "Health", "nginx", "CPU spike"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestNodeDocumentationUsesSmallAnalyticsConnections(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Bank Ops"}},
		nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			Hostname: "core-api-01",
			State:    storage.NodeStateActive,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP:      config.HTTPConfig{Address: ":0"},
		Auth:      authWithTokens("viewer", "viewer-token"),
		Analytics: config.AnalyticsConfig{Mode: "small", SQLiteDir: t.TempDir()},
	}, store, &stubQueue{})
	defer func() { _ = srv.Stop(context.Background()) }()
	if srv.localAnalytics == nil {
		t.Fatal("localAnalytics was not initialized")
	}
	base := time.Now().UTC().Add(-10 * time.Minute)
	if err := srv.localAnalytics.AppendConnectionRows(context.Background(), []map[string]any{
		smallAnalyticsConnRow(tenantID, nodeID, "doc-conn-1", base, base.Add(30*time.Second), "outbound", "10.0.0.5", "8.8.4.4", 20, 40, ""),
	}); err != nil {
		t.Fatalf("append connection row: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/documentation", nil)
	req.Header.Set("Authorization", "Bearer viewer-token")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET documentation status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body nodeDocumentationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode documentation response: %v", err)
	}
	if len(body.TopConnections) != 1 || body.TopConnections[0].ProcessName != "curl" || body.TopConnections[0].DstIP != "8.8.4.4" {
		t.Fatalf("small analytics top connections missing: %+v", body.TopConnections)
	}
}

func TestKnowledgeGraphCompressionPreservesExactMatches(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	targetID := uuid.New()
	targetIP := "203.0.113.250"
	nodes := make([]storage.Node, 0, 1000)
	for i := 0; i < 1000; i++ {
		id := uuid.New()
		ip := fmt.Sprintf("10.20.%d.%d", i/255, i%255)
		hostname := fmt.Sprintf("node-%04d", i)
		if i == 777 {
			id = targetID
			ip = targetIP
			hostname = "target-node"
		}
		nodes = append(nodes, storage.Node{
			ID:           id,
			TenantID:     tenantID,
			Hostname:     hostname,
			OS:           sql.NullString{String: "linux", Valid: true},
			Arch:         sql.NullString{String: "amd64", Valid: true},
			AgentVersion: sql.NullString{String: "1.2.3", Valid: true},
			PublicIP:     sql.NullString{String: ip, Valid: true},
			State:        storage.NodeStateActive,
		})
	}
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "Large Tenant"}},
		nodes:   nodes,
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, store, &stubQueue{})

	sections, err := srv.buildKGSections(t.Context(), tenantID)
	if err != nil {
		t.Fatalf("build KG: %v", err)
	}
	const budgetTokens = 3000
	doc := compressForQuery(sections, "investigate "+targetID.String()+" and "+targetIP, budgetTokens)
	budgetBytes := budgetTokens * approxCharsPerToken
	if len(doc) > budgetBytes {
		t.Fatalf("knowledge graph length = %d, want <= %d", len(doc), budgetBytes)
	}
	if !strings.Contains(doc, targetID.String()) || !strings.Contains(doc, targetIP) || !strings.Contains(doc, "target-node") {
		t.Fatalf("exact match node missing from compressed graph:\n%s", doc)
	}
}
