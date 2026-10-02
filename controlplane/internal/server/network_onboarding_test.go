package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type networkTestStore struct {
	*fakeStore
	credential *storage.ProviderCredential
	receipt    storage.NetworkConnectionTest
	access     storage.TargetAccess
	beginCalls int
}

func (f *networkTestStore) CreateNetworkCredential(_ context.Context, p storage.CreateProviderCredentialParams, a storage.TargetAccess) (*storage.ProviderCredential, error) {
	f.access = a
	f.credential = &storage.ProviderCredential{ID: uuid.New(), TenantID: p.TenantID, Name: p.Name, Provider: p.Provider, ConfigEncrypted: p.ConfigEncrypted, Nonce: p.Nonce}
	return f.credential, nil
}
func (f *networkTestStore) GetNetworkCredential(_ context.Context, id, tenant uuid.UUID, a storage.TargetAccess) (*storage.ProviderCredential, error) {
	f.access = a
	if f.credential != nil && f.credential.ID == id && f.credential.TenantID == tenant {
		return f.credential, nil
	}
	return nil, nil
}
func (f *networkTestStore) BeginNetworkConnectionTest(_ context.Context, p storage.NetworkConnectionTest, a storage.TargetAccess) (*storage.NetworkConnectionTest, error) {
	f.access = a
	f.beginCalls++
	p.ID = uuid.New()
	f.receipt = p
	return &f.receipt, nil
}
func (f *networkTestStore) FinishNetworkConnectionTest(_ context.Context, _ uuid.UUID, result networkdevice.Result) (*storage.NetworkConnectionTest, error) {
	f.receipt.State = result.State
	f.receipt.Result = result
	return &f.receipt, nil
}
func (f *networkTestStore) SaveNetworkOnboarding(_ context.Context, _ storage.NetworkSaveParams, a storage.TargetAccess) (*storage.Target, error) {
	f.access = a
	return &storage.Target{ID: uuid.New(), TenantID: f.receipt.TenantID, Family: "network_security"}, nil
}

func TestNetworkOnboardingAPISecurityAndReceipts(t *testing.T) {
	user, tenant := uuid.New(), uuid.New()
	store := &networkTestStore{fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{user: {ID: user}}}}
	logsCore, logs := observer.New(zap.DebugLevel)
	s := New(zap.New(logsCore), &config.Config{NetworkOnboarding: config.NetworkOnboardingConfig{AllowedCIDRs: []string{"192.0.2.0/24"}}}, store, nil)
	s.auditAsync = false
	s.sealer, _ = secretbox.NewSealer(bytes.Repeat([]byte{7}, 32))
	principal := &auth.Principal{Type: "user", Subject: user.String(), Roles: []string{roleOperator}}
	request := func(path string, payload any, who *auth.Principal) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(w, withPrincipal(httptest.NewRequest(http.MethodPost, "/api/v1/network-onboarding/"+path, bytes.NewReader(raw)), who))
		return w
	}
	secrets := networkdevice.Credential{Username: "readonly", AuthProtocol: "SHA256", PrivProtocol: "AES", AuthSecret: "sensitive-auth", PrivSecret: "sensitive-priv"}
	credentialResponse := request("credentials", networkCredentialRequest{TenantID: tenant, Protocol: "snmpv3", Name: "read only", Config: secrets}, principal)
	require.Equal(t, 201, credentialResponse.Code, credentialResponse.Body.String())
	require.Equal(t, "targets.connect", store.access.Permission)
	require.NotContains(t, credentialResponse.Body.String(), "sensitive-auth")
	require.NotContains(t, credentialResponse.Body.String(), "config_encrypted")
	plain, err := s.sealer.Open(store.credential.ConfigEncrypted, store.credential.Nonce)
	require.NoError(t, err)
	require.Contains(t, string(plain), "sensitive-auth")
	probeCalls := 0
	s.networkProbe = func(_ context.Context, _ string, _ int, _ string, c networkdevice.Credential) networkdevice.Result {
		probeCalls++
		require.Equal(t, secrets.AuthSecret, c.AuthSecret)
		return networkdevice.Outcome("auth_failed")
	}
	payload := networkTestRequest{TenantID: tenant, CredentialID: store.credential.ID, Address: "192.0.2.1"}
	for _, state := range []string{"auth_failed", "unreachable", "authenticated"} {
		s.networkProbe = func(_ context.Context, _ string, _ int, _ string, _ networkdevice.Credential) networkdevice.Result {
			probeCalls++
			return networkdevice.Outcome(state)
		}
		w := request("tests", payload, principal)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"state":"`+state+`"`)
		require.NotContains(t, w.Body.String(), "sensitive-auth")
		require.NotContains(t, w.Body.String(), "sensitive-priv")
	}
	require.Equal(t, 3, probeCalls)
	require.Equal(t, 3, store.beginCalls)
	payload.TenantID = uuid.New()
	w := request("tests", payload, principal)
	require.Equal(t, 403, w.Code)
	require.Equal(t, 3, probeCalls)
	payload.TenantID = tenant
	payload.Address = "169.254.169.254"
	w = request("tests", payload, principal)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "policy_blocked")
	require.Equal(t, 3, probeCalls)
	viewer := *principal
	viewer.Roles = []string{roleViewer}
	w = request("tests", payload, &viewer)
	require.Equal(t, 403, w.Code)
	agent := *principal
	agent.Type = "agent"
	w = request("tests", payload, &agent)
	require.Equal(t, 403, w.Code)
	s.sealer = nil
	w = request("credentials", networkCredentialRequest{TenantID: tenant, Protocol: "snmpv3", Name: "ref", Config: secrets}, principal)
	require.Equal(t, 503, w.Code)
	audit, _ := json.Marshal(store.auditLogs)
	logged, _ := json.Marshal(logs.All())
	for _, raw := range []string{string(audit), string(logged)} {
		require.NotContains(t, raw, "sensitive-auth")
		require.NotContains(t, raw, "sensitive-priv")
	}
	require.Contains(t, string(audit), "network_connection.completed")
	require.NotContains(t, string(audit), "enrollment")
	// Strict payload decoding rejects caller-supplied credentials on test/save.
	w = request("tests", map[string]any{"tenant_id": tenant, "credential_id": uuid.New(), "address": "192.0.2.1", "auth_secret": "injected"}, principal)
	// Restore sealer to reach strict decode rather than missing-encryption response.
	s.sealer, _ = secretbox.NewSealer(bytes.Repeat([]byte{7}, 32))
	w = request("tests", map[string]any{"tenant_id": tenant, "credential_id": uuid.New(), "address": "192.0.2.1", "password": "injected"}, principal)
	require.Equal(t, 400, w.Code)
	require.False(t, strings.Contains(w.Body.String(), "injected"))
}
