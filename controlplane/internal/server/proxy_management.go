package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type proxyManagementStore interface {
	GetProxyManagement(context.Context, uuid.UUID, uuid.UUID) (*storage.ProxyManagement, error)
	BindProxyManagement(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, []string, []string) (*storage.ProxyManagement, error)
	QueueProxyLifecycle(context.Context, uuid.UUID, uuid.UUID, string, patchsupport.ProxyLifecycle) (*storage.Job, error)
}

func (s *Server) handleProxyManagement(w http.ResponseWriter, r *http.Request, principal *auth.Principal, proxy *storage.SquidProxy) {
	store, ok := s.store.(proxyManagementStore)
	if !ok {
		http.Error(w, "proxy management storage unavailable", 503)
		return
	}
	if r.Method == http.MethodGet {
		p, err := store.GetProxyManagement(r.Context(), proxy.TenantID, proxy.ID)
		if err != nil {
			http.Error(w, "management lookup unavailable", 503)
			return
		}
		var job *storage.Job
		eligible := false
		reason := "Bind an enrolled host to review management eligibility"
		if p != nil {
			node, e := s.store.GetNode(r.Context(), p.NodeID)
			if e == nil && node != nil && node.TenantID == proxy.TenantID && node.State == storage.NodeStateActive {
				runtime, e := nodePatchRuntime(node, time.Now().UTC())
				eligible = e == nil && runtime.OS == "linux" && runtime.Manager == "apt" && runtime.ProxyLifecycleVersion == 2
			}
			reason = "A current supported Ubuntu 24.04/systemd agent is required; no action can be queued on this host"
			if eligible {
				reason = "Host supports managed actions; each change still requires reviewed scope and target verification"
			}
		}
		if p != nil && p.LastJobID != nil {
			var jobErr error
			job, jobErr = s.store.GetJob(r.Context(), *p.LastJobID)
			if jobErr != nil || job == nil {
				http.Error(w, "latest proxy job evidence unavailable", 503)
				return
			}
			if job != nil && job.Status == storage.JobStatusRunning {
				var pending patchsupport.ProxyLifecycle
				if json.Unmarshal(job.Payload, &pending) == nil && !pending.ExpiresAt.After(time.Now().UTC()) {
					if expiry, ok := s.store.(interface {
						ExpireManagedProxy(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
					}); ok {
						if e := expiry.ExpireManagedProxy(r.Context(), proxy.TenantID, proxy.ID, job.ID); e != nil {
							http.Error(w, "cannot persist proxy timeout", 503)
							return
						}
						job, jobErr = s.store.GetJob(r.Context(), job.ID)
						if jobErr != nil || job == nil {
							http.Error(w, "expired proxy job evidence unavailable", 503)
							return
						}
					}
				}
			}
		}
		var jobView any
		message := ""
		if job != nil {
			if job.TenantID != proxy.TenantID || (p != nil && jobPayloadNodeID(job.Payload) != p.NodeID) {
				http.Error(w, "job ownership mismatch", 503)
				return
			}
			jobView = map[string]any{"id": job.ID, "type": job.Type, "status": job.Status, "payload": json.RawMessage(job.Payload), "created_at": job.CreatedAt, "updated_at": job.UpdatedAt}
			events, eventErr := s.store.ListJobEvents(r.Context(), job.ID)
			if eventErr != nil {
				http.Error(w, "job evidence unavailable", 503)
				return
			}
			if len(events) > 0 {
				message = events[len(events)-1].Message
			}
		}
		writeJSON(w, 200, map[string]any{"management": p, "job": jobView, "eligible": eligible, "reason": reason, "message": message})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		NodeID      string   `json:"node_id"`
		ClientCIDRs []string `json:"client_cidrs"`
		ProbeURLs   []string `json:"probe_urls"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil {
		http.Error(w, "invalid binding", 400)
		return
	}
	nodeID, err := uuid.Parse(body.NodeID)
	if err != nil || nodeID == uuid.Nil {
		http.Error(w, "node UUID required", 400)
		return
	}
	p, err := store.BindProxyManagement(r.Context(), proxy.TenantID, proxy.ID, nodeID, body.ClientCIDRs, body.ProbeURLs)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.recordAudit(r.Context(), principal, proxy.TenantID, "patch.proxy.bind", "squid_proxy", proxy.ID.String(), map[string]any{"node_id": nodeID, "client_cidrs": body.ClientCIDRs, "probe_urls": body.ProbeURLs})
	writeJSON(w, 201, map[string]any{"management": p})
}

func (s *Server) requestProxyLifecycle(w http.ResponseWriter, r *http.Request, principal *auth.Principal, proxy *storage.SquidProxy, action string) {
	store, ok := s.store.(proxyManagementStore)
	if !ok {
		http.Error(w, errSquidLifecycleUnqualified.Error(), 501)
		return
	}
	policy, err := store.GetProxyManagement(r.Context(), proxy.TenantID, proxy.ID)
	if err != nil {
		http.Error(w, "binding lookup unavailable", 503)
		return
	}
	if policy == nil {
		http.Error(w, "bind this proxy to its enrolled management host first", 409)
		return
	}
	var body struct {
		ExpectedRevision int64    `json:"expected_revision"`
		Whitelist        []string `json:"whitelist"`
		ClientCIDRs      []string `json:"client_cidrs"`
		ProbeURLs        []string `json:"probe_urls"`
		SquidVersion     string   `json:"squid_version"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&body) != nil {
		http.Error(w, "invalid lifecycle request", 400)
		return
	}
	if body.ExpectedRevision != policy.Revision {
		http.Error(w, "policy changed; reload and review the latest revision", 409)
		return
	}
	node, err := s.store.GetNode(r.Context(), policy.NodeID)
	if err != nil || node == nil || node.TenantID != proxy.TenantID {
		http.Error(w, "bound host unavailable", 409)
		return
	}
	runtime, err := nodePatchRuntime(node, time.Now().UTC())
	if err != nil || runtime.OS != "linux" || runtime.Manager != "apt" || runtime.ProxyLifecycleVersion != 2 {
		http.Error(w, "bound host lacks current Ubuntu/systemd managed-proxy capability; no action queued", 409)
		return
	}
	p := patchsupport.ProxyLifecycle{ProxyID: proxy.ID.String(), NodeID: policy.NodeID.String(), Revision: policy.Revision + 1, Whitelist: body.Whitelist, ClientCIDRs: body.ClientCIDRs, ProbeURLs: body.ProbeURLs, SquidVersion: body.SquidVersion, RequestedBy: principal.Subject, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	if action == "squid.remove" {
		p.Whitelist = nil
		p.ClientCIDRs = nil
		p.ProbeURLs = nil
	}
	if err = p.Validate(action, time.Now().UTC()); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	job, err := store.QueueProxyLifecycle(r.Context(), proxy.TenantID, policy.NodeID, action, p)
	if err != nil {
		http.Error(w, err.Error(), 409)
		return
	}
	s.recordAudit(r.Context(), principal, proxy.TenantID, "patch.proxy."+action, "job", job.ID.String(), map[string]any{"proxy_id": proxy.ID, "node_id": policy.NodeID, "revision": p.Revision, "client_cidrs": p.ClientCIDRs, "whitelist": p.Whitelist})
	writeJSON(w, 202, map[string]any{"job_id": job.ID, "status": job.Status, "message": "Change reserved; service and upstream health are not yet verified"})
}
