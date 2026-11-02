package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type governanceTestStore struct {
	*patchSecurityStore
	policy      *storage.PatchGovernancePolicy
	writes      int
	requireRole string
	findings    []storage.VulnerabilityFinding
}

func (s *governanceTestStore) ListTenantPatchReviewFindings(context.Context, uuid.UUID) ([]storage.VulnerabilityFinding, int, error) {
	return s.findings, len(s.findings), nil
}
func (s *governanceTestStore) GetPatchReviewPage(context.Context, uuid.UUID, uuid.UUID, int) (storage.PatchReviewPage, error) {
	return storage.PatchReviewPage{Findings: s.findings, Total: len(s.findings), Tracking: []storage.PatchGovernanceTracking{}}, nil
}
func (s *governanceTestStore) GetPatchReviewEventPage(context.Context, uuid.UUID, int, int64, int) ([]storage.PatchGovernanceEvent, int64, error) {
	return []storage.PatchGovernanceEvent{}, 0, nil
}
func (s *governanceTestStore) WritePatchReviewExport(_ context.Context, _ uuid.UUID, w io.Writer, _ *storage.PatchReviewArchiveInput) error {
	_, err := io.WriteString(w, `{"complete":true,"findings":[]}`)
	return err
}
func (s *governanceTestStore) RecordPatchReviewArchive(context.Context, uuid.UUID, string, string, storage.PatchReviewArchiveInput) (uuid.UUID, error) {
	return uuid.New(), nil
}

func (s *governanceTestStore) UserHasTenantRole(_ context.Context, _, _ uuid.UUID, roles []string) (bool, error) {
	if !s.allowed {
		return false, nil
	}
	for _, r := range roles {
		if r == s.requireRole {
			return true, nil
		}
	}
	return false, nil
}
func (s *governanceTestStore) GetPatchGovernancePolicy(context.Context, uuid.UUID) (*storage.PatchGovernancePolicy, error) {
	return s.policy, nil
}
func (s *governanceTestStore) SavePatchGovernancePolicy(_ context.Context, _ uuid.UUID, expected int64, p patchgovernance.Policy, actor string) (*storage.PatchGovernancePolicy, error) {
	s.writes++
	return &storage.PatchGovernancePolicy{Revision: expected + 1, Policy: p, UpdatedBy: actor}, nil
}
func (s *governanceTestStore) SavePatchGovernanceTracking(_ context.Context, _, finding uuid.UUID, expected int64, p patchgovernance.Tracking, actor string) (*storage.PatchGovernanceTracking, error) {
	s.writes++
	return &storage.PatchGovernanceTracking{FindingID: finding, Revision: expected + 1, Tracking: p, UpdatedBy: actor}, nil
}
func (s *governanceTestStore) ListPatchGovernanceTracking(context.Context, uuid.UUID) ([]storage.PatchGovernanceTracking, error) {
	return nil, nil
}
func (s *governanceTestStore) ListPatchGovernanceEvents(context.Context, uuid.UUID, int) ([]storage.PatchGovernanceEvent, bool, error) {
	return nil, false, nil
}

