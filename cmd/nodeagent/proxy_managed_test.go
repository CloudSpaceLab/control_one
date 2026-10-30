package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type proxyInactiveError struct{}

func (proxyInactiveError) Error() string { return "inactive" }
func (proxyInactiveError) ExitCode() int { return 3 }
func proxyFixture(t *testing.T) (managedProxyExecutor, patchsupport.ProxyLifecycle, *[]string) {
	t.Helper()
	dir := t.TempDir()
	commands := []string{}
	e := managedProxyExecutor{ConfigPath: filepath.Join(dir, "squid.conf"), MarkerPath: filepath.Join(dir, "marker.json"), Ready: func(context.Context, []string) error { return nil }, Stopped: func(context.Context) error { return nil }, Version: func(context.Context) (string, error) { return "6.6-1", nil }}
	e.Run = func(ctx context.Context, name string, args ...string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		commands = append(commands, name+" "+strings.Join(args, " "))
		if name == "systemctl" && args[0] == "is-active" {
			return proxyInactiveError{}
		}
		if name == "apt-get" && strings.Contains(strings.Join(args, " "), " install ") {
			return os.WriteFile(e.ConfigPath, []byte("original\n"), 0644)
		}
		return nil
	}
	p := patchsupport.ProxyLifecycle{ProxyID: uuid.NewString(), NodeID: uuid.NewString(), Revision: 2, RequestedBy: "admin", ExpiresAt: time.Now().UTC().Add(time.Minute), Whitelist: []string{"archive.ubuntu.com"}, ClientCIDRs: []string{"127.0.0.1/32"}, ProbeURLs: []string{"https://archive.ubuntu.com/"}, SquidVersion: "6.6-1"}
	return e, p, &commands
}
func TestManagedProxyReconfigureAndRemovalPreserveOwnership(t *testing.T) {
	e, p, commands := proxyFixture(t)
	require.NoError(t, os.WriteFile(e.ConfigPath, []byte("original\n"), 0644))
	proof, err := e.Execute(context.Background(), "squid.reconfigure", p)
	require.NoError(t, err)
	require.True(t, proof.UpstreamVerified)
	require.False(t, proof.ObservedAt.IsZero())
	require.Contains(t, *commands, "systemctl restart squid.service")
	expected, err := patchsupport.ProxyConfigSHA(p.Whitelist, p.ClientCIDRs)
	require.NoError(t, err)
	require.Equal(t, expected, proof.ConfigSHA256)
	changed := p
	changed.ProxyID = uuid.NewString()
	_, err = e.Execute(context.Background(), "squid.reconfigure", changed)
	require.ErrorContains(t, err, "ownership")
	require.NoError(t, os.WriteFile(e.ConfigPath, []byte("operator drift\n"), 0644))
	_, err = e.Execute(context.Background(), "squid.reconfigure", p)
	require.ErrorContains(t, err, "drift")
	body, err := patchsupport.ManagedProxyConfig(p.Whitelist, p.ClientCIDRs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(e.ConfigPath, body, 0644))
	p.Whitelist = nil
	p.ClientCIDRs = nil
	p.ProbeURLs = nil
	proof, err = e.Execute(context.Background(), "squid.remove", p)
	require.NoError(t, err)
	require.True(t, proof.Removed)
	restored, err := os.ReadFile(e.ConfigPath)
	require.NoError(t, err)
	require.Equal(t, "original\n", string(restored))
	_, err = e.Execute(context.Background(), "squid.remove", p)
	require.ErrorContains(t, err, "already removed")
	oldProxyID := p.ProxyID
	p.ProxyID = uuid.NewString()
	p.Revision = 2
	p.Whitelist = []string{"archive.ubuntu.com"}
	p.ClientCIDRs = []string{"127.0.0.1/32"}
	p.ProbeURLs = []string{"https://archive.ubuntu.com/"}
	_, err = e.Execute(context.Background(), "squid.reconfigure", p)
	require.NoError(t, err, "a freshly bound proxy may adopt the verified restored baseline")
	_, err = os.Stat(filepath.Join(filepath.Dir(e.MarkerPath), "managed-proxy-retired-"+oldProxyID+".json"))
	require.NoError(t, err)
}
func TestManagedProxyFailuresNeverClaimVerifiedHealth(t *testing.T) {
	for _, scenario := range []string{"parse", "restart", "upstream", "cancel", "listener still open", "inactive unknown"} {
		t.Run(scenario, func(t *testing.T) {
			e, p, commands := proxyFixture(t)
			require.NoError(t, os.WriteFile(e.ConfigPath, []byte("original\n"), 0644))
			base := e.Run
			calls := 0
			removing := false
			e.Run = func(ctx context.Context, name string, args ...string) error {
				if scenario == "parse" && name == "squid" {
					return fmt.Errorf("parser rejected candidate")
				}
				if scenario == "restart" && name == "systemctl" && args[0] == "restart" {
					calls++
					if calls == 1 {
						return fmt.Errorf("activation failed")
					}
				}
				if scenario == "inactive unknown" && removing && name == "systemctl" && args[0] == "is-active" {
					return fmt.Errorf("bus unavailable")
				}
				return base(ctx, name, args...)
			}
			if scenario == "upstream" {
				e.Ready = func(context.Context, []string) error { return fmt.Errorf("TLS upstream unavailable") }
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				cancel()
			}
			if scenario == "listener still open" || scenario == "inactive unknown" {
				_, err := e.Execute(ctx, "squid.reconfigure", p)
				require.NoError(t, err)
				p.Whitelist = nil
				p.ClientCIDRs = nil
				p.ProbeURLs = nil
				removing = true
				if scenario == "listener still open" {
					e.Stopped = func(context.Context) error { return fmt.Errorf("listener remains open") }
				}
			}
			action := "squid.reconfigure"
			if scenario == "listener still open" || scenario == "inactive unknown" {
				action = "squid.remove"
			}
			proof, err := e.Execute(ctx, action, p)
			require.Error(t, err)
			require.False(t, proof.UpstreamVerified)
			require.False(t, proof.Removed)
			if scenario == "parse" || scenario == "restart" || scenario == "upstream" {
				body, readErr := os.ReadFile(e.ConfigPath)
				require.NoError(t, readErr)
				require.Equal(t, "original\n", string(body))
			}
			if scenario == "parse" {
				require.NotContains(t, *commands, "systemctl restart squid.service")
			}
		})
	}
}
func TestManagedProxyInstallMasksBeforePackagesAndRejectsWrongVersion(t *testing.T) {
	for _, badVersion := range []bool{false, true} {
		t.Run(fmt.Sprint(badVersion), func(t *testing.T) {
			e, p, commands := proxyFixture(t)
			if badVersion {
				e.Version = func(context.Context) (string, error) { return "wrong", nil }
			}
			proof, err := e.Execute(context.Background(), "squid.install", p)
			require.GreaterOrEqual(t, len(*commands), 3)
			require.Equal(t, "systemctl mask squid.service", (*commands)[0])
			require.Contains(t, (*commands)[2], "--no-remove --no-upgrade --no-install-recommends install squid=6.6-1")
			if badVersion {
				require.Error(t, err)
				require.False(t, proof.ServiceActive)
				require.NotContains(t, *commands, "systemctl unmask squid.service")
			} else {
				require.NoError(t, err)
				require.True(t, proof.ServiceActive)
			}
		})
	}
}

func TestManagedProxyFailedInstallStaysMaskedAndRequiresFreshRecovery(t *testing.T) {
	e, p, commands := proxyFixture(t)
	e.Ready = func(context.Context, []string) error { return fmt.Errorf("upstream unavailable") }
	proof, err := e.Execute(context.Background(), "squid.install", p)
	require.Error(t, err)
	require.False(t, proof.ServiceActive)
	restarts := 0
	for _, command := range *commands {
		if command == "systemctl restart squid.service" {
			restarts++
		}
	}
	require.Equal(t, 1, restarts, "rollback must not start vendor-default configuration")
	require.Equal(t, "systemctl mask squid.service", (*commands)[len(*commands)-1])
	p.Revision++
	e.Ready = func(context.Context, []string) error { return nil }
	proof, err = e.Execute(context.Background(), "squid.reconfigure", p)
	require.NoError(t, err)
	require.True(t, proof.UpstreamVerified)
}
