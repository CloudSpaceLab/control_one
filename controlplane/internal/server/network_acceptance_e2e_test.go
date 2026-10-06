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
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// networkAcceptanceStore backs the public HTTP handlers with a small in-memory
// store so this test can exercise the complete network-device API lifecycle
// without a database, Docker, a collector token, or physical network hardware.
type networkAcceptanceStore struct {
	*fakeStore
	tenantID    uuid.UUID
	otherTenant uuid.UUID
	target      *storage.Target
	credential  *storage.ProviderCredential
	receipt     *storage.NetworkConnectionTest
	connection  *storage.NetworkInventoryConnection
	inventory   *storage.NetworkInventory
	sources     []storage.NetworkSource
}

func (s *networkAcceptanceStore) CreateNetworkCredential(_ context.Context, p storage.CreateProviderCredentialParams, _ storage.TargetAccess) (*storage.ProviderCredential, error) {
	s.credential = &storage.ProviderCredential{ID: uuid.New(), TenantID: p.TenantID, Name: p.Name, Provider: p.Provider, ConfigEncrypted: p.ConfigEncrypted, Nonce: p.Nonce}
	return s.credential, nil
}

func (s *networkAcceptanceStore) GetNetworkCredential(_ context.Context, id, tenant uuid.UUID, _ storage.TargetAccess) (*storage.ProviderCredential, error) {
	if s.credential == nil || id != s.credential.ID || tenant != s.credential.TenantID {
		return nil, nil
	}
	return s.credential, nil
}

func (s *networkAcceptanceStore) BeginNetworkConnectionTest(_ context.Context, p storage.NetworkConnectionTest, _ storage.TargetAccess) (*storage.NetworkConnectionTest, error) {
	p.ID = uuid.New()
	p.State = "testing"
	s.receipt = &p
	return s.receipt, nil
}

func (s *networkAcceptanceStore) FinishNetworkConnectionTest(_ context.Context, _ uuid.UUID, result networkdevice.Result) (*storage.NetworkConnectionTest, error) {
	s.receipt.State = result.State
	s.receipt.Result = result
	now := time.Now().UTC()
	s.receipt.CompletedAt = &now
	return s.receipt, nil
}

func (s *networkAcceptanceStore) SaveNetworkOnboarding(_ context.Context, p storage.NetworkSaveParams, _ storage.TargetAccess) (*storage.Target, error) {
	if s.receipt == nil || p.TestID != s.receipt.ID || s.receipt.State != "authenticated" {
		return nil, context.Canceled
	}
	s.target = &storage.Target{
		ID: uuid.New(), TenantID: s.tenantID, Family: "network_security", Type: p.Type,
		DisplayName: p.DisplayName, Vendor: s.receipt.Result.Vendor, Model: s.receipt.Result.Model,
		Platform: s.receipt.Result.Platform, ReachabilityState: "reachable", CollectionState: "authenticated",
		Classification: storage.TargetClassification{Source: "snmpv3", Confidence: s.receipt.Result.Confidence, Evidence: s.receipt.Result.Evidence},
	}
	return s.target, nil
}

func (s *networkAcceptanceStore) ListTargets(_ context.Context, filter storage.TargetFilter, _ storage.TargetAccess) ([]storage.Target, int, error) {
	if filter.TenantID != uuid.Nil && filter.TenantID != s.tenantID {
		return []storage.Target{}, 0, nil
	}
	if s.target == nil {
		return []storage.Target{}, 0, nil
	}
	return []storage.Target{*s.target}, 1, nil
}

func (s *networkAcceptanceStore) GetTarget(_ context.Context, id uuid.UUID, _ storage.TargetAccess) (*storage.Target, error) {
	if s.target == nil || id != s.target.ID {
		return nil, nil
	}
	return s.target, nil
}

func (s *networkAcceptanceStore) CreateNetworkTarget(_ context.Context, p storage.NetworkTargetParams, _ storage.TargetAccess) (*storage.Target, error) {
	target := &storage.Target{ID: uuid.New(), TenantID: p.TenantID, Family: "network_security", Type: p.Type, DisplayName: p.DisplayName, CollectionState: "discovered", ReachabilityState: "unknown"}
	s.target = target
	return target, nil
}

func (s *networkAcceptanceStore) GetNetworkInventory(_ context.Context, _ uuid.UUID, _ storage.TargetAccess) (*storage.NetworkInventory, error) {
	if s.inventory == nil {
		return &storage.NetworkInventory{TargetID: s.target.ID, TenantID: s.tenantID, State: "not_collected"}, nil
	}
	return s.inventory, nil
}

