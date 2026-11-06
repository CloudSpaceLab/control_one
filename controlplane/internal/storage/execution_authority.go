package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/executionauthority"
	"github.com/google/uuid"
)

// Proofs are private, single-boundary context values, never accepted from API
// payloads. HTTP is completed before opening execution transactions/row locks.
type authorityProofKey struct{}
type authorityProof struct {
	external, authority, subject string
	decision                     executionauthority.Decision
}
type authorityProofs struct {
	tenant              uuid.UUID
	boundary, reference string
	entries             map[string]authorityProof
}
type authorityTarget struct {
	user    uuid.UUID
	purpose string
}

func bindAuthenticationAuthority(ctx context.Context, tx *sql.Tx, user uuid.UUID, external, authority, subject string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO authentication_authority_bindings(user_id,external_id,authority,subject) VALUES($1,$2,$3,$4) ON CONFLICT(user_id) DO UPDATE SET verified_at=clock_timestamp() WHERE authentication_authority_bindings.external_id=EXCLUDED.external_id AND authentication_authority_bindings.authority=EXCLUDED.authority AND authentication_authority_bindings.subject=EXCLUDED.subject`, user, external, authority, subject)
	if err != nil {
		return err
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT external_id=$2 AND authority=$3 AND subject=$4 FROM authentication_authority_bindings WHERE user_id=$1`, user, external, authority, subject).Scan(&matches)
	if err == nil && !matches {
		return errors.New("authentication authority binding cannot be reassigned")
	}
	return err
}

func (s *Store) prepareExecutionAuthority(ctx context.Context, tenant uuid.UUID, boundary, reference string, extra ...authorityTarget) (context.Context, error) {
	// Include the current organizational owner and independent approver. If a
	// concurrent policy selects another person, the transaction has no proof and denies.
	record, err := scanOrganization(s.db.QueryRowContext(ctx, `SELECT `+organizationColumns+` FROM patch_organization_policy WHERE tenant_id=$1`, tenant))
	if err != nil && err != sql.ErrNoRows {
		return ctx, err
	}
	if err == nil && record.Status == "approved" && !record.Policy.EffectiveAt.After(time.Now()) && record.Policy.ExpiresAt.After(time.Now()) {
		extra = append(extra, authorityTarget{record.Policy.OwnerID, "organization_owner"}, authorityTarget{record.Policy.ApproverID, "organization_approver"})
	}
	proofs := authorityProofs{tenant: tenant, boundary: boundary, reference: reference, entries: map[string]authorityProof{}}
	for _, target := range extra {
		if target.user == uuid.Nil {
			continue
		}
		key := target.user.String() + ":" + target.purpose
		if _, ok := proofs.entries[key]; ok {
			continue
		}
		var external string
		if err = s.db.QueryRowContext(ctx, `SELECT external_id FROM users WHERE id=$1 AND disabled_at IS NULL`, target.user).Scan(&external); err != nil {
			if target.purpose == "individual_approval" || target.purpose == "tenant_policy" {
				return ctx, fmt.Errorf("current scheduling authority unavailable: current execution identity unavailable")
			}
			return ctx, fmt.Errorf("current execution identity unavailable")
		}
		if !strings.HasPrefix(external, "auth:v1:") {
			continue
		}
		var authority, subject string
		if err = s.db.QueryRowContext(ctx, `SELECT authority,subject FROM authentication_authority_bindings WHERE user_id=$1 AND external_id=$2`, target.user, external).Scan(&authority, &subject); err != nil {
			return ctx, errors.New("authentication-backed authority needs a fresh verified sign-in or reviewed binding")
		}
		decision, e := s.executionAuthority.Check(ctx, executionauthority.Request{Authority: authority, Subject: subject, UserID: target.user.String(), TenantID: tenant.String(), Boundary: boundary, Purpose: target.purpose, Reference: reference})
		digest := sha256.Sum256([]byte(authority))
		outcome := "provider_snapshot_allowed"
		detail := "A separate execution transaction must still authorize this action."
		if e != nil {
			outcome = "provider_check_denied"
			detail = e.Error()
		}
		resource := target.user.String()
		_, auditErr := s.CreateAuditLog(ctx, &AuditLog{ActorType: "system", Action: "identity.execution_authority.check", ResourceType: "identity", ResourceID: &resource, Metadata: map[string]any{"tenant_id": tenant.String(), "authority_sha256": hex.EncodeToString(digest[:]), "boundary": boundary, "purpose": target.purpose, "reference": reference, "outcome": outcome, "detail": detail, "decision_id": decision.DecisionID, "observed_at": decision.ObservedAt, "expires_at": decision.ExpiresAt}})
		if auditErr != nil {
			return ctx, errors.New("execution authority evidence could not be retained")
		}
		if e != nil {
			return ctx, e
		}
		proofs.entries[key] = authorityProof{external: external, authority: authority, subject: subject, decision: decision}
	}
	return context.WithValue(ctx, authorityProofKey{}, proofs), nil
}

