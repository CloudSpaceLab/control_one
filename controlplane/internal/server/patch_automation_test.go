package server

import (
	"bytes"
	"context"
	"errors"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
	"time"
)

type automationTestStore struct {
	*operationsTestStore
	runs            []storage.PatchAutomationRun
	queues, cancels int
	readyErr        error
}

func (s *automationTestStore) ListPatchAutomationRuns(context.Context, uuid.UUID) ([]storage.PatchAutomationRun, error) {
	return s.runs, nil
}
func (s *automationTestStore) DuePatchAutomationRuns(context.Context) ([]storage.PatchAutomationRun, error) {
	return s.runs, nil
}
func (s *automationTestStore) QueuePatchAutomationRun(_ context.Context, tenant, approval, actor uuid.UUID, revision int64, at time.Time, authority string) (storage.PatchAutomationRun, error) {
	s.queues++
	return storage.PatchAutomationRun{ID: uuid.New(), TenantID: tenant, ApprovalID: approval, ActorID: actor, PolicyRevision: revision, Authority: authority, NotBefore: at}, nil
}
func (s *automationTestStore) CancelPatchAutomationRun(context.Context, uuid.UUID, uuid.UUID, string) error {
	s.cancels++
	return nil
}
func (s *automationTestStore) SetPatchAutomationBlocked(context.Context, uuid.UUID, string) error {
	return nil
}
func (s *automationTestStore) CheckPatchAutomationRun(context.Context, uuid.UUID, uuid.UUID) (storage.PatchAutomationRun, error) {
	return storage.PatchAutomationRun{}, errors.New("fixture health unknown")
}
func (s *automationTestStore) PatchAutomationReadiness(context.Context, uuid.UUID, uuid.UUID) error {
	return s.readyErr
}
func (s *automationTestStore) DiscoverPatchAutomationRuns(context.Context) error { return nil }
func TestAutomationAPIAuthorityAndReadiness(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	principal := &auth.Principal{Type: "user", Subject: "automation-fixture", Roles: []string{roleViewer, roleOperator, roleAdmin}}
	base.users = map[string]*storage.User{principal.Subject: {ID: uuid.New(), ExternalID: principal.Subject}}
	st := &automationTestStore{operationsTestStore: &operationsTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleViewer}}, readyErr: errors.New("current complete service and reboot evidence required")}
	srv := newPatchTestServer(st)
	call := func(method, action, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1/patch/automation-runs?tenant_id="+tenant.String()+action, bytes.NewBufferString(body))
		w := httptest.NewRecorder()
		srv.handlePatchAutomation(w, withPrincipal(r, principal))
		return w
	}
	require.Equal(t, 200, call("GET", "", "").Code)
	require.Equal(t, 403, call("POST", "&action=schedule", `{}`).Code)
	require.Zero(t, st.queues)
	st.requireRole = roleAdmin
	result := call("POST", "&action=readiness", `{"node_id":"`+node.String()+`"}`)
	require.Equal(t, 200, result.Code)
	require.Contains(t, result.Body.String(), `"ready":false`)
	require.Contains(t, result.Body.String(), `"installation_authorized":false`)
	require.Equal(t, 404, call("POST", "&action=readiness", `{"node_id":"`+uuid.NewString()+`"}`).Code)
	require.Equal(t, 201, call("POST", "&action=schedule", `{"approval_id":"`+uuid.NewString()+`","revision":7,"not_before":"2026-11-03T12:00:00Z"}`).Code)
	require.Equal(t, 1, st.queues)
	principal.Type = "agent"
	require.Equal(t, 403, call("GET", "", "").Code)
	principal.Type = "user"
	st.allowed = false
	require.Equal(t, 403, call("POST", "&action=cancel", `{}`).Code)
	require.Zero(t, st.cancels)
	require.Empty(t, st.jobs)
}
