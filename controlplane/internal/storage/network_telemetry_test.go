package storage

import (
	"context"
	"database/sql"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestNetworkTelemetryWithPostgres(t *testing.T) {
	ctx := context.Background()
	s := setupTargetPostgresStore(t, ctx)
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "network sources"})
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
	target, err := s.CreateNetworkTarget(ctx, NetworkTargetParams{TenantID: tenant.ID, Type: "switch", DisplayName: "fixture", ManagementAddresses: []string{"192.0.2.10"}}, write)
	require.NoError(t, err)
	_, err = s.UpsertContentPackEdgeCollectorRegistration(ctx, UpsertContentPackEdgeCollectorRegistrationParams{TenantID: tenant.ID, CollectorID: "edge-site", Kind: "otel"})
	require.NoError(t, err)
	p := NetworkSourceConfig{SourceType: "syslog", CollectorID: "edge-site", Site: "Lagos", SenderAddress: "192.0.2.10", StaleAfterSeconds: 300}
	require.NoError(t, s.ConfigureNetworkSource(ctx, target.ID, p, write))
	require.ErrorIs(t, s.ConfigureNetworkSource(ctx, target.ID, p, read), sql.ErrNoRows)
	rows, err := s.ListNetworkSources(ctx, target.ID, read)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	id := rows[0].ID
	contact := time.Now().UTC().Add(-time.Minute)
	observed := time.Now().UTC()
	r := networkdevice.SourceReport{BindingID: id.String(), State: "ready", ObservedAt: observed, LastContactAt: &contact}
	require.ErrorIs(t, s.ReportNetworkSources(ctx, other.ID, "edge-site", []networkdevice.SourceReport{r}), sql.ErrNoRows)
	require.ErrorIs(t, s.ReportNetworkSources(ctx, tenant.ID, "other-collector", []networkdevice.SourceReport{r}), sql.ErrNoRows)
	require.NoError(t, s.ReportNetworkSources(ctx, tenant.ID, "edge-site", []networkdevice.SourceReport{r}))
	r.State = "unreachable"
	r.ObservedAt = observed.Add(-time.Second)
	r.LastContactAt = nil
	require.NoError(t, s.ReportNetworkSources(ctx, tenant.ID, "edge-site", []networkdevice.SourceReport{r}))
	rows, err = s.ListNetworkSources(ctx, target.ID, read)
	require.NoError(t, err)
	require.Equal(t, "ready", rows[0].State)
	resolved, err := s.ResolveNetworkSyslogSource(ctx, tenant.ID, "edge-site", "192.0.2.10")
	require.NoError(t, err)
	require.Equal(t, target.ID, resolved.TargetID)
	_, err = s.ResolveNetworkSyslogSource(ctx, other.ID, "edge-site", "192.0.2.10")
	require.ErrorIs(t, err, sql.ErrNoRows)
	p.SourceType = "snmp_poll"
	p.SenderAddress = ""
	require.NoError(t, s.ConfigureNetworkSource(ctx, target.ID, p, write))
	rows, err = s.ListNetworkSources(ctx, target.ID, read)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "not_configured", rows[0].State)
	require.Equal(t, "ready", rows[1].State)
	after, err := s.GetTarget(ctx, target.ID, read)
	require.NoError(t, err)
	require.Equal(t, target.ReachabilityState, after.ReachabilityState)
	require.Nil(t, after.NodeID)
	p.SourceType = "syslog"
	p.SenderAddress = "192.0.2.10"
	require.NoError(t, s.ConfigureNetworkSource(ctx, target.ID, p, write))
	r.State = "ready"
	r.ObservedAt = time.Now().UTC()
	r.LastContactAt = &contact
	require.ErrorIs(t, s.ReportNetworkSources(ctx, tenant.ID, "edge-site", []networkdevice.SourceReport{r}), sql.ErrNoRows)
	for _, sourceType := range []string{"snmp_trap", "netflow", "ipfix", "sflow", "ssh_config", "netconf", "restconf", "vendor_api"} {
		p.SourceType = sourceType
		p.SenderAddress = ""
		if networkdevice.IsReceiverSource(sourceType) {
			p.SenderAddress = "192.0.2.10"
		}
		require.NoError(t, s.ConfigureNetworkSource(ctx, target.ID, p, write))
		if networkdevice.IsReceiverSource(sourceType) {
			bound, err := s.ResolveNetworkReceiverSource(ctx, tenant.ID, "edge-site", sourceType, "192.0.2.10")
			require.NoError(t, err)
			require.Equal(t, target.ID, bound.TargetID)
			require.Equal(t, sourceType, bound.SourceType)
			_, err = s.ResolveNetworkReceiverSource(ctx, other.ID, "edge-site", sourceType, "192.0.2.10")
			require.ErrorIs(t, err, sql.ErrNoRows)
		}
	}
	rows, err = s.ListNetworkSources(ctx, target.ID, read)
	require.NoError(t, err)
	require.Len(t, rows, 10)
	_, err = s.db.ExecContext(ctx, `UPDATE user_roles SET expires_at=NOW()-INTERVAL '1 second' WHERE user_id=$1`, user)
	require.NoError(t, err)
	require.ErrorIs(t, s.ConfigureNetworkSource(ctx, target.ID, p, write), sql.ErrNoRows)
	_, err = s.ListNetworkSources(ctx, target.ID, read)
	require.ErrorIs(t, err, sql.ErrNoRows)
}
