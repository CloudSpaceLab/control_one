package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
)

func TestPingEndpointAuthentication(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{
			Address:      ":0",
			ReadTimeout:  0,
			WriteTimeout: 0,
		},
		TLS: config.TLSConfig{
			RequireClientTLS: false,
		},
		Observability: config.ObservabilityConfig{
			EnableMetrics: true,
			MetricsPath:   "/metrics",
		},
		Worker: config.WorkerConfig{},
		Auth:   authWithTokens("admin", "test-token"),
	}

	srv := New(logger, cfg, nil, nil)
	handler := srv.Handler()

	t.Run("unauthenticated requests are rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected status %d got %d", http.StatusUnauthorized, rec.Code)
		}
	})

	t.Run("bearer token is accepted", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected status %d got %d", http.StatusOK, rec.Code)
		}

		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected application/json content-type got %s", ct)
		}

		body := rec.Body.String()
		if !contains(body, "test-token") {
			t.Fatalf("expected body to contain principal token, got %s", body)
		}
	})
}

func TestHandleComplianceScanPersistsResultsAndAudits(t *testing.T) {
	t.Parallel()

	// Synthetic fallback was removed in PR 1 (Compliance Foundation). When a
	// node has no policies assigned, the scan job runs cleanly and emits its
	// completion audit log but persists no results — the UI surfaces that as
	// no_policies_assigned. This test asserts that contract.
	logger := zap.NewNop()
	tenantID := uuid.New()
	jobID := uuid.New()
	store := &fakeStore{
		jobs:      map[uuid.UUID]*storage.Job{},
		auditLogs: []storage.AuditLog{},
	}

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Jobs: config.JobsConfig{
			Compliance: config.ComplianceJobConfig{
				Region:    "us-west-2",
				RuleSets:  []string{"cis-foundations"},
				AutoApply: true,
			},
		},
	}

	srv := New(logger, cfg, store, &stubQueue{})
	srv.configureJobIntegrations()

	payload := map[string]any{
		"scan_id":   "scan-123",
		"tenant_id": tenantID.String(),
		"node_id":   uuid.New().String(),
		"policies": map[string]string{
			"policy.cis-foundations": "fail-control",
		},
	}
	body, _ := json.Marshal(payload)

	job := &storage.Job{
		ID:        jobID,
		TenantID:  tenantID,
		Type:      JobTypeComplianceScan,
		Payload:   body,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	store.jobs = map[uuid.UUID]*storage.Job{jobID: job}

	srv.auditAsync = false

	if handler, ok := srv.jobHandlers[JobTypeComplianceScan]; ok {
		if err := handler(context.Background(), job); err != nil {
			t.Fatalf("handle compliance scan: %v", err)
		}
	} else {
		t.Fatalf("compliance scan handler not registered")
	}

	if len(store.complianceResults[jobID]) != 0 {
		t.Fatalf("expected zero persisted results when node has no policies, got %d", len(store.complianceResults[jobID]))
	}

	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "compliance.scan.completed" {
		t.Fatalf("expected compliance audit log, got %+v", store.auditLogs)
	}
}

func TestJobDetailIncludesComplianceResults(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "job-viewer"),
	}

	jobID := uuid.New()
	tenantID := uuid.New()
	severity := "high"
	now := time.Unix(1700000900, 0).UTC()
	store := &fakeStore{
		jobs: map[uuid.UUID]*storage.Job{
			jobID: {
				ID:        jobID,
				TenantID:  tenantID,
				Type:      JobTypeComplianceScan,
				Status:    storage.JobStatusSucceeded,
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		events:    map[uuid.UUID][]storage.JobEvent{},
		userRoles: map[uuid.UUID][]string{},
		complianceResults: map[uuid.UUID][]storage.ComplianceResult{
			jobID: {
				{
					JobID:     jobID,
					TenantID:  tenantID,
					RuleID:    "rule-1",
					Passed:    false,
					Severity:  &severity,
					CheckedAt: &now,
				},
			},
		},
	}

	srv := New(logger, cfg, store, &stubQueue{})

	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer job-viewer")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodGet, "/api/v1/tenants")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected warm-up ok got %d", rec.Code)
	}
	store.overrideRoles = map[uuid.UUID][]string{
		store.lastUserID: {"viewer"},
	}

	rec = call(http.MethodGet, "/api/v1/jobs/"+jobID.String())
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	var resp jobResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode job response: %v", err)
	}
	if len(resp.ComplianceResults) != 1 {
		t.Fatalf("expected 1 compliance result, got %d", len(resp.ComplianceResults))
	}
	if resp.ComplianceResults[0].RuleID != "rule-1" {
		t.Fatalf("unexpected compliance rule: %+v", resp.ComplianceResults[0])
	}
	if resp.ComplianceResults[0].Severity == nil || *resp.ComplianceResults[0].Severity != severity {
		t.Fatalf("expected severity %s, got %+v", severity, resp.ComplianceResults[0].Severity)
	}
	if resp.ComplianceResults[0].CheckedAt == nil {
		t.Fatalf("expected checked_at timestamp")
	}
}

func TestUserAndRoleEndpoints(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "admin-token"),
	}

	targetUserID := uuid.New()
	targetUser := storage.User{
		ID:          targetUserID,
		ExternalID:  "user-123",
		DisplayName: storageNullString("Sample User"),
		Email:       storageNullString("sample@example.com"),
		CreatedAt:   time.Unix(1700000500, 0),
	}

	store := &fakeStore{
		users: map[string]*storage.User{
			targetUser.ExternalID: &targetUser,
		},
		usersByID: map[uuid.UUID]*storage.User{
			targetUserID: &targetUser,
		},
		userList: []storage.User{targetUser},
		userRoles: map[uuid.UUID][]string{
			targetUserID: {"viewer"},
		},
		overrideRoles: map[uuid.UUID][]string{},
		rolesCatalog: []storage.Role{
			{ID: uuid.New(), Name: "viewer", CreatedAt: time.Unix(1700000000, 0)},
			{ID: uuid.New(), Name: "operator", CreatedAt: time.Unix(1700000000, 0)},
			{ID: uuid.New(), Name: "admin", CreatedAt: time.Unix(1700000000, 0)},
		},
	}

	srv := New(logger, cfg, store, &stubQueue{})

	call := func(method, path string, body any) *httptest.ResponseRecorder {
		var reader *bytes.Reader
		if body != nil {
			payload, _ := json.Marshal(body)
			reader = bytes.NewReader(payload)
		} else {
			reader = bytes.NewReader(nil)
		}
		req := httptest.NewRequest(method, path, reader)
		req.Header.Set("Authorization", "Bearer admin-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	// Warm-up request to persist admin principal and then elevate via override.
	rec := call(http.MethodGet, "/api/v1/tenants", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tenant warm-up 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	if store.overrideRoles == nil {
		store.overrideRoles = map[uuid.UUID][]string{}
	}
	store.overrideRoles[store.lastUserID] = []string{"admin"}

	t.Run("list users", func(t *testing.T) {
		rec := call(http.MethodGet, "/api/v1/users", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp paginatedResponse[userResponse]
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(resp.Data) == 0 {
			t.Fatalf("expected at least 1 user, got 0")
		}
		var found bool
		for _, u := range resp.Data {
			if u.ID == targetUserID.String() {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected response to include user %s, got %+v", targetUserID, resp.Data)
		}
	})

	t.Run("get user details", func(t *testing.T) {
		rec := call(http.MethodGet, "/api/v1/users/"+targetUserID.String(), nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp userResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode user response: %v", err)
		}
		if len(resp.Roles) != 1 || resp.Roles[0] != "viewer" {
			t.Fatalf("expected viewer role, got %+v", resp.Roles)
		}
		if resp.Email == nil || *resp.Email != "sample@example.com" {
			t.Fatalf("expected stored email propagated, got %+v", resp.Email)
		}
	})

	t.Run("update user roles", func(t *testing.T) {
		payload := updateUserRolesRequest{Roles: []string{"operator", "admin"}}
		rec := call(http.MethodPatch, "/api/v1/users/"+targetUserID.String(), payload)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp userResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode patch response: %v", err)
		}
		if len(resp.Roles) != 2 {
			t.Fatalf("expected updated roles, got %+v", resp.Roles)
		}
		storedRoles, err := store.ListUserRoles(context.Background(), targetUserID)
		if err != nil {
			t.Fatalf("list roles after update: %v", err)
		}
		if len(storedRoles) != 2 || storedRoles[0] != "operator" {
			t.Fatalf("expected stored roles updated, got %+v", storedRoles)
		}
	})

	t.Run("list roles catalog", func(t *testing.T) {
		rec := call(http.MethodGet, "/api/v1/roles", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp []roleResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode list roles response: %v", err)
		}
		if len(resp) != len(store.rolesCatalog) {
			t.Fatalf("expected %d roles, got %d", len(store.rolesCatalog), len(resp))
		}
	})
}

func TestTemplateEndpoints(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "subject-templates", "test-token"),
	}

	store := &fakeStore{}
	srv := New(logger, cfg, store, &stubQueue{})

	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer subject-templates")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	// Warm up to persist user and grant admin role.
	rec := call(http.MethodGet, "/api/v1/tenants", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected tenants call success, got %d", rec.Code)
	}
	if store.lastUserID == uuid.Nil {
		t.Fatalf("expected user to be persisted")
	}
	store.overrideRoles = map[uuid.UUID][]string{
		store.lastUserID: {"admin"},
	}
	templateTenantID := uuid.New()

	createPayload := map[string]any{
		"tenant_id":   templateTenantID.String(),
		"name":        "web-template",
		"provider":    "aws",
		"description": "Sample template",
		"labels": map[string]string{
			"env": "dev",
		},
	}
	body, _ := json.Marshal(createPayload)
	rec = call(http.MethodPost, "/api/v1/templates", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create template 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var created templateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected template id")
	}

	rec = call(http.MethodGet, "/api/v1/templates?tenant_id="+templateTenantID.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list template 200 got %d", rec.Code)
	}
	var listResp struct {
		Data []templateResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listResp.Data) != 1 {
		t.Fatalf("expected 1 template, got %d", len(listResp.Data))
	}

	assignmentPayload := map[string]any{
		"tenant_id":  templateTenantID.String(),
		"scope_type": "label_selector",
		"selector": map[string]any{
			"agent.primary_purpose": "db_node",
		},
	}
	body, _ = json.Marshal(assignmentPayload)
	assignmentPath := fmt.Sprintf("/api/v1/templates/%s/assignments", created.ID)
	rec = call(http.MethodPost, assignmentPath, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create template assignment 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var templateAssignment templateAssignmentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &templateAssignment); err != nil {
		t.Fatalf("decode template assignment response: %v", err)
	}
	if templateAssignment.ScopeType != storage.AssignmentScopeLabelSelector {
		t.Fatalf("expected label selector assignment, got %+v", templateAssignment)
	}

	rec = call(http.MethodGet, assignmentPath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list template assignments 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var assignmentList struct {
		Items []templateAssignmentResponse `json:"items"`
		Total int                          `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &assignmentList); err != nil {
		t.Fatalf("decode template assignment list: %v", err)
	}
	if assignmentList.Total != 1 || len(assignmentList.Items) != 1 {
		t.Fatalf("expected one template assignment, got %+v", assignmentList)
	}

	versionPayload := map[string]any{
		"body":     "#cloud-config",
		"checksum": "abc123",
	}
	body, _ = json.Marshal(versionPayload)
	path := fmt.Sprintf("/api/v1/templates/%s/versions", created.ID)
	rec = call(http.MethodPost, path, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected create version 201 got %d body=%s", rec.Code, rec.Body.String())
	}
	var version templateVersionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatalf("decode version response: %v", err)
	}
	if version.Version != 1 {
		t.Fatalf("expected version 1, got %d", version.Version)
	}

	promotePath := fmt.Sprintf("/api/v1/templates/%s/versions/1/promote", created.ID)
	rec = call(http.MethodPost, promotePath, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected promote 200 got %d", rec.Code)
	}

	rec = call(http.MethodGet, "/api/v1/templates/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected get template 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var detail templateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detail.PromotedVersionID == nil || detail.PromotedVersion == nil {
		t.Fatalf("expected promoted version metadata in detail response")
	}

	updatePayload := map[string]any{
		"name":        "web-template-renamed",
		"provider":    "aws",
		"description": "Updated description",
		"labels": map[string]string{
			"env":  "prod",
			"team": "platform",
		},
		"archived": true,
	}
	body, _ = json.Marshal(updatePayload)
	rec = call(http.MethodPatch, "/api/v1/templates/"+created.ID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected patch template 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var patched templateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &patched); err != nil {
		t.Fatalf("decode patch response: %v", err)
	}
	if patched.Name != "web-template-renamed" {
		t.Fatalf("expected updated name, got %s", patched.Name)
	}
	if patched.ArchivedAt == nil {
		t.Fatalf("expected archived timestamp")
	}
	if patched.Labels["team"] != "platform" {
		t.Fatalf("expected labels merged, got %+v", patched.Labels)
	}

	globalTemplateID := uuid.New()
	store.templates = append(store.templates, storage.ProvisioningTemplate{
		ID:        globalTemplateID,
		TenantID:  uuid.Nil,
		Name:      "platform-linux-baseline",
		Provider:  "linux",
		Labels:    map[string]string{"source": "platform"},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	})

	rec = call(http.MethodGet, "/api/v1/templates/"+globalTemplateID.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected platform-global template read 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"name": "mutated-platform-template"})
	rec = call(http.MethodPatch, "/api/v1/templates/"+globalTemplateID.String(), body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected platform-global template patch 403 got %d body=%s", rec.Code, rec.Body.String())
	}
	body, _ = json.Marshal(map[string]any{"body": "#cloud-config"})
	rec = call(http.MethodPost, fmt.Sprintf("/api/v1/templates/%s/versions", globalTemplateID), body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected platform-global template version create 403 got %d body=%s", rec.Code, rec.Body.String())
	}
	rec = call(http.MethodPost, fmt.Sprintf("/api/v1/templates/%s/versions/1/promote", globalTemplateID), nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected platform-global template promote 403 got %d body=%s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]any{
		"tenant_id":  templateTenantID.String(),
		"scope_type": storage.AssignmentScopeTenant,
	})
	rec = call(http.MethodPost, fmt.Sprintf("/api/v1/templates/%s/assignments", globalTemplateID), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected platform-global template assignment to authorized tenant 201 got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestScopedPolicyAssignmentEndpoints(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "subject-policy-scopes"),
	}

	tenantID := uuid.New()
	policyID := uuid.New()
	nodeID := uuid.New()
	clusterID := uuid.New()
	hypervisorID := uuid.New()
	tokenID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "scope-tenant", CreatedAt: now}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "scope-node",
			State:     storage.NodeStateActive,
			Labels:    map[string]any{"agent.primary_purpose": "db_node"},
			CreatedAt: now,
			UpdatedAt: now,
		}},
		policies: map[uuid.UUID]storage.Policy{
			policyID: {
				ID:        policyID,
				TenantID:  tenantID,
				Name:      "Scoped hardening",
				RuleType:  "compliance",
				Enabled:   true,
				Labels:    map[string]string{},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		clusters: map[uuid.UUID]*storage.Cluster{
			clusterID: {
				ID:        clusterID,
				TenantID:  tenantID,
				Name:      "prod-cluster",
				Provider:  "vmware",
				Labels:    map[string]any{"function": "payments"},
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		hypervisorHosts: map[uuid.UUID]*storage.HypervisorHost{
			hypervisorID: {
				ID:           hypervisorID,
				TenantID:     tenantID,
				Provider:     "vmware",
				Name:         "vcenter-prod",
				EndpointURL:  "https://vcenter.example",
				HealthStatus: "healthy",
				CreatedAt:    now,
				UpdatedAt:    now,
			},
		},
		enrollmentTokens: map[string]storage.EnrollmentToken{
			"hash": {
				ID:        tokenID,
				TenantID:  tenantID,
				Name:      "fleet-token",
				TokenHash: "hash",
				Labels:    map[string]string{"onboard_source": "fleet-enroll"},
				ExpiresAt: now.Add(time.Hour),
				CreatedAt: now,
			},
		},
	}
	srv := New(logger, cfg, store, &stubQueue{})

	call := func(method, path string, payload map[string]any) *httptest.ResponseRecorder {
		var body []byte
		if payload != nil {
			body, _ = json.Marshal(payload)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer subject-policy-scopes")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodGet, "/api/v1/tenants", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected warm-up success, got %d body=%s", rec.Code, rec.Body.String())
	}
	store.overrideRoles = map[uuid.UUID][]string{store.lastUserID: {"admin"}}

	endpoint := fmt.Sprintf("/api/v1/policies/%s/assignments", policyID)
	cases := []struct {
		name      string
		payload   map[string]any
		scopeType string
	}{
		{
			name: "tenant",
			payload: map[string]any{
				"tenant_id":  tenantID.String(),
				"scope_type": storage.AssignmentScopeTenant,
			},
			scopeType: storage.AssignmentScopeTenant,
		},
		{
			name: "node",
			payload: map[string]any{
				"tenant_id":  tenantID.String(),
				"scope_type": storage.AssignmentScopeNode,
				"scope_id":   nodeID.String(),
			},
			scopeType: storage.AssignmentScopeNode,
		},
		{
			name: "label selector",
			payload: map[string]any{
				"tenant_id":  tenantID.String(),
				"scope_type": storage.AssignmentScopeLabelSelector,
				"selector":   map[string]any{"agent.primary_purpose": "db_node"},
			},
			scopeType: storage.AssignmentScopeLabelSelector,
		},
		{
			name: "cluster",
			payload: map[string]any{
				"tenant_id":  tenantID.String(),
				"scope_type": storage.AssignmentScopeCluster,
				"scope_id":   clusterID.String(),
			},
			scopeType: storage.AssignmentScopeCluster,
		},
		{
			name: "hypervisor",
			payload: map[string]any{
				"tenant_id":  tenantID.String(),
				"scope_type": storage.AssignmentScopeHypervisorHost,
				"scope_id":   hypervisorID.String(),
			},
			scopeType: storage.AssignmentScopeHypervisorHost,
		},
		{
			name: "enrollment token",
			payload: map[string]any{
				"tenant_id":  tenantID.String(),
				"scope_type": storage.AssignmentScopeEnrollmentToken,
				"scope_id":   tokenID.String(),
			},
			scopeType: storage.AssignmentScopeEnrollmentToken,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := call(http.MethodPost, endpoint, tc.payload)
			if rec.Code != http.StatusCreated {
				t.Fatalf("expected 201 got %d body=%s", rec.Code, rec.Body.String())
			}
			var resp policyAssignmentResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode assignment response: %v", err)
			}
			if resp.ScopeType != tc.scopeType {
				t.Fatalf("expected scope %s got %+v", tc.scopeType, resp)
			}
		})
	}

	rec = call(http.MethodGet, endpoint, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected list assignments 200 got %d body=%s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Items []policyAssignmentResponse `json:"items"`
		Total int                        `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("decode assignment list: %v", err)
	}
	if listResp.Total != len(cases) || len(listResp.Items) != len(cases) {
		t.Fatalf("expected %d assignments, got %+v", len(cases), listResp)
	}
}

func TestEnrichProvisioningMetadata(t *testing.T) {
	logger := zap.NewNop()
	store := &fakeStore{}
	srv := New(logger, &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
	}, store, &stubQueue{})

	templateID := uuid.New()
	store.templates = []storage.ProvisioningTemplate{
		{
			ID:        templateID,
			Name:      "web",
			Provider:  "aws",
			Labels:    map[string]string{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	store.templateVersions = map[uuid.UUID][]storage.ProvisioningTemplateVersion{
		templateID: {
			{
				ID:         uuid.New(),
				TemplateID: templateID,
				Version:    1,
				Checksum:   sql.NullString{String: "sha256:abc", Valid: true},
				Body:       "#cloud-config",
				CreatedAt:  time.Now(),
			},
			{
				ID:         uuid.New(),
				TemplateID: templateID,
				Version:    2,
				Checksum:   sql.NullString{String: "sha256:def", Valid: true},
				Body:       "#cloud-config v2",
				CreatedAt:  time.Now(),
			},
		},
	}
	versionID := store.templateVersions[templateID][1].ID
	store.templates[0].PromotedVersionID = &versionID

	t.Run("uses promoted version when template_version absent", func(t *testing.T) {
		meta := map[string]string{}
		srv.enrichProvisioningMetadata(context.Background(), templateID.String(), meta)
		if meta["template_version"] != "2" {
			t.Fatalf("expected promoted version 2, got %s", meta["template_version"])
		}
		if meta["template_checksum"] != "sha256:def" {
			t.Fatalf("expected checksum sha256:def, got %s", meta["template_checksum"])
		}
	})

	t.Run("uses explicit version when provided", func(t *testing.T) {
		meta := map[string]string{"template_version": "1"}
		srv.enrichProvisioningMetadata(context.Background(), templateID.String(), meta)
		if meta["template_version"] != "1" {
			t.Fatalf("expected version 1, got %s", meta["template_version"])
		}
		if meta["template_checksum"] != "sha256:abc" {
			t.Fatalf("expected checksum sha256:abc, got %s", meta["template_checksum"])
		}
	})
}

func TestJobDetailAndCancelEndpoints(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "job-admin"),
	}

	jobID := uuid.New()
	store := &fakeStore{
		jobs: map[uuid.UUID]*storage.Job{
			jobID: {
				ID:        jobID,
				Type:      JobTypeProvisionApply,
				Status:    storage.JobStatusRunning,
				CreatedAt: time.Unix(1700000700, 0),
				UpdatedAt: time.Unix(1700000700, 0),
			},
		},
		events: map[uuid.UUID][]storage.JobEvent{
			jobID: {
				{
					ID:        uuid.New(),
					JobID:     jobID,
					Status:    storage.JobStatusQueued,
					Message:   "queued",
					CreatedAt: time.Unix(1700000700, 0),
				},
			},
		},
		userRoles: map[uuid.UUID][]string{},
	}
	srv := New(logger, cfg, store, &stubQueue{})
	srv.configureJobIntegrations()

	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer job-admin")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	// Persist user and upgrade to admin/operator.
	rec := call(http.MethodGet, "/api/v1/tenants")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected warm-up ok got %d", rec.Code)
	}
	store.overrideRoles = map[uuid.UUID][]string{
		store.lastUserID: {"viewer", "operator"},
	}

	t.Run("returns job detail with events", func(t *testing.T) {
		rec := call(http.MethodGet, "/api/v1/jobs/"+jobID.String())
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp jobResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode job response: %v", err)
		}
		if resp.ID != jobID.String() {
			t.Fatalf("expected job id %s got %s", jobID, resp.ID)
		}
		if len(resp.Events) != 1 {
			t.Fatalf("expected 1 event got %d", len(resp.Events))
		}
	})

	t.Run("agent can fetch own executable job detail", func(t *testing.T) {
		nodeID := uuid.New()
		otherNodeID := uuid.New()
		store.nodes = append(store.nodes,
			storage.Node{ID: nodeID, TenantID: uuid.New(), Hostname: "agent-node", AuthToken: sql.NullString{String: "node-job-token", Valid: true}},
			storage.Node{ID: otherNodeID, TenantID: uuid.New(), Hostname: "other-node", AuthToken: sql.NullString{String: "other-node-token", Valid: true}},
		)
		payload, _ := json.Marshal(map[string]string{"node_id": nodeID.String()})
		agentJob, _ := store.CreateJob(context.Background(), &storage.Job{
			TenantID: nodeID,
			Type:     JobTypeWebserverInventoryScan,
			Status:   storage.JobStatusRunning,
			Payload:  payload,
		}, nil)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+agentJob.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer node-job-token")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected agent job fetch 200 got %d body=%s", rec.Code, rec.Body.String())
		}

		req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/"+agentJob.ID.String(), nil)
		req.Header.Set("Authorization", "Bearer other-node-token")
		rec = httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected other agent forbidden got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("cancels running job", func(t *testing.T) {
		rec := call(http.MethodPost, "/api/v1/jobs/"+jobID.String()+"/cancel")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected cancel 200 got %d body=%s", rec.Code, rec.Body.String())
		}
		var resp jobResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode cancel response: %v", err)
		}
		if resp.Status != string(storage.JobStatusCancelled) {
			t.Fatalf("expected cancelled status, got %s", resp.Status)
		}
		if store.jobs[jobID].Status != storage.JobStatusCancelled {
			t.Fatalf("store job status not updated")
		}
		if len(resp.Events) < 2 {
			t.Fatalf("expected cancel event appended, got %d", len(resp.Events))
		}
	})
}

