package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestIPBehaviorScoringRequiresCorroboration(t *testing.T) {
	rareCountryOnly := &ipBehaviorBucket{
		srcIP:       "203.0.113.10",
		countryCode: "NG",
		lastTS:      time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC),
		count:       1,
		statuses:    map[int]int{200: 1},
		paths:       map[string]int{"/": 1},
	}
	score, _, _, corroborating := scoreIPBehaviorBucket(rareCountryOnly)
	if score >= 50 || corroborating != 0 {
		t.Fatalf("country/time alone must stay below alert threshold with no corroboration, score=%d corroborating=%d", score, corroborating)
	}

	credentialAttack := &ipBehaviorBucket{
		srcIP:       "203.0.113.10",
		countryCode: "NG",
		lastTS:      time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC),
		count:       12,
		statuses:    map[int]int{401: 10, 200: 2},
		paths:       map[string]int{"/login": 12},
	}
	score, category, _, corroborating := scoreIPBehaviorBucket(credentialAttack)
	if score < 70 || category != "credential_attack" || corroborating == 0 {
		t.Fatalf("expected high credential attack score, score=%d category=%s corroborating=%d", score, category, corroborating)
	}

	knownBad := &ipBehaviorBucket{
		srcIP:       "45.135.193.156",
		countryCode: "DE",
		lastTS:      time.Date(2026, 5, 18, 18, 0, 0, 0, time.UTC),
		count:       1,
		threatScore: 100,
		statuses:    map[int]int{403: 1},
		paths:       map[string]int{"/.env": 1},
	}
	score, category, _, corroborating = scoreIPBehaviorBucket(knownBad)
	if score != 100 || category != "known_malicious_source" || corroborating == 0 {
		t.Fatalf("expected known bad source to reach 100 confidence, score=%d category=%s corroborating=%d", score, category, corroborating)
	}
}

func TestDetectIPBehaviorUsesCurrentWindowsAcrossBatches(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	s := &Server{}
	start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)

	first := makeIPBehaviorWebRequests(start, 3)
	if got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeID, first); len(got) != 0 {
		t.Fatalf("first small batch produced %d anomalies, want 0", len(got))
	}

	second := makeIPBehaviorWebRequests(start.Add(30*time.Second), 3)
	got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeID, second)
	if len(got) != 1 {
		t.Fatalf("second batch anomalies = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.Type != "anomaly.ip_behavior" {
		t.Fatalf("event type = %q, want anomaly.ip_behavior", ev.Type)
	}
	if ev.Details["category"] != "credential_attack" {
		t.Fatalf("category = %v, want credential_attack", ev.Details["category"])
	}
	if ev.Details["request_count"] != 6 {
		t.Fatalf("request_count = %v, want 6", ev.Details["request_count"])
	}
	if ev.Details["window"] == "" {
		t.Fatal("expected window label in anomaly evidence")
	}
}

func TestRedisIPBehaviorWindowCountersRollupAndTTL(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	store := newRedisIPBehaviorWindowStore(client, time.Hour, nil)
	tenantID := uuid.New()
	nodeID := uuid.New()
	start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
	events := []IngestedEvent{
		makeIPBehaviorWebRequest(start, "203.0.113.10", "/login", 401, 1024),
		makeIPBehaviorWebRequest(start.Add(10*time.Second), "203.0.113.10", "/admin", 403, 2048),
	}

	buckets := store.addAndRollup(context.Background(), tenantID, nodeID, events)
	got := findIPBehaviorWindow(t, buckets, "1h")
	if got.count != 2 || got.bytesOut != 3072 {
		t.Fatalf("rollup count/bytes = %d/%d, want 2/3072", got.count, got.bytesOut)
	}
	if got.bytesIn != 256 {
		t.Fatalf("bytesIn = %d, want 256", got.bytesIn)
	}
	if got.statuses[401] != 1 || got.statuses[403] != 1 {
		t.Fatalf("status counts = %#v, want one 401 and one 403", got.statuses)
	}
	if got.sensitiveHits != 2 {
		t.Fatalf("sensitiveHits = %d, want 2", got.sensitiveHits)
	}
	if got.uniquePaths < 2 {
		t.Fatalf("uniquePaths = %d, want >= 2", got.uniquePaths)
	}

	keys := mr.Keys()
	var counterKey string
	for _, key := range keys {
		if strings.HasPrefix(key, "c1:ipb:v1:source_ip:") && strings.Contains(key, ":slot:") {
			counterKey = key
			break
		}
	}
	if counterKey == "" {
		t.Fatalf("expected source_ip counter key, got keys %v", keys)
	}
	if ttl := mr.TTL(counterKey); ttl <= 0 {
		t.Fatalf("counter key TTL = %s, want positive", ttl)
	}
	mr.FastForward(time.Hour + 3*time.Minute)
	if mr.Exists(counterKey) {
		t.Fatalf("counter key %s survived past TTL", counterKey)
	}
}

func TestRedisIPBehaviorWindowCountersConcurrentUpdates(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	store := newRedisIPBehaviorWindowStore(client, time.Hour, nil)
	tenantID := uuid.New()
	nodeID := uuid.New()
	start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
	base := makeIPBehaviorWebRequest(start, "203.0.113.10", "/login", 401, 100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ev := base
			ev.TS = start.Add(time.Duration(i) * time.Second)
			_ = store.addAndRollup(context.Background(), tenantID, nodeID, []IngestedEvent{ev})
		}(i)
	}
	wg.Wait()

	shape := newIPBehaviorBucketFromEvent(&base)
	prefix, ok := redisIPBehaviorCounterPrefix("source_ip", tenantID, nodeID, shape)
	if !ok {
		t.Fatal("expected source_ip prefix")
	}
	rolled, err := store.readRedisSourceWindow(context.Background(), prefix, shape, start.Add(59*time.Second), "1h", time.Hour)
	if err != nil {
		t.Fatalf("read redis source window: %v", err)
	}
	if rolled == nil || rolled.count != 20 {
		t.Fatalf("rolled count = %v, want 20", rolled)
	}
	if rolled.bytesOut != 2000 {
		t.Fatalf("rolled bytes = %d, want 2000", rolled.bytesOut)
	}
}

func TestRedisIPBehaviorWindowCountersHighCardinalityBoundedDimensions(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	store := newRedisIPBehaviorWindowStore(client, time.Hour, nil)
	tenantID := uuid.New()
	nodeID := uuid.New()
	start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
	events := make([]IngestedEvent, 0, 250)
	for i := 0; i < 250; i++ {
		src := net.IPv4(203, 0, 113, byte(i%250)).String()
		events = append(events, makeIPBehaviorWebRequest(start, src, "/session/start", 200, 128))
	}
	_ = store.addAndRollup(context.Background(), tenantID, nodeID, events)

	var sourceSlotKeys, aggregateSlotKeys int
	for _, key := range mr.Keys() {
		if !strings.Contains(key, ":slot:") {
			continue
		}
		if strings.Contains(key, ":source_ip:") {
			sourceSlotKeys++
		} else {
			aggregateSlotKeys++
		}
	}
	if sourceSlotKeys != 250 {
		t.Fatalf("source slot keys = %d, want 250", sourceSlotKeys)
	}
	if aggregateSlotKeys > 4 {
		t.Fatalf("aggregate slot keys = %d, want bounded dimensions <= 4", aggregateSlotKeys)
	}
}

func makeIPBehaviorWebRequests(ts time.Time, count int) []IngestedEvent {
	out := make([]IngestedEvent, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, makeIPBehaviorWebRequest(ts.Add(time.Duration(i)*time.Second), "203.0.113.10", "/session/start", 401, 512))
	}
	return out
}

func makeIPBehaviorWebRequest(ts time.Time, srcIP, path string, status int, bytesOut int64) IngestedEvent {
	return IngestedEvent{
		Type:     "web.request",
		TS:       ts,
		SrcIP:    srcIP,
		BytesIn:  128,
		BytesOut: bytesOut,
		Details: map[string]any{
			"app":          "core-api",
			"server_group": "core-banking",
			"country_code": "NG",
			"country":      "Nigeria",
			"asn":          "AS64500",
			"path":         path,
			"status_code":  status,
		},
	}
}

func findIPBehaviorWindow(t *testing.T, buckets []*ipBehaviorBucket, window string) *ipBehaviorBucket {
	t.Helper()
	for _, b := range buckets {
		if b.windowLabel == window {
			return b
		}
	}
	t.Fatalf("missing %s window in %#v", window, buckets)
	return nil
}

