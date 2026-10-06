package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type configurationHistoryTestStore struct {
	*fakeStore
	history *storage.NetworkConfigurationHistory
}

func (s *configurationHistoryTestStore) GetNetworkConfigurationHistory(context.Context, uuid.UUID, storage.TargetAccess) (*storage.NetworkConfigurationHistory, error) {
	return s.history, nil
}

func TestNetworkConfigurationHistoryIsReadOnlyAndEvidenceLinked(t *testing.T) {
	userID, tenantID, targetID := uuid.New(), uuid.New(), uuid.New()
	first, err := networkdevice.NormalizeConfigurationSnapshot("ssh_config/cisco", "management-read/v1", "text", "hostname edge\n")
	require.NoError(t, err)
	second, err := networkdevice.NormalizeConfigurationSnapshot("ssh_config/cisco", "management-read/v1", "text", "hostname edge\ntransport input telnet\n")
	require.NoError(t, err)
	history := &storage.NetworkConfigurationHistory{TargetID: targetID, State: "ready", Snapshots: []storage.NetworkConfigurationSnapshot{
		{ID: uuid.New(), TenantID: tenantID, TargetID: targetID, SourceType: "ssh_config", Format: "text", Content: first.Content, ContentHash: first.ContentHash, Revision: 1},
		{ID: uuid.New(), TenantID: tenantID, TargetID: targetID, SourceType: "ssh_config", Format: "text", Content: second.Content, ContentHash: second.ContentHash, Revision: 2},
	}}
	store := &configurationHistoryTestStore{fakeStore: &fakeStore{usersByID: map[uuid.UUID]*storage.User{userID: {ID: userID}}}, history: history}
	s := New(zap.NewNop(), &config.Config{}, store, nil)
	principal := &auth.Principal{Type: "user", Subject: userID.String(), Roles: []string{roleOperator}}
	call := func(method, path string, who *auth.Principal) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if who != nil {
			req = withPrincipal(req, who)
		}
		rec := httptest.NewRecorder()
		s.baseRouter.ServeHTTP(rec, req)
		return rec
	}
	rec := call(http.MethodGet, "/api/v1/network-configuration/"+targetID.String(), principal)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Snapshots []struct {
			Added    []string                             `json:"added"`
			Removed  []string                             `json:"removed"`
			Findings []networkdevice.ConfigurationFinding `json:"findings"`
		} `json:"snapshots"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Snapshots, 2)
	require.Equal(t, []string{"transport input telnet"}, response.Snapshots[1].Added)
	require.Contains(t, rec.Body.String(), "network_configuration_snapshots:")
	require.Contains(t, rec.Body.String(), "telnet_management")
	require.Equal(t, "finding", response.Snapshots[1].Findings[11].Status)
	require.Equal(t, "configuration_drift", response.Snapshots[1].Findings[11].ID)
	require.Contains(t, response.Snapshots[1].Findings[11].EvidenceRef, "network_configuration_snapshots:")
	require.Contains(t, response.Snapshots[1].Findings[11].RelatedEvidenceRef, "network_configuration_snapshots:")
	require.Equal(t, http.StatusMethodNotAllowed, call(http.MethodPost, "/api/v1/network-configuration/"+targetID.String(), principal).Code)
	require.Equal(t, http.StatusForbidden, call(http.MethodGet, "/api/v1/network-configuration/"+targetID.String(), &auth.Principal{Type: "agent", Subject: userID.String(), Roles: []string{roleOperator}}).Code)
	require.Equal(t, http.StatusUnauthorized, call(http.MethodGet, "/api/v1/network-configuration/"+targetID.String(), nil).Code)
}
