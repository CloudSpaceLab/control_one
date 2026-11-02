package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type onlineTestStore struct {
	*patchSecurityStore
	request  *patchsupport.OnlinePreparation
	pending  []storage.Job
	finished int
	roles    []string
	cfg      *storage.NodePatchConfig
	proxy    *storage.SquidProxy
}

func (s *onlineTestStore) UserHasTenantRole(_ context.Context, _ uuid.UUID, _ uuid.UUID, roles []string) (bool, error) {
	s.roles = roles
	return s.allowed, nil
}
func (s *onlineTestStore) GetNodePatchConfig(context.Context, uuid.UUID) (*storage.NodePatchConfig, error) {
	return s.cfg, nil
}
func (s *onlineTestStore) GetSquidProxy(context.Context, uuid.UUID) (*storage.SquidProxy, error) {
	return s.proxy, nil
}
func (s *onlineTestStore) QueueOnlinePreparation(_ context.Context, tenant, node uuid.UUID, p patchsupport.OnlinePreparation) (*storage.Job, error) {
	s.request = &p
	body, _ := json.Marshal(p)
	return &storage.Job{ID: uuid.New(), TenantID: tenant, Type: patchsupport.PrepareOnlineJob, Status: storage.JobStatusRunning, Payload: body}, nil
}
func (s *onlineTestStore) PendingOnlinePreparations(context.Context, uuid.UUID, uuid.UUID) ([]storage.Job, error) {
	return s.pending, nil
}
func (s *onlineTestStore) FinishOnlinePreparation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string, string, *patchsupport.OnlinePlan) error {
	s.finished++
	return nil
}

func reviewedOnlineFixture(t *testing.T, mode string) (*Server, *onlineTestStore, patchsupport.OnlinePlan) {
	t.Helper()
	tenant, node := uuid.New(), uuid.New()
	store := &onlineTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node), allowed: true}}
	principal := onlineOperator(store)
	store.users = map[string]*storage.User{principal.Subject: {ID: uuid.MustParse(principal.Subject), ExternalID: principal.Subject}}
	store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", OnlinePlanVersion: 1}, ObservedAt: time.Now().UTC()}
	if mode == "proxy" {
		id := uuid.New()
		store.cfg = &storage.NodePatchConfig{NodeID: node, Mode: "proxy", ProxyID: &id}
		store.proxy = &storage.SquidProxy{ID: id, TenantID: tenant, Host: "proxy.local", Port: 3128, Whitelist: []string{"archive.ubuntu.com"}}
	}
	srv := newPatchTestServer(store)
	digest, proxy, hosts, err := srv.onlineTransportContext(context.Background(), &store.nodes[0], mode)
	require.NoError(t, err)
	now := time.Now().UTC()
	p := patchsupport.OnlinePlan{RepositoryHosts: hosts, Version: 1, NodeID: node.String(), Mode: mode, ProxyURL: proxy, TransportContext: digest, InstalledStateSHA256: strings.Repeat("b", 64), InputsSHA256: strings.Repeat("c", 64), ManifestSHA256: strings.Repeat("d", 64), Requested: []string{"liba"}, Resolved: []patchsupport.ExactPackage{{Name: "liba", Version: "2"}}, Review: patchsupport.OfflineReview{PreparedAt: now.Add(-time.Minute), ExpiresAt: now.Add(59 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{{Name: "liba", InstalledVersion: "1", ProposedVersion: "2", Role: "requested"}}}}
	p.SHA256 = p.Digest()
	store.nodes[0].Labels["patch.online_plan"] = onlinePlanEvidence{Status: "ready", Plan: p, JobID: uuid.NewString(), ObservedAt: now}
	return srv, store, p
}

func TestOnlinePreparationPermissionsAndScope(t *testing.T) {
	srv, store, p := reviewedOnlineFixture(t, "direct")
	body, _ := json.Marshal(map[string]any{"node_id": p.NodeID, "mode": "direct", "packages": []string{"liba"}})
	rec := httptest.NewRecorder()
	srv.handleOnlinePreparation(rec, withPrincipal(httptest.NewRequest("POST", "/api/v1/patch/online/prepare", bytes.NewReader(body)), onlineOperator(store)))
	require.Equal(t, 202, rec.Code, rec.Body.String())
	require.Equal(t, []string{roleOperator, roleAdmin}, store.roles)
	require.Equal(t, p.TransportContext, store.request.TransportContext)
	require.Empty(t, store.jobs, "read-only preparation must not dispatch installation")
	store.allowed = false
	rec = httptest.NewRecorder()
	srv.handleOnlinePreparation(rec, withPrincipal(httptest.NewRequest("POST", "/api/v1/patch/online/prepare", bytes.NewReader(body)), onlineOperator(store)))
	require.Equal(t, 403, rec.Code)
}

