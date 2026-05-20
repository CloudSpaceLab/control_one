package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestM9IPBehaviorReadEndpointsDoNotLeakCrossTenantData(t *testing.T) {
	t.Parallel()

	tenantA := uuid.New()
	tenantB := uuid.New()
	ip := "203.0.113.10"
	store := &tenantScopedIPBehaviorStore{
		fakeStore: &fakeStore{},
		tenantID:  tenantA,
		country: storage.IPBehaviorCountrySummary{
			CountryCode:     "NG",
			Country:         "Nigeria",
			UniqueSourceIPs: 2,
			RequestCount:    20,
			BytesOut:        4096,
			StatusCounts:    map[string]int64{"401": 12},
			FirstSeenAt:     time.Now().UTC().Add(-time.Hour),
			LastSeenAt:      time.Now().UTC(),
		},
		profile: storage.IPBehaviorIPProfile{
			SourceIP:     ip,
			Countries:    []string{"NG"},
			ASNs:         []string{"AS64500"},
			Apps:         []string{"core-api"},
			RequestCount: 20,
			StatusCounts: map[string]int64{"401": 12},
		},
		baseline: storage.IPBehaviorBaseline{
			ID:           uuid.New(),
			TenantID:     tenantA,
			Dimension:    "country_app",
			DimensionKey: "core-api|NG",
			Baseline:     map[string]any{"sample_count": 30},
			WindowDays:   30,
		},
	}
	s := &Server{store: store, logger: zap.NewNop()}

	allowed := []struct {
		name    string
		path    string
		handler http.HandlerFunc
		want    string
	}{
		{"overview", "/api/v1/ip-behavior/overview?tenant_id=" + tenantA.String(), s.handleIPBehaviorOverview, "Nigeria"},
		{"countries", "/api/v1/ip-behavior/countries?tenant_id=" + tenantA.String(), s.handleIPBehaviorCountries, "Nigeria"},
		{"country detail", "/api/v1/ip-behavior/countries/NG?tenant_id=" + tenantA.String(), s.handleIPBehaviorCountryDetail, "Nigeria"},
		{"ip profile", "/api/v1/ip-behavior/ips/" + ip + "?tenant_id=" + tenantA.String(), s.handleIPBehaviorIPProfile, "core-api"},
		{"baselines", "/api/v1/ip-behavior/baselines?tenant_id=" + tenantA.String(), s.handleIPBehaviorBaselines, "core-api|NG"},
	}
	for _, tc := range allowed {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req = withPrincipal(req, viewerPrincipal())
		rr := httptest.NewRecorder()
		tc.handler(rr, req)
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), tc.want) {
			t.Fatalf("%s allowed status=%d body=%s, want OK containing %q", tc.name, rr.Code, rr.Body.String(), tc.want)
		}
	}

	blocked := []struct {
		name       string
		path       string
		handler    http.HandlerFunc
		wantStatus int
	}{
		{"overview", "/api/v1/ip-behavior/overview?tenant_id=" + tenantB.String(), s.handleIPBehaviorOverview, http.StatusOK},
		{"countries", "/api/v1/ip-behavior/countries?tenant_id=" + tenantB.String(), s.handleIPBehaviorCountries, http.StatusOK},
		{"country detail", "/api/v1/ip-behavior/countries/NG?tenant_id=" + tenantB.String(), s.handleIPBehaviorCountryDetail, http.StatusNotFound},
		{"ip profile", "/api/v1/ip-behavior/ips/" + ip + "?tenant_id=" + tenantB.String(), s.handleIPBehaviorIPProfile, http.StatusOK},
		{"baselines", "/api/v1/ip-behavior/baselines?tenant_id=" + tenantB.String(), s.handleIPBehaviorBaselines, http.StatusOK},
	}
	for _, tc := range blocked {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req = withPrincipal(req, viewerPrincipal())
		rr := httptest.NewRecorder()
		tc.handler(rr, req)
		if rr.Code != tc.wantStatus {
			t.Fatalf("%s cross-tenant status=%d body=%s, want %d", tc.name, rr.Code, rr.Body.String(), tc.wantStatus)
		}
		leakedIP := tc.name != "ip profile" && strings.Contains(rr.Body.String(), ip)
		if strings.Contains(rr.Body.String(), "Nigeria") || strings.Contains(rr.Body.String(), "core-api|NG") || leakedIP {
			t.Fatalf("%s leaked tenant A data to tenant B: %s", tc.name, rr.Body.String())
		}
	}
}

