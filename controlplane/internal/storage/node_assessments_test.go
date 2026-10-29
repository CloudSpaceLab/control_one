package storage

import (
	"context"
	"encoding/json"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNodeAssessmentsDedicatedPostgres(t *testing.T) {
	url := os.Getenv("PATCH_SECURITY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("dedicated disposable database required")
	}
	ctx := context.Background()
	s, err := New(zap.NewNop(), config.DatabaseConfig{URL: url}, Options{})
	require.NoError(t, err)
	var name string
	require.NoError(t, s.db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name))
	require.True(t, strings.HasPrefix(name, "controlone_patch_security_test_"))
	require.NoError(t, migrate.Apply(ctx, s.db))
	tenant, err := s.CreateTenant(ctx, &Tenant{ID: uuid.New(), Name: uuid.NewString()})
	require.NoError(t, err)
	node, err := s.CreateNode(ctx, &Node{ID: uuid.New(), TenantID: tenant.ID, Hostname: uuid.NewString()})
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	makeRecord := func(start time.Time, status string) NodeAssessment {
		return NodeAssessment{ID: uuid.New(), TenantID: tenant.ID, NodeID: node.ID, StartedAt: start, CompletedAt: now.Add(time.Minute), Status: status, InventoryGeneration: "generation", Evidence: json.RawMessage(`{"reasons":["test"],"inventory_completeness":"unknown"}`)}
	}
	fresh := makeRecord(now, "expired")
	old := makeRecord(now.Add(-time.Minute), "partial")
	require.NoError(t, s.RecordNodeAssessment(ctx, fresh))
	require.NoError(t, s.RecordNodeAssessment(ctx, old))
	require.Error(t, s.RecordNodeAssessment(ctx, fresh), "attempts cannot be overwritten")
	wrong := makeRecord(now, "partial")
	wrong.TenantID = uuid.New()
	require.Error(t, s.RecordNodeAssessment(ctx, wrong))
	require.NoError(t, s.Close())
	s, err = New(zap.NewNop(), config.DatabaseConfig{URL: url}, Options{})
	require.NoError(t, err)
	defer s.Close()
	got, err := s.GetLatestNodeAssessment(ctx, tenant.ID, node.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, fresh.ID, got.ID)
	require.Equal(t, "expired", got.Status)
	require.JSONEq(t, string(fresh.Evidence), string(got.Evidence))
	got, err = s.GetLatestNodeAssessment(ctx, uuid.New(), node.ID)
	require.NoError(t, err)
	require.Nil(t, got)
}
