package networkcollector

import (
	"context"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAssignedTargetsRecurringPollsAndHeartbeat(t *testing.T) {
	const bindingID = "00000000-0000-0000-0000-000000000003"
	const targetID = "00000000-0000-0000-0000-000000000002"
	var polls atomic.Int32
	var reports atomic.Int32
	var heartbeats atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" || r.URL.Query().Get("tenant_id") != "00000000-0000-0000-0000-000000000001" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/v1/content-packs/collectors/site/network-bindings":
			json.NewEncoder(w).Encode(map[string]any{"data": []binding{{ID: bindingID, TargetID: targetID, SourceType: "snmp_poll"}, {ID: "00000000-0000-0000-0000-000000000004", TargetID: targetID, SourceType: "syslog"}}})
		case "/api/v1/content-packs/collectors/site/network-reports":
			var p struct {
				Reports []networkdevice.SourceReport `json:"reports"`
			}
			require.NoError(t, json.NewDecoder(r.Body).Decode(&p))
			require.Len(t, p.Reports, 1)
			report := p.Reports[0]
			require.Equal(t, bindingID, report.BindingID)
			require.Equal(t, "ready", report.State)
			require.NotNil(t, report.LastContactAt)
			require.Equal(t, float64(123), report.Metrics["uptime_ticks"])
			if reports.Add(1) == 2 {
				cancel()
			}
			w.WriteHeader(200)
		case "/api/v1/content-packs/collectors/site/heartbeat":
			heartbeats.Add(1)
			w.WriteHeader(200)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	t.Setenv("TOKEN", "fixture-token")
	t.Setenv("AUTH", "fixture-auth-secret")
	t.Setenv("PRIV", "fixture-priv-secret")
	cfg := Config{ControlPlaneURL: server.URL, TenantID: "00000000-0000-0000-0000-000000000001", CollectorID: "site", TokenEnv: "TOKEN", IntervalSeconds: 5, AllowedCIDRs: []string{"192.0.2.0/24"}, Targets: []Target{{ID: targetID, Address: "192.0.2.10", Username: "readonly", AuthProtocol: "SHA256", AuthSecretEnv: "AUTH", PrivSecretEnv: "PRIV"}}}
	c, err := New(cfg)
	require.NoError(t, err)
	c.poll = func(context.Context, string, int, networkdevice.Credential) (string, map[string]float64) {
		polls.Add(1)
		return "ready", map[string]float64{"uptime_ticks": 123, "interface_count": 2}
	}
	deadline, stop := context.WithTimeout(ctx, 8*time.Second)
	defer stop()
	c.Run(deadline, nil)
	require.Equal(t, int32(2), polls.Load())
	require.Equal(t, int32(2), reports.Load())
	require.GreaterOrEqual(t, heartbeats.Load(), int32(1))
}

func TestConfigurationRequiresLocalSecretsAndAllowlist(t *testing.T) {
	_, err := New(Config{ControlPlaneURL: "https://user:password@example.com"})
	require.Error(t, err)
	t.Setenv("TOKEN", "fixture")
	_, err = New(Config{ControlPlaneURL: "https://example.com", TenantID: "00000000-0000-0000-0000-000000000001", CollectorID: "site", TokenEnv: "TOKEN"})
	require.ErrorContains(t, err, "allowlist")
}
