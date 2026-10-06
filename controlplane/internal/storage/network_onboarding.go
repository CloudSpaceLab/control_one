package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/networkdevice"
	"github.com/google/uuid"
)

type NetworkConnectionTest struct {
	ID           uuid.UUID            `json:"id"`
	TenantID     uuid.UUID            `json:"tenant_id"`
	UserID       uuid.UUID            `json:"-"`
	CredentialID uuid.UUID            `json:"credential_id"`
	Protocol     string               `json:"protocol"`
	Address      string               `json:"address"`
	Port         int                  `json:"port"`
	State        string               `json:"state"`
	Result       networkdevice.Result `json:"result"`
	CreatedAt    time.Time            `json:"created_at"`
	CompletedAt  *time.Time           `json:"completed_at,omitempty"`
	TargetID     *uuid.UUID           `json:"target_id,omitempty"`
}

type NetworkSaveParams struct {
	TestID           uuid.UUID `json:"test_id"`
	DisplayName      string    `json:"display_name"`
	Type             string    `json:"type"`
	Site             string    `json:"site"`
	Group            string    `json:"group"`
	TelemetrySources []string  `json:"telemetry_sources"`
}

func (s *Store) CreateNetworkCredential(ctx context.Context, p CreateProviderCredentialParams, a TargetAccess) (*ProviderCredential, error) {
	if a.Permission != "targets.connect" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	if p.Provider != "network_snmpv3" && p.Provider != "network_ssh" {
		return nil, errors.New("unsupported network credential")
	}
	if len(p.ConfigEncrypted) == 0 || len(p.Nonce) == 0 {
		return nil, errors.New("encrypted credential required")
	}
	row := s.db.QueryRowContext(ctx, `INSERT INTO provider_credentials(id,tenant_id,provider,name,config_encrypted,nonce)
 SELECT $1,t.id,$3,$4,$5,$6 FROM tenants t WHERE t.id=$2 AND `+targetAccessPredicate("t.id", 7, 8)+`
 RETURNING id,tenant_id,provider,name,config_encrypted,nonce,created_at,updated_at,rotated_at`, uuid.New(), p.TenantID, p.Provider, p.Name, p.ConfigEncrypted, p.Nonce, a.UserID, a.Permission)
	result, err := scanProviderCredentialRow(row)
	if result == nil && err == nil {
		return nil, sql.ErrNoRows
	}
	return result, err
}

func (s *Store) GetNetworkCredential(ctx context.Context, id, tenant uuid.UUID, a TargetAccess) (*ProviderCredential, error) {
	if a.Permission != "targets.connect" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	return scanProviderCredentialRow(s.db.QueryRowContext(ctx, `SELECT id,tenant_id,provider,name,config_encrypted,nonce,created_at,updated_at,rotated_at
 FROM provider_credentials c WHERE c.id=$1 AND c.tenant_id=$2 AND c.provider IN ('network_snmpv3','network_ssh') AND `+targetAccessPredicate("c.tenant_id", 3, 4), id, tenant, a.UserID, a.Permission))
}

func (s *Store) BeginNetworkConnectionTest(ctx context.Context, p NetworkConnectionTest, a TargetAccess) (*NetworkConnectionTest, error) {
	if a.Permission != "targets.connect" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	p.ID = uuid.New()
	p.UserID = a.UserID
	p.CreatedAt = s.clock().UTC()
	p.State = "testing"
	err := s.db.QueryRowContext(ctx, `INSERT INTO network_connection_tests(id,tenant_id,user_id,credential_id,protocol,address,port,state,created_at)
 SELECT $1,c.tenant_id,$3,c.id,$5,$6,$7,'testing',$8 FROM provider_credentials c
 WHERE c.id=$4 AND c.tenant_id=$2 AND c.provider=$9 AND `+targetAccessPredicate("c.tenant_id", 3, 10)+` RETURNING id`, p.ID, p.TenantID, p.UserID, p.CredentialID, p.Protocol, p.Address, p.Port, p.CreatedAt, "network_"+p.Protocol, a.Permission).Scan(&p.ID)
	return &p, err
}

func (s *Store) FinishNetworkConnectionTest(ctx context.Context, id uuid.UUID, result networkdevice.Result) (*NetworkConnectionTest, error) {
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `UPDATE network_connection_tests SET state=$2,result=$3,completed_at=$4 WHERE id=$1 AND state='testing'
 RETURNING id,tenant_id,user_id,credential_id,protocol,address,port,state,result,created_at,completed_at,target_id`, id, result.State, raw, s.clock().UTC())
	return scanNetworkTest(row)
}

