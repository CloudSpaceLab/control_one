package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type postureTenantAccessStore struct {
	*fakeStore
	allowed       bool
	checkedTenant uuid.UUID
}

func (s *postureTenantAccessStore) UserHasTenantRole(_ context.Context, _ uuid.UUID, tenantID uuid.UUID, _ []string) (bool, error) {
	s.checkedTenant = tenantID
	return s.allowed, nil
}

func TestComplianceControlPostureRequiresTenantAccess(t *testing.T) {
	tenantID := uuid.New()
	store := &postureTenantAccessStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		allowed:   false,
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "posture-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/control-posture?tenant_id="+tenantID.String()+"&framework=SOC2", nil)
	req.Header.Set("Authorization", "Bearer posture-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected tenant access denial, got %d body=%s", rec.Code, rec.Body.String())
	}
	if store.checkedTenant != tenantID {
		t.Fatalf("tenant access gate was not called with requested tenant: got %s want %s", store.checkedTenant, tenantID)
	}
}
