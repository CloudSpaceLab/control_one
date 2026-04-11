package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/migrate"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/server"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

func TestEndToEndProvisioningFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Address:     ":8443",
			ReadTimeout: 15 * time.Second,
		},
		Auth: config.AuthConfig{
			OIDC: config.OIDCConfig{
				Enabled: false,
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, nil)

	// Create tenant
	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: "test-tenant",
	})
	require.NoError(t, err)

	// Create template
	template, err := store.CreateProvisioningTemplate(context.Background(), &storage.ProvisioningTemplate{
		ID:       uuid.New(),
		Name:     "test-template",
		Provider: "mock",
	})
	require.NoError(t, err)

	// Create provisioning job
	jobReq := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"type":      "provision.apply",
		"parameters": map[string]interface{}{
			"template_id": template.ID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	assert.NotEmpty(t, jobResp["id"])

	// Verify job exists
	jobID, _ := uuid.Parse(jobResp["id"].(string))
	job, err := store.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, "provision.apply", job.Type)
	assert.Equal(t, tenantID, job.TenantID)
}

func TestComplianceScanFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Address:     ":8443",
			ReadTimeout: 15 * time.Second,
		},
		Auth: config.AuthConfig{
			OIDC: config.OIDCConfig{
				Enabled: false,
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, nil)

	// Create tenant and node
	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: "test-tenant",
	})
	require.NoError(t, err)

	nodeID := uuid.New()
	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID:       nodeID,
		TenantID: tenantID,
		Hostname: "test-node",
	})
	require.NoError(t, err)

	// Create compliance job
	jobReq := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"type":      "compliance.scan",
		"parameters": map[string]interface{}{
			"node_id": nodeID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Verify compliance results
	results, err := store.ListComplianceResults(context.Background(), nodeID)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

func TestJobLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Address:     ":8443",
			ReadTimeout: 15 * time.Second,
		},
		Auth: config.AuthConfig{
			OIDC: config.OIDCConfig{
				Enabled: false,
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, nil)

	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: "test-tenant",
	})
	require.NoError(t, err)

	// Create job
	jobReq := map[string]interface{}{
		"tenant_id":  tenantID.String(),
		"type":       "provision.apply",
		"parameters": map[string]interface{}{},
	}
	body, _ := json.Marshal(jobReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	jobID := jobResp["id"].(string)

	// Get job
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Cancel job
	req = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/cancel", nil)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify job status
	jobUUID, _ := uuid.Parse(jobID)
	job, err := store.GetJob(context.Background(), jobUUID)
	require.NoError(t, err)
	assert.Equal(t, storage.JobStatusCancelled, job.Status)
}

func TestMultiTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Address:     ":8443",
			ReadTimeout: 15 * time.Second,
		},
		Auth: config.AuthConfig{
			OIDC: config.OIDCConfig{
				Enabled: false,
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, nil)

	// Create two tenants
	tenant1ID := uuid.New()
	tenant2ID := uuid.New()

	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenant1ID,
		Name: "tenant-1",
	})
	require.NoError(t, err)

	_, err = store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenant2ID,
		Name: "tenant-2",
	})
	require.NoError(t, err)

	// Create nodes for each tenant
	node1ID := uuid.New()
	node2ID := uuid.New()

	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID:       node1ID,
		TenantID: tenant1ID,
		Hostname: "node-1",
	})
	require.NoError(t, err)

	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID:       node2ID,
		TenantID: tenant2ID,
		Hostname: "node-2",
	})
	require.NoError(t, err)

	// List nodes for tenant 1
	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes?tenant_id="+tenant1ID.String(), nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	data := resp["data"].([]interface{})
	assert.Len(t, data, 1)
	node := data[0].(map[string]interface{})
	assert.Equal(t, node1ID.String(), node["id"])
}

func setupTestStore(t *testing.T) *storage.Store {
	t.Helper()
	logger := zap.NewNop()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgresql://controlone:controlone@localhost:5432/controlone_test?sslmode=disable"
	}
	cfg := config.DatabaseConfig{
		URL:             dbURL,
		ApplyMigrations: true,
	}
	store, err := storage.New(logger, cfg, storage.Options{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, migrate.Apply(ctx, store.DB()))

	return store
}