func TestComplianceEvaluateEndpoint(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("operator", "compliance-token"),
		Jobs: config.JobsConfig{
			Compliance: config.ComplianceJobConfig{
				Region:         "us-west-2",
				RuleSets:       []string{"cis-foundations", "nist-sp800"},
				Certifications: []string{"soc2"},
				AutoApply:      true,
			},
		},
	}

	nodeID := uuid.New()
	store := &fakeStore{nodes: []storage.Node{{ID: nodeID, TenantID: uuid.New(), Hostname: "eval-node"}}}
	srv := New(logger, cfg, store, &stubQueue{})
	handler := srv.Handler()

	call := func(body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/evaluate", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer compliance-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	ruleSets := []string{"cis-foundations", "nist-sp800"}
	certifications := []string{"soc2"}
	payload := map[string]any{
		"node_id":        nodeID.String(),
		"region":         "us-west-2",
		"rulesets":       ruleSets,
		"certifications": certifications,
		"policies": map[string]string{
			"policy.cis-foundations": "fail-control-1",
			"policy.nist-sp800":      "warn-drift",
		},
		"auto_apply": true,
	}
	body, _ := json.Marshal(payload)

	rec := call(body)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 got %d body=%s", rec.Code, rec.Body.String())
	}

	// Synthetic fallback was removed in PR 1: when no policies are assigned,
	// the endpoint returns empty results plus no_policies_assigned
	// metadata so the UI can render an empty state instead of fabricated zeros.
	var resp struct {
		Results []struct {
			RuleID string `json:"rule_id"`
		} `json:"results"`
		Metadata map[string]any `json:"metadata,omitempty"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Fatalf("expected zero results without policies, got %d", len(resp.Results))
	}
	if resp.Metadata == nil || resp.Metadata["no_policies_assigned"] != true {
		t.Fatalf("expected no_policies_assigned metadata, got %+v", resp.Metadata)
	}
	_ = ruleSets
	_ = certifications
	_ = nodeID

	t.Run("invalid payload rejected", func(t *testing.T) {
		rec := call([]byte(`{"region":"us","rulesets":["cis"]}`))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d body=%s", rec.Code, rec.Body.String())
		}
	})
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func authWithTokens(defaultRole string, tokens ...string) config.AuthConfig {
	static := make(map[string]config.StaticPrincipalConfig, len(tokens))
	for _, tok := range tokens {
		static[tok] = config.StaticPrincipalConfig{
			Subject: tok,
		}
	}
	return config.AuthConfig{
		OIDC: config.OIDCConfig{
			Enabled:      true,
			StaticTokens: static,
		},
		RBAC: config.RBACConfig{DefaultRole: defaultRole},
	}
}

func TestRBACAuthorization(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "subject-123", "test-token"),
	}

	store := &fakeStore{userRoles: map[uuid.UUID][]string{}}
	srv := New(logger, cfg, store, &stubQueue{})
	srv.configureJobIntegrations()

	call := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer subject-123")
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		return rec
	}

	t.Run("viewer can access tenant list", func(t *testing.T) {
		rec := call(http.MethodGet, "/api/v1/tenants", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
	})

	t.Run("viewer denied control plane operations", func(t *testing.T) {
		rec := call(http.MethodPost, "/api/v1/tenants", []byte(`{"name":"Tenant X"}`))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403 got %d", rec.Code)
		}
	})

	if store.lastUserID == uuid.Nil {
		t.Fatalf("expected user to be persisted")
	}
	store.overrideRoles = map[uuid.UUID][]string{store.lastUserID: {"admin"}}
	t.Run("admin role grants access", func(t *testing.T) {
		rec := call(http.MethodPost, "/api/v1/tenants", []byte(`{"name":"Tenant Y"}`))
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201 got %d", rec.Code)
		}
	})

	bearerReq := func(method, path string, body []byte) *http.Request {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer subject-123")
		return req
	}

	tenantID := uuid.New()
	store.tenants = []storage.Tenant{{ID: tenantID, Name: "Tenant A", CreatedAt: time.Unix(1700000000, 0)}}
	store.jobs = map[uuid.UUID]*storage.Job{}
	store.events = map[uuid.UUID][]storage.JobEvent{}
	templateID := uuid.New()
	store.templates = []storage.ProvisioningTemplate{
		{
			ID:        templateID,
			Name:      "demo",
			Provider:  "mock",
			Labels:    map[string]string{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	store.templateVersions = map[uuid.UUID][]storage.ProvisioningTemplateVersion{
		templateID: {
			{
				ID:         uuid.New(),
				TemplateID: templateID,
				Version:    1,
				Body:       "body",
				CreatedAt:  time.Now(),
				PromotedAt: sql.NullTime{Time: time.Now(), Valid: true},
			},
		},
	}
	id := store.templateVersions[templateID][0].ID
	store.templates[0].PromotedVersionID = &id

	t.Run("GET /api/v1/jobs returns jobs", func(t *testing.T) {
		jobID := uuid.New()
		store.jobs[jobID] = &storage.Job{
			ID:        jobID,
			TenantID:  tenantID,
			Type:      "provision.apply",
			Status:    storage.JobStatusQueued,
			CreatedAt: time.Unix(1700000000, 0),
			UpdatedAt: time.Unix(1700000000, 0),
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodGet, "/api/v1/jobs?tenant_id="+tenantID.String(), nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected json response, got %s", ct)
		}
		if !contains(rec.Body.String(), jobID.String()) {
			t.Fatalf("expected response to contain job id: %s", rec.Body.String())
		}
	})

	t.Run("POST /api/v1/jobs enqueues job", func(t *testing.T) {
		store.tenants = []storage.Tenant{
			{ID: tenantID, Name: "Tenant A", CreatedAt: time.Unix(1700000000, 0)},
		}
		body := fmt.Sprintf(`{
			"type":"%s",
			"tenant_id":"%s",
			"payload":{
				"plan_id":"%s",
				"tenant_id":"%s",
				"node_id":"node-123",
				"metadata":{"env":"dev"}
			}
		}`, JobTypeProvisionApply, tenantID.String(), templateID.String(), tenantID.String())
		payload := []byte(body)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("POST /api/v1/jobs validates tenant existence", func(t *testing.T) {
		tenant := uuid.New()
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"%s","tenant_id":"%s","node_id":"node-999"}}`, tenant.String(), templateID.String(), tenant.String()))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/jobs rejects tenant mismatch", func(t *testing.T) {
		otherTenant := uuid.New()
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"%s","tenant_id":"%s","node_id":"node-abc"}}`, tenantID.String(), templateID.String(), otherTenant.String()))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/jobs rejects missing template plan", func(t *testing.T) {
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"%s","tenant_id":"%s","node_id":"node-abc"}}`, tenantID.String(), uuid.New().String(), tenantID.String()))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("POST /api/v1/jobs rejects unpromoted template plan", func(t *testing.T) {
		unpromotedID := uuid.New()
		store.templates = append(store.templates, storage.ProvisioningTemplate{
			ID:        unpromotedID,
			Name:      "unpromoted",
			Provider:  "mock",
			Labels:    map[string]string{},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		})
		store.templateVersions[unpromotedID] = []storage.ProvisioningTemplateVersion{
			{
				ID:         uuid.New(),
				TemplateID: unpromotedID,
				Version:    1,
				Body:       "body",
				CreatedAt:  time.Now(),
			},
		}
		payload := []byte(fmt.Sprintf(`{"type":"provision.apply","tenant_id":"%s","payload":{"plan_id":"%s","tenant_id":"%s","node_id":"node-abc"}}`, tenantID.String(), unpromotedID.String(), tenantID.String()))
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, bearerReq(http.MethodPost, "/api/v1/jobs", payload))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", rec.Code)
		}
	})

	t.Run("GET /api/v1/jobs/:id returns job state", func(t *testing.T) {
		job, _ := store.CreateJob(context.Background(), &storage.Job{Type: "provision"}, &storage.JobEvent{Status: storage.JobStatusQueued})
		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/jobs/"+job.ID.String(), nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if !contains(rec.Body.String(), job.ID.String()) {
			t.Fatalf("expected response to contain job id")
		}
	})

	t.Run("GET /api/v1/jobs filters by status", func(t *testing.T) {
		jobA, _ := store.CreateJob(context.Background(), &storage.Job{TenantID: tenantID, Type: "provision.apply", Status: storage.JobStatusQueued, CreatedAt: time.Now().Add(-2 * time.Minute)}, nil)
		jobB, _ := store.CreateJob(context.Background(), &storage.Job{TenantID: tenantID, Type: "provision.apply", Status: storage.JobStatusFailed, CreatedAt: time.Now().Add(-time.Minute)}, nil)

		rec := httptest.NewRecorder()
		req := bearerReq(http.MethodGet, "/api/v1/jobs?tenant_id="+tenantID.String()+"&status="+string(storage.JobStatusFailed), nil)
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		body := rec.Body.String()
		if !contains(body, jobB.ID.String()) {
			t.Fatalf("expected failed job in response, got %s", body)
		}
		if contains(body, jobA.ID.String()) {
			t.Fatalf("expected queued job to be filtered out, got %s", body)
		}
	})

	t.Run("buildJobExecution persists success state", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.Config{
			HTTP:   config.HTTPConfig{Address: ":0"},
			TLS:    config.TLSConfig{RequireClientTLS: false},
			Auth:   authWithTokens("operator", "job-success"),
			Worker: config.WorkerConfig{RetryBackoff: 5 * time.Millisecond},
		}
		successStore := &fakeStore{}
		srv := New(logger, cfg, successStore, &stubQueue{})
		srv.auditAsync = false

		const jobType = "test.job.success"
		srv.jobHandlers = map[string]jobHandler{
			jobType: func(ctx context.Context, job *storage.Job) error {
				return nil
			},
		}

		job := &storage.Job{
			ID:         uuid.New(),
			Type:       jobType,
			Status:     storage.JobStatusQueued,
			MaxRetries: 3,
		}
		if _, err := successStore.CreateJob(context.Background(), job, &storage.JobEvent{Status: storage.JobStatusQueued}); err != nil {
			t.Fatalf("create job: %v", err)
		}

		exec := srv.buildJobExecution(job.ID, job.Type, job.MaxRetries)
		if err := exec(context.Background()); err != nil {
			t.Fatalf("execute job: %v", err)
		}

		persisted := successStore.jobs[job.ID]
		if persisted == nil {
			t.Fatalf("job not persisted")
		}
		if persisted.Status != storage.JobStatusSucceeded {
			t.Fatalf("expected succeeded status, got %s", persisted.Status)
		}
		if persisted.StartedAt == nil {
			t.Fatalf("expected started timestamp recorded")
		}
		if persisted.FinishedAt == nil {
			t.Fatalf("expected finished timestamp recorded")
		}

		events := successStore.events[job.ID]
		if len(events) != 3 {
			t.Fatalf("expected 3 events (queued, running, succeeded), got %d", len(events))
		}
		if events[1].Status != storage.JobStatusRunning {
			t.Fatalf("expected running event, got %s", events[1].Status)
		}
		if events[2].Status != storage.JobStatusSucceeded {
			t.Fatalf("expected succeeded event, got %s", events[2].Status)
		}

		logs := successStore.auditLogs
		if len(logs) != 2 {
			t.Fatalf("expected 2 audit logs, got %d", len(logs))
		}
		if logs[0].Action != "job.running" {
			t.Fatalf("expected first audit action job.running, got %s", logs[0].Action)
		}
		if attempt, _ := logs[0].Metadata["attempt"].(int); attempt != 1 {
			t.Fatalf("expected running attempt metadata 1, got %v", logs[0].Metadata["attempt"])
		}
		if logs[1].Action != "job.succeeded" {
			t.Fatalf("expected second audit action job.succeeded, got %s", logs[1].Action)
		}
		if attempt, _ := logs[1].Metadata["attempt"].(int); attempt != 1 {
			t.Fatalf("expected succeeded attempt metadata 1, got %v", logs[1].Metadata["attempt"])
		}
	})

	t.Run("buildJobExecution persists retries and failures", func(t *testing.T) {
		logger := zap.NewNop()
		cfg := &config.Config{
			HTTP:   config.HTTPConfig{Address: ":0"},
			TLS:    config.TLSConfig{RequireClientTLS: false},
			Auth:   authWithTokens("operator", "job-retry"),
			Worker: config.WorkerConfig{RetryBackoff: 5 * time.Millisecond},
		}
		retryStore := &fakeStore{}
		srv := New(logger, cfg, retryStore, &stubQueue{})
		srv.auditAsync = false

		const jobType = "test.job.retry"
		handlerErr := errors.New("boom")
		srv.jobHandlers = map[string]jobHandler{
			jobType: func(ctx context.Context, job *storage.Job) error {
				return handlerErr
			},
		}

		job := &storage.Job{
			ID:         uuid.New(),
			Type:       jobType,
			Status:     storage.JobStatusQueued,
			MaxRetries: 2,
		}
		if _, err := retryStore.CreateJob(context.Background(), job, &storage.JobEvent{Status: storage.JobStatusQueued}); err != nil {
			t.Fatalf("create job: %v", err)
		}

		exec := srv.buildJobExecution(job.ID, job.Type, job.MaxRetries)

		if err := exec(context.Background()); err == nil {
			t.Fatalf("expected error on first attempt")
		}
		persisted := retryStore.jobs[job.ID]
		if persisted.Status != storage.JobStatusQueued {
			t.Fatalf("expected job to remain queued after first failure, got %s", persisted.Status)
		}
		if persisted.Retries != 1 {
			t.Fatalf("expected retries=1 after first failure, got %d", persisted.Retries)
		}
		if persisted.FinishedAt != nil {
			t.Fatalf("expected no finished timestamp after first failure")
		}
		events := retryStore.events[job.ID]
		if len(events) != 3 {
			t.Fatalf("expected 3 events after first failure, got %d", len(events))
		}
		if events[2].Status != storage.JobStatusQueued {
			t.Fatalf("expected queued event recorded, got %s", events[2].Status)
		}

		if err := exec(context.Background()); err == nil {
			t.Fatalf("expected error on final attempt")
		}
		persisted = retryStore.jobs[job.ID]
		if persisted.Status != storage.JobStatusFailed {
			t.Fatalf("expected failed status after final attempt, got %s", persisted.Status)
		}
		if persisted.Retries != 2 {
			t.Fatalf("expected retries=2 after final attempt, got %d", persisted.Retries)
		}
		if persisted.FinishedAt == nil {
			t.Fatalf("expected finished timestamp recorded on failure")
		}
		events = retryStore.events[job.ID]
		if len(events) != 5 {
			t.Fatalf("expected 5 events after final failure, got %d", len(events))
		}
		if events[len(events)-1].Status != storage.JobStatusFailed {
			t.Fatalf("expected final event to be failed, got %s", events[len(events)-1].Status)
		}

		logs := retryStore.auditLogs
		if len(logs) != 4 {
			t.Fatalf("expected 4 audit logs recorded, got %d", len(logs))
		}
		expected := []string{"job.running", "job.retry_scheduled", "job.running", "job.failed"}
		for idx, action := range expected {
			if logs[idx].Action != action {
				t.Fatalf("expected audit action %s at index %d, got %s", action, idx, logs[idx].Action)
			}
		}
		for attemptIdx, attempt := range []int{1, 1, 2, 2} {
			if got, _ := logs[attemptIdx].Metadata["attempt"].(int); got != attempt {
				t.Fatalf("expected audit attempt %d at index %d, got %v", attempt, attemptIdx, logs[attemptIdx].Metadata["attempt"])
			}
		}
	})

	t.Run("GET /api/v1/me returns principal profile", func(t *testing.T) {
		userID := uuid.New()
		store.users = map[string]*storage.User{
			"test-token": {
				ID:          userID,
				ExternalID:  "test-token",
				Email:       storageNullString("stored@example.com"),
				DisplayName: storageNullString("Stored User"),
				CreatedAt:   time.Unix(1700000500, 0),
			},
		}
		store.userRoles = map[uuid.UUID][]string{
			userID: {"viewer", "operator"},
		}
		store.overrideRoles = map[uuid.UUID][]string{
			userID: {"viewer", "operator"},
		}

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		srv.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 got %d", rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Fatalf("expected application/json got %s", ct)
		}

		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("expected valid json: %v", err)
		}
		if subject, _ := resp["subject"].(string); subject != "test-token" {
			t.Fatalf("expected subject test-token got %v", subject)
		}

		storedRoles, _ := resp["stored_roles"].([]any)
		if len(storedRoles) != 2 {
			t.Fatalf("expected stored roles to be returned, got %v", storedRoles)
		}

		userPayload, _ := resp["user"].(map[string]any)
		if userPayload == nil {
			t.Fatalf("expected user payload")
		}
		if email, _ := userPayload["email"].(string); email != "stored@example.com" {
			t.Fatalf("expected stored email propagated, got %v", email)
		}
		if display, _ := userPayload["display_name"].(string); display != "Stored User" {
			t.Fatalf("expected display name propagated, got %v", display)
		}
	})
}

func TestAuthorizeRejectsAgentPrincipalForUserRoles(t *testing.T) {
	t.Parallel()

	srv := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/query", nil)
	req = req.WithContext(context.WithValue(req.Context(), auth.ContextKeyPrincipal, &auth.Principal{
		Type:    "agent",
		Name:    uuid.NewString(),
		Subject: "CN=" + uuid.NewString(),
		Roles:   []string{"agent"},
	}))
	rec := httptest.NewRecorder()

	if principal, ok := srv.authorize(rec, req, roleViewer); ok || principal != nil {
		t.Fatalf("agent principal authorized for viewer role")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s, want 403", rec.Code, rec.Body.String())
	}
}

type fakeStore struct {
	mu                  sync.Mutex
	nodes               []storage.Node
	tenants             []storage.Tenant
	createdNode         *storage.Node
	createdTenant       *storage.Tenant
	jobs                map[uuid.UUID]*storage.Job
	events              map[uuid.UUID][]storage.JobEvent
	complianceResults   map[uuid.UUID][]storage.ComplianceResult
	complianceEvidence  []storage.ComplianceEvidence
	auditReports        map[uuid.UUID]*storage.AuditReport
	users               map[string]*storage.User
	usersByID           map[uuid.UUID]*storage.User
	userList            []storage.User
	userRoles           map[uuid.UUID][]string
	rolesCatalog        []storage.Role
	lastUserID          uuid.UUID
	overrideRoles       map[uuid.UUID][]string
	skipUserPersistence bool
	templates           []storage.ProvisioningTemplate
	templateVersions    map[uuid.UUID][]storage.ProvisioningTemplateVersion
	templateAssignments []storage.ProvisioningTemplateAssignment
	policies            map[uuid.UUID]storage.Policy
	policyAssignments   []storage.PolicyAssignment
	auditLogs           []storage.AuditLog
	clusters            map[uuid.UUID]*storage.Cluster
	hypervisorHosts     map[uuid.UUID]*storage.HypervisorHost
	clusterMembers      map[uuid.UUID][]storage.ClusterMember
	clusterRollouts     map[uuid.UUID][]storage.ClusterRollout
	clusterRolloutWaves map[uuid.UUID][]storage.ClusterRolloutWave // keyed by rollout id
	clusterLBRegs       []storage.ClusterLBRegistration
	// nodeLabels mirrors the nodes.labels JSONB column introduced by
	// migration 0028 (Worktree A). Storing it here lets Worktree E's tests
	// assert label propagation without depending on A's merge. Keyed by node id.
	nodeLabels             map[uuid.UUID]map[string]any
	leases                 map[uuid.UUID]storage.RemediationLease
	enrollmentTokens       map[string]storage.EnrollmentToken // keyed by token hash
	remediationConfigs     map[uuid.UUID]storage.TenantRemediationConfig
	eventFilters           map[uuid.UUID]storage.TenantEventFilters
	knownDestinations      map[string]int64
	knownExeHashes         map[string]int64
	knownQueryHashes       map[string]int64
	remediationApprovals   map[uuid.UUID]storage.RemediationApproval
	actionPlans            map[uuid.UUID]storage.ActionPlan
	actionReceipts         map[uuid.UUID][]storage.ActionReceipt
	actionPlanApprovals    map[uuid.UUID][]storage.ActionPlanApproval
	patchApprovals         map[uuid.UUID]storage.PatchApproval
	circuitBreakers        map[string]storage.RemediationCircuitBreakerState // key = tenant|rule
	remediationFailRates   map[string]storage.RemediationFailRate            // key = tenant|rule, test-seeded
	nodeCertHistory        map[uuid.UUID][]storage.NodeCertHistory           // Worktree B cert rotation history
	nodePackages           map[uuid.UUID][]storage.NodePackage               // Sprint 4 packages-tab — read-back inventory
	nodeAppDependencies    map[uuid.UUID][]storage.NodeAppDependency
	vulnerabilityFindings  []storage.VulnerabilityFinding
	nodeServices           map[uuid.UUID][]storage.NodeService
	sourceProposals        []storage.ContentPackSourceProposalRecord
	sourceStates           []storage.ContentPackSourceRuntimeStateRecord
	firewallStates         map[uuid.UUID]storage.NodeFirewallState
	privateAccessFindings  []storage.PrivateAccessExposureFindingRecord
	nodeHealthScores       map[uuid.UUID]storage.NodeHealthScore
	telemetryMetrics       []storage.CreateTelemetryMetricParams
	telemetryLogs          []storage.CreateTelemetryLogParams
	agentReplayReceipts    map[string]storage.AgentIngestReplayReceipt
	alerts                 []storage.Alert
	securityCounts         storage.SecurityEventCounts
	healthCounts           storage.SecurityEventCounts
	eventIngestBacklog     storage.EventIngestBacklogSummary
	eventIngestBatches     []storage.EventIngestBatch
	eventIngestReplayByKey map[string]storage.EventIngestBatch
	eventIngestRecords     []storage.CreateEventIngestBatchParams
	activeBlocks           []storage.ActiveBlock
	patchDeployments       []storage.PatchDeployment
	ipBehaviorCountries    []storage.IPBehaviorCountrySummary
	ipBehaviorFindings     []storage.IPBehaviorFinding
	webserverInstances     []storage.WebserverInstance
	aiConfig               *storage.AIConfig
	flowDeltas             []FlowDeltaRow
	fileGrowthDeltas       []FileGrowthDeltaRow
	resourceDeltas         []ResourceDeltaRow
	logTailRows            []LogTailRow
	rootCauseFindings      []RootCauseFinding
	aiInvestigations       []storage.AIInvestigation
	aiOperatorProposals    []storage.AIOperatorProposal
	aiLogFixerActions      map[uuid.UUID]storage.AILogFixerAction // keyed by job id
	savedSearches          []storage.SavedSearch

	// UC7 — misconduct & whistleblowing.
	misconductCases   map[uuid.UUID]*storage.MisconductCase
	whistleblowerSubs []storage.WhistleblowerSubmission
	caseEvidenceLinks map[uuid.UUID][]storage.CaseEvidenceLink
	riskSignals       map[uuid.UUID][]storage.RiskSignal

	// bugs §1.3 — port observations written by the node_services -> port_observations
	// bridge in handleNodeServicesIngest. Tests assert this slice is non-empty
	// after a recommendation cycle.
	portObservations []storage.CreatePortObservationParams
}

type stubQueue struct{}

func (s *stubQueue) Enqueue(worker.Task) error {
	return nil
}

func (s *stubQueue) EnqueueAt(worker.Task, time.Time) error {
	return nil
}

func (f *fakeStore) CreateNode(_ context.Context, node *storage.Node) (*storage.Node, error) {
	if node.ID == uuid.Nil {
		node.ID = uuid.New()
	}
	if node.CreatedAt.IsZero() {
		node.CreatedAt = time.Now()
	}
	if node.UpdatedAt.IsZero() {
		node.UpdatedAt = node.CreatedAt
	}
	f.createdNode = node
	f.nodes = append(f.nodes, *node)
	return node, nil
}

