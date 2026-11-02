package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func agentOnlineFixture() patchActionDetail {
	now := time.Now().UTC()
	node := uuid.NewString()
	p := patchsupport.OnlinePlan{Version: 1, NodeID: node, Mode: "direct", TransportContext: strings.Repeat("a", 64), InstalledStateSHA256: strings.Repeat("b", 64), InputsSHA256: strings.Repeat("c", 64), ManifestSHA256: strings.Repeat("d", 64), Requested: []string{"liba"}, Resolved: []patchsupport.ExactPackage{{Name: "liba", Version: "2"}}, Review: patchsupport.OfflineReview{PreparedAt: now.Add(-time.Minute), ExpiresAt: now.Add(59 * time.Minute), Reboot: "unknown", ServiceImpact: "requires_operator_review", Changes: []patchsupport.PlanChange{{Name: "liba", InstalledVersion: "1", ProposedVersion: "2", Role: "requested"}}}}
	p.SHA256 = p.Digest()
	return patchActionDetail{OnlinePlan: &p, NodeID: node, Mode: "direct", PackageAllowlist: p.Requested, PostPatchRescan: true}
}

func TestOnlineAgentRejectsUnboundExecution(t *testing.T) {
	for _, scenario := range []string{"valid", "node", "mode", "proxy", "scope", "deny", "expiry", "rescan", "content"} {
		t.Run(scenario, func(t *testing.T) {
			d := agentOnlineFixture()
			switch scenario {
			case "node":
				d.NodeID = uuid.NewString()
			case "mode":
				d.Mode = "proxy"
			case "proxy":
				d.ProxyURL = "http://different:3128"
			case "scope":
				d.PackageAllowlist = []string{"other"}
			case "deny":
				d.PackageDenylist = []string{"other"}
			case "expiry":
				d.OnlinePlan.Review.ExpiresAt = time.Now().Add(-time.Second)
				d.OnlinePlan.SHA256 = d.OnlinePlan.Digest()
			case "rescan":
				d.PostPatchRescan = false
			case "content":
				d.OnlinePlan.ManifestSHA256 = strings.Repeat("e", 64)
			}
			err := validatePatchPackagePolicy(d)
			if scenario == "valid" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestOnlinePreparationRecoveryNeverReplays(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	body, _ := json.Marshal(map[string]patchJournalEntry{"reserved-online": {Action: patchsupport.PrepareOnlineJob}})
	require.NoError(t, os.WriteFile(path, body, 0600))
	j, err := openPatchJournal(path)
	require.NoError(t, err)
	reserved, err := j.reserve("reserved-online", patchsupport.PrepareOnlineJob)
	require.NoError(t, err)
	require.False(t, reserved)
	require.Contains(t, j.entries["reserved-online"].Result.Error, "preparation interrupted")
	require.False(t, j.entries["reserved-online"].Acknowledged)
}
