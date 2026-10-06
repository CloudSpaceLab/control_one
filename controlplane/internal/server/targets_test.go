package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type targetTestStore struct {
	*fakeStore
	filter      storage.TargetFilter
	access      storage.TargetAccess
	targets     []storage.Target
	total       int
	createCalls int
	createErr   error
}

func (s *targetTestStore) ListTargets(_ context.Context, f storage.TargetFilter, a storage.TargetAccess) ([]storage.Target, int, error) {
	s.filter, s.access = f, a
	return s.targets, s.total, nil
}
func (s *targetTestStore) GetTarget(_ context.Context, id uuid.UUID, a storage.TargetAccess) (*storage.Target, error) {
	s.access = a
	for _, target := range s.targets {
		if target.ID == id {
			return &target, nil
		}
	}
	return nil, nil
}
func (s *targetTestStore) CreateNetworkTarget(_ context.Context, p storage.NetworkTargetParams, a storage.TargetAccess) (*storage.Target, error) {
	s.createCalls++
	s.access = a
	if s.createErr != nil {
		return nil, s.createErr
	}
	return &storage.Target{ID: uuid.New(), TenantID: p.TenantID, Family: "network_security", Type: p.Type, CollectionState: "discovered", ReachabilityState: "unknown"}, nil
}

func targetTestServer() (*Server, *targetTestStore, *auth.Principal) {
	userID := uuid.New()
	store := &targetTestStore{fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{userID: {ID: userID}}}}
	server := New(zap.NewNop(), &config.Config{}, store, nil)
	return server, store, &auth.Principal{Type: "user", Subject: userID.String(), Roles: []string{roleOperator}}
}

func TestTargetsAllTenantsAndExactPagination(t *testing.T) {
	s, store, principal := targetTestServer()
	store.targets = []storage.Target{{ID: uuid.New(), TenantID: uuid.New(), Family: "network_security"}}
	store.total = 1234 // The API must not derive a total from a capped page.
	req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/targets?family=network_security&site=branch&group=core&search=switch&limit=1&offset=20", nil), principal)
	rec := httptest.NewRecorder()
	s.baseRouter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, uuid.Nil, store.filter.TenantID)
	require.Equal(t, "network_security", store.filter.Family)
	require.Equal(t, "branch", store.filter.Site)
	require.Equal(t, "core", store.filter.Group)
	require.Equal(t, "switch", store.filter.Search)
	require.Equal(t, 20, store.filter.Offset)
	require.Equal(t, "targets.read", store.access.Permission)
	require.Equal(t, principal.Subject, store.access.UserID.String())
	var result paginatedResponse[storage.Target]
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.Equal(t, newPaginationMeta(1234, 1, 20, 1), result.Pagination)
	require.Len(t, result.Data, 1)
}

func TestNetworkTargetCreationRejectsSecretsAndVerifiedClaims(t *testing.T) {
	s, store, principal := targetTestServer()
	tenant := uuid.New()
	base := `{"tenant_id":"` + tenant.String() + `","type":"switch","display_name":"branch","management_addresses":["192.0.2.1"]`
	for _, field := range []string{`"password":"secret"`, `"private_key":"secret"`, `"snmp_auth_password":"secret"`, `"node_id":"` + uuid.NewString() + `"`, `"collection_state":"telemetry_ready"`, `"capabilities":["snmp"]`, `"classification":{"source":"snmp","confidence":100}`} {
		req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/targets", strings.NewReader(base+","+field+"}")), principal)
		rec := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
		require.NotContains(t, rec.Body.String(), "secret")
	}
	require.Zero(t, store.createCalls)
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/targets", strings.NewReader(base+"}")), principal)
	rec := httptest.NewRecorder()
	s.baseRouter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "targets.write", store.access.Permission)
	require.Contains(t, rec.Header().Get("Location"), "/api/v1/targets/")
	require.NotContains(t, rec.Body.String(), "node_id")
	require.Contains(t, rec.Body.String(), `"collection_state":"discovered"`)
}

func TestTargetAccessFailsClosed(t *testing.T) {
	s, store, principal := targetTestServer()
	for _, tc := range []struct {
		name      string
		principal *auth.Principal
		status    int
	}{
		{"unauthenticated", nil, http.StatusUnauthorized},
		{"agent", &auth.Principal{Type: "agent", Subject: principal.Subject, Roles: []string{roleOperator}}, http.StatusForbidden},
		{"unknown user", &auth.Principal{Type: "user", Subject: uuid.NewString(), Roles: []string{roleAdmin}}, http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/targets", nil)
			if tc.principal != nil {
				req = withPrincipal(req, tc.principal)
			}
			rec := httptest.NewRecorder()
			s.baseRouter.ServeHTTP(rec, req)
			require.Equal(t, tc.status, rec.Code)
		})
	}
	principal.Roles = []string{roleViewer}
	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/targets", strings.NewReader(`{}`)), principal)
	rec := httptest.NewRecorder()
	s.baseRouter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	principal.Roles = []string{roleOperator}
	store.createErr = sql.ErrNoRows
	req = withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/targets", strings.NewReader(`{"tenant_id":"`+uuid.NewString()+`","type":"router","display_name":"x","management_addresses":["192.0.2.1"]}`)), principal)
	rec = httptest.NewRecorder()
	s.baseRouter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	req = withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/targets/"+uuid.NewString(), nil), principal)
	rec = httptest.NewRecorder()
	s.baseRouter.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestTargetsInvalidScopeAndUnsupportedOperations(t *testing.T) {
	s, _, principal := targetTestServer()
	for _, query := range []string{"?tenant_id=all", "?tenant_id=" + uuid.Nil.String(), "?offset=-1", "?limit=0"} {
		req := withPrincipal(httptest.NewRequest(http.MethodGet, "/api/v1/targets"+query, nil), principal)
		rec := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(rec, req)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	for _, path := range []string{"/api/v1/targets", "/api/v1/targets/" + uuid.NewString()} {
		req := withPrincipal(httptest.NewRequest(http.MethodPatch, path, nil), principal)
		rec := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(rec, req)
		require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestNodeResponseHasStableTargetIdentity(t *testing.T) {
	node := storage.Node{ID: uuid.New(), TenantID: uuid.New(), Hostname: "existing-server"}
	response := nodeResponseFromModel(node)
	require.Equal(t, node.ID.String(), response.ID)
	require.Equal(t, response.ID, response.TargetID)
}
