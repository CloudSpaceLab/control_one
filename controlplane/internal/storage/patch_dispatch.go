package storage

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"time"
)

type DispatchPatchJobParams struct {
	TenantID, DeploymentID, NodeID uuid.UUID
	Mode                           string
	Payload                        json.RawMessage
	Plan                           CreateActionPlanParams
	ApprovalID, ApproverID         uuid.UUID
	PolicyHash                     string
}

// A rejected duplicate dispatch must never terminalize an already-linked job.
func (s *Store) MarkUnlinkedPatchDispatchFailed(ctx context.Context, id uuid.UUID, reason, logTail string) error {
	if s.db == nil {
		return errors.New("store database not initialized")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE node_patch_state SET status='failed',error=$2,log_tail=$3 WHERE id=$1 AND status='pending' AND job_id IS NULL`, id, reason, logTail)
	return err
}

// DispatchPatchJob publishes a job only together with its reviewed plan and
// unique state link. The state lock serializes duplicate approval/wave requests.
func (s *Store) DispatchPatchJob(ctx context.Context, p DispatchPatchJobParams) (*NodePatchState, error) {
	if s.db == nil {
		return nil, errors.New("store database not initialized")
	}
	if p.Mode != "direct" && p.Mode != "proxy" && p.Mode != "airgapped" {
		return nil, errors.New("unsupported patch dispatch mode")
	}
	if p.Mode == "airgapped" && p.ApprovalID == uuid.Nil {
		return nil, errors.New("offline execution requires explicit approval")
	}
	var preflight struct {
		AutomationRunID string `json:"automation_run_id"`
	}
	if json.Unmarshal(p.Payload, &preflight) != nil {
		return nil, errors.New("invalid patch payload")
	}
	var prepErr error
	if preflight.AutomationRunID != "" {
		runID, e := uuid.Parse(preflight.AutomationRunID)
		if e != nil {
			return nil, e
		}
		ctx, prepErr = s.prepareAutomationAuthority(ctx, p.TenantID, runID, "dispatch", p.ApprovalID.String())
	} else {
		ctx, prepErr = s.prepareExecutionAuthority(ctx, p.TenantID, "dispatch", p.ApprovalID.String())
	}
	if prepErr != nil {
		return nil, prepErr
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockAutomationTenant(ctx, tx, p.TenantID); err != nil {
		return nil, err
	}
	organizationRevision, err := checkPatchOrganizationTx(ctx, tx, p.TenantID, nil)
	if err != nil {
		return nil, err
	}
	if err = checkOrganizationApprovalBinding(ctx, tx, p.TenantID, p.ApprovalID, organizationRevision); err != nil {
		return nil, err
	}
	if err = checkPatchOrganizationScopeTx(ctx, tx, p.TenantID, p.NodeID, p.Mode, p.Payload, organizationRevision); err != nil {
		return nil, err
	}
	var scheduled struct {
		AutomationRunID string `json:"automation_run_id"`
	}
	if err = json.Unmarshal(p.Payload, &scheduled); err != nil {
		return nil, err
	}
	var automationRun uuid.UUID
	if scheduled.AutomationRunID != "" {
		automationRun, err = uuid.Parse(scheduled.AutomationRunID)
		if err != nil {
			return nil, errors.New("invalid automation authorization")
		}
		run, binding, e := checkAutomationRunTx(ctx, tx, p.TenantID, automationRun)
		if e != nil {
			return nil, e
		}
		if run.Status == "dispatched" {
			var old NodePatchState
			e = tx.QueryRowContext(ctx, `SELECT id,deployment_id,node_id,tenant_id,status,job_id FROM node_patch_state WHERE job_id=$1 AND tenant_id=$2 AND deployment_id=$3 AND node_id=$4`, run.ExecutionJobID, p.TenantID, p.DeploymentID, p.NodeID).Scan(&old.ID, &old.DeploymentID, &old.NodeID, &old.TenantID, &old.Status, &old.JobID)
			return &old, e
		}
		if run.ApprovalID != p.ApprovalID || run.ActorID != p.ApproverID || run.PolicyHash != p.PolicyHash || binding.node != p.NodeID || binding.deployment != p.DeploymentID || binding.mode != p.Mode {
			return nil, errors.New("automation dispatch identity mismatch")
		}
	} else if p.ApprovalID != uuid.Nil {
		// Serialize the retained-schedule check with enqueue's approval lock.
		// Without this lock a manual dispatch could observe no schedule, wait
		// for enqueue, then consume the newly scheduled approval immediately.
		var lockedApproval uuid.UUID
		if err = tx.QueryRowContext(ctx, `SELECT id FROM patch_approvals WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, p.ApprovalID, p.TenantID).Scan(&lockedApproval); err != nil {
			return nil, err
		}
		var scheduledApproval bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM patch_automation_runs WHERE approval_id=$1)`, p.ApprovalID).Scan(&scheduledApproval); err != nil {
			return nil, err
		}
		if scheduledApproval {
			return nil, errors.New("approval has a retained automation authorization; use its schedule or create a fresh request")
		}
	}
	var lockedNode uuid.UUID
	if err = tx.QueryRowContext(ctx, `SELECT id FROM nodes WHERE id=$1 AND tenant_id=$2 FOR SHARE`, p.NodeID, p.TenantID).Scan(&lockedNode); err != nil {
		return nil, err
	}
	if err = rejectUnreconciledProxyAction(ctx, tx, p.TenantID, p.NodeID); err != nil {
		return nil, err
	}
	var requested struct {
		OnlinePlan                 *patchsupport.OnlinePlan     `json:"online_plan"`
		PublishedOfflinePlanSHA256 string                       `json:"published_offline_plan_sha256"`
		PostPatchRescan            bool                         `json:"post_patch_rescan"`
		OfflineContent             *patchsupport.OfflineContent `json:"offline_content"`
		Allowlist                  []string                     `json:"package_allowlist"`
		Denylist                   []string                     `json:"package_denylist"`
		Runtime                    *patchsupport.Runtime        `json:"patch_runtime"`
		Window                     *patchsupport.Window         `json:"patch_window"`
	}
	if err = json.Unmarshal(p.Payload, &requested); err != nil {
		return nil, err
	}
	if err = validateOnlineDispatch(ctx, tx, p, requested.OnlinePlan, requested.Allowlist, requested.Denylist, requested.PostPatchRescan, requested.Runtime, requested.Window); err != nil {
		return nil, err
	}
	if err := requested.OfflineContent.Validate(p.Mode, requested.Allowlist, requested.Denylist); err != nil {
		return nil, err
	}
	if p.Mode == "airgapped" {
		if !requested.PostPatchRescan {
			return nil, errors.New("offline execution requires post-install inventory")
		}
		if requested.Runtime == nil {
			return nil, errors.New("offline runtime evidence required")
		}
		if err := requested.OfflineContent.ValidateInstallation(p.Mode, requested.Allowlist, requested.Denylist); err != nil {
			return nil, err
		}
		var reviewed []byte
		if err = tx.QueryRowContext(ctx, `SELECT summary->'offline_content' FROM patch_deployments WHERE id=$1 AND tenant_id=$2 FOR SHARE`, p.DeploymentID, p.TenantID).Scan(&reviewed); err != nil {
			return nil, err
		}
		var content any
		if err = json.Unmarshal(reviewed, &content); err != nil {
			return nil, err
		}
		canonical, _ := json.Marshal(content)
		raw, _ := json.Marshal(requested.OfflineContent)
		var normalized any
		_ = json.Unmarshal(raw, &normalized)
		raw, _ = json.Marshal(normalized)
		if !bytes.Equal(canonical, raw) {
			return nil, errors.New("offline payload differs from reviewed deployment content")
		}
		planRaw, err := json.Marshal(p.Plan.Diff["offline_content"])
		if err != nil {
			return nil, err
		}
		var normalizedPlan any
		if err = json.Unmarshal(planRaw, &normalizedPlan); err != nil {
			return nil, err
		}
		planRaw, _ = json.Marshal(normalizedPlan)
		if !bytes.Equal(planRaw, raw) {
			return nil, errors.New("offline action plan differs from reviewed content")
		}
	}
	if requested.PublishedOfflinePlanSHA256 != "" {
		if p.Mode != "airgapped" {
			return nil, errors.New("published offline plan requires airgapped mode")
		}
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT labels->'patch.offline_plans' FROM nodes WHERE id=$1`, p.NodeID).Scan(&raw); err != nil {
			return nil, err
		}
		var evidence patchsupport.PublishedPlanEvidence
		if json.Unmarshal(raw, &evidence) != nil {
			return nil, errors.New("published offline plan unavailable at dispatch commit")
		}
		if err = evidence.Require(requested.PublishedOfflinePlanSHA256, requested.OfflineContent, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	// Source selection is immutable across reviewed deployment, action plan and
	// dispatch, including attempts to strip an agent selection into manual mode.
	if p.Mode == "airgapped" {
		var selected string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(summary->>'published_offline_plan_sha256','') FROM patch_deployments WHERE id=$1 AND tenant_id=$2`, p.DeploymentID, p.TenantID).Scan(&selected); err != nil {
			return nil, err
		}
		planSelected, _ := p.Plan.Diff["published_offline_plan_sha256"].(string)
		if selected != requested.PublishedOfflinePlanSHA256 || planSelected != selected {
			return nil, errors.New("offline plan source differs from review")
		}
	}
	if requested.Runtime != nil {
		var raw []byte
		if err = tx.QueryRowContext(ctx, `SELECT labels->'patch.runtime' FROM nodes WHERE id=$1`, p.NodeID).Scan(&raw); err != nil {
			return nil, err
		}
		var evidence patchsupport.Evidence
		if json.Unmarshal(raw, &evidence) != nil || !evidence.Current(time.Now().UTC()) || evidence.Runtime != *requested.Runtime {
			return nil, errors.New("patch runtime changed or expired before dispatch commit")
		}
		if err = evidence.Runtime.Validate(p.Mode); err != nil {
			return nil, err
		}
	}
	if requested.Window != nil {
		if requested.Runtime == nil || !requested.Runtime.CalendarWindowStartCheck {
			return nil, errors.New("agent calendar-window enforcement unavailable at dispatch commit")
		}
		if err := requested.Window.Validate(time.Now().UTC()); err != nil {
			return nil, err
		}
		var opens, closes time.Time
		var status string
		var forced bool
		var repositories int
		err := tx.QueryRowContext(ctx, `SELECT opens_at,closes_at,status,force_closed_at IS NOT NULL,cardinality(allow_repos) FROM maintenance_windows WHERE id=$1 AND tenant_id=$2 AND $3=ANY(node_ids) FOR SHARE`, requested.Window.ID, p.TenantID, p.NodeID).Scan(&opens, &closes, &status, &forced, &repositories)
		if err != nil {
			return nil, fmt.Errorf("maintenance window unavailable at dispatch commit: %w", err)
		}
		if status != "open" || forced || repositories > 0 || !opens.Equal(requested.Window.OpensAt) || !closes.Equal(requested.Window.ClosesAt) {
			return nil, errors.New("maintenance window changed or enforcement unavailable before dispatch commit")
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO node_patch_state(deployment_id,node_id,tenant_id,status)
 SELECT d.id,n.id,d.tenant_id,'pending' FROM patch_deployments d JOIN nodes n ON n.id=$2
 WHERE d.id=$1 AND d.tenant_id=$3 AND n.tenant_id=$3
 ON CONFLICT(deployment_id,node_id) DO NOTHING`, p.DeploymentID, p.NodeID, p.TenantID); err != nil {
		return nil, err
	}
	state, err := scanNodePatchState(tx.QueryRowContext(ctx, `SELECT id,deployment_id,node_id,tenant_id,status,packages_upgraded,log_tail,error,job_id,requested_at,applied_at FROM node_patch_state WHERE deployment_id=$1 AND node_id=$2 AND tenant_id=$3 FOR UPDATE`, p.DeploymentID, p.NodeID, p.TenantID))
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err = json.Unmarshal(p.Payload, &payload); err != nil || payload == nil {
		return nil, errors.New("invalid patch dispatch payload")
	}
	payload["node_patch_state_id"], payload["node_id"], payload["deployment_id"], payload["mode"] = state.ID.String(), p.NodeID.String(), p.DeploymentID.String(), p.Mode
	if organizationRevision > 0 {
		payload["organization_policy_revision"] = organizationRevision
	} else {
		delete(payload, "organization_policy_revision")
	}
	delete(payload, "action_plan_id")
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if state.JobID != nil {
		var same bool
		if err = tx.QueryRowContext(ctx, `SELECT tenant_id=$2 AND type=$3 AND (payload-'action_plan_id')=($4::jsonb-'action_plan_id') FROM jobs WHERE id=$1`, *state.JobID, p.TenantID, "patch.deploy_"+p.Mode, raw).Scan(&same); err != nil {
			return nil, err
		}
		if !same {
			return nil, errors.New("existing patch dispatch differs from requested policy")
		}
		return state, nil
	}
	if state.Status != "pending" {
		return nil, errors.New("patch state already terminal; create a new deployment")
	}
	now := s.clock().UTC()
	if p.ApprovalID != uuid.Nil {
		if p.PolicyHash == "" {
			return nil, errors.New("patch approval snapshot required")
		}
		var approver any
		if p.ApproverID != uuid.Nil {
			approver = p.ApproverID
		}
		result, err := tx.ExecContext(ctx, `UPDATE patch_approvals SET status='approved',approved_by=$2,approved_at=$3
  WHERE id=$1 AND tenant_id=$4 AND deployment_id=$5 AND node_id=$6 AND mode=$7 AND status='pending' AND expires_at>$3 AND policy_hash=$8`, p.ApprovalID, approver, now, p.TenantID, p.DeploymentID, p.NodeID, p.Mode, p.PolicyHash)
		if err != nil {
			return nil, err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return nil, err
		}
		if n != 1 {
			return nil, errors.New("patch approval is no longer pending, current or owned")
		}
	}
	p.Plan.TenantID, p.Plan.NodeID, p.Plan.Domain, p.Plan.ActionKind = p.TenantID, &p.NodeID, "patch", "patch.deploy"
	p.Plan.IdempotencyKey = "patch_node_state:" + state.ID.String()
	// Bind plan identities even when the caller did not create the state first.
	scope := map[string]any{}
	for k, v := range p.Plan.Scope {
		scope[k] = v
	}
	p.Plan.Scope = scope
	p.Plan.Scope["node_patch_state_id"], p.Plan.Scope["deployment_id"], p.Plan.Scope["node_id"], p.Plan.Scope["tenant_id"] = state.ID.String(), p.DeploymentID.String(), p.NodeID.String(), p.TenantID.String()
	sourceRef := map[string]any{}
	for k, v := range p.Plan.SourceRef {
		sourceRef[k] = v
	}
	p.Plan.SourceRef = sourceRef
	if p.Plan.Diff == nil {
		p.Plan.Diff = map[string]any{}
	}
	p.Plan.SourceRef["node_patch_state_id"], p.Plan.SourceRef["deployment_id"] = state.ID.String(), p.DeploymentID.String()
	plan, err := createActionPlan(ctx, tx, p.Plan)
	if err != nil {
		return nil, fmt.Errorf("create patch plan: %w", err)
	}
	expected, err := canonicalPatchDispatchJSON(p.Plan.Diff)
	if err != nil {
		return nil, err
	}
	actual, err := canonicalPatchDispatchJSON(plan.Diff)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(expected, actual) || plan.TenantID != p.TenantID || !plan.NodeID.Valid || plan.NodeID.UUID != p.NodeID || plan.State != ActionPlanStateQueued {
		return nil, errors.New("existing action plan differs or failed; reconciliation required")
	}
	payload["action_plan_id"] = plan.ID.String()
	raw, err = json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	jobID := uuid.New()
	if _, err = tx.ExecContext(ctx, `INSERT INTO jobs(id,tenant_id,type,status,payload,created_at,updated_at) VALUES($1,$2,$3,'queued',$4,$5,$5)`, jobID, p.TenantID, "patch.deploy_"+p.Mode, raw, now); err != nil {
		return nil, err
	}
	if err = createPatchVerificationTx(ctx, tx, jobID, p.TenantID, p.NodeID, p.DeploymentID, raw); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO job_events(id,job_id,status,message,created_at) VALUES($1,$2,'queued','patch dispatch committed',$3)`, uuid.New(), jobID, now); err != nil {
		return nil, err
	}
	link, err := tx.ExecContext(ctx, `UPDATE node_patch_state SET job_id=$2 WHERE id=$1 AND job_id IS NULL AND status='pending'`, state.ID, jobID)
	if err != nil {
		return nil, err
	}
	linked, err := link.RowsAffected()
	if err != nil {
		return nil, err
	}
	if linked != 1 {
		return nil, errors.New("patch job linkage was not persisted")
	}
	if automationRun != uuid.Nil {
		if _, err = tx.ExecContext(ctx, `UPDATE patch_automation_runs SET status='dispatched',reason='Exact execution committed',execution_job_id=$2,checked_at=NOW() WHERE id=$1`, automationRun, jobID); err != nil {
			return nil, err
		}
		body, _ := json.Marshal(map[string]any{"run_id": automationRun, "job_id": jobID, "approval_id": p.ApprovalID})
		if _, err = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) SELECT tenant_id,'automation_dispatched',policy_revision,actor_id::text,$2 FROM patch_automation_runs WHERE id=$1`, automationRun, body); err != nil {
			return nil, err
		}
	}
	if err = checkExecutionProofDeadlines(ctx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	state.JobID = &jobID
	return state, nil
}

// Struct fields and persisted JSON objects can have different key order.
// Compare canonical JSON without changing array order or coercing numbers.
func canonicalPatchDispatchJSON(value any) ([]byte, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err = decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return json.Marshal(decoded)
}

// AcquirePatchDeploymentDispatch holds a database session lock across the
// advance handler's reads and writes. A process/connection failure releases it.
func (s *Store) AcquirePatchDeploymentDispatch(ctx context.Context, id uuid.UUID) (func(), bool, error) {
	if s.db == nil {
		return nil, false, errors.New("store database not initialized")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, false, err
	}
	key := "controlone.patch.dispatch:" + id.String()
	var acquired bool
	if err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, key).Scan(&acquired); err != nil || !acquired {
		_ = conn.Close()
		return nil, false, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, key); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	return release, true, nil
}
