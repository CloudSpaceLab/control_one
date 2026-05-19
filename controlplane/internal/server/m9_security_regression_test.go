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
