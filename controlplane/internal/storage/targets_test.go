package storage

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"
)

func TestNetworkTargetValidation(t *testing.T) {
	tenant := uuid.New()
	valid := func() NetworkTargetParams {
		return NetworkTargetParams{TenantID: tenant, Type: "switch", DisplayName: "Branch switch", ManagementAddresses: []string{"192.0.2.1"}}
	}
	for _, tc := range []struct {
		name   string
		change func(*NetworkTargetParams)
	}{
		{"missing tenant", func(p *NetworkTargetParams) { p.TenantID = uuid.Nil }},
		{"compute type", func(p *NetworkTargetParams) { p.Type = "server" }},
		{"missing display name", func(p *NetworkTargetParams) { p.DisplayName = " " }},
		{"no address", func(p *NetworkTargetParams) { p.ManagementAddresses = nil }},
		{"URL", func(p *NetworkTargetParams) { p.ManagementAddresses = []string{"https://switch.example"} }},
		{"credential URL", func(p *NetworkTargetParams) { p.ManagementAddresses = []string{"user:secret@switch.example"} }},
		{"command", func(p *NetworkTargetParams) { p.ManagementAddresses = []string{"switch; reboot"} }},
		{"invalid DNS", func(p *NetworkTargetParams) { p.ManagementAddresses = []string{"-switch.example"} }},
		{"multiline", func(p *NetworkTargetParams) { p.Site = "a\nb" }},
	} {
		t.Run(tc.name, func(t *testing.T) { p := valid(); tc.change(&p); require.Error(t, p.Validate()) })
	}
	p := valid()
	p.ManagementAddresses = []string{" SWITCH.Example. ", "switch.example", "2001:0db8::1", "2001:db8::1"}
	require.NoError(t, p.Validate())
	require.Equal(t, []string{"switch.example", "2001:db8::1"}, p.ManagementAddresses)
	for _, typ := range []string{"router", "switch", "firewall", "load_balancer", "waf", "vpn_gateway", "wireless_controller", "access_point", "ids_ips", "network_appliance"} {
		p := valid()
		p.Type = typ
		require.NoError(t, p.Validate())
	}
}

func TestTargetFilterRequiresAccessAndEscapesSearch(t *testing.T) {
	user, tenant := uuid.New(), uuid.New()
	_, _, err := targetWhere(TargetFilter{Limit: 25}, TargetAccess{})
	require.Error(t, err)
	for _, f := range []TargetFilter{{Limit: 0}, {Limit: 501}, {Limit: 25, Offset: -1}} {
		_, _, err := targetWhere(f, TargetAccess{UserID: user, Permission: "targets.read"})
		require.Error(t, err)
	}
	where, args, err := targetWhere(TargetFilter{Limit: 25, TenantID: tenant, Family: "network_security", Site: "branch", Search: "_%' OR TRUE --"}, TargetAccess{UserID: user, Permission: "targets.read"})
	require.NoError(t, err)
	require.NotContains(t, where, "OR TRUE --")
	require.Contains(t, where, "ur.tenant_id IS NULL OR ur.tenant_id = t.tenant_id")
	require.Contains(t, where, "ur.expires_at > NOW()")
	require.Equal(t, user, args[0])
	require.Equal(t, tenant, args[2])
	require.Equal(t, `%\_\%' or true --%`, args[len(args)-1])
}

