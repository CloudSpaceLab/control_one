package server

import (
	"bytes"
	"context"
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

// patchTestStore narrows fakeStore so patch tests can reach in-memory rows
// for both deployments and node patch states. Keeping these fields private
// to the test package avoids polluting the production fakeStore with
// state-tracking the rest of the suite doesn't need.
type patchTestStore struct {
	*fakeStore
	deployments map[uuid.UUID]*storage.PatchDeployment
	states      []storage.NodePatchState
}

func newPatchTestStore(tenantID, nodeID uuid.UUID) *patchTestStore {
	return &patchTestStore{
		fakeStore: &fakeStore{
			tenants: []storage.Tenant{{ID: tenantID, Name: "patch-tenant", CreatedAt: time.Now()}},
			nodes: []storage.Node{{
				ID:        nodeID,
				TenantID:  tenantID,
				Hostname:  "patch-host",
				State:     storage.NodeStateActive,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}},
		},
		deployments: map[uuid.UUID]*storage.PatchDeployment{},
	}
}

func (p *patchTestStore) CreatePatchDeployment(_ context.Context, in storage.PatchDeployment) (*storage.PatchDeployment, error) {
	in.ID = uuid.New()
	in.RequestedAt = time.Now().UTC()
	if in.Status == "" {
		in.Status = "pending"
	}
	saved := in
	p.deployments[in.ID] = &saved
	out := saved
	return &out, nil
}

func (p *patchTestStore) GetPatchDeployment(_ context.Context, id uuid.UUID) (*storage.PatchDeployment, error) {
	d, ok := p.deployments[id]
	if !ok {
		return nil, nil
	}
	copy := *d
	return &copy, nil
}

func (p *patchTestStore) UpdatePatchDeploymentStatus(_ context.Context, id uuid.UUID, status string, _ bool) error {
	if d, ok := p.deployments[id]; ok {
		d.Status = status
	}
	return nil
}

func (p *patchTestStore) CreateNodePatchState(_ context.Context, in storage.NodePatchState) (*storage.NodePatchState, error) {
	in.ID = uuid.New()
	in.RequestedAt = time.Now().UTC()
	if in.Status == "" {
		in.Status = "pending"
	}
	p.states = append(p.states, in)
	out := in
	return &out, nil
}

// patchOperatorPrincipal returns a stand-in operator principal for the patch
// API tests. Subject is a real UUID so approver_id round-trips cleanly.
func patchOperatorPrincipal() *auth.Principal {
	return &auth.Principal{
		Type:    "user",
		Name:    "operator@example.com",
		Subject: uuid.New().String(),
		Roles:   []string{"operator"},
	}
}

func newPatchTestServer(store Store) *Server {
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}
	return New(zap.NewNop(), cfg, store, &stubQueue{})
}