func TestRecordBlockProposalEntityActionUsesEntryTTL(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
	expiresAt := now.Add(15 * time.Minute)
	tenantID := uuid.New()
	approverID := uuid.New()
	store := &blockProposalInvestigateStore{fakeStore: &fakeStore{}}
	s := &Server{store: store}
	entry := &storage.IPBlocklistEntry{
		ID:        uuid.New(),
		TenantID:  tenantID,
		IPCIDR:    "203.0.113.10",
		Reason:    "credential stuffing from unusual country",
		ExpiresAt: sql.NullTime{Time: expiresAt, Valid: true},
	}

	row, err := s.recordBlockProposalEntityAction(context.Background(), entry, &approverID, now)
	if err != nil {
		t.Fatalf("record entity action: %v", err)
	}
	if row.ID == uuid.Nil {
		t.Fatal("expected entity action id")
	}
	if len(store.recorded) != 1 {
		t.Fatalf("recorded actions = %d, want 1", len(store.recorded))
	}
	got := store.recorded[0]
	if got.TenantID != tenantID || got.EntityType != "ip" || got.EntityID != entry.IPCIDR || got.Action != "block" {
		t.Fatalf("unexpected entity action: %#v", got)
	}
	if got.CreatedBy == nil || *got.CreatedBy != approverID {
		t.Fatalf("CreatedBy = %v, want %s", got.CreatedBy, approverID)
	}
	if got.TTLSeconds == nil || *got.TTLSeconds != 900 {
		t.Fatalf("TTLSeconds = %v, want 900", got.TTLSeconds)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("ExpiresAt = %v, want %s", got.ExpiresAt, expiresAt)
	}
}

