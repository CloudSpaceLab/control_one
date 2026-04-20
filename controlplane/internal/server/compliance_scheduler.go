package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
)

// ComplianceScheduler runs periodic compliance scans across all nodes.
type ComplianceScheduler struct {
	cron   *cron.Cron
	server *Server
	logger *zap.Logger
}

// NewComplianceScheduler creates a scheduler bound to the given server.
func NewComplianceScheduler(s *Server) *ComplianceScheduler {
	return &ComplianceScheduler{
		cron:   cron.New(),
		server: s,
		logger: s.logger.Named("compliance-scheduler"),
	}
}

// Start registers the cron schedule and begins ticking.
func (cs *ComplianceScheduler) Start(schedule string) error {
	_, err := cs.cron.AddFunc(schedule, func() {
		cs.runScheduledScan()
	})
	if err != nil {
		return fmt.Errorf("register compliance schedule %q: %w", schedule, err)
	}
	cs.cron.Start()
	cs.logger.Info("compliance scheduler started", zap.String("schedule", schedule))
	return nil
}

// Stop halts the cron scheduler.
func (cs *ComplianceScheduler) Stop() {
	ctx := cs.cron.Stop()
	<-ctx.Done()
	cs.logger.Info("compliance scheduler stopped")
}

func (cs *ComplianceScheduler) runScheduledScan() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	jobs, err := cs.createScanJobs(ctx, uuid.Nil, nil)
	if err != nil {
		cs.logger.Error("scheduled compliance scan failed", zap.Error(err))
		return
	}
	cs.logger.Info("scheduled compliance scan dispatched", zap.Int("jobs", len(jobs)))
}

// createScanJobs creates compliance.scan jobs for the given scope.
// If tenantID is nil (uuid.Nil), scans all tenants. If nodeIDs is non-nil, scans only those nodes.
func (cs *ComplianceScheduler) createScanJobs(ctx context.Context, tenantID uuid.UUID, nodeIDs []uuid.UUID) ([]uuid.UUID, error) {
	if cs.server.store == nil {
		return nil, fmt.Errorf("store unavailable")
	}

	type nodeRef struct {
		tenantID uuid.UUID
		nodeID   uuid.UUID
	}

	var targets []nodeRef

	if len(nodeIDs) > 0 {
		for _, nid := range nodeIDs {
			node, err := cs.server.store.GetNode(ctx, nid)
			if err != nil {
				cs.logger.Warn("lookup node for scan", zap.Error(err), zap.String("node_id", nid.String()))
				continue
			}
			if node != nil {
				targets = append(targets, nodeRef{tenantID: node.TenantID, nodeID: node.ID})
			}
		}
	} else {
		tenants, _, err := cs.server.store.ListTenants(ctx, "", 0, 0)
		if err != nil {
			return nil, fmt.Errorf("list tenants: %w", err)
		}
		for _, t := range tenants {
			if tenantID != uuid.Nil && t.ID != tenantID {
				continue
			}
			nodes, _, err := cs.server.store.ListNodes(ctx, t.ID, "", 0, 0)
			if err != nil {
				cs.logger.Warn("list nodes for tenant", zap.Error(err), zap.String("tenant_id", t.ID.String()))
				continue
			}
			for _, n := range nodes {
				targets = append(targets, nodeRef{tenantID: t.ID, nodeID: n.ID})
			}
		}
	}

	var jobIDs []uuid.UUID
	for _, target := range targets {
		scanID := uuid.NewString()
		payload := compliancePayload{
			ScanID:   scanID,
			TenantID: target.tenantID.String(),
			NodeID:   target.nodeID.String(),
		}
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			cs.logger.Warn("marshal scan payload", zap.Error(err))
			continue
		}

		job := &storage.Job{
			Type:     JobTypeComplianceScan,
			TenantID: target.tenantID,
			Payload:  payloadBytes,
			Status:   storage.JobStatusQueued,
		}
		event := &storage.JobEvent{
			Status:  storage.JobStatusQueued,
			Message: "scheduled compliance scan",
		}

		created, err := cs.server.store.CreateJob(ctx, job, event)
		if err != nil {
			cs.logger.Warn("create scan job", zap.Error(err), zap.String("node_id", target.nodeID.String()))
			continue
		}
		jobIDs = append(jobIDs, created.ID)

		if cs.server.worker != nil {
			handler, ok := cs.server.jobHandlers[JobTypeComplianceScan]
			if ok {
				jobCopy := *created
				task := worker.Task{
					Name: fmt.Sprintf("%s:%s", JobTypeComplianceScan, created.ID.String()),
					Job: func(taskCtx context.Context) error {
						return handler(taskCtx, &jobCopy)
					},
				}
				if err := cs.server.worker.Enqueue(task); err != nil {
					cs.logger.Warn("enqueue scan job", zap.Error(err), zap.String("job_id", created.ID.String()))
				}
			}
		}
	}

	return jobIDs, nil
}
