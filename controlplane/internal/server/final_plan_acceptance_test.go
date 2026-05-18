package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestFinalPlanAcceptanceIPBehaviorScenarios(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()

	t.Run("new country at 4am with high 401 produces anomaly", func(t *testing.T) {
		s := &Server{}
		start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
		got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeID, makeIPBehaviorWebRequests(start, 6))
		if len(got) != 1 || got[0].Details["category"] != "credential_attack" {
			t.Fatalf("anomalies=%d details=%#v, want credential_attack", len(got), firstEventDetails(got))
		}
	})

	t.Run("same country normal business traffic does not alert", func(t *testing.T) {
		s := &Server{}
		start := time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC)
		events := []IngestedEvent{
			makeIPBehaviorWebRequest(start, "203.0.113.10", "/api/status", 200, 512),
			makeIPBehaviorWebRequest(start.Add(time.Second), "203.0.113.10", "/api/status", 200, 512),
			makeIPBehaviorWebRequest(start.Add(2*time.Second), "203.0.113.10", "/api/status", 200, 512),
		}
		if got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeID, events); len(got) != 0 {
			t.Fatalf("normal business traffic produced anomalies: %#v", got)
		}
	})

	t.Run("bytes above learned peak produces exfiltration risk", func(t *testing.T) {
		bucket := &ipBehaviorBucket{
			srcIP:       "203.0.113.11",
			countryCode: "NG",
			asn:         "AS64500",
			app:         "core-api",
			lastTS:      time.Date(2026, 5, 18, 10, 0, 0, 0, time.UTC),
			count:       10,
			bytesOut:    12 * 1024 * 1024,
			statuses:    map[int]int{200: 10},
			paths:       map[string]int{"/api/export": 10},
		}
		base := storage.BehavioralBaseline{SignalType: "ip_behavior.country_app", Dimension: "core-banking|core-api|NG", Baseline: map[string]any{
			"sample_count": int64(30),
			"bytes_out":    map[string]any{"p99": float64(2 * 1024 * 1024), "peak": float64(3 * 1024 * 1024)},
		}}
		score, category, reasons, corroborating := scoreIPBehaviorBucketWithBaselines(bucket, []storage.BehavioralBaseline{base})
		if score < 70 || category != "exfiltration_risk" || corroborating == 0 {
			t.Fatalf("score=%d category=%s corroborating=%d reasons=%v, want exfiltration_risk", score, category, corroborating, reasons)
		}
	})

	t.Run("server-error spike after suspicious paths from rare ASN produces exploit finding", func(t *testing.T) {
		bucket := &ipBehaviorBucket{
			srcIP:         "203.0.113.12",
			countryCode:   "NG",
			asn:           "AS64599",
			app:           "core-api",
			lastTS:        time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC),
			count:         4,
			sensitiveHits: 3,
			statuses:      map[int]int{500: 2, 502: 1, 200: 1},
			paths:         map[string]int{"/api/upload": 2, "/admin/import": 2},
		}
		score, category, reasons, corroborating := scoreIPBehaviorBucket(bucket)
		if score < 70 || category != "exploit_attempt" || corroborating == 0 {
			t.Fatalf("score=%d category=%s corroborating=%d reasons=%v, want exploit_attempt", score, category, corroborating, reasons)
		}
	})
}

