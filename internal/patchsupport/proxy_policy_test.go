package patchsupport

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedProxyPolicyRequiresExplicitSourceAndDestination(t *testing.T) {
	hosts := []string{"archive.ubuntu.com"}
	body, err := ManagedProxyConfig(hosts, []string{"10.20.30.0/24", "2001:db8::/64"})
	require.NoError(t, err)
	require.Contains(t, string(body), "10.20.30.0/24 2001:db8::/64")
	require.Contains(t, string(body), "http_access allow local_clients allowed_hosts\nhttp_access deny all")
	for _, clients := range [][]string{nil, {"0.0.0.0/0"}, {"::/0"}, {"10.20.30.1/24"}, {"127.0.0.1/32\nhttp_access allow all"}, {"224.0.0.0/4"}, {"::ffff:10.0.0.1/128"}, {"10.0.0.0/24", "10.0.0.0/24"}} {
		_, err := ManagedProxyConfig(hosts, clients)
		require.Error(t, err)
	}
	for _, bad := range []string{".ubuntu.com", "archive.ubuntu.com.", "127.0.0.1:80"} {
		_, err := ManagedProxyConfig([]string{bad}, []string{"127.0.0.1/32"})
		require.Error(t, err)
	}
	require.NoError(t, ValidateProxyProbes(hosts, []string{"https://archive.ubuntu.com/ubuntu/"}))
	for _, probes := range [][]string{nil, {"http://archive.ubuntu.com/"}, {"https://archive.ubuntu.com:444/"}, {"https://evil.example/"}, {"https://user:pass@archive.ubuntu.com/"}, {"https://archive.ubuntu.com/?token=secret"}} {
		require.Error(t, ValidateProxyProbes(hosts, probes))
	}
}
