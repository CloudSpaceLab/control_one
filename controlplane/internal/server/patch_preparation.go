package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type offlinePreparationStore interface {
	SetOfflineRepository(context.Context, uuid.UUID, uuid.UUID, patchsupport.OfflineRepository) error
	QueueOfflinePreparation(context.Context, uuid.UUID, uuid.UUID, patchsupport.OfflinePreparation) (*storage.Job, error)
	PendingOfflinePreparations(context.Context, uuid.UUID, uuid.UUID) ([]storage.Job, error)
	CompleteOfflinePreparation(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string) error
}

func approvedOfflineRepository(node *storage.Node) (patchsupport.OfflineRepository, error) {
	var repo patchsupport.OfflineRepository
	if node == nil {
		return repo, fmt.Errorf("target unavailable")
	}
	body, err := json.Marshal(node.Labels["patch.offline_repository"])
	if err != nil || json.Unmarshal(body, &repo) != nil || repo.Validate() != nil {
		return repo, fmt.Errorf("no approved offline repository registered")
	}
	return repo, nil
}

func (s *Server) handleOfflineRepository(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", 503)
		return
	}
	var body struct {
		NodeID     string                         `json:"node_id"`
		Repository patchsupport.OfflineRepository `json:"repository"`
	}
	if r.Method == http.MethodGet {
		body.NodeID = r.URL.Query().Get("node_id")
	} else if r.Method == http.MethodPut {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384))
		dec.DisallowUnknownFields()
		if dec.Decode(&body) != nil {
			http.Error(w, "invalid repository registration", 400)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := uuid.Parse(body.NodeID)
	if err != nil {
		http.Error(w, "node_id must be UUID", 400)
		return
	}
	node, err := s.store.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, "target unavailable", 404)
		return
	}
	if principal.Type == "agent" {
		if r.Method != http.MethodGet || agentPrincipalNodeID(principal) != id {
			http.Error(w, "forbidden", 403)
			return
		}
	} else {
		roles := []string{roleViewer, roleOperator, roleAdmin}
		if r.Method != http.MethodGet {
			roles = []string{roleAdmin}
		}
		if !s.requireTenantAccess(w, r, principal, node.TenantID, roles...) {
			return
		}
	}
	repo, _ := approvedOfflineRepository(node)
	if r.Method == http.MethodPut {
		store, ok := s.store.(offlinePreparationStore)
		if !ok {
			http.Error(w, "offline preparation storage unavailable", 503)
			return
		}
		oldID := repo.ID
		repo = body.Repository
		if oldID == "" {
			oldID = uuid.NewString()
		}
		repo.ID = oldID
		if err := repo.Validate(); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := store.SetOfflineRepository(r.Context(), node.TenantID, id, repo); err != nil {
			http.Error(w, "repository registration failed", 503)
			return
		}
		s.recordAudit(r.Context(), principal, node.TenantID, "patch.offline_repository.registered", "node", id.String(), map[string]any{"repository": repo})
	}
	if repo.ID == "" {
		writeJSON(w, 200, map[string]any{"repository": nil})
		return
	}
	writeJSON(w, 200, map[string]any{"repository": repo, "repository_path": repo.RepositoryPath()})
}

