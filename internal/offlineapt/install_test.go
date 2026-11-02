package offlineapt

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNativeOfflineDurablePlan(t *testing.T) {
	spec := vendorFixture(t)
	archive, err := os.ReadFile("/tmp/controlone-offline-liblzma5.deb")
	require.NoError(t, err)
	path := filepath.Join(spec.Repository, "pool/main/x/xz-utils/liblzma5_5.6.1+really5.4.5-1ubuntu0.4_amd64.deb")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, archive, 0644))
	s, err := Stage(spec, []Selection{{Name: "liblzma5", Version: "5.6.1+really5.4.5-1ubuntu0.4"}})
	require.NoError(t, err)
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	identity, err := s.Publish(ctx, spec.WorkRoot)
	require.NoError(t, err)
	require.Equal(t, 3, identity.Format)
	require.NotNil(t, identity.Review)
	require.Equal(t, "liblzma5", identity.Review.Changes[0].Name)
	copy := s.ApprovalContent()
	copy.Review.Changes[0].InstalledVersion = "999"
	require.NotEqual(t, copy.Review.Changes, s.ApprovalContent().Review.Changes, "review export must not alias staged approval")
	_, err = OpenApproved(spec.WorkRoot, copy)
	require.ErrorContains(t, err, "differs from persistent snapshot", "review mutation must require new content and approval")
	copy = s.ApprovalContent()
	copy.Review.PreparedAt = time.Now().UTC().Add(-2 * time.Hour)
	copy.Review.ExpiresAt = time.Now().UTC().Add(-time.Hour)
	_, err = Install(ctx, spec.WorkRoot, copy)
	require.Error(t, err, "expired or altered review must never install")
	require.Equal(t, identity.Requested, identity.Resolved)
	require.NoError(t, s.Close(), "closing a published handle must preserve durable content")
	reopened, err := OpenApproved(spec.WorkRoot, identity)
	require.NoError(t, err)
	require.Equal(t, identity, reopened.ApprovalContent())
	require.NoError(t, PublishSelection(spec.WorkRoot, identity))
	cancelled, stop := context.WithCancel(ctx)
	stop()
	require.ErrorIs(t, PublishSelectionContext(cancelled, spec.WorkRoot, identity), context.Canceled)
	require.NoError(t, requirePublishedSelection(spec.WorkRoot, identity.ManifestSHA256, identity), "cancelled publication must retain the previous selection")
	report := PublishedSelection(ctx, spec.WorkRoot)
	require.Equal(t, "ready", report.Status)
	require.Equal(t, identity, report.Plans[0])
	_, err = InstallPublished(ctx, spec.WorkRoot, identity, strings.Repeat("f", 64))
	require.ErrorContains(t, err, "withdrawn or changed", "queued agent-selected jobs must recheck the local selection before any install")
	selectionPath := filepath.Join(spec.WorkRoot, publishedPlanFile)
	require.NoError(t, os.Rename(selectionPath, selectionPath+".withdrawn"))
	_, err = InstallPublished(ctx, spec.WorkRoot, identity, identity.ManifestSHA256)
	require.ErrorContains(t, err, "selection unavailable")
	require.NoError(t, os.Rename(selectionPath+".withdrawn", selectionPath))
	changed := identity
	changed.Resolved = append([]Selection(nil), identity.Resolved...)
	changed.Resolved[0].Version = "999"
	_, err = OpenApproved(spec.WorkRoot, changed)
	require.Error(t, err)
	unlock, err := lockStaging(spec.WorkRoot)
	require.NoError(t, err)
	_, err = Install(ctx, spec.WorkRoot, identity)
	require.ErrorContains(t, err, "another offline")
	require.Equal(t, "unavailable", PublishedSelection(ctx, spec.WorkRoot).Status)
	unlock()
	manifest := filepath.Join(reopened.root, "manifest.json")
	require.NoError(t, os.Chmod(manifest, 0644))
	require.NoError(t, os.WriteFile(manifest, []byte("{}"), 0644))
	_, err = OpenApproved(spec.WorkRoot, identity)
	require.ErrorContains(t, err, "manifest mismatch")
	require.Equal(t, "unavailable", PublishedSelection(ctx, spec.WorkRoot).Status, "changed staged content withdraws the advertised plan")
	t.Log("signed local archive plan durably published and reopened; identity mutation, concurrent operation and manifest tampering rejected; no installation")
}

