package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAPTPolicyCandidateIdentityAndCredentialExclusion(t *testing.T) {
	items := []PackageInfo{{Name: "demo", Version: "1", Source: "dpkg", Arch: "amd64"}}
	for _, scenario := range []string{"verified_identity", "different_installed", "credentials", "wrong_arch"} {
		t.Run(scenario, func(t *testing.T) {
			installed, uri, arch := "1", "https://archive.ubuntu.com/ubuntu", "amd64"
			if scenario == "different_installed" {
				installed = "3"
			}
			if scenario == "credentials" {
				uri = "https://private:secret@archive.ubuntu.com/ubuntu"
			}
			if scenario == "wrong_arch" {
				arch = "arm64"
			}
			body := fmt.Sprintf("demo:amd64:\n  Installed: %s\n  Candidate: 2\n  Version table:\n     2 500\n        500 %s noble-updates/main %s Packages\n *** 1 100\n        100 /var/lib/dpkg/status\n", installed, uri, arch)
			rows := parseAPTPolicy(body, items)
			require.Len(t, rows, 1)
			require.Equal(t, "2", rows[0].CandidateVersion)
			if scenario == "verified_identity" {
				require.Equal(t, uri, rows[0].Repository)
				require.Equal(t, "amd64", rows[0].RepositoryArch)
			} else {
				require.Empty(t, rows[0].Repository)
			}
		})
	}
}

func TestSignedAPTReleaseFreshnessAndIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, scenario := range []string{"fresh", "expired", "future", "bad_expiry", "wrong_vendor", "missing_digest"} {
		t.Run(scenario, func(t *testing.T) {
			date, expiry, vendor := now.Add(-time.Hour), now.Add(time.Hour), "Ubuntu"
			if scenario == "expired" {
				expiry = now.Add(-time.Second)
			}
			if scenario == "future" {
				date = now.Add(time.Hour)
			}
			if scenario == "wrong_vendor" {
				vendor = "unknown"
			}
			expiryText := expiry.Format(time.RFC1123)
			if scenario == "bad_expiry" {
				expiryText = "invalid"
			}
			body := fmt.Sprintf("Origin: %s\nSuite: noble-updates\nDate: %s\nValid-Until: %s\nSHA256:\n %s 10 main/binary-amd64/Packages\n", vendor, date.Format(time.RFC1123), expiryText, strings.Repeat("a", 64))
			if scenario == "missing_digest" {
				body = strings.ReplaceAll(body, "SHA256:", "MD5Sum:")
			}
			proof, err := parseVerifiedAPTRelease([]byte(body), "main", "amd64", now)
			if scenario == "fresh" {
				require.NoError(t, err)
				require.Equal(t, "Ubuntu", proof.Vendor)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestNativeAPTRepositoryProof(t *testing.T) {
	if os.Getenv("CONTROLONE_APT_NATIVE_ORACLE") != "1" {
		t.Skip("existing disposable Ubuntu lab only")
	}
	out, err := exec.Command("dpkg-query", "-W", "-f=${Package}\t${Version}\t${Architecture}\t${db:Status-Status}\n", "liblzma5").Output()
	require.NoError(t, err)
	items, err := parseCompleteNativeInventory(string(out), "dpkg")
	require.NoError(t, err)
	require.Len(t, items, 1)
	rows := collectAPTCandidates(items)
	require.Len(t, rows, 1)
	t.Logf("candidate identity=%s installed=%s candidate=%s state=%s reason=%s vendor=%s release=%s index=%s artifact=%s", rows[0].Name, rows[0].InstalledVersion, rows[0].CandidateVersion, rows[0].State, rows[0].Reason, rows[0].Vendor, rows[0].ReleaseSHA256, rows[0].IndexSHA256, rows[0].ArtifactSHA256)
	require.Equal(t, "verified_repository_candidate", rows[0].State)
	paths, err := filepath.Glob("/var/lib/apt/lists/*_InRelease")
	require.NoError(t, err)
	require.NotEmpty(t, paths)
	body, err := os.ReadFile(paths[0])
	require.NoError(t, err)
	tampered := strings.Replace(string(body), "Origin: Ubuntu", "Origin: Forged", 1)
	require.NotEqual(t, string(body), tampered)
	file := filepath.Join(t.TempDir(), "tampered-InRelease")
	require.NoError(t, os.WriteFile(file, []byte(tampered), 0600))
	_, err = exec.Command("gpgv", "--keyring", "/usr/share/keyrings/ubuntu-archive-keyring.gpg", "--output", "-", file).Output()
	require.Error(t, err, "altered signed repository metadata must be rejected")
}
