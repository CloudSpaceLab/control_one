package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestNetworkOnboardingWithPostgres(t *testing.T) {
	ctx := context.Background()
	s := setupTargetPostgresStore(t, ctx)
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "network-onboarding"})
	require.NoError(t, err)
	other, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "other-tenant"})
	require.NoError(t, err)
	user := uuid.New()
	second := uuid.New()
	for _, id := range []uuid.UUID{user, second} {
		_, err = s.db.ExecContext(ctx, `INSERT INTO users(id,external_id) VALUES($1,$2)`, id, id.String())
		require.NoError(t, err)
		_, err = s.db.ExecContext(ctx, `INSERT INTO user_roles(user_id,role_id,tenant_id) SELECT $1,id,$2 FROM roles WHERE name='operator'`, id, tenant.ID)
		require.NoError(t, err)
	}
	connect := TargetAccess{UserID: user, Permission: "targets.connect"}
	write := TargetAccess{UserID: user, Permission: "targets.write"}
	read := TargetAccess{UserID: user, Permission: "targets.read"}
	sealer, err := secretbox.NewSealer(bytes.Repeat([]byte{1}, 32))
	require.NoError(t, err)
	secret := []byte(`{"username":"readonly","auth_secret":"SENSITIVE_AUTH","priv_secret":"SENSITIVE_PRIV"}`)
	ciphertext, nonce, err := sealer.Seal(secret)
	require.NoError(t, err)
	params := CreateProviderCredentialParams{TenantID: tenant.ID, Provider: "network_snmpv3", Name: "test SNMPv3", ConfigEncrypted: ciphertext, Nonce: nonce}
	credential, err := s.CreateNetworkCredential(ctx, params, connect)
	require.NoError(t, err)
	require.NotContains(t, string(credential.ConfigEncrypted), "SENSITIVE_AUTH")
	plaintext, err := sealer.Open(credential.ConfigEncrypted, credential.Nonce)
	require.NoError(t, err)
	require.Equal(t, secret, plaintext)
	hidden, err := s.GetNetworkCredential(ctx, credential.ID, other.ID, connect)
	require.NoError(t, err)
	require.Nil(t, hidden)
	params.TenantID = other.ID
	_, err = s.CreateNetworkCredential(ctx, params, connect)
	require.ErrorIs(t, err, sql.ErrNoRows)
	attempt := NetworkConnectionTest{TenantID: tenant.ID, CredentialID: credential.ID, Protocol: "snmpv3", Address: "192.0.2.10", Port: 161}
	receipt, err := s.BeginNetworkConnectionTest(ctx, attempt, connect)
	require.NoError(t, err)
	result := networkdevice.Fingerprint("Cisco IOS XE Catalyst C9300-48P", "", networkdevice.Credential{})
	result.Capabilities = []string{"snmp_identity"}
	receipt, err = s.FinishNetworkConnectionTest(ctx, receipt.ID, result)
	require.NoError(t, err)
	p := NetworkSaveParams{TestID: receipt.ID, DisplayName: "Branch switch", Type: "switch", Site: "Lagos", TelemetrySources: []string{"snmp_identity"}}
	_, err = s.SaveNetworkOnboarding(ctx, p, TargetAccess{UserID: second, Permission: "targets.write"})
	require.ErrorIs(t, err, sql.ErrNoRows)
	target, err := s.SaveNetworkOnboarding(ctx, p, write)
	require.NoError(t, err)
	require.Nil(t, target.NodeID)
	require.Equal(t, "network_security", target.Family)
	require.Equal(t, "authenticated", target.CollectionState)
	require.Equal(t, "reachable", target.ReachabilityState)
	require.Equal(t, "Cisco", target.Vendor)
	require.Equal(t, "C9300-48P", target.Model)
	require.Equal(t, "snmpv3", target.Classification.Source)
	require.NotNil(t, target.LastObservedAt)
	require.Nil(t, target.LastSuccessfulCollectionAt)
	repeated, err := s.SaveNetworkOnboarding(ctx, p, write)
	require.NoError(t, err)
	require.Equal(t, target.ID, repeated.ID)
	retrieved, err := s.GetTarget(ctx, target.ID, read)
	require.NoError(t, err)
	encoded, _ := json.Marshal(retrieved)
	require.NotContains(t, string(encoded), "SENSITIVE_AUTH")
	require.NotContains(t, string(encoded), "SENSITIVE_PRIV")
	var storedCredential uuid.UUID
	err = s.db.QueryRowContext(ctx, `SELECT credential_id FROM network_target_connections WHERE target_id=$1`, target.ID).Scan(&storedCredential)
	require.NoError(t, err)
	require.Equal(t, credential.ID, storedCredential)
	for _, state := range []string{"auth_failed", "unreachable", "policy_blocked", "unsupported"} {
		receipt, err = s.BeginNetworkConnectionTest(ctx, attempt, connect)
		require.NoError(t, err)
		_, err = s.FinishNetworkConnectionTest(ctx, receipt.ID, networkdevice.Outcome(state))
		require.NoError(t, err)
		p.TestID = receipt.ID
		_, err = s.SaveNetworkOnboarding(ctx, p, write)
		require.Error(t, err)
	}
	receipt, err = s.BeginNetworkConnectionTest(ctx, attempt, connect)
	require.NoError(t, err)
	unknown := networkdevice.Fingerprint("Unrecognized appliance", "", networkdevice.Credential{})
	unknown.Capabilities = []string{"snmp_identity"}
	_, err = s.FinishNetworkConnectionTest(ctx, receipt.ID, unknown)
	require.NoError(t, err)
	p.TestID = receipt.ID
	p.Type = "firewall"
	overridden, err := s.SaveNetworkOnboarding(ctx, p, write)
	require.NoError(t, err)
	require.Equal(t, "operator_override", overridden.Classification.Source)
	require.Zero(t, overridden.Classification.Confidence)
	require.Equal(t, "firewall", overridden.Type)
	receipt, err = s.BeginNetworkConnectionTest(ctx, attempt, connect)
	require.NoError(t, err)
	_, err = s.FinishNetworkConnectionTest(ctx, receipt.ID, result)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `UPDATE network_connection_tests SET completed_at=$2 WHERE id=$1`, receipt.ID, time.Now().Add(-16*time.Minute))
	require.NoError(t, err)
	p.TestID = receipt.ID
	_, err = s.SaveNetworkOnboarding(ctx, p, write)
	require.Error(t, err)
	_, err = s.db.ExecContext(ctx, `UPDATE user_roles SET expires_at=NOW()-INTERVAL '1 minute' WHERE user_id=$1`, user)
	require.NoError(t, err)
	hidden, err = s.GetNetworkCredential(ctx, credential.ID, tenant.ID, connect)
	require.NoError(t, err)
	require.Nil(t, hidden)
	var nodes, tokens int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes`).Scan(&nodes))
	require.Zero(t, nodes)
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM enrollment_tokens`).Scan(&tokens))
	require.Zero(t, tokens)
}

func TestNetworkOnboardingPolicyPersistsCIDRs(t *testing.T) {
	ctx := context.Background()
	s := setupTargetPostgresStore(t, ctx)
	policy, err := s.GetNetworkOnboardingPolicy(ctx)
	require.NoError(t, err)
	require.Nil(t, policy)

	want := []string{"192.168.56.10/32", "192.168.56.0/24"}
	require.NoError(t, s.UpsertNetworkOnboardingPolicy(ctx, want))
	policy, err = s.GetNetworkOnboardingPolicy(ctx)
	require.NoError(t, err)
	require.NotNil(t, policy)
	require.Equal(t, want, policy.AllowedCIDRs)
	require.False(t, policy.UpdatedAt.IsZero())
}
