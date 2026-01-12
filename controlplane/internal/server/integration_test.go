package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/server"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// stubQueue implements worker.Queue for testing
type stubQueue struct{}

func (s *stubQueue) Enqueue(worker.Task) error {
	return nil
}

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
				Enabled: true,
				StaticTokens: map[string]config.StaticPrincipalConfig{
					"test-token": {
						Subject: "test-user",
					},
				},
			},
			RBAC: config.RBACConfig{DefaultRole: "admin"},
		},
		Jobs: config.JobsConfig{
			Provisioning: config.ProvisioningJobConfig{
				Template:  "demo-template",
				Provider:  "mock",
				Baselines: []string{"cis-aws-foundations"},
			},
			Compliance: config.ComplianceJobConfig{
				Region: "us-east-1",
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, &stubQueue{})

	// Create tenant and node for testing
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

	// Create template
	template, err := store.CreateProvisioningTemplate(context.Background(), &storage.ProvisioningTemplate{
		ID:       uuid.New(),
		Name:     "test-template",
		Provider: "mock",
	})
	require.NoError(t, err)

	// Create and promote a template version
	createdByVersion := uuid.New()
	_, err = store.CreateProvisioningTemplateVersion(context.Background(), storage.CreateTemplateVersionParams{
		TemplateID: template.ID,
		Body:       "test template body",
		CreatedBy:  &createdByVersion,
	})
	require.NoError(t, err)

	// Get the created version
	versions, _, err := store.ListProvisioningTemplateVersions(context.Background(), template.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, versions, 1)

	// Promote the version
	_, err = store.PromoteProvisioningTemplateVersion(context.Background(), template.ID, versions[0].Version)
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Debug: print actual response
	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

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
				Enabled: true,
				StaticTokens: map[string]config.StaticPrincipalConfig{
					"test-token": {
						Subject: "test-user",
					},
				},
			},
			RBAC: config.RBACConfig{DefaultRole: "admin"},
		},
		Jobs: config.JobsConfig{
			Provisioning: config.ProvisioningJobConfig{
				Template:  "demo-template",
				Provider:  "mock",
				Baselines: []string{"cis-aws-foundations"},
			},
			Compliance: config.ComplianceJobConfig{
				Region: "us-east-1",
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, &stubQueue{})

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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Debug: print actual response for compliance test
	t.Logf("Compliance test - Response status: %d", w.Code)
	t.Logf("Compliance test - Response body: %s", w.Body.String())

	assert.Equal(t, http.StatusCreated, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	assert.NotEmpty(t, jobResp["id"])

	// Verify job was created successfully
	jobID, _ := uuid.Parse(jobResp["id"].(string))
	job, err := store.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	assert.Equal(t, "compliance.scan", job.Type)
	assert.Equal(t, tenantID, job.TenantID)
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
				Enabled: true,
				StaticTokens: map[string]config.StaticPrincipalConfig{
					"test-token": {
						Subject: "test-user",
					},
				},
			},
			RBAC: config.RBACConfig{DefaultRole: "admin"},
		},
		Jobs: config.JobsConfig{
			Provisioning: config.ProvisioningJobConfig{
				Template:  "demo-template",
				Provider:  "mock",
				Baselines: []string{"cis-aws-foundations"},
			},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, &stubQueue{})

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

	// Create template
	template, err := store.CreateProvisioningTemplate(context.Background(), &storage.ProvisioningTemplate{
		ID:       uuid.New(),
		Name:     "test-template",
		Provider: "mock",
	})
	require.NoError(t, err)

	// Create and promote a template version
	createdByVersion := uuid.New()
	_, err = store.CreateProvisioningTemplateVersion(context.Background(), storage.CreateTemplateVersionParams{
		TemplateID: template.ID,
		Body:       "test template body",
		CreatedBy:  &createdByVersion,
	})
	require.NoError(t, err)

	versions, _, err := store.ListProvisioningTemplateVersions(context.Background(), template.ID, 10, 0)
	require.NoError(t, err)
	require.Len(t, versions, 1)

	_, err = store.PromoteProvisioningTemplateVersion(context.Background(), template.ID, versions[0].Version)
	require.NoError(t, err)

	// Try to create a provision job with minimal config
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
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	// Debug: print actual response for job lifecycle test
	t.Logf("JobLifecycle test - Response status: %d", w.Code)
	t.Logf("JobLifecycle test - Response body: %s", w.Body.String())

	assert.Equal(t, http.StatusCreated, w.Code)

	var jobResp map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jobResp)
	require.NoError(t, err)
	jobID := jobResp["id"].(string)

	// Get job
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+jobID, nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Cancel job
	req = httptest.NewRequest(http.MethodPost, "/api/v1/jobs/"+jobID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer test-token")
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
				Enabled: true,
				StaticTokens: map[string]config.StaticPrincipalConfig{
					"test-token": {
						Subject: "test-user",
					},
				},
			},
			RBAC: config.RBACConfig{DefaultRole: "admin"},
		},
	}

	store := setupTestStore(t)
	srv := server.New(logger, cfg, store, &stubQueue{})

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
	req.Header.Set("Authorization", "Bearer test-token")
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
	logger := zap.NewNop()

	// Use SQLite temporary file database for testing
	tempFile := t.TempDir() + "/test.db"
	cfg := config.DatabaseConfig{
		URL: "file:" + tempFile,
	}

	store, err := storage.New(logger, cfg, storage.Options{})
	require.NoError(t, err)

	// Initialize database schema for SQLite
	db := store.DB()
	if db != nil {
		// Create complete schema needed for tests
		_, err = db.Exec(`
			CREATE TABLE tenants (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
			);
			
			CREATE TABLE nodes (
				id TEXT PRIMARY KEY,
				tenant_id TEXT NOT NULL,
				hostname TEXT NOT NULL,
				os TEXT,
				arch TEXT,
				public_ip TEXT,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (tenant_id) REFERENCES tenants(id)
			);
			
			CREATE TABLE jobs (
				id TEXT PRIMARY KEY,
				tenant_id TEXT NOT NULL,
				template_id TEXT,
				status TEXT NOT NULL,
				payload TEXT,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (tenant_id) REFERENCES tenants(id)
			);
			
			CREATE TABLE job_events (
				id TEXT PRIMARY KEY,
				job_id TEXT NOT NULL,
				event_type TEXT NOT NULL,
				message TEXT,
				timestamp TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (job_id) REFERENCES jobs(id)
			);
			
			CREATE TABLE compliance_results (
				id TEXT PRIMARY KEY,
				job_id TEXT NOT NULL,
				tenant_id TEXT NOT NULL,
				scan_id TEXT NOT NULL,
				node_id TEXT,
				rule_id TEXT NOT NULL,
				passed BOOLEAN NOT NULL,
				severity TEXT,
				details TEXT,
				remediation TEXT,
				metadata TEXT,
				checked_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (job_id) REFERENCES jobs(id)
			);
			
			CREATE TABLE provisioning_templates (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL UNIQUE,
				provider TEXT,
				description TEXT,
				labels TEXT NOT NULL DEFAULT '{}',
				template_type TEXT,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				archived_at TIMESTAMP,
				promoted_version_id TEXT
			);
			
			CREATE TABLE provisioning_template_versions (
				id TEXT PRIMARY KEY,
				template_id TEXT NOT NULL,
				version INTEGER NOT NULL,
				checksum TEXT,
				body TEXT NOT NULL,
				metadata_schema TEXT,
				rollout_notes TEXT,
				created_by TEXT,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				promoted_at TIMESTAMP,
				UNIQUE (template_id, version),
				FOREIGN KEY (template_id) REFERENCES provisioning_templates(id) ON DELETE CASCADE
			);
			
			CREATE TABLE provisioning_template_rollouts (
				id TEXT PRIMARY KEY,
				template_version_id TEXT NOT NULL,
				target_percent INTEGER NOT NULL CHECK (target_percent >= 0 AND target_percent <= 100),
				state TEXT NOT NULL DEFAULT 'scheduled',
				metadata TEXT,
				scheduled_for TIMESTAMP,
				completed_at TIMESTAMP,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (template_version_id) REFERENCES provisioning_template_versions(id) ON DELETE CASCADE
			);
			
			CREATE TABLE template_executions (
				id TEXT PRIMARY KEY,
				template_id TEXT NOT NULL,
				template_type TEXT NOT NULL,
				target_type TEXT NOT NULL,
				target_id TEXT,
				parameters TEXT,
				created_by TEXT,
				status TEXT NOT NULL DEFAULT 'pending',
				started_at TIMESTAMP,
				completed_at TIMESTAMP,
				result TEXT,
				error TEXT,
				created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
				FOREIGN KEY (template_id) REFERENCES provisioning_templates(id)
			);
		`)
		require.NoError(t, err)
	}

	return store
}
