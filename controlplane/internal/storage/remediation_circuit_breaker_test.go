package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCircuitBreaker_TripAndAck(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "cb-trip-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	ruleID := "rule-cb-" + uuid.NewString()[:4]

	// Nothing seeded initially.
	state, err := store.GetCircuitBreakerState(ctx, tenant.ID, ruleID)
	require.NoError(t, err)
	require.Nil(t, state)

	tripped, err := store.TripCircuitBreaker(ctx, tenant.ID, ruleID, "fail rate 50%")
	require.NoError(t, err)
	require.NotNil(t, tripped)
	require.Equal(t, "fail rate 50%", tripped.TrippedReason)
	require.Nil(t, tripped.AckedAt)

	// Double-trip is idempotent — refreshes tripped_at, keeps ack unset.
	tripped2, err := store.TripCircuitBreaker(ctx, tenant.ID, ruleID, "new reason")
	require.NoError(t, err)
	require.Equal(t, "new reason", tripped2.TrippedReason)
	require.Nil(t, tripped2.AckedAt)

	// Seed the approver user to satisfy the FK.
	approverID := uuid.New()
	_, err = store.db.ExecContext(ctx, `INSERT INTO users (id, external_id) VALUES ($1, $2)`, approverID, "ack-"+approverID.String())
	require.NoError(t, err)

	acked, err := store.AckCircuitBreaker(ctx, tenant.ID, ruleID, approverID)
	require.NoError(t, err)
	require.NotNil(t, acked)
	require.NotNil(t, acked.AckedAt)
	require.NotNil(t, acked.AckedBy)
	require.Equal(t, approverID, *acked.AckedBy)

	// Ack on missing rule returns ErrNoRows.
	_, err = store.AckCircuitBreaker(ctx, tenant.ID, "missing-rule", approverID)
	require.Error(t, err)
	require.True(t, errors.Is(err, sql.ErrNoRows))

	// Re-trip after ack should clear the ack.
	reTripped, err := store.TripCircuitBreaker(ctx, tenant.ID, ruleID, "second breach")
	require.NoError(t, err)
	require.Nil(t, reTripped.AckedAt)
	require.Nil(t, reTripped.AckedBy)
}

func TestRemediationFailRate_JobsSeeded(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "fr-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	ruleID := "rule-fr-" + uuid.NewString()[:4]

	// Seed 10 remediation.execute jobs: 4 failed, 6 succeeded.
	payload := map[string]any{"rule_id": ruleID}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	seedJob := func(status JobStatus) uuid.UUID {
		j, err := store.CreateJob(ctx, &Job{
			TenantID: tenant.ID,
			Type:     "remediation.execute",
			Status:   JobStatusQueued,
			Payload:  payloadBytes,
		}, nil)
		require.NoError(t, err)
		require.NoError(t, store.UpdateJobStatus(ctx, j.ID, status, "seeded", nil))
		return j.ID
	}

	for i := 0; i < 6; i++ {
		seedJob(JobStatusSucceeded)
	}
	for i := 0; i < 4; i++ {
		seedJob(JobStatusFailed)
	}

	rate, err := store.RemediationFailRate(ctx, tenant.ID, ruleID, time.Hour)
	require.NoError(t, err)
	require.NotNil(t, rate)
	require.Equal(t, 10, rate.Samples)
	require.Equal(t, 4, rate.Failed)
	require.Equal(t, 40, rate.Pct)
}

func TestRemediationFailRate_OutsideWindow(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "fr-old-" + uuid.NewString()[:6]})
	require.NoError(t, err)
	ruleID := "rule-fr-old-" + uuid.NewString()[:4]

	payload := map[string]any{"rule_id": ruleID}
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	j, err := store.CreateJob(ctx, &Job{
		TenantID: tenant.ID,
		Type:     "remediation.execute",
		Status:   JobStatusQueued,
		Payload:  payloadBytes,
	}, nil)
	require.NoError(t, err)
	require.NoError(t, store.UpdateJobStatus(ctx, j.ID, JobStatusFailed, "old failure", nil))

	// Backdate the job to 2 hours ago.
	_, err = store.db.ExecContext(ctx, `UPDATE jobs SET updated_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, j.ID)
	require.NoError(t, err)

	rate, err := store.RemediationFailRate(ctx, tenant.ID, ruleID, time.Hour)
	require.NoError(t, err)
	require.Equal(t, 0, rate.Samples, "old job should fall outside the 1h window")
	require.Equal(t, 0, rate.Failed)
	require.Equal(t, 0, rate.Pct)
}

func TestRemediationFailRate_Validation(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	_, err := store.RemediationFailRate(ctx, uuid.Nil, "x", time.Minute)
	require.Error(t, err)

	_, err = store.RemediationFailRate(ctx, uuid.New(), "", time.Minute)
	require.Error(t, err)

	_, err = store.RemediationFailRate(ctx, uuid.New(), "x", 0)
	require.Error(t, err)
}