func TestM9WebserverConfigActionRejectsCrossTenantInstance(t *testing.T) {
	t.Parallel()

	tenantA := uuid.New()
	tenantB := uuid.New()
	nodeA := uuid.New()
	nodeB := uuid.New()
	instanceID := uuid.New()
	store := &webserverAPIStore{
		fakeStore: &fakeStore{},
		instances: []storage.WebserverInstance{{
			ID:          instanceID,
			TenantID:    tenantA,
			NodeID:      nodeA,
			Kind:        "nginx",
			Version:     "1.26",
			ServiceName: "nginx",
			ObservedAt:  time.Now().UTC(),
		}},
	}
	s := &Server{store: store, logger: zap.NewNop()}
	body := bytes.NewBufferString(`{"tenant_id":"` + tenantB.String() + `","node_id":"` + nodeB.String() + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webservers/"+instanceID.String()+"/config/plan", body)
	req = withPrincipal(req, operatorPrincipal())
	rr := httptest.NewRecorder()
	s.handleWebserverSubroutes(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("cross-tenant webserver plan status=%d body=%s, want 400", rr.Code, rr.Body.String())
	}
}

func TestM9ViewerCannotMutateBlockProposalsOrOfflineBundles(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	blockStore := &blockProposalInvestigateStore{fakeStore: &fakeStore{}}
	s := &Server{store: blockStore, logger: zap.NewNop()}
	body := bytes.NewBufferString(`{"tenant_id":"` + tenantID.String() + `","ip_cidr":"203.0.113.10","scope":"tenant","target_type":"tenant","enforcement":"firewall","reason":"viewer should not mutate"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals", body)
	req = withPrincipal(req, viewerPrincipal())
	rr := httptest.NewRecorder()
	s.handleBlockProposals(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer block proposal status=%d body=%s, want 403", rr.Code, rr.Body.String())
	}
	if len(blockStore.createdBlocks) != 0 {
		t.Fatalf("viewer created block proposal: %#v", blockStore.createdBlocks)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/offline-bundles?tenant_id="+tenantID.String(), bytes.NewReader([]byte("not reached")))
	req = withPrincipal(req, viewerPrincipal())
	rr = httptest.NewRecorder()
	s.handleOfflineBundles(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("viewer offline bundle import status=%d body=%s, want 403", rr.Code, rr.Body.String())
	}
}

func TestM9TenantAccessGateStopsScopedHandlersBeforeDataAccess(t *testing.T) {
	t.Parallel()

	userID := uuid.New()
	tenantID := uuid.New()
	store := &workerFTenantGateStore{
		fakeStore: fakeStore{users: map[string]*storage.User{
			"tenant-user": {ID: userID, ExternalID: "tenant-user"},
		}},
		allowed: false,
		webhook: storage.Webhook{
			ID:       uuid.New(),
			TenantID: uuid.NullUUID{UUID: tenantID, Valid: true},
		},
		blockEntry: storage.IPBlocklistEntry{
			ID:       uuid.New(),
			TenantID: tenantID,
			IPCIDR:   "203.0.113.10/32",
			Status:   "proposed",
		},
	}
	s := &Server{store: store, logger: zap.NewNop()}
	principal := &auth.Principal{Type: "user", Subject: "tenant-user", Roles: []string{roleAdmin, roleOperator, roleViewer}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ip-behavior/overview?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, principal)
	rr := httptest.NewRecorder()
	s.handleIPBehaviorOverview(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("ip behavior status=%d body=%s, want 403", rr.Code, rr.Body.String())
	}
	if store.ipBehaviorCountryCalls != 0 {
		t.Fatalf("ip behavior query ran %d time(s), want 0", store.ipBehaviorCountryCalls)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/v1/webhooks/"+store.webhook.ID.String(), strings.NewReader(`{"name":"new-name"}`))
	req = withPrincipal(req, principal)
	rr = httptest.NewRecorder()
	s.handleWebhookResource(rr, req, store.webhook.ID)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("webhook update status=%d body=%s, want 403", rr.Code, rr.Body.String())
	}
	if store.webhookUpdateCalls != 0 {
		t.Fatalf("webhook update ran %d time(s), want 0", store.webhookUpdateCalls)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/network/block-proposals/"+store.blockEntry.ID.String()+"/approve", nil)
	req = withPrincipal(req, principal)
	rr = httptest.NewRecorder()
	s.handleBlockProposalSubroutes(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("block approval status=%d body=%s, want 403", rr.Code, rr.Body.String())
	}
	if store.blockStatusUpdates != 0 {
		t.Fatalf("block status update ran %d time(s), want 0", store.blockStatusUpdates)
	}
}

type tenantScopedIPBehaviorStore struct {
	*fakeStore
	tenantID uuid.UUID
	country  storage.IPBehaviorCountrySummary
	profile  storage.IPBehaviorIPProfile
	baseline storage.IPBehaviorBaseline
}

func (f *tenantScopedIPBehaviorStore) ListIPBehaviorCountries(_ context.Context, tenantID uuid.UUID, _ time.Time, code string) ([]storage.IPBehaviorCountrySummary, error) {
	if tenantID != f.tenantID {
		return nil, nil
	}
	if strings.TrimSpace(code) != "" && !strings.EqualFold(code, f.country.CountryCode) {
		return nil, nil
	}
	return []storage.IPBehaviorCountrySummary{f.country}, nil
}

func (f *tenantScopedIPBehaviorStore) GetIPBehaviorIPProfile(_ context.Context, tenantID uuid.UUID, ip string, _ time.Time) (*storage.IPBehaviorIPProfile, error) {
	if tenantID != f.tenantID || ip != f.profile.SourceIP {
		return nil, nil
	}
	return &f.profile, nil
}

func (f *tenantScopedIPBehaviorStore) ListIPBehaviorBaselines(_ context.Context, tenantID uuid.UUID, _ string, _, _ int) ([]storage.IPBehaviorBaseline, int, error) {
	if tenantID != f.tenantID {
		return nil, 0, nil
	}
	return []storage.IPBehaviorBaseline{f.baseline}, 1, nil
}

type workerFTenantGateStore struct {
	fakeStore
	allowed                bool
	ipBehaviorCountryCalls int
	webhook                storage.Webhook
	webhookUpdateCalls     int
	blockEntry             storage.IPBlocklistEntry
	blockStatusUpdates     int
}

func (f *workerFTenantGateStore) UserHasTenantRole(context.Context, uuid.UUID, uuid.UUID, []string) (bool, error) {
	return f.allowed, nil
}

func (f *workerFTenantGateStore) ListIPBehaviorCountries(context.Context, uuid.UUID, time.Time, string) ([]storage.IPBehaviorCountrySummary, error) {
	f.ipBehaviorCountryCalls++
	return []storage.IPBehaviorCountrySummary{{CountryCode: "NG", Country: "Nigeria"}}, nil
}

func (f *workerFTenantGateStore) GetWebhook(_ context.Context, id uuid.UUID) (*storage.Webhook, error) {
	if id != f.webhook.ID {
		return nil, nil
	}
	webhook := f.webhook
	return &webhook, nil
}

func (f *workerFTenantGateStore) UpdateWebhook(_ context.Context, id uuid.UUID, params storage.UpdateWebhookParams) (*storage.Webhook, error) {
	f.webhookUpdateCalls++
	if id != f.webhook.ID {
		return nil, nil
	}
	if params.Name != nil {
		f.webhook.Name = *params.Name
	}
	webhook := f.webhook
	return &webhook, nil
}

func (f *workerFTenantGateStore) CreateIPBlocklistEntry(context.Context, storage.CreateIPBlocklistEntryParams) (*storage.IPBlocklistEntry, error) {
	return &f.blockEntry, nil
}

func (f *workerFTenantGateStore) GetIPBlocklistEntry(_ context.Context, id uuid.UUID) (*storage.IPBlocklistEntry, error) {
	if id != f.blockEntry.ID {
		return nil, nil
	}
	entry := f.blockEntry
	return &entry, nil
}

func (f *workerFTenantGateStore) SetIPBlocklistEntryEntityAction(_ context.Context, _ uuid.UUID, entityActionID uuid.UUID) (*storage.IPBlocklistEntry, error) {
	f.blockEntry.EntityActionID = uuid.NullUUID{UUID: entityActionID, Valid: true}
	return &f.blockEntry, nil
}

func (f *workerFTenantGateStore) UpdateIPBlocklistEntryStatus(_ context.Context, _ uuid.UUID, status string, _ *uuid.UUID, _ string) (*storage.IPBlocklistEntry, error) {
	f.blockStatusUpdates++
	f.blockEntry.Status = status
	return &f.blockEntry, nil
}
