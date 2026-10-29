package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"testing"
)

type atomicDispatchFailureStore struct {
	*patchSecurityStore
	calls  int
	params storage.DispatchPatchJobParams
}

func (s *atomicDispatchFailureStore) DispatchPatchJob(_ context.Context, p storage.DispatchPatchJobParams) (*storage.NodePatchState, error) {
	s.calls++
	s.params = p
	return nil, errors.New("injected transaction failure")
}

func TestPatchApprovalTransactionFailureRemainsPending(t *testing.T) {
	tenantID, nodeID := uuid.New(), uuid.New()
	base := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID), allowed: true}
	base.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{tenantID: {TenantID: tenantID, PatchRequiresApproval: true}}
	principal := patchOperatorPrincipal()
	base.users = map[string]*storage.User{principal.Subject: {ID: uuid.MustParse(principal.Subject), ExternalID: principal.Subject}}
	store := &atomicDispatchFailureStore{patchSecurityStore: base}
	srv := newPatchTestServer(base)
	srv.store = store
	body, _ := json.Marshal(patchDeployRequest{TenantID: tenantID.String(), NodeIDs: []string{nodeID.String()}, Mode: patchModeDirect, PackageAllowlist: []string{"curl"}})
	rec := httptest.NewRecorder()
	srv.handlePatchDeployments(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments", bytes.NewReader(body)), principal))
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, base.patchApprovals, 1)
	var approval storage.PatchApproval
	for _, a := range base.patchApprovals {
		approval = a
	}
	rec = httptest.NewRecorder()
	srv.handlePatchApprovalSubroutes(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/approvals/"+approval.ID.String()+"/approve", nil), principal))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	require.Equal(t, 1, store.calls)
	require.Equal(t, approval.ID, store.params.ApprovalID)
	require.Equal(t, approval.PolicyHash, store.params.PolicyHash)
	require.Equal(t, storage.ApprovalStatusPending, base.patchApprovals[approval.ID].Status)
	require.Empty(t, base.jobs)
	rows, err := base.ListNodePatchStatesForDeployment(context.Background(), approval.DeploymentID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "pending", rows[0].Status)
}

type waveLockStore struct {
	*patchSecurityStore
	acquired bool
	err      error
	released bool
}

type offlineDispatchRecoveryStore struct {
	*waveLockStore
	outcome string
	calls   int
}

func (s *offlineDispatchRecoveryStore) ReconcileIncompletePatchDispatches(context.Context, uuid.UUID) (int, error) {
	s.calls++
	if s.outcome == "unavailable" {
		return 0, errors.New("recovery database unavailable")
	}
	if s.outcome == "repaired" {
		for i := range s.states {
			s.states[i].Status = "failed"
		}
		return 1, nil
	}
	return 0, nil
}

func TestPatchWaveAdvanceReconcilesOfflineUnlinkedIntent(t *testing.T) {
	for _, outcome := range []string{"repaired", "fresh", "unavailable"} {
		t.Run(outcome, func(t *testing.T) {
			tenantID, nodeID, deploymentID := uuid.New(), uuid.New(), uuid.New()
			base := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID), allowed: true}
			base.nodes[0].State = "offline"
			p := patchOperatorPrincipal()
			base.users = map[string]*storage.User{p.Subject: {ID: uuid.MustParse(p.Subject), ExternalID: p.Subject}}
			base.deployments[deploymentID] = &storage.PatchDeployment{ID: deploymentID, TenantID: tenantID, TargetNodeCount: 1}
			_, err := base.CreateNodePatchState(context.Background(), storage.NodePatchState{TenantID: tenantID, NodeID: nodeID, DeploymentID: deploymentID})
			require.NoError(t, err)
			store := &offlineDispatchRecoveryStore{waveLockStore: &waveLockStore{patchSecurityStore: base, acquired: true}, outcome: outcome}
			srv := &Server{store: store, logger: zap.NewNop()}
			rec := httptest.NewRecorder()
			srv.handleAdvancePatchDeployment(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments/"+deploymentID.String()+"/advance", nil), p), deploymentID)
			status := http.StatusConflict
			if outcome == "unavailable" {
				status = http.StatusServiceUnavailable
			}
			require.Equal(t, status, rec.Code, rec.Body.String())
			require.Equal(t, 1, store.calls)
			require.True(t, store.released)
			require.Empty(t, base.jobs)
			if outcome == "repaired" {
				require.Equal(t, "failed", base.states[0].Status)
			} else {
				require.Equal(t, "pending", base.states[0].Status)
			}
		})
	}
}

func (s *waveLockStore) AcquirePatchDeploymentDispatch(context.Context, uuid.UUID) (func(), bool, error) {
	return func() { s.released = true }, s.acquired, s.err
}

func TestPatchWaveLockFailureAndBusyPreventDispatch(t *testing.T) {
	for _, scenario := range []string{"busy", "unavailable", "release"} {
		t.Run(scenario, func(t *testing.T) {
			tenantID, nodeID, deploymentID := uuid.New(), uuid.New(), uuid.New()
			base := &patchSecurityStore{patchTestStore: newPatchTestStore(tenantID, nodeID), allowed: true}
			p := patchOperatorPrincipal()
			base.users = map[string]*storage.User{p.Subject: {ID: uuid.MustParse(p.Subject), ExternalID: p.Subject}}
			base.deployments[deploymentID] = &storage.PatchDeployment{ID: deploymentID, TenantID: tenantID, TargetNodeCount: 1}
			store := &waveLockStore{patchSecurityStore: base}
			if scenario == "unavailable" {
				store.err = errors.New("database unavailable")
			}
			if scenario == "release" {
				store.acquired = true
			}
			srv := &Server{store: store, logger: zap.NewNop()}
			rec := httptest.NewRecorder()
			srv.handleAdvancePatchDeployment(rec, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/patch/deployments/"+deploymentID.String()+"/advance", nil), p), deploymentID)
			expected := http.StatusConflict
			if scenario == "unavailable" {
				expected = http.StatusServiceUnavailable
			}
			require.Equal(t, expected, rec.Code, rec.Body.String())
			require.Empty(t, base.jobs)
			require.Equal(t, scenario == "release", store.released)
		})
	}
}
