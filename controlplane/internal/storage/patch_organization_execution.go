package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

// A retained organizational record adopts this additional gate. There is no
// delete API or draft/withdrawal fallback to legacy authority. Unadopted tenants
// retain the existing approval contract and are not production-governance ready.
// Lock tenant before organization/operations/approval rows to serialize policy
// changes with dispatch and avoid reversing the scheduler's lock order.
func checkPatchOrganizationTx(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, expected *int64) (int64, error) {
	record, e := scanOrganization(tx.QueryRowContext(ctx, `SELECT `+organizationColumns+` FROM patch_organization_policy WHERE tenant_id=$1`, tenant))
	if e == sql.ErrNoRows {
		if expected != nil && *expected != 0 {
			return 0, fmt.Errorf("organizational authorization record is missing")
		}
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	now := time.Now().UTC()
	if record.Status != "approved" || record.ApprovedBy == nil || record.ApprovedAt == nil || *record.ApprovedBy != record.Policy.ApproverID || *record.ApprovedBy == record.AuthoredBy {
		return 0, fmt.Errorf("organizational policy requires independent approval before patch execution")
	}
	if record.Policy.EffectiveAt.After(now) || !record.Policy.ExpiresAt.After(now) {
		return 0, fmt.Errorf("organizational approval is expired or not yet effective")
	}
	if expected != nil && *expected != record.Revision {
		return 0, fmt.Errorf("organizational authorization changed; create a fresh reviewed request")
	}
	// Stored LDAP grants can outlive directory membership. Until the execution
	// boundary can establish fresh directory authority, never use those grants
	// as evidence for an unattended organizational policy decision.
	var directoryIdentity bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id IN ($1,$2) AND auth_provider='ldap')`, record.Policy.OwnerID, record.Policy.ApproverID).Scan(&directoryIdentity); e != nil {
		return 0, e
	}
	if directoryIdentity {
		return 0, fmt.Errorf("directory-backed organizational owner or approver cannot authorize patch execution until directory authority is qualified")
	}
	var authenticationIdentity bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id IN ($1,$2) AND external_id LIKE 'auth:v1:%')`, record.Policy.OwnerID, record.Policy.ApproverID).Scan(&authenticationIdentity); e != nil {
		return 0, e
	}
	if authenticationIdentity {
		return 0, fmt.Errorf("authentication-backed organizational authority requires execution-time provider revalidation")
	}
	if e = organizationAdmin(ctx, tx, tenant, record.Policy.OwnerID); e != nil {
		return 0, e
	}
	if e = organizationApprover(ctx, tx, tenant, record.Policy.ApproverID); e != nil {
		return 0, e
	}
	var urgency, operations int64
	e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT revision FROM patch_governance_policy WHERE tenant_id=$1),0),COALESCE((SELECT revision FROM patch_review_operations WHERE tenant_id=$1),0)`, tenant).Scan(&urgency, &operations)
	if e != nil {
		return 0, e
	}
	if urgency == 0 || operations == 0 || urgency != record.UrgencyRevision || operations != record.OperationsRevision {
		return 0, fmt.Errorf("organizational approval no longer matches saved controls")
	}
	return record.Revision, nil
}

func checkPatchOrganizationScopeTx(ctx context.Context, tx *sql.Tx, tenant, node uuid.UUID, mode string, payload []byte, revision int64) error {
	if revision == 0 {
		return nil
	}
	var requested struct {
		OnlinePlan json.RawMessage      `json:"online_plan"`
		Packages   []string             `json:"package_allowlist"`
		Window     *patchsupport.Window `json:"patch_window"`
	}
	if json.Unmarshal(payload, &requested) != nil {
		return fmt.Errorf("invalid organizational execution scope")
	}
	// The live revocation hook is qualified only for exact online APT. Do not
	// allow disconnected/offline or other agents to bypass the new start gate.
	if (mode != "direct" && mode != "proxy") || len(requested.OnlinePlan) == 0 || string(requested.OnlinePlan) == "null" {
		return fmt.Errorf("organizational enforcement requires an exact online APT plan and start-time authorization; this path is unavailable")
	}
	var raw []byte
	if e := tx.QueryRowContext(ctx, `SELECT document FROM patch_review_operations WHERE tenant_id=$1`, tenant).Scan(&raw); e != nil {
		return e
	}
	var operations patchgovernance.Operations
	if e := json.Unmarshal(raw, &operations); e != nil {
		return e
	}
	scope := operations.Automation
	if !slices.Contains(scope.NodeIDs, node.String()) || scope.Transport != mode || len(requested.Packages) == 0 {
		return fmt.Errorf("patch target, transport or explicit package scope is outside approved organizational controls")
	}
	for _, pkg := range requested.Packages {
		if !slices.Contains(scope.Packages, pkg) || slices.Contains(scope.Exclusions, pkg) {
			return fmt.Errorf("package is outside approved organizational controls")
		}
	}
	if requested.Window == nil || requested.Window.ID != scope.WindowID {
		return fmt.Errorf("maintenance window differs from approved organizational controls")
	}
	var plan patchsupport.OnlinePlan
	if json.Unmarshal(requested.OnlinePlan, &plan) != nil {
		return fmt.Errorf("invalid exact online plan")
	}
	for _, pkg := range plan.Resolved {
		if slices.Contains(scope.Exclusions, pkg.Name) {
			return fmt.Errorf("resolved dependency is excluded by organizational controls")
		}
	}
	return nil
}

func checkOrganizationApprovalBinding(ctx context.Context, tx *sql.Tx, tenant, approval uuid.UUID, revision int64) error {
	if revision == 0 {
		return nil
	}
	var bound int64
	e := tx.QueryRowContext(ctx, `SELECT organization_revision FROM patch_organization_approval_bindings WHERE approval_id=$1 AND tenant_id=$2`, approval, tenant).Scan(&bound)
	if e != nil || bound != revision {
		return fmt.Errorf("patch approval is not bound to the current organizational revision; request a new approval")
	}
	return nil
}

// Called by the existing agent's under-APT-lock callback. Jobs created before
// policy adoption have revision zero and cannot start after adoption.
func (s *Store) CheckPatchOrganizationExecution(ctx context.Context, tenant, node, jobID uuid.UUID) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockAutomationTenant(ctx, tx, tenant); e != nil {
		return e
	}
	var raw []byte
	var kind, status string
	e = tx.QueryRowContext(ctx, `SELECT payload,type,status FROM jobs WHERE id=$1 AND tenant_id=$2`, jobID, tenant).Scan(&raw, &kind, &status)
	if e != nil {
		return e
	}
	var payload struct {
		Revision int64  `json:"organization_policy_revision"`
		NodeID   string `json:"node_id"`
		Mode     string `json:"mode"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.NodeID != node.String() || kind != "patch.deploy_"+payload.Mode || (status != "queued" && status != "running") {
		return fmt.Errorf("current node-bound patch job required")
	}
	revision, e := checkPatchOrganizationTx(ctx, tx, tenant, &payload.Revision)
	if e != nil {
		return e
	}
	return checkPatchOrganizationScopeTx(ctx, tx, tenant, node, payload.Mode, raw, revision)
}
