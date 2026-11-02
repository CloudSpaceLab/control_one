package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPatchReviewPaginationAndCompleteExportDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive})
	require.NoError(t, err)
	batch := []VulnerabilityFinding{}
	now := time.Now().UTC()
	for i := 0; i < 103; i++ {
		batch = append(batch, testVulnerabilityFinding(tenant.ID, node.ID, "liba", fmt.Sprintf("CVE-PAGE-%04d", i), "isolated", now))
	}
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, batch))
	_, err = s.db.ExecContext(ctx, `WITH d AS (INSERT INTO patch_deployments(tenant_id,mode,status,target_node_count,summary) SELECT $1,'direct','pending',1,'{}'::jsonb FROM generate_series(1,105) RETURNING id) INSERT INTO patch_approvals(id,tenant_id,deployment_id,node_id,mode,status,expires_at,policy_hash) SELECT gen_random_uuid(),$1,d.id,$2,'direct','pending',NOW()+INTERVAL '1 hour','isolated-export-fixture' FROM d`, tenant.ID, node.ID)
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document) SELECT $1,'finding_reviewed',0,'isolated','{}'::jsonb FROM generate_series(1,1105)`, tenant.ID)
	require.NoError(t, err)
	seen := map[uuid.UUID]bool{}
	cursor := uuid.Nil
	for {
		page, e := s.GetPatchReviewPage(ctx, tenant.ID, cursor, 25)
		require.NoError(t, e)
		require.Equal(t, 103, page.Total)
		for _, f := range page.Findings {
			require.False(t, seen[f.ID])
			seen[f.ID] = true
		}
		if page.Next == "" {
			break
		}
		cursor = uuid.MustParse(page.Next)
	}
	require.Len(t, seen, 103)
	before := int64(0)
	eventCount := 0
	for {
		events, next, e := s.GetPatchReviewEventPage(ctx, tenant.ID, 30, before, 25)
		require.NoError(t, e)
		eventCount += len(events)
		if next == 0 {
			break
		}
		require.True(t, before == 0 || next < before)
		before = next
	}
	require.Equal(t, 1105, eventCount)
	var body bytes.Buffer
	var manifest PatchReviewArchiveInput
	require.NoError(t, s.WritePatchReviewExport(ctx, tenant.ID, &body, &manifest))
	var exported struct {
		Complete bool              `json:"complete"`
		Counts   map[string]int    `json:"counts"`
		Findings []json.RawMessage `json:"findings"`
		Events   []json.RawMessage `json:"events"`
	}
	require.NoError(t, json.Unmarshal(body.Bytes(), &exported))
	require.True(t, exported.Complete)
	require.Len(t, exported.Findings, 103)
	require.Len(t, exported.Events, 1105)
	require.Equal(t, 1105, manifest.Counts["events"])
	require.Equal(t, 105, manifest.Counts["approval_history"], "complete export must exceed the former approval limit")
	other, e := s.GetPatchReviewPage(ctx, uuid.New(), uuid.Nil, 25)
	require.NoError(t, e)
	require.Empty(t, other.Findings)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, s.WritePatchReviewExport(cancelled, tenant.ID, &bytes.Buffer{}, nil))
}

func TestPatchReviewArchiveRetentionDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	ops := patchgovernance.DefaultOperations()
	ops.Owner = "Isolated owner"
	ops.Reason = "Isolated retention test"
	ops.Retention.Mode = "archive_then_purge"
	saved, err := s.SavePatchReviewOperations(ctx, tenant.ID, 0, ops, "fixture-admin")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `INSERT INTO patch_governance_events(tenant_id,kind,revision,actor,document,created_at) SELECT $1,'finding_reviewed',0,'isolated','{}'::jsonb,NOW()-INTERVAL '400 days' FROM generate_series(1,3)`, tenant.ID)
	require.NoError(t, err)
	p, err := s.PreviewPatchReviewRetention(ctx, tenant.ID)
	require.NoError(t, err)
	require.Equal(t, 3, p.Count)
	require.Contains(t, p.Blocked, "Legal hold")
	_, err = s.PurgePatchReviewRetention(ctx, tenant.ID, p.Revision, p.Cutoff, p.SHA256, uuid.New(), "fixture-admin", "isolated archive")
	require.Error(t, err)
	ops.Retention.LegalHold = false
	saved, err = s.SavePatchReviewOperations(ctx, tenant.ID, saved.Revision, ops, "fixture-admin")
	require.NoError(t, err)
	_, err = s.PurgePatchReviewRetention(ctx, tenant.ID, p.Revision, p.Cutoff, p.SHA256, uuid.New(), "fixture-admin", "isolated archive")
	require.ErrorIs(t, err, ErrGovernanceConflict)
	p, err = s.PreviewPatchReviewRetention(ctx, tenant.ID)
	require.NoError(t, err)
	require.Empty(t, p.Blocked)
	_, err = s.PurgePatchReviewRetention(ctx, tenant.ID, p.Revision, p.Cutoff, p.SHA256, uuid.New(), "fixture-admin", "isolated archive")
	require.Error(t, err)
	var body bytes.Buffer
	var manifest PatchReviewArchiveInput
	require.NoError(t, s.WritePatchReviewExport(ctx, tenant.ID, &body, &manifest))
	hash := sha256.Sum256(body.Bytes())
	archive, err := s.RecordPatchReviewArchive(ctx, tenant.ID, "fixture-admin", hex.EncodeToString(hash[:]), manifest)
	require.NoError(t, err)
	_, err = s.PurgePatchReviewRetention(ctx, tenant.ID, p.Revision, p.Cutoff, p.SHA256, archive, "wrong-actor", "isolated archive")
	require.Error(t, err)
	deleted, err := s.PurgePatchReviewRetention(ctx, tenant.ID, p.Revision, p.Cutoff, p.SHA256, archive, "fixture-admin", "isolated verified archive")
	require.NoError(t, err)
	require.Equal(t, 3, deleted)
	_, err = s.PurgePatchReviewRetention(ctx, tenant.ID, p.Revision, p.Cutoff, p.SHA256, archive, "fixture-admin", "isolated archive")
	require.ErrorIs(t, err, ErrGovernanceConflict)
	var receipts int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM patch_governance_events WHERE tenant_id=$1 AND kind='retention_purged'`, tenant.ID).Scan(&receipts))
	require.Equal(t, 1, receipts)
	current, err := s.GetPatchReviewOperations(ctx, tenant.ID)
	require.NoError(t, err)
	require.Equal(t, saved.Revision, current.Revision)
}