func (s *networkAcceptanceStore) GetNetworkInventoryConnection(_ context.Context, id uuid.UUID, _ storage.TargetAccess) (*storage.NetworkInventoryConnection, error) {
	if s.connection == nil || id != s.connection.TargetID {
		return nil, nil
	}
	return s.connection, nil
}

func (s *networkAcceptanceStore) BeginNetworkInventory(_ context.Context, id uuid.UUID, _ storage.TargetAccess) (*storage.NetworkInventory, error) {
	s.inventory = &storage.NetworkInventory{TargetID: id, TenantID: s.tenantID, RefreshID: uuid.New(), State: "collecting"}
	return s.inventory, nil
}

func (s *networkAcceptanceStore) FinishNetworkInventory(_ context.Context, _, _ uuid.UUID, result networkdevice.Inventory) (*storage.NetworkInventory, error) {
	s.inventory.State = result.State
	s.inventory.Snapshot = &result
	now := time.Now().UTC()
	s.inventory.CompletedAt = &now
	return s.inventory, nil
}

func (s *networkAcceptanceStore) ListNetworkSources(context.Context, uuid.UUID, storage.TargetAccess) ([]storage.NetworkSource, error) {
	return s.sources, nil
}

func (s *networkAcceptanceStore) ConfigureNetworkSource(context.Context, uuid.UUID, storage.NetworkSourceConfig, storage.TargetAccess) error {
	return nil
}

func (s *networkAcceptanceStore) ReportNetworkSources(context.Context, uuid.UUID, string, []networkdevice.SourceReport) error {
	return nil
}

func (s *networkAcceptanceStore) ResolveNetworkSyslogSource(context.Context, uuid.UUID, string, string) (*storage.NetworkSource, error) {
	if len(s.sources) == 0 {
		return nil, nil
	}
	return &s.sources[0], nil
}

