package server

import (
	"context"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPatchRuntimeBlocksBeforeApprovalAndDispatch(t *testing.T) {
	for _, scenario := range []string{"missing", "expired", "future", "macos", "windows_proxy", "offline"} {
		t.Run(scenario, func(t *testing.T) {
			tenant, nodeID := uuid.New(), uuid.New()
			store := newPatchTestStore(tenant, nodeID)
			srv := newPatchTestServer(store)
			r := patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt"}, ObservedAt: time.Now().UTC()}
			mode := "direct"
			switch scenario {
			case "expired":
				r.ObservedAt = time.Now().Add(-6 * time.Minute)
			case "future":
				r.ObservedAt = time.Now().Add(time.Minute)
			case "macos":
				r.OS = "darwin"
			case "windows_proxy":
				r.OS = "windows"
				r.Manager = "winget"
				mode = "proxy"
			case "offline":
				mode = "airgapped"
			}
			store.nodes[0].Labels["patch.runtime"] = r
			if scenario == "missing" {
				delete(store.nodes[0].Labels, "patch.runtime")
			}
			gate := srv.runPatchSafetyGates(context.Background(), tenant, nodeID, uuid.New(), mode, nil, nil)
			require.False(t, gate.Allowed)
			require.False(t, gate.AwaitingApproval)
			require.NotEmpty(t, gate.Reason)
			_, err := srv.dispatchPatchModeToNode(context.Background(), tenant, uuid.New(), nodeID, mode, nil, nil, patchPackagePolicy{})
			require.Error(t, err)
			require.Empty(t, store.states)
			require.Empty(t, store.jobs)
		})
	}
}

func TestPatchRuntimeBindsApprovalIdentityNotHeartbeatTime(t *testing.T) {
	tenant, nodeID := uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, nodeID)
	srv := newPatchTestServer(store)
	ctx := context.Background()
	first, _, err := srv.patchApprovalPolicyHash(ctx, tenant, nodeID, "direct", nil, nil, patchPackagePolicy{})
	require.NoError(t, err)
	store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt"}, ObservedAt: time.Now().UTC()}
	second, _, err := srv.patchApprovalPolicyHash(ctx, tenant, nodeID, "direct", nil, nil, patchPackagePolicy{})
	require.NoError(t, err)
	require.Equal(t, first, second)
	store.nodes[0].Labels["patch.runtime"] = patchRuntimeEvidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "dnf"}, ObservedAt: time.Now().UTC()}
	third, _, err := srv.patchApprovalPolicyHash(ctx, tenant, nodeID, "direct", nil, nil, patchPackagePolicy{})
	require.NoError(t, err)
	require.NotEqual(t, first, third)
	require.Error(t, validateRuntimePackageScope(patchsupport.Runtime{OS: "windows", Manager: "winget"}, patchPackagePolicy{Allowlist: []string{"demo"}}))
}

func TestHeartbeatInterfaceSnapshotRefreshesAndReplacesAddresses(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-time.Minute).Format(time.RFC3339)
	existing := []any{map[string]any{"kind": "private_ip", "value": "172.19.0.3", "source": "agent_interface_snapshot", "first_seen_at": old, "last_seen_at": old, "confidence": 80}, map[string]any{"kind": "private_ip", "value": "172.19.0.2", "source": "agent_interface_snapshot", "first_seen_at": old, "last_seen_at": old, "confidence": 80}}
	rows := mergeHeartbeatNetworkObservations(existing, []heartbeatNetworkObservation{{Kind: "private_ip", Value: "172.19.0.2", Source: "agent_interface_snapshot", Confidence: 80}}, now)
	require.Len(t, rows, 1)
	require.Equal(t, "172.19.0.2", rows[0]["value"])
	require.Equal(t, old, rows[0]["first_seen_at"])
	require.Equal(t, now.Format(time.RFC3339), rows[0]["last_seen_at"])
}

func TestOlderAgentCannotAcceptCalendarWindow(t *testing.T) {
	tenant, nodeID, windowID := uuid.New(), uuid.New(), uuid.New()
	store := newPatchTestStore(tenant, nodeID)
	srv := newPatchTestServer(store)
	gate := srv.runPatchSafetyGates(context.Background(), tenant, nodeID, uuid.New(), "direct", nil, &windowID)
	require.False(t, gate.Allowed)
	require.False(t, gate.AwaitingApproval)
	require.Contains(t, gate.Reason, "calendar-window enforcement unavailable")
	_, err := srv.dispatchPatchModeToNode(context.Background(), tenant, uuid.New(), nodeID, "direct", nil, &windowID, patchPackagePolicy{})
	require.ErrorContains(t, err, "calendar-window enforcement unavailable")
	require.Empty(t, store.states)
	require.Empty(t, store.jobs)
}