// TestPatchDeploy_ApprovalRequired_ParksRow confirms that when a tenant has
// patch_requires_approval=true (the production default), a deploy request
// does NOT dispatch the underlying patch.deploy_* job. Instead it writes
// a pending row to patch_approvals and the deployment header sits in
// 'pending' awaiting operator green-light.
func TestPatchDeploy_ApprovalRequired_ParksRow(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := newPatchTestStore(tenantID, nodeID)
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{
		tenantID: {
			TenantID:              tenantID,
			MinApprovalSeverity:   "high",
			ChangeWindows:         []storage.ChangeWindow{},
			CriticalOverride:      true,
			PatchRequiresApproval: true,
		},
	}

	srv := newPatchTestServer(store)

	body, _ := json.Marshal(patchDeployRequest{
		TenantID: tenantID.String(),
		NodeIDs:  []string{nodeID.String()},
		Mode:     "direct",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body))
	req = withPrincipal(req, patchOperatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handlePatchDeployments(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp patchDeployResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.NodeCount != 0 {
		t.Fatalf("expected 0 dispatched nodes when gate parks the deploy, got %d", resp.NodeCount)
	}
	if len(resp.AwaitingApproval) != 1 {
		t.Fatalf("expected 1 awaiting_approval entry, got %d", len(resp.AwaitingApproval))
	}
	if resp.AwaitingApproval[0]["node_id"] != nodeID.String() {
		t.Fatalf("expected awaiting node_id=%s, got %s", nodeID.String(), resp.AwaitingApproval[0]["node_id"])
	}
	if _, ok := resp.AwaitingApproval[0]["approval_id"]; !ok {
		t.Fatalf("expected approval_id in awaiting entry, got %+v", resp.AwaitingApproval[0])
	}

	// No NodePatchState should have been created yet — the dispatch is
	// gated behind the approval.
	if len(store.states) != 0 {
		t.Fatalf("expected 0 dispatched node patch states pre-approval, got %d", len(store.states))
	}

	// The approval row should be pending.
	store.fakeStore.mu.Lock()
	defer store.fakeStore.mu.Unlock()
	if len(store.fakeStore.patchApprovals) != 1 {
		t.Fatalf("expected 1 pending patch_approval, got %d", len(store.fakeStore.patchApprovals))
	}
	for _, a := range store.fakeStore.patchApprovals {
		if a.Status != storage.ApprovalStatusPending {
			t.Fatalf("approval status = %q, want pending", a.Status)
		}
		if a.NodeID != nodeID || a.TenantID != tenantID {
			t.Fatalf("approval shape mismatch: %+v", a)
		}
	}
}

// TestPatchApprove_FlipsAndDispatches confirms the operator approval flow:
// hit /approve, the row flips to approved, and dispatchPatchModeToNode runs
// — manifesting as a new NodePatchState row. This is the critical fix for
// bugs §3.1 (no approval-then-redispatch loop).
func TestPatchApprove_FlipsAndDispatches(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := newPatchTestStore(tenantID, nodeID)
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{
		tenantID: {
			TenantID:              tenantID,
			MinApprovalSeverity:   "high",
			CriticalOverride:      true,
			PatchRequiresApproval: true,
		},
	}

	srv := newPatchTestServer(store)

	// 1. Operator queues the deploy — gate parks it.
	body, _ := json.Marshal(patchDeployRequest{
		TenantID: tenantID.String(),
		NodeIDs:  []string{nodeID.String()},
		Mode:     "direct",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body))
	req = withPrincipal(req, patchOperatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handlePatchDeployments(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create deploy status=%d body=%s", rec.Code, rec.Body.String())
	}

	var firstResp patchDeployResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &firstResp)
	if len(firstResp.AwaitingApproval) != 1 {
		t.Fatalf("step 1: expected 1 awaiting approval, got %d", len(firstResp.AwaitingApproval))
	}
	approvalIDStr := firstResp.AwaitingApproval[0]["approval_id"]
	approvalID, err := uuid.Parse(approvalIDStr)
	if err != nil {
		t.Fatalf("approval_id parse: %v", err)
	}

	// Sanity: no dispatched state yet.
	if len(store.states) != 0 {
		t.Fatalf("step 1: expected 0 dispatched states, got %d", len(store.states))
	}

	// 2. Operator approves.
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/patch/approvals/"+approvalIDStr+"/approve", nil)
	approveReq = withPrincipal(approveReq, patchOperatorPrincipal())
	approveRec := httptest.NewRecorder()
	srv.handlePatchApprovalSubroutes(approveRec, approveReq)

	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	var approvedResp patchApprovalResponse
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approvedResp); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	if approvedResp.Status != string(storage.ApprovalStatusApproved) {
		t.Fatalf("status = %q, want approved", approvedResp.Status)
	}
	if approvedResp.ID != approvalID.String() {
		t.Fatalf("approval id mismatch: %s vs %s", approvedResp.ID, approvalID)
	}

	// 3. Dispatch must have fired — exactly one NodePatchState row for
	//    the (deployment, node) pair.
	if len(store.states) != 1 {
		t.Fatalf("step 3: expected 1 dispatched state post-approval, got %d", len(store.states))
	}
	if store.states[0].NodeID != nodeID || store.states[0].TenantID != tenantID {
		t.Fatalf("state shape mismatch: %+v", store.states[0])
	}
}

// TestPatchDeploy_ApprovalNotRequired_DispatchesImmediately confirms the
// legacy immediate-dispatch behaviour for tenants that explicitly opt out
// of the gate (PatchRequiresApproval=false). This is the fallback the
// timeline §11 D1 calls "the 4-6h flag flip" — kept reachable per the
// "configurable per tenant" requirement.
func TestPatchDeploy_ApprovalNotRequired_DispatchesImmediately(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := newPatchTestStore(tenantID, nodeID)
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{
		tenantID: {
			TenantID:              tenantID,
			MinApprovalSeverity:   "high",
			CriticalOverride:      true,
			PatchRequiresApproval: false, // opt-out: legacy path
		},
	}

	srv := newPatchTestServer(store)

	body, _ := json.Marshal(patchDeployRequest{
		TenantID: tenantID.String(),
		NodeIDs:  []string{nodeID.String()},
		Mode:     "direct",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body))
	req = withPrincipal(req, patchOperatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handlePatchDeployments(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp patchDeployResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.NodeCount != 1 {
		t.Fatalf("expected NodeCount=1 on legacy path, got %d (gate_blocked=%d awaiting=%d failed=%d)",
			resp.NodeCount, len(resp.GateBlocked), len(resp.AwaitingApproval), len(resp.Failed))
	}
	if len(resp.AwaitingApproval) != 0 {
		t.Fatalf("expected 0 awaiting_approval on legacy path, got %d", len(resp.AwaitingApproval))
	}
	if len(resp.Succeeded) != 1 || resp.Succeeded[0] != nodeID.String() {
		t.Fatalf("expected succeeded=[%s], got %+v", nodeID.String(), resp.Succeeded)
	}
	if len(store.states) != 1 {
		t.Fatalf("expected 1 dispatched state on legacy path, got %d", len(store.states))
	}

	// And no patch_approvals row was written — confirms the gate was a
	// no-op for this tenant.
	store.fakeStore.mu.Lock()
	defer store.fakeStore.mu.Unlock()
	if len(store.fakeStore.patchApprovals) != 0 {
		t.Fatalf("expected 0 patch_approvals on legacy path, got %d", len(store.fakeStore.patchApprovals))
	}
}

// TestPatchDeny_NoDispatch confirms the denial branch: rejecting an
// approval keeps the row in 'denied' and never invokes
// dispatchPatchModeToNode.
func TestPatchDeny_NoDispatch(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := newPatchTestStore(tenantID, nodeID)
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{
		tenantID: {
			TenantID:              tenantID,
			MinApprovalSeverity:   "high",
			CriticalOverride:      true,
			PatchRequiresApproval: true,
		},
	}

	srv := newPatchTestServer(store)

	// Park the deploy.
	body, _ := json.Marshal(patchDeployRequest{
		TenantID: tenantID.String(),
		NodeIDs:  []string{nodeID.String()},
		Mode:     "direct",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body))
	req = withPrincipal(req, patchOperatorPrincipal())
	rec := httptest.NewRecorder()
	srv.handlePatchDeployments(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var firstResp patchDeployResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &firstResp)
	approvalID := firstResp.AwaitingApproval[0]["approval_id"]

	// Operator denies.
	denyReq := httptest.NewRequest(http.MethodPost, "/api/v1/patch/approvals/"+approvalID+"/deny", nil)
	denyReq = withPrincipal(denyReq, patchOperatorPrincipal())
	denyRec := httptest.NewRecorder()
	srv.handlePatchApprovalSubroutes(denyRec, denyReq)
	if denyRec.Code != http.StatusOK {
		t.Fatalf("deny status=%d body=%s", denyRec.Code, denyRec.Body.String())
	}

	var deniedResp patchApprovalResponse
	_ = json.Unmarshal(denyRec.Body.Bytes(), &deniedResp)
	if deniedResp.Status != string(storage.ApprovalStatusDenied) {
		t.Fatalf("status = %q, want denied", deniedResp.Status)
	}

	// No dispatch ever happened.
	if len(store.states) != 0 {
		t.Fatalf("expected 0 dispatched states post-deny, got %d", len(store.states))
	}
}
