package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type PatchAutomationRun struct {
	ID             uuid.UUID     `json:"id"`
	TenantID       uuid.UUID     `json:"tenant_id"`
	ApprovalID     uuid.UUID     `json:"approval_id"`
	PolicyRevision int64         `json:"policy_revision"`
	ActorID        uuid.UUID     `json:"actor_id"`
	Authority      string        `json:"authority"`
	PolicyHash     string        `json:"policy_hash"`
	NotBefore      time.Time     `json:"not_before"`
	ExpiresAt      time.Time     `json:"expires_at"`
	Status         string        `json:"status"`
	Reason         string        `json:"reason"`
	ExecutionJobID uuid.NullUUID `json:"execution_job_id"`
	CreatedAt      time.Time     `json:"created_at"`
	CheckedAt      *time.Time    `json:"checked_at,omitempty"`
}

const automationColumns = `id,tenant_id,approval_id,policy_revision,actor_id,authority,policy_hash,not_before,expires_at,status,reason,execution_job_id,created_at,checked_at`

func lockAutomationTenant(ctx context.Context, tx *sql.Tx, tenant uuid.UUID) error {
	var id uuid.UUID
	return tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id=$1 FOR SHARE`, tenant).Scan(&id)
}

func scanAutomation(scan func(...any) error) (PatchAutomationRun, error) {
	var r PatchAutomationRun
	err := scan(&r.ID, &r.TenantID, &r.ApprovalID, &r.PolicyRevision, &r.ActorID, &r.Authority, &r.PolicyHash, &r.NotBefore, &r.ExpiresAt, &r.Status, &r.Reason, &r.ExecutionJobID, &r.CreatedAt, &r.CheckedAt)
	return r, err
}
func (s *Store) ListPatchAutomationRuns(ctx context.Context, tenant uuid.UUID) ([]PatchAutomationRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+automationColumns+` FROM patch_automation_runs WHERE tenant_id=$1 ORDER BY created_at DESC,id DESC LIMIT 100`, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PatchAutomationRun{}
	for rows.Next() {
		r, e := scanAutomation(rows.Scan)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) DuePatchAutomationRuns(ctx context.Context) ([]PatchAutomationRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+automationColumns+` FROM patch_automation_runs WHERE status IN ('waiting','blocked') AND not_before<=NOW() ORDER BY COALESCE(checked_at,created_at),id LIMIT 25`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PatchAutomationRun{}
	for rows.Next() {
		r, e := scanAutomation(rows.Scan)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) SetPatchAutomationBlocked(ctx context.Context, id uuid.UUID, reason string) error {
	if len(reason) > 500 {
		reason = reason[:500]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE patch_automation_runs SET status=CASE WHEN expires_at<=NOW() THEN 'expired' ELSE 'blocked' END,reason=$2,checked_at=NOW() WHERE id=$1 AND status IN ('waiting','blocked')`, id, reason)
	return err
}
func (s *Store) CancelPatchAutomationRun(ctx context.Context, tenant, id uuid.UUID, actor string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockAutomationTenant(ctx, tx, tenant); e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, `UPDATE patch_automation_runs SET status='cancelled',reason='Cancelled by administrator',checked_at=NOW() WHERE id=$1 AND tenant_id=$2 AND status IN ('waiting','blocked')`, id, tenant)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return fmt.Errorf("only an undispatched tenant request can be cancelled")
	}
	body, _ := json.Marshal(map[string]any{"run_id": id})
	_, e = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) VALUES($1,'automation_cancelled',0,$2,$3)`, tenant, actor, body)
	if e != nil {
		return e
	}
	return tx.Commit()
}

type automationBinding struct {
	node                       uuid.UUID
	deployment                 uuid.UUID
	mode, window, hash, status string
	expires                    time.Time
	plan                       patchsupport.OnlinePlan
}

func loadAutomationApproval(ctx context.Context, tx *sql.Tx, tenant, approval uuid.UUID) (automationBinding, error) {
	var b automationBinding
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT a.node_id,a.deployment_id,a.mode,COALESCE(a.window_id::text,''),a.policy_hash,a.status,a.expires_at,d.summary->'online_plan' FROM patch_approvals a JOIN patch_deployments d ON d.id=a.deployment_id AND d.tenant_id=a.tenant_id WHERE a.id=$1 AND a.tenant_id=$2 FOR SHARE OF a,d`, approval, tenant).Scan(&b.node, &b.deployment, &b.mode, &b.window, &b.hash, &b.status, &b.expires, &raw)
	if e != nil {
		return b, e
	}
	if e = json.Unmarshal(raw, &b.plan); e != nil {
		return b, fmt.Errorf("exact online plan required")
	}
	return b, nil
}
func automationRole(ctx context.Context, tx *sql.Tx, tenant, actor uuid.UUID, authority string) error {
	var allowed bool
	// A stored LDAP grant alone does not prove present directory membership.
	// Fail closed for scheduled authority until directory revalidation is
	// available at every execution boundary.
	var provider, external string
	e := tx.QueryRowContext(ctx, `SELECT auth_provider,external_id FROM users WHERE id=$1 AND disabled_at IS NULL`, actor).Scan(&provider, &external)
	if e != nil {
		return fmt.Errorf("current scheduling authority unavailable: %w", e)
	}
	if strings.HasPrefix(external, "auth:v1:") {
		return fmt.Errorf("authentication-backed scheduling authority requires execution-time provider revalidation")
	}
	if provider == "ldap" {
		return fmt.Errorf("directory-backed scheduling authority cannot be used until directory authority is qualified")
	}
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles u JOIN roles r ON r.id=u.role_id WHERE u.user_id=$1 AND (u.tenant_id=$2 OR u.tenant_id IS NULL) AND (u.expires_at IS NULL OR u.expires_at>NOW()) AND (r.name='admin' OR ($3='individual_approval' AND r.name='operator')))`, actor, tenant, authority).Scan(&allowed)
	if e != nil {
		return e
	}
	if !allowed {
		return fmt.Errorf("current scheduling authority unavailable")
	}
	return nil
}
func automationScope(p patchgovernance.Operations, b automationBinding, now time.Time) error {
	changed := []string{}
	for _, pkg := range b.plan.Resolved {
		changed = append(changed, pkg.Name)
	}
	if e := p.Validate(now); e != nil {
		return e
	}
	if e := b.plan.Validate(now); e != nil {
		return e
	}
	return patchgovernance.MatchAutomationApproval(p.Automation, b.node.String(), b.mode, b.window, b.plan.Requested, changed, now)
}

func (s *Store) QueuePatchAutomationRun(ctx context.Context, tenant, approval, actor uuid.UUID, revision int64, at time.Time, authority string) (PatchAutomationRun, error) {
	if authority != "individual_approval" && authority != "tenant_policy" {
		return PatchAutomationRun{}, fmt.Errorf("invalid authority")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return PatchAutomationRun{}, e
	}
	defer tx.Rollback()
	if e = lockAutomationTenant(ctx, tx, tenant); e != nil {
		return PatchAutomationRun{}, e
	}
	organizationRevision, e := checkPatchOrganizationTx(ctx, tx, tenant, nil)
	if e != nil {
		return PatchAutomationRun{}, e
	}
	if e = checkOrganizationApprovalBinding(ctx, tx, tenant, approval, organizationRevision); e != nil {
		return PatchAutomationRun{}, e
	}
	var raw []byte
	var current int64
	e = tx.QueryRowContext(ctx, `SELECT revision,document FROM patch_review_operations WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&current, &raw)
	if e != nil {
		return PatchAutomationRun{}, e
	}
	if current != revision {
		return PatchAutomationRun{}, ErrGovernanceConflict
	}
	var p patchgovernance.Operations
	if e = json.Unmarshal(raw, &p); e != nil {
		return PatchAutomationRun{}, e
	}
	now := time.Now().UTC()
	b, e := loadAutomationApproval(ctx, tx, tenant, approval)
	if e != nil {
		return PatchAutomationRun{}, e
	}
	if b.status != "pending" {
		return PatchAutomationRun{}, fmt.Errorf("a pending exact approval is required")
	}
	if e = automationScope(p, b, now); e != nil {
		return PatchAutomationRun{}, e
	}
	if authority == "tenant_policy" && (p.Automation.Mode != "automatic" || p.Automation.RequireApproval || slices.Contains(p.Automation.CanaryNodeIDs, b.node.String())) {
		return PatchAutomationRun{}, fmt.Errorf("this target requires individual approval")
	}
	if e = automationRole(ctx, tx, tenant, actor, authority); e != nil {
		return PatchAutomationRun{}, e
	}
	var opens, closes time.Time
	var forced bool
	var status string
	e = tx.QueryRowContext(ctx, `SELECT opens_at,closes_at,force_closed_at IS NOT NULL,status FROM maintenance_windows WHERE id=$1 AND tenant_id=$2 AND $3=ANY(node_ids)`, p.Automation.WindowID, tenant, b.node).Scan(&opens, &closes, &forced, &status)
	if e != nil || forced || status == "closed" || status == "aborted" {
		return PatchAutomationRun{}, fmt.Errorf("a current or future tenant window is required")
	}
	if at.IsZero() {
		at = now
	}
	if at.Before(now) {
		at = now
	}
	if at.Before(opens) {
		at = opens
	}
	end := b.expires
	for _, t := range []time.Time{closes, b.plan.Review.ExpiresAt, *p.Automation.ValidUntil} {
		if t.Before(end) {
			end = t
		}
	}
	if !end.After(at) {
		return PatchAutomationRun{}, fmt.Errorf("window does not fit within immutable plan, approval and policy expiry; prepare a fresh plan nearer the window")
	}
	id := uuid.New()
	e = tx.QueryRowContext(ctx, `INSERT INTO patch_automation_runs(id,tenant_id,approval_id,policy_revision,actor_id,authority,policy_hash,not_before,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(approval_id) DO NOTHING RETURNING id`, id, tenant, approval, revision, actor, authority, b.hash, at, end).Scan(&id)
	if e == sql.ErrNoRows {
		return PatchAutomationRun{}, fmt.Errorf("approval already has a retained schedule; create a new reviewed request")
	}
	if e != nil {
		return PatchAutomationRun{}, e
	}
	body, _ := json.Marshal(map[string]any{"run_id": id, "approval_id": approval, "authority": authority, "not_before": at, "expires_at": end})
	_, e = tx.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) VALUES($1,'automation_scheduled',$2,$3,$4)`, tenant, revision, actor.String(), body)
	if e != nil {
		return PatchAutomationRun{}, e
	}
	r, e := scanAutomation(tx.QueryRowContext(ctx, `SELECT `+automationColumns+` FROM patch_automation_runs WHERE id=$1`, id).Scan)
	if e != nil {
		return r, e
	}
	return r, tx.Commit()
}

// The caller locks the policy before the run, consistently with enqueue and
// cancellation. Dispatch commits this authorization and the one execution link
// together; a restart never manufactures a second package-manager execution.
func checkAutomationRunTx(ctx context.Context, tx *sql.Tx, tenant, id uuid.UUID) (PatchAutomationRun, automationBinding, error) {
	var r PatchAutomationRun
	var b automationBinding
	var raw []byte
	var revision int64
	if e := lockAutomationTenant(ctx, tx, tenant); e != nil {
		return r, b, e
	}
	organizationRevision, organizationErr := checkPatchOrganizationTx(ctx, tx, tenant, nil)
	if organizationErr != nil {
		return r, b, organizationErr
	}
	if e := tx.QueryRowContext(ctx, `SELECT revision,document FROM patch_review_operations WHERE tenant_id=$1 FOR UPDATE`, tenant).Scan(&revision, &raw); e != nil {
		return r, b, e
	}
	var p patchgovernance.Operations
	if e := json.Unmarshal(raw, &p); e != nil {
		return r, b, e
	}
	var e error
	r, e = scanAutomation(tx.QueryRowContext(ctx, `SELECT `+automationColumns+` FROM patch_automation_runs WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, tenant).Scan)
	if e != nil {
		return r, b, e
	}
	if e = checkOrganizationApprovalBinding(ctx, tx, tenant, r.ApprovalID, organizationRevision); e != nil {
		return r, b, e
	}
	now := time.Now().UTC()
	if r.Status == "dispatched" {
		return r, b, nil
	}
	if (r.Status != "waiting" && r.Status != "blocked") || r.PolicyRevision != revision || now.Before(r.NotBefore) || !r.ExpiresAt.After(now) {
		return r, b, fmt.Errorf("schedule is cancelled, not due, expired or policy revision changed")
	}
	b, e = loadAutomationApproval(ctx, tx, tenant, r.ApprovalID)
	if e != nil {
		return r, b, e
	}
	if b.status != "pending" || b.hash != r.PolicyHash {
		return r, b, fmt.Errorf("bound approval changed")
	}
	if e = automationScope(p, b, now); e != nil {
		return r, b, e
	}
	if e = automationRole(ctx, tx, tenant, r.ActorID, r.Authority); e != nil {
		return r, b, e
	}
	if r.Authority == "tenant_policy" && (p.Automation.Mode != "automatic" || p.Automation.RequireApproval || slices.Contains(p.Automation.CanaryNodeIDs, b.node.String())) {
		return r, b, fmt.Errorf("individual approval is required")
	}
	if e = checkAutomationHealthTx(ctx, tx, tenant, b.node); e != nil {
		return r, b, e
	}
	if e = automationCohortGateTx(ctx, tx, tenant, revision, p.Automation, b); e != nil {
		return r, b, e
	}
	var active int
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM patch_automation_runs r JOIN jobs j ON j.id=r.execution_job_id WHERE r.tenant_id=$1 AND r.policy_revision=$2 AND j.status IN ('queued','running')`, tenant, revision).Scan(&active)
	if e != nil {
		return r, b, e
	}
	if active >= p.Automation.MaxWaveSize {
		return r, b, fmt.Errorf("maximum active wave size reached")
	}
	return r, b, nil
}

func checkAutomationHealthTx(ctx context.Context, tx *sql.Tx, tenant, node uuid.UUID) error {
	var raw []byte
	var observed time.Time
	e := tx.QueryRowContext(ctx, `SELECT h.observed_at,h.evidence FROM node_patch_health h JOIN nodes n ON n.id=h.node_id WHERE n.id=$1 AND n.tenant_id=$2 AND n.state='active' AND n.os='ubuntu 24.04' AND n.arch='amd64' FOR SHARE OF h,n`, node, tenant).Scan(&observed, &raw)
	if e != nil {
		return fmt.Errorf("supported native host health evidence unavailable")
	}
	var h struct {
		Boot      string `json:"boot_id"`
		Audit     string `json:"package_audit"`
		Service   string `json:"service_health"`
		Reboot    *bool  `json:"reboot_required"`
		Services  bool   `json:"services_complete"`
		Consumers bool   `json:"consumers_complete"`
	}
	now := time.Now().UTC()
	if json.Unmarshal(raw, &h) != nil {
		return fmt.Errorf("host health evidence is malformed")
	}
	missing := []string{}
	if observed.After(now) || now.Sub(observed) > 5*time.Minute {
		missing = append(missing, "host health observation is stale or future-dated")
	}
	if h.Boot == "" {
		missing = append(missing, "boot identity is unavailable")
	}
	if h.Audit != "healthy" {
		missing = append(missing, "package audit is "+h.Audit)
	}
	if h.Service != "healthy" {
		missing = append(missing, "service health is "+h.Service)
	}
	if h.Reboot == nil {
		missing = append(missing, "reboot requirement is unknown")
	} else if *h.Reboot {
		missing = append(missing, "a required reboot is outstanding")
	}
	if !h.Services {
		missing = append(missing, "service inventory is incomplete")
	}
	if !h.Consumers {
		missing = append(missing, "running-consumer verification is incomplete")
	}
	if len(missing) > 0 {
		return fmt.Errorf("automation blocked: %s", strings.Join(missing, "; "))
	}
	var fresh bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM node_assessments a JOIN nodes n ON n.id=a.node_id JOIN node_inventory_sync i ON i.node_id=a.node_id JOIN vulnerability_publication f ON f.id=1 WHERE a.node_id=$1 AND a.tenant_id=$2 AND a.id=(SELECT id FROM node_assessments WHERE node_id=$1 ORDER BY started_at DESC,id DESC LIMIT 1) AND a.completed_at BETWEEN NOW()-INTERVAL '24 hours' AND NOW() AND a.status='partial' AND a.evidence->>'findings_updated'='true' AND (a.evidence->>'inventory_revision')::bigint=n.inventory_revision AND a.evidence->>'feed_generation'=f.generation AND a.evidence->'native_snapshot'->>'sha256'=i.snapshot->>'sha256' AND jsonb_array_length(a.evidence->'feeds')>0 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(a.evidence->'feeds') fe WHERE fe->>'expires_at' IS NULL OR (fe->>'expires_at')::timestamptz<=NOW()))`, node, tenant).Scan(&fresh)
	if e != nil {
		return e
	}
	if !fresh {
		return fmt.Errorf("fresh authoritative inventory and selected-advisory assessment required")
	}
	return nil
}