func (f *fakeStore) GetNodeByHostname(_ context.Context, tenantID uuid.UUID, hostname string) (*storage.Node, error) {
	hostname = strings.TrimSpace(hostname)
	for _, node := range f.nodes {
		if node.TenantID == tenantID && strings.EqualFold(node.Hostname, hostname) {
			copy := node
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) GetNodeByMachineID(_ context.Context, tenantID uuid.UUID, machineID string) (*storage.Node, error) {
	machineID = strings.TrimSpace(machineID)
	if machineID == "" {
		return nil, nil
	}
	for _, node := range f.nodes {
		if node.TenantID == tenantID && node.MachineID.Valid && node.MachineID.String == machineID {
			copy := node
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) RetireNode(_ context.Context, id uuid.UUID) error {
	for i, node := range f.nodes {
		if node.ID == id {
			f.nodes[i].State = storage.NodeStateRetired
			f.nodes[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) ListProvisioningTemplates(_ context.Context, filter storage.ProvisioningTemplateFilter, limit, offset int) ([]storage.ProvisioningTemplate, int, error) {
	var filtered []storage.ProvisioningTemplate
	for _, tpl := range f.templates {
		if !filter.IncludeArchived && tpl.ArchivedAt.Valid {
			continue
		}
		if filter.Provider != "" && !strings.EqualFold(filter.Provider, tpl.Provider) {
			continue
		}
		if filter.NamePrefix != "" && !strings.HasPrefix(strings.ToLower(tpl.Name), strings.ToLower(filter.NamePrefix)) {
			continue
		}
		if filter.TenantID != uuid.Nil {
			tenantMatches := tpl.TenantID == filter.TenantID
			globalIncluded := filter.IncludeGlobal && tpl.TenantID == uuid.Nil
			if !tenantMatches && !globalIncluded {
				continue
			}
		}
		filtered = append(filtered, tpl)
	}
	total := len(filtered)
	if offset > total {
		return []storage.ProvisioningTemplate{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) UpdateTenant(_ context.Context, id uuid.UUID, name string) (*storage.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, tenant := range f.tenants {
		if tenant.ID == id {
			f.tenants[i].Name = name
			copy := f.tenants[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeleteTenant(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, tenant := range f.tenants {
		if tenant.ID == id {
			f.tenants = append(f.tenants[:i], f.tenants[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) CreateAuditLog(_ context.Context, entry *storage.AuditLog) (*storage.AuditLog, error) {
	if entry == nil {
		return nil, errors.New("audit entry required")
	}
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	entryCopy := *entry
	f.mu.Lock()
	f.auditLogs = append(f.auditLogs, entryCopy)
	f.mu.Unlock()
	return &entryCopy, nil
}

func (f *fakeStore) ListAuditLogs(_ context.Context, filter storage.AuditLogFilter, limit, offset int) ([]storage.AuditLog, int, error) {
	f.mu.Lock()
	logs := make([]storage.AuditLog, len(f.auditLogs))
	copy(logs, f.auditLogs)
	f.mu.Unlock()

	var filtered []storage.AuditLog
	for _, entry := range logs {
		if filter.TenantID != uuid.Nil && entry.TenantID != filter.TenantID {
			continue
		}
		if strings.TrimSpace(filter.ActorType) != "" && !strings.EqualFold(entry.ActorType, filter.ActorType) {
			continue
		}
		if strings.TrimSpace(filter.Action) != "" && !strings.EqualFold(entry.Action, filter.Action) {
			continue
		}
		if strings.TrimSpace(filter.ResourceType) != "" && !strings.EqualFold(entry.ResourceType, filter.ResourceType) {
			continue
		}
		if strings.TrimSpace(filter.ResourceID) != "" {
			if entry.ResourceID == nil || !strings.EqualFold(*entry.ResourceID, filter.ResourceID) {
				continue
			}
		}
		if filter.Since != nil && entry.CreatedAt.Before(*filter.Since) {
			continue
		}
		if filter.Until != nil && entry.CreatedAt.After(*filter.Until) {
			continue
		}
		filtered = append(filtered, entry)
	}
	total := len(filtered)
	if offset > total {
		return []storage.AuditLog{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) CreateProvisioningTemplate(_ context.Context, tpl *storage.ProvisioningTemplate) (*storage.ProvisioningTemplate, error) {
	if tpl.ID == uuid.Nil {
		tpl.ID = uuid.New()
	}
	if tpl.CreatedAt.IsZero() {
		tpl.CreatedAt = time.Now()
	}
	if tpl.UpdatedAt.IsZero() {
		tpl.UpdatedAt = tpl.CreatedAt
	}
	f.templates = append(f.templates, *tpl)
	return tpl, nil
}

func (f *fakeStore) UpdateProvisioningTemplate(_ context.Context, id uuid.UUID, params storage.UpdateProvisioningTemplateParams) (*storage.ProvisioningTemplate, error) {
	for i, tpl := range f.templates {
		if tpl.ID != id {
			continue
		}
		if params.Name != nil {
			f.templates[i].Name = strings.TrimSpace(*params.Name)
		}
		if params.Provider != nil {
			f.templates[i].Provider = strings.TrimSpace(*params.Provider)
		}
		if params.Description != nil {
			desc := strings.TrimSpace(*params.Description)
			if desc == "" {
				f.templates[i].Description = sql.NullString{}
			} else {
				f.templates[i].Description = sql.NullString{String: desc, Valid: true}
			}
		}
		if params.Labels != nil {
			f.templates[i].Labels = sanitizeLabels(*params.Labels)
		}
		if params.Archived != nil {
			if *params.Archived {
				f.templates[i].ArchivedAt = sql.NullTime{Time: time.Now(), Valid: true}
			} else {
				f.templates[i].ArchivedAt = sql.NullTime{}
			}
		}
		copy := f.templates[i]
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) GetProvisioningTemplate(_ context.Context, id uuid.UUID) (*storage.ProvisioningTemplate, error) {
	for _, tpl := range f.templates {
		if tpl.ID == id {
			copy := tpl
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateProvisioningTemplateAssignment(_ context.Context, params storage.CreateProvisioningTemplateAssignmentParams) (*storage.ProvisioningTemplateAssignment, error) {
	scopeType := strings.ToLower(strings.TrimSpace(params.ScopeType))
	if scopeType == "" {
		scopeType = storage.AssignmentScopeTenant
	}
	assignment := storage.ProvisioningTemplateAssignment{
		ID:         uuid.New(),
		TemplateID: params.TemplateID,
		TenantID:   params.TenantID,
		ScopeType:  scopeType,
		ScopeID:    params.ScopeID,
		Selector:   params.Selector,
		AssignedAt: time.Now(),
		AssignedBy: params.AssignedBy,
	}
	if assignment.Selector == nil {
		assignment.Selector = map[string]any{}
	}
	if params.ExpiresAt != nil {
		assignment.ExpiresAt = sql.NullTime{Time: *params.ExpiresAt, Valid: true}
	}
	f.templateAssignments = append(f.templateAssignments, assignment)
	return &assignment, nil
}

func (f *fakeStore) GetProvisioningTemplateAssignment(_ context.Context, id uuid.UUID) (*storage.ProvisioningTemplateAssignment, error) {
	for i := range f.templateAssignments {
		if f.templateAssignments[i].ID == id {
			copy := f.templateAssignments[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListProvisioningTemplateAssignments(_ context.Context, templateID uuid.UUID, limit, offset int) ([]storage.ProvisioningTemplateAssignment, int, error) {
	var filtered []storage.ProvisioningTemplateAssignment
	for _, assignment := range f.templateAssignments {
		if assignment.TemplateID == templateID {
			filtered = append(filtered, assignment)
		}
	}
	total := len(filtered)
	if offset > total {
		return []storage.ProvisioningTemplateAssignment{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) DeleteProvisioningTemplateAssignment(_ context.Context, id uuid.UUID) error {
	for i := range f.templateAssignments {
		if f.templateAssignments[i].ID == id {
			f.templateAssignments = append(f.templateAssignments[:i], f.templateAssignments[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) CreateProvisioningTemplateVersion(_ context.Context, params storage.CreateTemplateVersionParams) (*storage.ProvisioningTemplateVersion, error) {
	if params.TemplateID == uuid.Nil {
		return nil, errors.New("template id required")
	}
	if f.templateVersions == nil {
		f.templateVersions = make(map[uuid.UUID][]storage.ProvisioningTemplateVersion)
	}
	versionNumber := len(f.templateVersions[params.TemplateID]) + 1
	version := storage.ProvisioningTemplateVersion{
		ID:         uuid.New(),
		TemplateID: params.TemplateID,
		Version:    versionNumber,
		Body:       params.Body,
		CreatedAt:  time.Now(),
	}
	if params.Checksum != nil {
		version.Checksum = sql.NullString{String: *params.Checksum, Valid: true}
	}
	if len(params.MetadataSchema) > 0 {
		version.MetadataSchema = params.MetadataSchema
	}
	if params.RolloutNotes != nil {
		version.RolloutNotes = sql.NullString{String: *params.RolloutNotes, Valid: true}
	}
	if params.CreatedBy != nil {
		version.CreatedBy = params.CreatedBy
	}
	f.templateVersions[params.TemplateID] = append(f.templateVersions[params.TemplateID], version)
	return &version, nil
}

func (f *fakeStore) PromoteProvisioningTemplateVersion(_ context.Context, templateID uuid.UUID, versionNumber int) (*storage.ProvisioningTemplateVersion, error) {
	versions := f.templateVersions[templateID]
	if versionNumber <= 0 || versionNumber > len(versions) {
		return nil, errors.New("version not found")
	}
	version := versions[versionNumber-1]
	version.PromotedAt = sql.NullTime{Time: time.Now(), Valid: true}
	for i, tpl := range f.templates {
		if tpl.ID == templateID {
			id := version.ID
			f.templates[i].PromotedVersionID = &id
			f.templates[i].UpdatedAt = time.Now()
			break
		}
	}
	versions[versionNumber-1] = version
	f.templateVersions[templateID] = versions
	return &version, nil
}

func (f *fakeStore) GetProvisioningTemplateVersion(_ context.Context, templateID uuid.UUID, versionNumber int) (*storage.ProvisioningTemplateVersion, error) {
	versions := f.templateVersions[templateID]
	if versionNumber <= 0 || versionNumber > len(versions) {
		return nil, nil
	}
	version := versions[versionNumber-1]
	return &version, nil
}

func (f *fakeStore) GetPromotedProvisioningTemplateVersion(ctx context.Context, templateID uuid.UUID) (*storage.ProvisioningTemplateVersion, error) {
	tpl, err := f.GetProvisioningTemplate(ctx, templateID)
	if err != nil || tpl == nil || tpl.PromotedVersionID == nil {
		return nil, err
	}
	for _, version := range f.templateVersions[templateID] {
		if version.ID == *tpl.PromotedVersionID {
			copy := version
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListProvisioningTemplateVersions(_ context.Context, templateID uuid.UUID, limit, offset int) ([]storage.ProvisioningTemplateVersion, int, error) {
	versions := f.templateVersions[templateID]
	total := len(versions)
	if offset > total {
		return []storage.ProvisioningTemplateVersion{}, total, nil
	}

	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	slice := versions[offset:end]

	// Return copies to avoid mutation issues.
	out := make([]storage.ProvisioningTemplateVersion, len(slice))
	copy(out, slice)
	return out, total, nil
}

func (f *fakeStore) ListNodes(_ context.Context, tenantID uuid.UUID, hostnamePrefix string, limit, offset int) ([]storage.Node, int, error) {
	var filtered []storage.Node
	for _, node := range f.nodes {
		if tenantID != uuid.Nil && node.TenantID != tenantID {
			continue
		}
		if hostnamePrefix != "" && !strings.HasPrefix(strings.ToLower(node.Hostname), strings.ToLower(hostnamePrefix)) {
			continue
		}
		filtered = append(filtered, node)
	}
	total := len(filtered)
	if offset > len(filtered) {
		return []storage.Node{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) FindNodesByPublicIP(_ context.Context, ip string) ([]storage.Node, error) {
	var found []storage.Node
	for _, n := range f.nodes {
		if n.PublicIP.Valid && strings.EqualFold(n.PublicIP.String, ip) {
			found = append(found, n)
		}
	}
	return found, nil
}

func (f *fakeStore) GetNode(_ context.Context, id uuid.UUID) (*storage.Node, error) {
	for _, node := range f.nodes {
		if node.ID == id {
			copy := node
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) UpdateNode(_ context.Context, node *storage.Node) (*storage.Node, error) {
	for i, existing := range f.nodes {
		if existing.ID == node.ID {
			if strings.TrimSpace(node.Hostname) != "" {
				f.nodes[i].Hostname = node.Hostname
			}
			f.nodes[i].OS = node.OS
			f.nodes[i].Arch = node.Arch
			f.nodes[i].PublicIP = node.PublicIP
			f.nodes[i].UpdatedAt = time.Now()
			copy := f.nodes[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeleteNode(_ context.Context, id uuid.UUID) error {
	for i, node := range f.nodes {
		if node.ID == id {
			f.nodes = append(f.nodes[:i], f.nodes[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) CreateTenant(_ context.Context, tenant *storage.Tenant) (*storage.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if tenant.ID == uuid.Nil {
		tenant.ID = uuid.New()
	}
	if tenant.CreatedAt.IsZero() {
		tenant.CreatedAt = time.Now()
	}
	f.createdTenant = tenant
	f.tenants = append(f.tenants, *tenant)
	return tenant, nil
}

func (f *fakeStore) ListTenants(_ context.Context, prefix string, limit, offset int) ([]storage.Tenant, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	var filtered []storage.Tenant
	for _, tenant := range f.tenants {
		if prefix != "" && !strings.HasPrefix(strings.ToLower(tenant.Name), prefix) {
			continue
		}
		filtered = append(filtered, tenant)
	}
	total := len(filtered)
	if offset > len(filtered) {
		return []storage.Tenant{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) ListJobs(_ context.Context, tenantID uuid.UUID, jobType string, status storage.JobStatus, limit, offset int) ([]storage.Job, int, error) {
	var filtered []storage.Job
	for _, job := range f.jobs {
		if tenantID != uuid.Nil && job.TenantID != tenantID {
			continue
		}
		if strings.TrimSpace(jobType) != "" && !strings.EqualFold(job.Type, jobType) {
			continue
		}
		if status != "" && job.Status != status {
			continue
		}
		filtered = append(filtered, *job)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return strings.Compare(filtered[i].ID.String(), filtered[j].ID.String()) < 0
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	total := len(filtered)
	if offset > len(filtered) {
		return []storage.Job{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) GetTenant(_ context.Context, id uuid.UUID) (*storage.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tenants {
		if t.ID == id {
			copy := t
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) EnsureTenant(_ context.Context, id uuid.UUID, name string) (*storage.Tenant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, t := range f.tenants {
		if t.ID == id {
			copy := t
			return &copy, nil
		}
	}
	tenant := storage.Tenant{ID: id, Name: name, CreatedAt: time.Now()}
	f.tenants = append(f.tenants, tenant)
	return &tenant, nil
}

func (f *fakeStore) EnsureUser(_ context.Context, externalID, email, displayName string) (*storage.User, error) {
	if f.users == nil {
		f.users = make(map[string]*storage.User)
	}
	if f.usersByID == nil {
		f.usersByID = make(map[uuid.UUID]*storage.User)
	}

	if existing, ok := f.users[externalID]; ok {
		f.lastUserID = existing.ID
		return existing, nil
	}

	user := &storage.User{
		ID:          uuid.New(),
		ExternalID:  externalID,
		Email:       storageNullString(email),
		DisplayName: storageNullString(displayName),
		CreatedAt:   time.Now(),
	}
	f.lastUserID = user.ID

	if f.skipUserPersistence {
		return user, nil
	}

	f.users[externalID] = user
	f.usersByID[user.ID] = user
	f.userList = append(f.userList, *user)
	return user, nil
}

func (f *fakeStore) AssignRolesToUser(_ context.Context, userID uuid.UUID, roles []string) error {
	if f.userRoles == nil {
		f.userRoles = make(map[uuid.UUID][]string)
	}
	f.userRoles[userID] = sanitizeRoles(roles)
	return nil
}

func (f *fakeStore) ListUserRoles(_ context.Context, userID uuid.UUID) ([]string, error) {
	if f.overrideRoles != nil {
		if roles, ok := f.overrideRoles[userID]; ok {
			return sanitizeRoles(roles), nil
		}
	}
	return f.userRoles[userID], nil
}

func (f *fakeStore) GetUser(_ context.Context, userID uuid.UUID) (*storage.User, error) {
	if f.usersByID == nil {
		return nil, nil
	}
	if user, ok := f.usersByID[userID]; ok {
		copy := *user
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) ListUsers(_ context.Context, limit, offset int) ([]storage.User, int, error) {
	total := len(f.userList)
	if offset > total {
		return []storage.User{}, total, nil
	}
	slice := f.userList
	if offset > 0 {
		slice = slice[offset:]
	}
	if limit > 0 && len(slice) > limit {
		slice = slice[:limit]
	}
	copies := make([]storage.User, len(slice))
	copy(copies, slice)
	return copies, total, nil
}

func (f *fakeStore) SetUserRoles(_ context.Context, userID uuid.UUID, roles []string) error {
	if f.userRoles == nil {
		f.userRoles = make(map[uuid.UUID][]string)
	}
	f.userRoles[userID] = sanitizeRoles(roles)
	return nil
}

func (f *fakeStore) ListRoles(_ context.Context) ([]storage.Role, error) {
	if len(f.rolesCatalog) == 0 {
		return []storage.Role{
			{
				ID:        uuid.New(),
				Name:      "viewer",
				CreatedAt: time.Now(),
			},
			{
				ID:        uuid.New(),
				Name:      "operator",
				CreatedAt: time.Now(),
			},
			{
				ID:        uuid.New(),
				Name:      "admin",
				CreatedAt: time.Now(),
			},
		}, nil
	}
	out := make([]storage.Role, len(f.rolesCatalog))
	copy(out, f.rolesCatalog)
	return out, nil
}

func storageNullString(val string) sql.NullString {
	val = strings.TrimSpace(val)
	if val == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: val, Valid: true}
}

func sanitizeRoles(roles []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, role := range roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		key := strings.ToLower(role)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, role)
	}
	return out
}

func (f *fakeStore) CreateJob(_ context.Context, job *storage.Job, event *storage.JobEvent) (*storage.Job, error) {
	if f.jobs == nil {
		f.jobs = make(map[uuid.UUID]*storage.Job)
	}
	if f.events == nil {
		f.events = make(map[uuid.UUID][]storage.JobEvent)
	}
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}
	job.UpdatedAt = job.CreatedAt
	f.jobs[job.ID] = job
	if event != nil {
		if event.ID == uuid.Nil {
			event.ID = uuid.New()
		}
		event.JobID = job.ID
		if event.CreatedAt.IsZero() {
			event.CreatedAt = job.CreatedAt
		}
		f.events[job.ID] = append(f.events[job.ID], *event)
	}
	return job, nil
}

func (f *fakeStore) UpdateJobStatus(_ context.Context, jobID uuid.UUID, status storage.JobStatus, message string, fields map[string]any) error {
	if f.jobs == nil {
		return errors.New("job store empty")
	}
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	job.Status = status
	job.UpdatedAt = time.Now()
	if fields != nil {
		if started, ok := fields["started_at"].(time.Time); ok {
			job.StartedAt = &started
		}
		if finished, ok := fields["finished_at"].(time.Time); ok {
			job.FinishedAt = &finished
		}
		if retries, ok := fields["retries"].(int); ok {
			job.Retries = retries
		}
	}
	if f.events == nil {
		f.events = make(map[uuid.UUID][]storage.JobEvent)
	}
	evt := storage.JobEvent{
		ID:        uuid.New(),
		JobID:     jobID,
		Status:    status,
		Message:   message,
		CreatedAt: time.Now(),
	}
	f.events[jobID] = append(f.events[jobID], evt)
	return nil
}

func (f *fakeStore) GetJob(_ context.Context, jobID uuid.UUID) (*storage.Job, error) {
	if job, ok := f.jobs[jobID]; ok {
		return job, nil
	}
	return nil, nil
}

func (f *fakeStore) ListJobEvents(_ context.Context, jobID uuid.UUID) ([]storage.JobEvent, error) {
	return f.events[jobID], nil
}

func (f *fakeStore) CreateComplianceResults(_ context.Context, results []storage.ComplianceResult) error {
	if len(results) == 0 {
		return nil
	}
	if f.complianceResults == nil {
		f.complianceResults = make(map[uuid.UUID][]storage.ComplianceResult)
	}
	for _, result := range results {
		resultCopy := result
		f.complianceResults[result.JobID] = append(f.complianceResults[result.JobID], resultCopy)
	}
	return nil
}

func (f *fakeStore) ListComplianceResults(_ context.Context, jobID uuid.UUID) ([]storage.ComplianceResult, error) {
	results := f.complianceResults[jobID]
	if len(results) == 0 {
		return nil, nil
	}
	out := make([]storage.ComplianceResult, len(results))
	copy(out, results)
	return out, nil
}

func (f *fakeStore) GetUserByExternalID(_ context.Context, externalID string) (*storage.User, error) {
	externalID = strings.TrimSpace(externalID)
	if externalID == "" {
		return nil, errors.New("external id required")
	}
	if f.users == nil {
		return nil, nil
	}
	if user, ok := f.users[externalID]; ok {
		return user, nil
	}
	return nil, nil
}

func (f *fakeStore) CreateEntitlement(_ context.Context, params storage.CreateEntitlementParams) (*storage.AccessEntitlement, error) {
	if params.UserID == uuid.Nil {
		return nil, errors.New("user id is required")
	}
	if params.NodeID == uuid.Nil {
		return nil, errors.New("node id is required")
	}
	if strings.TrimSpace(params.Role) == "" {
		return nil, errors.New("role is required")
	}

	ent := &storage.AccessEntitlement{
		ID:        uuid.New(),
		TenantID:  params.TenantID,
		UserID:    params.UserID,
		NodeID:    params.NodeID,
		Role:      params.Role,
		Metadata:  params.Metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if params.GroupName != nil {
		ent.GroupName = sql.NullString{String: *params.GroupName, Valid: true}
	}
	if params.ExpiresAt != nil {
		ent.ExpiresAt = sql.NullTime{Time: *params.ExpiresAt, Valid: true}
	}
	if params.GrantedBy != nil {
		ent.GrantedBy = uuid.NullUUID{UUID: *params.GrantedBy, Valid: true}
		ent.GrantedAt = time.Now()
	}

	return ent, nil
}

func (f *fakeStore) CreatePolicy(_ context.Context, params storage.CreatePolicyParams) (*storage.Policy, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) UpdatePolicy(_ context.Context, id uuid.UUID, params storage.UpdatePolicyParams) (*storage.Policy, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) DeletePolicy(_ context.Context, id uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeStore) GetPolicy(_ context.Context, id uuid.UUID) (*storage.Policy, error) {
	if f.policies != nil {
		if p, ok := f.policies[id]; ok {
			copy := p
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListPolicies(_ context.Context, filter storage.PolicyFilter, limit, offset int) ([]storage.Policy, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) ListPolicyVersions(_ context.Context, policyID uuid.UUID, limit, offset int) ([]storage.PolicyVersion, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) GetPolicyVersion(_ context.Context, policyID uuid.UUID, version int) (*storage.PolicyVersion, error) {
	return nil, nil
}

func (f *fakeStore) GetPromotedPolicyVersion(_ context.Context, policyID uuid.UUID) (*storage.PolicyVersion, error) {
	return nil, nil
}

func (f *fakeStore) CreatePolicyVersion(_ context.Context, params storage.CreatePolicyVersionParams) (*storage.PolicyVersion, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) PromotePolicyVersion(_ context.Context, policyID uuid.UUID, version int) (*storage.PolicyVersion, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) ListRollouts(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.TemplateRollout, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) GetRollout(_ context.Context, id uuid.UUID) (*storage.TemplateRollout, error) {
	return nil, nil
}

func (f *fakeStore) CreateRollout(_ context.Context, params storage.CreateRolloutParams) (*storage.TemplateRollout, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) UpdateRollout(_ context.Context, id uuid.UUID, params storage.UpdateRolloutParams) (*storage.TemplateRollout, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) ListTelemetryMetrics(_ context.Context, filter storage.TelemetryMetricFilter, limit, offset int) ([]storage.TelemetryMetric, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.TelemetryMetric, 0, len(f.telemetryMetrics))
	for _, row := range f.telemetryMetrics {
		if filter.TenantID != uuid.Nil && row.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && row.NodeID != filter.NodeID {
			continue
		}
		if strings.TrimSpace(filter.MetricName) != "" && row.MetricName != strings.TrimSpace(filter.MetricName) {
			continue
		}
		if filter.Since != nil && row.Timestamp.Before(*filter.Since) {
			continue
		}
		if filter.Until != nil && row.Timestamp.After(*filter.Until) {
			continue
		}
		metric := storage.TelemetryMetric{
			ID:          uuid.New(),
			TenantID:    row.TenantID,
			NodeID:      row.NodeID,
			MetricName:  row.MetricName,
			MetricValue: row.MetricValue,
			Labels:      cloneStringMapContentPack(row.Labels),
			Timestamp:   row.Timestamp,
			CreatedAt:   row.Timestamp,
		}
		if row.MetricUnit != nil {
			metric.MetricUnit = sql.NullString{String: *row.MetricUnit, Valid: true}
		}
		if metric.Timestamp.IsZero() {
			metric.Timestamp = time.Now().UTC()
			metric.CreatedAt = metric.Timestamp
		}
		out = append(out, metric)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	total := len(out)
	if offset > total {
		return []storage.TelemetryMetric{}, total, nil
	}
	if offset > 0 {
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeStore) ListTelemetryLogs(_ context.Context, filter storage.TelemetryLogFilter, limit, offset int) ([]storage.TelemetryLog, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) CreateTelemetryMetrics(_ context.Context, metrics []storage.CreateTelemetryMetricParams) error {
	f.telemetryMetrics = append(f.telemetryMetrics, metrics...)
	return nil
}

func (f *fakeStore) CreateTelemetryLogs(_ context.Context, logs []storage.CreateTelemetryLogParams) error {
	f.telemetryLogs = append(f.telemetryLogs, logs...)
	return nil
}
func (f *fakeStore) GetAgentIngestReplayReceipt(_ context.Context, tenantID, nodeID uuid.UUID, endpoint, replayKey string) (*storage.AgentIngestReplayReceipt, error) {
	if f.agentReplayReceipts == nil {
		return nil, nil
	}
	receipt, ok := f.agentReplayReceipts[fakeAgentReplayReceiptKey(tenantID, nodeID, endpoint, replayKey)]
	if !ok {
		return nil, nil
	}
	return &receipt, nil
}
func (f *fakeStore) UpsertAgentIngestReplayReceipt(_ context.Context, p storage.UpsertAgentIngestReplayReceiptParams) error {
	if f.agentReplayReceipts == nil {
		f.agentReplayReceipts = map[string]storage.AgentIngestReplayReceipt{}
	}
	id := uuid.New()
	key := fakeAgentReplayReceiptKey(p.TenantID, p.NodeID, p.Endpoint, p.ReplayKey)
	if existing, ok := f.agentReplayReceipts[key]; ok {
		id = existing.ID
	}
	f.agentReplayReceipts[key] = storage.AgentIngestReplayReceipt{
		ID:        id,
		TenantID:  p.TenantID,
		NodeID:    p.NodeID,
		Endpoint:  strings.TrimSpace(p.Endpoint),
		ReplayKey: strings.TrimSpace(p.ReplayKey),
		Status:    firstNonEmpty(p.Status, "accepted"),
		Response:  append([]byte(nil), p.Response...),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	return nil
}

func fakeAgentReplayReceiptKey(tenantID, nodeID uuid.UUID, endpoint, replayKey string) string {
	return tenantID.String() + "/" + nodeID.String() + "/" + strings.TrimSpace(endpoint) + "/" + strings.TrimSpace(replayKey)
}

func (f *fakeStore) GetComplianceAggregation(_ context.Context, filter storage.ComplianceResultFilter) (*storage.ComplianceAggregation, error) {
	return nil, nil
}

func (f *fakeStore) GetComplianceTrends(_ context.Context, filter storage.ComplianceResultFilter, days int) ([]storage.ComplianceTrend, error) {
	return nil, nil
}

func (f *fakeStore) GetRemediationScript(_ context.Context, ruleID, platform string) (*storage.RemediationScript, error) {
	return nil, nil
}

func (f *fakeStore) GetRemediationScriptByID(_ context.Context, id uuid.UUID) (*storage.RemediationScript, error) {
	return nil, nil
}

func (f *fakeStore) ListRemediationScripts(_ context.Context, ruleID, platform string, limit, offset int) ([]storage.RemediationScript, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) CreateRemediationScript(_ context.Context, params storage.CreateRemediationScriptParams) (*storage.RemediationScript, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) UpdateRemediationScript(_ context.Context, id uuid.UUID, params storage.UpdateRemediationScriptParams) (*storage.RemediationScript, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) ListWebhooks(_ context.Context, tenantID uuid.UUID, active *bool, limit, offset int) ([]storage.Webhook, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) ListWebhooksByEvent(_ context.Context, tenantID uuid.UUID, eventType string) ([]storage.Webhook, error) {
	return nil, nil
}

func (f *fakeStore) GetEnabledWebhooksForEvent(_ context.Context, eventType string) ([]storage.Webhook, error) {
	return nil, nil
}

func (f *fakeStore) RecordWebhookDelivery(_ context.Context, delivery storage.WebhookDelivery) error {
	return nil
}

func (f *fakeStore) CreateWebhook(_ context.Context, params storage.CreateWebhookParams) (*storage.Webhook, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) GetWebhook(_ context.Context, id uuid.UUID) (*storage.Webhook, error) {
	return nil, nil
}

func (f *fakeStore) UpdateWebhook(_ context.Context, id uuid.UUID, params storage.UpdateWebhookParams) (*storage.Webhook, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) DeleteWebhook(_ context.Context, id uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeStore) ListWebhookDeliveries(_ context.Context, webhookID uuid.UUID, status *string, limit, offset int) ([]storage.WebhookDelivery, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) GetRetentionPolicy(_ context.Context, tenantID uuid.UUID, dataType string) (*storage.TelemetryRetentionPolicy, error) {
	return nil, nil
}

func (f *fakeStore) ListRetentionPolicies(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.TelemetryRetentionPolicy, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) CreateRetentionPolicy(_ context.Context, params storage.CreateRetentionPolicyParams) (*storage.TelemetryRetentionPolicy, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) DeleteExpiredTelemetry(_ context.Context, tenantID uuid.UUID, dataType string) (int64, error) {
	return 0, nil
}

func (f *fakeStore) ListSecretGroups(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.SecretGroup, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) GetSecretGroup(_ context.Context, id uuid.UUID) (*storage.SecretGroup, error) {
	return nil, nil
}

func (f *fakeStore) CreateSecretGroup(_ context.Context, params storage.CreateSecretGroupParams) (*storage.SecretGroup, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) ListSecretSyncs(_ context.Context, groupID uuid.UUID, limit, offset int) ([]storage.SecretSync, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) UpdateSecretGroupSyncStatus(_ context.Context, id uuid.UUID, status string, syncErr error) error {
	return nil
}

func (f *fakeStore) ListEntitlements(_ context.Context, filter storage.EntitlementFilter, limit, offset int) ([]storage.AccessEntitlement, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) GetEntitlement(_ context.Context, id uuid.UUID) (*storage.AccessEntitlement, error) {
	return nil, nil
}

func (f *fakeStore) UpdateEntitlement(_ context.Context, id uuid.UUID, params storage.UpdateEntitlementParams) (*storage.AccessEntitlement, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) DeleteEntitlement(_ context.Context, id uuid.UUID) error {
	return errors.New("not implemented")
}

func (f *fakeStore) RecordAccessSync(_ context.Context, tenantID, userID uuid.UUID, provider, status, message string, usersFound, groupsFound, entitlementsCreated int, syncErr error) error {
	return nil
}

func (f *fakeStore) CreateSessionRecording(_ context.Context, params storage.CreateSessionRecordingParams) (*storage.SessionRecording, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) GetSessionRecording(_ context.Context, id uuid.UUID) (*storage.SessionRecording, error) {
	return nil, nil
}

func (f *fakeStore) ListSessionRecordings(_ context.Context, params storage.ListSessionRecordingsParams, limit, offset int) ([]storage.SessionRecording, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) UpdateSessionRecording(_ context.Context, id uuid.UUID, params storage.UpdateSessionRecordingParams) (*storage.SessionRecording, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) CreateSessionEvent(_ context.Context, recordingID uuid.UUID, eventType string, timestamp time.Time, metadata map[string]any) (*storage.SessionEvent, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) ListSessionEvents(_ context.Context, recordingID uuid.UUID, limit, offset int) ([]storage.SessionEvent, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) ListComplianceResultsFiltered(_ context.Context, filter storage.ComplianceResultFilter, limit, offset int) ([]storage.ComplianceResult, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var results []storage.ComplianceResult
	for _, byJob := range f.complianceResults {
		for _, r := range byJob {
			if filter.JobID != uuid.Nil && r.JobID != filter.JobID {
				continue
			}
			if filter.TenantID != uuid.Nil && r.TenantID != filter.TenantID {
				continue
			}
			if filter.NodeID != uuid.Nil && r.NodeID != filter.NodeID {
				continue
			}
			if strings.TrimSpace(filter.RuleID) != "" && !strings.EqualFold(r.RuleID, filter.RuleID) {
				continue
			}
			if strings.TrimSpace(filter.ScanID) != "" {
				if r.ScanID == nil || !strings.EqualFold(*r.ScanID, filter.ScanID) {
					continue
				}
			}
			if filter.Passed != nil && r.Passed != *filter.Passed {
				continue
			}
			if strings.TrimSpace(filter.Severity) != "" {
				if r.Severity == nil || !strings.EqualFold(*r.Severity, filter.Severity) {
					continue
				}
			}
			if filter.Since != nil && r.CheckedAt != nil && r.CheckedAt.Before(*filter.Since) {
				continue
			}
			if filter.Until != nil && r.CheckedAt != nil && r.CheckedAt.After(*filter.Until) {
				continue
			}
			results = append(results, r)
		}
	}
	total := len(results)
	if offset > total {
		return []storage.ComplianceResult{}, total, nil
	}
	if offset > 0 {
		results = results[offset:]
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, total, nil
}

func (f *fakeStore) CreateEnrollmentToken(_ context.Context, params storage.CreateEnrollmentTokenParams) (*storage.EnrollmentToken, error) {
	return nil, nil
}

func (f *fakeStore) GetEnrollmentTokenByHash(_ context.Context, hash string) (*storage.EnrollmentToken, error) {
	if f.enrollmentTokens == nil {
		return nil, nil
	}
	tok, ok := f.enrollmentTokens[hash]
	if !ok {
		return nil, nil
	}
	copy := tok
	return &copy, nil
}

func (f *fakeStore) GetEnrollmentToken(_ context.Context, id uuid.UUID) (*storage.EnrollmentToken, error) {
	if f.enrollmentTokens == nil {
		return nil, nil
	}
	for _, tok := range f.enrollmentTokens {
		if tok.ID == id {
			copy := tok
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListEnrollmentTokens(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.EnrollmentToken, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) RevokeEnrollmentToken(_ context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeStore) IncrementEnrollmentCount(_ context.Context, id uuid.UUID) error {
	return nil
}

func (f *fakeStore) CreateFleetEnrollmentResult(_ context.Context, r *storage.FleetEnrollmentResult) error {
	return nil
}

func (f *fakeStore) ListFleetEnrollmentResults(_ context.Context, jobID uuid.UUID) ([]storage.FleetEnrollmentResult, error) {
	return nil, nil
}

func (f *fakeStore) CreatePolicyAssignment(_ context.Context, params storage.CreatePolicyAssignmentParams) (*storage.PolicyAssignment, error) {
	scopeType := strings.ToLower(strings.TrimSpace(params.ScopeType))
	if scopeType == "" {
		if params.NodeID != uuid.Nil {
			scopeType = storage.AssignmentScopeNode
			params.ScopeID = params.NodeID
		} else {
			scopeType = storage.AssignmentScopeTenant
		}
	}
	if scopeType == storage.AssignmentScopeNode && params.ScopeID == uuid.Nil {
		params.ScopeID = params.NodeID
	}
	assignment := storage.PolicyAssignment{
		ID:         uuid.New(),
		PolicyID:   params.PolicyID,
		TenantID:   params.TenantID,
		NodeID:     params.NodeID,
		ScopeType:  scopeType,
		ScopeID:    params.ScopeID,
		Selector:   params.Selector,
		AssignedAt: time.Now(),
		AssignedBy: params.AssignedBy,
	}
	if assignment.Selector == nil {
		assignment.Selector = map[string]any{}
	}
	if params.ExpiresAt != nil {
		assignment.ExpiresAt = sql.NullTime{Time: *params.ExpiresAt, Valid: true}
	}
	f.policyAssignments = append(f.policyAssignments, assignment)
	return &assignment, nil
}

func (f *fakeStore) ListPolicyAssignments(_ context.Context, policyID uuid.UUID, limit, offset int) ([]storage.PolicyAssignment, int, error) {
	var filtered []storage.PolicyAssignment
	for _, assignment := range f.policyAssignments {
		if assignment.PolicyID == policyID {
			filtered = append(filtered, assignment)
		}
	}
	total := len(filtered)
	if offset > total {
		return []storage.PolicyAssignment{}, total, nil
	}
	if offset > 0 {
		filtered = filtered[offset:]
	}
	if limit > 0 && limit < len(filtered) {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (f *fakeStore) GetPolicyAssignment(_ context.Context, id uuid.UUID) (*storage.PolicyAssignment, error) {
	for i := range f.policyAssignments {
		if f.policyAssignments[i].ID == id {
			copy := f.policyAssignments[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeletePolicyAssignment(_ context.Context, id uuid.UUID) error {
	for i := range f.policyAssignments {
		if f.policyAssignments[i].ID == id {
			f.policyAssignments = append(f.policyAssignments[:i], f.policyAssignments[i+1:]...)
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) GetEffectivePolicies(_ context.Context, tenantID, nodeID uuid.UUID) ([]storage.PolicyWithVersion, error) {
	return nil, nil
}

func (f *fakeStore) GetLatestComplianceResultForRule(_ context.Context, nodeID uuid.UUID, ruleID string) (*storage.ComplianceResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var latest *storage.ComplianceResult
	for _, batch := range f.complianceResults {
		for i := range batch {
			r := &batch[i]
			if r.NodeID == nodeID && r.RuleID == ruleID {
				if latest == nil || r.CreatedAt.After(latest.CreatedAt) {
					copy := *r
					latest = &copy
				}
			}
		}
	}
	return latest, nil
}

func (f *fakeStore) UpdateComplianceResultVerification(_ context.Context, resultID uuid.UUID, verified bool, verificationJobID *uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for jobID, batch := range f.complianceResults {
		for i := range batch {
			if batch[i].ID == resultID {
				batch[i].Verified = verified
				if verificationJobID != nil {
					jid := *verificationJobID
					batch[i].VerificationJobID = &jid
				} else {
					batch[i].VerificationJobID = nil
				}
				f.complianceResults[jobID] = batch
				return nil
			}
		}
	}
	return nil
}

func (f *fakeStore) UpdateComplianceResultRollback(_ context.Context, resultID, rollbackJobID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for jobID, batch := range f.complianceResults {
		for i := range batch {
			if batch[i].ID == resultID {
				jid := rollbackJobID
				batch[i].RollbackJobID = &jid
				f.complianceResults[jobID] = batch
				return nil
			}
		}
	}
	return nil
}

func (f *fakeStore) AcquireRemediationLease(_ context.Context, tenantID, nodeID, jobID uuid.UUID, ttl time.Duration) (*storage.RemediationLease, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.leases == nil {
		f.leases = make(map[uuid.UUID]storage.RemediationLease)
	}
	now := time.Now().UTC()
	if existing, ok := f.leases[nodeID]; ok {
		if existing.ExpiresAt.After(now) {
			return nil, storage.ErrLeaseHeld
		}
		// Expired — sweep and fall through to re-acquire.
		delete(f.leases, nodeID)
	}
	lease := storage.RemediationLease{
		NodeID:     nodeID,
		TenantID:   tenantID,
		JobID:      jobID,
		AcquiredAt: now,
		ExpiresAt:  now.Add(ttl),
	}
	f.leases[nodeID] = lease
	return &lease, nil
}

func (f *fakeStore) ReleaseRemediationLease(_ context.Context, nodeID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.leases, nodeID)
	return nil
}

func (f *fakeStore) CountTenantLeases(_ context.Context, tenantID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	count := 0
	for _, lease := range f.leases {
		if lease.TenantID == tenantID && lease.ExpiresAt.After(now) {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) CreateCluster(_ context.Context, params storage.CreateClusterParams) (*storage.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clusters == nil {
		f.clusters = map[uuid.UUID]*storage.Cluster{}
	}
	// Enforce unique (tenant_id, name).
	for _, c := range f.clusters {
		if c.TenantID == params.TenantID && c.Name == params.Name {
			return nil, errors.New("cluster with that name already exists for tenant")
		}
	}
	now := time.Now()
	state := params.State
	if strings.TrimSpace(state) == "" {
		state = "pending"
	}
	strategy := params.FailureDomainStrategy
	if strings.TrimSpace(strategy) == "" {
		strategy = "spread"
	}
	rolePlan := params.RolePlan
	if rolePlan == nil {
		rolePlan = map[string]any{}
	}
	labels := params.Labels
	if labels == nil {
		labels = map[string]any{}
	}
	cluster := &storage.Cluster{
		ID:                    uuid.New(),
		TenantID:              params.TenantID,
		Name:                  params.Name,
		Provider:              params.Provider,
		DesiredSize:           params.DesiredSize,
		RolePlan:              rolePlan,
		Labels:                labels,
		FailureDomainStrategy: strategy,
		State:                 state,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if params.TemplateID != nil && *params.TemplateID != uuid.Nil {
		cluster.TemplateID = uuid.NullUUID{UUID: *params.TemplateID, Valid: true}
	}
	f.clusters[cluster.ID] = cluster
	copy := *cluster
	return &copy, nil
}

func (f *fakeStore) ListClusters(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.Cluster, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var filtered []storage.Cluster
	for _, c := range f.clusters {
		if tenantID != uuid.Nil && c.TenantID != tenantID {
			continue
		}
		filtered = append(filtered, *c)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	total := len(filtered)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return filtered[offset:end], total, nil
}

func (f *fakeStore) GetClusterByID(_ context.Context, id uuid.UUID) (*storage.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clusters[id]; ok {
		copy := *c
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) GetClusterByName(_ context.Context, tenantID uuid.UUID, name string) (*storage.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.clusters {
		if c.TenantID == tenantID && c.Name == name {
			copy := *c
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) UpdateCluster(_ context.Context, id uuid.UUID, params storage.UpdateClusterParams) (*storage.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cluster, ok := f.clusters[id]
	if !ok {
		return nil, nil
	}
	if params.Name != nil {
		cluster.Name = *params.Name
	}
	if params.Provider != nil {
		cluster.Provider = *params.Provider
	}
	if params.DesiredSize != nil {
		cluster.DesiredSize = *params.DesiredSize
	}
	if params.RolePlan != nil {
		cluster.RolePlan = *params.RolePlan
	}
	if params.Labels != nil {
		cluster.Labels = *params.Labels
	}
	if params.FailureDomainStrategy != nil {
		cluster.FailureDomainStrategy = *params.FailureDomainStrategy
	}
	if params.State != nil {
		cluster.State = *params.State
	}
	if params.ClearTemplateID {
		cluster.TemplateID = uuid.NullUUID{}
	} else if params.TemplateID != nil && *params.TemplateID != uuid.Nil {
		cluster.TemplateID = uuid.NullUUID{UUID: *params.TemplateID, Valid: true}
	}
	cluster.UpdatedAt = time.Now()
	copy := *cluster
	return &copy, nil
}

func (f *fakeStore) DeleteCluster(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.clusters[id]; !ok {
		return sql.ErrNoRows
	}
	delete(f.clusters, id)
	delete(f.clusterMembers, id)
	delete(f.clusterRollouts, id)
	return nil
}

func (f *fakeStore) CountClustersByTenant(_ context.Context, tenantID uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, c := range f.clusters {
		if c.TenantID == tenantID {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) AddClusterMember(_ context.Context, clusterID, nodeID uuid.UUID, role string, position int) (*storage.ClusterMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clusterMembers == nil {
		f.clusterMembers = map[uuid.UUID][]storage.ClusterMember{}
	}
	for _, m := range f.clusterMembers[clusterID] {
		if m.Role == role && m.Position == position {
			return nil, errors.New("cluster member (role, position) already exists")
		}
	}
	member := storage.ClusterMember{
		ClusterID: clusterID,
		NodeID:    nodeID,
		Role:      role,
		Position:  position,
		JoinedAt:  time.Now(),
	}
	f.clusterMembers[clusterID] = append(f.clusterMembers[clusterID], member)
	copy := member
	return &copy, nil
}

func (f *fakeStore) RemoveClusterMember(_ context.Context, clusterID, nodeID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	members, ok := f.clusterMembers[clusterID]
	if !ok {
		return sql.ErrNoRows
	}
	for i, m := range members {
		if m.NodeID == nodeID {
			f.clusterMembers[clusterID] = append(members[:i], members[i+1:]...)
			// Strip any `cluster.`-prefixed labels from the node — matches
			// the real storage layer's transactional behavior.
			if existing := f.nodeLabels[nodeID]; existing != nil {
				stripped := make(map[string]any, len(existing))
				for k, v := range existing {
					if !strings.HasPrefix(k, "cluster.") {
						stripped[k] = v
					}
				}
				f.nodeLabels[nodeID] = stripped
			}
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) CreateClusterLBRegistration(_ context.Context, params storage.CreateClusterLBRegistrationParams) (*storage.ClusterLBRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Upsert semantics — match the real storage path.
	for i := range f.clusterLBRegs {
		reg := &f.clusterLBRegs[i]
		if reg.ClusterID == params.ClusterID && reg.NodeID == params.NodeID && reg.LBIdentifier == params.LBIdentifier {
			reg.Provider = params.Provider
			reg.RegisteredAt = time.Now()
			reg.DeregisteredAt = nil
			copy := *reg
			return &copy, nil
		}
	}
	reg := storage.ClusterLBRegistration{
		ClusterID:    params.ClusterID,
		NodeID:       params.NodeID,
		Provider:     params.Provider,
		LBIdentifier: params.LBIdentifier,
		RegisteredAt: time.Now(),
	}
	f.clusterLBRegs = append(f.clusterLBRegs, reg)
	copy := reg
	return &copy, nil
}

func (f *fakeStore) MarkClusterLBRegistrationDeregistered(_ context.Context, clusterID, nodeID uuid.UUID, lbIdentifier string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.clusterLBRegs {
		reg := &f.clusterLBRegs[i]
		if reg.ClusterID == clusterID && reg.NodeID == nodeID && reg.LBIdentifier == lbIdentifier {
			now := time.Now()
			reg.DeregisteredAt = &now
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) ListClusterLBRegistrationsForNode(_ context.Context, nodeID uuid.UUID) ([]storage.ClusterLBRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.ClusterLBRegistration
	for _, reg := range f.clusterLBRegs {
		if reg.NodeID == nodeID {
			out = append(out, reg)
		}
	}
	return out, nil
}

func (f *fakeStore) ListClusterLBRegistrationsForCluster(_ context.Context, clusterID uuid.UUID) ([]storage.ClusterLBRegistration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.ClusterLBRegistration
	for _, reg := range f.clusterLBRegs {
		if reg.ClusterID == clusterID {
			out = append(out, reg)
		}
	}
	return out, nil
}

func (f *fakeStore) PropagateClusterLabelsToNode(_ context.Context, clusterID, nodeID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cluster, ok := f.clusters[clusterID]
	if !ok {
		return sql.ErrNoRows
	}
	if f.nodeLabels == nil {
		f.nodeLabels = map[uuid.UUID]map[string]any{}
	}
	existing := f.nodeLabels[nodeID]
	// Keep non-cluster keys, overwrite cluster.* with fresh values.
	merged := map[string]any{}
	for k, v := range existing {
		if !strings.HasPrefix(k, "cluster.") {
			merged[k] = v
		}
	}
	for k, v := range cluster.Labels {
		if strings.TrimSpace(k) == "" {
			continue
		}
		merged["cluster."+k] = v
	}
	f.nodeLabels[nodeID] = merged
	return nil
}

func (f *fakeStore) ListClusterMembers(_ context.Context, clusterID uuid.UUID) ([]storage.ClusterMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	members := f.clusterMembers[clusterID]
	out := make([]storage.ClusterMember, len(members))
	copy(out, members)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].Position < out[j].Position
	})
	return out, nil
}

func (f *fakeStore) CreateClusterRollout(_ context.Context, params storage.CreateClusterRolloutParams) (*storage.ClusterRollout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clusterRollouts == nil {
		f.clusterRollouts = map[uuid.UUID][]storage.ClusterRollout{}
	}
	waveSize := params.WaveSize
	if waveSize == 0 {
		waveSize = 1
	}
	strategy := params.WaveStrategy
	if strings.TrimSpace(strategy) == "" {
		strategy = "rolling"
	}
	state := params.State
	if strings.TrimSpace(state) == "" {
		state = "pending"
	}
	healthGate := params.HealthGate
	if healthGate == nil {
		healthGate = map[string]any{}
	}
	rollout := storage.ClusterRollout{
		ID:                uuid.New(),
		ClusterID:         params.ClusterID,
		TemplateVersionID: params.TemplateVersionID,
		WaveSize:          waveSize,
		WaveStrategy:      strategy,
		HealthGate:        healthGate,
		State:             state,
		CurrentWave:       params.CurrentWave,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}
	f.clusterRollouts[params.ClusterID] = append(f.clusterRollouts[params.ClusterID], rollout)
	copy := rollout
	return &copy, nil
}

func (f *fakeStore) GetClusterRolloutByID(_ context.Context, id uuid.UUID) (*storage.ClusterRollout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, rollouts := range f.clusterRollouts {
		for _, r := range rollouts {
			if r.ID == id {
				copy := r
				return &copy, nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) ListClusterRollouts(_ context.Context, clusterID uuid.UUID, limit, offset int) ([]storage.ClusterRollout, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rollouts := make([]storage.ClusterRollout, len(f.clusterRollouts[clusterID]))
	copy(rollouts, f.clusterRollouts[clusterID])
	sort.SliceStable(rollouts, func(i, j int) bool {
		return rollouts[i].CreatedAt.After(rollouts[j].CreatedAt)
	})
	total := len(rollouts)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return rollouts[offset:end], total, nil
}

func (f *fakeStore) UpdateClusterRollout(_ context.Context, id uuid.UUID, params storage.UpdateClusterRolloutParams) (*storage.ClusterRollout, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for clusterID, rollouts := range f.clusterRollouts {
		for i := range rollouts {
			if rollouts[i].ID != id {
				continue
			}
			if params.WaveSize != nil {
				rollouts[i].WaveSize = *params.WaveSize
			}
			if params.WaveStrategy != nil {
				rollouts[i].WaveStrategy = *params.WaveStrategy
			}
			if params.HealthGate != nil {
				rollouts[i].HealthGate = *params.HealthGate
			}
			if params.State != nil {
				rollouts[i].State = *params.State
			}
			if params.CurrentWave != nil {
				rollouts[i].CurrentWave = *params.CurrentWave
			}
			rollouts[i].UpdatedAt = time.Now()
			f.clusterRollouts[clusterID] = rollouts
			copy := rollouts[i]
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) DeleteClusterRollout(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for clusterID, rollouts := range f.clusterRollouts {
		for i, r := range rollouts {
			if r.ID == id {
				f.clusterRollouts[clusterID] = append(rollouts[:i], rollouts[i+1:]...)
				delete(f.clusterRolloutWaves, id)
				return nil
			}
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) GetTenantRemediationConfig(_ context.Context, tenantID uuid.UUID) (*storage.TenantRemediationConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cfg, ok := f.remediationConfigs[tenantID]; ok {
		copy := cfg
		if copy.ChangeWindows == nil {
			copy.ChangeWindows = []storage.ChangeWindow{}
		}
		return &copy, nil
	}
	defaults := storage.DefaultTenantRemediationConfig(tenantID)
	return &defaults, nil
}

func (f *fakeStore) UpsertTenantRemediationConfig(_ context.Context, cfg storage.TenantRemediationConfig) (*storage.TenantRemediationConfig, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remediationConfigs == nil {
		f.remediationConfigs = map[uuid.UUID]storage.TenantRemediationConfig{}
	}
	if cfg.ChangeWindows == nil {
		cfg.ChangeWindows = []storage.ChangeWindow{}
	}
	cfg.UpdatedAt = time.Now().UTC()
	f.remediationConfigs[cfg.TenantID] = cfg
	copy := cfg
	return &copy, nil
}

func (f *fakeStore) GetTenantEventFilters(_ context.Context, tenantID uuid.UUID) (*storage.TenantEventFilters, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if cfg, ok := f.eventFilters[tenantID]; ok {
		copy := cfg
		return &copy, nil
	}
	defaults := storage.DefaultTenantEventFilters(tenantID)
	return &defaults, nil
}

func (f *fakeStore) UpsertTenantEventFilters(_ context.Context, cfg storage.TenantEventFilters) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.eventFilters == nil {
		f.eventFilters = map[uuid.UUID]storage.TenantEventFilters{}
	}
	cfg.UpdatedAt = time.Now().UTC()
	f.eventFilters[cfg.TenantID] = cfg
	return nil
}

// Anomaly baseline stubs — fakeStore tracks first-sightings in maps so
// tests can assert detector behaviour without a real Postgres.

func (f *fakeStore) UpsertKnownDestination(_ context.Context, tenantID uuid.UUID, dstIP string) (storage.UpsertKnownDestinationResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.knownDestinations == nil {
		f.knownDestinations = map[string]int64{}
	}
	key := tenantID.String() + "|" + dstIP
	if _, ok := f.knownDestinations[key]; ok {
		f.knownDestinations[key]++
		return storage.UpsertKnownDestinationResult{FirstSighting: false, ConnCount: f.knownDestinations[key]}, nil
	}
	f.knownDestinations[key] = 1
	return storage.UpsertKnownDestinationResult{FirstSighting: true, ConnCount: 1, FirstSeenAt: time.Now().UTC()}, nil
}

func (f *fakeStore) UpsertKnownExeHash(_ context.Context, tenantID uuid.UUID, hash, _ string, _ int64, _ *uuid.UUID) (storage.UpsertKnownExeHashResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.knownExeHashes == nil {
		f.knownExeHashes = map[string]int64{}
	}
	key := tenantID.String() + "|" + hash
	if _, ok := f.knownExeHashes[key]; ok {
		f.knownExeHashes[key]++
		return storage.UpsertKnownExeHashResult{FirstSighting: false, ExecCount: f.knownExeHashes[key]}, nil
	}
	f.knownExeHashes[key] = 1
	return storage.UpsertKnownExeHashResult{FirstSighting: true, ExecCount: 1}, nil
}

func (f *fakeStore) GetConnectionDurationBaseline(_ context.Context, _ uuid.UUID, _ string, _ int) (*storage.ConnectionDurationBaseline, error) {
	return nil, nil
}

func (f *fakeStore) GetConnectionBytesBaseline(_ context.Context, _ uuid.UUID, _ string, _ int) (*storage.ConnectionBytesBaseline, error) {
	return nil, nil
}

func (f *fakeStore) UpsertKnownQueryHash(_ context.Context, tenantID uuid.UUID, engine, db, user, hash, _ string, rows, execMS int64) (storage.UpsertKnownQueryHashResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.knownQueryHashes == nil {
		f.knownQueryHashes = map[string]int64{}
	}
	key := tenantID.String() + "|" + engine + "|" + db + "|" + user + "|" + hash
	if _, ok := f.knownQueryHashes[key]; ok {
		f.knownQueryHashes[key]++
		return storage.UpsertKnownQueryHashResult{FirstSighting: false, ExecCount: f.knownQueryHashes[key]}, nil
	}
	f.knownQueryHashes[key] = 1
	return storage.UpsertKnownQueryHashResult{FirstSighting: true, ExecCount: 1}, nil
}

// --- Local + LDAP auth + RBAC + dashboards (Phase 9 + 10) ---
// Tests don't exercise these paths against a real DB; the stubs return
// safe defaults so server.New can satisfy the Store interface.

func (f *fakeStore) CreateLocalUser(_ context.Context, p storage.CreateLocalUserParams) (*storage.LocalUser, error) {
	return &storage.LocalUser{ID: uuid.New(), Email: p.Email, DisplayName: p.DisplayName, AuthProvider: p.Provider}, nil
}
func (f *fakeStore) VerifyLocalUserPassword(_ context.Context, _, _ string) (*storage.LocalUser, error) {
	return nil, storage.ErrInvalidCredentials
}
func (f *fakeStore) GetLocalUserByEmail(_ context.Context, _ string) (*storage.LocalUser, error) {
	return nil, nil
}
func (f *fakeStore) SetUserPassword(_ context.Context, _ uuid.UUID, _ string) error { return nil }
func (f *fakeStore) SetUserDisabled(_ context.Context, _ uuid.UUID, _ bool) error   { return nil }
func (f *fakeStore) MarkLoginSuccess(_ context.Context, _ uuid.UUID) error          { return nil }
func (f *fakeStore) IssueSession(_ context.Context, userID uuid.UUID, ttl time.Duration, ua, ip string) (*storage.Session, error) {
	return &storage.Session{ID: uuid.New(), UserID: userID, Token: "test-token", IssuedAt: time.Now(), ExpiresAt: time.Now().Add(ttl)}, nil
}
func (f *fakeStore) ValidateSessionToken(_ context.Context, _ string) (*storage.Session, *storage.LocalUser, error) {
	return nil, nil, nil
}
func (f *fakeStore) RevokeSession(_ context.Context, _ uuid.UUID) error            { return nil }
func (f *fakeStore) RevokeAllSessionsForUser(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) PurgeExpiredSessions(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}
func (f *fakeStore) ListPermissions(_ context.Context) ([]storage.Permission, error) {
	return nil, nil
}
func (f *fakeStore) ListRolesWithPermissions(_ context.Context) ([]storage.RolePermissions, error) {
	return nil, nil
}
func (f *fakeStore) SetRolePermissions(_ context.Context, _ uuid.UUID, _ []string) error { return nil }
func (f *fakeStore) CreateCustomRole(_ context.Context, name, desc string, perms []string) (*storage.RolePermissions, error) {
	return &storage.RolePermissions{ID: uuid.New(), Name: name, Description: desc, Permissions: perms}, nil
}
func (f *fakeStore) DeleteRoleByID(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) GetUserPermissions(_ context.Context, _ uuid.UUID) ([]string, error) {
	return nil, nil
}
func (f *fakeStore) CreateDashboard(_ context.Context, t, o uuid.UUID, name, desc string, shared bool) (*storage.CustomDashboard, error) {
	return &storage.CustomDashboard{ID: uuid.New(), TenantID: t, OwnerID: o, Name: name, Description: desc, Shared: shared}, nil
}
func (f *fakeStore) ListDashboardsForUser(_ context.Context, _, _ uuid.UUID) ([]storage.CustomDashboard, error) {
	return nil, nil
}
func (f *fakeStore) GetDashboard(_ context.Context, _, _ uuid.UUID) (*storage.CustomDashboard, error) {
	return nil, nil
}
func (f *fakeStore) UpdateDashboard(_ context.Context, _, _ uuid.UUID, _, _ string, _ bool, _ json.RawMessage) error {
	return nil
}
func (f *fakeStore) DeleteDashboard(_ context.Context, _, _ uuid.UUID) error { return nil }
func (f *fakeStore) CreateWidget(_ context.Context, w storage.DashboardWidget) (*storage.DashboardWidget, error) {
	w.ID = uuid.New()
	return &w, nil
}
func (f *fakeStore) UpdateWidget(_ context.Context, _ storage.DashboardWidget) error { return nil }
func (f *fakeStore) DeleteWidget(_ context.Context, _ uuid.UUID) error               { return nil }

func (f *fakeStore) CreateRemediationApproval(_ context.Context, params storage.CreateRemediationApprovalParams) (*storage.RemediationApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.remediationApprovals == nil {
		f.remediationApprovals = map[uuid.UUID]storage.RemediationApproval{}
	}
	id := uuid.New()
	a := storage.RemediationApproval{
		ID:          id,
		TenantID:    params.TenantID,
		NodeID:      params.NodeID,
		RuleID:      params.RuleID,
		ScriptID:    params.ScriptID,
		Severity:    params.Severity,
		TaskPayload: append([]byte(nil), params.TaskPayload...),
		Status:      storage.ApprovalStatusPending,
		CreatedAt:   time.Now().UTC(),
		ExpiresAt:   params.ExpiresAt.UTC(),
	}
	f.remediationApprovals[id] = a
	copy := a
	return &copy, nil
}

func (f *fakeStore) GetRemediationApproval(_ context.Context, id uuid.UUID) (*storage.RemediationApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.remediationApprovals[id]; ok {
		copy := a
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) ListRemediationApprovals(_ context.Context, filter storage.ListRemediationApprovalsFilter, limit, offset int) ([]storage.RemediationApproval, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []storage.RemediationApproval
	for _, a := range f.remediationApprovals {
		if filter.TenantID != uuid.Nil && a.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && a.NodeID != filter.NodeID {
			continue
		}
		if string(filter.Status) != "" && a.Status != filter.Status {
			continue
		}
		all = append(all, a)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	total := len(all)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return all[offset:end], total, nil
}

func (f *fakeStore) ResolveRemediationApproval(_ context.Context, id uuid.UUID, status storage.ApprovalStatus, approverID uuid.UUID) (*storage.RemediationApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.remediationApprovals[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	if a.Status != storage.ApprovalStatusPending {
		return nil, sql.ErrNoRows
	}
	a.Status = status
	now := time.Now().UTC()
	a.ApprovedAt = &now
	if approverID != uuid.Nil {
		approver := approverID
		a.ApprovedBy = &approver
	}
	f.remediationApprovals[id] = a
	copy := a
	return &copy, nil
}

func (f *fakeStore) ExpireRemediationApprovals(_ context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	n := 0
	for id, a := range f.remediationApprovals {
		if a.Status == storage.ApprovalStatusPending && !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(now) {
			a.Status = storage.ApprovalStatusExpired
			f.remediationApprovals[id] = a
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) CreateActionPlan(_ context.Context, params storage.CreateActionPlanParams) (*storage.ActionPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.actionPlans == nil {
		f.actionPlans = map[uuid.UUID]storage.ActionPlan{}
	}
	if strings.TrimSpace(params.IdempotencyKey) != "" {
		for _, existing := range f.actionPlans {
			if existing.TenantID == params.TenantID && existing.IdempotencyKey.Valid && existing.IdempotencyKey.String == strings.TrimSpace(params.IdempotencyKey) {
				copy := copyFakeActionPlan(existing)
				return &copy, nil
			}
		}
	}
	state := params.State
	if strings.TrimSpace(string(state)) == "" {
		state = storage.ActionPlanStateProposed
	}
	risk := strings.TrimSpace(params.Risk)
	if risk == "" {
		risk = "medium"
	}
	now := time.Now().UTC()
	plan := storage.ActionPlan{
		ID:                uuid.New(),
		TenantID:          params.TenantID,
		Domain:            strings.TrimSpace(params.Domain),
		ActionKind:        strings.TrimSpace(params.ActionKind),
		State:             state,
		Risk:              risk,
		Scope:             copyFakeMap(params.Scope),
		Diff:              copyFakeMap(params.Diff),
		RequiredApprovals: copyFakeMap(params.RequiredApprovals),
		MaintenanceWindow: copyFakeMap(params.MaintenanceWindow),
		RollbackPlan:      copyFakeMap(params.RollbackPlan),
		VerificationPlan:  copyFakeMap(params.VerificationPlan),
		SourceRef:         copyFakeMap(params.SourceRef),
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if params.NodeID != nil && *params.NodeID != uuid.Nil {
		plan.NodeID = uuid.NullUUID{UUID: *params.NodeID, Valid: true}
	}
	if strings.TrimSpace(params.IdempotencyKey) != "" {
		plan.IdempotencyKey = sql.NullString{String: strings.TrimSpace(params.IdempotencyKey), Valid: true}
	}
	if params.CreatedBy != nil && *params.CreatedBy != uuid.Nil {
		plan.CreatedBy = uuid.NullUUID{UUID: *params.CreatedBy, Valid: true}
	}
	f.actionPlans[plan.ID] = plan
	copy := copyFakeActionPlan(plan)
	return &copy, nil
}

func (f *fakeStore) GetActionPlan(_ context.Context, id uuid.UUID) (*storage.ActionPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plan, ok := f.actionPlans[id]
	if !ok {
		return nil, nil
	}
	copy := copyFakeActionPlan(plan)
	return &copy, nil
}

func (f *fakeStore) ListActionPlans(_ context.Context, filter storage.ListActionPlansFilter, limit, offset int) ([]storage.ActionPlan, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []storage.ActionPlan
	for _, plan := range f.actionPlans {
		if filter.TenantID != uuid.Nil && plan.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && (!plan.NodeID.Valid || plan.NodeID.UUID != filter.NodeID) {
			continue
		}
		if strings.TrimSpace(filter.Domain) != "" && plan.Domain != strings.TrimSpace(filter.Domain) {
			continue
		}
		if strings.TrimSpace(filter.ActionKind) != "" && plan.ActionKind != strings.TrimSpace(filter.ActionKind) {
			continue
		}
		if strings.TrimSpace(string(filter.State)) != "" && plan.State != filter.State {
			continue
		}
		all = append(all, copyFakeActionPlan(plan))
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	total := len(all)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return all[offset:end], total, nil
}

func (f *fakeStore) UpdateActionPlanState(_ context.Context, id uuid.UUID, state storage.ActionPlanState) (*storage.ActionPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	plan, ok := f.actionPlans[id]
	if !ok {
		return nil, nil
	}
	plan.State = state
	plan.UpdatedAt = time.Now().UTC()
	f.actionPlans[id] = plan
	copy := copyFakeActionPlan(plan)
	return &copy, nil
}

func (f *fakeStore) CreateActionPlanApproval(_ context.Context, params storage.CreateActionPlanApprovalParams) (*storage.ActionPlanApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.actionPlanApprovals == nil {
		f.actionPlanApprovals = map[uuid.UUID][]storage.ActionPlanApproval{}
	}
	plan, ok := f.actionPlans[params.ActionPlanID]
	if !ok || plan.TenantID != params.TenantID {
		return nil, errors.New("action plan not found")
	}
	actorKey := fakeActionPlanApprovalActorKey(params.ActorID, params.ActorSubject)
	if actorKey == "" {
		return nil, errors.New("approval actor identity is required")
	}
	for _, existing := range f.actionPlanApprovals[params.ActionPlanID] {
		if existing.ActorKey == actorKey {
			return nil, errors.New("actor has already recorded an action plan approval decision")
		}
	}
	decision := strings.ToLower(strings.TrimSpace(params.Decision))
	switch decision {
	case "approve", "approved":
		decision = "approved"
	case "deny", "denied", "reject", "rejected":
		decision = "denied"
	default:
		return nil, fmt.Errorf("invalid action plan approval decision %q", params.Decision)
	}
	approval := storage.ActionPlanApproval{
		ID:           uuid.New(),
		ActionPlanID: params.ActionPlanID,
		TenantID:     params.TenantID,
		Decision:     decision,
		ActorSubject: strings.TrimSpace(params.ActorSubject),
		ActorKey:     actorKey,
		ActorRoles:   append([]string{}, params.ActorRoles...),
		Note:         strings.TrimSpace(params.Note),
		CreatedAt:    time.Now().UTC(),
	}
	if params.ActorID != nil && *params.ActorID != uuid.Nil {
		approval.ActorID = uuid.NullUUID{UUID: *params.ActorID, Valid: true}
	}
	f.actionPlanApprovals[params.ActionPlanID] = append(f.actionPlanApprovals[params.ActionPlanID], approval)
	copy := copyFakeActionPlanApproval(approval)
	return &copy, nil
}

func (f *fakeStore) ListActionPlanApprovals(_ context.Context, actionPlanID uuid.UUID) ([]storage.ActionPlanApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.actionPlanApprovals[actionPlanID]
	out := make([]storage.ActionPlanApproval, 0, len(rows))
	for _, row := range rows {
		out = append(out, copyFakeActionPlanApproval(row))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeStore) CreateActionReceipt(_ context.Context, params storage.CreateActionReceiptParams) (*storage.ActionReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.actionReceipts == nil {
		f.actionReceipts = map[uuid.UUID][]storage.ActionReceipt{}
	}
	plan, ok := f.actionPlans[params.ActionPlanID]
	if !ok || plan.TenantID != params.TenantID {
		return nil, errors.New("action plan not found")
	}
	receipt := storage.ActionReceipt{
		ID:           uuid.New(),
		ActionPlanID: params.ActionPlanID,
		TenantID:     params.TenantID,
		NodeID:       plan.NodeID,
		State:        params.State,
		Receipt:      copyFakeMap(params.Receipt),
		Verification: copyFakeMap(params.Verification),
		RollbackRef:  strings.TrimSpace(params.RollbackRef),
		Error:        strings.TrimSpace(params.Error),
		CreatedAt:    time.Now().UTC(),
	}
	if params.NodeID != nil && *params.NodeID != uuid.Nil {
		receipt.NodeID = uuid.NullUUID{UUID: *params.NodeID, Valid: true}
	}
	if params.JobID != nil && *params.JobID != uuid.Nil {
		receipt.JobID = uuid.NullUUID{UUID: *params.JobID, Valid: true}
	}
	f.actionReceipts[params.ActionPlanID] = append(f.actionReceipts[params.ActionPlanID], receipt)
	plan.State = params.State
	plan.UpdatedAt = receipt.CreatedAt
	f.actionPlans[params.ActionPlanID] = plan
	copy := copyFakeActionReceipt(receipt)
	return &copy, nil
}

func (f *fakeStore) ListActionReceipts(_ context.Context, actionPlanID uuid.UUID) ([]storage.ActionReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows := f.actionReceipts[actionPlanID]
	out := make([]storage.ActionReceipt, 0, len(rows))
	for _, row := range rows {
		out = append(out, copyFakeActionReceipt(row))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func fakeActionPlanApprovalActorKey(actorID *uuid.UUID, actorSubject string) string {
	if actorID != nil && *actorID != uuid.Nil {
		return "user:" + actorID.String()
	}
	if actorSubject = strings.TrimSpace(actorSubject); actorSubject != "" {
		return "subject:" + strings.ToLower(actorSubject)
	}
	return ""
}

func copyFakeActionPlan(in storage.ActionPlan) storage.ActionPlan {
	out := in
	out.Scope = copyFakeMap(in.Scope)
	out.Diff = copyFakeMap(in.Diff)
	out.RequiredApprovals = copyFakeMap(in.RequiredApprovals)
	out.MaintenanceWindow = copyFakeMap(in.MaintenanceWindow)
	out.RollbackPlan = copyFakeMap(in.RollbackPlan)
	out.VerificationPlan = copyFakeMap(in.VerificationPlan)
	out.SourceRef = copyFakeMap(in.SourceRef)
	return out
}

func copyFakeActionPlanApproval(in storage.ActionPlanApproval) storage.ActionPlanApproval {
	out := in
	out.ActorRoles = append([]string{}, in.ActorRoles...)
	return out
}

func copyFakeActionReceipt(in storage.ActionReceipt) storage.ActionReceipt {
	out := in
	out.Receipt = copyFakeMap(in.Receipt)
	out.Verification = copyFakeMap(in.Verification)
	return out
}

func copyFakeMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func fakeBreakerKey(tenantID uuid.UUID, ruleID string) string {
	return tenantID.String() + "|" + strings.TrimSpace(ruleID)
}

func (f *fakeStore) GetCircuitBreakerState(_ context.Context, tenantID uuid.UUID, ruleID string) (*storage.RemediationCircuitBreakerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.circuitBreakers[fakeBreakerKey(tenantID, ruleID)]; ok {
		copy := s
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) TripCircuitBreaker(_ context.Context, tenantID uuid.UUID, ruleID, reason string) (*storage.RemediationCircuitBreakerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.circuitBreakers == nil {
		f.circuitBreakers = map[string]storage.RemediationCircuitBreakerState{}
	}
	state := storage.RemediationCircuitBreakerState{
		TenantID:      tenantID,
		RuleID:        strings.TrimSpace(ruleID),
		TrippedAt:     time.Now().UTC(),
		TrippedReason: reason,
	}
	f.circuitBreakers[fakeBreakerKey(tenantID, ruleID)] = state
	copy := state
	return &copy, nil
}

func (f *fakeStore) AckCircuitBreaker(_ context.Context, tenantID uuid.UUID, ruleID string, ackerID uuid.UUID) (*storage.RemediationCircuitBreakerState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fakeBreakerKey(tenantID, ruleID)
	state, ok := f.circuitBreakers[key]
	if !ok {
		return nil, sql.ErrNoRows
	}
	now := time.Now().UTC()
	state.AckedAt = &now
	if ackerID != uuid.Nil {
		acker := ackerID
		state.AckedBy = &acker
	}
	f.circuitBreakers[key] = state
	copy := state
	return &copy, nil
}

func (f *fakeStore) RemediationFailRate(_ context.Context, tenantID uuid.UUID, ruleID string, window time.Duration) (*storage.RemediationFailRate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if rate, ok := f.remediationFailRates[fakeBreakerKey(tenantID, ruleID)]; ok {
		copy := rate
		return &copy, nil
	}
	return &storage.RemediationFailRate{}, nil
}

// ── Sprint 2 Worktree D additions ────────────────────────────────────────────

func (f *fakeStore) CreateClusterRolloutWave(_ context.Context, params storage.CreateClusterRolloutWaveParams) (*storage.ClusterRolloutWave, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.clusterRolloutWaves == nil {
		f.clusterRolloutWaves = map[uuid.UUID][]storage.ClusterRolloutWave{}
	}
	for _, existing := range f.clusterRolloutWaves[params.RolloutID] {
		if existing.WaveNumber == params.WaveNumber {
			return nil, errors.New("wave already exists")
		}
	}
	state := params.State
	if strings.TrimSpace(state) == "" {
		state = storage.ClusterRolloutWaveStateRunning
	}
	members := make([]uuid.UUID, len(params.MemberIDs))
	copy(members, params.MemberIDs)
	started := params.StartedAt
	if started.IsZero() {
		started = time.Now()
	}
	wave := storage.ClusterRolloutWave{
		ID:         uuid.New(),
		RolloutID:  params.RolloutID,
		WaveNumber: params.WaveNumber,
		MemberIDs:  members,
		State:      state,
		StartedAt:  started,
		GateResult: params.GateResult,
	}
	f.clusterRolloutWaves[params.RolloutID] = append(f.clusterRolloutWaves[params.RolloutID], wave)
	copyWave := wave
	return &copyWave, nil
}

func (f *fakeStore) GetClusterRolloutWave(_ context.Context, id uuid.UUID) (*storage.ClusterRolloutWave, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, waves := range f.clusterRolloutWaves {
		for _, w := range waves {
			if w.ID == id {
				copyWave := w
				return &copyWave, nil
			}
		}
	}
	return nil, nil
}

func (f *fakeStore) GetClusterRolloutWaveByNumber(_ context.Context, rolloutID uuid.UUID, waveNumber int) (*storage.ClusterRolloutWave, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.clusterRolloutWaves[rolloutID] {
		if w.WaveNumber == waveNumber {
			copyWave := w
			return &copyWave, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListClusterRolloutWaves(_ context.Context, rolloutID uuid.UUID) ([]storage.ClusterRolloutWave, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	waves := make([]storage.ClusterRolloutWave, len(f.clusterRolloutWaves[rolloutID]))
	copy(waves, f.clusterRolloutWaves[rolloutID])
	sort.SliceStable(waves, func(i, j int) bool {
		return waves[i].WaveNumber < waves[j].WaveNumber
	})
	return waves, nil
}

func (f *fakeStore) UpdateClusterRolloutWave(_ context.Context, id uuid.UUID, params storage.UpdateClusterRolloutWaveParams) (*storage.ClusterRolloutWave, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for rolloutID, waves := range f.clusterRolloutWaves {
		for i := range waves {
			if waves[i].ID != id {
				continue
			}
			if params.State != nil {
				waves[i].State = *params.State
			}
			if params.GateResult != nil {
				waves[i].GateResult = *params.GateResult
			}
			if params.CompletedAt != nil {
				completed := *params.CompletedAt
				waves[i].CompletedAt = &completed
			}
			f.clusterRolloutWaves[rolloutID] = waves
			copyWave := waves[i]
			return &copyWave, nil
		}
	}
	return nil, nil
}

// ── Sprint 2 Worktree B additions ────────────────────────────────────────────

func (f *fakeStore) RotateNodeCertificate(_ context.Context, nodeID uuid.UUID, serial string) (*storage.NodeCertHistory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	serial = strings.TrimSpace(serial)
	if serial == "" {
		return nil, errors.New("serial is required")
	}

	found := false
	now := time.Now().UTC()
	for i := range f.nodes {
		if f.nodes[i].ID == nodeID {
			found = true
			f.nodes[i].CertSerial = sql.NullString{String: serial, Valid: true}
			f.nodes[i].CertRotatedAt = sql.NullTime{Time: now, Valid: true}
			f.nodes[i].UpdatedAt = now
			break
		}
	}
	if !found {
		return nil, sql.ErrNoRows
	}

	newEntry := storage.NodeCertHistory{
		ID:       uuid.New(),
		NodeID:   nodeID,
		Serial:   serial,
		IssuedAt: now,
	}
	if f.nodeCertHistory == nil {
		f.nodeCertHistory = make(map[uuid.UUID][]storage.NodeCertHistory)
	}
	existing := f.nodeCertHistory[nodeID]
	// Chain: any prior unreplaced entry becomes replaced_by newEntry, revoked now.
	for i := range existing {
		if !existing[i].ReplacedBy.Valid {
			existing[i].ReplacedBy = uuid.NullUUID{UUID: newEntry.ID, Valid: true}
			existing[i].RevokedAt = sql.NullTime{Time: newEntry.IssuedAt, Valid: true}
		}
	}
	f.nodeCertHistory[nodeID] = append(existing, newEntry)
	return &newEntry, nil
}

func (f *fakeStore) GetNodeCertHistory(_ context.Context, nodeID uuid.UUID) ([]storage.NodeCertHistory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodeCertHistory == nil {
		return nil, nil
	}
	entries := f.nodeCertHistory[nodeID]
	out := make([]storage.NodeCertHistory, len(entries))
	copy(out, entries)
	return out, nil
}

func (f *fakeStore) LatestNodeCertHistory(_ context.Context, nodeID uuid.UUID) (*storage.NodeCertHistory, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodeCertHistory == nil {
		return nil, nil
	}
	entries := f.nodeCertHistory[nodeID]
	for i := len(entries) - 1; i >= 0; i-- {
		if !entries[i].ReplacedBy.Valid {
			entry := entries[i]
			return &entry, nil
		}
	}
	return nil, nil
}

// ── Sprint 2 Worktree A additions ────────────────────────────────────────────

func (f *fakeStore) SetNodeState(_ context.Context, id uuid.UUID, state string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, node := range f.nodes {
		if node.ID == id {
			f.nodes[i].State = state
			f.nodes[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) ResetNodeForReenrollment(_ context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, node := range f.nodes {
		if node.ID == id {
			f.nodes[i].State = storage.NodeStateEnrollmentPending
			f.nodes[i].LastSeenAt = nil
			f.nodes[i].FirstScanAt = nil
			f.nodes[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) SetNodeAuthToken(_ context.Context, id uuid.UUID, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, node := range f.nodes {
		if node.ID == id {
			f.nodes[i].AuthToken = sql.NullString{String: token, Valid: token != ""}
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) ValidateNodeToken(_ context.Context, token string) (*storage.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, node := range f.nodes {
		if node.AuthToken.Valid && node.AuthToken.String == token && node.State != storage.NodeStateRetired {
			n := node
			return &n, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) TouchNodeHeartbeat(_ context.Context, id uuid.UUID) (*storage.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for i, node := range f.nodes {
		if node.ID == id {
			t := now
			f.nodes[i].LastSeenAt = &t
			f.nodes[i].UpdatedAt = now
			copy := f.nodes[i]
			return &copy, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeStore) MarkNodeFirstScan(_ context.Context, id uuid.UUID) (*storage.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for i, node := range f.nodes {
		if node.ID == id {
			if f.nodes[i].FirstScanAt == nil {
				t := now
				f.nodes[i].FirstScanAt = &t
			}
			f.nodes[i].UpdatedAt = now
			copy := f.nodes[i]
			return &copy, nil
		}
	}
	return nil, sql.ErrNoRows
}

func (f *fakeStore) UpdateNodeLabels(_ context.Context, id uuid.UUID, labels map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, node := range f.nodes {
		if node.ID == id {
			if labels == nil {
				labels = map[string]any{}
			}
			f.nodes[i].Labels = labels
			f.nodes[i].UpdatedAt = time.Now()
			return nil
		}
	}
	return sql.ErrNoRows
}

func (f *fakeStore) ListEnrollmentPendingNodesOlderThan(_ context.Context, cutoff time.Time) ([]storage.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []storage.Node
	for _, node := range f.nodes {
		if node.State != storage.NodeStateEnrollmentPending {
			continue
		}
		if !node.CreatedAt.Before(cutoff) {
			continue
		}
		copy := node
		out = append(out, copy)
	}
	return out, nil
}

func (f *fakeStore) UpdateNodeAgentVersion(_ context.Context, id uuid.UUID, version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, node := range f.nodes {
		if node.ID == id {
			f.nodes[i].AgentVersion = sql.NullString{String: version, Valid: version != ""}
			return nil
		}
	}
	return nil
}

func (f *fakeStore) GetPendingAgentUpdateJob(_ context.Context, _ uuid.UUID) (*storage.Job, error) {
	return nil, nil
}

// ── Phase 1 Worktree (provider credentials + hypervisor hosts) ─────────────

func (f *fakeStore) CreateProviderCredential(_ context.Context, _ storage.CreateProviderCredentialParams) (*storage.ProviderCredential, error) {
	return nil, errors.New("provider credentials not implemented in fakeStore")
}
func (f *fakeStore) UpdateProviderCredential(_ context.Context, _ uuid.UUID, _ storage.UpdateProviderCredentialParams) (*storage.ProviderCredential, error) {
	return nil, errors.New("provider credentials not implemented in fakeStore")
}
func (f *fakeStore) GetProviderCredential(_ context.Context, _ uuid.UUID) (*storage.ProviderCredential, error) {
	return nil, nil
}
func (f *fakeStore) ListProviderCredentials(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]storage.ProviderCredential, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) DeleteProviderCredential(_ context.Context, _ uuid.UUID) error {
	return sql.ErrNoRows
}
func (f *fakeStore) CreateHypervisorHost(_ context.Context, _ storage.CreateHypervisorHostParams) (*storage.HypervisorHost, error) {
	return nil, errors.New("hypervisor hosts not implemented in fakeStore")
}
func (f *fakeStore) GetHypervisorHost(_ context.Context, id uuid.UUID) (*storage.HypervisorHost, error) {
	if f.hypervisorHosts != nil {
		if host, ok := f.hypervisorHosts[id]; ok {
			copy := *host
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) ListHypervisorHosts(_ context.Context, _ uuid.UUID, _ string, _, _ int) ([]storage.HypervisorHost, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) UpdateHypervisorHost(_ context.Context, _ uuid.UUID, _ storage.UpdateHypervisorHostParams) (*storage.HypervisorHost, error) {
	return nil, errors.New("hypervisor hosts not implemented in fakeStore")
}
func (f *fakeStore) RecordHypervisorHostHealth(_ context.Context, _ uuid.UUID, _, _ string) (*storage.HypervisorHost, error) {
	return nil, errors.New("hypervisor hosts not implemented in fakeStore")
}
func (f *fakeStore) DeleteHypervisorHost(_ context.Context, _ uuid.UUID) error {
	return sql.ErrNoRows
}

// --- Phase 2: port/log rules + dashboard events stubs ---

func (f *fakeStore) CreatePortRule(_ context.Context, _ storage.CreatePortRuleParams) (*storage.PortMonitoringRule, error) {
	return nil, errors.New("port rules not implemented in fakeStore")
}
func (f *fakeStore) GetPortRule(_ context.Context, _ uuid.UUID) (*storage.PortMonitoringRule, error) {
	return nil, nil
}
func (f *fakeStore) ListPortRules(_ context.Context, _ storage.PortRuleFilter, _, _ int) ([]storage.PortMonitoringRule, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) UpdatePortRule(_ context.Context, _ uuid.UUID, _ storage.UpdatePortRuleParams) (*storage.PortMonitoringRule, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeletePortRule(_ context.Context, _ uuid.UUID) error { return nil }

func (f *fakeStore) CreateLogRule(_ context.Context, _ storage.CreateLogRuleParams) (*storage.LogMonitoringRule, error) {
	return nil, errors.New("log rules not implemented in fakeStore")
}
func (f *fakeStore) GetLogRule(_ context.Context, _ uuid.UUID) (*storage.LogMonitoringRule, error) {
	return nil, nil
}
func (f *fakeStore) ListLogRules(_ context.Context, _ storage.LogRuleFilter, _, _ int) ([]storage.LogMonitoringRule, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) UpdateLogRule(_ context.Context, _ uuid.UUID, _ storage.UpdateLogRuleParams) (*storage.LogMonitoringRule, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeleteLogRule(_ context.Context, _ uuid.UUID) error { return nil }

func (f *fakeStore) CreateSecurityEvent(_ context.Context, _ storage.CreateSecurityEventParams) (*storage.SecurityEvent, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ListSecurityEvents(_ context.Context, _ storage.SecurityEventFilter, _, _ int) ([]storage.SecurityEvent, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) CountSecurityEvents(_ context.Context, _ storage.SecurityEventFilter) (storage.SecurityEventCounts, error) {
	return f.securityCounts, nil
}
func (f *fakeStore) CreateHealthIncident(_ context.Context, _ storage.CreateHealthIncidentParams) (*storage.HealthIncident, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ResolveHealthIncident(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) CountOpenHealthIncidents(_ context.Context, _ uuid.UUID) (storage.SecurityEventCounts, error) {
	return f.healthCounts, nil
}
func (f *fakeStore) CreateRuleTrigger(_ context.Context, _ storage.CreateRuleTriggerParams) (*storage.RuleTrigger, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) CountRuleTriggersSince(_ context.Context, _ uuid.UUID, _ time.Time) (map[string]int, error) {
	return map[string]int{}, nil
}

// --- MFA stubs (Phase 4 iter) ---

func (f *fakeStore) CreateMFAFactor(_ context.Context, _ storage.CreateMFAFactorParams) (*storage.MFAFactor, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetMFAFactor(_ context.Context, _ uuid.UUID) (*storage.MFAFactor, error) {
	return nil, nil
}
func (f *fakeStore) ListMFAFactors(_ context.Context, _ uuid.UUID) ([]storage.MFAFactor, error) {
	return nil, nil
}
func (f *fakeStore) DisableMFAFactor(_ context.Context, _ uuid.UUID) error          { return nil }
func (f *fakeStore) EnableMFAFactor(_ context.Context, _ uuid.UUID, _ string) error { return nil }
func (f *fakeStore) RecordMFAUse(_ context.Context, _ uuid.UUID, _ int64) error     { return nil }
func (f *fakeStore) CreateStepUpChallenge(_ context.Context, _ uuid.UUID, _, _ string, _ []byte, _ time.Duration) (*storage.StepUpChallenge, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) ConsumeStepUpChallenge(_ context.Context, _ uuid.UUID) (*storage.StepUpChallenge, error) {
	return nil, nil
}

// --- Phase 3 stubs (alerts, PAM, correlation, baselines) ---

func (f *fakeStore) CreateAlert(_ context.Context, _ storage.CreateAlertParams) (*storage.Alert, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetAlert(_ context.Context, id uuid.UUID) (*storage.Alert, error) {
	for _, alert := range f.alerts {
		if alert.ID == id {
			copy := alert
			if copy.Context == nil {
				copy.Context = map[string]any{}
			}
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) ListAlerts(_ context.Context, filter storage.AlertFilter, limit, offset int) ([]storage.Alert, int, error) {
	var out []storage.Alert
	for _, alert := range f.alerts {
		if filter.TenantID != uuid.Nil && alert.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && (!alert.NodeID.Valid || alert.NodeID.UUID != filter.NodeID) {
			continue
		}
		if filter.State != "" && alert.State != filter.State {
			continue
		}
		if filter.Severity != "" && alert.Severity != filter.Severity {
			continue
		}
		out = append(out, alert)
	}
	total := len(out)
	if offset > total {
		offset = total
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}
func (f *fakeStore) AckAlert(_ context.Context, _ uuid.UUID, _ uuid.UUID) error     { return nil }
func (f *fakeStore) ResolveAlert(_ context.Context, _ uuid.UUID, _ uuid.UUID) error { return nil }
func (f *fakeStore) UpdateAlertDisposition(_ context.Context, id uuid.UUID, params storage.UpdateAlertDispositionParams) (*storage.Alert, error) {
	for i := range f.alerts {
		if f.alerts[i].ID != id {
			continue
		}
		if f.alerts[i].Context == nil {
			f.alerts[i].Context = map[string]any{}
		}
		at := params.At.UTC()
		if at.IsZero() {
			at = time.Now().UTC()
		}
		entry := map[string]any{
			"value":      strings.ToLower(strings.TrimSpace(params.Disposition)),
			"updated_at": at.Format(time.RFC3339),
		}
		if reason := strings.TrimSpace(params.Reason); reason != "" {
			entry["reason"] = reason
		}
		if params.By != uuid.Nil {
			entry["updated_by"] = params.By.String()
		}
		if params.SuppressUntil != nil {
			entry["suppress_until"] = params.SuppressUntil.UTC().Format(time.RFC3339)
		}
		f.alerts[i].Context["disposition"] = entry
		switch entry["value"] {
		case "true_positive":
			if f.alerts[i].State != "resolved" {
				f.alerts[i].State = "acked"
				f.alerts[i].AckedAt = sql.NullTime{Time: at, Valid: true}
				if params.By != uuid.Nil {
					f.alerts[i].AckedBy = uuid.NullUUID{UUID: params.By, Valid: true}
				}
			}
		default:
			f.alerts[i].State = "resolved"
			f.alerts[i].ResolvedAt = sql.NullTime{Time: at, Valid: true}
			if params.By != uuid.Nil {
				f.alerts[i].ResolvedBy = uuid.NullUUID{UUID: params.By, Valid: true}
			}
		}
		copy := f.alerts[i]
		return &copy, nil
	}
	return nil, sql.ErrNoRows
}

func (f *fakeStore) CreateAccessRequest(_ context.Context, _ storage.CreateAccessRequestParams) (*storage.AccessRequest, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetAccessRequest(_ context.Context, _ uuid.UUID) (*storage.AccessRequest, error) {
	return nil, nil
}
func (f *fakeStore) ListAccessRequests(_ context.Context, _ storage.AccessRequestFilter, _, _ int) ([]storage.AccessRequest, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) DecideAccessRequest(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID, _ string, _ *time.Time) (*storage.AccessRequest, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeStore) CreateSSHCA(_ context.Context, _ storage.CreateSSHCAParams) (*storage.SSHCA, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetActiveSSHCA(_ context.Context, _ uuid.UUID) (*storage.SSHCA, error) {
	return nil, nil
}
func (f *fakeStore) CreateIssuedCert(_ context.Context, _ storage.CreateIssuedCertParams) (*storage.IssuedCert, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) NextCertSerial(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil }
func (f *fakeStore) ListIssuedCerts(_ context.Context, _ uuid.UUID, _, _ int) ([]storage.IssuedCert, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) CreateCommandACL(_ context.Context, _ storage.CreateCommandACLParams) (*storage.CommandACL, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetCommandACL(_ context.Context, _ uuid.UUID) (*storage.CommandACL, error) {
	return nil, nil
}
func (f *fakeStore) ListCommandACLs(_ context.Context, _ uuid.UUID, _, _ int) ([]storage.CommandACL, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) DeleteCommandACL(_ context.Context, _ uuid.UUID) error { return nil }

func (f *fakeStore) CreateCorrelationRule(_ context.Context, _ storage.CreateCorrelationRuleParams) (*storage.CorrelationRule, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetCorrelationRule(_ context.Context, _ uuid.UUID, _ uuid.UUID) (*storage.CorrelationRule, error) {
	return nil, nil
}
func (f *fakeStore) ListCorrelationRules(_ context.Context, _ uuid.UUID) ([]storage.CorrelationRule, error) {
	return nil, nil
}
func (f *fakeStore) DeleteCorrelationRule(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}

func (f *fakeStore) CreateCertification(_ context.Context, _ *storage.Certification) error {
	return nil
}
func (f *fakeStore) ListCertifications(_ context.Context, _ string) ([]storage.Certification, error) {
	return nil, nil
}
func (f *fakeStore) DeleteCertification(_ context.Context, _ string) error {
	return nil
}
func (f *fakeStore) CreateFAQItem(_ context.Context, _ *storage.SecurityFAQItem) error {
	return nil
}
func (f *fakeStore) ListFAQItems(_ context.Context, _ string) ([]storage.SecurityFAQItem, error) {
	return nil, nil
}
func (f *fakeStore) DeleteFAQItem(_ context.Context, _ string) error {
	return nil
}
func (f *fakeStore) CreateIncidentReport(_ context.Context, _ *storage.IncidentReport) error {
	return nil
}
func (f *fakeStore) ListIncidentReports(_ context.Context, _ string, _ int) ([]storage.IncidentReport, error) {
	return nil, nil
}
func (f *fakeStore) DeleteIncidentReport(_ context.Context, _ string) error {
	return nil
}
func (f *fakeStore) CreateSubprocessor(_ context.Context, _ *storage.Subprocessor) error {
	return nil
}
func (f *fakeStore) ListSubprocessors(_ context.Context, _ string) ([]storage.Subprocessor, error) {
	return nil, nil
}
func (f *fakeStore) DeleteSubprocessor(_ context.Context, _ string) error {
	return nil
}
func (f *fakeStore) GetTrustCenterData(_ context.Context, _ string) (*storage.TrustCenterData, error) {
	return nil, nil
}
func (f *fakeStore) GetTenantByName(_ context.Context, _ string) (*storage.Tenant, error) {
	return nil, nil
}
func (f *fakeStore) UpsertBehavioralBaseline(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _, _ string, _ map[string]any, _ int) error {
	return nil
}
func (f *fakeStore) ListBehavioralBaselines(_ context.Context, _ uuid.UUID, _ uuid.UUID) ([]storage.BehavioralBaseline, error) {
	return nil, nil
}
func (f *fakeStore) CreatePortObservation(_ context.Context, p storage.CreatePortObservationParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.portObservations = append(f.portObservations, p)
	return nil
}
func (f *fakeStore) AggregatePortObservations(_ context.Context, tenantID uuid.UUID, _ time.Time) ([]storage.PortObservationStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type key struct {
		port     int
		protocol string
		state    string
	}
	counts := map[key]int{}
	for _, o := range f.portObservations {
		if o.TenantID != tenantID {
			continue
		}
		counts[key{o.Port, o.Protocol, o.State}]++
	}
	out := make([]storage.PortObservationStats, 0, len(counts))
	for k, c := range counts {
		out = append(out, storage.PortObservationStats{
			Port:     k.port,
			Protocol: k.protocol,
			State:    k.state,
			Count:    c,
		})
	}
	return out, nil
}

// --- Threat feeds stubs ---
func (f *fakeStore) CreateThreatFeed(_ context.Context, _ storage.CreateThreatFeedParams) (*storage.ThreatFeed, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) GetThreatFeed(_ context.Context, _ uuid.UUID) (*storage.ThreatFeed, error) {
	return nil, nil
}
func (f *fakeStore) ListThreatFeeds(_ context.Context, _ storage.ThreatFeedFilter) ([]storage.ThreatFeed, error) {
	return nil, nil
}
func (f *fakeStore) UpdateThreatFeed(_ context.Context, _ uuid.UUID, _ storage.UpdateThreatFeedParams) (*storage.ThreatFeed, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeStore) DeleteThreatFeed(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) RecordThreatFeedRefresh(_ context.Context, _ uuid.UUID, _, _ string, _ int) error {
	return nil
}

// --- Event ingest journal + rollup + retention stubs ---
func (f *fakeStore) RecordEventIngest(_ context.Context, p storage.CreateEventIngestBatchParams) (uuid.UUID, error) {
	if strings.TrimSpace(p.ReplayKey) != "" {
		key := fakeEventIngestReplayKey(p)
		if f.eventIngestReplayByKey != nil {
			if batch, ok := f.eventIngestReplayByKey[key]; ok {
				return batch.ID, &storage.DuplicateEventIngestReplayError{Batch: batch}
			}
		} else {
			f.eventIngestReplayByKey = map[string]storage.EventIngestBatch{}
		}
		id := uuid.New()
		batch := storage.EventIngestBatch{
			ID:          id,
			Rows:        p.Rows,
			Status:      firstNonEmpty(p.Status, "received"),
			ReplayKey:   sql.NullString{String: strings.TrimSpace(p.ReplayKey), Valid: true},
			DorisStatus: sql.NullString{String: "", Valid: true},
		}
		if p.TenantID != nil && *p.TenantID != uuid.Nil {
			batch.TenantID = uuid.NullUUID{UUID: *p.TenantID, Valid: true}
		}
		if p.NodeID != nil && *p.NodeID != uuid.Nil {
			batch.NodeID = uuid.NullUUID{UUID: *p.NodeID, Valid: true}
		}
		f.eventIngestReplayByKey[key] = batch
		f.eventIngestRecords = append(f.eventIngestRecords, p)
		return id, nil
	}
	f.eventIngestRecords = append(f.eventIngestRecords, p)
	return uuid.New(), nil
}

func fakeEventIngestReplayKey(p storage.CreateEventIngestBatchParams) string {
	tenantID := uuid.Nil
	if p.TenantID != nil {
		tenantID = *p.TenantID
	}
	nodeID := uuid.Nil
	if p.NodeID != nil {
		nodeID = *p.NodeID
	}
	return tenantID.String() + "/" + nodeID.String() + "/" + strings.TrimSpace(p.ReplayKey)
}
func (f *fakeStore) MarkEventIngestStatus(_ context.Context, _ uuid.UUID, _, _, _ string) error {
	return nil
}
func (f *fakeStore) MarkEventIngestLocalComplete(_ context.Context, _ uuid.UUID, _ []byte, _ int) error {
	return nil
}
func (f *fakeStore) PendingEventIngestBatches(_ context.Context, _ time.Duration, _ int) ([]storage.EventIngestBatch, error) {
	return nil, nil
}
func (f *fakeStore) EventIngestBacklog(_ context.Context) (storage.EventIngestBacklogSummary, error) {
	return f.eventIngestBacklog, nil
}
func (f *fakeStore) EventIngestBacklogForTenant(_ context.Context, tenantID uuid.UUID) (storage.EventIngestBacklogSummary, error) {
	if tenantID == uuid.Nil {
		return storage.EventIngestBacklogSummary{}, errors.New("tenant_id required")
	}
	return f.eventIngestBacklog, nil
}
func (f *fakeStore) ListEventIngestBacklogBatches(_ context.Context, tenantID uuid.UUID, limit int) ([]storage.EventIngestBatch, error) {
	if tenantID == uuid.Nil {
		return nil, errors.New("tenant_id required")
	}
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	out := make([]storage.EventIngestBatch, 0, len(f.eventIngestBatches))
	for _, batch := range f.eventIngestBatches {
		if !batch.TenantID.Valid || batch.TenantID.UUID != tenantID {
			continue
		}
		out = append(out, batch)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
func (f *fakeStore) PruneAcceptedEventIngestBatches(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}
func (f *fakeStore) IncrementHourlyRollup(_ context.Context, _ uuid.UUID, _ *uuid.UUID, _ string, _ time.Time, _, _, _ int64, _ string) error {
	return nil
}
func (f *fakeStore) QueryHourlyRollup(_ context.Context, _ uuid.UUID, _, _ time.Time) ([]storage.HourlyRollupRow, error) {
	return nil, nil
}

func (f *fakeStore) CalculateRiskScore(_ context.Context, _ uuid.UUID) (*storage.RiskScore, error) {
	return &storage.RiskScore{
		Score:          0,
		MaxScore:       100,
		Percent:        0,
		TrendDirection: "stable",
		TrendDelta:     0,
		Components:     []storage.RiskComponent{},
		CalculatedAt:   time.Now().UTC(),
	}, nil
}

func (f *fakeStore) GetFindingAging(_ context.Context, _ uuid.UUID, severity string) (*storage.FindingAging, error) {
	return &storage.FindingAging{Severity: severity}, nil
}

func (f *fakeStore) GetMTTDMetrics(_ context.Context, _ uuid.UUID, severity string, _ time.Time) (*storage.MTTDMetrics, error) {
	return &storage.MTTDMetrics{Severity: severity, CalculatedAt: time.Now().UTC()}, nil
}

func (f *fakeStore) GetMTTRMetrics(_ context.Context, _ uuid.UUID, severity string, _ time.Time) (*storage.MTTRMetrics, error) {
	return &storage.MTTRMetrics{Severity: severity, CalculatedAt: time.Now().UTC()}, nil
}

func (f *fakeStore) GetRemediationVelocity(_ context.Context, _ uuid.UUID, periodDays int) (*storage.RemediationVelocity, error) {
	return &storage.RemediationVelocity{Period: fmt.Sprintf("%d days", periodDays)}, nil
}

func (f *fakeStore) GetRiskScoreHistory(_ context.Context, _ uuid.UUID, days int) ([]storage.RiskScorePoint, error) {
	if days <= 0 {
		days = 1
	}
	out := make([]storage.RiskScorePoint, 0, days)
	now := time.Now().UTC()
	for i := 0; i < days; i++ {
		out = append(out, storage.RiskScorePoint{
			Timestamp: now.AddDate(0, 0, -i),
			Score:     50,
		})
	}
	return out, nil
}

func (f *fakeStore) GetRemediationVelocityHistory(_ context.Context, _ uuid.UUID, days int) ([]storage.RemediationVelocityPoint, error) {
	if days <= 0 {
		days = 1
	}
	out := make([]storage.RemediationVelocityPoint, 0, days)
	now := time.Now().UTC()
	for i := 0; i < days; i++ {
		out = append(out, storage.RemediationVelocityPoint{
			Timestamp: now.AddDate(0, 0, -i),
			Count:     0,
		})
	}
	return out, nil
}

func (f *fakeStore) GetComplianceByFramework(_ context.Context, _ uuid.UUID) ([]storage.FrameworkComplianceSummary, error) {
	return []storage.FrameworkComplianceSummary{
		{Name: "cis-foundations", Pass: 4, Fail: 1, Coverage: 0.8},
	}, nil
}

// Data classification / DLP stubs (Sprint 2).
func (f *fakeStore) ListDataClassificationRules(_ context.Context, _ uuid.UUID) ([]storage.DataClassificationRule, error) {
	return nil, nil
}
func (f *fakeStore) CreateDataClassificationRule(_ context.Context, _ *storage.DataClassificationRule) (*storage.DataClassificationRule, error) {
	return nil, nil
}
func (f *fakeStore) DeleteDataClassificationRule(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) UpsertColumnClassification(_ context.Context, _ *storage.ColumnClassification) (*storage.ColumnClassification, error) {
	return nil, nil
}
func (f *fakeStore) ListColumnClassifications(_ context.Context, _ uuid.UUID, _, _ int) ([]storage.ColumnClassification, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) ListPIIFindings(_ context.Context, _ uuid.UUID, _ *bool, _, _ int) ([]storage.PIIFinding, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) ResolvePIIFinding(_ context.Context, _, _ uuid.UUID) error { return nil }
func (f *fakeStore) CreatePIIFinding(_ context.Context, _ *storage.PIIFinding) (*storage.PIIFinding, error) {
	return nil, nil
}

// Compliance evidence + audit reports stubs (Sprint 3).
func (f *fakeStore) CreateComplianceEvidence(_ context.Context, e *storage.ComplianceEvidence) (*storage.ComplianceEvidence, error) {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.UploadedAt.IsZero() {
		e.UploadedAt = time.Now().UTC()
	}
	f.complianceEvidence = append(f.complianceEvidence, *e)
	return e, nil
}
func (f *fakeStore) ListComplianceEvidence(_ context.Context, tenantID uuid.UUID, framework, evidenceType string, limit, offset int) ([]storage.ComplianceEvidence, int, error) {
	return f.ListComplianceEvidenceFiltered(context.Background(), storage.ComplianceEvidenceFilter{
		TenantID:       tenantID,
		Framework:      framework,
		EvidenceType:   evidenceType,
		IncludeExpired: true,
	}, limit, offset)
}
func (f *fakeStore) ListComplianceEvidenceFiltered(_ context.Context, filter storage.ComplianceEvidenceFilter, limit, offset int) ([]storage.ComplianceEvidence, int, error) {
	var out []storage.ComplianceEvidence
	expiresAtReference := time.Now().UTC()
	if filter.ExpirationReference != nil && !filter.ExpirationReference.IsZero() {
		expiresAtReference = filter.ExpirationReference.UTC()
	}
	for _, item := range f.complianceEvidence {
		if filter.TenantID != uuid.Nil && item.TenantID != filter.TenantID {
			continue
		}
		if strings.TrimSpace(filter.Framework) != "" {
			if item.Framework == nil || !strings.EqualFold(strings.TrimSpace(*item.Framework), strings.TrimSpace(filter.Framework)) {
				continue
			}
		}
		if strings.TrimSpace(filter.ControlRef) != "" {
			if item.ControlRef == nil || !strings.EqualFold(strings.TrimSpace(*item.ControlRef), strings.TrimSpace(filter.ControlRef)) {
				continue
			}
		}
		if strings.TrimSpace(filter.EvidenceType) != "" && !strings.EqualFold(item.EvidenceType, filter.EvidenceType) {
			continue
		}
		if filter.UploadedSince != nil && item.UploadedAt.Before(*filter.UploadedSince) {
			continue
		}
		if filter.UploadedUntil != nil && item.UploadedAt.After(*filter.UploadedUntil) {
			continue
		}
		if !filter.IncludeExpired && item.ExpiresAt != nil && !item.ExpiresAt.After(expiresAtReference) {
			continue
		}
		out = append(out, item)
	}
	total := len(out)
	if offset > total {
		return []storage.ComplianceEvidence{}, total, nil
	}
	if offset > 0 {
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}
func (f *fakeStore) GetComplianceEvidence(_ context.Context, id uuid.UUID) (*storage.ComplianceEvidence, error) {
	for _, item := range f.complianceEvidence {
		if item.ID == id {
			copy := item
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) DeleteComplianceEvidence(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) CreateAuditReport(_ context.Context, r *storage.AuditReport) (*storage.AuditReport, error) {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now().UTC()
	}
	if r.Status == "" {
		r.Status = "pending"
	}
	if f.auditReports == nil {
		f.auditReports = map[uuid.UUID]*storage.AuditReport{}
	}
	copy := *r
	f.auditReports[r.ID] = &copy
	return r, nil
}
func (f *fakeStore) ListAuditReports(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.AuditReport, int, error) {
	var out []storage.AuditReport
	for _, report := range f.auditReports {
		if report != nil && report.TenantID == tenantID {
			out = append(out, *report)
		}
	}
	total := len(out)
	if offset > total {
		return []storage.AuditReport{}, total, nil
	}
	if offset > 0 {
		out = out[offset:]
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}
func (f *fakeStore) GetAuditReport(_ context.Context, id uuid.UUID) (*storage.AuditReport, error) {
	if report := f.auditReports[id]; report != nil {
		copy := *report
		return &copy, nil
	}
	return nil, nil
}
func (f *fakeStore) UpdateAuditReportStatus(_ context.Context, id uuid.UUID, status string, pdfPath *string, generatedAt *time.Time) error {
	if f.auditReports == nil {
		f.auditReports = map[uuid.UUID]*storage.AuditReport{}
	}
	report := f.auditReports[id]
	if report == nil {
		report = &storage.AuditReport{ID: id}
		f.auditReports[id] = report
	}
	report.Status = status
	report.PDFPath = pdfPath
	report.GeneratedAt = generatedAt
	return nil
}

// Framework control mapping + coverage stubs (PR 1 Compliance Foundation).
func (f *fakeStore) ListControlMappings(_ context.Context, _ string) ([]storage.ControlMappingRow, error) {
	return nil, nil
}
func (f *fakeStore) GetControlCoverage(_ context.Context, _ uuid.UUID, _ string, _, _ time.Time) ([]storage.ControlCoverage, error) {
	return nil, nil
}
func (f *fakeStore) CountResultsForReport(_ context.Context, _ uuid.UUID, _ string, _, _ time.Time) (int, int, error) {
	return 0, 0, nil
}
func (f *fakeStore) GetPerNodeMatrix(_ context.Context, _ uuid.UUID, _ string, _, _ time.Time, _ int) ([]storage.NodeControlRow, error) {
	return nil, nil
}

// Heartbeat inventory + firewall stubs (PR 2).
func (f *fakeStore) ReplaceNodePackages(_ context.Context, nodeID uuid.UUID, packages []storage.NodePackage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodePackages == nil {
		f.nodePackages = make(map[uuid.UUID][]storage.NodePackage)
	}
	if len(packages) == 0 {
		delete(f.nodePackages, nodeID)
		return nil
	}
	cp := make([]storage.NodePackage, len(packages))
	copy(cp, packages)
	f.nodePackages[nodeID] = cp
	return nil
}
func (f *fakeStore) ListNodePackages(_ context.Context, nodeID uuid.UUID) ([]storage.NodePackage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pkgs, ok := f.nodePackages[nodeID]
	if !ok {
		return nil, nil
	}
	out := make([]storage.NodePackage, len(pkgs))
	copy(out, pkgs)
	return out, nil
}
func (f *fakeStore) ReplaceNodeAppDependencies(_ context.Context, nodeID, tenantID uuid.UUID, deps []storage.NodeAppDependency) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodeAppDependencies == nil {
		f.nodeAppDependencies = make(map[uuid.UUID][]storage.NodeAppDependency)
	}
	if len(deps) == 0 {
		delete(f.nodeAppDependencies, nodeID)
		return nil
	}
	now := time.Now().UTC()
	cp := make([]storage.NodeAppDependency, len(deps))
	for i, dep := range deps {
		dep.NodeID = nodeID
		dep.TenantID = tenantID
		if dep.ID == uuid.Nil {
			dep.ID = uuid.New()
		}
		if dep.ObservedAt.IsZero() {
			dep.ObservedAt = now
		}
		cp[i] = dep
	}
	f.nodeAppDependencies[nodeID] = cp
	return nil
}
func (f *fakeStore) ListNodeAppDependencies(_ context.Context, nodeID uuid.UUID) ([]storage.NodeAppDependency, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	deps, ok := f.nodeAppDependencies[nodeID]
	if !ok {
		return nil, nil
	}
	out := make([]storage.NodeAppDependency, len(deps))
	copy(out, deps)
	return out, nil
}
func (f *fakeStore) UpsertVulnerabilityFindings(_ context.Context, findings []storage.VulnerabilityFinding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for _, finding := range findings {
		if finding.ID == uuid.Nil {
			finding.ID = uuid.New()
		}
		if finding.FirstSeenAt.IsZero() {
			finding.FirstSeenAt = now
		}
		if finding.LastSeenAt.IsZero() {
			finding.LastSeenAt = now
		}
		replaced := false
		for i := range f.vulnerabilityFindings {
			existing := f.vulnerabilityFindings[i]
			if existing.TenantID == finding.TenantID &&
				existing.NodeID == finding.NodeID &&
				existing.PackageName == finding.PackageName &&
				existing.InstalledVersion == finding.InstalledVersion &&
				existing.PackageSource == finding.PackageSource &&
				existing.Arch == finding.Arch &&
				strings.EqualFold(existing.CVEID, finding.CVEID) &&
				strings.EqualFold(existing.EvidenceSource, finding.EvidenceSource) {
				f.vulnerabilityFindings[i] = finding
				replaced = true
				break
			}
		}
		if !replaced {
			f.vulnerabilityFindings = append(f.vulnerabilityFindings, finding)
		}
	}
	return nil
}
func (f *fakeStore) ReconcileVulnerabilityFindings(_ context.Context, params storage.ReconcileVulnerabilityFindingsParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	observedAt := params.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	scoped := map[string]struct{}{}
	for _, scope := range params.PackageScopes {
		scoped[fakeVulnerabilityPackageKey(scope.PackageName, scope.InstalledVersion, scope.PackageSource, scope.Arch)] = struct{}{}
	}
	if len(scoped) == 0 {
		return 0, nil
	}
	active := map[string]struct{}{}
	for _, finding := range params.ActiveFindings {
		key := fakeVulnerabilityPackageKey(finding.PackageName, finding.InstalledVersion, finding.PackageSource, finding.Arch) + "|" + strings.ToUpper(strings.TrimSpace(finding.CVEID))
		active[key] = struct{}{}
	}
	var resolved int64
	for i := range f.vulnerabilityFindings {
		finding := &f.vulnerabilityFindings[i]
		if finding.TenantID != params.TenantID ||
			finding.NodeID != params.NodeID ||
			finding.ResolvedAt != nil ||
			!strings.EqualFold(finding.EvidenceSource, params.EvidenceSource) {
			continue
		}
		scopeKey := fakeVulnerabilityPackageKey(finding.PackageName, finding.InstalledVersion, finding.PackageSource, finding.Arch)
		if _, ok := scoped[scopeKey]; !ok {
			continue
		}
		activeKey := scopeKey + "|" + strings.ToUpper(strings.TrimSpace(finding.CVEID))
		if _, ok := active[activeKey]; ok {
			continue
		}
		t := observedAt
		finding.ResolvedAt = &t
		if finding.LastSeenAt.Before(t) {
			finding.LastSeenAt = t
		}
		resolved++
	}
	return resolved, nil
}
func fakeVulnerabilityPackageKey(name, version, source, arch string) string {
	return strings.Join([]string{
		strings.ToLower(strings.TrimSpace(name)),
		strings.TrimSpace(version),
		strings.ToLower(strings.TrimSpace(source)),
		strings.ToLower(strings.TrimSpace(arch)),
	}, "\x00")
}
func (f *fakeStore) ListNodeVulnerabilityFindings(_ context.Context, filter storage.VulnerabilityFindingFilter, limit, offset int) ([]storage.VulnerabilityFinding, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.VulnerabilityFinding, 0, len(f.vulnerabilityFindings))
	for _, finding := range f.vulnerabilityFindings {
		if filter.TenantID != uuid.Nil && finding.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && finding.NodeID != filter.NodeID {
			continue
		}
		if !filter.IncludeResolved && finding.ResolvedAt != nil {
			continue
		}
		if strings.TrimSpace(filter.CVEID) != "" && !strings.EqualFold(finding.CVEID, filter.CVEID) {
			continue
		}
		if strings.TrimSpace(filter.PackageName) != "" && !strings.EqualFold(finding.PackageName, filter.PackageName) {
			continue
		}
		if strings.TrimSpace(filter.Severity) != "" && !strings.EqualFold(finding.Severity, filter.Severity) {
			continue
		}
		if filter.KEVOnly && !finding.KEV {
			continue
		}
		out = append(out, finding)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if vulnerabilitySeverityRank(out[i].Severity) != vulnerabilitySeverityRank(out[j].Severity) {
			return vulnerabilitySeverityRank(out[i].Severity) > vulnerabilitySeverityRank(out[j].Severity)
		}
		if out[i].KEV != out[j].KEV {
			return out[i].KEV
		}
		return out[i].LastSeenAt.After(out[j].LastSeenAt)
	})
	total := len(out)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return out[offset:end], total, nil
}
func (f *fakeStore) GetNodeInventorySync(_ context.Context, _ uuid.UUID) (*storage.NodeInventorySync, error) {
	return nil, nil
}
func (f *fakeStore) UpsertNodeInventorySync(_ context.Context, _ storage.NodeInventorySync) error {
	return nil
}
func (f *fakeStore) TouchNodeInventorySync(_ context.Context, _ uuid.UUID, _ string) (int64, error) {
	return 0, nil
}
func (f *fakeStore) UpsertNodeFirewallState(_ context.Context, st storage.NodeFirewallState) error {
	if f.firewallStates == nil {
		f.firewallStates = map[uuid.UUID]storage.NodeFirewallState{}
	}
	f.firewallStates[st.NodeID] = st
	return nil
}
func (f *fakeStore) GetNodeFirewallState(_ context.Context, id uuid.UUID) (*storage.NodeFirewallState, error) {
	if f.firewallStates != nil {
		if st, ok := f.firewallStates[id]; ok {
			copy := st
			return &copy, nil
		}
	}
	return nil, nil
}

// Network security stubs (PR 3)
func (f *fakeStore) CreateNodeFirewallRule(_ context.Context, _ storage.NodeFirewallRuleInsert) (*storage.NodeFirewallRule, error) {
	return nil, nil
}
func (f *fakeStore) SetNodeFirewallRuleJobID(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}
func (f *fakeStore) MarkNodeFirewallRuleApplied(_ context.Context, _ uuid.UUID) error {
	return nil
}
func (f *fakeStore) MarkNodeFirewallRuleFailed(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (f *fakeStore) MarkNodeFirewallRuleRemoved(_ context.Context, _ uuid.UUID) error {
	return nil
}
func (f *fakeStore) ListPendingNodeFirewallRules(_ context.Context, _ uuid.UUID) ([]storage.NodeFirewallRule, error) {
	return nil, nil
}
func (f *fakeStore) ListNodeFirewallRulesForEntityAction(_ context.Context, _ uuid.UUID) ([]storage.NodeFirewallRule, error) {
	return nil, nil
}
func (f *fakeStore) ListActiveBlocks(_ context.Context, tenantID uuid.UUID, limit, offset int, _ bool) ([]storage.ActiveBlock, error) {
	var out []storage.ActiveBlock
	for _, block := range f.activeBlocks {
		if tenantID == uuid.Nil || block.TenantID == tenantID {
			out = append(out, block)
		}
	}
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeStore) GetNodeFirewallRuleByJobID(_ context.Context, _ uuid.UUID) (*storage.NodeFirewallRule, error) {
	return nil, nil
}

// Agent self-update rollout stubs (PR 4a)
func (f *fakeStore) GetAgentRolloutState(_ context.Context, _ uuid.UUID) (*storage.AgentRolloutState, error) {
	return nil, nil
}
func (f *fakeStore) UpsertAgentRolloutState(_ context.Context, _ uuid.UUID, _ storage.AgentRolloutUpdate) (*storage.AgentRolloutState, error) {
	return nil, nil
}

// Patch management stubs (PR 4)
func (f *fakeStore) CreatePatchDeployment(_ context.Context, _ storage.PatchDeployment) (*storage.PatchDeployment, error) {
	return nil, nil
}
func (f *fakeStore) ListPatchDeployments(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.PatchDeployment, error) {
	var out []storage.PatchDeployment
	for _, deployment := range f.patchDeployments {
		if tenantID == uuid.Nil || deployment.TenantID == tenantID {
			out = append(out, deployment)
		}
	}
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
func (f *fakeStore) GetPatchDeployment(_ context.Context, _ uuid.UUID) (*storage.PatchDeployment, error) {
	return nil, nil
}
func (f *fakeStore) UpdatePatchDeploymentStatus(_ context.Context, _ uuid.UUID, _ string, _ bool) error {
	return nil
}
func (f *fakeStore) CreateNodePatchState(_ context.Context, _ storage.NodePatchState) (*storage.NodePatchState, error) {
	return nil, nil
}
func (f *fakeStore) SetNodePatchStateJobID(_ context.Context, _ uuid.UUID, _ uuid.UUID) error {
	return nil
}
func (f *fakeStore) MarkNodePatchApplied(_ context.Context, _ uuid.UUID, _ int, _ string) error {
	return nil
}
func (f *fakeStore) MarkNodePatchFailed(_ context.Context, _ uuid.UUID, _ string, _ string) error {
	return nil
}
func (f *fakeStore) ListPendingNodePatchStates(_ context.Context, _ uuid.UUID) ([]storage.NodePatchState, error) {
	return nil, nil
}
func (f *fakeStore) ListNodePatchStatesForDeployment(_ context.Context, _ uuid.UUID) ([]storage.NodePatchState, error) {
	return nil, nil
}
func (f *fakeStore) GetNodePatchStateByJobID(_ context.Context, _ uuid.UUID) (*storage.NodePatchState, error) {
	return nil, nil
}

// Patch approvals — D1 proper approve→dispatch loop (S4 row 8).
func (f *fakeStore) CreatePatchApproval(_ context.Context, params storage.CreatePatchApprovalParams) (*storage.PatchApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.patchApprovals == nil {
		f.patchApprovals = map[uuid.UUID]storage.PatchApproval{}
	}
	id := uuid.New()
	a := storage.PatchApproval{
		ID:           id,
		TenantID:     params.TenantID,
		DeploymentID: params.DeploymentID,
		NodeID:       params.NodeID,
		Mode:         params.Mode,
		ProxyID:      params.ProxyID,
		WindowID:     params.WindowID,
		Status:       storage.ApprovalStatusPending,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    params.ExpiresAt.UTC(),
	}
	f.patchApprovals[id] = a
	copy := a
	return &copy, nil
}

func (f *fakeStore) GetPatchApproval(_ context.Context, id uuid.UUID) (*storage.PatchApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.patchApprovals[id]; ok {
		copy := a
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) ListPatchApprovals(_ context.Context, filter storage.ListPatchApprovalsFilter, limit, offset int) ([]storage.PatchApproval, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var all []storage.PatchApproval
	for _, a := range f.patchApprovals {
		if filter.TenantID != uuid.Nil && a.TenantID != filter.TenantID {
			continue
		}
		if filter.DeploymentID != uuid.Nil && a.DeploymentID != filter.DeploymentID {
			continue
		}
		if filter.NodeID != uuid.Nil && a.NodeID != filter.NodeID {
			continue
		}
		if string(filter.Status) != "" && a.Status != filter.Status {
			continue
		}
		all = append(all, a)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt.After(all[j].CreatedAt) })
	total := len(all)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return all[offset:end], total, nil
}

func (f *fakeStore) ResolvePatchApproval(_ context.Context, id uuid.UUID, status storage.ApprovalStatus, approverID uuid.UUID) (*storage.PatchApproval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.patchApprovals[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	if a.Status != storage.ApprovalStatusPending {
		return nil, sql.ErrNoRows
	}
	a.Status = status
	now := time.Now().UTC()
	a.ApprovedAt = &now
	if approverID != uuid.Nil {
		approver := approverID
		a.ApprovedBy = &approver
	}
	f.patchApprovals[id] = a
	copy := a
	return &copy, nil
}

func (f *fakeStore) ExpirePatchApprovals(_ context.Context, now time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	n := 0
	for id, a := range f.patchApprovals {
		if a.Status == storage.ApprovalStatusPending && !a.ExpiresAt.IsZero() && a.ExpiresAt.Before(now) {
			a.Status = storage.ApprovalStatusExpired
			f.patchApprovals[id] = a
			n++
		}
	}
	return n, nil
}

// Patch management — Wave C stubs.
func (f *fakeStore) GetNodePatchConfig(_ context.Context, _ uuid.UUID) (*storage.NodePatchConfig, error) {
	return nil, nil
}
func (f *fakeStore) UpsertNodePatchConfig(_ context.Context, in storage.NodePatchConfig) (*storage.NodePatchConfig, error) {
	return &in, nil
}
func (f *fakeStore) CreateMaintenanceWindow(_ context.Context, in storage.MaintenanceWindow) (*storage.MaintenanceWindow, error) {
	return &in, nil
}
func (f *fakeStore) GetMaintenanceWindow(_ context.Context, _ uuid.UUID) (*storage.MaintenanceWindow, error) {
	return nil, nil
}
func (f *fakeStore) ListMaintenanceWindows(_ context.Context, _ uuid.UUID) ([]storage.MaintenanceWindow, error) {
	return nil, nil
}
func (f *fakeStore) MarkMaintenanceWindowOpen(_ context.Context, _ uuid.UUID, _ *uuid.UUID) error {
	return nil
}
func (f *fakeStore) MarkMaintenanceWindowClosing(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) MarkMaintenanceWindowClosed(_ context.Context, _ uuid.UUID) error  { return nil }
func (f *fakeStore) MarkMaintenanceWindowAborted(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) ForceCloseMaintenanceWindow(_ context.Context, _ uuid.UUID) error  { return nil }
func (f *fakeStore) CreateSquidProxy(_ context.Context, in storage.SquidProxy) (*storage.SquidProxy, error) {
	return &in, nil
}
func (f *fakeStore) GetSquidProxy(_ context.Context, _ uuid.UUID) (*storage.SquidProxy, error) {
	return nil, nil
}
func (f *fakeStore) ListSquidProxies(_ context.Context, _ uuid.UUID) ([]storage.SquidProxy, error) {
	return nil, nil
}
func (f *fakeStore) UpdateSquidProxyStatus(_ context.Context, _ uuid.UUID, _ string, _ string) error {
	return nil
}
func (f *fakeStore) UpdateSquidProxyWhitelist(_ context.Context, _ uuid.UUID, _ []string) error {
	return nil
}

// ComplianceReview stubs
func (f *fakeStore) ListComplianceReviews(_ context.Context, _ uuid.UUID, _, _ int) ([]storage.ComplianceReview, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) CreateComplianceReview(_ context.Context, r *storage.ComplianceReview) (*storage.ComplianceReview, error) {
	return r, nil
}
func (f *fakeStore) GetComplianceReview(_ context.Context, _ uuid.UUID) (*storage.ComplianceReview, error) {
	return nil, nil
}
func (f *fakeStore) CompleteComplianceReview(_ context.Context, _ uuid.UUID, _ uuid.UUID, _ *string) error {
	return nil
}
func (f *fakeStore) DeleteComplianceReview(_ context.Context, _ uuid.UUID) error {
	return nil
}

// Predictive server downtime stubs (Use Case 5 / PR 31)
func (f *fakeStore) GetNodeHealthScore(_ context.Context, id uuid.UUID) (*storage.NodeHealthScore, error) {
	if f.nodeHealthScores != nil {
		if score, ok := f.nodeHealthScores[id]; ok {
			copy := score
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) UpsertNodeHealthScore(_ context.Context, params storage.UpsertNodeHealthScoreParams) (*storage.NodeHealthScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.nodeHealthScores == nil {
		f.nodeHealthScores = map[uuid.UUID]storage.NodeHealthScore{}
	}
	score := storage.NodeHealthScore{
		NodeID:     params.NodeID,
		Score:      params.Score,
		RiskLevel:  params.RiskLevel,
		Components: params.Components,
		ComputedAt: time.Now().UTC(),
	}
	f.nodeHealthScores[params.NodeID] = score
	copy := score
	return &copy, nil
}
func (f *fakeStore) ListAtRiskNodes(_ context.Context, _ uuid.UUID, _ int) ([]storage.AtRiskNodeRow, error) {
	return nil, nil
}

// Misconduct & whistleblowing stubs (UC7). The fakeStore keeps an in-memory
// map of cases + submissions + signals so handler tests can round-trip
// without a real database.
func (f *fakeStore) ensureMisconductMaps() {
	if f.misconductCases == nil {
		f.misconductCases = map[uuid.UUID]*storage.MisconductCase{}
	}
	if f.whistleblowerSubs == nil {
		f.whistleblowerSubs = []storage.WhistleblowerSubmission{}
	}
	if f.caseEvidenceLinks == nil {
		f.caseEvidenceLinks = map[uuid.UUID][]storage.CaseEvidenceLink{}
	}
	if f.riskSignals == nil {
		f.riskSignals = map[uuid.UUID][]storage.RiskSignal{}
	}
}

func (f *fakeStore) CreateMisconductCase(_ context.Context, p storage.CreateMisconductCaseParams) (*storage.MisconductCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	now := time.Now()
	c := &storage.MisconductCase{
		ID:            uuid.New(),
		TenantID:      p.TenantID,
		Status:        "open",
		OpenedAt:      now,
		OpenedBy:      p.OpenedBy,
		Summary:       p.Summary,
		SubjectUserID: p.SubjectUserID,
		SubjectLabel:  p.SubjectLabel,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	f.misconductCases[c.ID] = c
	return c, nil
}

func (f *fakeStore) GetMisconductCase(_ context.Context, id uuid.UUID) (*storage.MisconductCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	if c, ok := f.misconductCases[id]; ok {
		copy := *c
		return &copy, nil
	}
	return nil, nil
}

func (f *fakeStore) ListMisconductCases(_ context.Context, filter storage.MisconductCaseFilter, limit, offset int) ([]storage.MisconductCase, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	out := []storage.MisconductCase{}
	for _, c := range f.misconductCases {
		if c.TenantID != filter.TenantID {
			continue
		}
		if filter.Status != "" && c.Status != filter.Status {
			continue
		}
		out = append(out, *c)
	}
	total := len(out)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return out[offset:end], total, nil
}

func (f *fakeStore) UpdateMisconductCase(_ context.Context, id uuid.UUID, p storage.UpdateMisconductCaseParams) (*storage.MisconductCase, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	c, ok := f.misconductCases[id]
	if !ok {
		return nil, nil
	}
	if p.Status != "" {
		c.Status = p.Status
	}
	if p.Summary != nil {
		c.Summary = *p.Summary
	}
	if p.RiskScore != nil {
		c.RiskScore = *p.RiskScore
	}
	if p.SubjectUserID != nil {
		v := *p.SubjectUserID
		c.SubjectUserID = &v
	}
	if p.SubjectLabel != nil {
		v := *p.SubjectLabel
		c.SubjectLabel = &v
	}
	c.UpdatedAt = time.Now()
	copy := *c
	return &copy, nil
}

func (f *fakeStore) SetMisconductCaseRiskScore(_ context.Context, id uuid.UUID, score int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	if c, ok := f.misconductCases[id]; ok {
		c.RiskScore = score
		c.UpdatedAt = time.Now()
	}
	return nil
}

func (f *fakeStore) CreateWhistleblowerSubmission(_ context.Context, p storage.CreateWhistleblowerSubmissionParams) (*storage.WhistleblowerSubmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	ws := storage.WhistleblowerSubmission{
		ID:             uuid.New(),
		TokenHash:      p.TokenHash,
		SubmittedAt:    time.Now(),
		BodyEncrypted:  p.BodyEncrypted,
		BodyNonce:      p.BodyNonce,
		RetentionUntil: p.RetentionUntil,
		Status:         "received",
	}
	f.whistleblowerSubs = append(f.whistleblowerSubs, ws)
	return &ws, nil
}

func (f *fakeStore) GetWhistleblowerSubmission(_ context.Context, id uuid.UUID) (*storage.WhistleblowerSubmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	for i := range f.whistleblowerSubs {
		if f.whistleblowerSubs[i].ID == id {
			ws := f.whistleblowerSubs[i]
			return &ws, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) ListAllWhistleblowerSubmissions(_ context.Context) ([]storage.WhistleblowerSubmission, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	out := make([]storage.WhistleblowerSubmission, len(f.whistleblowerSubs))
	copy(out, f.whistleblowerSubs)
	return out, nil
}

func (f *fakeStore) SweepWhistleblowerSubmissions(_ context.Context, now time.Time) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	if now.IsZero() {
		now = time.Now()
	}
	kept := f.whistleblowerSubs[:0]
	var deleted int64
	for _, ws := range f.whistleblowerSubs {
		if ws.RetentionUntil.Before(now) {
			deleted++
			continue
		}
		kept = append(kept, ws)
	}
	f.whistleblowerSubs = kept
	return deleted, nil
}

func (f *fakeStore) AttachCaseEvidence(_ context.Context, caseID, evidenceID uuid.UUID) (*storage.CaseEvidenceLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	link := storage.CaseEvidenceLink{CaseID: caseID, EvidenceID: evidenceID, AttachedAt: time.Now()}
	f.caseEvidenceLinks[caseID] = append(f.caseEvidenceLinks[caseID], link)
	return &link, nil
}

func (f *fakeStore) ListCaseEvidence(_ context.Context, caseID uuid.UUID) ([]storage.CaseEvidenceLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	links := f.caseEvidenceLinks[caseID]
	out := make([]storage.CaseEvidenceLink, len(links))
	copy(out, links)
	return out, nil
}

func (f *fakeStore) CreateRiskSignal(_ context.Context, p storage.CreateRiskSignalParams) (*storage.RiskSignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	occurred := p.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	rs := storage.RiskSignal{
		ID:         uuid.New(),
		CaseID:     p.CaseID,
		SignalType: p.SignalType,
		Severity:   p.Severity,
		SourceID:   p.SourceID,
		OccurredAt: occurred,
		Weight:     p.Weight,
	}
	if p.SourceTable != "" {
		v := p.SourceTable
		rs.SourceTable = &v
	}
	f.riskSignals[p.CaseID] = append(f.riskSignals[p.CaseID], rs)
	return &rs, nil
}

func (f *fakeStore) ListRiskSignals(_ context.Context, caseID uuid.UUID) ([]storage.RiskSignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	signals := f.riskSignals[caseID]
	out := make([]storage.RiskSignal, len(signals))
	copy(out, signals)
	return out, nil
}

func (f *fakeStore) DeleteRiskSignalsForCase(_ context.Context, caseID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ensureMisconductMaps()
	delete(f.riskSignals, caseID)
	return nil
}

func (f *fakeStore) CountAuditLogsForActor(_ context.Context, actorID uuid.UUID, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, log := range f.auditLogs {
		if log.ActorID == actorID {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) CountSecurityEventsBySeverity(_ context.Context, _ uuid.UUID, _ time.Time) (map[string]int, error) {
	return map[string]int{}, nil
}

func (f *fakeStore) CountFailedComplianceForTenant(_ context.Context, _ uuid.UUID, _ time.Time) (int, error) {
	return 0, nil
}

// Finacle integration stubs (UC6). Behaviour-bearing tests live in
// finacle_test.go and use a dedicated wrapping store.
func (f *fakeStore) CreateFinacleConnection(_ context.Context, _ storage.CreateFinacleConnectionParams) (*storage.FinacleConnection, error) {
	return nil, nil
}
func (f *fakeStore) GetFinacleConnection(_ context.Context, _ uuid.UUID) (*storage.FinacleConnection, error) {
	return nil, nil
}
func (f *fakeStore) ListFinacleConnections(_ context.Context, _ uuid.UUID) ([]storage.FinacleConnection, error) {
	return nil, nil
}
func (f *fakeStore) UpdateFinacleConnection(_ context.Context, _ uuid.UUID, _ storage.UpdateFinacleConnectionParams) (*storage.FinacleConnection, error) {
	return nil, nil
}
func (f *fakeStore) DeleteFinacleConnection(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) CreateFinacleShiftConfig(_ context.Context, _ storage.CreateFinacleShiftConfigParams) (*storage.FinacleShiftConfig, error) {
	return nil, nil
}
func (f *fakeStore) GetFinacleShiftConfig(_ context.Context, _ uuid.UUID) (*storage.FinacleShiftConfig, error) {
	return nil, nil
}
func (f *fakeStore) ListFinacleShiftConfigs(_ context.Context, _ uuid.UUID) ([]storage.FinacleShiftConfig, error) {
	return nil, nil
}
func (f *fakeStore) UpdateFinacleShiftConfig(_ context.Context, _ uuid.UUID, _ storage.UpdateFinacleShiftConfigParams) (*storage.FinacleShiftConfig, error) {
	return nil, nil
}
func (f *fakeStore) DeleteFinacleShiftConfig(_ context.Context, _ uuid.UUID) error { return nil }
func (f *fakeStore) UpsertFinacleProfile(_ context.Context, _ storage.UpsertFinacleProfileParams) (*storage.FinacleProfile, error) {
	return nil, nil
}
func (f *fakeStore) UpdateFinacleProfile(_ context.Context, _ uuid.UUID, _ storage.UpdateFinacleProfileParams) (*storage.FinacleProfile, error) {
	return nil, nil
}
func (f *fakeStore) GetFinacleProfile(_ context.Context, _ uuid.UUID) (*storage.FinacleProfile, error) {
	return nil, nil
}
func (f *fakeStore) ListFinacleProfiles(_ context.Context, _ uuid.UUID, _, _ int) ([]storage.FinacleProfile, int, error) {
	return nil, 0, nil
}
func (f *fakeStore) ListFinacleProfilesByShift(_ context.Context, _ uuid.UUID) ([]storage.FinacleProfile, error) {
	return nil, nil
}
func (f *fakeStore) MarkFinacleProfileRotated(_ context.Context, _ uuid.UUID, _ string) error {
	return nil
}
func (f *fakeStore) DeleteFinacleProfile(_ context.Context, _ uuid.UUID) error { return nil }

// Pre-existing dashboard metric (test was missing this method).
func (f *fakeStore) CountRemediationsSince(_ context.Context, _ uuid.UUID, _ time.Time, _ time.Time) (int, error) {
	return 0, nil
}

// Listening-services inventory (Phase 1 of /round-up knowledge graph).
func (f *fakeStore) ReplaceNodeServices(_ context.Context, nodeID uuid.UUID, _ uuid.UUID, services []storage.NodeService) error {
	if f.nodeServices == nil {
		f.nodeServices = map[uuid.UUID][]storage.NodeService{}
	}
	f.nodeServices[nodeID] = append([]storage.NodeService(nil), services...)
	return nil
}
func (f *fakeStore) ListNodeServicesForNode(_ context.Context, nodeID uuid.UUID) ([]storage.NodeService, error) {
	if f.nodeServices != nil {
		return append([]storage.NodeService(nil), f.nodeServices[nodeID]...), nil
	}
	return nil, nil
}
func (f *fakeStore) ListNodeServicesForTenant(_ context.Context, tenantID uuid.UUID) ([]storage.NodeService, error) {
	var out []storage.NodeService
	for _, services := range f.nodeServices {
		for _, svc := range services {
			if tenantID == uuid.Nil || svc.TenantID == tenantID {
				out = append(out, svc)
			}
		}
	}
	return out, nil
}

func (f *fakeStore) UpsertContentPackSourceProposals(_ context.Context, p storage.UpsertContentPackSourceProposalsParams) ([]storage.ContentPackSourceProposalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	out := make([]storage.ContentPackSourceProposalRecord, 0, len(p.Proposals))
	for _, proposal := range p.Proposals {
		program := strings.ToLower(strings.TrimSpace(proposal.Program))
		if program == "" {
			continue
		}
		proposalID := strings.ToLower(strings.TrimSpace(proposal.ID))
		if proposalID == "" {
			proposalID = strings.ToLower(strings.TrimSpace(proposal.Kind)) + ":" + program
		}
		status := storage.ContentPackSourceProposalStatusProposed
		requiresApproval := proposal.RequiresApproval || strings.EqualFold(proposal.Risk, "high") || strings.EqualFold(proposal.Risk, "critical")
		autoEligible := proposal.AutoConnectEligible && !requiresApproval
		switch {
		case requiresApproval:
			status = storage.ContentPackSourceProposalStatusApprovalRequired
		case autoEligible:
			status = storage.ContentPackSourceProposalStatusAutoEligible
		}
		record := storage.ContentPackSourceProposalRecord{
			ID:                  uuid.New(),
			TenantID:            p.TenantID,
			NodeID:              p.NodeID,
			ProposalID:          proposalID,
			Kind:                strings.ToLower(strings.TrimSpace(proposal.Kind)),
			Program:             program,
			SourceID:            firstNonEmptyContentPack(proposal.Labels["content_pack_source_id"], proposal.Labels["source_id"], proposal.Labels["parser_profile"], program),
			CollectorType:       strings.ToLower(strings.TrimSpace(proposal.CollectorType)),
			Formatter:           strings.ToLower(strings.TrimSpace(proposal.Formatter)),
			Status:              status,
			Confidence:          proposal.Confidence,
			Risk:                strings.ToLower(strings.TrimSpace(proposal.Risk)),
			AutoConnectEligible: autoEligible,
			RequiresApproval:    requiresApproval,
			Reason:              strings.TrimSpace(proposal.Reason),
			Paths:               append([]string(nil), proposal.Paths...),
			Evidence:            append([]string(nil), proposal.Evidence...),
			Labels:              cloneStringMapContentPack(proposal.Labels),
			FirstSeenAt:         now,
			LastSeenAt:          now,
			CreatedAt:           now,
			UpdatedAt:           now,
		}
		found := false
		for i := range f.sourceProposals {
			if f.sourceProposals[i].TenantID == p.TenantID && f.sourceProposals[i].NodeID == p.NodeID && f.sourceProposals[i].ProposalID == proposalID {
				record.ID = f.sourceProposals[i].ID
				record.FirstSeenAt = f.sourceProposals[i].FirstSeenAt
				f.sourceProposals[i] = record
				found = true
				break
			}
		}
		if !found {
			f.sourceProposals = append(f.sourceProposals, record)
		}
		out = append(out, record)
	}
	return out, nil
}

func (f *fakeStore) ListContentPackSourceProposals(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.ContentPackSourceProposalRecord, int, error) {
	return f.ListContentPackSourceProposalsFiltered(context.Background(), tenantID, storage.ContentPackSourceProposalFilter{}, limit, offset)
}

func (f *fakeStore) ListContentPackSourceProposalsFiltered(_ context.Context, tenantID uuid.UUID, filter storage.ContentPackSourceProposalFilter, limit, offset int) ([]storage.ContentPackSourceProposalRecord, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	filtered := make([]storage.ContentPackSourceProposalRecord, 0, len(f.sourceProposals))
	for _, row := range f.sourceProposals {
		if row.TenantID == tenantID && fakeSourceProposalMatchesFilter(row, filter) {
			filtered = append(filtered, row)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].LastSeenAt.After(filtered[j].LastSeenAt)
	})
	total := len(filtered)
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 || offset >= total {
		if offset < 0 {
			offset = 0
		}
		return []storage.ContentPackSourceProposalRecord{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return append([]storage.ContentPackSourceProposalRecord(nil), filtered[offset:end]...), total, nil
}

func (f *fakeStore) ContentPackSourceProposalSummaryFiltered(_ context.Context, tenantID uuid.UUID, filter storage.ContentPackSourceProposalFilter) (storage.ContentPackSourceProposalSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	summary := storage.ContentPackSourceProposalSummary{ByStatus: map[string]int{}}
	for _, row := range f.sourceProposals {
		if row.TenantID != tenantID || !fakeSourceProposalMatchesFilter(row, filter) {
			continue
		}
		summary.Total++
		summary.ByStatus[row.Status]++
	}
	return summary, nil
}

func fakeSourceProposalMatchesFilter(row storage.ContentPackSourceProposalRecord, filter storage.ContentPackSourceProposalFilter) bool {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	if query != "" {
		values := []string{
			row.ID.String(),
			row.NodeID.String(),
			row.ProposalID,
			row.Kind,
			row.Program,
			row.SourceID,
			row.CollectorType,
			row.Formatter,
			row.Status,
			row.Risk,
			row.Reason,
			row.ApprovedBySubject,
			row.ApprovalNote,
			row.RejectedBySubject,
			row.RejectionReason,
			strings.Join(row.Paths, " "),
			strings.Join(row.Evidence, " "),
		}
		matched := false
		for _, value := range values {
			if strings.Contains(strings.ToLower(strings.TrimSpace(value)), query) {
				matched = true
				break
			}
		}
		if !matched {
			for key, value := range row.Labels {
				if strings.Contains(strings.ToLower(strings.TrimSpace(key)), query) ||
					strings.Contains(strings.ToLower(strings.TrimSpace(value)), query) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}
	if len(filter.Statuses) == 0 {
		return true
	}
	for _, status := range filter.Statuses {
		if strings.EqualFold(strings.TrimSpace(status), strings.TrimSpace(row.Status)) {
			return true
		}
	}
	return false
}

func (f *fakeStore) ListContentPackSourceProposalsByIDs(_ context.Context, tenantID uuid.UUID, proposalIDs []uuid.UUID) ([]storage.ContentPackSourceProposalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := map[uuid.UUID]struct{}{}
	for _, id := range proposalIDs {
		if id != uuid.Nil {
			ids[id] = struct{}{}
		}
	}
	out := make([]storage.ContentPackSourceProposalRecord, 0, len(ids))
	for _, row := range f.sourceProposals {
		if row.TenantID != tenantID {
			continue
		}
		if _, ok := ids[row.ID]; !ok {
			continue
		}
		out = append(out, row)
	}
	return append([]storage.ContentPackSourceProposalRecord(nil), out...), nil
}

func (f *fakeStore) ListApprovedContentPackSourceProposalsForNode(_ context.Context, tenantID, nodeID uuid.UUID, limit int) ([]storage.ContentPackSourceProposalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if limit <= 0 {
		limit = 128
	}
	out := make([]storage.ContentPackSourceProposalRecord, 0, limit)
	for _, row := range f.sourceProposals {
		if row.TenantID != tenantID || row.NodeID != nodeID {
			continue
		}
		if row.Status != storage.ContentPackSourceProposalStatusApproved {
			continue
		}
		if row.Kind != connectordiscovery.KindLocalLog {
			continue
		}
		if row.CollectorType != "" && row.CollectorType != connectordiscovery.CollectorTypeFile {
			continue
		}
		if !storage.ContentPackSourceProposalCollectModeDeploysNodeAgent(row.CollectMode) {
			continue
		}
		out = append(out, row)
		if len(out) >= limit {
			break
		}
	}
	return append([]storage.ContentPackSourceProposalRecord(nil), out...), nil
}

func (f *fakeStore) ApproveContentPackSourceProposal(_ context.Context, p storage.ApproveContentPackSourceProposalParams) (*storage.ContentPackSourceProposalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for i := range f.sourceProposals {
		if f.sourceProposals[i].TenantID != p.TenantID || f.sourceProposals[i].ID != p.ProposalID {
			continue
		}
		switch f.sourceProposals[i].Status {
		case storage.ContentPackSourceProposalStatusProposed,
			storage.ContentPackSourceProposalStatusAutoEligible,
			storage.ContentPackSourceProposalStatusApprovalRequired,
			storage.ContentPackSourceProposalStatusStale:
		default:
			return nil, fmt.Errorf("content pack source proposal is not approvable or not found")
		}
		f.sourceProposals[i].Status = storage.ContentPackSourceProposalStatusApproved
		f.sourceProposals[i].ApprovedBySubject = strings.TrimSpace(p.ApprovedBySubject)
		f.sourceProposals[i].ApprovalNote = strings.TrimSpace(p.ApprovalNote)
		collectMode := strings.ToLower(strings.TrimSpace(p.CollectMode))
		if collectMode == "" {
			collectMode = storage.ContentPackSourceProposalCollectModeCollectRaw
		}
		f.sourceProposals[i].CollectMode = collectMode
		f.sourceProposals[i].ApprovedAt = &now
		f.sourceProposals[i].RejectedBySubject = ""
		f.sourceProposals[i].RejectedAt = nil
		f.sourceProposals[i].RejectionReason = ""
		f.sourceProposals[i].UpdatedAt = now
		out := f.sourceProposals[i]
		return &out, nil
	}
	return nil, fmt.Errorf("content pack source proposal is not approvable or not found")
}

func (f *fakeStore) RejectContentPackSourceProposal(_ context.Context, p storage.RejectContentPackSourceProposalParams) (*storage.ContentPackSourceProposalRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	for i := range f.sourceProposals {
		if f.sourceProposals[i].TenantID != p.TenantID || f.sourceProposals[i].ID != p.ProposalID {
			continue
		}
		switch f.sourceProposals[i].Status {
		case storage.ContentPackSourceProposalStatusProposed,
			storage.ContentPackSourceProposalStatusAutoEligible,
			storage.ContentPackSourceProposalStatusApprovalRequired,
			storage.ContentPackSourceProposalStatusStale:
		default:
			return nil, fmt.Errorf("content pack source proposal is not rejectable or not found")
		}
		f.sourceProposals[i].Status = storage.ContentPackSourceProposalStatusRejected
		if p.PrivacyBlocked {
			f.sourceProposals[i].Status = storage.ContentPackSourceProposalStatusPrivacyBlocked
		}
		f.sourceProposals[i].RejectedBySubject = strings.TrimSpace(p.RejectedBySubject)
		f.sourceProposals[i].RejectionReason = strings.TrimSpace(p.RejectionReason)
		f.sourceProposals[i].RejectedAt = &now
		f.sourceProposals[i].ApprovedBySubject = ""
		f.sourceProposals[i].ApprovedAt = nil
		f.sourceProposals[i].ApprovalNote = ""
		f.sourceProposals[i].CollectMode = ""
		f.sourceProposals[i].UpdatedAt = now
		out := f.sourceProposals[i]
		return &out, nil
	}
	return nil, fmt.Errorf("content pack source proposal is not rejectable or not found")
}

func (f *fakeStore) UpsertContentPackSourceRuntimeState(_ context.Context, p storage.UpsertContentPackSourceRuntimeStateParams) (*storage.ContentPackSourceRuntimeStateRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	record := storage.ContentPackSourceRuntimeStateRecord{
		ID:        uuid.New(),
		TenantID:  p.TenantID,
		State:     p.State,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if record.State.UpdatedAt.IsZero() {
		record.State.UpdatedAt = now
	}
	for i := range f.sourceStates {
		if f.sourceStates[i].TenantID == p.TenantID && f.sourceStates[i].State.SourceInstanceID == p.State.SourceInstanceID {
			record.ID = f.sourceStates[i].ID
			record.CreatedAt = f.sourceStates[i].CreatedAt
			f.sourceStates[i] = record
			out := f.sourceStates[i]
			return &out, nil
		}
	}
	f.sourceStates = append(f.sourceStates, record)
	return &record, nil
}

func (f *fakeStore) ListContentPackSourceRuntimeStates(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.ContentPackSourceRuntimeStateRecord, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	filtered := make([]storage.ContentPackSourceRuntimeStateRecord, 0, len(f.sourceStates))
	for _, row := range f.sourceStates {
		if row.TenantID == tenantID {
			filtered = append(filtered, row)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	total := len(filtered)
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 || offset >= total {
		if offset < 0 {
			offset = 0
		}
		return []storage.ContentPackSourceRuntimeStateRecord{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return append([]storage.ContentPackSourceRuntimeStateRecord(nil), filtered[offset:end]...), total, nil
}

func (f *fakeStore) ListIPBehaviorCountries(_ context.Context, tenantID uuid.UUID, _ time.Time, countryCode string) ([]storage.IPBehaviorCountrySummary, error) {
	var out []storage.IPBehaviorCountrySummary
	// Test rows do not carry tenant_id, so tenant filtering is validated in
	// dedicated IP behavior API tests.
	_ = tenantID
	for _, country := range f.ipBehaviorCountries {
		if countryCode != "" && !strings.EqualFold(country.CountryCode, countryCode) {
			continue
		}
		out = append(out, country)
	}
	return out, nil
}

func (f *fakeStore) GetIPBehaviorIPProfile(_ context.Context, _ uuid.UUID, _ string, _ time.Time) (*storage.IPBehaviorIPProfile, error) {
	return nil, nil
}

func (f *fakeStore) ListIPBehaviorBaselines(_ context.Context, _ uuid.UUID, _ string, _ int, _ int) ([]storage.IPBehaviorBaseline, int, error) {
	return nil, 0, nil
}

func (f *fakeStore) ListIPBehaviorFindings(_ context.Context, filter storage.IPBehaviorFindingFilter, limit, offset int) ([]storage.IPBehaviorFinding, int, error) {
	var out []storage.IPBehaviorFinding
	for _, finding := range f.ipBehaviorFindings {
		if filter.TenantID != uuid.Nil && finding.TenantID != filter.TenantID {
			continue
		}
		if filter.SourceIP != "" && (!finding.SourceIP.Valid || finding.SourceIP.String != filter.SourceIP) {
			continue
		}
		if filter.Resolved != nil {
			resolved := finding.Status == "resolved" || finding.Status == "suppressed"
			if resolved != *filter.Resolved {
				continue
			}
		}
		out = append(out, finding)
	}
	total := len(out)
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeStore) ListWebserverInstances(_ context.Context, tenantID uuid.UUID, nodeID uuid.UUID, limit, offset int) ([]storage.WebserverInstance, int, error) {
	var out []storage.WebserverInstance
	for _, instance := range f.webserverInstances {
		if tenantID != uuid.Nil && instance.TenantID != tenantID {
			continue
		}
		if nodeID != uuid.Nil && instance.NodeID != nodeID {
			continue
		}
		out = append(out, instance)
	}
	total := len(out)
	if offset > len(out) {
		offset = len(out)
	}
	out = out[offset:]
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeStore) CountRuleTriggersBetween(_ context.Context, _ uuid.UUID, _ time.Time, _ time.Time) (map[string]int, error) {
	return nil, nil
}

func (f *fakeStore) GetSecurityEventSeries(_ context.Context, _ uuid.UUID, _ time.Time, _ string) ([]storage.SecurityEventPoint, error) {
	return nil, nil
}

// Ask AI LLM config (Phase 2).
func (f *fakeStore) GetAIConfig(_ context.Context, tenantID uuid.UUID) (*storage.AIConfig, error) {
	if f.aiConfig != nil && (tenantID == uuid.Nil || f.aiConfig.TenantID == tenantID) {
		copy := *f.aiConfig
		return &copy, nil
	}
	return nil, nil
}
func (f *fakeStore) UpsertAIConfig(_ context.Context, cfg storage.AIConfig) error {
	copy := cfg
	f.aiConfig = &copy
	return nil
}

func (f *fakeStore) ListFlowDeltas(_ context.Context, filter EventCaptureFilter) ([]FlowDeltaRow, error) {
	out := make([]FlowDeltaRow, 0, len(f.flowDeltas))
	for _, row := range f.flowDeltas {
		if row.TenantID == filter.TenantID && row.NodeID == filter.NodeID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeStore) ListFileGrowthDeltas(_ context.Context, filter EventCaptureFilter) ([]FileGrowthDeltaRow, error) {
	out := make([]FileGrowthDeltaRow, 0, len(f.fileGrowthDeltas))
	for _, row := range f.fileGrowthDeltas {
		if row.TenantID == filter.TenantID && row.NodeID == filter.NodeID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeStore) ListResourceDeltas(_ context.Context, filter EventCaptureFilter) ([]ResourceDeltaRow, error) {
	out := make([]ResourceDeltaRow, 0, len(f.resourceDeltas))
	for _, row := range f.resourceDeltas {
		if row.TenantID == filter.TenantID && row.NodeID == filter.NodeID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeStore) ListLogTail(_ context.Context, filter EventCaptureFilter) ([]LogTailRow, error) {
	out := make([]LogTailRow, 0, len(f.logTailRows))
	for _, row := range f.logTailRows {
		if row.TenantID == filter.TenantID && row.NodeID == filter.NodeID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeStore) ListRootCauseFindings(_ context.Context, filter EventCaptureFilter) ([]RootCauseFinding, error) {
	out := make([]RootCauseFinding, 0, len(f.rootCauseFindings))
	for _, row := range f.rootCauseFindings {
		if row.TenantID == filter.TenantID && row.NodeID == filter.NodeID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (f *fakeStore) CreateAIInvestigation(_ context.Context, params storage.CreateAIInvestigationParams) (*storage.AIInvestigation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	now := time.Now().UTC()
	dedup := strings.TrimSpace(params.TriggerDedupKey)
	if dedup == "" {
		dedup = id.String()
	}
	for i := range f.aiInvestigations {
		if f.aiInvestigations[i].TenantID == params.TenantID && f.aiInvestigations[i].TriggerDedupKey == dedup {
			f.aiInvestigations[i].TriggerEventType = params.TriggerEventType
			f.aiInvestigations[i].Severity = params.Severity
			f.aiInvestigations[i].Summary = params.Summary
			f.aiInvestigations[i].Evidence = append([]byte(nil), params.Evidence...)
			f.aiInvestigations[i].UpdatedAt = now
			copy := f.aiInvestigations[i]
			return &copy, nil
		}
	}
	row := storage.AIInvestigation{
		ID:               id,
		TenantID:         params.TenantID,
		NodeID:           params.NodeID,
		TriggerType:      firstNonEmpty(params.TriggerType, "manual"),
		TriggerEventType: firstNonEmpty(params.TriggerEventType, params.TriggerType),
		TriggerDedupKey:  dedup,
		Severity:         firstNonEmpty(params.Severity, "info"),
		Summary:          firstNonEmpty(params.Summary, params.TriggerEventType),
		Evidence:         append([]byte(nil), params.Evidence...),
		Status:           params.Status,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if row.Status == "" {
		row.Status = storage.AIInvestigationStatusOpen
	}
	f.aiInvestigations = append(f.aiInvestigations, row)
	copy := row
	return &copy, nil
}

func (f *fakeStore) ListAIInvestigations(_ context.Context, filter storage.ListAIInvestigationsFilter, limit, offset int) ([]storage.AIInvestigation, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.AIInvestigation, 0, len(f.aiInvestigations))
	for _, row := range f.aiInvestigations {
		if filter.TenantID != uuid.Nil && row.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && row.NodeID != filter.NodeID {
			continue
		}
		if filter.Status != "" && row.Status != filter.Status {
			continue
		}
		if filter.TriggerType != "" && row.TriggerType != filter.TriggerType {
			continue
		}
		if filter.TriggerEventType != "" && row.TriggerEventType != filter.TriggerEventType {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := len(out)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return out[offset:end], total, nil
}

func (f *fakeStore) GetAIInvestigation(_ context.Context, id uuid.UUID) (*storage.AIInvestigation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.aiInvestigations {
		if row.ID == id {
			copy := row
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) GetPrivateAccessExposureFinding(_ context.Context, tenantID, id uuid.UUID) (*storage.PrivateAccessExposureFindingRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, row := range f.privateAccessFindings {
		if row.TenantID == tenantID && row.ID == id {
			copy := row
			if row.NodeID != nil {
				nodeID := *row.NodeID
				copy.NodeID = &nodeID
			}
			if row.ServiceID != nil {
				serviceID := *row.ServiceID
				copy.ServiceID = &serviceID
			}
			if row.ResolvedAt != nil {
				resolvedAt := *row.ResolvedAt
				copy.ResolvedAt = &resolvedAt
			}
			copy.Evidence = append([]string(nil), row.Evidence...)
			return &copy, nil
		}
	}
	return nil, nil
}

func (f *fakeStore) CreateAIOperatorProposal(_ context.Context, params storage.CreateAIOperatorProposalParams) (*storage.AIOperatorProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	now := time.Now().UTC()
	var createdBy *uuid.UUID
	if params.CreatedBy != uuid.Nil {
		v := params.CreatedBy
		createdBy = &v
	}
	row := storage.AIOperatorProposal{
		ID:           id,
		TenantID:     params.TenantID,
		NodeID:       params.NodeID,
		Action:       params.Action,
		Reason:       params.Reason,
		Status:       params.Status,
		DryRun:       true,
		ApprovalKind: firstNonEmpty(params.ApprovalKind, "manual"),
		ApprovalPath: params.ApprovalPath,
		SourceTool:   firstNonEmpty(params.SourceTool, "operator_propose_action"),
		Metadata:     append([]byte(nil), params.Metadata...),
		CreatedBy:    createdBy,
		CreatedAt:    now,
	}
	if row.Status == "" {
		row.Status = storage.AIOperatorProposalStatusProposed
	}
	f.aiOperatorProposals = append(f.aiOperatorProposals, row)
	copy := row
	return &copy, nil
}

func (f *fakeStore) ListAIOperatorProposals(_ context.Context, filter storage.ListAIOperatorProposalsFilter, limit, offset int) ([]storage.AIOperatorProposal, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.AIOperatorProposal, 0, len(f.aiOperatorProposals))
	for _, row := range f.aiOperatorProposals {
		if filter.TenantID != uuid.Nil && row.TenantID != filter.TenantID {
			continue
		}
		if filter.NodeID != uuid.Nil && row.NodeID != filter.NodeID {
			continue
		}
		if filter.Status != "" && row.Status != filter.Status {
			continue
		}
		if filter.Action != "" && row.Action != filter.Action {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	total := len(out)
	if offset > total {
		offset = total
	}
	end := total
	if limit > 0 && offset+limit < total {
		end = offset + limit
	}
	return out[offset:end], total, nil
}

func (f *fakeStore) CreateAILogFixerAction(_ context.Context, params storage.CreateAILogFixerActionParams) (*storage.AILogFixerAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.aiLogFixerActions == nil {
		f.aiLogFixerActions = map[uuid.UUID]storage.AILogFixerAction{}
	}
	action := storage.AILogFixerAction{
		ID:        uuid.New(),
		TenantID:  params.TenantID,
		NodeID:    params.NodeID,
		JobID:     params.JobID,
		Action:    strings.TrimSpace(params.Action),
		Status:    "pending",
		Policy:    copyFakeMap(params.Policy),
		Result:    map[string]any{},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if params.RunID != nil && *params.RunID != uuid.Nil {
		action.RunID = uuid.NullUUID{UUID: *params.RunID, Valid: true}
	}
	f.aiLogFixerActions[params.JobID] = action
	copy := action
	copy.Policy = copyFakeMap(action.Policy)
	copy.Result = copyFakeMap(action.Result)
	return &copy, nil
}

func (f *fakeStore) GetAILogFixerActionByJobID(_ context.Context, jobID uuid.UUID) (*storage.AILogFixerAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	action, ok := f.aiLogFixerActions[jobID]
	if !ok {
		return nil, nil
	}
	copy := action
	copy.Policy = copyFakeMap(action.Policy)
	copy.Result = copyFakeMap(action.Result)
	return &copy, nil
}

func (f *fakeStore) ListPendingAILogFixerActions(_ context.Context, nodeID uuid.UUID) ([]storage.AILogFixerAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]storage.AILogFixerAction, 0, len(f.aiLogFixerActions))
	for _, action := range f.aiLogFixerActions {
		if action.NodeID == nodeID && (action.Status == "pending" || action.Status == "running") {
			copy := action
			copy.Policy = copyFakeMap(action.Policy)
			copy.Result = copyFakeMap(action.Result)
			out = append(out, copy)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (f *fakeStore) MarkAILogFixerActionStatus(_ context.Context, jobID uuid.UUID, status string, result map[string]any, errMsg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	action, ok := f.aiLogFixerActions[jobID]
	if !ok {
		return nil
	}
	action.Status = strings.TrimSpace(status)
	action.Result = copyFakeMap(result)
	if strings.TrimSpace(errMsg) != "" {
		action.ErrorMessage = sql.NullString{String: strings.TrimSpace(errMsg), Valid: true}
	} else {
		action.ErrorMessage = sql.NullString{}
	}
	action.UpdatedAt = time.Now().UTC()
	f.aiLogFixerActions[jobID] = action
	return nil
}

func (f *fakeStore) MarkAILogFixerRunStatus(context.Context, uuid.UUID, string, map[string]any, map[string]any) error {
	return nil
}

func (f *fakeStore) CreateSavedSearch(_ context.Context, in storage.SavedSearch) (*storage.SavedSearch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	if len(in.Filters) == 0 {
		in.Filters = json.RawMessage(`{}`)
	}
	now := time.Now().UTC()
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = in.CreatedAt
	}
	f.savedSearches = append(f.savedSearches, in)
	copy := in
	return &copy, nil
}