func TestOnlinePolicyRejectsScopeModeExpiryAndPublicationDrift(t *testing.T) {
	for _, scenario := range []string{"valid", "missing", "scope", "deny", "node", "mode", "expired", "withdrawn", "stale", "replaced"} {
		t.Run(scenario, func(t *testing.T) {
			srv, store, p := reviewedOnlineFixture(t, "direct")
			node := &store.nodes[0]
			policy := patchPackagePolicy{OnlinePlan: &p, Allowlist: []string{"liba"}, PostPatchRescan: true}
			mode := "direct"
			switch scenario {
			case "missing":
				policy.OnlinePlan = nil
			case "scope":
				policy.Allowlist = []string{"other"}
			case "deny":
				policy.Denylist = []string{"other"}
			case "node":
				p.NodeID = uuid.NewString()
				p.SHA256 = p.Digest()
			case "mode":
				mode = "proxy"
			case "expired":
				p.Review.ExpiresAt = time.Now().Add(-time.Second)
				p.SHA256 = p.Digest()
			case "withdrawn":
				node.Labels["patch.online_plan"] = onlinePlanEvidence{Status: "unavailable", Plan: p, JobID: uuid.NewString(), ObservedAt: time.Now().UTC()}
			case "stale":
				node.Labels["patch.online_plan"] = onlinePlanEvidence{Status: "ready", Plan: p, JobID: uuid.NewString(), ObservedAt: time.Now().Add(-6 * time.Minute)}
			case "replaced":
				copy := p
				copy.ManifestSHA256 = strings.Repeat("e", 64)
				copy.SHA256 = copy.Digest()
				node.Labels["patch.online_plan"] = onlinePlanEvidence{Status: "ready", Plan: copy, JobID: uuid.NewString(), ObservedAt: time.Now().UTC()}
			}
			err := srv.validateOnlinePolicy(context.Background(), node, mode, policy, time.Now().UTC())
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestOnlineProxyConfigurationDriftAndExplicitAssociation(t *testing.T) {
	srv, store, p := reviewedOnlineFixture(t, "proxy")
	policy := patchPackagePolicy{OnlinePlan: &p, Allowlist: p.Requested, PostPatchRescan: true}
	node := &store.nodes[0]
	require.NoError(t, srv.validateOnlinePolicy(context.Background(), node, "proxy", policy, time.Now().UTC()))
	mode, id, _ := srv.resolvePatchModeForNode(context.Background(), node.ID, "proxy")
	require.Equal(t, "proxy", mode)
	require.Equal(t, store.cfg.ProxyID, id)
	store.proxy.Whitelist = []string{"different.example"}
	require.ErrorContains(t, srv.validateOnlinePolicy(context.Background(), node, "proxy", policy, time.Now().UTC()), "transport/proxy policy changed")
	prep := patchsupport.OnlinePreparation{RepositoryHosts: p.RepositoryHosts, NodeID: p.NodeID, Mode: p.Mode, ProxyURL: p.ProxyURL, TransportContext: p.TransportContext, Packages: p.Requested, ExpiresAt: time.Now().Add(time.Minute)}
	raw, _ := json.Marshal(prep)
	store.pending = []storage.Job{{ID: uuid.New(), TenantID: node.TenantID, Type: patchsupport.PrepareOnlineJob, Status: storage.JobStatusRunning, Payload: raw}}
	require.Empty(t, srv.onlinePreparationActions(context.Background(), node))
	require.Equal(t, 1, store.finished)
}

func TestOnlinePlanAlwaysRequiresApprovalAndRejectsChangedSelection(t *testing.T) {
	srv, store, p := reviewedOnlineFixture(t, "direct")
	node := &store.nodes[0]
	tenant := node.TenantID
	store.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{tenant: {TenantID: tenant, PatchRequiresApproval: false}}
	body, _ := json.Marshal(patchDeployRequest{TenantID: tenant.String(), NodeIDs: []string{p.NodeID}, Mode: "direct", PackageAllowlist: p.Requested, OnlinePlan: &p})
	rec := httptest.NewRecorder()
	srv.handlePatchDeployments(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body)), onlineOperator(store)))
	require.Equal(t, 201, rec.Code, rec.Body.String())
	require.Len(t, store.patchApprovals, 1)
	require.Empty(t, store.jobs)
	var approval storage.PatchApproval
	for _, a := range store.patchApprovals {
		approval = a
	}
	require.Equal(t, p.Review.ExpiresAt, approval.ExpiresAt)
	node.Labels["patch.online_plan"] = onlinePlanEvidence{Status: "unavailable", Plan: p, JobID: uuid.NewString(), ObservedAt: time.Now().UTC()}
	rec = httptest.NewRecorder()
	srv.handlePatchApprovalSubroutes(rec, withPrincipal(httptest.NewRequest("POST", "/api/v1/patch/approvals/"+approval.ID.String()+"/approve", nil), onlineOperator(store)))
	require.Equal(t, 409, rec.Code, rec.Body.String())
	require.Empty(t, store.jobs)
	require.Equal(t, storage.ApprovalStatusPending, store.patchApprovals[approval.ID].Status)
}

func TestOnlineExplicitModesRetainNodeCalendar(t *testing.T) {
	srv, store, _ := reviewedOnlineFixture(t, "direct")
	window := uuid.New()
	store.cfg = &storage.NodePatchConfig{NodeID: store.nodes[0].ID, Mode: "direct", WindowID: &window}
	for _, mode := range []string{"direct", "proxy", "airgapped"} {
		actual, _, bound := srv.resolvePatchModeForNode(context.Background(), store.nodes[0].ID, mode)
		require.Equal(t, mode, actual)
		require.Equal(t, &window, bound)
	}
}

func onlineOperator(store *onlineTestStore) *auth.Principal {
	p := patchOperatorPrincipal()
	if store.users == nil {
		store.users = map[string]*storage.User{}
	}
	store.users[p.Subject] = &storage.User{ID: uuid.MustParse(p.Subject), ExternalID: p.Subject}
	return p
}

func TestOnlineExecutionAuthorizationRechecksLinkedJobAndPolicy(t *testing.T) {
	for _, scenario := range []string{"valid", "terminal", "unlinked", "unapproved", "runtime", "proxy_changed", "calendar_changed", "publication_withdrawn", "breaker_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			mode := "direct"
			if scenario == "proxy_changed" {
				mode = "proxy"
			}
			srv, store, p := reviewedOnlineFixture(t, mode)
			node := &store.nodes[0]
			ctx := context.Background()
			policy := patchPackagePolicy{OnlinePlan: &p, Allowlist: p.Requested, PostPatchRescan: true}
			d, err := store.CreatePatchDeployment(ctx, storage.PatchDeployment{TenantID: node.TenantID, Mode: mode, Summary: map[string]any{"online_plan": p, "package_allowlist": p.Requested, "post_patch_rescan": true}})
			require.NoError(t, err)
			var proxyID *uuid.UUID
			if store.cfg != nil {
				proxyID = store.cfg.ProxyID
			}
			hash, _, err := srv.patchApprovalPolicyHash(ctx, node.TenantID, node.ID, mode, proxyID, nil, policy)
			require.NoError(t, err)
			a := storage.PatchApproval{ID: uuid.New(), TenantID: node.TenantID, NodeID: node.ID, DeploymentID: d.ID, Mode: mode, ProxyID: proxyID, Status: storage.ApprovalStatusApproved, PolicyHash: hash, ExpiresAt: p.Review.ExpiresAt}
			store.patchApprovals = map[uuid.UUID]storage.PatchApproval{a.ID: a}
			jobID, stateID := uuid.New(), uuid.New()
			runtime, err := nodePatchRuntime(node, time.Now().UTC())
			require.NoError(t, err)
			payload := patchJobPayload{OnlinePlan: &p, NodeID: node.ID.String(), NodePatchStateID: stateID.String(), DeploymentID: d.ID.String(), Mode: mode, ProxyURL: p.ProxyURL, Runtime: &runtime, PackageAllowlist: p.Requested, PostPatchRescan: true}
			raw, _ := json.Marshal(payload)
			store.jobs = map[uuid.UUID]*storage.Job{jobID: {ID: jobID, TenantID: node.TenantID, Type: "patch.deploy_" + mode, Status: storage.JobStatusRunning, Payload: raw}}
			store.states = []storage.NodePatchState{{ID: stateID, TenantID: node.TenantID, NodeID: node.ID, DeploymentID: d.ID, Status: "pending", JobID: &jobID}}
			switch scenario {
			case "terminal":
				store.jobs[jobID].Status = storage.JobStatusSucceeded
			case "unlinked":
				store.states[0].JobID = nil
			case "unapproved":
				store.patchApprovals = map[uuid.UUID]storage.PatchApproval{}
			case "runtime":
				runtime.OnlinePlanVersion = 0
				node.Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: runtime, ObservedAt: time.Now().UTC()}
			case "proxy_changed":
				store.proxy.Whitelist = []string{"different.example"}
			case "calendar_changed":
				window := uuid.New()
				store.cfg = &storage.NodePatchConfig{NodeID: node.ID, Mode: mode, WindowID: &window}
			case "publication_withdrawn":
				node.Labels["patch.online_plan"] = onlinePlanEvidence{Status: "unavailable", Plan: p, JobID: uuid.NewString(), ObservedAt: time.Now().UTC()}
			case "breaker_unavailable":
				store.failRead = "breaker"
			}
			err = srv.validateOnlineExecutionAuthorization(ctx, node, mode, jobID.String(), p.SHA256)
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestOnlineGenericJobsCannotBypassApproval(t *testing.T) {
	srv, store, p := reviewedOnlineFixture(t, "direct")
	for _, kind := range []string{"patch.deploy_direct", "patch.deploy_proxy", patchsupport.PrepareOnlineJob} {
		body, _ := json.Marshal(map[string]any{"type": kind, "tenant_id": store.nodes[0].TenantID.String(), "payload": map[string]any{"node_id": p.NodeID}})
		rec := httptest.NewRecorder()
		srv.handleCreateJob(rec, withPrincipal(httptest.NewRequest("POST", "/api/v1/jobs", bytes.NewReader(body)), onlineOperator(store)))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		require.Empty(t, store.jobs)
	}
}
