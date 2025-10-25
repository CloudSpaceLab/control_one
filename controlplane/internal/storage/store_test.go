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
	if _, _, err := testcontainers.DockerImageAuth(ctx, "postgres:latest"); err != nil {
		t.Skipf("skipping: docker daemon unavailable: %v", err)
	}

	pg, err := postgres.RunContainer(ctx,
		postgres.WithInitScripts("../migrate/sql/0001_init.up.sql", "../migrate/sql/0002_jobs.up.sql"),
		postgres.WithDatabase("control_one"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pg.Terminate(ctx))
	})

	connStr, err := pg.ConnectionString(ctx)
	require.NoError(t, err)

	logger := zap.NewNop()
	store, err := New(logger, config.DatabaseConfig{URL: connStr}, Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

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
