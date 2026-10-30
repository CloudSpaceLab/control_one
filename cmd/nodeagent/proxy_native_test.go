package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/stretchr/testify/require"
)

func TestNativeManagedProxyACLAndTLS(t *testing.T) {
	if os.Getenv("CONTROLONE_SQUID_NATIVE_ISOLATION") != "1" {
		t.Skip("explicit isolated native Squid fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var hits atomic.Int32
	var redirectTargets atomic.Int32
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/redirect-target", http.StatusFound)
			return
		}
		if r.URL.Path == "/redirect-target" {
			redirectTargets.Add(1)
		}
		w.WriteHeader(204)
	}))
	listener, err := net.Listen("tcp", "127.0.0.1:443")
	require.NoError(t, err)
	_ = origin.Listener.Close()
	origin.Listener = listener
	origin.StartTLS()
	defer origin.Close()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(filepath.Dir(dir), 0755))
	require.NoError(t, os.Chmod(dir, 0777))
	body, err := patchsupport.ManagedProxyConfig([]string{"127.0.0.1"}, []string{"127.0.0.2/32"})
	require.NoError(t, err)
	body = append(body, []byte(fmt.Sprintf("pid_filename %s/squid.pid\ncache_log %s/cache.log\naccess_log none\ncache_store_log none\ncache deny all\ncache_mem 8 MB\n", dir, dir))...)
	path := filepath.Join(dir, "squid.conf")
	require.NoError(t, os.WriteFile(path, body, 0644))
	logFile, err := os.Create(filepath.Join(dir, "process.log"))
	require.NoError(t, err)
	defer logFile.Close()
	cmd := exec.CommandContext(ctx, "squid", "-N", "-f", path)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	require.NoError(t, cmd.Start())
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if t.Failed() {
			data, _ := os.ReadFile(logFile.Name())
			t.Log(string(data))
		}
	}()
	ready := false
	for end := time.Now().Add(6 * time.Second); time.Now().Before(end); {
		conn, e := net.DialTimeout("tcp", "127.0.0.1:3128", 100*time.Millisecond)
		if e == nil {
			_ = conn.Close()
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	require.True(t, ready, "native Squid listener did not start")
	proxy, _ := url.Parse("http://127.0.0.1:3128")
	request := func(source, host string, trusted bool) (int, error) {
		roots := x509.NewCertPool()
		if trusted {
			roots.AddCert(origin.Certificate())
		}
		transport := &http.Transport{Proxy: http.ProxyURL(proxy), DialContext: (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(source)}}).DialContext, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
		req, e := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+host+"/", nil)
		if e != nil {
			return 0, e
		}
		resp, e := client.Do(req)
		if e != nil {
			return 0, e
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}
	status, err := request("127.0.0.2", "127.0.0.1", true)
	require.NoError(t, err)
	require.Equal(t, 204, status)
	_, err = request("127.0.0.3", "127.0.0.1", true)
	require.Error(t, err)
	require.True(t, strings.Contains(strings.ToLower(err.Error()), "forbidden"))
	_, err = request("127.0.0.2", "localhost", true)
	require.Error(t, err)
	_, err = request("127.0.0.2", "127.0.0.1", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "certificate")
	require.EqualValues(t, 1, hits.Load(), "only the authorized source/destination with verified TLS may reach HTTP content")
	// Exercise the production probe implementation, not only hand-built HEAD
	// requests. It must reject redirects even with a redirect-following client.
	roots := x509.NewCertPool()
	roots.AddCert(origin.Certificate())
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DialContext: (&net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.2")}}).DialContext, TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}
	require.NoError(t, probeProxyRepositories(ctx, client, []string{"https://127.0.0.1/"}))
	require.ErrorContains(t, probeProxyRepositories(ctx, client, []string{"https://127.0.0.1/redirect"}), "HTTP 302")
	require.Zero(t, redirectTargets.Load(), "a repository redirect must never be followed")
}
