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

func TestCalendarWindowDispatchAndForceCloseDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	for _, scenario := range []string{"current", "expired", "changed bounds", "network", "force closed", "other node", "other tenant", "older agent"} {
		t.Run(scenario, func(t *testing.T) {
			runtime := patchsupport.Runtime{OS: "linux", Manager: "apt", CalendarWindowStartCheck: scenario != "older agent"}
			n, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive, Labels: map[string]any{"patch.runtime": patchsupport.Evidence{Runtime: runtime, ObservedAt: time.Now().UTC()}}})
			require.NoError(t, err)
			w, err := s.CreateMaintenanceWindow(ctx, MaintenanceWindow{TenantID: tenant.ID, Name: uuid.NewString(), NodeIDs: []uuid.UUID{n.ID}, OpensAt: time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond), ClosesAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)})
			require.NoError(t, err)
			require.NoError(t, s.MarkMaintenanceWindowOpen(ctx, w.ID, nil))
			bounds := patchsupport.Window{ID: w.ID.String(), OpensAt: w.OpensAt, ClosesAt: w.ClosesAt}
			switch scenario {
			case "expired":
				bounds.ClosesAt = time.Now().Add(-time.Second)
			case "changed bounds":
				_, err = s.db.ExecContext(ctx, `UPDATE maintenance_windows SET closes_at=closes_at+interval '1 minute' WHERE id=$1`, w.ID)
			case "network":
				_, err = s.db.ExecContext(ctx, `UPDATE maintenance_windows SET allow_repos=ARRAY['archive.ubuntu.com'] WHERE id=$1`, w.ID)
			case "force closed":
				err = s.ForceCloseMaintenanceWindow(ctx, w.ID)
			case "other node":
				_, err = s.db.ExecContext(ctx, `UPDATE maintenance_windows SET node_ids='{}' WHERE id=$1`, w.ID)
			case "other tenant":
				_, err = s.db.ExecContext(ctx, `UPDATE maintenance_windows SET tenant_id=$2 WHERE id=$1`, w.ID, uuid.New())
			}
			require.NoError(t, err)
			d, err := s.CreatePatchDeployment(ctx, PatchDeployment{TenantID: tenant.ID, Mode: "direct", TargetNodeCount: 1})
			require.NoError(t, err)
			payload, _ := json.Marshal(map[string]any{"patch_window": bounds, "patch_runtime": runtime})
			_, err = s.DispatchPatchJob(ctx, DispatchPatchJobParams{TenantID: tenant.ID, NodeID: n.ID, DeploymentID: d.ID, Mode: "direct", Payload: payload, Plan: CreateActionPlanParams{State: ActionPlanStateQueued}})
			if scenario == "current" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				var count int
				require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM node_patch_state WHERE deployment_id=$1`, d.ID).Scan(&count))
				require.Zero(t, count)
			}
			if scenario == "network" {
				require.NoError(t, s.ForceCloseMaintenanceWindow(ctx, w.ID))
				saved, err := s.GetMaintenanceWindow(ctx, w.ID)
				require.NoError(t, err)
				require.Equal(t, "closing", saved.Status)
				require.NotNil(t, saved.ForceClosedAt)
				require.Error(t, s.MarkMaintenanceWindowClosed(ctx, w.ID), "network closure cannot be inferred from a database update")
			}
			if scenario == "force closed" {
				require.Error(t, s.MarkMaintenanceWindowOpen(ctx, w.ID, nil), "a delayed open must not undo revocation")
			}
		})
	}
}
