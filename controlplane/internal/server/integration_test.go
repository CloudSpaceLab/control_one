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

const integrationAdminToken = "integration-admin-token"

func TestEndToEndProvisioningFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := integrationTestConfig()
	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, nil)

	// Create tenant
	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: "test-tenant",
	})
	require.NoError(t, err)

	// Create node and promoted template
	nodeID := uuid.New()
	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID:       nodeID,
		TenantID: tenantID,
		Hostname: "test-node",
	})
	require.NoError(t, err)

	template, err := store.CreateProvisioningTemplate(context.Background(), &storage.ProvisioningTemplate{
		ID:       uuid.New(),
		Name:     "test-template",
		Provider: "mock",
	})
	require.NoError(t, err)
	version, err := store.CreateProvisioningTemplateVersion(context.Background(), storage.CreateTemplateVersionParams{
		TemplateID: template.ID,
		Body:       "version 1",
	})
	require.NoError(t, err)
	_, err = store.PromoteProvisioningTemplateVersion(context.Background(), template.ID, version.Version)
	require.NoError(t, err)

	// Create provisioning job
	jobReq := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"type":      "provision.apply",
		"payload": map[string]interface{}{
			"plan_id":   template.ID.String(),
			"tenant_id": tenantID.String(),
			"node_id":   nodeID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	authorizeIntegrationRequest(req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

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
	require.Eventually(t, func() bool {
		job, err := store.GetJob(context.Background(), jobID)
		return err == nil && job != nil && (job.Status == storage.JobStatusSucceeded || job.Status == storage.JobStatusFailed)
	}, 5*time.Second, 100*time.Millisecond)
}

func TestComplianceScanFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := integrationTestConfig()
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
		"payload": map[string]interface{}{
			"scan_id":   uuid.New().String(),
			"tenant_id": tenantID.String(),
			"node_id":   nodeID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	authorizeIntegrationRequest(req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	// Verify the scan job can complete. The local policy evaluator returns no
	// results when no effective tenant/node policies are configured.
	require.Eventually(t, func() bool {
		jobs, _, err := store.ListJobs(context.Background(), tenantID, "compliance.scan", storage.JobStatusSucceeded, 10, 0)
		return err == nil && len(jobs) == 1
	}, 5*time.Second, 100*time.Millisecond)
	results, err := store.ListComplianceResults(context.Background(), nodeID)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestJobLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := integrationTestConfig()
	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, nil)

	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: "test-tenant",
	})
	require.NoError(t, err)

	// Create job
	nodeID := uuid.New()
	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID:       nodeID,
		TenantID: tenantID,
		Hostname: "test-node",
	})
	require.NoError(t, err)
	template, err := store.CreateProvisioningTemplate(context.Background(), &storage.ProvisioningTemplate{
		ID:       uuid.New(),
		Name:     "test-template",
		Provider: "mock",
	})
	require.NoError(t, err)
	version, err := store.CreateProvisioningTemplateVersion(context.Background(), storage.CreateTemplateVersionParams{
		TemplateID: template.ID,
		Body:       "version 1",
	})
	require.NoError(t, err)
	_, err = store.PromoteProvisioningTemplateVersion(context.Background(), template.ID, version.Version)
	require.NoError(t, err)

	// Create job
	jobReq := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"type":      "provision.apply",
		"payload": map[string]interface{}{
			"plan_id":   template.ID.String(),
			"tenant_id": tenantID.String(),
			"node_id":   nodeID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/jobs", bytes.NewReader(body))
	authorizeIntegrationRequest(req)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	jobID := jobResp["id"].(string)

	// Get job
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	authorizeIntegrationRequest(req)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Cancel job
	req = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/cancel", nil)
	authorizeIntegrationRequest(req)
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Contains(t, []int{http.StatusOK, http.StatusConflict}, w.Code)

	// Verify job status. The inline test worker may complete the job before the
	// cancel request arrives, in which case the API correctly returns 409.
	jobUUID, _ := uuid.Parse(jobID)
	job, err := store.GetJob(context.Background(), jobUUID)
	require.NoError(t, err)
	assert.Contains(t, []storage.JobStatus{storage.JobStatusCancelled, storage.JobStatusSucceeded}, job.Status)
}

func TestMultiTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	logger := zap.NewNop()
	cfg := integrationTestConfig()
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
	authorizeIntegrationRequest(req)
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

func integrationTestConfig() *config.Config {
	return &config.Config{
		HTTP: config.HTTPConfig{
			Address:     ":8443",
			ReadTimeout: 15 * time.Second,
		},
		Auth: config.AuthConfig{
			OIDC: config.OIDCConfig{
				Enabled: true,
				StaticTokens: map[string]config.StaticPrincipalConfig{
					integrationAdminToken: {
						Subject: "integration-admin",
						Name:    "Integration Admin",
						Roles:   []string{"admin"},
					},
				},
			},
			RBAC: config.RBACConfig{DefaultRole: "viewer"},
		},
	}
}

func authorizeIntegrationRequest(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+integrationAdminToken)
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
	_, err = store.DB().ExecContext(ctx, `DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;`)
	require.NoError(t, err)
	require.NoError(t, migrate.Apply(ctx, store.DB()))

	return store
}
