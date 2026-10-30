package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type managedProxyEndpointStore struct {
	*squidTargetTestStore
	policy *storage.ProxyManagement
	queued []patchsupport.ProxyLifecycle
}

func (s *managedProxyEndpointStore) GetProxyManagement(context.Context, uuid.UUID, uuid.UUID) (*storage.ProxyManagement, error) {
	return s.policy, nil
}
func (s *managedProxyEndpointStore) BindProxyManagement(_ context.Context, tenant, proxy, node uuid.UUID, clients, probes []string) (*storage.ProxyManagement, error) {
	s.policy = &storage.ProxyManagement{ProxyID: proxy, NodeID: node, Revision: 1, ClientCIDRs: clients, ProbeURLs: probes}
	return s.policy, nil
}
func (s *managedProxyEndpointStore) QueueProxyLifecycle(_ context.Context, tenant, node uuid.UUID, action string, p patchsupport.ProxyLifecycle) (*storage.Job, error) {
	s.queued = append(s.queued, p)
	return &storage.Job{ID: uuid.New(), TenantID: tenant, Type: action, Status: storage.JobStatusRunning}, nil
}
func TestManagedProxyEndpointRequiresBoundCurrentRevisionAndRuntime(t *testing.T) {
	tenant, nodeID, proxyID := uuid.New(), uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, nodeID)
	store := &managedProxyEndpointStore{squidTargetTestStore: &squidTargetTestStore{patchTestStore: base, proxy: &storage.SquidProxy{ID: proxyID, TenantID: tenant, Host: base.nodes[0].Hostname, Port: 3128}}, policy: &storage.ProxyManagement{ProxyID: proxyID, NodeID: nodeID, Revision: 1}}
	srv := newProxyTestServer(store)
	principal := &auth.Principal{Type: "user", Subject: uuid.NewString(), Roles: []string{roleAdmin}}
	valid := `{"expected_revision":1,"whitelist":["archive.ubuntu.com"],"client_cidrs":["127.0.0.1/32"],"probe_urls":["https://archive.ubuntu.com/"]}`
	for _, scenario := range []string{"supported", "old agent", "stale runtime", "other tenant", "changed revision", "injected target", "unbound"} {
		t.Run(scenario, func(t *testing.T) {
			store.queued = nil
			store.policy = &storage.ProxyManagement{ProxyID: proxyID, NodeID: nodeID, Revision: 1}
			store.nodes[0].TenantID = tenant
			store.nodes[0].Labels["patch.runtime"] = patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", ProxyLifecycleVersion: 2}, ObservedAt: time.Now().UTC()}
			body := valid
			want := http.StatusAccepted
			switch scenario {
			case "old agent":
				store.nodes[0].Labels["patch.runtime"] = patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt"}, ObservedAt: time.Now().UTC()}
				want = 409
			case "stale runtime":
				store.nodes[0].Labels["patch.runtime"] = patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt", ProxyLifecycleVersion: 2}, ObservedAt: time.Now().Add(-time.Hour)}
				want = 409
			case "other tenant":
				store.nodes[0].TenantID = uuid.New()
				want = 409
			case "changed revision":
				store.policy.Revision = 2
				want = 409
			case "injected target":
				body = strings.TrimSuffix(valid, "}") + `,"node_id":"` + uuid.NewString() + `"}`
				want = 400
			case "unbound":
				store.policy = nil
				want = 409
			}
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
			srv.requestProxyLifecycle(rr, req, principal, store.proxy, "squid.reconfigure")
			require.Equal(t, want, rr.Code, rr.Body.String())
			if want == 202 {
				require.Len(t, store.queued, 1)
				require.Equal(t, nodeID.String(), store.queued[0].NodeID)
				require.EqualValues(t, 2, store.queued[0].Revision)
			} else {
				require.Empty(t, store.queued)
			}
		})
	}
}
func TestManagedProxyStatusUsesJSONPayloadAndTenantAuthorization(t *testing.T) {
	tenant, nodeID, proxyID, id := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	base := newPatchTestStore(tenant, nodeID)
	store := &managedProxyEndpointStore{squidTargetTestStore: &squidTargetTestStore{patchTestStore: base, proxy: &storage.SquidProxy{ID: proxyID, TenantID: tenant}}, policy: &storage.ProxyManagement{ProxyID: proxyID, NodeID: nodeID, Revision: 2, LastJobID: &id}}
	raw, _ := json.Marshal(map[string]any{"node_id": nodeID.String(), "expires_at": time.Now().Add(time.Minute)})
	store.jobs = map[uuid.UUID]*storage.Job{id: {ID: id, TenantID: tenant, Type: "squid.reconfigure", Status: storage.JobStatusRunning, Payload: raw}}
	srv := newProxyTestServer(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/patch/proxies/"+proxyID.String()+"/management", nil)
	rr := httptest.NewRecorder()
	srv.handleSquidProxySubroute(rr, req)
	require.Equal(t, 401, rr.Code)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: uuid.NewString(), Roles: []string{roleAdmin}}))
	rr = httptest.NewRecorder()
	srv.handleSquidProxySubroute(rr, req)
	require.Equal(t, 200, rr.Code, rr.Body.String())
	var response struct {
		Job struct {
			ID      string         `json:"id"`
			Status  string         `json:"status"`
			Payload map[string]any `json:"payload"`
		}
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	require.Equal(t, id.String(), response.Job.ID)
	require.Equal(t, "running", response.Job.Status)
	require.Equal(t, nodeID.String(), response.Job.Payload["node_id"])
}

func TestManagedProxyRemovalDetailIsReadableOnlyByItsTargetAgent(t *testing.T) {
	node := uuid.New()
	payload, _ := json.Marshal(map[string]string{"node_id": node.String(), "proxy_id": uuid.NewString()})
	job := &storage.Job{ID: uuid.New(), Type: "squid.remove", Payload: payload}
	require.True(t, agentCanReadJob(&auth.Principal{Type: "agent", Name: node.String()}, job))
	require.False(t, agentCanReadJob(&auth.Principal{Type: "agent", Name: uuid.NewString()}, job))
	require.False(t, agentCanReadJob(&auth.Principal{Type: "agent"}, job))
}
