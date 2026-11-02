package server

import (
	"context"
	"net/http"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchgovernance"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

type patchReviewOperationsStore interface {
	GetPatchReviewOperations(context.Context, uuid.UUID) (storage.PatchReviewOperations, error)
	SavePatchReviewOperations(context.Context, uuid.UUID, int64, patchgovernance.Operations, string) (storage.PatchReviewOperations, error)
	ListPatchReviewRecipients(context.Context, uuid.UUID) ([]storage.PatchReviewRecipient, error)
	PreviewPatchReviewRetention(context.Context, uuid.UUID) (storage.PatchRetentionPreview, error)
	PurgePatchReviewRetention(context.Context, uuid.UUID, int64, time.Time, string, uuid.UUID, string, string) (int, error)
	DispatchPatchReviewEscalations(context.Context, uuid.UUID) (int, error)
	ListEnabledPatchReviewTenants(context.Context, uuid.UUID) ([]uuid.UUID, error)
	GetPatchReviewOperationState(context.Context, uuid.UUID) (storage.PatchReviewOperationState, error)
	RecordPatchReviewOperationError(context.Context, uuid.UUID) error
}

func (s *Server) handlePatchReviewOperations(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if principal.Type == "agent" {
		http.Error(w, "operator access required", http.StatusForbidden)
		return
	}
	tenant, err := uuid.Parse(r.URL.Query().Get("tenant_id"))
	if err != nil {
		http.Error(w, "tenant_id required", 400)
		return
	}
	roles := []string{roleViewer, roleOperator, roleAdmin}
	if r.Method != "GET" {
		roles = []string{roleAdmin}
	}
	if !s.requireTenantAccess(w, r, principal, tenant, roles...) {
		return
	}
	store, ok := s.store.(patchReviewOperationsStore)
	if !ok {
		http.Error(w, "review operations unavailable", 503)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == "GET" {
		ops, e := store.GetPatchReviewOperations(r.Context(), tenant)
		if e != nil {
			http.Error(w, "cannot read operations", 503)
			return
		}
		recipients, e := store.ListPatchReviewRecipients(r.Context(), tenant)
		if e != nil {
			http.Error(w, "cannot read authorized recipients", 503)
			return
		}
		state, e := store.GetPatchReviewOperationState(r.Context(), tenant)
		if e != nil {
			http.Error(w, "cannot read delivery status", 503)
			return
		}
		effective := "recommend_only"
		if ops.Policy.Automation.Enabled {
			effective = ops.Policy.Automation.Mode
		}
		writeJSON(w, 200, map[string]any{"operations": ops, "recipients": recipients, "delivery": state, "can_manage": s.checkTenantAccess(r.Context(), principal, tenant, roleAdmin) == nil, "current_user_id": principalStorageUserID(s, r.Context(), principal), "effective_installation_mode": effective, "installation_activation_available": true, "notification_channel": "in_app", "external_delivery_available": false})
		return
	}
	if r.Method == "PUT" {
		var req struct {
			ExpectedRevision int64                      `json:"expected_revision"`
			Policy           patchgovernance.Operations `json:"policy"`
		}
		if !decodeGovernance(w, r, &req) {
			return
		}
		if err = req.Policy.Validate(time.Now().UTC()); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		actor := principalStorageUserID(s, r.Context(), principal)
		if actor == uuid.Nil {
			http.Error(w, "current operator identity unavailable", 503)
			return
		}
		saved, e := store.SavePatchReviewOperations(r.Context(), tenant, req.ExpectedRevision, req.Policy, actor.String())
		if e != nil {
			governanceWriteError(w, e)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "patch.review.operations_updated", "tenant", tenant.String(), map[string]any{"revision": saved.Revision, "automation_mode": saved.Policy.Automation.Mode, "enabled": saved.Policy.Automation.Enabled})
		writeJSON(w, 200, saved)
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Allow", "GET, PUT, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	switch r.URL.Query().Get("action") {
	case "retention_preview":
		p, e := store.PreviewPatchReviewRetention(r.Context(), tenant)
		if e != nil {
			http.Error(w, "save valid operations before retention preview", 409)
			return
		}
		writeJSON(w, 200, p)
	case "retention_purge":
		var req struct {
			Revision           int64     `json:"revision"`
			Cutoff             time.Time `json:"cutoff"`
			SHA256             string    `json:"sha256"`
			ArchiveID          uuid.UUID `json:"archive_id"`
			ArchiveDestination string    `json:"archive_destination"`
			Verified           bool      `json:"verified_archive"`
		}
		if !decodeGovernance(w, r, &req) {
			return
		}
		if !req.Verified || req.ArchiveID == uuid.Nil {
			http.Error(w, "verify and retain the complete archive before purging", 400)
			return
		}
		count, e := store.PurgePatchReviewRetention(r.Context(), tenant, req.Revision, req.Cutoff, req.SHA256, req.ArchiveID, principal.Subject, req.ArchiveDestination)
		if e != nil {
			http.Error(w, "retention blocked: legal hold, policy, archive coverage or preview changed", 409)
			return
		}
		s.recordAudit(r.Context(), principal, tenant, "patch.review.retention_purged", "tenant", tenant.String(), map[string]any{"deleted": count, "archive_id": req.ArchiveID})
		writeJSON(w, 200, map[string]any{"deleted": count})
	case "escalation_check":
		count, e := store.DispatchPatchReviewEscalations(r.Context(), tenant)
		if e != nil {
			_ = store.RecordPatchReviewOperationError(r.Context(), tenant)
			http.Error(w, "escalation check failed; no successful delivery is assumed", 503)
			return
		}
		writeJSON(w, 200, map[string]any{"delivered": count, "channel": "in_app"})
	default:
		http.Error(w, "unknown operations action", 400)
	}
}

func (s *Server) startPatchReviewOperations() {
	store, ok := s.store.(patchReviewOperationsStore)
	if !ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.patchReviewStop = func() { cancel(); <-done }
	go func() {
		defer close(done)
		after := uuid.Nil
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				run, cancelRun := context.WithTimeout(ctx, 50*time.Second)
				s.runPatchAutomationCycle(run)
				for run.Err() == nil {
					tenants, err := store.ListEnabledPatchReviewTenants(run, after)
					if err != nil {
						s.logger.Warn("patch review recipient scan unavailable", zap.Error(err))
						break
					}
					if len(tenants) == 0 {
						after = uuid.Nil
						break
					}
					for _, tenant := range tenants {
						if run.Err() != nil {
							break
						}
						if _, err = store.DispatchPatchReviewEscalations(run, tenant); err != nil {
							_ = store.RecordPatchReviewOperationError(run, tenant)
							s.logger.Warn("patch review escalation failed", zap.String("tenant_id", tenant.String()))
						}
						after = tenant
					}
				}
				cancelRun()
			}
		}
	}()
}
