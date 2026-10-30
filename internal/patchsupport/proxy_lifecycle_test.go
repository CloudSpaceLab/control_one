package patchsupport

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProxyLifecycleRequiresBoundAuthorityAndDeadline(t *testing.T) {
	now := time.Now().UTC()
	p := ProxyLifecycle{ProxyID: uuid.NewString(), NodeID: uuid.NewString(), RequestedBy: "admin", ExpiresAt: now.Add(time.Minute), Whitelist: []string{"archive.ubuntu.com"}}
	require.NoError(t, p.Validate("squid.reconfigure", now))
	for _, change := range []func(*ProxyLifecycle){
		func(p *ProxyLifecycle) { p.ProxyID = "" }, func(p *ProxyLifecycle) { p.NodeID = uuid.Nil.String() },
		func(p *ProxyLifecycle) { p.RequestedBy = "" }, func(p *ProxyLifecycle) { p.ExpiresAt = now },
		func(p *ProxyLifecycle) { p.ExpiresAt = now.Add(time.Hour) }, func(p *ProxyLifecycle) { p.Whitelist = []string{"bad\nhttp_access allow all"} },
	} {
		bad := p
		change(&bad)
		require.Error(t, bad.Validate("squid.reconfigure", now))
	}
	require.Error(t, p.Validate("squid.configure_client", now))
	require.Error(t, p.Validate("squid.remove", now))
	p.Whitelist = nil
	require.NoError(t, p.Validate("squid.remove", now))
}
