package onlineapt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The test executable also exercises the same APT hook protocol as the agent.
func TestMain(m *testing.M) {
	if len(os.Args) == 4 && os.Args[1] == "verify-online-plan" {
		err := VerifyInvocation(context.Background(), Spec{Root: os.Args[3]}, os.Args[2], os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func nativeFixture(t *testing.T) (Spec, string, func() int64) {
	t.Helper()
	if runtime.GOOS != "linux" || os.Getenv("CONTROLONE_ONLINE_APT_NATIVE") != "1" {
		t.Skip("explicit isolated Ubuntu public repository fixture required")
	}
	root, err := os.MkdirTemp("/tmp", "controlone-online-native-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	require.NoError(t, os.Chmod(root, 0755))
	repo := filepath.Join(root, "repository")
	index := filepath.Join(repo, "dists/noble-updates/main/binary-amd64/Packages.xz")
	require.NoError(t, os.MkdirAll(filepath.Dir(index), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(repo, "pool/main/x/xz-utils"), 0755))
	copyFile := func(from, to string) {
		body, e := os.ReadFile(from)
		require.NoError(t, e)
		require.NoError(t, os.WriteFile(to, body, 0644))
	}
	copyFile("/var/lib/apt/lists/archive.ubuntu.com_ubuntu_dists_noble-updates_InRelease", filepath.Join(repo, "dists/noble-updates/InRelease"))
	copyFile("/tmp/controlone-offline-Packages.xz", index)
	copyFile("/tmp/controlone-offline-liblzma5.deb", filepath.Join(repo, "pool/main/x/xz-utils/liblzma5_5.6.1+really5.4.5-1ubuntu0.4_amd64.deb"))
	var hits atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.FileServer(http.Dir(repo)).ServeHTTP(w, r)
	}))
	listener, e := net.Listen("tcp", "127.0.0.1:80")
	require.NoError(t, e)
	_ = server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	sources := filepath.Join(root, "inputs")
	require.NoError(t, os.Mkdir(sources, 0755))
	require.NoError(t, os.Mkdir(filepath.Join(sources, "sources.list.d"), 0755))
	source := fmt.Sprintf("deb [arch=amd64 signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] %s noble-updates main\n", server.URL)
	require.NoError(t, os.WriteFile(filepath.Join(sources, "sources.list"), []byte(source), 0644))
	return Spec{Root: filepath.Join(root, "plans"), SourcesRoot: sources}, server.URL, hits.Load
}

func onlineRequest(mode, proxy string) patchsupport.OnlinePreparation {
	var hosts []string
	if mode == "proxy" {
		hosts = []string{"127.0.0.1"}
	}
	return patchsupport.OnlinePreparation{RepositoryHosts: hosts, NodeID: uuid.NewString(), Mode: mode, ProxyURL: proxy, TransportContext: strings.Repeat("a", 64), Packages: []string{"liblzma5"}, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), RequestedBy: "native-isolated-test"}
}

