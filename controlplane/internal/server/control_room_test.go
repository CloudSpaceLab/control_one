package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestControlRoomOverviewReturnsSixBankingLanes(t *testing.T) {
	srv, store := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := store.tenants[0].ID
	nodeID := uuid.New()
	now := time.Now().UTC()
	store.nodes = []storage.Node{
		{
			ID: nodeID, TenantID: tenantID, Hostname: "core-api-01", LastSeenAt: &now, State: "active",
			Labels: map[string]any{
				isolationModeLabel:      isolationModeWhitelist,
				isolationAllowAppsLabel: []any{"control-one-agent", "patch"},
			},
		},
	}
	store.securityCounts = storage.SecurityEventCounts{Critical: 1, High: 2, Total: 3}
	store.healthCounts = storage.SecurityEventCounts{High: 1, Total: 1}
	store.alerts = []storage.Alert{
		{ID: uuid.New(), TenantID: tenantID, Source: "rule", Severity: "critical", Title: "Credential stuffing suspected", State: "open", OpenedAt: now},
	}
	store.nodeServices = map[uuid.UUID][]storage.NodeService{
		nodeID: {
			{NodeID: nodeID, TenantID: tenantID, ListenAddr: "0.0.0.0", Port: 443, ServiceKind: "web", ObservedAt: now},
			{NodeID: nodeID, TenantID: tenantID, ListenAddr: "127.0.0.1", Port: 5432, ServiceKind: "postgres", ObservedAt: now},
		},
	}
	store.activeBlocks = []storage.ActiveBlock{{EntityActionID: uuid.New(), TenantID: tenantID, EntityType: "ip", EntityID: "203.0.113.10", Action: "block", CreatedAt: now, TotalNodes: 1, NodesApplied: 1}}
	store.patchDeployments = []storage.PatchDeployment{{ID: uuid.New(), TenantID: tenantID, Mode: "direct", Status: "failed", RequestedAt: now, NodesFailed: 1}}
	store.patchApprovals = map[uuid.UUID]storage.PatchApproval{
		uuid.New(): {ID: uuid.New(), TenantID: tenantID, Status: storage.ApprovalStatusPending, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
	}
	store.ipBehaviorCountries = []storage.IPBehaviorCountrySummary{
		{CountryCode: "NG", Country: "Nigeria", RequestCount: 2900, BytesOut: 1024, UniqueSourceIPs: 12, StatusCounts: map[string]int64{"401": 120}, FirstSeenAt: now.Add(-time.Hour), LastSeenAt: now},
	}
	findingID := uuid.New()
	store.ipBehaviorFindings = []storage.IPBehaviorFinding{
		{ID: findingID, TenantID: tenantID, SourceIP: sql.NullString{String: "203.0.113.10", Valid: true}, CountryCode: "NG", Category: "credential_attack", Severity: "critical", Score: 91, Status: "open", Reason: "401 burst outside baseline", LastSeenAt: now, FirstSeenAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Minute), UpdatedAt: now},
	}
	store.webserverInstances = []storage.WebserverInstance{
		{
			ID: uuid.New(), TenantID: tenantID, NodeID: nodeID, Kind: "nginx", ServiceName: "nginx", ConfigPath: "/etc/nginx/nginx.conf",
			VHosts: []map[string]any{{
				"server_name":        "core.example.bank",
				"document_root":      "/srv/core-banking",
				"application_type":   "go",
				"application_name":   "Go application",
				"coverage_state":     "profile_available",
				"parser_profile_id":  "go",
				"detection_evidence": []any{"go.mod"},
			}},
			Capabilities: map[string]any{"capture": true, "enforce": true, "server_purposes": []any{"web_server", "app_node"}},
			ObservedAt:   now,
		},
	}

	rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/control-room/overview?tenant_id="+tenantID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp controlRoomOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Lanes) != 6 {
		t.Fatalf("expected 6 lanes, got %d", len(resp.Lanes))
	}
	want := map[string]bool{
		"server-health": false, "security": false, "app-db-health": false,
		"exposure": false, "ip-behavior": false, "patch-posture": false,
	}
	for _, lane := range resp.Lanes {
		if _, ok := want[lane.ID]; ok {
			want[lane.ID] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("missing lane %s in %#v", id, resp.Lanes)
		}
	}
	lanes := map[string]controlRoomLane{}
	for _, lane := range resp.Lanes {
		lanes[lane.ID] = lane
	}
	if len(lanes["exposure"].Items) == 0 {
		t.Fatalf("expected exposure evidence items, got %#v", lanes["exposure"])
	}
	if len(lanes["app-db-health"].Items) == 0 {
		t.Fatalf("expected app/db evidence items, got %#v", lanes["app-db-health"])
	}
	if len(lanes["patch-posture"].Items) == 0 {
		t.Fatalf("expected patch evidence items, got %#v", lanes["patch-posture"])
	}
	if len(resp.IPBehavior.Findings) != 1 || resp.IPBehavior.Findings[0].Score != 91 {
		t.Fatalf("expected backend IP finding score, got %#v", resp.IPBehavior.Findings)
	}
	if len(resp.Webservers.Instances) != 1 || resp.Webservers.CaptureReady != 1 || resp.Webservers.EnforceReady != 1 {
		t.Fatalf("expected webserver summary, got %#v", resp.Webservers)
	}
	if len(resp.Webservers.Instances[0].VHosts) != 1 || resp.Webservers.Instances[0].Capabilities["server_purposes"] == nil {
		t.Fatalf("expected app root and purpose context in control room webserver payload, got %#v", resp.Webservers.Instances[0])
	}
	if got := lanes["app-db-health"].SecondaryMetric.Drilldown; got != "/security/webservers" {
		t.Fatalf("expected app/db capture metric to drill into webserver control, got %q", got)
	}
	if got := controlRoomMetricDrilldown(lanes["exposure"], "Web block ready"); got != "/security/webservers" {
		t.Fatalf("expected exposure web block metric to drill into webserver control, got %q", got)
	}
	if resp.Isolation.Whitelist != 1 {
		t.Fatalf("expected whitelist isolation posture, got %#v", resp.Isolation)
	}
	if resp.Isolation.Protected != 1 || resp.Isolation.WhitelistGap != 0 {
		t.Fatalf("expected covered whitelist posture, got %#v", resp.Isolation)
	}
	if len(resp.PendingActions) != 4 {
		t.Fatalf("expected pending action groups, got %#v", resp.PendingActions)
	}
}

