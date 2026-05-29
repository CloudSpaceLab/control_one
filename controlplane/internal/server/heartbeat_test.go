package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

// mtlsRequest builds a request where the auth middleware has already promoted
// the caller to an agent Principal with the given CN. This mirrors what the
// real middleware does off r.TLS.PeerCertificates[0]; using the context
// directly lets us exercise the handler without spinning up a TLS stack.
func mtlsRequest(method, path, cn string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	principal := &auth.Principal{
		Type:    "agent",
		Name:    cn,
		Subject: "CN=" + cn,
		Roles:   []string{"agent"},
	}
	return req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
}

func buildHeartbeatServer(t *testing.T, store *fakeStore) *Server {
	t.Helper()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	t.Cleanup(func() { srv.stopEnrollmentReaper() })
	return srv
}

// TestHeartbeatBumpsLastSeen covers the happy path: a pending node without a
// TestHeartbeatActivatesOnFirstBeat verifies that a pending node with no prior
// heartbeat transitions to active on its first successful heartbeat.
// first_scan_at is no longer required — nodes with no compliance rule sets
// would never set it and would be stuck enrollment_pending forever.
func TestHeartbeatActivatesOnFirstBeat(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "pending-host",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.Activated {
		t.Fatalf("node should activate on first heartbeat; got activated=false, state=%s", resp.State)
	}
	if resp.State != storage.NodeStateActive {
		t.Fatalf("state = %s, want active", resp.State)
	}
	if store.nodes[0].LastSeenAt == nil {
		t.Fatalf("last_seen_at was not stamped")
	}
}

func TestHeartbeatReturnsApprovedLogSourcesForCapableAgent(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "collector-host",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
			Labels:    map[string]any{},
		}},
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:            uuid.New(),
			TenantID:      tenantID,
			NodeID:        nodeID,
			ProposalID:    "local-log:nginx",
			Kind:          connectordiscovery.KindLocalLog,
			Program:       "nginx",
			CollectorType: connectordiscovery.CollectorTypeFile,
			Formatter:     "nginx",
			Status:        storage.ContentPackSourceProposalStatusApproved,
			Paths:         []string{"/var/log/nginx/access.log"},
			FirstSeenAt:   now,
			LastSeenAt:    now,
			ApprovedAt:    &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}
	srv := buildHeartbeatServer(t, store)

	raw := []byte(`{"capabilities":["connector_approved_sources.v1"]}`)
	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.ContentLength = int64(len(raw))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.ApprovedLogSources) != 1 || resp.ApprovedLogSources[0].Program != "nginx" {
		t.Fatalf("approved log sources = %#v", resp.ApprovedLogSources)
	}
}

func TestHeartbeatReturnsConnectorPolicyForCapableAgent(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "collector-host",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
			Labels:    map[string]any{},
		}},
		eventFilters: map[uuid.UUID]storage.TenantEventFilters{
			tenantID: {
				TenantID:                          tenantID,
				CaptureExternal:                   true,
				CaptureInternalSummary:            true,
				CaptureListeningChanges:           true,
				ConnectorAutoConnectMediumRisk:    true,
				ConnectorAutoConnectPrograms:      []string{"postgresql"},
				ConnectorApprovalRequiredPrograms: []string{"nginx"},
				ConnectorBlockedPrograms:          []string{"temenos-t24"},
				UpdatedAt:                         now,
			},
		},
	}
	srv := buildHeartbeatServer(t, store)

	raw := []byte(`{"capabilities":["connector_auto_connect_policy.v1"]}`)
	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	req.Body = io.NopCloser(bytes.NewReader(raw))
	req.ContentLength = int64(len(raw))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ConnectorPolicy == nil {
		t.Fatal("connector policy missing from heartbeat response")
	}
	if !resp.ConnectorPolicy.AllowMediumRisk || len(resp.ConnectorPolicy.AutoConnectPrograms) != 1 || resp.ConnectorPolicy.AutoConnectPrograms[0] != "postgresql" {
		t.Fatalf("connector policy = %#v", resp.ConnectorPolicy)
	}
	if len(resp.ConnectorPolicy.ApprovalRequiredPrograms) != 1 || resp.ConnectorPolicy.ApprovalRequiredPrograms[0] != "nginx" {
		t.Fatalf("approval-required programs = %#v", resp.ConnectorPolicy.ApprovalRequiredPrograms)
	}
	if len(resp.ConnectorPolicy.BlockedPrograms) != 1 || resp.ConnectorPolicy.BlockedPrograms[0] != "temenos-t24" {
		t.Fatalf("blocked programs = %#v", resp.ConnectorPolicy.BlockedPrograms)
	}
}

