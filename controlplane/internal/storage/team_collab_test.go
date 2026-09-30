package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
)

func TestListTenantUsersMatchingQuery(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Parallel()

	ctx := context.Background()
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts("../migrate/sql/0001_init.up.sql",
			"../migrate/sql/0003_auth.up.sql",
			"../migrate/sql/0005_seed_roles.up.sql",
			"../migrate/sql/0061_user_roles_nullable_tenant.up.sql"),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(ctx)) })

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	tenantID := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "test-tenant")
	require.NoError(t, err)

	// Create user A: "Ada CISO"
	ada, err := store.EnsureUser(ctx, "ada-external", "ada@example.com", "Ada CISO")
	require.NoError(t, err)

	// Create user B: "Bob Ops"
	bob, err := store.EnsureUser(ctx, "bob-external", "bob@example.com", "Bob Ops")
	require.NoError(t, err)

	// Create user C: viewer only (should be excluded)
	carol, err := store.EnsureUser(ctx, "carol-external", "carol@example.com", "Carol Viewer")
	require.NoError(t, err)

	// Assign roles via AssignRolesToUser (creates roles via ensureRole if
	// not already seeded, and inserts user_roles with tenant_id NULL).
	err = store.AssignRolesToUser(ctx, ada.ID, []string{"investigator"})
	require.NoError(t, err)
	err = store.AssignRolesToUser(ctx, bob.ID, []string{"operator"})
	require.NoError(t, err)
	err = store.AssignRolesToUser(ctx, carol.ID, []string{"viewer"})
	require.NoError(t, err)

	// Resolve role IDs after assignment (investigator may have been created by ensureRole).
	allRoles, err := store.ListRoles(ctx)
	require.NoError(t, err)
	roleByName := make(map[string]uuid.UUID, len(allRoles))
	for _, r := range allRoles {
		roleByName[r.Name] = r.ID
	}

	// Scoped tenant-role assignments: replicate the rows with a specific tenant_id
	// by inserting directly (AssignRolesToUser sets tenant_id NULL).
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, ada.ID, roleByName["investigator"], tenantID)
	require.NoError(t, err)

	_, err = store.db.ExecContext(ctx, `
		INSERT INTO user_roles (user_id, role_id, tenant_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
	`, bob.ID, roleByName["operator"], tenantID)
	require.NoError(t, err)

	// Query: search "ada" — should return only Ada.
	results, err := store.ListTenantUsers(ctx, tenantID, "ada", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ada.ID, results[0].ID)
	require.Equal(t, "Ada CISO", results[0].Name)
	require.Equal(t, "ada@example.com", results[0].Email)

	// Query: search "bob" — should return only Bob.
	results, err = store.ListTenantUsers(ctx, tenantID, "bob", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, bob.ID, results[0].ID)

	// Query: search "ADA" (upper/mixed case) — should still return Ada,
	// matching is case-insensitive.
	results, err = store.ListTenantUsers(ctx, tenantID, "ADA", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, ada.ID, results[0].ID)
	require.Equal(t, "Ada CISO", results[0].Name)
	require.Equal(t, "ada@example.com", results[0].Email)

	// Query: search "bOb o" (mixed case) — should still return Bob.
	results, err = store.ListTenantUsers(ctx, tenantID, "bOb o", 50)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, bob.ID, results[0].ID)

	// Query: empty query — should return both Ada and Bob (investigator/operator), no Carol.
	results, err = store.ListTenantUsers(ctx, tenantID, "", 50)
	require.NoError(t, err)
	require.Len(t, results, 2)

	// Query: limit 1.
	results, err = store.ListTenantUsers(ctx, tenantID, "", 1)
	require.NoError(t, err)
	require.Len(t, results, 1)
}

func TestNotificationLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Parallel()

	ctx := context.Background()
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts(
			"../migrate/sql/0001_init.up.sql",
			"../migrate/sql/0003_auth.up.sql",
			"../migrate/sql/0093_ai_operator_persistence.up.sql",
			"../migrate/sql/0149_team_collaboration.up.sql",
		),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(ctx)) })

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	// Seed a tenant so FK constraints are satisfied.
	tenantID := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "test-tenant")
	require.NoError(t, err)

	// Create two users: R (recipient) and A (actor).
	recipient, err := store.EnsureUser(ctx, "recipient-ext", "recipient@example.com", "Recipient User")
	require.NoError(t, err)
	actor, err := store.EnsureUser(ctx, "actor-ext", "actor@example.com", "Actor User")
	require.NoError(t, err)

	// Create a case in ai_investigations so the FK is satisfied.
	caseID := uuid.New()
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO ai_investigations (id, tenant_id, trigger_type, trigger_event_type, trigger_dedup_key, summary)
		VALUES ($1, $2, 'manual', 'note', $3, 'test case')
	`, caseID, tenantID, "dedup-"+caseID.String())
	require.NoError(t, err)

	// Insert two notifications for the recipient: case_assigned, case_mentioned.
	n1, err := store.CreateNotification(ctx, CreateNotificationParams{
		TenantID:    tenantID,
		RecipientID: recipient.ID,
		ActorID:     actor.ID,
		Kind:        "case_assigned",
		CaseID:      caseID,
		CaseTitle:   "Suspicious Login",
	})
	require.NoError(t, err)
	require.Equal(t, "case_assigned", n1.Kind)
	require.False(t, n1.CreatedAt.IsZero())

	// Ensure distinct created_at so newest-first ordering is deterministic.
	time.Sleep(2 * time.Millisecond)

	n2, err := store.CreateNotification(ctx, CreateNotificationParams{
		TenantID:    tenantID,
		RecipientID: recipient.ID,
		ActorID:     actor.ID,
		Kind:        "case_mentioned",
		CaseID:      caseID,
		CaseTitle:   "Suspicious Login",
	})
	require.NoError(t, err)
	require.Equal(t, "case_mentioned", n2.Kind)

	// List all: should return 2 newest-first.
	items, total, err := store.ListNotifications(ctx, NotificationFilter{TenantID: tenantID, RecipientID: recipient.ID}, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, items, 2)
	require.Equal(t, n2.ID, items[0].ID, "newest first")
	require.Equal(t, n1.ID, items[1].ID)

	// Unread-only filter.
	items, total, err = store.ListNotifications(ctx, NotificationFilter{TenantID: tenantID, RecipientID: recipient.ID, UnreadOnly: true}, 50, 0)
	require.NoError(t, err)
	require.Equal(t, 2, total)
	require.Len(t, items, 2)

	// Count unread: 2.
	count, err := store.CountUnreadNotifications(ctx, tenantID, recipient.ID)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	// Mark n1 read.
	err = store.MarkNotificationRead(ctx, tenantID, recipient.ID, n1.ID)
	require.NoError(t, err)

	// Count unread: 1.
	count, err = store.CountUnreadNotifications(ctx, tenantID, recipient.ID)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Mark all read.
	affected, err := store.MarkAllNotificationsRead(ctx, tenantID, recipient.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), affected, "only the remaining unread notification should be affected")

	// Count unread: 0.
	count, err = store.CountUnreadNotifications(ctx, tenantID, recipient.ID)
	require.NoError(t, err)
	require.Equal(t, 0, count)

	// Marking a notification in a different tenant should affect 0 rows.
	otherTenant := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, otherTenant, "other-tenant")
	require.NoError(t, err)
	err = store.MarkNotificationRead(ctx, otherTenant, recipient.ID, n1.ID)
	require.NoError(t, err)

	// MarkAll for another tenant should return 0 affected.
	affected, err = store.MarkAllNotificationsRead(ctx, otherTenant, recipient.ID)
	require.NoError(t, err)
	require.Equal(t, int64(0), affected)
}

func TestAssignCaseOwnership(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Parallel()

	ctx := context.Background()
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts(
			"../migrate/sql/0001_init.up.sql",
			"../migrate/sql/0003_auth.up.sql",
			"../migrate/sql/0044_alerts.up.sql",
			"../migrate/sql/0064_entity_tags.up.sql",
			"../migrate/sql/0093_ai_operator_persistence.up.sql",
			"../migrate/sql/0138_alert_cases.up.sql",
			"../migrate/sql/0148_team_activity.up.sql",
			"../migrate/sql/0149_team_collaboration.up.sql",
		),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(ctx)) })

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	tenantID := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "test-tenant")
	require.NoError(t, err)
	assignee, err := store.EnsureUser(ctx, "assignee-ext", "assignee@example.com", "Assignee")
	require.NoError(t, err)
	assignedBy, err := store.EnsureUser(ctx, "assigner-ext", "assigner@example.com", "Assigner")
	require.NoError(t, err)
	reassignedBy, err := store.EnsureUser(ctx, "reassigner-ext", "reassigner@example.com", "Reassigner")
	require.NoError(t, err)
	caseID := uuid.New()
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO ai_investigations (id, tenant_id, trigger_type, trigger_event_type, trigger_dedup_key, summary)
		VALUES ($1, $2, 'manual', 'assignment', $3, 'test case')
	`, caseID, tenantID, "dedup-"+caseID.String())
	require.NoError(t, err)
	_, err = store.db.ExecContext(ctx, `UPDATE ai_investigations SET updated_at = NOW() - INTERVAL '1 hour' WHERE id = $1`, caseID)
	require.NoError(t, err)
	var createdUpdatedAt time.Time
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT updated_at FROM ai_investigations WHERE id = $1`, caseID).Scan(&createdUpdatedAt))

	now := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.AssignCase(ctx, tenantID, caseID, assignee.ID, assignedBy.ID, now))
	gotAssignee, err := store.CaseAssignee(ctx, tenantID, caseID)
	require.NoError(t, err)
	require.Equal(t, assignee.ID, gotAssignee)
	var originalAssignedBy uuid.UUID
	var originalAssignedAt time.Time
	var originalUpdatedAt time.Time
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT assigned_by, assigned_at, updated_at FROM ai_investigations WHERE id = $1`, caseID).Scan(&originalAssignedBy, &originalAssignedAt, &originalUpdatedAt))
	require.Equal(t, assignedBy.ID, originalAssignedBy)
	require.True(t, originalUpdatedAt.After(createdUpdatedAt))

	loaded, err := store.GetAIInvestigation(ctx, caseID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, uuid.NullUUID{UUID: assignee.ID, Valid: true}, loaded.AssigneeID)
	items, _, err := store.ListAIInvestigations(ctx, ListAIInvestigationsFilter{TenantID: tenantID}, 50, 0)
	require.NoError(t, err)
	require.Contains(t, items, *loaded)

	created, err := store.CreateAIInvestigation(ctx, CreateAIInvestigationParams{
		TenantID:         tenantID,
		TriggerType:      "manual",
		TriggerEventType: "created",
		TriggerDedupKey:  "created-" + uuid.NewString(),
		Summary:          "created case",
		CreatedBy:        assignedBy.ID,
	})
	require.NoError(t, err)
	require.False(t, created.AssigneeID.Valid)

	// Reassigning the same owner must preserve the original assignment attribution.
	require.NoError(t, store.AssignCase(ctx, tenantID, caseID, assignee.ID, reassignedBy.ID, now.Add(time.Minute)))
	var gotAssignedBy uuid.UUID
	var gotAssignedAt time.Time
	var gotUpdatedAt time.Time
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT assigned_by, assigned_at, updated_at FROM ai_investigations WHERE id = $1`, caseID).Scan(&gotAssignedBy, &gotAssignedAt, &gotUpdatedAt))
	require.Equal(t, originalAssignedBy, gotAssignedBy)
	require.Equal(t, originalAssignedAt, gotAssignedAt)
	require.Equal(t, originalUpdatedAt, gotUpdatedAt)

	require.NoError(t, store.AssignCase(ctx, tenantID, caseID, uuid.Nil, assignedBy.ID, now.Add(2*time.Minute)))
	gotAssignee, err = store.CaseAssignee(ctx, tenantID, caseID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, gotAssignee)
	var clearedUpdatedAt time.Time
	var clearedAssignedBy uuid.UUID
	var clearedAssignedAt time.Time
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT assigned_by, assigned_at, updated_at FROM ai_investigations WHERE id = $1`, caseID).Scan(&clearedAssignedBy, &clearedAssignedAt, &clearedUpdatedAt))
	require.True(t, clearedUpdatedAt.After(originalUpdatedAt))

	require.NoError(t, store.AssignCase(ctx, tenantID, caseID, uuid.Nil, reassignedBy.ID, now.Add(3*time.Minute)))
	gotAssignee, err = store.CaseAssignee(ctx, tenantID, caseID)
	require.NoError(t, err)
	require.Equal(t, uuid.Nil, gotAssignee)
	var clearedAgainAssignedBy uuid.UUID
	var clearedAgainAssignedAt time.Time
	var clearedAgainUpdatedAt time.Time
	require.NoError(t, store.db.QueryRowContext(ctx, `SELECT assigned_by, assigned_at, updated_at FROM ai_investigations WHERE id = $1`, caseID).Scan(&clearedAgainAssignedBy, &clearedAgainAssignedAt, &clearedAgainUpdatedAt))
	require.Equal(t, clearedAssignedBy, clearedAgainAssignedBy)
	require.Equal(t, clearedAssignedAt, clearedAgainAssignedAt)
	require.Equal(t, clearedUpdatedAt, clearedAgainUpdatedAt)
}