func (s *Server) handleOfflinePreparation(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin)
	if !ok {
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", 503)
		return
	}
	var req struct {
		NodeID       string                      `json:"node_id"`
		RepositoryID string                      `json:"repository_id"`
		Packages     []patchsupport.ExactPackage `json:"packages"`
	}
	if r.Method == http.MethodGet {
		req.NodeID = r.URL.Query().Get("node_id")
	} else if r.Method == http.MethodPost {
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
		dec.DisallowUnknownFields()
		if dec.Decode(&req) != nil {
			http.Error(w, "invalid preparation request", 400)
			return
		}
	} else {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	id, err := uuid.Parse(req.NodeID)
	if err != nil {
		http.Error(w, "node_id must be UUID", 400)
		return
	}
	node, err := s.store.GetNode(r.Context(), id)
	if err != nil || node == nil {
		http.Error(w, "target unavailable", 404)
		return
	}
	roles := []string{roleViewer, roleOperator, roleAdmin}
	if r.Method == http.MethodPost {
		roles = []string{roleOperator, roleAdmin}
	}
	if !s.requireTenantAccess(w, r, principal, node.TenantID, roles...) {
		return
	}
	if r.Method == http.MethodGet {
		rawID := r.URL.Query().Get("job_id")
		if rawID == "" {
			rawID, _ = node.Labels["patch.offline_preparation"].(string)
		}
		if rawID == "" {
			writeJSON(w, 200, map[string]any{"status": "idle"})
			return
		}
		jid, err := uuid.Parse(rawID)
		if err != nil {
			http.Error(w, "job_id required", 400)
			return
		}
		job, err := s.store.GetJob(r.Context(), jid)
		if err != nil || job == nil || job.Type != patchsupport.PrepareOfflineJob || job.TenantID != node.TenantID || jobPayloadNodeID(job.Payload) != id {
			http.Error(w, "preparation not found", 404)
			return
		}
		if job.Status == storage.JobStatusRunning || job.Status == storage.JobStatusQueued {
			var p patchsupport.OfflinePreparation
			if json.Unmarshal(job.Payload, &p) != nil || !p.ExpiresAt.After(time.Now().UTC()) {
				if store, ok := s.store.(offlinePreparationStore); ok {
					if err := store.CompleteOfflinePreparation(r.Context(), node.TenantID, id, jid, false, "Preparation timed out; inspect the machine before requesting a fresh plan"); err == nil {
						job, _ = s.store.GetJob(r.Context(), jid)
					}
				}
			}
		}
		if job == nil {
			http.Error(w, "preparation status unavailable", 503)
			return
		}
		events, _ := s.store.ListJobEvents(r.Context(), jid)
		message := ""
		if len(events) > 0 {
			message = events[len(events)-1].Message
		}
		writeJSON(w, 200, map[string]any{"job_id": jid, "status": job.Status, "message": message})
		return
	}
	repo, err := approvedOfflineRepository(node)
	if err != nil || !repo.Enabled || repo.ID != req.RepositoryID {
		http.Error(w, "active approved repository required", 409)
		return
	}
	runtime, err := nodePatchRuntime(node, time.Now().UTC())
	if err != nil || runtime.OfflinePrepareVersion != 1 || runtime.Validate("airgapped") != nil {
		http.Error(w, "current compatible preparation agent required", 409)
		return
	}
	sort.Slice(req.Packages, func(i, j int) bool { return req.Packages[i].Name < req.Packages[j].Name })
	p := patchsupport.OfflinePreparation{NodeID: id.String(), Repository: repo, Packages: req.Packages, ExpiresAt: time.Now().UTC().Add(10 * time.Minute), RequestedBy: principal.Subject}
	if err = p.Validate(time.Now().UTC()); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	store, ok := s.store.(offlinePreparationStore)
	if !ok {
		http.Error(w, "offline preparation storage unavailable", 503)
		return
	}
	job, err := store.QueueOfflinePreparation(r.Context(), node.TenantID, id, p)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.recordAudit(r.Context(), principal, node.TenantID, "patch.offline_plan.preparation_requested", "job", job.ID.String(), map[string]any{"node_id": id, "repository_id": repo.ID, "packages": p.Packages})
	writeJSON(w, http.StatusAccepted, map[string]any{"job_id": job.ID, "status": job.Status, "message": "Preparation queued; installation is not authorized"})
}

func (s *Server) offlinePreparationActions(ctx context.Context, node *storage.Node) []string {
	store, ok := s.store.(offlinePreparationStore)
	if !ok {
		return nil
	}
	jobs, err := store.PendingOfflinePreparations(ctx, node.TenantID, node.ID)
	if err != nil {
		return nil
	}
	out := []string{}
	for _, job := range jobs {
		var p patchsupport.OfflinePreparation
		repo, rerr := approvedOfflineRepository(node)
		if node.State != storage.NodeStateActive || json.Unmarshal(job.Payload, &p) != nil || p.Validate(time.Now().UTC()) != nil || rerr != nil || !reflect.DeepEqual(repo, p.Repository) {
			_ = store.CompleteOfflinePreparation(ctx, node.TenantID, node.ID, job.ID, false, "Preparation expired or approved repository changed")
			continue
		}
		out = append(out, patchsupport.PrepareOfflineJob+":"+job.ID.String())
	}
	return out
}

func (s *Server) completeOfflinePreparation(ctx context.Context, nodeID uuid.UUID, c heartbeatCompletedAction) {
	if c.Status != "succeeded" && c.Status != "failed" {
		return
	}
	id, err := uuid.Parse(c.JobID)
	if err != nil {
		return
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil || job == nil || job.Type != patchsupport.PrepareOfflineJob || jobPayloadNodeID(job.Payload) != nodeID {
		return
	}
	node, nerr := s.store.GetNode(ctx, nodeID)
	if nerr != nil || node == nil || node.TenantID != job.TenantID {
		return
	}
	store, ok := s.store.(offlinePreparationStore)
	if !ok {
		return
	}
	message := "Offline plan prepared; no packages installed"
	if c.Status == "failed" {
		message = c.Error
	}
	if receipts, ok := store.(interface {
		RecordOfflinePreparationReceipt(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, bool, string, ...string) error
	}); ok {
		digest, _ := c.Metadata["manifest_sha256"].(string)
		_ = receipts.RecordOfflinePreparationReceipt(ctx, job.TenantID, nodeID, id, c.Status == "succeeded", message, digest)
	}
}
