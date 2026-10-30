package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/stretchr/testify/require"
)

func TestOfflinePreparationJournalRecoveryNeverReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prepare-journal.json")
	j, err := openPatchJournal(path)
	require.NoError(t, err)
	ok, err := j.reserve("prepare-fixture", patchsupport.PrepareOfflineJob)
	require.NoError(t, err)
	require.True(t, ok)
	recovered, err := openPatchJournal(path)
	require.NoError(t, err)
	require.Len(t, recovered.pending(), 1)
	ok, err = recovered.reserve("prepare-fixture", patchsupport.PrepareOfflineJob)
	require.NoError(t, err)
	require.False(t, ok)
	require.NoError(t, recovered.acknowledge([]string{"prepare-fixture"}))
	ok, err = recovered.reserve("prepare-fixture", patchsupport.PrepareOfflineJob)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestOfflinePayloadIdentityAndExecutionBlock(t *testing.T) {
	identity := &patchsupport.OfflineContent{Format: 1, ManifestSHA256: strings.Repeat("a", 64), InstalledStatusSHA256: strings.Repeat("b", 64), KeyringSHA256: strings.Repeat("c", 64), Suite: "noble-updates", Component: "main", Architecture: "amd64", Requested: []patchsupport.ExactPackage{{Name: "liblzma5", Version: "1.2.3"}}}
	encoded, err := json.Marshal(map[string]any{"mode": "airgapped", "package_allowlist": []string{"liblzma5"}, "offline_content": identity})
	require.NoError(t, err)
	var detail patchActionDetail
	require.NoError(t, json.Unmarshal(encoded, &detail))
	require.Equal(t, identity, detail.OfflineContent)
	require.NoError(t, detail.OfflineContent.Validate(detail.Mode, detail.PackageAllowlist, detail.PackageDenylist))
	_, _, _, _, ok := patchCommandForJob(patchJobAirgapped, detail)
	require.False(t, ok, "content identity must never enable unqualified installation")
	detail.Mode = "direct"
	require.ErrorContains(t, validatePatchPackagePolicy(detail), "explicit airgapped")
	detail.Mode = "airgapped"
	detail.OfflineContent = nil
	detail.PostPatchRescan = true
	require.ErrorContains(t, validatePatchPackagePolicy(detail), "format-2")
}

func TestOfflineJournalRecoveryNeverReplaysInstallation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "offline-journal.json")
	j, err := openPatchJournal(path)
	require.NoError(t, err)
	reserved, err := j.reserve("offline-interrupted-fixture", patchJobAirgapped)
	require.NoError(t, err)
	require.True(t, reserved)
	recovered, err := openPatchJournal(path)
	require.NoError(t, err)
	results := recovered.pending()
	require.Len(t, results, 1)
	require.Equal(t, "failed", results[0].Status)
	require.Contains(t, results[0].Error, "outcome unknown")
	reserved, err = recovered.reserve("offline-interrupted-fixture", patchJobAirgapped)
	require.NoError(t, err)
	require.False(t, reserved)
	require.NoError(t, recovered.acknowledge([]string{"offline-interrupted-fixture"}))
	reserved, err = recovered.reserve("offline-interrupted-fixture", patchJobAirgapped)
	require.NoError(t, err)
	require.False(t, reserved)
}