func TestNativeOnlinePreparationTransportAndTampering(t *testing.T) {
	spec, origin, hits := nativeFixture(t)
	before, err := hashFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	t.Cleanup(func() {
		after, e := hashFile("/var/lib/dpkg/status")
		require.NoError(t, e)
		require.Equal(t, before, after, "preparation must not install packages")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var proxyHits atomic.Int64
	target, err := url.Parse(origin)
	require.NoError(t, err)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits.Add(1)
		if r.URL.Host != target.Host {
			http.Error(w, "destination denied", 403)
			return
		}
		outgoing := r.Clone(r.Context())
		outgoing.RequestURI = ""
		response, e := transport.RoundTrip(outgoing)
		if e != nil {
			http.Error(w, "upstream failed", 502)
			return
		}
		defer response.Body.Close()
		for key, values := range response.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	defer proxy.Close()
	// Ambient proxy variables are intentionally hostile; direct config must win.
	t.Setenv("http_proxy", proxy.URL)
	t.Setenv("HTTP_PROXY", proxy.URL)
	direct, err := Prepare(ctx, spec, onlineRequest("direct", ""))
	require.NoError(t, err)
	require.Positive(t, hits())
	require.Zero(t, proxyHits.Load())
	require.NoError(t, Select(ctx, spec, direct))
	require.NoError(t, Verify(spec, direct))
	viaProxy, err := Prepare(ctx, spec, onlineRequest("proxy", proxy.URL))
	require.NoError(t, err)
	require.Positive(t, proxyHits.Load())
	require.NoError(t, Select(ctx, spec, viaProxy))
	require.ErrorContains(t, requireSelected(spec, direct.SHA256), "replaced or withdrawn")
	require.NoError(t, Verify(spec, viaProxy))
	require.Equal(t, "ready", Discovery(spec).Status)
	root := filepath.Join(spec.Root, viaProxy.SHA256)
	guard, _ := json.Marshal(executionGuard{Nonce: "negative-guard-check", PlanSHA256: viaProxy.SHA256})
	require.NoError(t, os.WriteFile(filepath.Join(root, "execution-guard.json"), guard, 0600))
	require.Error(t, VerifyInvocation(ctx, spec, viaProxy.SHA256, strings.NewReader("/tmp/unapproved.deb\n")), "unapproved archive must fail before dpkg")
	beforeFallback := hits()
	proxy.Close()
	_, err = Prepare(ctx, spec, onlineRequest("proxy", proxy.URL))
	require.Error(t, err)
	require.Equal(t, beforeFallback, hits(), "unavailable proxy must not fall back to direct repositories")

	changed := viaProxy
	changed.TransportContext = strings.Repeat("e", 64)
	require.Error(t, Verify(spec, changed))
	require.NoError(t, os.WriteFile(filepath.Join(spec.SourcesRoot, "sources.list"), []byte("# changed source policy\n"), 0644))
	require.ErrorContains(t, Verify(spec, viaProxy), "sources/preferences/trust changed")
	require.Equal(t, "unavailable", Discovery(spec).Status)
	t.Log("actual authenticated APT refresh/resolution passed for direct and explicit proxy; ambient proxy ignored in direct mode; replaced selection, altered transport and source changes rejected; installed state unchanged")
}

func TestNativeOnlineRealInstallation(t *testing.T) {
	if os.Getenv("CONTROLONE_ONLINE_INSTALL_NATIVE") != "1" {
		t.Skip("explicit unenrolled, network-disabled disposable fixture required")
	}
	spec, origin, _ := nativeFixture(t)
	routes, err := os.ReadFile("/proc/net/route")
	require.NoError(t, err)
	for _, line := range strings.Split(string(routes), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 {
			require.NotEqual(t, "00000000", f[1])
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	mode := os.Getenv("CONTROLONE_ONLINE_TEST_MODE")
	if mode == "" {
		mode = "direct"
	}
	request := onlineRequest("direct", "")
	var proxyHits atomic.Int64
	if mode == "proxy" {
		target, e := url.Parse(origin)
		require.NoError(t, e)
		relay := httputil.NewSingleHostReverseProxy(target)
		relay.Transport = &http.Transport{Proxy: nil}
		proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { proxyHits.Add(1); relay.ServeHTTP(w, r) }))
		defer proxy.Close()
		request = onlineRequest("proxy", proxy.URL)
	}
	before, err := exec.Command("dpkg-query", "-W", "-f=${Version}", "liblzma5").Output()
	require.NoError(t, err)
	p, err := Prepare(ctx, spec, request)
	require.NoError(t, err)
	require.NotEmpty(t, p.Resolved, "fresh disposable image must contain a real older package")
	require.NoError(t, Select(ctx, spec, p))
	binary, err := os.Executable()
	require.NoError(t, err)
	t.Setenv("CONTROLONE_ONLINE_TEST_ROOT", spec.Root)
	out, err := Install(ctx, spec, p, binary, nil)
	require.NoError(t, err, string(out))
	after, err := exec.Command("dpkg-query", "-W", "-f=${Version}", "liblzma5").Output()
	require.NoError(t, err)
	require.Equal(t, "5.6.1+really5.4.5-1ubuntu0.4", string(after))
	require.ErrorContains(t, Verify(spec, p), "installed baseline changed")
	if mode == "proxy" {
		require.Positive(t, proxyHits.Load())
	}
	t.Logf("real %s acquired exact APT upgrade %s -> %s passed the under-lock pre-install guard and matching nonce receipt; no route outside loopback fixture; replay on changed baseline rejected", mode, before, after)
}
