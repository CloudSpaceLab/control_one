package main

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInterfaceObservationFiltering(t *testing.T) {
	rows := interfaceObservations([]net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("169.254.1.2"), net.ParseIP("172.19.0.2"), net.ParseIP("172.19.0.2"), net.ParseIP("::1"), nil})
	require.Len(t, rows, 1)
	require.Equal(t, "private_ip", rows[0].Kind)
	require.Equal(t, "172.19.0.2", rows[0].Value)
}

func TestPatchRuntimeRejectsUnknownActionAndOfflineFallback(t *testing.T) {
	for _, action := range []string{"unexpected", patchJobAirgapped} {
		_, _, _, _, ok := patchCommandForJob(action, patchActionDetail{StagedRepoPath: "/tmp/repo; touch /tmp/unwanted"})
		require.False(t, ok)
	}
}

func TestAPTProxyIsExplicitAndRefreshFailsClosed(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux apt command contract")
	}
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apt-get"), []byte("#!/bin/sh\nexit 0\n"), 0755))
	t.Setenv("PATH", dir)
	_, args, env, _, ok := patchCommandForJob(patchJobProxy, patchActionDetail{ProxyURL: "http://repo-proxy:3128", PackageAllowlist: []string{"demo"}})
	require.True(t, ok)
	require.Contains(t, args[1], "APT::Update::Error-Mode=any")
	require.Equal(t, 2, strings.Count(args[1], "Acquire::https::Proxy="))
	require.Contains(t, env, "no_proxy=")
	require.Contains(t, env, "NO_PROXY=")
	for _, proxy := range []string{"", "http://user:secret@proxy:3128", "file:///tmp/proxy", "http://proxy:3128/path"} {
		_, _, _, _, ok := patchCommandForJob(patchJobProxy, patchActionDetail{ProxyURL: proxy})
		require.False(t, ok)
	}
}

func TestAPTRefreshFailureNeverStartsInstall(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux shell execution boundary")
	}
	dir := t.TempDir()
	trace := filepath.Join(dir, "trace")
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$TRACE\"\ncase \" $* \" in *\" update \"*) exit 100;; esac\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apt-get"), []byte(script), 0755))
	t.Setenv("PATH", dir)
	t.Setenv("TRACE", trace)
	name, args, env, _, ok := patchCommandForJob(patchJobProxy, patchActionDetail{ProxyURL: "http://repo-proxy:3128", PackageAllowlist: []string{"demo"}})
	require.True(t, ok)
	command := exec.Command(name, args...)
	command.Env = append(command.Environ(), env...)
	require.Error(t, command.Run())
	raw, err := os.ReadFile(trace)
	require.NoError(t, err)
	require.Contains(t, string(raw), "update -qq")
	require.NotContains(t, string(raw), "install --only-upgrade")
}
