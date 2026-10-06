package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNetworkInventoryWithPostgres(t *testing.T) {
	ctx := context.Background()
	s := setupTargetPostgresStore(t, ctx)
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "inventory-fixture"})
	require.NoError(t, err)
	other, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "isolated"})
	require.NoError(t, err)
	user := uuid.New()
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id,external_id) VALUES($1,$2)`, user, user.String())
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id) SELECT $1,id,$2 FROM roles WHERE name='operator'`, user, tenant.ID)
	require.NoError(t, err)
	read := TargetAccess{UserID: user, Permission: "targets.read"}
	write := TargetAccess{UserID: user, Permission: "targets.write"}
	connect := TargetAccess{UserID: user, Permission: "targets.connect"}
	credential, err := s.CreateNetworkCredential(ctx, CreateProviderCredentialParams{TenantID: tenant.ID, Provider: "network_snmpv3", Name: "sealed fixture", ConfigEncrypted: []byte("test cipher"), Nonce: []byte("test nonce")}, connect)
	require.NoError(t, err)
	receipt, err := s.BeginNetworkConnectionTest(ctx, NetworkConnectionTest{TenantID: tenant.ID, CredentialID: credential.ID, Protocol: "snmpv3", Address: "192.0.2.10", Port: 161}, connect)
	require.NoError(t, err)
	_, err = s.FinishNetworkConnectionTest(ctx, receipt.ID, networkdevice.Outcome("authenticated"))
	require.NoError(t, err)
	target, err := s.SaveNetworkOnboarding(ctx, NetworkSaveParams{TestID: receipt.ID, DisplayName: "inventory fixture", Type: "switch"}, write)
	require.NoError(t, err)
	before, err := s.GetNetworkInventory(ctx, target.ID, read)
	require.NoError(t, err)
	require.Equal(t, "not_collected", before.State)
	connection, err := s.GetNetworkInventoryConnection(ctx, target.ID, connect)
	require.NoError(t, err)
	require.Equal(t, credential.ID, connection.Credential.ID)
	attempt, err := s.BeginNetworkInventory(ctx, target.ID, connect)
	require.NoError(t, err)
	_, err = s.BeginNetworkInventory(ctx, target.ID, connect)
	require.ErrorIs(t, err, ErrInventoryBusy)
	observed := time.Now().UTC().Truncate(time.Microsecond)
	snapshot := networkdevice.Inventory{State: "inventory_ready", ObservedAt: observed, Adapter: "synthetic-test", Facts: map[string]networkdevice.Fact{"hostname": {Value: "fixture-host", Protocol: "snmpv3", Source: "sysName", ObservedAt: observed}}, Interfaces: []networkdevice.InventoryRecord{{ID: "7", Facts: map[string]networkdevice.Fact{"name": {Value: "port7", ObservedAt: observed, Protocol: "snmpv3", Source: "ifName"}}}}}
	saved, err := s.FinishNetworkInventory(ctx, target.ID, attempt.RefreshID, snapshot)
	require.NoError(t, err)
	require.Len(t, saved.Snapshot.Interfaces, 1)
	target, err = s.GetTarget(ctx, target.ID, read)
	require.NoError(t, err)
	require.Nil(t, target.NodeID)
	require.Equal(t, "inventory_ready", target.CollectionState)
	require.Equal(t, observed, target.LastSuccessfulCollectionAt.UTC())
	// Repeated refresh replaces the snapshot, never appends duplicate interfaces.
	attempt, err = s.BeginNetworkInventory(ctx, target.ID, connect)
	require.NoError(t, err)
	saved, err = s.FinishNetworkInventory(ctx, target.ID, attempt.RefreshID, snapshot)
	require.NoError(t, err)
	require.Len(t, saved.Snapshot.Interfaces, 1)
	attempt, err = s.BeginNetworkInventory(ctx, target.ID, connect)
	require.NoError(t, err)
	saved, err = s.FinishNetworkInventory(ctx, target.ID, attempt.RefreshID, networkdevice.Inventory{State: "auth_failed"})
	require.NoError(t, err)
	require.Equal(t, "auth_failed", saved.State)
	require.Equal(t, observed, saved.Snapshot.ObservedAt)
	target, err = s.GetTarget(ctx, target.ID, read)
	require.NoError(t, err)
	require.Equal(t, "stale", target.CollectionState)
	require.Equal(t, observed, target.LastSuccessfulCollectionAt.UTC())
	// Late completion cannot overwrite a newer attempt.
	_, err = s.FinishNetworkInventory(ctx, target.ID, uuid.New(), snapshot)
	require.ErrorIs(t, err, sql.ErrNoRows)
	foreign, err := s.CreateNetworkTarget(ctx, NetworkTargetParams{TenantID: other.ID, Type: "switch", DisplayName: "blocked", ManagementAddresses: []string{"192.0.2.20"}}, write)
	require.Error(t, err)
	require.Nil(t, foreign)
	_, err = s.GetNetworkInventory(ctx, uuid.New(), read)
	require.ErrorIs(t, err, sql.ErrNoRows)
	// Another user with a grant only to the other tenant cannot read this snapshot.
	outsider := uuid.New()
	_, err = s.db.ExecContext(ctx, `INSERT INTO users(id,external_id) VALUES($1,$2)`, outsider, outsider.String())
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id) SELECT $1,id,$2 FROM roles WHERE name='operator'`, outsider, other.ID)
	require.NoError(t, err)
	_, err = s.GetNetworkInventory(ctx, target.ID, TargetAccess{UserID: outsider, Permission: "targets.read"})
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = s.GetNetworkInventoryConnection(ctx, target.ID, TargetAccess{UserID: outsider, Permission: "targets.connect"})
	require.ErrorIs(t, err, sql.ErrNoRows)
	_, err = s.db.ExecContext(ctx, `UPDATE user_roles SET expires_at=NOW()-INTERVAL '1 minute' WHERE user_id=$1`, user)
	require.NoError(t, err)
	_, err = s.GetNetworkInventory(ctx, target.ID, read)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