func (s *Store) CheckPatchAutomationRun(ctx context.Context, tenant, id uuid.UUID) (PatchAutomationRun, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return PatchAutomationRun{}, e
	}
	defer tx.Rollback()
	r, _, e := checkAutomationRunTx(ctx, tx, tenant, id)
	return r, e
}

func (s *Store) PatchAutomationReadiness(ctx context.Context, tenant, node uuid.UUID) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	return checkAutomationHealthTx(ctx, tx, tenant, node)
}

// Only enabled automatic policies may discover requests. Canary installation
// always requires a separate individual authorization, even in automatic mode.
func (s *Store) DiscoverPatchAutomationRuns(ctx context.Context) error {
	rows, e := s.db.QueryContext(ctx, `SELECT a.tenant_id,a.id,o.revision,o.updated_by FROM patch_approvals a JOIN patch_deployments d ON d.id=a.deployment_id AND d.tenant_id=a.tenant_id JOIN patch_review_operations o ON o.tenant_id=a.tenant_id WHERE d.summary->'online_plan' IS NOT NULL AND (o.document->'automation'->'node_ids') ? a.node_id::text AND NOT ((o.document->'automation'->'canary_node_ids') ? a.node_id::text) AND o.document->'automation'->>'transport'=a.mode AND o.document->'automation'->>'window_id'=a.window_id::text AND (o.document->'automation'->'packages') @> (d.summary->'online_plan'->'requested') AND a.status='pending' AND a.expires_at>NOW() AND o.document->'automation'->>'mode'='automatic' AND o.document->'automation'->>'enabled'='true' AND o.document->'automation'->>'require_approval'='false' AND NOT EXISTS(SELECT 1 FROM patch_automation_runs r WHERE r.approval_id=a.id) ORDER BY a.created_at,a.id LIMIT 25`)
	if e != nil {
		return e
	}
	type entry struct {
		tenant, approval uuid.UUID
		revision         int64
		actor            string
	}
	batch := []entry{}
	for rows.Next() {
		var v entry
		if e = rows.Scan(&v.tenant, &v.approval, &v.revision, &v.actor); e != nil {
			rows.Close()
			return e
		}
		batch = append(batch, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, v := range batch {
		actor, err := uuid.Parse(v.actor)
		if err != nil {
			continue
		}
		_, _ = s.QueuePatchAutomationRun(ctx, v.tenant, v.approval, actor, v.revision, time.Time{}, "tenant_policy")
	}
	return nil
}

// This check is also called by the agent's under-APT-lock authorization hook.
func (s *Store) CheckPatchAutomationExecution(ctx context.Context, tenant, id, job uuid.UUID) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revision int64
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT revision,document FROM patch_review_operations WHERE tenant_id=$1 FOR SHARE`, tenant).Scan(&revision, &raw)
	if e != nil {
		return e
	}
	var p patchgovernance.Operations
	if json.Unmarshal(raw, &p) != nil {
		return fmt.Errorf("invalid policy")
	}
	var r PatchAutomationRun
	r, e = scanAutomation(tx.QueryRowContext(ctx, `SELECT `+automationColumns+` FROM patch_automation_runs WHERE id=$1 AND tenant_id=$2`, id, tenant).Scan)
	if e != nil {
		return e
	}
	if r.Status != "dispatched" || !r.ExecutionJobID.Valid || r.ExecutionJobID.UUID != job || r.PolicyRevision != revision || !p.Automation.Enabled || p.Automation.ValidUntil == nil || !p.Automation.ValidUntil.After(time.Now()) || !r.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("automation authorization withdrawn, expired or changed")
	}
	if e = automationRole(ctx, tx, tenant, r.ActorID, r.Authority); e != nil {
		return e
	}
	var node uuid.UUID
	e = tx.QueryRowContext(ctx, `SELECT node_id FROM patch_approvals WHERE id=$1 AND tenant_id=$2`, r.ApprovalID, tenant).Scan(&node)
	if e != nil {
		return e
	}
	if e = checkAutomationHealthTx(ctx, tx, tenant, node); e != nil {
		return e
	}
	b, e := loadAutomationApproval(ctx, tx, tenant, r.ApprovalID)
	if e != nil {
		return e
	}
	if e = automationScope(p, b, time.Now().UTC()); e != nil {
		return e
	}
	if b.hash != r.PolicyHash || b.status != "approved" {
		return fmt.Errorf("retained execution approval changed")
	}
	if r.Authority == "tenant_policy" && (p.Automation.Mode != "automatic" || p.Automation.RequireApproval || slices.Contains(p.Automation.CanaryNodeIDs, b.node.String())) {
		return fmt.Errorf("automatic execution authority changed")
	}
	return automationCohortGateTx(ctx, tx, tenant, revision, p.Automation, b)
}

func automationCohortGateTx(ctx context.Context, tx *sql.Tx, tenant uuid.UUID, revision int64, policy patchgovernance.AutomationPolicy, b automationBinding) error {
	now := time.Now().UTC()
	// A failed, cancelled, partial or unknown previous execution stops the policy
	// revision. A fresh policy revision and exact review are required to resume.
	var stopped bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM patch_automation_runs r JOIN jobs j ON j.id=r.execution_job_id LEFT JOIN node_patch_state n ON n.job_id=j.id LEFT JOIN patch_verifications v ON v.job_id=j.id WHERE r.tenant_id=$1 AND r.policy_revision=$2 AND (j.status IN ('failed','cancelled') OR n.status='failed' OR v.state IN ('execution_failed','verification_failed')))`, tenant, revision).Scan(&stopped)
	if e != nil {
		return e
	}
	if stopped {
		return fmt.Errorf("circuit breaker: a policy execution failed or was cancelled")
	}
	// A later wave cannot run while an earlier completed wave has an unknown,
	// stale or pending verification. Live queued/running jobs are wave-limited
	// separately; no blanket retry is generated for an uncertain outcome.
	rows, err := tx.QueryContext(ctx, `SELECT r.execution_job_id FROM patch_automation_runs r JOIN jobs j ON j.id=r.execution_job_id JOIN patch_approvals a ON a.id=r.approval_id WHERE r.tenant_id=$1 AND r.policy_revision=$2 AND j.status='succeeded' AND a.node_id<>$3`, tenant, revision, b.node)
	if err != nil {
		return err
	}
	var prior []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		prior = append(prior, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range prior {
		var verified bool
		if err = tx.QueryRowContext(ctx, patchPromotionQuery, id).Scan(&verified); err != nil {
			return err
		}
		if !verified {
			return fmt.Errorf("previous wave verification is unknown, expired or incomplete")
		}
	}
	if !slices.Contains(policy.CanaryNodeIDs, b.node.String()) {
		for _, canary := range policy.CanaryNodeIDs {
			var job uuid.UUID
			var completed time.Time
			var planRaw []byte
			e = tx.QueryRowContext(ctx, `SELECT r.execution_job_id,v.execution_completed_at,d.summary->'online_plan' FROM patch_automation_runs r JOIN patch_approvals a ON a.id=r.approval_id JOIN patch_deployments d ON d.id=a.deployment_id JOIN patch_verifications v ON v.job_id=r.execution_job_id WHERE r.tenant_id=$1 AND r.policy_revision=$2 AND a.node_id=$3 AND r.status='dispatched' AND r.authority='individual_approval' ORDER BY r.created_at DESC LIMIT 1`, tenant, revision, canary).Scan(&job, &completed, &planRaw)
			if e != nil {
				return fmt.Errorf("individually approved verified canary required")
			}
			var ready bool
			if e := tx.QueryRowContext(ctx, patchPromotionQuery, job).Scan(&ready); e != nil {
				return e
			}
			if !ready || completed.Add(time.Duration(policy.ObservationMinutes)*time.Minute).After(now) {
				return fmt.Errorf("canary verification or observation period incomplete")
			}
			var verified patchsupport.OnlinePlan
			if json.Unmarshal(planRaw, &verified) != nil || verified.ArtifactsSHA256 == "" || verified.ArtifactsSHA256 != b.plan.ArtifactsSHA256 || verified.InputsSHA256 != b.plan.InputsSHA256 || !slices.Equal(verified.Requested, b.plan.Requested) || !slices.Equal(verified.Resolved, b.plan.Resolved) {
				return fmt.Errorf("candidate versions or dependency closure differ from verified canary")
			}
		}
	}

	return nil
}
