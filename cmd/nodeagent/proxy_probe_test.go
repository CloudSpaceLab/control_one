package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManagedProxyRepositoryProbeRequiresExactSuccessfulResource(t *testing.T) {
	for _, status := range []int{200, 204, 206, 301, 302, 307, 308, 401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var redirected atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodHead, r.Method)
				if r.URL.Path == "/next" {
					redirected.Add(1)
					w.WriteHeader(200)
					return
				}
				w.Header().Set("Location", "/next")
				w.WriteHeader(status)
			}))
			defer srv.Close()
			client := srv.Client()
			err := probeProxyRepositories(context.Background(), client, []string{srv.URL})
			if status >= 200 && status < 300 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, fmt.Sprint(status))
			}
			require.Zero(t, redirected.Load())
			require.Nil(t, client.CheckRedirect, "probe must not mutate shared client")
		})
	}
	require.Error(t, probeProxyRepositories(context.Background(), nil, []string{"https://example.invalid/"}))
	require.Error(t, probeProxyRepositories(context.Background(), http.DefaultClient, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.Error(t, probeProxyRepositories(ctx, http.DefaultClient, []string{"https://example.invalid/"}))
}
