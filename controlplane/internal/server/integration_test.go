package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

const integrationToken = "integration-test-token"

func integrationConfig() *config.Config {
	return &config.Config{
		HTTP: config.HTTPConfig{
			Address:     ":8443",
			ReadTimeout: 15 * time.Second,
		},
		Auth: config.AuthConfig{
			OIDC: config.OIDCConfig{
				Enabled: false,
				StaticTokens: map[string]config.StaticPrincipalConfig{
					integrationToken: {
						Subject: "integration-admin",
						Name:    "Integration Admin",
						Email:   "admin@test.local",
						Roles:   []string{"admin"},
					},
				},
			},
		},
	}
}

func authedRequest(method, path string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.Header.Set("Authorization", "Bearer "+integrationToken)
	return req
}

func TestEndToEndProvisioningFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	store := setupTestStore(t)
	srv := server.New(zap.NewNop(), integrationConfig(), store, nil)

	// Create tenant
	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: fmt.Sprintf("test-tenant-provision-%s", tenantID),
	})
	require.NoError(t, err)

	// Create template with a promoted version
	template, err := store.CreateProvisioningTemplate(context.Background(), &storage.ProvisioningTemplate{
		ID:       uuid.New(),
		Name:     fmt.Sprintf("test-template-%s", uuid.NewString()[:8]),
		Provider: "mock",
	})
	require.NoError(t, err)
	_, err = store.CreateProvisioningTemplateVersion(context.Background(), storage.CreateTemplateVersionParams{
		TemplateID: template.ID, Body: "#!/bin/bash\necho hello",
	})
	require.NoError(t, err)
	_, err = store.PromoteProvisioningTemplateVersion(context.Background(), template.ID, 1)
	require.NoError(t, err)

	// Create node
	nodeID := uuid.New()
	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID: nodeID, TenantID: tenantID, Hostname: "provision-node",
	})
	require.NoError(t, err)

	// Submit provisioning job
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
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/v1/jobs", body))
	// Jobs are accepted asynchronously.
	assert.Contains(t, []int{http.StatusCreated, http.StatusAccepted}, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	assert.NotEmpty(t, jobResp["id"])

	// Verify job exists in store
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

	store := setupTestStore(t)
	srv := server.New(zap.NewNop(), integrationConfig(), store, nil)

	// Create tenant and node
	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: fmt.Sprintf("test-tenant-compliance-%s", tenantID),
	})
	require.NoError(t, err)

	nodeID := uuid.New()
	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID: nodeID, TenantID: tenantID, Hostname: "test-node",
	})
	require.NoError(t, err)

	// Submit compliance job
	jobReq := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"type":      "compliance.scan",
		"payload": map[string]interface{}{
			"scan_id":   uuid.NewString(),
			"tenant_id": tenantID.String(),
			"node_id":   nodeID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/v1/jobs", body))
	assert.Contains(t, []int{http.StatusCreated, http.StatusAccepted}, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	assert.NotEmpty(t, jobResp["id"])

	// Job is async; verify it was persisted.
	jobID, _ := uuid.Parse(jobResp["id"].(string))
	job, err := store.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, "compliance.scan", job.Type)
}

func TestJobLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	store := setupTestStore(t)
	srv := server.New(zap.NewNop(), integrationConfig(), store, nil)

	tenantID := uuid.New()
	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID:   tenantID,
		Name: fmt.Sprintf("test-tenant-lifecycle-%s", tenantID),
	})
	require.NoError(t, err)

	nodeID := uuid.New()
	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID: nodeID, TenantID: tenantID, Hostname: "lifecycle-node",
	})
	require.NoError(t, err)

	// Submit a compliance scan (simpler — no template promotion needed)
	jobReq := map[string]interface{}{
		"tenant_id": tenantID.String(),
		"type":      "compliance.scan",
		"payload": map[string]interface{}{
			"scan_id":   uuid.NewString(),
			"tenant_id": tenantID.String(),
			"node_id":   nodeID.String(),
		},
	}
	body, _ := json.Marshal(jobReq)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/v1/jobs", body))
	assert.Contains(t, []int{http.StatusCreated, http.StatusAccepted}, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	jobID := jobResp["id"].(string)

	// Get job
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil))
	assert.Equal(t, http.StatusOK, w.Code)

	// Cancel job — may return 200 (cancelled) or 409 (already completed).
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/cancel", nil))
	assert.Contains(t, []int{http.StatusOK, http.StatusConflict}, w.Code)

	// Verify job reached a terminal status.
	jobUUID, _ := uuid.Parse(jobID)
	job, err := store.GetJob(context.Background(), jobUUID)
	require.NoError(t, err)
	assert.Contains(t, []storage.JobStatus{storage.JobStatusCancelled, storage.JobStatusSucceeded, storage.JobStatusFailed}, job.Status)
}

func TestMultiTenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	store := setupTestStore(t)
	srv := server.New(zap.NewNop(), integrationConfig(), store, nil)

	tenant1ID := uuid.New()
	tenant2ID := uuid.New()

	_, err := store.CreateTenant(context.Background(), &storage.Tenant{
		ID: tenant1ID, Name: fmt.Sprintf("tenant-1-%s", tenant1ID),
	})
	require.NoError(t, err)

	_, err = store.CreateTenant(context.Background(), &storage.Tenant{
		ID: tenant2ID, Name: fmt.Sprintf("tenant-2-%s", tenant2ID),
	})
	require.NoError(t, err)

	node1ID := uuid.New()
	node2ID := uuid.New()

	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID: node1ID, TenantID: tenant1ID, Hostname: "node-1",
	})
	require.NoError(t, err)

	_, err = store.CreateNode(context.Background(), &storage.Node{
		ID: node2ID, TenantID: tenant2ID, Hostname: "node-2",
	})
	require.NoError(t, err)

	// List nodes for tenant 1
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedRequest(http.MethodGet, "/api/v1/nodes?tenant_id="+tenant1ID.String(), nil))
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
	logger := zap.NewNop()
	cfg := config.DatabaseConfig{
		URL: "postgresql://controlone:controlone@localhost:5432/controlone_test?sslmode=disable",
	}
	store, err := storage.New(logger, cfg, storage.Options{})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, migrate.Apply(ctx, store.DB()))

	return store
}