// Explicit opt-in, network-disabled disposable container only. Never run this
// package-changing case on the enrolled lab or an operator workstation.
func TestNativeOfflineRealInstallation(t *testing.T) {
	if os.Getenv("CONTROLONE_OFFLINE_INSTALL_NATIVE") != "1" {
		t.Skip("network-disabled disposable Ubuntu container required")
	}
	routes, err := os.ReadFile("/proc/net/route")
	require.NoError(t, err)
	for _, line := range strings.Split(string(routes), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 {
			require.NotEqual(t, "00000000", fields[1], "installer qualification must have no default network route")
		}
	}
	root, err := os.MkdirTemp("/tmp", "controlone-offline-real-install-")
	require.NoError(t, err)
	// Disposable container cleanup owns the tree; published content is durable.
	work := filepath.Join(root, "work")
	require.NoError(t, os.Mkdir(work, 0755))
	require.NoError(t, os.Chmod(root, 0755))
	key := "/usr/share/keyrings/ubuntu-archive-keyring.gpg"
	keyBytes, err := os.ReadFile(key)
	require.NoError(t, err)
	before, err := exec.Command("/usr/bin/dpkg-query", "-W", "-f=${Version}", "liblzma5").Output()
	require.NoError(t, err)
	spec := Spec{Repository: "/input/repository", WorkRoot: work, Keyring: key, KeyringSHA256: fmt.Sprintf("%x", sha256.Sum256(keyBytes)), Suite: "noble-updates", Component: "main", Architecture: "amd64"}
	s, err := Stage(spec, []Selection{{Name: "liblzma5", Version: "5.6.1+really5.4.5-1ubuntu0.4"}})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	identity, err := s.Publish(ctx, work)
	require.NoError(t, err)
	out, err := Install(ctx, work, identity)
	require.NoError(t, err, string(out))
	after, err := exec.Command("/usr/bin/dpkg-query", "-W", "-f=${Version}", "liblzma5").Output()
	require.NoError(t, err)
	require.Equal(t, "5.6.1+really5.4.5-1ubuntu0.4", strings.TrimSpace(string(after)))
	t.Logf("real offline package operation: liblzma5 %s -> %s; no network route; exact resolved signed local plan; dpkg audit clean", before, after)
	if string(before) != string(after) {
		_, err = OpenApproved(work, identity)
		require.ErrorContains(t, err, "installed package state changed")
	}
	// A new baseline can be staged after success, but cancellation and missing
	// content must fail without changing the installed package database.
	fresh, err := Stage(spec, []Selection{{Name: "liblzma5", Version: string(after)}})
	require.NoError(t, err)
	identity, err = fresh.Publish(ctx, work)
	require.NoError(t, err)
	status, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	cancelled, stop := context.WithCancel(context.Background())
	stop()
	_, err = Install(cancelled, work, identity)
	require.Error(t, err)
	archive := filepath.Join(fresh.spec.Repository, "pool/main/x/xz-utils/liblzma5_5.6.1+really5.4.5-1ubuntu0.4_amd64.deb")
	require.NoError(t, os.Chmod(filepath.Dir(archive), 0755))
	require.NoError(t, os.Remove(archive))
	_, err = Install(ctx, work, identity)
	require.ErrorContains(t, err, "content changed")
	statusAfter, err := os.ReadFile("/var/lib/dpkg/status")
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(status), sha256.Sum256(statusAfter))
	t.Log("stale-baseline replay, cancellation and missing staged archive rejected; failure cases did not change installed state")
}