func TestFinalPlanAcceptanceProxyTrustAndAirgappedScoring(t *testing.T) {
	t.Run("spoofed XFF is rejected when proxy CIDR is not trusted", func(t *testing.T) {
		s := &Server{}
		entry := &agentLogEntry{
			Timestamp: time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC).Format(time.RFC3339),
			Message:   `198.51.100.20 - - "GET /login HTTP/1.1" 401 120`,
			Fields: map[string]any{
				"remote_ip": "198.51.100.20",
				"xff_chain": "203.0.113.10",
				"request":   "GET /login HTTP/1.1",
				"status":    401,
				"bytes":     120,
			},
		}
		ev, ok := s.webRequestFromLog(context.Background(), uuid.New(), uuid.New(), "nginx", "/var/log/nginx/access.log", nil, entry, nil, nil)
		if !ok || ev.SrcIP != "198.51.100.20" || ev.Details["xff_spoof_rejected"] != true {
			t.Fatalf("unexpected XFF trust decision: ok=%v src=%s details=%#v", ok, ev.SrcIP, ev.Details)
		}
	})

	t.Run("airgapped bundle enriches logs and detector scores without internet", func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generate key: %v", err)
		}
		root := t.TempDir()
		keyPath := writeOfflinePublicKey(t, pub)
		now := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
		body := makeServerOfflineBundle(t, priv, "c1-airgap", "2026.05.18", 1, time.Now().UTC(), false)
		tenantID := uuid.New()
		userID := uuid.New()
		store := &offlineBundleFakeStore{fakeStore: &fakeStore{}, active: map[string]storage.OfflineContentBundle{}}
		s := &Server{
			logger:             zap.NewNop(),
			store:              store,
			cfg:                &config.Config{OfflineContent: config.OfflineContentConfig{Enabled: true, RootDir: root, PublicKeyFile: keyPath, MaxBundleBytes: 10 << 20}},
			offlineContentRoot: root,
		}
		req := httptestRequestWithPrincipal(http.MethodPost, "/api/v1/offline-bundles?tenant_id="+tenantID.String(), bytes.NewReader(body), &authPrincipalForTest{subject: userID.String(), role: roleAdmin})
		rr := httptest.NewRecorder()
		s.handleOfflineBundles(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("import status=%d body=%s", rr.Code, rr.Body.String())
		}
		events := make([]IngestedEvent, 0, 6)
		for i := 0; i < 6; i++ {
			entry := &agentLogEntry{
				Timestamp: now.Add(time.Duration(i) * time.Second).Format(time.RFC3339),
				Message:   `203.0.113.42 - - "GET /login HTTP/1.1" 401 120`,
				Fields: map[string]any{
					"remote_ip": "203.0.113.42",
					"request":   "GET /login HTTP/1.1",
					"status":    401,
					"bytes":     120,
				},
			}
			ev, ok := s.webRequestFromLog(context.Background(), tenantID, nodeIDForAcceptance, "nginx", "/var/log/nginx/access.log", nil, entry, nil, nil)
			if !ok {
				t.Fatal("expected web.request from offline-enriched log")
			}
			if ev.Details["content_bundle_id"] != "c1-airgap" || ev.Details["country_code"] != "NG" {
				t.Fatalf("missing offline provenance/enrichment: %#v", ev.Details)
			}
			events = append(events, ev)
		}
		got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeIDForAcceptance, events)
		if len(got) != 1 || got[0].Details["category"] != "credential_attack" {
			t.Fatalf("offline scoring anomalies=%d details=%#v, want credential_attack", len(got), firstEventDetails(got))
		}
	})
}

func TestFinalPlanAcceptanceBlockProposalCombinedTTL(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	actionID := uuid.New()
	instanceID := uuid.New()
	now := time.Now().UTC()
	store := &acceptanceEnforcementStore{
		fakeStore: &fakeStore{
			nodes: []storage.Node{{ID: nodeID, TenantID: tenantID, State: storage.NodeStateActive, Labels: map[string]any{"server_group": "core"}}},
		},
		instances: []storage.WebserverInstance{{
			ID:       instanceID,
			TenantID: tenantID,
			NodeID:   nodeID,
			Kind:     "nginx",
			VHosts:   []map[string]any{{"app": "core-api", "server_name": "api.bank.test"}},
		}},
	}
	s := &Server{store: store, logger: zap.NewNop()}
	expires := now.Add(15 * time.Minute)
	entry := &storage.IPBlocklistEntry{
		ID:          uuid.New(),
		TenantID:    tenantID,
		IPCIDR:      "203.0.113.77",
		Scope:       "node",
		TargetType:  "node",
		TargetID:    uuid.NullUUID{UUID: nodeID, Valid: true},
		App:         "core-api",
		VHost:       "api.bank.test",
		Enforcement: "both",
		Reason:      "acceptance malicious IP",
		ExpiresAt:   sqlNullTime(expires),
	}
	dispatched, err := s.dispatchBlockProposalToNode(context.Background(), entry, actionID, nodeID)
	if err != nil {
		t.Fatalf("dispatch combined block: %v", err)
	}
	if dispatched != 2 || len(store.rules) != 1 || len(store.actions) != 1 {
		t.Fatalf("dispatched=%d firewall_rules=%d webserver_actions=%d, want 2/1/1", dispatched, len(store.rules), len(store.actions))
	}
	policy := store.actions[0].Policy
	if policy["block_ttl_seconds"] == nil || !strings.Contains(string(mustJSON(t, policy["block_cidrs"])), "203.0.113.77") {
		t.Fatalf("webserver policy missing TTL/block CIDR: %#v", policy)
	}
}

