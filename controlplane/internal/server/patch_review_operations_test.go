package server

import (
	"bytes"
	"context"
	"errors"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"io"
	"net/http/httptest"
	"testing"
	"time"
)

type operationsTestStore struct {
	*governanceTestStore
	operationWrites, purges, checks int
	exportFailure                   bool
}

func (s *operationsTestStore) GetPatchReviewOperations(context.Context, uuid.UUID) (storage.PatchReviewOperations, error) {
	return storage.PatchReviewOperations{Policy: patchgovernance.DefaultOperations()}, nil
}
func (s *operationsTestStore) SavePatchReviewOperations(_ context.Context, _ uuid.UUID, revision int64, p patchgovernance.Operations, _ string) (storage.PatchReviewOperations, error) {
	s.operationWrites++
	return storage.PatchReviewOperations{Revision: revision + 1, Policy: p}, nil
}
func (s *operationsTestStore) ListPatchReviewRecipients(context.Context, uuid.UUID) ([]storage.PatchReviewRecipient, error) {
	return []storage.PatchReviewRecipient{}, nil
}
func (s *operationsTestStore) PreviewPatchReviewRetention(context.Context, uuid.UUID) (storage.PatchRetentionPreview, error) {
	return storage.PatchRetentionPreview{Blocked: "Legal hold"}, nil
}
func (s *operationsTestStore) PurgePatchReviewRetention(context.Context, uuid.UUID, int64, time.Time, string, uuid.UUID, string, string) (int, error) {
	s.purges++
	return 0, errors.New("legal hold")
}
func (s *operationsTestStore) DispatchPatchReviewEscalations(context.Context, uuid.UUID) (int, error) {
	s.checks++
	return 0, nil
}
func (s *operationsTestStore) ListEnabledPatchReviewTenants(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (s *operationsTestStore) GetPatchReviewOperationState(context.Context, uuid.UUID) (storage.PatchReviewOperationState, error) {
	return storage.PatchReviewOperationState{}, nil
}
func (s *operationsTestStore) RecordPatchReviewOperationError(context.Context, uuid.UUID) error {
	return nil
}
func (s *operationsTestStore) WritePatchReviewExport(_ context.Context, _ uuid.UUID, w io.Writer, _ *storage.PatchReviewArchiveInput) error {
	if s.exportFailure {
		_, _ = io.WriteString(w, `{"secret_partial":"not a completed archive"`)
		return errors.New("query failed")
	}
	_, err := io.WriteString(w, `{"complete":true}`)
	return err
}

func TestReviewOperationsRolesAndActivationGate(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	principal := &auth.Principal{Type: "user", Subject: "ops-fixture", Roles: []string{roleViewer, roleOperator, roleAdmin}}
	base.users = map[string]*storage.User{principal.Subject: {ID: uuid.New(), ExternalID: principal.Subject}}
	store := &operationsTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleOperator}}
	srv := newPatchTestServer(store)
	call := func(method, action, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/patch/review-operations?tenant_id="+tenant.String()+action, bytes.NewBufferString(body))
		srv.handlePatchReviewOperations(rec, withPrincipal(req, principal))
		return rec
	}
	require.Equal(t, 403, call("PUT", "", `{}`).Code)
	require.Equal(t, 403, call("POST", "&action=retention_purge", `{}`).Code)
	require.Zero(t, store.purges)
	store.requireRole = roleAdmin
	require.Equal(t, 400, call("POST", "&action=retention_purge", `{"verified_archive":false}`).Code)
	require.Zero(t, store.purges)
	require.Equal(t, 400, call("PUT", "", `{"expected_revision":0,"policy":{"owner":"Lab","reason":"Draft","retention":{"mode":"retain","days":90},"notifications":{"channel":"in_app","repeat_hours":24},"automation":{"mode":"automatic","enabled":true}}}`).Code)
	require.Zero(t, store.operationWrites)
	require.Equal(t, 200, call("POST", "&action=escalation_check", `{}`).Code)
	require.Equal(t, 1, store.checks)
	principal.Type = "agent"
	require.Equal(t, 403, call("GET", "", ``).Code)
	principal.Type = "user"
	store.allowed = false
	require.Equal(t, 403, call("GET", "", ``).Code)
	require.Empty(t, store.jobs)
}
func TestReviewExportFailureNeverDeliversPartialFile(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	principal := &auth.Principal{Type: "user", Subject: "archive-reader", Roles: []string{roleViewer}}
	base.users = map[string]*storage.User{principal.Subject: {ID: uuid.New(), ExternalID: principal.Subject}}
	store := &operationsTestStore{governanceTestStore: &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleViewer}, exportFailure: true}
	srv := newPatchTestServer(store)
	rec := httptest.NewRecorder()
	srv.handlePatchGovernance(rec, withPrincipal(httptest.NewRequest("GET", "/api/v1/patch/reviews?tenant_id="+tenant.String()+"&action=export", nil), principal))
	require.Equal(t, 503, rec.Code)
	require.Empty(t, rec.Header().Get("Content-Disposition"))
	require.NotContains(t, rec.Body.String(), "secret_partial")
}