type ExecutionAuthorityReadiness struct {
	AuthenticationBacked bool   `json:"authentication_backed"`
	Configured           bool   `json:"configured"`
	BindingPresent       bool   `json:"binding_present"`
	Detail               string `json:"detail"`
}

// Readiness is descriptive. It never calls the provider or authorizes execution.
func (s *Store) ExecutionAuthorityReadiness(ctx context.Context, user uuid.UUID) (ExecutionAuthorityReadiness, error) {
	var out ExecutionAuthorityReadiness
	var provider string
	err := s.db.QueryRowContext(ctx, `SELECT auth_provider,external_id LIKE 'auth:v1:%',EXISTS(SELECT 1 FROM authentication_authority_bindings b WHERE b.user_id=users.id AND b.external_id=users.external_id) FROM users WHERE id=$1`, user).Scan(&provider, &out.AuthenticationBacked, &out.BindingPresent)
	if err != nil {
		return out, err
	}
	out.Configured = s.executionAuthority != nil
	switch {
	case !out.AuthenticationBacked:
		out.Detail = "This identity uses its existing local or directory authorization path."
	case provider != "oidc":
		out.Detail = "This connection supports OIDC identities only. Static test-token identities remain blocked from unattended provider-backed execution."
	case !out.BindingPresent:
		out.Detail = "A fresh verified sign-in is required to retain this identity's provider binding. Unattended execution remains blocked."
	case !out.Configured:
		out.Detail = "The current-provider authority connection is not configured. SSO-backed unattended execution remains blocked."
	default:
		out.Detail = "The authority connection is configured. Each schedule, dispatch and installation start still requires a fresh provider decision and current Control One permissions. Configuration alone is not provider qualification."
	}
	return out, nil
}

func (s *Store) prepareAutomationAuthority(ctx context.Context, tenant, run uuid.UUID, boundary, reference string) (context.Context, error) {
	var actor uuid.UUID
	var purpose string
	err := s.db.QueryRowContext(ctx, `SELECT actor_id,authority FROM patch_automation_runs WHERE tenant_id=$1 AND id=$2`, tenant, run).Scan(&actor, &purpose)
	if err != nil {
		return ctx, err
	}
	return s.prepareExecutionAuthority(ctx, tenant, boundary, reference, authorityTarget{actor, purpose})
}

func checkExecutionAuthorityTx(ctx context.Context, tx *sql.Tx, tenant, user uuid.UUID, purpose string) error {
	var external, provider string
	// All writers lock the user exclusively. Keep the identity stable until the
	// guarded transaction ends; expiry is evaluated after any lock wait.
	err := tx.QueryRowContext(ctx, `SELECT external_id,auth_provider FROM users WHERE id=$1 AND disabled_at IS NULL FOR SHARE`, user).Scan(&external, &provider)
	if err != nil {
		return errors.New("current execution identity unavailable")
	}
	if provider == "ldap" {
		return errors.New("directory-backed execution authority is not qualified")
	}
	if !strings.HasPrefix(external, "auth:v1:") {
		return nil
	}
	proofs, ok := ctx.Value(authorityProofKey{}).(authorityProofs)
	if !ok || proofs.tenant != tenant {
		return errors.New("authentication-backed authority requires execution-time provider revalidation")
	}
	proof, ok := proofs.entries[user.String()+":"+purpose]
	if !ok || provider != "oidc" || proof.external != external || proof.decision.Boundary != proofs.boundary || proof.decision.Reference != proofs.reference || !proof.decision.ExpiresAt.After(time.Now()) {
		return errors.New("fresh execution-time provider revalidation required")
	}
	var matches bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM authentication_authority_bindings WHERE user_id=$1 AND external_id=$2 AND authority=$3 AND subject=$4)`, user, external, proof.authority, proof.subject).Scan(&matches)
	if err != nil {
		return err
	}
	if !matches {
		return errors.New("execution authority identity binding changed")
	}
	return nil
}

// Later SQL lock waits must not extend a provider decision's validity.
func checkExecutionProofDeadlines(ctx context.Context) error {
	proofs, ok := ctx.Value(authorityProofKey{}).(authorityProofs)
	if !ok {
		return nil
	}
	now := time.Now()
	for _, proof := range proofs.entries {
		if !proof.decision.ExpiresAt.After(now) {
			return errors.New("execution authority expired while awaiting database authorization; retry with a fresh check")
		}
	}
	return nil
}
