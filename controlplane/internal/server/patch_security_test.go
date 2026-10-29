package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type patchSecurityStore struct {
	*patchTestStore
	allowed  bool
	failRead string
}

func (s *patchSecurityStore) UserHasTenantRole(context.Context, uuid.UUID, uuid.UUID, []string) (bool, error) {
	return s.allowed, nil
}

func (s *patchSecurityStore) GetNode(ctx context.Context, id uuid.UUID) (*storage.Node, error) {
	if s.failRead == "node" {
		return nil, errors.New("node read unavailable")
	}
	return s.patchTestStore.GetNode(ctx, id)
}

func (s *patchSecurityStore) GetTenantRemediationConfig(ctx context.Context, id uuid.UUID) (*storage.TenantRemediationConfig, error) {
	if s.failRead == "policy" {
		return nil, errors.New("policy read unavailable")
	}
	return s.patchTestStore.GetTenantRemediationConfig(ctx, id)
}

func (s *patchSecurityStore) GetCircuitBreakerState(ctx context.Context, id uuid.UUID, rule string) (*storage.RemediationCircuitBreakerState, error) {
	if s.failRead == "breaker" {
		return nil, errors.New("breaker read unavailable")
	}
	return s.patchTestStore.GetCircuitBreakerState(ctx, id, rule)
}

func TestPatchRoutesRejectUnassignedTenant(t *testing.T) {
	tenantID, nodeID, deploymentID, userID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	store := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID)}
	store.users = map[string]*storage.User{"patch-user": {ID: userID, ExternalID: "patch-user"}}
	store.deployments[deploymentID] = &storage.PatchDeployment{ID: deploymentID, TenantID: tenantID}
	srv := &Server{store: store, logger: zap.NewNop()}
	for _, route := range []struct {
		name, method, path, body string
		handler                  http.HandlerFunc
	}{
		{"plan", "POST", "/api/v1/patch/deployments/plan", `{"tenant_id":"` + tenantID.String() + `"}`, srv.handlePatchDeploymentPlan},
		{"nodes", "GET", "/api/v1/patch/deployments/" + deploymentID.String() + "/nodes", "", func(w http.ResponseWriter, r *http.Request) { srv.handleListPatchDeploymentNodes(w, r, deploymentID) }},
		{"advance", "POST", "/api/v1/patch/deployments/" + deploymentID.String() + "/advance", "", func(w http.ResponseWriter, r *http.Request) { srv.handleAdvancePatchDeployment(w, r, deploymentID) }},
		{"config", "GET", "/api/v1/patch/config?node_id=" + nodeID.String(), "", srv.handleGetPatchConfig},
		{"windows", "GET", "/api/v1/patch/maintenance-windows?tenant_id=" + tenantID.String(), "", srv.handleListMaintenanceWindows},
		{"proxies", "GET", "/api/v1/patch/proxies?tenant_id=" + tenantID.String(), "", srv.handleListSquidProxies},
	} {
		for _, role := range []string{roleOperator, roleAdmin} {
			t.Run(route.name+"/"+role, func(t *testing.T) {
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				principal := &auth.Principal{Type: "user", Subject: "patch-user", Roles: []string{role}}
				req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, principal))
				rec := httptest.NewRecorder()
				route.handler(rec, req)
				require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			})
		}
	}
}

func TestDispatchedPatchJobsCannotBeCancelled(t *testing.T) {
	for _, mode := range []string{JobTypePatchDeployDirect, JobTypePatchDeployProxy, JobTypePatchDeployAirgapped} {
		for _, status := range []storage.JobStatus{storage.JobStatusQueued, storage.JobStatusRunning, storage.JobStatusSucceeded} {
			t.Run(mode+"/"+string(status), func(t *testing.T) {
				tenantID, nodeID, userID, jobID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
				store := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID), allowed: true}
				store.users = map[string]*storage.User{"patch-user": {ID: userID, ExternalID: "patch-user"}}
				store.jobs = map[uuid.UUID]*storage.Job{jobID: {ID: jobID, TenantID: tenantID, Type: mode, Status: status}}
				srv := &Server{store: store, logger: zap.NewNop()}
				req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID.String()+"/cancel", nil)
				req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: "patch-user", Roles: []string{roleOperator}}))
				rec := httptest.NewRecorder()
				srv.handleCancelJob(rec, req, jobID)
				require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
				require.Equal(t, status, store.jobs[jobID].Status)
			})
		}
	}
}

func TestPatchSafetyReadFailuresBlockDispatch(t *testing.T) {
	for _, failedRead := range []string{"node", "policy", "breaker"} {
		t.Run(failedRead, func(t *testing.T) {
			tenantID, nodeID := uuid.New(), uuid.New()
			store := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID), failRead: failedRead}
			srv := &Server{store: store, logger: zap.NewNop()}
			preview := srv.previewPatchSafetyGates(context.Background(), tenantID, nodeID, patchModeDirect, nil)
			require.False(t, preview.Allowed)
			require.False(t, preview.AwaitingApproval)
			gate := srv.runPatchSafetyGates(context.Background(), tenantID, nodeID, uuid.New(), patchModeDirect, nil, nil)
			require.False(t, gate.Allowed)
			require.False(t, gate.AwaitingApproval)
		})
	}
}

func TestPatchApprovalRejectsChangedPlanAndSafety(t *testing.T) {
	for _, change := range []string{"package_policy", "manual_only", "isolation_changed", "policy_unavailable", "legacy_snapshot"} {
		t.Run(change, func(t *testing.T) {
			tenantID, nodeID := uuid.New(), uuid.New()
			store := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID), allowed: true}
			principal := patchOperatorPrincipal()
			store.users = map[string]*storage.User{principal.Subject: {ID: uuid.MustParse(principal.Subject), ExternalID: principal.Subject}}
			srv := newPatchTestServer(store)
			body, _ := json.Marshal(patchDeployRequest{TenantID: tenantID.String(), NodeIDs: []string{nodeID.String()}, Mode: patchModeDirect, PackageAllowlist: []string{"curl"}})
			req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body)), principal)
			rec := httptest.NewRecorder()
			srv.handlePatchDeployments(rec, req)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			require.Len(t, store.patchApprovals, 1)
			var approval storage.PatchApproval
			for _, a := range store.patchApprovals {
				approval = a
			}
			require.NotEmpty(t, approval.PolicyHash)
			switch change {
			case "package_policy":
				store.deployments[approval.DeploymentID].Summary["package_allowlist"] = []string{"openssl"}
			case "manual_only":
				store.nodes[0].Labels = map[string]any{"remediation": "manual-only"}
			case "isolation_changed":
				store.nodes[0].Labels = map[string]any{isolationModeLabel: isolationModeAirgapped}
			case "policy_unavailable":
				store.failRead = "policy"
			case "legacy_snapshot":
				approval.PolicyHash = ""
				store.patchApprovals[approval.ID] = approval
			}
			req = withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/approvals/"+approval.ID.String()+"/approve", nil), principal)
			rec = httptest.NewRecorder()
			srv.handlePatchApprovalSubroutes(rec, req)
			require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
			require.Equal(t, storage.ApprovalStatusPending, store.patchApprovals[approval.ID].Status)
			require.Empty(t, store.jobs)
		})
	}
}
