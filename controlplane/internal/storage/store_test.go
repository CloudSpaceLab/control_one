package storage

import (
	"context"
	"encoding/json"
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

func TestJobLifecycleWithPostgres(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	require.NoError(t, store.Ping(ctx))

	payload := map[string]any{"task": "provision"}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	job, err := store.CreateJob(ctx, &Job{
		Type:       "provision",
		Status:     JobStatusQueued,
		Payload:    payloadBytes,
		MaxRetries: 5,
	}, &JobEvent{Status: JobStatusQueued, Message: "queued"})
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, job.ID)
	require.Equal(t, JobStatusQueued, job.Status)

	updateFields := map[string]any{"started_at": time.Now()}
	require.NoError(t, store.UpdateJobStatus(ctx, job.ID, JobStatusRunning, "started", updateFields))

	finishFields := map[string]any{
		"finished_at": time.Now(),
		"retries":     1,
	}
	require.NoError(t, store.UpdateJobStatus(ctx, job.ID, JobStatusSucceeded, "done", finishFields))

	loaded, err := store.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, JobStatusSucceeded, loaded.Status)
	require.Equal(t, 1, loaded.Retries)
	require.NotNil(t, loaded.StartedAt)
	require.NotNil(t, loaded.FinishedAt)

	events, err := store.ListJobEvents(ctx, job.ID)
	require.NoError(t, err)
	require.Len(t, events, 3)
	require.Equal(t, JobStatusQueued, events[0].Status)
	require.Equal(t, JobStatusRunning, events[1].Status)
	require.Equal(t, JobStatusSucceeded, events[2].Status)
}

func TestComplianceResultsPersistence(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := setupPostgresStore(t, ctx)

	require.NoError(t, store.Ping(ctx))

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "tenant-a"})
	require.NoError(t, err)

	node, err := store.CreateNode(ctx, &Node{
		ID:       uuid.New(),
		TenantID: tenant.ID,
		Hostname: "node-a",
	})
	require.NoError(t, err)

	job, err := store.CreateJob(ctx, &Job{
		TenantID: tenant.ID,
		Type:     "compliance.scan",
		Status:   JobStatusQueued,
	}, nil)
	require.NoError(t, err)

	scanID := "scan-1"
	severity := "high"
	details := "missing baseline"
	remediation := "apply baseline"
	checkedAt := time.Now().UTC()
	metadata := map[string]any{"control": "cis-1.1"}

	results := []ComplianceResult{
		{
			JobID:       job.ID,
			TenantID:    tenant.ID,
			NodeID:      node.ID,
			ScanID:      &scanID,
			RuleID:      "rule-123",
			Passed:      false,
			Severity:    &severity,
			Details:     &details,
			Remediation: &remediation,
			Metadata:    metadata,
			CheckedAt:   &checkedAt,
		},
	}

	require.NoError(t, store.CreateComplianceResults(ctx, results))

	stored, err := store.ListComplianceResults(ctx, job.ID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	result := stored[0]
	require.Equal(t, job.ID, result.JobID)
	require.Equal(t, tenant.ID, result.TenantID)
	require.Equal(t, node.ID, result.NodeID)
	require.NotNil(t, result.ScanID)
	require.Equal(t, scanID, *result.ScanID)
	require.NotNil(t, result.Severity)
	require.Equal(t, severity, *result.Severity)
	require.NotNil(t, result.Details)
	require.Equal(t, details, *result.Details)
	require.NotNil(t, result.Remediation)
	require.Equal(t, remediation, *result.Remediation)
	require.NotNil(t, result.CheckedAt)
	require.WithinDuration(t, checkedAt, *result.CheckedAt, time.Second)
	require.Equal(t, metadata["control"], result.Metadata["control"])
}

func setupPostgresStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.Run(ctx, "docker.io/postgres:16-alpine",
		postgres.WithInitScripts(
			"../migrate/sql/0001_init.up.sql",
			"../migrate/sql/0002_jobs.up.sql",
			"../migrate/sql/0003_auth.up.sql",
			"../migrate/sql/0004_provisioning_templates.up.sql",
			"../migrate/sql/0005_seed_roles.up.sql",
			"../migrate/sql/0006_compliance_results.up.sql",
		),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pg.Terminate(ctx))
	})

	connStr, err := pg.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	logger := zap.NewNop()
	store, err := New(logger, config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	return store
}