func TestControlRoomExposureIgnoresAirgappedListeners(t *testing.T) {
	srv, store := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := store.tenants[0].ID
	nodeID := uuid.New()
	old := time.Now().UTC().Add(-2 * time.Hour)
	expires := time.Now().UTC().Add(4 * time.Hour).Format(time.RFC3339)
	store.nodes = []storage.Node{
		{
			ID: nodeID, TenantID: tenantID, Hostname: "vault-offline-01", LastSeenAt: &old, State: "active",
			Labels: map[string]any{
				isolationModeLabel:      isolationModeAirgapped,
				isolationExpiresAtLabel: expires,
			},
		},
	}
	store.nodeServices = map[uuid.UUID][]storage.NodeService{
		nodeID: {{NodeID: nodeID, TenantID: tenantID, ListenAddr: "0.0.0.0", Port: 443, ServiceKind: "web", ObservedAt: old}},
	}

	rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/control-room/overview?tenant_id="+tenantID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp controlRoomOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Isolation.Airgapped != 1 {
		t.Fatalf("expected airgapped posture, got %#v", resp.Isolation)
	}
	lanes := map[string]controlRoomLane{}
	for _, lane := range resp.Lanes {
		lanes[lane.ID] = lane
	}
	if got := lanes["exposure"].PrimaryMetric.Value; got != "0" {
		t.Fatalf("expected airgapped public listener to be excluded, got public listeners=%s", got)
	}
	if got := lanes["server-health"].Metrics[0].Value; got != "0" {
		t.Fatalf("expected airgapped stale heartbeat to be excluded from offline count, got %s", got)
	}
}

