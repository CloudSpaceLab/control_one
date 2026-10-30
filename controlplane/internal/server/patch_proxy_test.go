package server

import (
	"context"
	"fmt"
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

func TestSquidPreflightValidatesEvenWithoutLocalBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	require.NoError(t, validateSquidConfig([]string{"archive.ubuntu.com"}))
	for _, hosts := range [][]string{nil, {"repo.example\nhttp_access allow all"}, {"."}, {"bad host"}} {
		require.Error(t, validateSquidConfig(hosts))
	}
}

type squidTargetTestStore struct {
	*patchTestStore
	proxy           *storage.SquidProxy
	statusWrites    int
	whitelistWrites int
}

func (s *squidTargetTestStore) UpdateSquidProxyStatus(context.Context, uuid.UUID, string, string) error {
	s.statusWrites++
	return nil
}
func (s *squidTargetTestStore) UpdateSquidProxyWhitelist(context.Context, uuid.UUID, []string) error {
	s.whitelistWrites++
	return nil
}

func TestSquidLifecycleNeverClaimsSuccessWithoutTargetExecution(t *testing.T) {
	t.Run("removal does not claim queued work", func(t *testing.T) {
		tenant, nodeID, proxyID := uuid.New(), uuid.New(), uuid.New()
		store := &squidTargetTestStore{patchTestStore: newPatchTestStore(tenant, nodeID), proxy: &storage.SquidProxy{ID: proxyID, TenantID: tenant}}
		srv := newProxyTestServer(store)
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/patch/proxies/"+proxyID.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{Type: "user", Subject: uuid.NewString(), Roles: []string{roleAdmin}}))
		rr := httptest.NewRecorder()
		srv.handleSquidProxySubroute(rr, req)
		require.Equal(t, http.StatusNotImplemented, rr.Code, rr.Body.String())
		require.Zero(t, store.statusWrites)
		require.Empty(t, store.jobs)
	})
	for _, action := range []string{"install", "reconfigure"} {
		t.Run(action, func(t *testing.T) {
			tenant, nodeID, proxyID := uuid.New(), uuid.New(), uuid.New()
			store := &squidTargetTestStore{patchTestStore: newPatchTestStore(tenant, nodeID), proxy: &storage.SquidProxy{ID: proxyID, TenantID: tenant, Host: "proxy", Port: 3128}}
			store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt"}, ObservedAt: time.Now().UTC()}
			srv := newProxyTestServer(store.patchTestStore)
			srv.store = store
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"node_id":%q,"whitelist":["archive.ubuntu.com"]}`, nodeID)))
			rr := httptest.NewRecorder()
			if action == "install" {
				srv.handleSquidInstall(rr, req, nil, proxyID)
			} else {
				srv.handleSquidReconfigure(rr, req, nil, proxyID)
			}
			require.Equal(t, http.StatusNotImplemented, rr.Code, rr.Body.String())
			require.Contains(t, rr.Body.String(), "durable target dispatch")
			require.Empty(t, store.jobs)
			require.Zero(t, store.statusWrites)
			require.Zero(t, store.whitelistWrites)
		})
	}
	for _, action := range []string{JobTypeSquidInstall, JobTypeSquidReconfigure, JobTypeSquidConfigureClient} {
		srv := &Server{}
		require.ErrorIs(t, srv.handleSquidHeartbeatJob(context.Background(), &storage.Job{Type: action}), errSquidLifecycleUnqualified)
		_, err := decodeSquidPayload([]byte(fmt.Sprintf(`{"node_id":%q,"proxy_id":%q}`, uuid.New(), uuid.New())))
		require.ErrorIs(t, err, errSquidLifecycleUnqualified, "generic job creation must not bypass the dedicated lifecycle gate")
	}
}

func (s *squidTargetTestStore) GetSquidProxy(context.Context, uuid.UUID) (*storage.SquidProxy, error) {
	return s.proxy, nil
}

func TestSquidManagementRejectsWrongTenantAndUnqualifiedTarget(t *testing.T) {
	for _, action := range []string{"install", "reconfigure"} {
		for _, scenario := range []string{"other tenant", "missing runtime", "expired runtime", "Windows"} {
			t.Run(action+"/"+scenario, func(t *testing.T) {
				tenant, nodeID, proxyID := uuid.New(), uuid.New(), uuid.New()
				store := &squidTargetTestStore{patchTestStore: newPatchTestStore(tenant, nodeID), proxy: &storage.SquidProxy{ID: proxyID, TenantID: tenant, Host: "proxy", Port: 3128}}
				want := http.StatusConflict
				switch scenario {
				case "other tenant":
					store.nodes[0].TenantID = uuid.New()
					want = http.StatusNotFound
				case "missing runtime":
					delete(store.nodes[0].Labels, "patch.runtime")
				case "expired runtime":
					store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt"}, ObservedAt: time.Now().Add(-6 * time.Minute)}
				case "Windows":
					store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "windows", Manager: "winget"}, ObservedAt: time.Now().UTC()}
				}
				srv := newProxyTestServer(store.patchTestStore)
				srv.store = store
				req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(fmt.Sprintf(`{"node_id":%q,"whitelist":["archive.ubuntu.com"]}`, nodeID)))
				rr := httptest.NewRecorder()
				if action == "install" {
					srv.handleSquidInstall(rr, req, nil, proxyID)
				} else {
					srv.handleSquidReconfigure(rr, req, nil, proxyID)
				}
				require.Equal(t, want, rr.Code, rr.Body.String())
				require.Empty(t, store.jobs)
			})
		}
	}
}