// TestHeartbeatActivatesAfterFirstScan drives the state machine: a pending
// node with both a heartbeat AND a prior first_scan flips to active.
func TestHeartbeatActivatesAfterFirstScan(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	firstScan := time.Now().Add(-time.Minute).UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:          nodeID,
			TenantID:    tenantID,
			Hostname:    "gated-host",
			State:       storage.NodeStateEnrollmentPending,
			FirstScanAt: &firstScan,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
			Labels:      map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Activated {
		t.Fatalf("expected node to activate on heartbeat+first_scan combo")
	}
	if resp.State != storage.NodeStateActive {
		t.Fatalf("state = %s, want active", resp.State)
	}
	if store.nodes[0].State != storage.NodeStateActive {
		t.Fatalf("node state not persisted as active: %s", store.nodes[0].State)
	}
}

func TestHeartbeatReturnsNetworkIsolationPolicy(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	expires := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			Hostname: "locked-host",
			State:    storage.NodeStateActive,
			Labels: map[string]any{
				isolationModeLabel:       isolationModeWhitelist,
				isolationExpiresAtLabel:  expires,
				isolationAllowAppsLabel:  []any{"control-one-agent", "patch"},
				isolationAllowCIDRsLabel: []any{"10.0.0.0/8"},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.NetworkPolicy == nil {
		t.Fatalf("expected network policy in heartbeat response")
	}
	if resp.NetworkPolicy.Mode != isolationModeWhitelist || !resp.NetworkPolicy.Active {
		t.Fatalf("unexpected network policy: %#v", resp.NetworkPolicy)
	}
	if resp.NetworkPolicy.SchemaVersion != networkPolicySchemaVersion || resp.NetworkPolicy.DesiredStateID == "" {
		t.Fatalf("expected desired-state contract, got %#v", resp.NetworkPolicy)
	}
	if resp.NetworkPolicy.Enforcement.DefaultInboundAction != "block" {
		t.Fatalf("expected blocking enforcement intent, got %#v", resp.NetworkPolicy.Enforcement)
	}
	if len(resp.NetworkPolicy.AllowedApplications) != 2 || resp.NetworkPolicy.AllowedApplications[1] != "patch" {
		t.Fatalf("expected allowed applications in network policy, got %#v", resp.NetworkPolicy.AllowedApplications)
	}
}

func TestHeartbeatRecordsNetworkPolicyReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:       nodeID,
			TenantID: tenantID,
			Hostname: "receipt-host",
			State:    storage.NodeStateActive,
			Labels: map[string]any{
				isolationModeLabel:       isolationModeWhitelist,
				isolationAllowCIDRsLabel: []any{"10.0.0.0/8"},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	srv := buildHeartbeatServer(t, store)
	srv.auditAsync = false
	desiredID := srv.compileNodeNetworkPolicy(context.Background(), store.nodes[0], time.Now().UTC()).DesiredStateID

	body := []byte(`{"network_policy_receipts":[{"desired_state_id":"` + desiredID + `","schema_version":"network_policy.desired_state.v1","mode":"whitelist","status":"planned_dry_run","backend":"iptables","dry_run":true,"planned_rules":3,"missing_controls":["application_allowlist"],"drift":["planned_rules_not_present"],"signature_present":false,"observed_at":"2026-05-19T10:00:00Z","rollback_available":false}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", bytes.NewReader(body))
	principal := &auth.Principal{
		Type:  "agent",
		Name:  nodeID.String(),
		Roles: []string{"agent"},
	}
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
	rec := httptest.NewRecorder()

	srv.handleNodeResource(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if len(store.auditLogs) == 0 {
		t.Fatalf("expected network policy receipt audit log")
	}
	got := store.auditLogs[len(store.auditLogs)-1]
	if got.Action != "network_policy.receipt" || got.ResourceType != "node" {
		t.Fatalf("unexpected audit entry: %#v", got)
	}
	if got.Metadata["desired_state_id"] != desiredID || got.Metadata["status"] != "planned_dry_run" || got.Metadata["receipt_valid"] != true {
		t.Fatalf("unexpected receipt metadata: %#v", got.Metadata)
	}
	if got.Metadata["current_desired_state_id"] != desiredID || got.Metadata["rollback_available"] != false {
		t.Fatalf("expected validated current dry-run receipt, got %#v", got.Metadata)
	}
}

func TestHeartbeatProjectsAgentSpoolBackpressureToSourceHealth(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "spool-host",
			State:     storage.NodeStateActive,
			Labels:    map[string]any{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	srv := buildHeartbeatServer(t, store)
	body := []byte(`{"agent_runtime_profile":"forensic","agent_self_metrics":{"event_spool_records":2,"event_spool_bytes":4096,"event_spool_max_bytes":1048576,"log_spool_records":1,"log_spool_bytes":2048,"log_spool_dropped":3}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", bytes.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:  "agent",
		Name:  nodeID.String(),
		Roles: []string{"agent"},
	}))
	rec := httptest.NewRecorder()

	srv.handleNodeResource(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if len(store.sourceStates) != 1 {
		t.Fatalf("source states = %#v", store.sourceStates)
	}
	state := store.sourceStates[0].State
	if state.SourceID != "control_one.agent_spool" || state.CoverageState != contentpacks.CoverageState(contentpacks.CoverageBackpressured) {
		t.Fatalf("unexpected spool source state: %#v", state)
	}
	if state.Metrics.QueueDepth != 3 || state.Metrics.EventsDropped != 3 {
		t.Fatalf("unexpected spool metrics: %#v", state.Metrics)
	}
	if state.Labels["agent_runtime_profile"] != "forensic" || state.Labels["event_spool_records"] != "2" || state.Labels["log_spool_dropped"] != "3" {
		t.Fatalf("unexpected spool labels: %#v", state.Labels)
	}
}

func TestHeartbeatAnnotatesInvalidNetworkPolicyReceipt(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "receipt-host",
			State:     storage.NodeStateActive,
			Labels:    map[string]any{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}
	srv := buildHeartbeatServer(t, store)
	srv.auditAsync = false

	body := []byte(`{"network_policy_receipts":[{"desired_state_id":"sha256:stale","schema_version":"wrong","mode":"whitelist","status":"planned","planned_rules":-2,"applied_rules":50000,"observed_at":"not-a-time"}]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", bytes.NewReader(body))
	principal := &auth.Principal{
		Type:  "agent",
		Name:  nodeID.String(),
		Roles: []string{"agent"},
	}
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
	rec := httptest.NewRecorder()

	srv.handleNodeResource(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if len(store.auditLogs) == 0 {
		t.Fatalf("expected network policy receipt audit log")
	}
	got := store.auditLogs[len(store.auditLogs)-1]
	if got.Metadata["receipt_valid"] != false || got.Metadata["status"] != "invalid" {
		t.Fatalf("expected invalid receipt metadata, got %#v", got.Metadata)
	}
	if got.Metadata["planned_rules"] != 0 || got.Metadata["applied_rules"] != 10000 {
		t.Fatalf("expected clamped rule counts, got %#v", got.Metadata)
	}
}

// TestHeartbeatRejectsMismatchedCN is the core mTLS authz check: agent cert
// CN must equal the URL-scoped node id.
func TestHeartbeatRejectsMismatchedCN(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			Hostname:  "victim",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	imposter := uuid.New().String()
	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", imposter)
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if store.nodes[0].LastSeenAt != nil {
		t.Fatalf("last_seen_at should not be stamped on rejected heartbeat")
	}
}

// TestHeartbeatRejectsNonAgentPrincipal ensures bearer-token operators cannot
// forge heartbeats on behalf of a node.
func TestHeartbeatRejectsNonAgentPrincipal(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			Hostname:  "host",
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nil)
	principal := &auth.Principal{
		Type:    "user",
		Name:    "admin",
		Subject: "user-admin",
		Roles:   []string{"admin"},
	}
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

// TestHeartbeatReturnsNotFoundForUnknownNode checks the ErrNoRows branch. The
// agent middleware can't prevent this because an agent cert survives the
// node row being deleted.
func TestHeartbeatReturnsNotFoundForUnknownNode(t *testing.T) {
	t.Parallel()

	unknown := uuid.New()
	store := &fakeStore{}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+unknown.String()+"/heartbeat", unknown.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// TestFirstScanHookActivatesWhenHeartbeatAlreadyLanded covers the second
// ordering: heartbeat first, then first compliance scan. The scan hook should
// activate the node.
func TestFirstScanHookActivatesWhenHeartbeatAlreadyLanded(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	lastSeen := time.Now().Add(-30 * time.Second).UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:         nodeID,
			TenantID:   tenantID,
			Hostname:   "flipped-by-scan",
			State:      storage.NodeStateEnrollmentPending,
			LastSeenAt: &lastSeen,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Labels:     map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	// Invoke the hook directly — this is what compliance.go calls after
	// persisting scan results.
	srv.handleFirstScanHook(context.Background(), nodeID)

	if store.nodes[0].State != storage.NodeStateActive {
		t.Fatalf("scan hook failed to activate node: state=%s", store.nodes[0].State)
	}
	if store.nodes[0].FirstScanAt == nil {
		t.Fatalf("scan hook failed to stamp first_scan_at")
	}
}

// TestEnrollmentReaperFlipsPendingToFailed drives the reaper directly to
// avoid relying on wall-clock sleeps. A pending node older than the timeout
// should become enrollment_failed; a fresher pending node should not.
func TestEnrollmentReaperFlipsPendingToFailed(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	oldID := uuid.New()
	freshID := uuid.New()
	activeID := uuid.New()
	past := time.Now().Add(-20 * time.Minute)
	store := &fakeStore{
		nodes: []storage.Node{
			{
				ID:        oldID,
				TenantID:  tenantID,
				Hostname:  "stale",
				State:     storage.NodeStateEnrollmentPending,
				CreatedAt: past,
				UpdatedAt: past,
				Labels:    map[string]any{},
			},
			{
				ID:        freshID,
				TenantID:  tenantID,
				Hostname:  "fresh",
				State:     storage.NodeStateEnrollmentPending,
				CreatedAt: time.Now().Add(-time.Minute),
				UpdatedAt: time.Now().Add(-time.Minute),
				Labels:    map[string]any{},
			},
			{
				ID:        activeID,
				TenantID:  tenantID,
				Hostname:  "already-active",
				State:     storage.NodeStateActive,
				CreatedAt: past,
				UpdatedAt: past,
				Labels:    map[string]any{},
			},
		},
	}
	srv := buildHeartbeatServer(t, store)
	srv.clockOverride = func() time.Time { return time.Now() }

	srv.reapPendingEnrollments()

	for _, n := range store.nodes {
		switch n.ID {
		case oldID:
			if n.State != storage.NodeStateEnrollmentFailed {
				t.Fatalf("old pending node: state=%s, want enrollment_failed", n.State)
			}
		case freshID:
			if n.State != storage.NodeStateEnrollmentPending {
				t.Fatalf("fresh pending node was flipped prematurely: state=%s", n.State)
			}
		case activeID:
			if n.State != storage.NodeStateActive {
				t.Fatalf("active node was touched by reaper: state=%s", n.State)
			}
		}
	}
}

// TestHeartbeatRejectsWrongMethod sanity-checks the allow list.
func TestHeartbeatRejectsWrongMethod(t *testing.T) {
	t.Parallel()

	nodeID := uuid.New()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			State:     storage.NodeStateEnrollmentPending,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	req := mtlsRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// Guard against unused imports when tests are trimmed.
var _ = sql.ErrNoRows

// patchPendingStore wraps fakeStore to feed the heartbeat handler a known set
// of pending node_patch_state rows. fakeStore's stub returns nil — we override
// just enough surface to drive the patch dispatch path without standing up a
// real DB.
type patchPendingStore struct {
	*fakeStore
	pending []storage.NodePatchState
}

func (p *patchPendingStore) ListPendingNodePatchStates(_ context.Context, _ uuid.UUID) ([]storage.NodePatchState, error) {
	return p.pending, nil
}

// TestHeartbeatPendingActionsUseJobTypePrefix pins the bug fix for §3.3 #5:
// previously heartbeat.go:259 hard-coded "patch.deploy_direct" as the prefix
// for every pending patch action, so proxy- and airgapped-mode deployments
// were silently routed as direct-mode by the agent. The fix reads the prefix
// from the job row's Type column, so the agent's switch dispatches to the
// correct handler. This test seeds three pending rows (one per mode) and
// asserts each lands in resp.PendingActions with the correct prefix.
func TestHeartbeatPendingActionsUseJobTypePrefix(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()

	directJobID := uuid.New()
	proxyJobID := uuid.New()
	airgapJobID := uuid.New()

	pending := []storage.NodePatchState{
		{ID: uuid.New(), DeploymentID: uuid.New(), NodeID: nodeID, TenantID: tenantID, Status: "pending", JobID: &directJobID, RequestedAt: now},
		{ID: uuid.New(), DeploymentID: uuid.New(), NodeID: nodeID, TenantID: tenantID, Status: "pending", JobID: &proxyJobID, RequestedAt: now.Add(time.Second)},
		{ID: uuid.New(), DeploymentID: uuid.New(), NodeID: nodeID, TenantID: tenantID, Status: "pending", JobID: &airgapJobID, RequestedAt: now.Add(2 * time.Second)},
	}
	jobs := map[uuid.UUID]*storage.Job{
		directJobID: {ID: directJobID, TenantID: tenantID, Type: JobTypePatchDeployDirect, Status: storage.JobStatusQueued},
		proxyJobID:  {ID: proxyJobID, TenantID: tenantID, Type: JobTypePatchDeployProxy, Status: storage.JobStatusQueued},
		airgapJobID: {ID: airgapJobID, TenantID: tenantID, Type: JobTypePatchDeployAirgapped, Status: storage.JobStatusQueued},
	}

	base := &fakeStore{
		nodes: []storage.Node{{
			ID:          nodeID,
			TenantID:    tenantID,
			Hostname:    "patch-host",
			State:       storage.NodeStateActive,
			FirstScanAt: &now,
			LastSeenAt:  &now,
			CreatedAt:   now,
			UpdatedAt:   now,
			Labels:      map[string]any{},
		}},
		jobs: jobs,
	}
	store := &patchPendingStore{fakeStore: base, pending: pending}

	cfg := &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	t.Cleanup(srv.stopEnrollmentReaper)

	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	want := map[string]string{
		JobTypePatchDeployDirect + ":" + directJobID.String():    JobTypePatchDeployDirect,
		JobTypePatchDeployProxy + ":" + proxyJobID.String():      JobTypePatchDeployProxy,
		JobTypePatchDeployAirgapped + ":" + airgapJobID.String(): JobTypePatchDeployAirgapped,
	}
	got := map[string]struct{}{}
	for _, a := range resp.PendingActions {
		got[a] = struct{}{}
	}
	for action := range want {
		if _, ok := got[action]; !ok {
			t.Errorf("missing pending_action %q\n got=%v", action, resp.PendingActions)
		}
	}

	// Each job should have flipped to running so the next heartbeat doesn't
	// re-dispatch the same action.
	for jobID, job := range jobs {
		if job.Status != storage.JobStatusRunning {
			t.Errorf("job %s status = %s, want running", jobID, job.Status)
		}
	}
}

func TestHeartbeatPersistsServerPurposeLabels(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "purpose-host",
			State:     storage.NodeStateActive,
			CreatedAt: now,
			UpdatedAt: now,
			Labels:    map[string]any{"existing": "kept"},
		}},
	}
	srv := buildHeartbeatServer(t, store)

	body := []byte(`{
		"agent_version":"dev",
		"server_purposes":[
			{"purpose":"DB Node","confidence":92,"evidence":["postgresql-16"]},
			{"purpose":"load_balancer","confidence":90,"evidence":["haproxy"]}
		]
	}`)
	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	labels := store.nodes[0].Labels
	if labels["existing"] != "kept" {
		t.Fatalf("existing labels were not preserved: %#v", labels)
	}
	purposes, ok := labels["agent.server_purposes"].([]string)
	if !ok || len(purposes) != 2 || purposes[0] != "db_node" || purposes[1] != "load_balancer" {
		t.Fatalf("server purposes not normalized/persisted: %#v", labels["agent.server_purposes"])
	}
	if labels["agent.primary_purpose"] != "db_node" {
		t.Fatalf("primary purpose = %#v, want db_node", labels["agent.primary_purpose"])
	}
	if evidence, ok := labels["agent.server_purpose_evidence"].([]map[string]any); !ok || len(evidence) != 2 {
		t.Fatalf("purpose evidence not persisted: %#v", labels["agent.server_purpose_evidence"])
	}
}

func TestHeartbeatDispatchesAgentUpdateJobIDForCapableAgent(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	payload, _ := json.Marshal(map[string]string{"node_id": nodeID.String(), "target_version": "main-test"})
	base := &fakeStore{
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "agent-host",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Labels:    map[string]any{},
		}},
		jobs: map[uuid.UUID]*storage.Job{
			jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeAgentUpdate, Status: storage.JobStatusQueued, Payload: payload, CreatedAt: time.Now()},
		},
	}
	store := &agentUpdatePendingStore{fakeStore: base, pending: base.jobs[jobID]}
	cfg := &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})
	t.Cleanup(func() { srv.stopEnrollmentReaper() })

	body, _ := json.Marshal(map[string]any{
		"agent_version": "dev",
		"capabilities":  []string{agentCapabilityUpdateJobStatus},
	})
	req := mtlsRequest(http.MethodPost, "/api/v1/nodes/"+nodeID.String()+"/heartbeat", nodeID.String())
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	rec := httptest.NewRecorder()
	srv.handleNodeResource(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	var resp heartbeatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	want := JobTypeAgentUpdate + ":" + jobID.String()
	if len(resp.PendingActions) != 1 || resp.PendingActions[0] != want {
		t.Fatalf("pending_actions = %#v, want [%s]", resp.PendingActions, want)
	}
	if got := base.jobs[jobID].Status; got != storage.JobStatusRunning {
		t.Fatalf("job status = %s, want running", got)
	}
}

func TestHeartbeatAgentUpdateCompletionMarksJobSucceeded(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	jobID := uuid.New()
	store := &fakeStore{jobs: map[uuid.UUID]*storage.Job{
		jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeAgentUpdate, Status: storage.JobStatusRunning},
	}}
	srv := &Server{logger: zap.NewNop(), store: store}

	srv.processHeartbeatCompletedActions(context.Background(), uuid.New(), []heartbeatCompletedAction{{
		Action:   JobTypeAgentUpdate,
		JobID:    jobID.String(),
		Status:   "succeeded",
		Metadata: map[string]any{"release_seq": float64(5)},
	}})

	if got := store.jobs[jobID].Status; got != storage.JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", got)
	}
}

func TestHeartbeatAgentUpdateAlreadyCurrentIsSucceeded(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	jobID := uuid.New()
	store := &fakeStore{jobs: map[uuid.UUID]*storage.Job{
		jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeAgentUpdate, Status: storage.JobStatusRunning},
	}}
	srv := &Server{logger: zap.NewNop(), store: store}

	srv.processHeartbeatCompletedActions(context.Background(), uuid.New(), []heartbeatCompletedAction{{
		Action: JobTypeAgentUpdate,
		JobID:  jobID.String(),
		Status: "failed",
		Error:  "self-update skipped: downgrade refused (manifest seq 4 <= current 4)",
	}})

	if got := store.jobs[jobID].Status; got != storage.JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", got)
	}
	events := store.events[jobID]
	if len(events) != 1 || events[0].Message != "agent already on current or newer release; update skipped safely" {
		t.Fatalf("events = %#v, want already-current success event", events)
	}
}

func TestHeartbeatReleaseSeqRetiresRunningAgentUpdateJob(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	jobID := uuid.New()
	payload, _ := json.Marshal(map[string]string{"node_id": nodeID.String(), "target_version": "legacy"})
	store := &fakeStore{jobs: map[uuid.UUID]*storage.Job{
		jobID: {ID: jobID, TenantID: tenantID, Type: JobTypeAgentUpdate, Status: storage.JobStatusRunning, Payload: payload},
	}}
	srv := &Server{logger: zap.NewNop(), store: store}
	node := &storage.Node{ID: nodeID, TenantID: tenantID}

	srv.retireAgentUpdateJobsFromHeartbeat(context.Background(), node, heartbeatRequest{
		AgentVersion:    "dev (linux/amd64)",
		AgentReleaseSeq: 5,
	})

	if got := store.jobs[jobID].Status; got != storage.JobStatusSucceeded {
		t.Fatalf("job status = %s, want succeeded", got)
	}
}

type agentUpdatePendingStore struct {
	*fakeStore
	pending *storage.Job
}

func (s *agentUpdatePendingStore) GetPendingAgentUpdateJob(_ context.Context, _ uuid.UUID) (*storage.Job, error) {
	return s.pending, nil
}

// patchCompletionStore wraps fakeStore to (a) return a known
// node_patch_state for any job_id and (b) record which state IDs were marked
// applied or failed. This lets the consumer-side test observe whether the
// patch case actually fired or fell through to the default branch.
type patchCompletionStore struct {
	*fakeStore
	state          *storage.NodePatchState
	appliedIDs     []uuid.UUID
	failedIDs      []uuid.UUID
	rollupSeen     []uuid.UUID
	deploymentRows []storage.NodePatchState
}

func (p *patchCompletionStore) GetNodePatchStateByJobID(_ context.Context, _ uuid.UUID) (*storage.NodePatchState, error) {
	return p.state, nil
}

func (p *patchCompletionStore) MarkNodePatchApplied(_ context.Context, id uuid.UUID, _ int, _ string) error {
	p.appliedIDs = append(p.appliedIDs, id)
	return nil
}

func (p *patchCompletionStore) MarkNodePatchFailed(_ context.Context, id uuid.UUID, _ string, _ string) error {
	p.failedIDs = append(p.failedIDs, id)
	return nil
}

func (p *patchCompletionStore) ListNodePatchStatesForDeployment(_ context.Context, deploymentID uuid.UUID) ([]storage.NodePatchState, error) {
	p.rollupSeen = append(p.rollupSeen, deploymentID)
	return p.deploymentRows, nil
}

// TestHeartbeatCompletedActionsAcceptAllPatchModes pins the consumer side of
// the fix: previously the completed_actions switch only matched
// JobTypePatchDeployDirect, so an agent reporting success for a proxy- or
// airgapped-mode deployment fell through to the "ignore unknown" default and
// the node_patch_state row stayed pending forever. The fix adds the proxy +
// airgapped cases. Each subtest sends one completed action per mode and
// asserts MarkNodePatchApplied was invoked.
func TestHeartbeatCompletedActionsAcceptAllPatchModes(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{JobTypePatchDeployDirect, JobTypePatchDeployProxy, JobTypePatchDeployAirgapped} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			tenantID := uuid.New()
			nodeID := uuid.New()
			deploymentID := uuid.New()
			stateID := uuid.New()
			jobID := uuid.New()

			ps := &storage.NodePatchState{
				ID: stateID, DeploymentID: deploymentID, NodeID: nodeID,
				TenantID: tenantID, Status: "pending", JobID: &jobID,
			}
			base := &fakeStore{
				jobs: map[uuid.UUID]*storage.Job{
					jobID: {ID: jobID, TenantID: tenantID, Type: mode, Status: storage.JobStatusRunning},
				},
			}
			store := &patchCompletionStore{fakeStore: base, state: ps}

			srv := &Server{logger: zap.NewNop(), store: store}

			completed := []heartbeatCompletedAction{{
				Action:   mode,
				JobID:    jobID.String(),
				Status:   "succeeded",
				Metadata: map[string]any{"packages_upgraded": float64(3), "log_tail": "ok"},
			}}
			srv.processHeartbeatCompletedActions(context.Background(), nodeID, completed)

			if len(store.appliedIDs) != 1 || store.appliedIDs[0] != stateID {
				t.Fatalf("MarkNodePatchApplied not called for mode %s; appliedIDs=%v", mode, store.appliedIDs)
			}
			if got := base.jobs[jobID].Status; got != storage.JobStatusSucceeded {
				t.Fatalf("job status = %s for mode %s, want succeeded", got, mode)
			}
		})
	}
}
