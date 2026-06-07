package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

type spyQueue struct {
	tasks []worker.Task
}

func (q *spyQueue) Enqueue(t worker.Task) error {
	q.tasks = append(q.tasks, t)
	return nil
}

func (q *spyQueue) EnqueueAt(t worker.Task, _ time.Time) error {
	return q.Enqueue(t)
}

func TestComplianceSchedulerCreateScanJobs(t *testing.T) {
	tenantID := uuid.New()
	node1 := uuid.New()
	node2 := uuid.New()

	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "test-tenant"}},
		nodes: []storage.Node{
			{ID: node1, TenantID: tenantID, Hostname: "node-1"},
			{ID: node2, TenantID: tenantID, Hostname: "node-2"},
		},
		jobs:   make(map[uuid.UUID]*storage.Job),
		events: make(map[uuid.UUID][]storage.JobEvent),
	}

	queue := &spyQueue{}
	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "test-token"),
	}

	srv := &Server{
		logger:      logger,
		cfg:         cfg,
		store:       store,
		worker:      queue,
		jobHandlers: map[string]jobHandler{JobTypeComplianceScan: func(_ context.Context, _ *storage.Job) error { return nil }},
	}

	cs := NewComplianceScheduler(srv)

	t.Run("scans all nodes across tenants", func(t *testing.T) {
		queue.tasks = nil
		jobIDs, err := cs.createScanJobs(context.Background(), uuid.Nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobIDs) != 2 {
			t.Fatalf("expected 2 jobs, got %d", len(jobIDs))
		}
		if len(queue.tasks) != 2 {
			t.Fatalf("expected 2 enqueued tasks, got %d", len(queue.tasks))
		}
		for _, task := range queue.tasks {
			if !task.DurableJob.Valid() || task.DurableJob.Type != JobTypeComplianceScan {
				t.Fatalf("scheduled scan task missing durable job ref: %#v", task.DurableJob)
			}
			if task.Job == nil {
				t.Fatal("scheduled scan task should retain an in-process job for memory workers")
			}
		}
	})

	t.Run("scans specific tenant only", func(t *testing.T) {
		queue.tasks = nil
		jobIDs, err := cs.createScanJobs(context.Background(), tenantID, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobIDs) != 2 {
			t.Fatalf("expected 2 jobs for tenant, got %d", len(jobIDs))
		}
	})

	t.Run("scans specific nodes only", func(t *testing.T) {
		queue.tasks = nil
		jobIDs, err := cs.createScanJobs(context.Background(), uuid.Nil, []uuid.UUID{node1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobIDs) != 1 {
			t.Fatalf("expected 1 job, got %d", len(jobIDs))
		}
	})

	t.Run("copies policy facts into generated scan payloads", func(t *testing.T) {
		queue.tasks = nil
		jobIDs, err := cs.createScanJobsWithPolicies(context.Background(), tenantID, []uuid.UUID{node1}, map[string]string{
			"rule_set": "cis-level-1",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(jobIDs) != 1 {
			t.Fatalf("expected 1 job, got %d", len(jobIDs))
		}
		job := store.jobs[jobIDs[0]]
		if job == nil {
			t.Fatalf("generated job not stored")
		}
		var payload compliancePayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		if payload.Policies["rule_set"] != "cis-level-1" {
			t.Fatalf("expected policy facts in payload, got %#v", payload.Policies)
		}
	})

	t.Run("no store returns error", func(t *testing.T) {
		emptySrv := &Server{logger: logger, cfg: cfg, store: nil}
		emptyCS := NewComplianceScheduler(emptySrv)
		_, err := emptyCS.createScanJobs(context.Background(), uuid.Nil, nil)
		if err == nil {
			t.Fatal("expected error for nil store")
		}
	})
}

func TestComplianceBatchScanEndpoint(t *testing.T) {
	tenantID := uuid.New()
	node1 := uuid.New()

	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "test-tenant"}},
		nodes: []storage.Node{
			{ID: node1, TenantID: tenantID, Hostname: "node-1"},
		},
		jobs:   make(map[uuid.UUID]*storage.Job),
		events: make(map[uuid.UUID][]storage.JobEvent),
	}

	logger := zap.NewNop()
	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "test-token"),
	}

	srv := New(logger, cfg, store, &stubQueue{})
	srv.jobHandlers[JobTypeComplianceScan] = func(_ context.Context, _ *storage.Job) error { return nil }
	handler := srv.Handler()

	t.Run("returns 202 with job IDs", func(t *testing.T) {
		tid := tenantID.String()
		body, _ := json.Marshal(batchScanRequest{
			TenantID: &tid,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/scan", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp batchScanResponse
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Count != 1 {
			t.Fatalf("expected count 1, got %d", resp.Count)
		}
	})

	t.Run("rejects non-POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/scan", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405, got %d", rec.Code)
		}
	})
}