func TestControlRoomWhitelistWithoutAllowlistCountsAsExposureGap(t *testing.T) {
	srv, store := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := store.tenants[0].ID
	nodeID := uuid.New()
	now := time.Now().UTC()
	store.nodes = []storage.Node{
		{
			ID: nodeID, TenantID: tenantID, Hostname: "core-api-gap-01", LastSeenAt: &now, State: "active",
			Labels: map[string]any{
				isolationModeLabel: isolationModeWhitelist,
				"criticality":      "core-banking",
			},
		},
	}
	store.nodeServices = map[uuid.UUID][]storage.NodeService{
		nodeID: {{NodeID: nodeID, TenantID: tenantID, ListenAddr: "0.0.0.0", Port: 443, ServiceKind: "web", ObservedAt: now}},
	}

	rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/control-room/overview?tenant_id="+tenantID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp controlRoomOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Isolation.Whitelist != 1 || resp.Isolation.Protected != 0 || resp.Isolation.WhitelistGap != 1 {
		t.Fatalf("expected whitelist gap posture, got %#v", resp.Isolation)
	}
	lanes := map[string]controlRoomLane{}
	for _, lane := range resp.Lanes {
		lanes[lane.ID] = lane
	}
	if lanes["exposure"].Tone != "critical" {
		t.Fatalf("expected critical exposure for unprotected critical whitelist gap, got %#v", lanes["exposure"])
	}
	foundGap := false
	for _, item := range lanes["exposure"].Items {
		if item.Value == "whitelist gap" {
			foundGap = true
			break
		}
	}
	if !foundGap {
		t.Fatalf("expected whitelist gap evidence item, got %#v", lanes["exposure"].Items)
	}
}

func TestControlRoomDefaultDenyFirewallReducesCriticalExposure(t *testing.T) {
	srv, store := dashboardAdminHarness(t, "viewer", "viewer-token")
	tenantID := store.tenants[0].ID
	nodeID := uuid.New()
	now := time.Now().UTC()
	store.nodes = []storage.Node{
		{
			ID: nodeID, TenantID: tenantID, Hostname: "core-api-firewalled-01", LastSeenAt: &now, State: "active",
			Labels: map[string]any{"criticality": "core-banking"},
		},
	}
	store.nodeServices = map[uuid.UUID][]storage.NodeService{
		nodeID: {{NodeID: nodeID, TenantID: tenantID, ListenAddr: "0.0.0.0", Port: 443, ServiceKind: "web", ObservedAt: now}},
	}
	store.firewallStates = map[uuid.UUID]storage.NodeFirewallState{
		nodeID: {
			NodeID:       nodeID,
			FirewallType: "ufw",
			Enabled:      true,
			Rules:        []storage.FirewallRule{{Raw: "Default: deny (incoming), allow (outgoing), deny (routed)"}},
			ObservedAt:   now,
		},
	}

	rec := dashboardCall(t, srv, "viewer-token", http.MethodGet, "/api/v1/control-room/overview?tenant_id="+tenantID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp controlRoomOverviewResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Firewall.Enabled != 1 || resp.Firewall.DefaultDeny != 1 {
		t.Fatalf("expected default-deny firewall posture, got %#v", resp.Firewall)
	}
	lanes := map[string]controlRoomLane{}
	for _, lane := range resp.Lanes {
		lanes[lane.ID] = lane
	}
	if lanes["exposure"].Tone == "critical" {
		t.Fatalf("expected default-deny firewall to avoid critical exposure, got %#v", lanes["exposure"])
	}
	foundFirewallEvidence := false
	for _, item := range lanes["exposure"].Items {
		if item.Value == "firewall" {
			foundFirewallEvidence = true
			break
		}
	}
	if !foundFirewallEvidence {
		t.Fatalf("expected firewall evidence item, got %#v", lanes["exposure"].Items)
	}
}

func controlRoomMetricDrilldown(lane controlRoomLane, label string) string {
	for _, metric := range lane.Metrics {
		if metric.Label == label {
			return metric.Drilldown
		}
	}
	return ""
}