func TestQueueFirewallRemovalUsesOriginalBlockRuleShape(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	entityActionID := uuid.New()
	ruleID := uuid.New()
	nodeID := uuid.New()
	src := "203.0.113.10"
	store := &blockProposalExpiryStore{
		fakeStore: &fakeStore{},
		rules: []storage.NodeFirewallRule{{
			ID:             ruleID,
			EntityActionID: entityActionID,
			NodeID:         nodeID,
			TenantID:       tenantID,
			Action:         "block",
			Direction:      "in",
			Source:         &src,
			Tag:            "c1-" + entityActionID.String(),
			Status:         "applied",
		}},
	}
	s := &Server{store: store}

	queued, err := s.queueFirewallRemovalForBlockEntry(context.Background(), &storage.IPBlocklistEntry{
		ID:       uuid.New(),
		TenantID: tenantID,
		IPCIDR:   src,
		Reason:   "ttl expired",
	}, entityActionID)
	if err != nil {
		t.Fatalf("queue firewall removal: %v", err)
	}
	if queued != 1 || len(store.queued) != 1 {
		t.Fatalf("queued = %d store.queued=%d, want 1", queued, len(store.queued))
	}
	job := store.jobs[store.queued[ruleID]]
	if job == nil {
		t.Fatalf("missing queued job")
	}
	if job.Type != JobTypeFirewallRuleDelete {
		t.Fatalf("job type = %q, want %s", job.Type, JobTypeFirewallRuleDelete)
	}
	var payload firewallJobPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode job payload: %v", err)
	}
	if payload.Action != "block" {
		t.Fatalf("payload action = %q, want original block shape", payload.Action)
	}
	if payload.Tag != "c1-"+entityActionID.String() || payload.Source != src {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestProtectedIPBlockReasonUsesTenantAllowlistAndAssetCIDRs(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	_, asset, err := net.ParseCIDR("203.0.113.0/24")
	if err != nil {
		t.Fatalf("parse asset cidr: %v", err)
	}
	store := &blockProposalInvestigateStore{
		fakeStore: &fakeStore{
			eventFilters: map[uuid.UUID]storage.TenantEventFilters{
				tenantID: {
					TenantID:       tenantID,
					AllowlistCIDRs: []string{"198.51.100.42/32"},
				},
			},
		},
		assets: []net.IPNet{*asset},
	}
	s := &Server{store: store}

	if got := s.protectedIPBlockReason(context.Background(), tenantID, "198.51.100.0/24"); !strings.Contains(got, "tenant allowlist") {
		t.Fatalf("allowlisted overlap reason = %q", got)
	}
	if got := s.protectedIPBlockReason(context.Background(), tenantID, "203.0.113.10"); !strings.Contains(got, "tenant asset CIDR") {
		t.Fatalf("asset overlap reason = %q", got)
	}
	if got := s.protectedIPBlockReason(context.Background(), tenantID, "192.0.2.10"); got != "" {
		t.Fatalf("unexpected protected reason for external test IP: %q", got)
	}
}

func TestBlockProposalSafetyStopsRateLimitAndOpenCircuit(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	store := &blockProposalInvestigateStore{
		fakeStore:    &fakeStore{},
		recentBlocks: 100,
		globalBlocks: 999,
	}
	s := &Server{store: store}

	status, msg := s.blockProposalSafetyViolation(context.Background(), tenantID, "")
	if status != 429 || !strings.Contains(msg, "rate limit") {
		t.Fatalf("safety violation = %d %q, want rate limit", status, msg)
	}

	store.recentBlocks = 0
	store.globalBlocks = 1000
	status, msg = s.blockProposalSafetyViolation(context.Background(), tenantID, "")
	if status != 429 || !strings.Contains(msg, "global") {
		t.Fatalf("safety violation = %d %q, want global rate limit", status, msg)
	}

	store.globalBlocks = 0
	if _, err := store.TripCircuitBreaker(context.Background(), tenantID, networkBlockCircuitRuleID, "feed runaway"); err != nil {
		t.Fatalf("trip breaker: %v", err)
	}
	status, msg = s.blockProposalSafetyViolation(context.Background(), tenantID, "")
	if status != 409 || !strings.Contains(msg, "feed runaway") {
		t.Fatalf("safety violation = %d %q, want open circuit", status, msg)
	}
}

func TestBlockProposalSafetyStopsServerGroupRateLimit(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	store := &blockProposalInvestigateStore{
		fakeStore:    &fakeStore{},
		globalBlocks: 0,
		groupBlocks:  map[string]int{"core-banking": 25},
	}
	s := &Server{store: store}

	status, msg := s.blockProposalSafetyViolation(context.Background(), tenantID, "core-banking")
	if status != 429 || !strings.Contains(msg, "core-banking") {
		t.Fatalf("safety violation = %d %q, want server-group rate limit", status, msg)
	}

	status, msg = s.blockProposalSafetyViolation(context.Background(), tenantID, "dmz")
	if status != 0 || msg != "" {
		t.Fatalf("unexpected safety violation for different group = %d %q", status, msg)
	}
}

func TestBlockProposalProtectedCIDRRequiresExplicitAdminOverride(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	_, protectedNet, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("parse protected cidr: %v", err)
	}
	store := &blockProposalInvestigateStore{
		fakeStore: &fakeStore{},
		assets:    []net.IPNet{*protectedNet},
	}
	s := &Server{store: store}

	body := []byte(`{"tenant_id":"` + tenantID.String() + `","ip_cidr":"10.12.0.10","target_type":"tenant","scope":"tenant","enforcement":"firewall","reason":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals", bytes.NewReader(body))
	req = withPrincipal(req, operatorPrincipal())
	rr := httptest.NewRecorder()
	s.handleBlockProposals(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want protected CIDR conflict", rr.Code)
	}
	if len(store.createdBlocks) != 0 {
		t.Fatalf("created protected block without override: %#v", store.createdBlocks)
	}

	overrideBody := []byte(`{"tenant_id":"` + tenantID.String() + `","ip_cidr":"10.12.0.10","target_type":"tenant","scope":"tenant","enforcement":"firewall","reason":"test","protected_override":true,"protected_override_reason":"emergency isolation of compromised internal host"}`)
	admin := operatorPrincipal()
	admin.Roles = []string{roleAdmin}
	req = httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals", bytes.NewReader(overrideBody))
	req = withPrincipal(req, admin)
	rr = httptest.NewRecorder()
	s.handleBlockProposals(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("override status = %d body=%q, want accepted", rr.Code, rr.Body.String())
	}
	if len(store.createdBlocks) != 1 {
		t.Fatalf("created blocks = %d, want 1", len(store.createdBlocks))
	}
	created := store.createdBlocks[0]
	if !created.ProtectedOverride || !strings.Contains(created.ProtectedOverrideReason, "emergency isolation") {
		t.Fatalf("created block missing protected override evidence: %#v", created)
	}
	foundAudit := false
	for _, entry := range store.auditLogs {
		if entry.Action == "network.block_proposal.protected_override" && entry.TenantID == tenantID {
			foundAudit = true
			break
		}
	}
	if !foundAudit {
		t.Fatalf("expected protected override audit, got %#v", store.auditLogs)
	}
}

func TestValidateBlockProposalTTLAllowsOnlyApprovedWindows(t *testing.T) {
	t.Parallel()

	for _, ttl := range []int{0, 900, 3600, 86400} {
		if err := validateBlockProposalTTL(ttl); err != nil {
			t.Fatalf("ttl %d rejected: %v", ttl, err)
		}
	}
	for _, ttl := range []int{-1, 60, 1800, 7200} {
		if err := validateBlockProposalTTL(ttl); err == nil {
			t.Fatalf("ttl %d accepted, want rejection", ttl)
		}
	}
}

func TestASNBlockProposalCreatesCappedPerIPProposals(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	now := time.Date(2026, 5, 18, 4, 0, 0, 0, time.UTC)
	store := &blockProposalInvestigateStore{
		fakeStore: &fakeStore{},
		asnSources: []storage.IPBehaviorASNSource{
			{
				SourceIP:     "203.0.113.10",
				RequestCount: 120,
				BytesOut:     4_096,
				Countries:    []string{"NG"},
				Apps:         []string{"core-api"},
				ServerGroups: []string{"core"},
				FirstSeenAt:  now.Add(-time.Hour),
				LastSeenAt:   now,
			},
			{
				SourceIP:     "2001:db8::10",
				RequestCount: 80,
				BytesOut:     2_048,
				Countries:    []string{"NG"},
				Apps:         []string{"core-api"},
				ServerGroups: []string{"core"},
				FirstSeenAt:  now.Add(-time.Hour),
				LastSeenAt:   now,
			},
		},
	}
	s := &Server{store: store}
	body := []byte(`{"tenant_id":"` + tenantID.String() + `","asn":"AS64500","limit":1,"ttl_seconds":3600,"scope":"app","target_type":"tenant","server_group":"core","app":"core-api","vhost":"api.bank.test","enforcement":"combined","reason":"credential stuffing across ASN"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals/asn", bytes.NewReader(body))
	req = withPrincipal(req, operatorPrincipal())
	rr := httptest.NewRecorder()

	s.handleCreateASNBlockProposals(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%q, want accepted", rr.Code, rr.Body.String())
	}
	var resp asnBlockProposalsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ASN != "AS64500" || len(resp.Created) != 1 || resp.TotalCandidates != 1 {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if len(store.createdBlocks) != 1 {
		t.Fatalf("created blocks = %d, want 1", len(store.createdBlocks))
	}
	created := store.createdBlocks[0]
	if created.IPCIDR != "203.0.113.10/32" || created.Scope != "app" || created.TargetType != "tenant" {
		t.Fatalf("unexpected created block scope: %#v", created)
	}
	if created.App != "core-api" || created.VHost != "api.bank.test" || created.Enforcement != "combined" {
		t.Fatalf("unexpected app/vhost/enforcement: %#v", created)
	}
	if !created.ExpiresAt.Valid {
		t.Fatal("expected ttl-backed expiry")
	}
	if !strings.Contains(created.Reason, "ASN AS64500") || !strings.Contains(created.Reason, "requests=120") {
		t.Fatalf("reason missing ASN evidence: %q", created.Reason)
	}
}

func TestBlockProposalListRejectAndRollbackLifecycle(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actionID := uuid.New()
	nodeID := uuid.New()
	store := &blockProposalInvestigateStore{
		fakeStore: &fakeStore{},
		createdBlocks: []storage.IPBlocklistEntry{
			{
				ID:          uuid.New(),
				TenantID:    tenantID,
				IPCIDR:      "203.0.113.10",
				TargetType:  "tenant",
				Scope:       "tenant",
				Enforcement: "firewall",
				Status:      "proposed",
				Reason:      "operator review",
				CreatedAt:   time.Now().UTC(),
				UpdatedAt:   time.Now().UTC(),
			},
			{
				ID:             uuid.New(),
				TenantID:       tenantID,
				EntityActionID: uuid.NullUUID{UUID: actionID, Valid: true},
				IPCIDR:         "203.0.113.20",
				TargetType:     "tenant",
				Scope:          "tenant",
				Enforcement:    "firewall",
				Status:         "active",
				Reason:         "confirmed attack",
				CreatedAt:      time.Now().UTC(),
				UpdatedAt:      time.Now().UTC(),
			},
		},
		rules: []storage.NodeFirewallRule{{
			ID:             uuid.New(),
			EntityActionID: actionID,
			NodeID:         nodeID,
			TenantID:       tenantID,
			Status:         "applied",
		}},
	}
	s := &Server{store: store}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/network/block-proposals?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, viewerPrincipal())
	rr := httptest.NewRecorder()
	s.handleBlockProposals(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%q, want OK", rr.Code, rr.Body.String())
	}
	var listed struct {
		Data []blockProposalResponse `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Data) != 2 {
		t.Fatalf("listed proposals = %d, want 2", len(listed.Data))
	}

	rejectID := store.createdBlocks[0].ID
	req = httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals/"+rejectID.String()+"/reject", bytes.NewReader([]byte(`{"reason":"false positive partner traffic"}`)))
	req = withPrincipal(req, operatorPrincipal())
	rr = httptest.NewRecorder()
	s.handleBlockProposalSubroutes(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("reject status = %d body=%q, want accepted", rr.Code, rr.Body.String())
	}
	if store.createdBlocks[0].Status != "rejected" || !store.createdBlocks[0].LastError.Valid {
		t.Fatalf("rejected block not updated: %#v", store.createdBlocks[0])
	}

	rollbackID := store.createdBlocks[1].ID
	req = httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals/"+rollbackID.String()+"/rollback", bytes.NewReader([]byte(`{"reason":"incident contained"}`)))
	req = withPrincipal(req, operatorPrincipal())
	rr = httptest.NewRecorder()
	s.handleBlockProposalSubroutes(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("rollback status = %d body=%q, want accepted", rr.Code, rr.Body.String())
	}
	if store.createdBlocks[1].Status != "rolled_back" {
		t.Fatalf("rollback block status = %q, want rolled_back", store.createdBlocks[1].Status)
	}
	if len(store.queued) != 1 {
		t.Fatalf("queued firewall removals = %d, want 1", len(store.queued))
	}
}

func TestIPBehaviorAPIOverviewAndIPValidation(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	store := &ipBehaviorAPIStore{
		fakeStore: &fakeStore{},
		countries: []storage.IPBehaviorCountrySummary{{
			CountryCode:     "NG",
			Country:         "Nigeria",
			UniqueSourceIPs: 3,
			RequestCount:    42,
			BytesOut:        4096,
			StatusCounts:    map[string]int64{"401": 12},
			FirstSeenAt:     time.Now().UTC().Add(-time.Hour),
			LastSeenAt:      time.Now().UTC(),
		}},
	}
	s := &Server{store: store, logger: zap.NewNop()}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ip-behavior/overview?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, viewerPrincipal())
	rr := httptest.NewRecorder()
	s.handleIPBehaviorOverview(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("overview status = %d body=%q, want OK", rr.Code, rr.Body.String())
	}
	var overview map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &overview); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if overview["request_count"].(float64) != 42 {
		t.Fatalf("request_count = %v, want 42", overview["request_count"])
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/ip-behavior/ips/not-an-ip?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, viewerPrincipal())
	rr = httptest.NewRecorder()
	s.handleIPBehaviorIPProfile(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid ip status = %d, want 400", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/ip-behavior/ips/203.0.113.44?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, viewerPrincipal())
	rr = httptest.NewRecorder()
	s.handleIPBehaviorIPProfile(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("missing profile status = %d body=%q, want OK", rr.Code, rr.Body.String())
	}
	var profile storage.IPBehaviorIPProfile
	if err := json.Unmarshal(rr.Body.Bytes(), &profile); err != nil {
		t.Fatalf("decode empty profile: %v", err)
	}
	if profile.SourceIP != "203.0.113.44" || profile.RequestCount != 0 || profile.StatusCounts["404"] != 0 {
		t.Fatalf("unexpected empty profile: %+v", profile)
	}
}

func TestRefreshBlockProposalEnforcementStatusMarksActiveAndFailed(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actionID := uuid.New()
	entryID := uuid.New()
	store := &blockProposalInvestigateStore{
		fakeStore: &fakeStore{},
		createdBlocks: []storage.IPBlocklistEntry{{
			ID:             entryID,
			TenantID:       tenantID,
			EntityActionID: uuid.NullUUID{UUID: actionID, Valid: true},
			IPCIDR:         "203.0.113.30",
			Enforcement:    "firewall",
			Status:         "dispatching",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		}},
		rules: []storage.NodeFirewallRule{{
			ID:             uuid.New(),
			EntityActionID: actionID,
			NodeID:         uuid.New(),
			TenantID:       tenantID,
			Status:         "applied",
		}},
	}
	s := &Server{store: store}

	s.refreshBlockProposalEnforcementStatusByEntityAction(context.Background(), actionID)
	if store.createdBlocks[0].Status != "active" {
		t.Fatalf("status = %q, want active", store.createdBlocks[0].Status)
	}

	failActionID := uuid.New()
	errMsg := "nftables rejected rule"
	store.createdBlocks = append(store.createdBlocks, storage.IPBlocklistEntry{
		ID:             uuid.New(),
		TenantID:       tenantID,
		EntityActionID: uuid.NullUUID{UUID: failActionID, Valid: true},
		IPCIDR:         "203.0.113.40",
		Enforcement:    "firewall",
		Status:         "dispatching",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	store.rules = append(store.rules, storage.NodeFirewallRule{
		ID:             uuid.New(),
		EntityActionID: failActionID,
		NodeID:         uuid.New(),
		TenantID:       tenantID,
		Status:         "failed",
		Error:          &errMsg,
	})
	s.refreshBlockProposalEnforcementStatusByEntityAction(context.Background(), failActionID)
	if got := store.createdBlocks[1]; got.Status != "failed" || !strings.Contains(got.LastError.String, errMsg) {
		t.Fatalf("failed status = %#v, want failed with firewall error", got)
	}
}

func TestWebserverEnforcementCircuitTripsAfterRepeatedFailures(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &webserverSafetyFakeStore{
		fakeStore:      &fakeStore{},
		recentFailures: 3,
	}
	s := &Server{store: store}

	s.maybeTripWebserverEnforcementCircuit(context.Background(), &storage.WebserverConfigAction{
		TenantID: tenantID,
		NodeID:   nodeID,
		Action:   JobTypeWebserverBlocklistUpdate,
	}, "reload failed")

	reason := s.openEnforcementCircuitReason(context.Background(), tenantID, webserverEnforcementCircuitRuleID(nodeID))
	if !strings.Contains(reason, "reload failed") {
		t.Fatalf("breaker reason = %q, want reload failure context", reason)
	}
	if err := s.ensureWebserverEnforcementCircuitClosed(context.Background(), tenantID, nodeID); err == nil {
		t.Fatalf("expected webserver enforcement gate to reject open circuit")
	}
}

func TestTomcatWebserverActionRequiresApprovalAndMaintenanceWindow(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	cfg := storage.DefaultTenantRemediationConfig(tenantID)
	cfg.ChangeWindows = []storage.ChangeWindow{{StartHour: 2, EndHour: 3, Timezone: "UTC"}}
	store := &fakeStore{
		remediationConfigs: map[uuid.UUID]storage.TenantRemediationConfig{tenantID: cfg},
	}
	s := &Server{store: store}
	instance := storage.WebserverInstance{TenantID: tenantID, NodeID: uuid.New(), Kind: "tomcat"}

	s.clockOverride = func() time.Time { return time.Date(2026, 5, 18, 2, 15, 0, 0, time.UTC) }
	if err := s.requireRestartSensitiveWebserverApproval(context.Background(), tenantID, instance, JobTypeWebserverConfigApply, map[string]any{"allow_restart": true}); err == nil || !strings.Contains(err.Error(), "explicit approval") {
		t.Fatalf("expected explicit approval error, got %v", err)
	}

	s.clockOverride = func() time.Time { return time.Date(2026, 5, 18, 4, 15, 0, 0, time.UTC) }
	if err := s.requireRestartSensitiveWebserverApproval(context.Background(), tenantID, instance, JobTypeWebserverConfigApply, map[string]any{"approved": true, "allow_restart": true}); err == nil || !strings.Contains(err.Error(), "maintenance window") {
		t.Fatalf("expected maintenance window error, got %v", err)
	}

	policy := map[string]any{"approved": true, "allow_restart": true}
	s.clockOverride = func() time.Time { return time.Date(2026, 5, 18, 2, 15, 0, 0, time.UTC) }
	if err := s.requireRestartSensitiveWebserverApproval(context.Background(), tenantID, instance, JobTypeWebserverConfigApply, policy); err != nil {
		t.Fatalf("expected approved maintenance-window action, got %v", err)
	}
	if policy["maintenance_window_approved"] != true {
		t.Fatalf("server did not stamp maintenance_window_approved: %#v", policy)
	}
	if !webserverAutoEnforcementRequiresMaintenance(instance) {
		t.Fatalf("tomcat should be skipped by auto webserver enforcement")
	}
}

func TestBlockProposalCanaryDispatchesOneNodePerServerGroup(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actionID := uuid.New()
	nodes := []storage.Node{
		{ID: uuid.New(), TenantID: tenantID, Hostname: "core-a", State: storage.NodeStateActive, Labels: map[string]any{"server_group": "core"}},
		{ID: uuid.New(), TenantID: tenantID, Hostname: "core-b", State: storage.NodeStateActive, Labels: map[string]any{"server_group": "core"}},
		{ID: uuid.New(), TenantID: tenantID, Hostname: "dmz-a", State: storage.NodeStateActive, Labels: map[string]any{"server_group": "dmz"}},
		{ID: uuid.New(), TenantID: tenantID, Hostname: "retired", State: storage.NodeStateRetired, Labels: map[string]any{"server_group": "dmz"}},
	}
	store := &blockProposalCanaryStore{fakeStore: &fakeStore{nodes: nodes}}
	s := &Server{
		store: store,
		cfg: &config.Config{Remediation: config.RemediationConfig{
			BlockCanaryEnabled:             true,
			BlockCanaryNodesPerServerGroup: 1,
		}},
	}
	entry := &storage.IPBlocklistEntry{
		ID:          uuid.New(),
		TenantID:    tenantID,
		IPCIDR:      "203.0.113.10",
		TargetType:  "tenant",
		Scope:       "tenant",
		Enforcement: "firewall",
		Reason:      "canary test",
	}

	dispatched, groups, err := s.dispatchBlockProposalCanaryToTenantNodes(context.Background(), entry, actionID)
	if err != nil {
		t.Fatalf("dispatch canary: %v", err)
	}
	if dispatched != 2 {
		t.Fatalf("dispatched = %d, want 2", dispatched)
	}
	if strings.Join(groups, ",") != "core,dmz" {
		t.Fatalf("groups = %v, want core/dmz", groups)
	}
	if len(store.rules) != 2 {
		t.Fatalf("rules = %d, want 2", len(store.rules))
	}
	for _, rule := range store.rules {
		if rule.NodeID == nodes[1].ID || rule.NodeID == nodes[3].ID {
			t.Fatalf("unexpected canary node selected: %#v", rule)
		}
	}
}

func TestBlockProposalServerGroupDispatchOnlyTargetsMatchingNodes(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actionID := uuid.New()
	nodes := []storage.Node{
		{ID: uuid.New(), TenantID: tenantID, Hostname: "core-a", State: storage.NodeStateActive, Labels: map[string]any{"server_group": "core"}},
		{ID: uuid.New(), TenantID: tenantID, Hostname: "dmz-a", State: storage.NodeStateActive, Labels: map[string]any{"server_group": "dmz"}},
	}
	store := &blockProposalCanaryStore{fakeStore: &fakeStore{nodes: nodes}}
	s := &Server{store: store}
	entry := &storage.IPBlocklistEntry{
		ID:          uuid.New(),
		TenantID:    tenantID,
		IPCIDR:      "203.0.113.10",
		TargetType:  "tenant",
		Scope:       "tenant",
		ServerGroup: "core",
		Enforcement: "firewall",
		Reason:      "server-group test",
	}

	dispatched, err := s.dispatchBlockProposalToTenantNodes(context.Background(), entry, actionID)
	if err != nil {
		t.Fatalf("dispatch tenant block: %v", err)
	}
	if dispatched != 1 || len(store.rules) != 1 {
		t.Fatalf("dispatched/rules = %d/%d, want 1/1", dispatched, len(store.rules))
	}
	if store.rules[0].NodeID != nodes[0].ID {
		t.Fatalf("rule node = %s, want core node %s", store.rules[0].NodeID, nodes[0].ID)
	}
}

func TestBlockProposalCanaryStopsOnFailedFirewallRule(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	actionID := uuid.New()
	errMsg := "iptables failed"
	store := &blockProposalExpiryStore{
		fakeStore: &fakeStore{},
		rules: []storage.NodeFirewallRule{{
			ID:             uuid.New(),
			EntityActionID: actionID,
			NodeID:         uuid.New(),
			TenantID:       tenantID,
			Status:         "failed",
			Error:          &errMsg,
		}},
	}
	s := &Server{store: store}
	reason := s.blockProposalCanaryFailureReason(context.Background(), &storage.IPBlocklistEntry{
		ID:             uuid.New(),
		TenantID:       tenantID,
		EntityActionID: uuid.NullUUID{UUID: actionID, Valid: true},
	})
	if !strings.Contains(reason, errMsg) {
		t.Fatalf("canary failure reason = %q, want firewall error", reason)
	}
}

func TestBlockProposalWebserverInstanceScopeHonorsAppAndVHost(t *testing.T) {
	t.Parallel()

	instance := storage.WebserverInstance{
		Kind:        "nginx",
		ServiceName: "edge-proxy",
		VHosts: []map[string]any{{
			"server_name": []any{"core.example.test", "api.example.test"},
			"app":         "core-banking",
		}},
	}
	matching := &storage.IPBlocklistEntry{App: "core-banking", VHost: "api.example.test"}
	if !blockProposalMatchesWebserverInstance(matching, instance) {
		t.Fatal("expected app/vhost scoped proposal to match instance")
	}
	wrongVHost := &storage.IPBlocklistEntry{App: "core-banking", VHost: "cards.example.test"}
	if blockProposalMatchesWebserverInstance(wrongVHost, instance) {
		t.Fatal("unexpected match for different vhost")
	}
	wrongApp := &storage.IPBlocklistEntry{App: "payments", VHost: "api.example.test"}
	if blockProposalMatchesWebserverInstance(wrongApp, instance) {
		t.Fatal("unexpected match for different app")
	}
}

type blockProposalInvestigateStore struct {
	*fakeStore
	recorded      []storage.EntityAction
	assets        []net.IPNet
	recentBlocks  int
	globalBlocks  int
	groupBlocks   map[string]int
	createdBlocks []storage.IPBlocklistEntry
	asnSources    []storage.IPBehaviorASNSource
	rules         []storage.NodeFirewallRule
	queued        map[uuid.UUID]uuid.UUID
}

type ipBehaviorAPIStore struct {
	*fakeStore
	countries []storage.IPBehaviorCountrySummary
	profile   *storage.IPBehaviorIPProfile
	baselines []storage.IPBehaviorBaseline
}

func (f *ipBehaviorAPIStore) ListIPBehaviorCountries(_ context.Context, _ uuid.UUID, _ time.Time, code string) ([]storage.IPBehaviorCountrySummary, error) {
	if strings.TrimSpace(code) == "" {
		return f.countries, nil
	}
	var out []storage.IPBehaviorCountrySummary
	for _, country := range f.countries {
		if strings.EqualFold(country.CountryCode, code) {
			out = append(out, country)
		}
	}
	return out, nil
}

func (f *ipBehaviorAPIStore) GetIPBehaviorIPProfile(_ context.Context, _ uuid.UUID, ip string, _ time.Time) (*storage.IPBehaviorIPProfile, error) {
	if f.profile != nil && f.profile.SourceIP == ip {
		return f.profile, nil
	}
	return nil, nil
}

func (f *ipBehaviorAPIStore) ListIPBehaviorBaselines(_ context.Context, _ uuid.UUID, _ string, limit, offset int) ([]storage.IPBehaviorBaseline, int, error) {
	total := len(f.baselines)
	if limit <= 0 {
		limit = 50
	}
	if offset > total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return f.baselines[offset:end], total, nil
}

type blockProposalExpiryStore struct {
	*fakeStore
	rules  []storage.NodeFirewallRule
	queued map[uuid.UUID]uuid.UUID
}

type blockProposalCanaryStore struct {
	*fakeStore
	rules []storage.NodeFirewallRule
}

func (f *blockProposalCanaryStore) CreateNodeFirewallRule(_ context.Context, in storage.NodeFirewallRuleInsert) (*storage.NodeFirewallRule, error) {
	rule := storage.NodeFirewallRule{
		ID:             uuid.New(),
		EntityActionID: in.EntityActionID,
		NodeID:         in.NodeID,
		TenantID:       in.TenantID,
		Action:         in.Action,
		Direction:      in.Direction,
		Protocol:       in.Protocol,
		Port:           in.Port,
		Source:         in.Source,
		Dest:           in.Dest,
		Tag:            in.Tag,
		Status:         "pending",
		RequestedAt:    time.Now().UTC(),
	}
	f.rules = append(f.rules, rule)
	return &rule, nil
}

func (f *blockProposalCanaryStore) SetNodeFirewallRuleJobID(_ context.Context, ruleID, jobID uuid.UUID) error {
	for i := range f.rules {
		if f.rules[i].ID == ruleID {
			f.rules[i].JobID = &jobID
			return nil
		}
	}
	return nil
}

func (f *blockProposalCanaryStore) ListNodeFirewallRulesForEntityAction(_ context.Context, entityActionID uuid.UUID) ([]storage.NodeFirewallRule, error) {
	var out []storage.NodeFirewallRule
	for _, rule := range f.rules {
		if rule.EntityActionID == entityActionID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (f *blockProposalExpiryStore) ListNodeFirewallRulesForEntityAction(_ context.Context, entityActionID uuid.UUID) ([]storage.NodeFirewallRule, error) {
	var out []storage.NodeFirewallRule
	for _, rule := range f.rules {
		if rule.EntityActionID == entityActionID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (f *blockProposalExpiryStore) QueueNodeFirewallRuleRemoval(_ context.Context, ruleID, jobID uuid.UUID) error {
	if f.queued == nil {
		f.queued = map[uuid.UUID]uuid.UUID{}
	}
	f.queued[ruleID] = jobID
	return nil
}

func (f *blockProposalInvestigateStore) CreateSavedSearch(context.Context, storage.SavedSearch) (*storage.SavedSearch, error) {
	return nil, nil
}

func (f *blockProposalInvestigateStore) ListSavedSearches(context.Context, uuid.UUID, uuid.UUID, int, int) ([]storage.SavedSearch, int, error) {
	return nil, 0, nil
}

func (f *blockProposalInvestigateStore) GetSavedSearch(context.Context, uuid.UUID) (*storage.SavedSearch, error) {
	return nil, nil
}

func (f *blockProposalInvestigateStore) UpdateSavedSearch(context.Context, uuid.UUID, storage.SavedSearch) (*storage.SavedSearch, error) {
	return nil, nil
}

func (f *blockProposalInvestigateStore) DeleteSavedSearch(context.Context, uuid.UUID) error {
	return nil
}

func (f *blockProposalInvestigateStore) AddEntityTag(context.Context, storage.EntityTag) (*storage.EntityTag, error) {
	return nil, nil
}

func (f *blockProposalInvestigateStore) RemoveEntityTag(context.Context, uuid.UUID, string, string, string) error {
	return nil
}

func (f *blockProposalInvestigateStore) ListEntityTags(context.Context, uuid.UUID, string, string) ([]storage.EntityTag, error) {
	return nil, nil
}

func (f *blockProposalInvestigateStore) CountRecentIPBlocklistEntries(context.Context, uuid.UUID, time.Time) (int, error) {
	return f.recentBlocks, nil
}

func (f *blockProposalInvestigateStore) CountRecentIPBlocklistEntriesForServerGroup(_ context.Context, _ uuid.UUID, serverGroup string, _ time.Time) (int, error) {
	if f.groupBlocks == nil {
		return 0, nil
	}
	return f.groupBlocks[strings.TrimSpace(serverGroup)], nil
}

func (f *blockProposalInvestigateStore) CountRecentIPBlocklistEntriesGlobal(context.Context, time.Time) (int, error) {
	return f.globalBlocks, nil
}

func (f *blockProposalInvestigateStore) RecordEntityAction(_ context.Context, a storage.EntityAction) (*storage.EntityAction, error) {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	f.recorded = append(f.recorded, a)
	return &a, nil
}

func (f *blockProposalInvestigateStore) CreateIPBlocklistEntry(_ context.Context, p storage.CreateIPBlocklistEntryParams) (*storage.IPBlocklistEntry, error) {
	entry := storage.IPBlocklistEntry{
		ID:                      uuid.New(),
		TenantID:                p.TenantID,
		IPCIDR:                  strings.TrimSpace(p.IPCIDR),
		Scope:                   p.Scope,
		TargetType:              p.TargetType,
		ServerGroup:             strings.TrimSpace(p.ServerGroup),
		App:                     strings.TrimSpace(p.App),
		VHost:                   strings.TrimSpace(p.VHost),
		Enforcement:             p.Enforcement,
		Status:                  "proposed",
		Reason:                  p.Reason,
		Score:                   p.Score,
		ProtectedOverride:       p.ProtectedOverride,
		ProtectedOverrideReason: p.ProtectedOverrideReason,
		CreatedAt:               time.Now().UTC(),
		UpdatedAt:               time.Now().UTC(),
	}
	if p.FindingID != nil {
		entry.FindingID = uuid.NullUUID{UUID: *p.FindingID, Valid: true}
	}
	if p.TargetID != nil {
		entry.TargetID = uuid.NullUUID{UUID: *p.TargetID, Valid: true}
	}
	if p.ExpiresAt != nil {
		entry.ExpiresAt = sql.NullTime{Time: *p.ExpiresAt, Valid: true}
	}
	f.createdBlocks = append(f.createdBlocks, entry)
	return &entry, nil
}

func (f *blockProposalInvestigateStore) GetIPBlocklistEntry(_ context.Context, id uuid.UUID) (*storage.IPBlocklistEntry, error) {
	for i := range f.createdBlocks {
		if f.createdBlocks[i].ID == id {
			entry := f.createdBlocks[i]
			return &entry, nil
		}
	}
	return nil, nil
}

func (f *blockProposalInvestigateStore) GetIPBlocklistEntryByEntityAction(_ context.Context, entityActionID uuid.UUID) (*storage.IPBlocklistEntry, error) {
	for i := range f.createdBlocks {
		if f.createdBlocks[i].EntityActionID.Valid && f.createdBlocks[i].EntityActionID.UUID == entityActionID {
			entry := f.createdBlocks[i]
			return &entry, nil
		}
	}
	return nil, nil
}

func (f *blockProposalInvestigateStore) ListIPBlocklistEntries(_ context.Context, filter storage.IPBlocklistEntryFilter, limit, offset int) ([]storage.IPBlocklistEntry, int, error) {
	var out []storage.IPBlocklistEntry
	for _, entry := range f.createdBlocks {
		if filter.TenantID != uuid.Nil && entry.TenantID != filter.TenantID {
			continue
		}
		if filter.FindingID != uuid.Nil && (!entry.FindingID.Valid || entry.FindingID.UUID != filter.FindingID) {
			continue
		}
		if strings.TrimSpace(filter.IPCIDR) != "" && entry.IPCIDR != strings.TrimSpace(filter.IPCIDR) {
			continue
		}
		if strings.TrimSpace(filter.Status) != "" && entry.Status != strings.TrimSpace(filter.Status) {
			continue
		}
		if strings.TrimSpace(filter.ServerGroup) != "" && entry.ServerGroup != strings.TrimSpace(filter.ServerGroup) {
			continue
		}
		if strings.TrimSpace(filter.TargetType) != "" && entry.TargetType != strings.TrimSpace(filter.TargetType) {
			continue
		}
		if filter.TargetID != uuid.Nil && (!entry.TargetID.Valid || entry.TargetID.UUID != filter.TargetID) {
			continue
		}
		if strings.TrimSpace(filter.App) != "" && entry.App != strings.TrimSpace(filter.App) {
			continue
		}
		if strings.TrimSpace(filter.VHost) != "" && entry.VHost != strings.TrimSpace(filter.VHost) {
			continue
		}
		out = append(out, entry)
	}
	total := len(out)
	if limit <= 0 {
		limit = 50
	}
	if offset > len(out) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], total, nil
}

func (f *blockProposalInvestigateStore) ListIPBehaviorSourceIPsByASN(_ context.Context, _ uuid.UUID, _ string, _ time.Time, limit int) ([]storage.IPBehaviorASNSource, error) {
	out := append([]storage.IPBehaviorASNSource(nil), f.asnSources...)
	if limit <= 0 || limit > len(out) {
		limit = len(out)
	}
	return out[:limit], nil
}

func (f *blockProposalInvestigateStore) SetIPBlocklistEntryEntityAction(_ context.Context, id, entityActionID uuid.UUID) (*storage.IPBlocklistEntry, error) {
	for i := range f.createdBlocks {
		if f.createdBlocks[i].ID == id {
			f.createdBlocks[i].EntityActionID = uuid.NullUUID{UUID: entityActionID, Valid: true}
			entry := f.createdBlocks[i]
			return &entry, nil
		}
	}
	return nil, nil
}

func (f *blockProposalInvestigateStore) UpdateIPBlocklistEntryStatus(_ context.Context, id uuid.UUID, status string, approverID *uuid.UUID, errMsg string) (*storage.IPBlocklistEntry, error) {
	for i := range f.createdBlocks {
		if f.createdBlocks[i].ID == id {
			f.createdBlocks[i].Status = status
			if approverID != nil {
				f.createdBlocks[i].ApprovedBy = uuid.NullUUID{UUID: *approverID, Valid: true}
				f.createdBlocks[i].ApprovedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
			}
			if strings.TrimSpace(errMsg) != "" {
				f.createdBlocks[i].LastError = sql.NullString{String: errMsg, Valid: true}
			}
			entry := f.createdBlocks[i]
			return &entry, nil
		}
	}
	return nil, nil
}

func (f *blockProposalInvestigateStore) ListNodeFirewallRulesForEntityAction(_ context.Context, entityActionID uuid.UUID) ([]storage.NodeFirewallRule, error) {
	var out []storage.NodeFirewallRule
	for _, rule := range f.rules {
		if rule.EntityActionID == entityActionID {
			out = append(out, rule)
		}
	}
	return out, nil
}

func (f *blockProposalInvestigateStore) QueueNodeFirewallRuleRemoval(_ context.Context, ruleID, jobID uuid.UUID) error {
	if f.queued == nil {
		f.queued = map[uuid.UUID]uuid.UUID{}
	}
	f.queued[ruleID] = jobID
	for i := range f.rules {
		if f.rules[i].ID == ruleID {
			f.rules[i].Status = "pending"
			f.rules[i].JobID = &jobID
			break
		}
	}
	return nil
}

func (f *blockProposalInvestigateStore) ListAssetCIDRs(context.Context, uuid.UUID) ([]net.IPNet, error) {
	return f.assets, nil
}

func (f *blockProposalInvestigateStore) EntityLifecycle(context.Context, storage.LifecycleFilter, int) ([]storage.LifecycleItem, error) {
	return nil, nil
}

func (f *blockProposalInvestigateStore) EntitySummary(context.Context, uuid.UUID, string, string) (*storage.EntitySummary, error) {
	return nil, nil
}

type webserverSafetyFakeStore struct {
	*fakeStore
	recentFailures int
}

func (f *webserverSafetyFakeStore) CountRecentFailedWebserverConfigActions(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (int, error) {
	return f.recentFailures, nil
}

func TestWebRequestFromLogDoesNotTrustXFFByDefault(t *testing.T) {
	s := &Server{}
	nodeID := uuid.New()
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
	ev, ok := s.webRequestFromLog(context.Background(), uuid.New(), nodeID, "nginx", "/var/log/nginx/access.log", nil, entry, nil, nil)
	if !ok {
		t.Fatal("expected web.request event")
	}
	if ev.SrcIP != "198.51.100.20" {
		t.Fatalf("spoofable XFF must not replace socket remote IP by default, got %s", ev.SrcIP)
	}
	if trusted, _ := ev.Details["trusted_proxy"].(bool); trusted {
		t.Fatal("trusted_proxy should default to false")
	}
	if rejected, _ := ev.Details["xff_spoof_rejected"].(bool); !rejected {
		t.Fatal("expected spoofable XFF to be marked rejected")
	}
}

func TestResolveWebRequestClientIPTrustsOnlyConfiguredProxy(t *testing.T) {
	decision, ok := resolveWebRequestClientIP("10.0.0.20", "", "203.0.113.10, 10.0.0.10", []string{"10.0.0.0/24"})
	if !ok {
		t.Fatal("expected client IP decision")
	}
	if decision.ClientIP != "203.0.113.10" {
		t.Fatalf("expected XFF client behind trusted proxy, got %s", decision.ClientIP)
	}
	if !decision.TrustedProxy || decision.Source != "xff_chain" || decision.RejectedXFF {
		t.Fatalf("unexpected trusted proxy decision: %#v", decision)
	}

	decision, ok = resolveWebRequestClientIP("198.51.100.20", "", "203.0.113.10", []string{"10.0.0.0/24"})
	if !ok {
		t.Fatal("expected direct client IP decision")
	}
	if decision.ClientIP != "198.51.100.20" || decision.TrustedProxy || !decision.RejectedXFF {
		t.Fatalf("untrusted socket must keep remote IP and reject XFF, got %#v", decision)
	}
}

func TestWebRequestFromLogCarriesProxyLifecycleHeaders(t *testing.T) {
	s := &Server{}
	nodeID := uuid.New()
	entry := &agentLogEntry{
		Timestamp: time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Message:   `401 GET /login`,
		Fields: map[string]any{
			"remote_ip":                 "203.0.113.10",
			"request":                   "GET /login HTTP/1.1",
			"status":                    401,
			"bytes":                     120,
			"request_id":                "req-edge",
			"response_request_id":       "req-app",
			"captured_response_headers": "X-Request-ID: req-app",
			"frontend":                  "public_https",
			"backend":                   "core_api",
			"termination_state":         "----",
			"duration_us":               250000,
		},
	}
	ev, ok := s.webRequestFromLog(context.Background(), uuid.New(), nodeID, "haproxy", "/var/log/haproxy.log", nil, entry, nil, nil)
	if !ok {
		t.Fatal("expected web.request event")
	}
	if ev.ProcessName != "haproxy" || ev.DurationMS != 250 {
		t.Fatalf("unexpected lifecycle basics: process=%s duration=%d", ev.ProcessName, ev.DurationMS)
	}
	for key, want := range map[string]string{
		"request_id":                "req-edge",
		"response_request_id":       "req-app",
		"captured_response_headers": "X-Request-ID: req-app",
		"frontend":                  "public_https",
		"backend":                   "core_api",
		"termination_state":         "----",
	} {
		if got, _ := ev.Details[key].(string); got != want {
			t.Fatalf("details[%s] = %q, want %q; details=%#v", key, got, want, ev.Details)
		}
	}
}

func TestIPBehaviorScoringUsesLearnedBaselines(t *testing.T) {
	bucket := &ipBehaviorBucket{
		srcIP:       "203.0.113.10",
		countryCode: "NG",
		app:         "core-api",
		lastTS:      time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC),
		count:       8,
		bytesOut:    12 * 1024 * 1024,
		statuses:    map[int]int{200: 8},
		paths:       map[string]int{"/api/export": 1},
	}
	baselines := []storage.BehavioralBaseline{{
		SignalType: "ip_behavior.country_app",
		Dimension:  "|core-api|NG",
		WindowDays: 30,
		ComputedAt: time.Now().UTC(),
		Baseline: map[string]any{
			"sample_count": int64(40),
			"request_count": map[string]any{
				"p99":  20.0,
				"peak": 30.0,
			},
			"bytes_out": map[string]any{
				"p99":  float64(2 * 1024 * 1024),
				"peak": float64(3 * 1024 * 1024),
			},
			"auth_fail_ratio":   0.01,
			"server_err_ratio":  0.01,
			"active_hours":      []any{int64(8), int64(9), int64(10)},
			"active_weekdays":   []any{int64(1), int64(2), int64(3), int64(4), int64(5)},
			"computed_at_utc":   time.Now().UTC().Format(time.RFC3339),
			"baseline_revision": "test",
		},
	}}
	score, category, reasons, corroborating := scoreIPBehaviorBucketWithBaselines(bucket, baselines)
	if score < 70 || category != "exfiltration_risk" || corroborating == 0 {
		t.Fatalf("expected baseline-driven exfiltration risk, score=%d category=%s corroborating=%d reasons=%v", score, category, corroborating, reasons)
	}
}

func TestIPBehaviorPracticalScenarioScoring(t *testing.T) {
	ts := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)
	baseline := storage.BehavioralBaseline{
		SignalType: "ip_behavior.country_app",
		Dimension:  "core-banking|core-api|NG",
		WindowDays: 30,
		ComputedAt: time.Now().UTC(),
		Baseline: map[string]any{
			"sample_count": int64(30),
			"request_count": map[string]any{
				"p99":  30.0,
				"peak": 40.0,
			},
			"bytes_out": map[string]any{
				"p99":  float64(1 * 1024 * 1024),
				"peak": float64(2 * 1024 * 1024),
			},
			"auth_fail_ratio":  0.01,
			"server_err_ratio": 0.01,
			"active_hours":     []any{int64(8), int64(9), int64(10), int64(11)},
			"active_weekdays":  []any{int64(1), int64(2), int64(3), int64(4), int64(5)},
		},
	}
	tests := []struct {
		name      string
		bucket    *ipBehaviorBucket
		baselines []storage.BehavioralBaseline
		category  string
		minScore  int
	}{
		{
			name: "scanner prober",
			bucket: &ipBehaviorBucket{
				srcIP:       "203.0.113.20",
				countryCode: "NG",
				asn:         "AS64500",
				app:         "core-api",
				lastTS:      ts,
				count:       12,
				statuses:    map[int]int{301: 2, 403: 3, 404: 7},
				paths: map[string]int{
					"/wp-login.php": 2,
					"/admin":        2,
					"/.env":         2,
					"/phpmyadmin":   2,
					"/api/debug":    2,
					"/owa/auth":     2,
				},
			},
			category: "scanner_probe",
			minScore: 70,
		},
		{
			name: "exploit attempt",
			bucket: &ipBehaviorBucket{
				srcIP:       "203.0.113.21",
				countryCode: "NG",
				asn:         "AS64500",
				app:         "core-api",
				lastTS:      ts,
				count:       4,
				statuses:    map[int]int{500: 2, 502: 1, 200: 1},
				paths:       map[string]int{"/api/upload": 2, "/api/admin/import": 2},
			},
			category: "exploit_attempt",
			minScore: 60,
		},
		{
			name: "slow distributed attack",
			bucket: &ipBehaviorBucket{
				srcIP:           "203.0.113.22",
				countryCode:     "NG",
				asn:             "AS64500",
				app:             "core-api",
				lastTS:          ts,
				count:           40,
				uniqueSourceIPs: 32,
				statuses:        map[int]int{200: 40},
				paths:           map[string]int{"/api/status": 40},
			},
			category: "slow_distributed_attack",
			minScore: 65,
		},
		{
			name: "webshell callback",
			bucket: &ipBehaviorBucket{
				srcIP:               "203.0.113.23",
				countryCode:         "NG",
				asn:                 "AS64500",
				app:                 "core-api",
				lastTS:              ts,
				count:               10,
				statuses:            map[int]int{200: 10},
				paths:               map[string]int{"/api/upload": 10},
				fileWrites:          1,
				processSpawns:       1,
				outboundConnections: 1,
			},
			category: "webshell_callback",
			minScore: 85,
		},
		{
			name: "partner drift",
			bucket: &ipBehaviorBucket{
				srcIP:          "203.0.113.24",
				countryCode:    "NG",
				asn:            "AS64500",
				app:            "core-api",
				serverGroup:    "core-banking",
				lastTS:         ts,
				count:          4,
				bytesOut:       12 * 1024 * 1024,
				statuses:       map[int]int{200: 4},
				paths:          map[string]int{"/api/partner/export": 4},
				trustedPartner: true,
				partnerID:      "payment-switch-a",
			},
			baselines: []storage.BehavioralBaseline{baseline},
			category:  "partner_drift",
			minScore:  85,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, category, reasons, corroborating := scoreIPBehaviorBucketWithBaselines(tt.bucket, tt.baselines)
			if score < tt.minScore || category != tt.category || corroborating == 0 {
				t.Fatalf("score=%d category=%s corroborating=%d reasons=%v, want category=%s score>=%d", score, category, corroborating, reasons, tt.category, tt.minScore)
			}
		})
	}
}

func TestDetectIPBehaviorIncludesCrossDomainEvidence(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &ipBehaviorFindingCaptureStore{fakeStore: &fakeStore{}}
	s := &Server{store: store}
	start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)

	events := make([]IngestedEvent, 0, 10)
	for i := 0; i < 10; i++ {
		ev := makeIPBehaviorWebRequest(start.Add(time.Duration(i)*time.Second), "203.0.113.25", "/api/upload", 200, 512)
		ev.CorrelationID = "req-webshell"
		ev.DedupKey = "web.request:req-webshell"
		if i == 0 {
			ev.Details["process_spawn_count"] = 1
			ev.Details["file_write_count"] = 1
			ev.Details["outbound_connection_count"] = 1
			ev.Details["source_file"] = "/var/log/nginx/control-one-access.log"
			ev.Details["connection_id"] = "conn-1"
			ev.Details["process_event_id"] = "proc-1"
			ev.Details["file_event_id"] = "file-1"
			ev.Details["exposure_id"] = "exposure-1"
		}
		events = append(events, ev)
	}

	got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeID, events)
	if len(got) != 1 {
		t.Fatalf("deduplicated anomalies = %d, want 1", len(got))
	}
	ev := got[0]
	if ev.Details["category"] != "webshell_callback" {
		t.Fatalf("category = %v, want webshell_callback", ev.Details["category"])
	}
	hostCorrelation, ok := ev.Details["host_correlation"].(map[string]any)
	if !ok {
		t.Fatalf("missing host correlation evidence: %#v", ev.Details)
	}
	if hostCorrelation["process_spawns"] != 1 || hostCorrelation["file_writes"] != 1 || hostCorrelation["outbound_connections"] != 1 {
		t.Fatalf("unexpected host correlation evidence: %#v", hostCorrelation)
	}
	refs, ok := ev.Details["evidence_refs"].([]map[string]any)
	if !ok || len(refs) == 0 {
		t.Fatalf("missing linked evidence refs: %#v", ev.Details["evidence_refs"])
	}
	if got := refs[0]["process_event_id"]; got != "proc-1" {
		t.Fatalf("process evidence ref = %v, want proc-1; refs=%#v", got, refs)
	}
	if len(store.findings) != 1 {
		t.Fatalf("recorded findings = %d, want 1", len(store.findings))
	}
	if _, ok := store.findings[0].Evidence["host_correlation"]; !ok {
		t.Fatalf("finding evidence missing host correlation: %#v", store.findings[0].Evidence)
	}
	if _, ok := store.findings[0].Evidence["evidence_refs"]; !ok {
		t.Fatalf("finding evidence missing evidence refs: %#v", store.findings[0].Evidence)
	}
}

func TestDetectIPBehaviorOpensAlertAtFullConfidence(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &ipBehaviorFindingCaptureStore{fakeStore: &fakeStore{}}
	s := &Server{store: store}
	start := time.Date(2026, 5, 17, 4, 0, 0, 0, time.UTC)

	events := make([]IngestedEvent, 0, 12)
	for i := 0; i < 12; i++ {
		events = append(events, makeIPBehaviorWebRequest(start.Add(time.Duration(i)*time.Second), "203.0.113.55", "/admin/login", 401, 5*1024*1024))
	}

	got := s.detectIPBehaviorBatch(context.Background(), tenantID, nodeID, events)
	if len(got) != 1 {
		t.Fatalf("deduplicated anomalies = %d, want 1", len(got))
	}
	if got[0].Details["score"] != 100 {
		t.Fatalf("score = %v, want 100", got[0].Details["score"])
	}
	if len(store.alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(store.alerts))
	}
	alert := store.alerts[0]
	if alert.Source != "ip_behavior" || alert.Severity != "critical" {
		t.Fatalf("alert source/severity = %s/%s, want ip_behavior/critical", alert.Source, alert.Severity)
	}
	if !strings.Contains(alert.Title, "100% confidence") {
		t.Fatalf("alert title missing confidence: %q", alert.Title)
	}
	if strings.Contains(strings.ToLower(alert.Summary), "baseline dimension") {
		t.Fatalf("alert summary exposes scoring internals: %q", alert.Summary)
	}
	if alert.Context["confidence"] != 100 || alert.Context["finding_dedup_key"] == "" {
		t.Fatalf("alert context missing confidence/dedup: %#v", alert.Context)
	}
}

func TestBackfillIPBehaviorConfidenceAlerts(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &ipBehaviorFindingCaptureStore{fakeStore: &fakeStore{}}
	s := &Server{store: store}
	now := time.Date(2026, 5, 18, 18, 17, 0, 0, time.UTC)

	s.backfillIPBehaviorConfidenceAlerts(context.Background(), []storage.IPBehaviorFinding{
		{
			ID:          uuid.New(),
			TenantID:    tenantID,
			NodeID:      uuid.NullUUID{UUID: nodeID, Valid: true},
			DedupKey:    "anomaly.ip_behavior:legacy",
			SourceIP:    sql.NullString{String: "102.89.69.217/32", Valid: true},
			CountryCode: "NG",
			ASN:         "AS29465",
			Category:    "credential_attack",
			Severity:    "critical",
			Score:       100,
			Status:      "open",
			Reason:      "credential_attack behavior from 102.89.69.217 scored 100: country/country-app behavior is being evaluated as a baseline dimension; request burst: 21 requests in 1m; auth failure spike: 21 401/403 responses",
			Evidence: map[string]any{
				"app":           "nginx",
				"country":       "Nigeria",
				"request_count": 21,
				"window":        "1m",
				"status_counts": map[string]any{"401": 21},
				"dedup_scope":   []string{"tenant", "src_ip"},
				"corroborating": 3,
				"score":         100,
			},
			LastSeenAt: now,
		},
		{ID: uuid.New(), TenantID: tenantID, Score: 90, Status: "open", SourceIP: sql.NullString{String: "203.0.113.90/32", Valid: true}},
		{ID: uuid.New(), TenantID: tenantID, Score: 100, Status: "resolved", SourceIP: sql.NullString{String: "203.0.113.100/32", Valid: true}},
	})

	if len(store.alerts) != 1 {
		t.Fatalf("alerts = %d, want 1", len(store.alerts))
	}
	alert := store.alerts[0]
	if alert.Source != "ip_behavior" || alert.Severity != "critical" {
		t.Fatalf("alert source/severity = %s/%s, want ip_behavior/critical", alert.Source, alert.Severity)
	}
	if !strings.Contains(alert.Title, "100% confidence credential attack") {
		t.Fatalf("alert title missing confidence/category: %q", alert.Title)
	}
	if strings.Contains(alert.Title, "/32") {
		t.Fatalf("alert title should use normalized host IP: %q", alert.Title)
	}
	if strings.Contains(strings.ToLower(alert.Summary), "baseline dimension") {
		t.Fatalf("alert summary exposes scoring internals: %q", alert.Summary)
	}
	if alert.Context["finding_dedup_key"] != "anomaly.ip_behavior:legacy" || alert.Context["country"] != "Nigeria" {
		t.Fatalf("alert context missing finding details: %#v", alert.Context)
	}
}

type ipBehaviorFindingCaptureStore struct {
	*fakeStore
	findings []storage.UpsertIPBehaviorFindingParams
	alerts   []storage.CreateAlertParams
}

func (f *ipBehaviorFindingCaptureStore) UpsertIPBehaviorFinding(_ context.Context, p storage.UpsertIPBehaviorFindingParams) (*storage.IPBehaviorFinding, error) {
	f.findings = append(f.findings, p)
	return &storage.IPBehaviorFinding{
		ID:          uuid.New(),
		TenantID:    p.TenantID,
		DedupKey:    p.DedupKey,
		SourceIP:    sql.NullString{String: p.SourceIP, Valid: p.SourceIP != ""},
		CountryCode: p.CountryCode,
		ASN:         p.ASN,
		Category:    p.Category,
		Severity:    p.Severity,
		Score:       p.Score,
		Reason:      p.Reason,
		Evidence:    p.Evidence,
		FirstSeenAt: p.SeenAt,
		LastSeenAt:  p.SeenAt,
	}, nil
}

func (f *ipBehaviorFindingCaptureStore) CreateAlert(_ context.Context, p storage.CreateAlertParams) (*storage.Alert, error) {
	f.alerts = append(f.alerts, p)
	return &storage.Alert{
		ID:       uuid.New(),
		TenantID: p.TenantID,
		Source:   p.Source,
		Severity: p.Severity,
		Title:    p.Title,
		Summary:  sql.NullString{String: p.Summary, Valid: p.Summary != ""},
		DedupKey: sql.NullString{String: p.DedupKey, Valid: p.DedupKey != ""},
		Context:  p.Context,
		OpenedAt: time.Now().UTC(),
	}, nil
}
