package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPatchRuntimeRevalidationDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	for _, scenario := range []string{"current", "changed", "expired", "future", "missing", "offline", "offline_legacy_without_runtime", "changes_while_waiting"} {
		t.Run(scenario, func(t *testing.T) {
			evidence := patchsupport.Evidence{Runtime: patchsupport.Runtime{OS: "linux", Manager: "apt"}, ObservedAt: time.Now().UTC()}
			payload, _ := json.Marshal(map[string]any{"patch_runtime": evidence.Runtime, "package_allowlist": []string{"fixture-package"}})
			switch scenario {
			case "changed":
				evidence.Manager = "dnf"
			case "expired":
				evidence.ObservedAt = time.Now().Add(-6 * time.Minute)
			case "future":
				evidence.ObservedAt = time.Now().Add(time.Hour)
			}
			labels := map[string]any{"patch.runtime": evidence}
			if scenario == "missing" {
				labels = nil
			}
			n, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: labels})
			require.NoError(t, err)
			d, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1})
			require.NoError(t, err)
			p := DispatchPatchJobParams{TenantID: tenant.ID, NodeID: n.ID, DeploymentID: d.ID, Mode: "direct", Payload: payload, Plan: CreateActionPlanParams{State: ActionPlanStateQueued}}
			if scenario == "offline" || scenario == "offline_legacy_without_runtime" {
				p.Mode = "airgapped"
			}
			if scenario == "offline_legacy_without_runtime" {
				p.Payload = json.RawMessage(`{"staged_repo_path":"/tmp/legacy"}`)
			}
			if scenario == "changes_while_waiting" {
				tx, err := s.db.BeginTx(ctx, nil)
				require.NoError(t, err)
				defer tx.Rollback()
				_, err = tx.ExecContext(ctx, `SELECT id FROM nodes WHERE id=$1 FOR UPDATE`, n.ID)
				require.NoError(t, err)
				result := make(chan error, 1)
				go func() { _, err := s.DispatchPatchJob(ctx, p); result <- err }()
				evidence.Manager = "dnf"
				raw, _ := json.Marshal(map[string]any{"patch.runtime": evidence})
				_, err = tx.ExecContext(ctx, `UPDATE nodes SET labels=$2 WHERE id=$1`, n.ID, raw)
				require.NoError(t, err)
				require.NoError(t, tx.Commit())
				select {
				case err = <-result:
					require.Error(t, err)
				case <-time.After(5 * time.Second):
					t.Fatal("dispatch did not return after runtime change")
				}
			} else {
				_, err = s.DispatchPatchJob(ctx, p)
				if scenario == "current" {
					require.NoError(t, err)
					return
				}
				require.Error(t, err)
			}
			var count int
			require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_patch_state WHERE deployment_id=$1`, d.ID).Scan(&count))
			require.Zero(t, count, "rejected runtime must not partially publish execution")
		})
	}
}
