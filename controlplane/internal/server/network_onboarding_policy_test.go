package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type networkPolicyTestStore struct {
	*fakeStore
	policy *storage.NetworkOnboardingPolicy
}

func (f *networkPolicyTestStore) GetNetworkOnboardingPolicy(context.Context) (*storage.NetworkOnboardingPolicy, error) {
	return f.policy, nil
}

func (f *networkPolicyTestStore) UpsertNetworkOnboardingPolicy(_ context.Context, cidrs []string) error {
	f.policy = &storage.NetworkOnboardingPolicy{AllowedCIDRs: cidrs}
	return nil
}

func TestNormalizeNetworkOnboardingCIDRs(t *testing.T) {
	got, err := normalizeNetworkOnboardingCIDRs([]string{"192.168.56.10", "192.168.57.9/24", "2001:db8::1"})
	require.NoError(t, err)
	require.Equal(t, []string{"192.168.56.10/32", "192.168.57.0/24", "2001:db8::1/128"}, got)
	for _, invalid := range [][]string{{"not-an-ip"}, {"0.0.0.0/0"}, {"::/0"}, {"192.168.1.1", "192.168.1.1/32"}} {
		_, err := normalizeNetworkOnboardingCIDRs(invalid)
		require.Error(t, err, "expected %v to be rejected", invalid)
	}
}

func TestNetworkOnboardingPolicyRequiresAdminAndPersistsCIDRs(t *testing.T) {
	user := uuid.New()
	store := &networkPolicyTestStore{fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{user: {ID: user}}}}
	s := New(zap.NewNop(), &config.Config{NetworkOnboarding: config.NetworkOnboardingConfig{AllowedCIDRs: []string{"172.20.0.6/32"}}}, store, nil)
	s.auditAsync = false
	request := func(method, body string, principal *auth.Principal) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/api/v1/network-onboarding/policy", bytes.NewBufferString(body))
		s.baseRouter.ServeHTTP(w, withPrincipal(r, principal))
		return w
	}
	admin := &auth.Principal{Type: "user", Subject: user.String(), Roles: []string{roleAdmin}}
	operator := &auth.Principal{Type: "user", Subject: user.String(), Roles: []string{roleOperator}}

	got := request(http.MethodGet, "", admin)
	require.Equal(t, http.StatusOK, got.Code, got.Body.String())
	require.Contains(t, got.Body.String(), `"source":"server_config"`)
	require.Contains(t, got.Body.String(), `172.20.0.6/32`)
	require.Equal(t, http.StatusForbidden, request(http.MethodGet, "", operator).Code)

	bad := request(http.MethodPut, `{"allowed_cidrs":["0.0.0.0/0"]}`, admin)
	require.Equal(t, http.StatusBadRequest, bad.Code)

	saved := request(http.MethodPut, `{"allowed_cidrs":["192.168.56.10","192.168.56.0/24"]}`, admin)
	require.Equal(t, http.StatusOK, saved.Code, saved.Body.String())
	require.Equal(t, []string{"192.168.56.10/32", "192.168.56.0/24"}, store.policy.AllowedCIDRs)
	require.Contains(t, saved.Body.String(), `"source":"database"`)
	require.NotEmpty(t, store.auditLogs)
	require.Equal(t, "network_onboarding.policy.updated", store.auditLogs[len(store.auditLogs)-1].Action)

	var response networkOnboardingPolicyResponse
	require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &response))
	require.Equal(t, store.policy.AllowedCIDRs, response.AllowedCIDRs)
}