// Integration coverage exercises the actual constraints, node triggers,
// permission scope, history, and exact counts rather than a mock SQL engine.
func TestTargetIdentityWithPostgres(t *testing.T) {
	ctx := context.Background()
	s := setupTargetPostgresStore(t, ctx)
	a, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "network-a"})
	require.NoError(t, err)
	b, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "network-b"})
	require.NoError(t, err)
	user := uuid.New()
	_, err = s.db.ExecContext(ctx, "INSERT INTO users(id, external_id) VALUES ($1, $2)", user, "target-user")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_roles(user_id, role_id, tenant_id) SELECT $1, id, $2 FROM roles WHERE name = 'operator'`, user, a.ID)
	require.NoError(t, err)
	read := TargetAccess{UserID: user, Permission: "targets.read"}
	write := TargetAccess{UserID: user, Permission: "targets.write"}
	p := NetworkTargetParams{TenantID: a.ID, Type: "switch", DisplayName: "branch-switch", Site: "branch", ManagementAddresses: []string{"192.0.2.10"}}
	network, err := s.CreateNetworkTarget(ctx, p, write)
	require.NoError(t, err)
	require.Nil(t, network.NodeID)
	require.Equal(t, "discovered", network.CollectionState)
	require.Equal(t, "unknown", network.ReachabilityState)
	require.Empty(t, network.Capabilities)
	require.Nil(t, network.LastObservedAt)
	require.Nil(t, network.LastSuccessfulCollectionAt)
	require.Equal(t, "management", network.Addresses[0].Purpose)
	require.Equal(t, "operator", network.Classification.Source)
	p.TenantID = b.ID
	_, err = s.CreateNetworkTarget(ctx, p, write)
	require.ErrorIs(t, err, sql.ErrNoRows)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: a.ID, Hostname: "branch-server", PublicIP: sql.NullString{String: "192.0.2.20", Valid: true}, Labels: map[string]any{"target.type": "server", "target.type_source": "agent", "target.classification_confidence": 85, "target.classification_evidence": []string{"server OS"}}})
	require.NoError(t, err)
	target, err := s.GetTarget(ctx, node.ID, read)
	require.NoError(t, err)
	require.NotNil(t, target)
	require.Equal(t, node.ID, *target.NodeID)
	require.Equal(t, "managed_compute", target.Family)
	require.Equal(t, "server", target.Type)
	require.Equal(t, 85, target.Classification.Confidence)
	require.Equal(t, []string{"server OS"}, target.Classification.Evidence)
	require.Equal(t, "observed", target.Addresses[0].Purpose)
	// Storage evidence writes preserve the existing state machine: the server
	// owns activation, rather than the target synchronization trigger.
	heartbeat, err := s.TouchNodeHeartbeat(ctx, node.ID)
	require.NoError(t, err)
	require.Equal(t, NodeStateEnrollmentPending, heartbeat.State)
	scanned, err := s.MarkNodeFirstScan(ctx, node.ID)
	require.NoError(t, err)
	require.NotNil(t, scanned.FirstScanAt)
	require.Equal(t, NodeStateEnrollmentPending, scanned.State)
	require.NoError(t, s.SetNodeState(ctx, node.ID, NodeStateActive))
	target, err = s.GetTarget(ctx, node.ID, read)
	require.NoError(t, err)
	require.NotNil(t, target.LastObservedAt)
	require.Equal(t, "discovered", target.CollectionState)
	require.NoError(t, s.UpdateNodeLabels(ctx, node.ID, map[string]any{"agent.capabilities": []any{"logs", "logs", "inventory", 42}}))
	target, err = s.GetTarget(ctx, node.ID, read)
	require.NoError(t, err)
	require.Equal(t, []string{"inventory", "logs"}, target.Capabilities)
	other, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: b.ID, Hostname: "hidden-server"})
	require.NoError(t, err)
	hidden, err := s.GetTarget(ctx, other.ID, read)
	require.NoError(t, err)
	require.Nil(t, hidden)
	targets, total, err := s.ListTargets(ctx, TargetFilter{Limit: 1}, read)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, targets, 1)
	targets, total, err = s.ListTargets(ctx, TargetFilter{Limit: 1, Offset: 100}, read)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Empty(t, targets)
	targets, total, err = s.ListTargets(ctx, TargetFilter{Limit: 25, Family: "network_security", Site: "branch", Search: "192.0.2.10"}, read)
	require.NoError(t, err)
	require.Equal(t, 1, total)
	require.Equal(t, network.ID, targets[0].ID)
	_, total, err = s.ListTargets(ctx, TargetFilter{Limit: 25, TenantID: b.ID}, read)
	require.NoError(t, err)
	require.Zero(t, total)
	// The same user has an expired grant in B; it must not expand All tenants.
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_roles(user_id, role_id, tenant_id, expires_at) SELECT $1, id, $2, $3 FROM roles WHERE name = 'viewer'`, user, b.ID, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	_, total, err = s.ListTargets(ctx, TargetFilter{Limit: 25}, read)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	// A real global read grant gives exact cross-tenant totals.
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_roles(user_id, role_id) SELECT $1, id FROM roles WHERE name = 'viewer'`, user)
	require.NoError(t, err)
	_, total, err = s.ListTargets(ctx, TargetFilter{Limit: 1}, read)
	require.NoError(t, err)
	require.Equal(t, 3, total)
	_, err = s.db.ExecContext(ctx, `UPDATE nodes SET public_ip = '192.0.2.21' WHERE id = $1`, node.ID)
	require.NoError(t, err)
	target, err = s.GetTarget(ctx, node.ID, read)
	require.NoError(t, err)
	require.Len(t, target.Addresses, 2)
	for _, address := range target.Addresses {
		require.Equal(t, address.Address == "192.0.2.21", address.Current)
	}
	require.NoError(t, s.RetireNode(ctx, node.ID))
	target, err = s.GetTarget(ctx, node.ID, read)
	require.NoError(t, err)
	require.Equal(t, "retired", target.LifecycleState)
	require.NoError(t, s.DeleteNode(ctx, node.ID))
	target, err = s.GetTarget(ctx, node.ID, read)
	require.NoError(t, err)
	require.NotNil(t, target)
	require.Nil(t, target.NodeID)
	// Foreign keys prevent a node from being attached across tenant boundaries.
	_, err = s.db.ExecContext(ctx, `UPDATE targets SET node_id = $1 WHERE id = $2`, other.ID, network.ID)
	require.Error(t, err)
	// Malformed legacy labels must not break agent writes.
	require.NoError(t, s.UpdateNodeLabels(ctx, other.ID, map[string]any{"target.type": "switch", "target.classification_confidence": "bad", "target.classification_evidence": []any{"fact", 42}}))
	target, err = s.GetTarget(ctx, other.ID, read)
	require.NoError(t, err)
	require.Equal(t, "unknown", target.Type)
	require.Equal(t, []string{"fact"}, target.Classification.Evidence)
	// No credential fields exist on estate identity tables.
	var cols string
	err = s.db.QueryRowContext(ctx, `SELECT string_agg(column_name, ',') FROM information_schema.columns WHERE table_name IN ('targets', 'target_addresses')`).Scan(&cols)
	require.NoError(t, err)
	require.False(t, strings.Contains(cols, "password"))
	// Exercise rollback and backfill with a node created before target identity.
	inventoryDown, err := os.ReadFile("../migrate/sql/0160_network_inventory.down.sql")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, string(inventoryDown))
	require.NoError(t, err)
	onboardingDown, err := os.ReadFile("../migrate/sql/0159_network_onboarding.down.sql")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, string(onboardingDown))
	require.NoError(t, err)
	down, err := os.ReadFile("../migrate/sql/0158_target_identity.down.sql")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, string(down))
	require.NoError(t, err)
	legacy, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: a.ID, Hostname: "pre-migration", Labels: map[string]any{"target.type": "workstation"}})
	require.NoError(t, err)
	up, err := os.ReadFile("../migrate/sql/0158_target_identity.up.sql")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, string(up))
	require.NoError(t, err)
	target, err = s.GetTarget(ctx, legacy.ID, read)
	require.NoError(t, err)
	require.NotNil(t, target)
	require.Equal(t, "workstation", target.Type)
	require.Equal(t, legacy.ID, *target.NodeID)
	onboardingUp, err := os.ReadFile("../migrate/sql/0159_network_onboarding.up.sql")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, string(onboardingUp))
	require.NoError(t, err)
	inventoryUp, err := os.ReadFile("../migrate/sql/0160_network_inventory.up.sql")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, string(inventoryUp))
	require.NoError(t, err)
}

// Checking registry credentials is not a Docker availability check: public
// postgres images require no login. Ping the daemon, then run the real fixture.
func setupTargetPostgresStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping PostgreSQL integration test in short mode")
	}
	client, err := testcontainers.NewDockerClientWithOpts(ctx)
	if err != nil {
		t.Skipf("Docker unavailable: %v", err)
	}
	defer func() { _ = client.Close() }()
	if _, err := client.Ping(ctx); err != nil {
		t.Skipf("Docker unavailable: %v", err)
	}
	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine", postgres.WithDatabase("targets"),
		postgres.WithUsername("postgres"), postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(time.Minute)))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(context.Background())) })
	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: dsn}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	applyCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	require.NoError(t, migrate.Apply(applyCtx, store.DB()))
	return store
}