func TestPatchGovernanceAuthorizationAndNoDispatch(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	userID := uuid.New()
	principal := &auth.Principal{Type: "user", Subject: "review-user", Roles: []string{roleViewer, roleOperator, roleAdmin}}
	base.users = map[string]*storage.User{principal.Subject: {ID: userID, ExternalID: principal.Subject}}
	store := &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleOperator}
	srv := newPatchTestServer(store)
	p := patchgovernance.Policy{Name: "Test", Owner: "Lab operations", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "Critical", Severity: "critical", Exposure: "any", Importance: "any", Priority: "high", DueHours: 72}}}
	body, _ := json.Marshal(map[string]any{"expected_revision": 0, "policy": p})
	call := func(method, action string, body []byte) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/v1/patch/reviews?tenant_id="+tenant.String()+action, bytes.NewReader(body))
		srv.handlePatchGovernance(rec, withPrincipal(req, principal))
		return rec.Code
	}
	require.Equal(t, 403, call("PUT", "", body))
	require.Zero(t, store.writes)
	store.requireRole = roleAdmin
	require.Equal(t, 200, call("PUT", "", body))
	require.Equal(t, 1, store.writes)
	p.Mode = "automatic"
	body, _ = json.Marshal(map[string]any{"expected_revision": 0, "policy": p})
	require.Equal(t, 400, call("PUT", "", body))
	require.Equal(t, 1, store.writes)
	until := time.Now().Add(time.Hour)
	review := patchgovernance.Tracking{Owner: "Lab", EscalateTo: "Review queue", Exposure: "unknown", Importance: "unknown", Compatibility: "unknown", Reason: "Time-bound review", ExceptionUntil: &until}
	body, _ = json.Marshal(map[string]any{"finding_id": uuid.New(), "expected_revision": 0, "tracking": review})
	store.requireRole = roleOperator
	require.Equal(t, 403, call("POST", "&action=tracking", body))
	require.Equal(t, 1, store.writes)
	store.allowed = false
	require.Equal(t, 403, call("GET", "", nil))
	require.Empty(t, store.jobs)
	principal.Type = "agent"
	require.Equal(t, 403, call("GET", "", nil))
}
func TestPatchGovernanceRejectsTrailingJSON(t *testing.T) {
	for _, body := range []string{`{} {}`, `{} garbage`} {
		rec := httptest.NewRecorder()
		var out map[string]any
		require.False(t, decodeGovernance(rec, httptest.NewRequest("POST", "/", bytes.NewBufferString(body)), &out))
		require.Equal(t, 400, rec.Code)
	}
}

func TestPatchGovernanceWithholdsPriorityWithoutAssessmentAndExportsNoJobs(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, node)
	userID := uuid.New()
	principal := &auth.Principal{Type: "user", Subject: "review-reader", Roles: []string{roleViewer}}
	base.users = map[string]*storage.User{principal.Subject: {ID: userID, ExternalID: principal.Subject}}
	p := patchgovernance.Policy{Name: "Test", Owner: "Lab", Mode: "recommend_only", RetentionDays: 30, Rules: []patchgovernance.Rule{{Name: "Critical", Severity: "critical", Exposure: "any", Importance: "any", Priority: "urgent", DueHours: 24}}}
	store := &governanceTestStore{patchSecurityStore: &patchSecurityStore{patchTestStore: base, allowed: true}, requireRole: roleViewer, policy: &storage.PatchGovernancePolicy{Revision: 1, Policy: p}, findings: []storage.VulnerabilityFinding{{ID: uuid.New(), TenantID: tenant, NodeID: node, PackageName: "liba", Severity: "critical", KEV: true, FirstSeenAt: time.Now().Add(-time.Hour), LastSeenAt: time.Now()}}}
	srv := newPatchTestServer(store)
	for _, action := range []string{"", "&action=export"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/v1/patch/reviews?tenant_id="+tenant.String()+action, nil)
		srv.handlePatchGovernance(rec, withPrincipal(req, principal))
		require.Equal(t, 200, rec.Code, rec.Body.String())
		if action != "" {
			require.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
			require.Contains(t, rec.Body.String(), `"complete":true`)
			require.NotEmpty(t, rec.Header().Get("X-Patch-Archive-ID"))
			continue
		}
		var response struct {
			Findings []patchGovernanceFinding `json:"findings"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
		require.Len(t, response.Findings, 1)
		require.False(t, response.Findings[0].EvidenceCurrent)
		require.Equal(t, "review_required", response.Findings[0].Decision.Priority)
		require.False(t, response.Findings[0].Decision.InstallationAuthorized)
		if action != "" {
			require.Contains(t, rec.Header().Get("Content-Disposition"), "attachment")
		}
	}
	require.Zero(t, store.writes)
	require.Empty(t, store.jobs)
}
