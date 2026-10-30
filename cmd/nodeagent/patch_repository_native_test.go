package main

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeAPTRejectsUnsignedIsolatedRepository(t *testing.T) {
	if runtime.GOOS != "linux" || os.Getenv("CONTROLONE_APT_NATIVE_ISOLATION") != "1" {
		t.Skip("explicit native isolated apt fixture required")
	}
	before, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	lists := filepath.Join(root, "lists")
	require.NoError(t, os.MkdirAll(repo, 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(lists, "partial"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(repo, "Release"), []byte("Origin: ControlOne isolated unsigned fixture\nSuite: stable\nCodename: stable\nArchitectures: amd64\nComponents: main\nDescription: unsigned rejection fixture\n"), 0644))
	source := filepath.Join(root, "sources.list")
	require.NoError(t, os.WriteFile(source, []byte("deb [trusted=no] file:"+repo+" ./\n"), 0644))
	cmd := exec.Command("apt-get", "-o", "APT::Update::Error-Mode=any", "-o", "APT::Sandbox::User=root", "-o", "Dir::Etc::sourcelist="+source, "-o", "Dir::Etc::sourceparts=-", "-o", "Dir::State::lists="+lists, "-o", "Dir::Cache="+filepath.Join(root, "cache"), "-o", "Acquire::AllowInsecureRepositories=false", "update")
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	out, runErr := cmd.CombinedOutput()
	require.Error(t, runErr, string(out))
	require.Contains(t, string(out), "not signed")
	after, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(before), sha256.Sum256(after))
	t.Log("native apt rejected unsigned local repository; normal source-parts excluded; installed package database unchanged")
}