func scanNetworkTest(row rowScanner) (*NetworkConnectionTest, error) {
	var result NetworkConnectionTest
	var raw []byte
	err := row.Scan(&result.ID, &result.TenantID, &result.UserID, &result.CredentialID, &result.Protocol, &result.Address, &result.Port, &result.State, &raw, &result.CreatedAt, &result.CompletedAt, &result.TargetID)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(raw, &result.Result)
	return &result, err
}

func (s *Store) SaveNetworkOnboarding(ctx context.Context, p NetworkSaveParams, a TargetAccess) (*Target, error) {
	if a.Permission != "targets.write" || a.UserID == uuid.Nil {
		return nil, sql.ErrNoRows
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	receipt, err := scanNetworkTest(tx.QueryRowContext(ctx, `SELECT id,tenant_id,user_id,credential_id,protocol,address,port,state,result,created_at,completed_at,target_id
 FROM network_connection_tests r WHERE r.id=$1 AND r.user_id=$2 AND `+targetAccessPredicate("r.tenant_id", 2, 3)+` FOR UPDATE`, p.TestID, a.UserID, a.Permission))
	if err != nil {
		return nil, err
	}
	if receipt.TargetID != nil {
		target, err := scanTarget(tx.QueryRowContext(ctx, targetSelect+" WHERE t.id=$1", *receipt.TargetID))
		if err != nil {
			return nil, err
		}
		return target, tx.Commit()
	}
	if receipt.State != "authenticated" || receipt.CompletedAt == nil || s.clock().Sub(*receipt.CompletedAt) > 15*time.Minute {
		return nil, errors.New("a successful connection test from the last 15 minutes is required")
	}
	identity := NetworkTargetParams{TenantID: receipt.TenantID, Type: p.Type, DisplayName: p.DisplayName, Site: p.Site, Group: p.Group, ManagementAddresses: []string{receipt.Address}}
	if err = identity.Validate(); err != nil {
		return nil, err
	}
	sources := make([]string, 0, len(p.TelemetrySources))
	allowedSource := "ssh_identity"
	if receipt.Protocol == "snmpv3" {
		allowedSource = "snmp_identity"
	}
	if len(p.TelemetrySources) > 1 {
		return nil, errors.New("unsupported telemetry source")
	}
	for _, source := range p.TelemetrySources {
		if source != allowedSource {
			return nil, errors.New("unsupported telemetry source")
		}
		sources = append(sources, source)
	}
	target, err := createNetworkTargetTx(ctx, tx, identity, a, s.clock().UTC())
	if err != nil {
		return nil, err
	}
	classification := TargetClassification{Source: receipt.Protocol, Confidence: receipt.Result.Confidence, Evidence: receipt.Result.Evidence}
	if p.Type != receipt.Result.SuggestedType || receipt.Result.Confidence < 80 {
		classification.Source = "operator_override"
		classification.Evidence = append(append([]string{}, classification.Evidence...), fmt.Sprintf("Operator selected %s; protocol suggested %s with confidence %d", p.Type, receipt.Result.SuggestedType, receipt.Result.Confidence))
	}
	raw, err := json.Marshal(classification)
	if err != nil {
		return nil, err
	}
	capabilities, _ := json.Marshal(receipt.Result.Capabilities)
	modes, _ := json.Marshal([]string{receipt.Protocol})
	sourceJSON, _ := json.Marshal(sources)
	_, err = tx.ExecContext(ctx, `UPDATE targets SET vendor=$2,model=$3,platform=$4,classification=$5,capabilities=$6,management_modes=$7,
 reachability_state='reachable',collection_state='authenticated',last_observed_at=$8 WHERE id=$1`, target.ID, receipt.Result.Vendor, receipt.Result.Model, receipt.Result.Platform, raw, capabilities, modes, receipt.CompletedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO network_target_connections(target_id,tenant_id,credential_id,test_id,protocol,address,port,telemetry_sources) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, target.ID, receipt.TenantID, receipt.CredentialID, receipt.ID, receipt.Protocol, receipt.Address, receipt.Port, sourceJSON)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE network_connection_tests SET target_id=$2 WHERE id=$1`, receipt.ID, target.ID)
	if err != nil {
		return nil, err
	}
	target, err = scanTarget(tx.QueryRowContext(ctx, targetSelect+" WHERE t.id=$1", target.ID))
	if err != nil {
		return nil, err
	}
	return target, tx.Commit()
}