func TestPatchReviewEscalationAtomicDeliveryDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	user, err := s.EnsureUser(ctx, uuid.NewString(), uuid.NewString()+"@example.invalid", "Isolated reviewer")
	require.NoError(t, err)
	require.NoError(t, s.AssignRolesToUser(ctx, user.ID, []string{"operator"}))
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive})
	require.NoError(t, err)
	require.NoError(t, s.UpsertVulnerabilityFindings(ctx, []VulnerabilityFinding{testVulnerabilityFinding(tenant.ID, node.ID, "liba", "CVE-NOTIFICATION-TEST", "isolated", time.Now().UTC())}))
	ops := patchgovernance.DefaultOperations()
	ops.Owner = "Isolated owner"
	ops.Reason = "Isolated notification test"
	ops.Notifications.Enabled = true
	ops.Notifications.Recipients = []string{user.ID.String()}
	_, err = s.SavePatchReviewOperations(ctx, tenant.ID, 0, ops, "fixture-admin")
	require.NoError(t, err)
	_, err = s.db.ExecContext(ctx, `ALTER TABLE patch_review_escalation_receipts ADD CONSTRAINT patch_review_fixture_fail CHECK(false) NOT VALID`)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = s.db.ExecContext(ctx, `ALTER TABLE patch_review_escalation_receipts DROP CONSTRAINT IF EXISTS patch_review_fixture_fail`)
	})
	_, err = s.DispatchPatchReviewEscalations(ctx, tenant.ID)
	require.Error(t, err)
	var notifications int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM notifications WHERE tenant_id=$1`, tenant.ID).Scan(&notifications))
	require.Zero(t, notifications, "notification and receipt must roll back together")
	_, err = s.db.ExecContext(ctx, `ALTER TABLE patch_review_escalation_receipts DROP CONSTRAINT patch_review_fixture_fail`)
	require.NoError(t, err)
	var wg sync.WaitGroup
	counts := make(chan int, 2)
	failures := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, e := s.DispatchPatchReviewEscalations(ctx, tenant.ID)
			counts <- n
			failures <- e
		}()
	}
	wg.Wait()
	close(counts)
	close(failures)
	sum := 0
	for n := range counts {
		sum += n
	}
	for e := range failures {
		require.NoError(t, e)
	}
	require.Equal(t, 1, sum)
	n, err := s.DispatchPatchReviewEscalations(ctx, tenant.ID)
	require.NoError(t, err)
	require.Zero(t, n)
	require.NoError(t, s.SetUserRoles(ctx, user.ID, []string{}))
	n, err = s.DispatchPatchReviewEscalations(ctx, tenant.ID)
	require.NoError(t, err)
	require.Zero(t, n)
	var jobs int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, tenant.ID).Scan(&jobs))
	require.Zero(t, jobs)
}

func TestPatchReviewAutomationDraftTenantScopeDedicatedPostgres(t *testing.T) {
	s := phase2SnapshotTestStore(t)
	ctx := context.Background()
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString(), State: NodeStateActive})
	require.NoError(t, err)
	window, err := s.CreateMaintenanceWindow(ctx, MaintenanceWindow{TenantID: tenant.ID, Name: "Isolated automation draft", NodeIDs: []uuid.UUID{node.ID}, OpensAt: time.Now().Add(time.Hour), ClosesAt: time.Now().Add(2 * time.Hour)})
	require.NoError(t, err)
	o := patchgovernance.DefaultOperations()
	o.Owner = "Isolated owner"
	o.Reason = "Disabled draft scope verification"
	end := time.Now().Add(time.Hour)
	o.Automation = patchgovernance.AutomationPolicy{Mode: "approved_scheduled", NodeIDs: []string{node.ID.String()}, CanaryNodeIDs: []string{node.ID.String()}, Packages: []string{"liblzma5"}, Exclusions: []string{}, WindowID: window.ID.String(), Transport: "direct", MaxWaveSize: 1, HealthProfile: "Unqualified fixture probes", ValidUntil: &end, RequireApproval: true, RequireExactPlan: true, RequireFreshAssessment: true, RequireHealth: true, RequireRebootVerification: true, StopOnFailure: true}
	saved, err := s.SavePatchReviewOperations(ctx, tenant.ID, 0, o, "fixture-admin")
	require.NoError(t, err)
	require.False(t, saved.Policy.Automation.Enabled)
	o.Automation.Enabled = true
	_, err = s.SavePatchReviewOperations(ctx, tenant.ID, saved.Revision, o, "fixture-admin")
	require.ErrorContains(t, err, "qualified")
	o.Automation.Enabled = false
	o.Automation.WindowID = uuid.NewString()
	_, err = s.SavePatchReviewOperations(ctx, tenant.ID, saved.Revision, o, "fixture-admin")
	require.Error(t, err)
	var jobs int
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT count(*) FROM jobs WHERE tenant_id=$1`, tenant.ID).Scan(&jobs))
	require.Zero(t, jobs)
}