func TestCaseMentionedUsers(t *testing.T) {
	if testing.Short() {
		t.Skip("requires database")
	}
	t.Parallel()

	ctx := context.Background()
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts(
			"../migrate/sql/0001_init.up.sql",
			"../migrate/sql/0003_auth.up.sql",
			"../migrate/sql/0044_alerts.up.sql",
			"../migrate/sql/0064_entity_tags.up.sql",
			"../migrate/sql/0093_ai_operator_persistence.up.sql",
			"../migrate/sql/0138_alert_cases.up.sql",
			"../migrate/sql/0148_team_activity.up.sql",
			"../migrate/sql/0149_team_collaboration.up.sql",
		),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, pg.Terminate(ctx)) })

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	store, err := New(zap.NewNop(), config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })

	tenantID := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, tenantID, "test-tenant")
	require.NoError(t, err)
	caseID := uuid.New()
	_, err = store.db.ExecContext(ctx, `
		INSERT INTO ai_investigations (id, tenant_id, trigger_type, trigger_event_type, trigger_dedup_key, summary)
		VALUES ($1, $2, 'manual', 'mentions', $3, 'test case')
	`, caseID, tenantID, "dedup-"+caseID.String())
	require.NoError(t, err)

	mentionedA := uuid.New()
	mentionedB := uuid.New()
	resourceID := caseID.String()
	_, err = store.CreateAuditLog(ctx, &AuditLog{
		TenantID:     tenantID,
		Action:       "soc.case.note.add",
		ResourceType: "ai_investigation",
		ResourceID:   &resourceID,
		Metadata: map[string]any{
			"mentions": []string{mentionedA.String(), mentionedB.String(), "not-a-uuid"},
		},
	})
	require.NoError(t, err)
	_, err = store.CreateAuditLog(ctx, &AuditLog{
		TenantID:     tenantID,
		Action:       "soc.case.note.add",
		ResourceType: "ai_investigation",
		ResourceID:   &resourceID,
		Metadata: map[string]any{
			"mentions": []string{mentionedB.String()},
		},
	})
	require.NoError(t, err)
	for _, mentions := range []any{
		"not-an-array",
		map[string]string{"id": mentionedA.String()},
		nil,
	} {
		_, err = store.CreateAuditLog(ctx, &AuditLog{
			TenantID:     tenantID,
			Action:       "soc.case.note.add",
			ResourceType: "ai_investigation",
			ResourceID:   &resourceID,
			Metadata:     map[string]any{"mentions": mentions},
		})
		require.NoError(t, err)
	}

	otherTenantID := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO tenants (id, name) VALUES ($1, $2)`, otherTenantID, "other-tenant")
	require.NoError(t, err)
	_, err = store.CreateAuditLog(ctx, &AuditLog{
		TenantID:     otherTenantID,
		Action:       "soc.case.note.add",
		ResourceType: "ai_investigation",
		ResourceID:   &resourceID,
		Metadata:     map[string]any{"mentions": []string{uuid.NewString()}},
	})
	require.NoError(t, err)
	otherCaseID := uuid.New().String()
	_, err = store.CreateAuditLog(ctx, &AuditLog{
		TenantID:     tenantID,
		Action:       "soc.case.note.add",
		ResourceType: "ai_investigation",
		ResourceID:   &otherCaseID,
		Metadata:     map[string]any{"mentions": []string{uuid.NewString()}},
	})
	require.NoError(t, err)

	mentioned, err := store.CaseMentionedUsers(ctx, tenantID, caseID)
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{mentionedA, mentionedB}, mentioned)
}
