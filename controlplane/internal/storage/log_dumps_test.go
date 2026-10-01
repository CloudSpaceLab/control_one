package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLogDumpClaimChunkAndTerminalJobLifecycle(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)

	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "log-dump-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	node, err := store.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: "log-dump-node-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	job, err := store.CreateJob(ctx, &Job{TenantID: tenant.ID, Type: "log_dump", Status: JobStatusQueued, Payload: []byte(`{"node_id":"` + node.ID.String() + `"}`)}, nil)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Millisecond)
	dump, err := store.CreateLogDump(ctx, LogDump{
		TenantID:      tenant.ID,
		NodeID:        node.ID,
		JobID:         uuid.NullUUID{UUID: job.ID, Valid: true},
		Source:        LogDumpSourceNodeAgent,
		WindowStart:   now.Add(-15 * time.Minute),
		WindowEnd:     now,
		RetentionDays: 7,
		CreatedAt:     now,
		ExpiresAt:     now.Add(7 * 24 * time.Hour),
	})
	require.NoError(t, err)

	tokens := []string{shaHex("claim-a"), shaHex("claim-b")}
	type claimResult struct {
		token string
		err   error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, token := range tokens {
		token := token
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, claimErr := store.ClaimLogDump(ctx, tenant.ID, node.ID, dump.ID, job.ID, token, now, now.Add(time.Minute))
			results <- claimResult{token: token, err: claimErr}
		}()
	}
	wg.Wait()
	close(results)

	var winner string
	var held int
	for result := range results {
		if result.err == nil {
			winner = result.token
			continue
		}
		require.ErrorIs(t, result.err, ErrLogDumpClaimHeld)
		held++
	}
	require.NotEmpty(t, winner)
	require.Equal(t, 1, held)

	takeoverAt := now.Add(2 * time.Minute)
	newToken := shaHex("claim-takeover")
	claimed, err := store.ClaimLogDump(ctx, tenant.ID, node.ID, dump.ID, job.ID, newToken, takeoverAt, takeoverAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(2), claimed.ClaimGeneration)

	chunk := LogDumpChunk{
		DumpID:    dump.ID,
		TenantID:  tenant.ID,
		NodeID:    node.ID,
		JobID:     uuid.NullUUID{UUID: job.ID, Valid: true},
		Ordinal:   0,
		SHA256:    shaHex("chunk-0"),
		SizeBytes: 7,
		TempPath:  "/tmp/log-dump/chunk-0",
	}
	_, err = store.PutLogDumpChunk(ctx, chunk, winner, takeoverAt)
	require.ErrorIs(t, err, ErrLogDumpClaimInvalid)

	idempotent, err := store.PutLogDumpChunk(ctx, chunk, newToken, takeoverAt)
	require.NoError(t, err)
	require.False(t, idempotent)

	duplicate := chunk
	duplicate.TempPath = "/tmp/log-dump/retry-chunk-0"
	idempotent, err = store.PutLogDumpChunk(ctx, duplicate, newToken, takeoverAt)
	require.NoError(t, err)
	require.True(t, idempotent)

	conflict := chunk
	conflict.SHA256 = shaHex("different")
	_, err = store.PutLogDumpChunk(ctx, conflict, newToken, takeoverAt)
	require.ErrorIs(t, err, ErrLogDumpChunkConflict)

	available := true
	artifactSHA := shaHex("artifact")
	completeAt := takeoverAt.Add(15 * time.Second)
	err = store.CompleteLogDumpAndJob(ctx, tenant.ID, node.ID, dump.ID, job.ID, newToken,
		"/var/lib/control-one/log-dumps/dump.ndjson", artifactSHA, 1, 7, false, &available, "", completeAt)
	require.NoError(t, err)
	require.NoError(t, store.CompleteLogDumpAndJob(ctx, tenant.ID, node.ID, dump.ID, job.ID, newToken,
		"/var/lib/control-one/log-dumps/dump.ndjson", artifactSHA, 1, 7, false, &available, "", completeAt))

	gotJob, err := store.GetJob(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, JobStatusSucceeded, gotJob.Status)
	events, err := store.ListJobEvents(ctx, job.ID)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, JobStatusSucceeded, events[0].Status)

	got, err := store.GetLogDump(ctx, tenant.ID, dump.ID)
	require.NoError(t, err)
	require.Equal(t, LogDumpStatusCaptured, got.Status)
	require.Equal(t, artifactSHA, got.ArtifactSHA256)
	require.NotNil(t, got.SourceAvailable)
	require.True(t, *got.SourceAvailable)
	require.Empty(t, got.ClaimTokenSHA256)
}

func TestLogDumpExpiryAndTimeoutSelection(t *testing.T) {
	ctx := context.Background()
	store := setupPostgresStoreFull(t, ctx)
	tenant, err := store.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: "log-dump-expiry-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	node, err := store.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: "expiry-node-" + uuid.NewString()[:8]})
	require.NoError(t, err)
	job, err := store.CreateJob(ctx, &Job{TenantID: tenant.ID, Type: "log_dump", Status: JobStatusQueued, Payload: []byte(`{}`)}, nil)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Millisecond)
	dump, err := store.CreateLogDump(ctx, LogDump{
		TenantID: tenant.ID, NodeID: node.ID, JobID: uuid.NullUUID{UUID: job.ID, Valid: true},
		Source: LogDumpSourceNodeAgent, WindowStart: now.Add(-time.Hour), WindowEnd: now,
		CreatedAt: now.Add(-20 * time.Minute), ExpiresAt: now.Add(time.Hour), RetentionDays: 1,
	})
	require.NoError(t, err)

	timedOut, err := store.ListTimedOutLogDumps(ctx, now, now.Add(-10*time.Minute), 10)
	require.NoError(t, err)
	require.Len(t, timedOut, 1)
	require.Equal(t, dump.ID, timedOut[0].ID)

	claimed, err := store.ClaimLogDump(ctx, tenant.ID, node.ID, dump.ID, job.ID, shaHex("expiry-claim"), now, now.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, LogDumpStatusCapturing, claimed.Status)

	expiredAt := now.Add(2 * time.Hour)
	_, err = store.ClaimLogDump(ctx, tenant.ID, node.ID, dump.ID, job.ID, shaHex("late"), expiredAt, expiredAt.Add(time.Minute))
	require.ErrorIs(t, err, ErrLogDumpExpired)

	changed, err := store.ExpireLogDump(ctx, tenant.ID, dump.ID, expiredAt)
	require.NoError(t, err)
	require.True(t, changed)
	got, err := store.GetLogDump(ctx, tenant.ID, dump.ID)
	require.NoError(t, err)
	require.Equal(t, LogDumpStatusExpired, got.Status)
}

func shaHex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func TestValidSHA256Hex(t *testing.T) {
	t.Parallel()
	require.True(t, validSHA256Hex(shaHex("x")))
	require.False(t, validSHA256Hex("ABC"))
	require.False(t, validSHA256Hex(strings.Repeat("f", 63)))
}