func TestNetworkDeviceAcceptanceLifecycleHTTP(t *testing.T) {
	userID, tenantID, otherTenantID := uuid.New(), uuid.New(), uuid.New()
	store := &networkAcceptanceStore{
		fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{userID: {ID: userID}}},
		tenantID:  tenantID, otherTenant: otherTenantID,
	}
	s := New(zap.NewNop(), &config.Config{NetworkOnboarding: config.NetworkOnboardingConfig{AllowedCIDRs: []string{"192.0.2.0/24"}}}, store, nil)
	s.sealer, _ = secretbox.NewSealer(bytes.Repeat([]byte{7}, 32))
	s.auditAsync = false
	s.networkProbe = func(_ context.Context, _ string, _ int, _ string, _ networkdevice.Credential) networkdevice.Result {
		return networkdevice.Result{
			State: "authenticated", SuggestedType: "switch", Vendor: "Cisco", Model: "Catalyst 9300",
			Platform: "IOS XE 17.9", Confidence: 96, Evidence: []string{"sysObjectID matched Cisco Catalyst"},
			Capabilities: []string{"snmpv3_identity"},
		}
	}
	s.networkInventory = func(context.Context, string, int, string, networkdevice.Credential) networkdevice.Inventory {
		return networkdevice.Inventory{State: "inventory_ready", Facts: map[string]networkdevice.Fact{
			"hostname":        {Value: "edge-sw-01", Protocol: "snmpv3", Source: "sysName.0"},
			"interface_count": {Value: 48, Protocol: "snmpv3", Source: "ifTable"},
		}}
	}
	principal := &auth.Principal{Type: "user", Subject: userID.String(), Roles: []string{roleOperator}}
	request := func(method, path, body string, actor *auth.Principal) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if actor != nil {
			req = withPrincipal(req, actor)
		}
		rec := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(rec, req)
		return rec
	}

	// Store SNMPv3 authPriv material through the credential-reference endpoint.
	credentialBody := `{"tenant_id":"` + tenantID.String() + `","protocol":"snmpv3","name":"branch read-only","config":{"username":"readonly","auth_protocol":"SHA256","auth_secret":"fixture-auth","priv_protocol":"AES","priv_secret":"fixture-priv"}}`
	credentialResponse := request(http.MethodPost, "/api/v1/network-onboarding/credentials", credentialBody, principal)
	require.Equal(t, http.StatusCreated, credentialResponse.Code, credentialResponse.Body.String())
	require.NotContains(t, credentialResponse.Body.String(), "fixture-auth")
	require.NotEmpty(t, store.credential.ConfigEncrypted)

	// Failed authentication stays an explicit failed receipt and cannot be saved.
	s.networkProbe = func(context.Context, string, int, string, networkdevice.Credential) networkdevice.Result {
		return networkdevice.Outcome("auth_failed")
	}
	testBody := `{"tenant_id":"` + tenantID.String() + `","credential_id":"` + store.credential.ID.String() + `","address":"192.0.2.10","port":161}`
	failed := request(http.MethodPost, "/api/v1/network-onboarding/tests", testBody, principal)
	require.Equal(t, http.StatusOK, failed.Code, failed.Body.String())
	require.Contains(t, failed.Body.String(), `"state":"auth_failed"`)
	var failedReceipt storage.NetworkConnectionTest
	require.NoError(t, json.Unmarshal(failed.Body.Bytes(), &failedReceipt))
	saveFailed, _ := json.Marshal(storage.NetworkSaveParams{TestID: failedReceipt.ID, DisplayName: "Edge switch", Type: "switch"})
	require.Equal(t, http.StatusConflict, request(http.MethodPost, "/api/v1/network-onboarding/save", string(saveFailed), principal).Code)

	// A successful read-only test classifies the appliance and permits onboarding.
	s.networkProbe = func(context.Context, string, int, string, networkdevice.Credential) networkdevice.Result {
		return networkdevice.Result{State: "authenticated", SuggestedType: "switch", Vendor: "Cisco", Model: "Catalyst 9300", Platform: "IOS XE 17.9", Confidence: 96, Evidence: []string{"sysObjectID matched Cisco Catalyst"}, Capabilities: []string{"snmpv3_identity"}}
	}
	verified := request(http.MethodPost, "/api/v1/network-onboarding/tests", testBody, principal)
	require.Equal(t, http.StatusOK, verified.Code, verified.Body.String())
	var verifiedReceipt storage.NetworkConnectionTest
	require.NoError(t, json.Unmarshal(verified.Body.Bytes(), &verifiedReceipt))
	saveBody, _ := json.Marshal(storage.NetworkSaveParams{TestID: verifiedReceipt.ID, DisplayName: "Lagos branch switch", Type: "switch"})
	saved := request(http.MethodPost, "/api/v1/network-onboarding/save", string(saveBody), principal)
	require.Equal(t, http.StatusCreated, saved.Code, saved.Body.String())
	require.Equal(t, "network_security", store.target.Family)
	require.Equal(t, "Cisco", store.target.Vendor)
	require.Equal(t, "Catalyst 9300", store.target.Model)
	require.Equal(t, 96, store.target.Classification.Confidence)

	// Inventory refresh runs against the stored credential and is returned with
	// the network-device target ID, never a node ID.
	store.connection = &storage.NetworkInventoryConnection{
		TargetID: store.target.ID, TenantID: tenantID, Protocol: "snmpv3", Address: "192.0.2.10", Port: 161,
		Credential: store.credential,
	}
	refresh := request(http.MethodPost, "/api/v1/network-inventory/"+store.target.ID.String(), `{}`, principal)
	require.Equal(t, http.StatusOK, refresh.Code, refresh.Body.String())
	require.Contains(t, refresh.Body.String(), `"state":"inventory_ready"`)
	require.Contains(t, refresh.Body.String(), `"edge-sw-01"`)
	require.NotContains(t, refresh.Body.String(), `"node_id"`)

	// All-tenant inventory includes the classified target; an explicit other
	// tenant query must not reveal it.
	all := request(http.MethodGet, "/api/v1/targets?family=network_security&limit=50&offset=0", "", principal)
	require.Equal(t, http.StatusOK, all.Code, all.Body.String())
	require.Contains(t, all.Body.String(), store.target.ID.String())
	other := request(http.MethodGet, "/api/v1/targets?tenant_id="+otherTenantID.String()+"&limit=50&offset=0", "", principal)
	require.Equal(t, http.StatusOK, other.Code, other.Body.String())
	require.NotContains(t, other.Body.String(), store.target.ID.String())

	// A stale telemetry source remains source-specific while the inventory
	// target is still classified as reachable.
	old := time.Now().UTC().Add(-time.Hour)
	store.sources = []storage.NetworkSource{{ID: uuid.New(), TargetID: store.target.ID, TenantID: tenantID, SourceType: "syslog", State: "ready", LastContactAt: &old, StaleAfterSeconds: 60}}
	telemetry := request(http.MethodGet, "/api/v1/network-telemetry/"+store.target.ID.String(), "", principal)
	require.Equal(t, http.StatusOK, telemetry.Code, telemetry.Body.String())
	require.Contains(t, telemetry.Body.String(), `"state":"stale"`)
	require.Equal(t, "reachable", store.target.ReachabilityState)
}