func TestFinalPlanAcceptanceTomcatRestartRequiresApprovalWindow(t *testing.T) {
	tenantID := uuid.New()
	cfg := storage.DefaultTenantRemediationConfig(tenantID)
	cfg.ChangeWindows = []storage.ChangeWindow{{StartHour: 2, EndHour: 3, Timezone: "UTC"}}
	store := &fakeStore{remediationConfigs: map[uuid.UUID]storage.TenantRemediationConfig{tenantID: cfg}}
	s := &Server{store: store}
	instance := storage.WebserverInstance{TenantID: tenantID, NodeID: uuid.New(), Kind: "tomcat"}
	s.clockOverride = func() time.Time { return time.Date(2026, 5, 18, 4, 15, 0, 0, time.UTC) }
	if err := s.requireRestartSensitiveWebserverApproval(context.Background(), tenantID, instance, JobTypeWebserverConfigApply, map[string]any{"approved": true, "allow_restart": true}); err == nil {
		t.Fatal("expected Tomcat restart-risking action outside maintenance window to be rejected")
	}
	s.clockOverride = func() time.Time { return time.Date(2026, 5, 18, 2, 15, 0, 0, time.UTC) }
	if err := s.requireRestartSensitiveWebserverApproval(context.Background(), tenantID, instance, JobTypeWebserverConfigApply, map[string]any{"approved": true, "allow_restart": true}); err != nil {
		t.Fatalf("expected approved Tomcat action inside maintenance window, got %v", err)
	}
}

var nodeIDForAcceptance = uuid.New()

type authPrincipalForTest struct {
	subject string
	role    string
}

func httptestRequestWithPrincipal(method, target string, body *bytes.Reader, p *authPrincipalForTest) *http.Request {
	req := httptest.NewRequest(method, target, body)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: p.subject, Roles: []string{p.role}})
	return req
}

func firstEventDetails(events []IngestedEvent) map[string]any {
	if len(events) == 0 {
		return nil
	}
	return events[0].Details
}

func sqlNullTime(t time.Time) sql.NullTime {
	return sql.NullTime{Time: t, Valid: true}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

type acceptanceEnforcementStore struct {
	*fakeStore
	instances []storage.WebserverInstance
	rules     []storage.NodeFirewallRule
	actions   []storage.WebserverConfigAction
}

func (f *acceptanceEnforcementStore) CreateNodeFirewallRule(_ context.Context, in storage.NodeFirewallRuleInsert) (*storage.NodeFirewallRule, error) {
	rule := storage.NodeFirewallRule{
		ID:             uuid.New(),
		EntityActionID: in.EntityActionID,
		NodeID:         in.NodeID,
		TenantID:       in.TenantID,
		Status:         "pending",
		JobID:          nil,
	}
	f.rules = append(f.rules, rule)
	return &f.rules[len(f.rules)-1], nil
}

func (f *acceptanceEnforcementStore) SetNodeFirewallRuleJobID(_ context.Context, ruleID, jobID uuid.UUID) error {
	for i := range f.rules {
		if f.rules[i].ID == ruleID {
			f.rules[i].JobID = &jobID
		}
	}
	return nil
}

func (f *acceptanceEnforcementStore) ListWebserverInstances(_ context.Context, tenantID, nodeID uuid.UUID, limit, offset int) ([]storage.WebserverInstance, int, error) {
	var out []storage.WebserverInstance
	for _, instance := range f.instances {
		if instance.TenantID == tenantID && instance.NodeID == nodeID {
			out = append(out, instance)
		}
	}
	return out, len(out), nil
}

func (f *acceptanceEnforcementStore) GetWebserverInstance(_ context.Context, id uuid.UUID) (*storage.WebserverInstance, error) {
	for _, instance := range f.instances {
		if instance.ID == id {
			copy := instance
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *acceptanceEnforcementStore) CreateWebserverConfigAction(_ context.Context, p storage.CreateWebserverConfigActionParams) (*storage.WebserverConfigAction, error) {
	action := storage.WebserverConfigAction{
		ID:       uuid.New(),
		TenantID: p.TenantID,
		NodeID:   p.NodeID,
		Action:   p.Action,
		Policy:   p.Policy,
		Status:   "pending",
	}
	f.actions = append(f.actions, action)
	return &f.actions[len(f.actions)-1], nil
}

func (f *acceptanceEnforcementStore) UpsertWebserverInstances(context.Context, uuid.UUID, uuid.UUID, []storage.WebserverInstance) error {
	return nil
}

func (f *acceptanceEnforcementStore) CreateWebserverConfigReceipt(context.Context, storage.CreateWebserverConfigReceiptParams) (*storage.WebserverConfigReceipt, error) {
	return nil, nil
}
