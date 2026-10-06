package server

import (
	"bytes"
	"context"
	"database/sql"
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

type inventoryAPIStore struct {
	*fakeStore
	connection *storage.NetworkInventoryConnection
	result     storage.NetworkInventory
	begins     int
	deny       bool
	busy       bool
}

func (f *inventoryAPIStore) GetNetworkInventory(_ context.Context, _ uuid.UUID, _ storage.TargetAccess) (*storage.NetworkInventory, error) {
	if f.deny {
		return nil, sql.ErrNoRows
	}
	return &f.result, nil
}
func (f *inventoryAPIStore) GetNetworkInventoryConnection(_ context.Context, _ uuid.UUID, _ storage.TargetAccess) (*storage.NetworkInventoryConnection, error) {
	if f.deny {
		return nil, sql.ErrNoRows
	}
	return f.connection, nil
}
func (f *inventoryAPIStore) BeginNetworkInventory(_ context.Context, _ uuid.UUID, _ storage.TargetAccess) (*storage.NetworkInventory, error) {
	if f.busy {
		return nil, storage.ErrInventoryBusy
	}
	f.begins++
	f.result.RefreshID = uuid.New()
	return &f.result, nil
}
func (f *inventoryAPIStore) FinishNetworkInventory(_ context.Context, _, _ uuid.UUID, inv networkdevice.Inventory) (*storage.NetworkInventory, error) {
	f.result.State = inv.State
	if inv.State == "inventory_ready" {
		f.result.Snapshot = &inv
	}
	return &f.result, nil
}

func TestNetworkInventoryAPISecurityAndAudit(t *testing.T) {
	user, tenant, target := uuid.New(), uuid.New(), uuid.New()
	store := &inventoryAPIStore{fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{user: {ID: user}}}, result: storage.NetworkInventory{TargetID: target, TenantID: tenant}}
	core, logs := observer.New(zap.DebugLevel)
	s := New(zap.New(core), &config.Config{NetworkOnboarding: config.NetworkOnboardingConfig{AllowedCIDRs: []string{"192.0.2.0/24"}}}, store, nil)
	s.auditAsync = false
	s.sealer, _ = secretbox.NewSealer(bytes.Repeat([]byte{1}, 32))
	credential := networkdevice.Credential{Username: "readonly", AuthProtocol: "SHA256", PrivProtocol: "AES", AuthSecret: "SECRET_AUTH", PrivSecret: "SECRET_PRIV"}
	raw, _ := json.Marshal(credential)
	cipher, nonce, err := s.sealer.Seal(raw)
	require.NoError(t, err)
	store.connection = &storage.NetworkInventoryConnection{TargetID: target, TenantID: tenant, Protocol: "snmpv3", Address: "192.0.2.10", Port: 161, Credential: &storage.ProviderCredential{ConfigEncrypted: cipher, Nonce: nonce}}
	operator := &auth.Principal{Type: "user", Subject: user.String(), Roles: []string{roleOperator}}
	request := func(method, body string, who *auth.Principal) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(w, withPrincipal(httptest.NewRequest(method, "/api/v1/network-inventory/"+target.String(), strings.NewReader(body)), who))
		return w
	}
	calls := 0
	s.networkInventory = func(_ context.Context, _ string, _ int, _ string, c networkdevice.Credential) networkdevice.Inventory {
		calls++
		require.Equal(t, credential, c)
		return networkdevice.Inventory{State: "inventory_ready", Facts: map[string]networkdevice.Fact{}}
	}
	w := request(http.MethodPost, "{}", operator)
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, 1, calls)
	require.Equal(t, 1, store.begins)
	require.NotContains(t, w.Body.String(), "SECRET_AUTH")
	require.NotContains(t, w.Body.String(), "SECRET_PRIV")
	for _, body := range []string{`{"password":"hidden"}`, `{"address":"127.0.0.1"}`, `{} {}`} {
		w = request(http.MethodPost, body, operator)
		require.Equal(t, 400, w.Code)
	}
	viewer := *operator
	viewer.Roles = []string{roleViewer}
	require.Equal(t, 403, request(http.MethodPost, "{}", &viewer).Code)
	require.Equal(t, 200, request(http.MethodGet, "", &viewer).Code)
	agent := *operator
	agent.Type = "agent"
	require.Equal(t, 403, request(http.MethodGet, "", &agent).Code)
	store.deny = true
	require.Equal(t, 403, request(http.MethodPost, "{}", operator).Code)
	require.Equal(t, 404, request(http.MethodGet, "", operator).Code)
	store.deny = false
	store.busy = true
	require.Equal(t, 409, request(http.MethodPost, "{}", operator).Code)
	store.busy = false
	store.connection.Address = "169.254.169.254"
	w = request(http.MethodPost, "{}", operator)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "policy_blocked")
	require.Equal(t, 1, calls)
	audit, _ := json.Marshal(store.auditLogs)
	require.Contains(t, string(audit), "network_inventory.started")
	require.Contains(t, string(audit), "network_inventory.completed")
	require.NotContains(t, string(audit), "SECRET_")
	for _, entry := range logs.All() {
		require.NotContains(t, entry.Message, "SECRET_")
		for _, field := range entry.Context {
			require.NotContains(t, field.String, "SECRET_")
		}
	}
}
