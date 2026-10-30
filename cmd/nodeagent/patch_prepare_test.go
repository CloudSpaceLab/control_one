package main

import (
	"context"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/internal/api"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOfflinePreparationRevalidatesBeforePublication(t *testing.T) {
	old := patchAgentNodeID
	defer func() { patchAgentNodeID = old }()
	patchAgentNodeID = uuid.NewString()
	id := uuid.NewString()
	p := patchsupport.OfflinePreparation{NodeID: patchAgentNodeID, Repository: patchsupport.OfflineRepository{ID: uuid.NewString(), Name: "approved", Enabled: true, KeyringSHA256: strings.Repeat("a", 64), Suite: "noble-updates", Component: "main"}, Packages: []patchsupport.ExactPackage{{Name: "libudev1", Version: "1.2"}}, ExpiresAt: time.Now().UTC().Add(5 * time.Minute), RequestedBy: "operator"}
	for _, scenario := range []string{"current", "disabled", "changed", "stopped", "expired"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") {
					current := p
					status := "running"
					if scenario == "stopped" {
						status = "failed"
					}
					if scenario == "expired" {
						current.ExpiresAt = time.Now().Add(-time.Minute)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "type": patchsupport.PrepareOfflineJob, "status": status, "payload": current})
					return
				}
				repo := p.Repository
				if scenario == "disabled" {
					repo.Enabled = false
				}
				if scenario == "changed" {
					repo.KeyringSHA256 = strings.Repeat("b", 64)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"repository": repo})
			}))
			defer server.Close()
			client, err := api.NewClient(server.URL, "", "", "", "")
			require.NoError(t, err)
			err = validatePreparationSource(context.Background(), client, id, p)
			if scenario == "current" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
