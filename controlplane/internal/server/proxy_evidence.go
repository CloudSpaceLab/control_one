package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type proxyEvidence struct {
	State   string     `json:"state"`
	Message string     `json:"message"`
	JobID   *uuid.UUID `json:"job_id,omitempty"`
}
type proxyListRow struct {
	storage.SquidProxy
	Evidence proxyEvidence
}

func proxyRecordEvidence(p storage.SquidProxy, policy *storage.ProxyManagement, job *storage.Job, now time.Time) proxyEvidence {
	unknown := proxyEvidence{State: "unverified", Message: "Saved status is historical; no matching lifecycle evidence establishes progress or health."}
	if policy == nil {
		if p.Status == "registered" {
			return proxyEvidence{State: "registered", Message: "Connection details saved; no installation or connectivity verification."}
		}
		return proxyEvidence{State: "legacy_unverified", Message: "Legacy saved status: " + p.Status + ". No managed lifecycle job proves this operation is running."}
	}
	if policy.ProxyID != p.ID || policy.NodeID == uuid.Nil {
		return unknown
	}
	if policy.LastJobID == nil {
		return proxyEvidence{State: "registered", Message: "Management host bound; no lifecycle action has been requested."}
	}
	if job == nil || job.ID != *policy.LastJobID || job.TenantID != p.TenantID || !patchsupport.IsProxyLifecycle(job.Type) {
		return unknown
	}
	var payload struct {
		patchsupport.ProxyLifecycle
		Receipt string                           `json:"_proxy_receipt"`
		Proof   patchsupport.ProxyLifecycleProof `json:"_proxy_proof"`
	}
	if json.Unmarshal(job.Payload, &payload) != nil || payload.ProxyID != p.ID.String() || payload.NodeID != policy.NodeID.String() || payload.Revision != policy.Revision {
		return unknown
	}
	unknown.JobID = &job.ID
	if job.Status == storage.JobStatusRunning || job.Status == storage.JobStatusQueued {
		if !payload.ExpiresAt.After(now) {
			return proxyEvidence{State: "outcome_unknown", Message: "Request expired; target outcome requires reconciliation. No automatic retry.", JobID: &job.ID}
		}
		return proxyEvidence{State: "pending", Message: "Awaiting target result; job delivery does not prove installation has started or completed.", JobID: &job.ID}
	}
	if job.Status != storage.JobStatusSucceeded || payload.Receipt != "succeeded" {
		return proxyEvidence{State: "failed_or_unknown", Message: "Latest change did not establish verified success. Inspect its recorded result before retrying.", JobID: &job.ID}
	}
	if job.Type == "squid.remove" && p.Status == "removed" && payload.Proof.Removed && !payload.Proof.ServiceActive {
		return proxyEvidence{State: "removed_verified", Message: "Recorded target proof confirms removal; retained packages and history remain.", JobID: &job.ID}
	}
	digest, err := patchsupport.ProxyConfigSHA(payload.Whitelist, payload.ClientCIDRs)
	if err != nil || payload.Proof.ConfigSHA256 != digest || !payload.Proof.ServiceActive || !payload.Proof.UpstreamVerified || p.LastValidatedAt == nil || payload.Proof.ObservedAt.IsZero() || !p.LastValidatedAt.Equal(payload.Proof.ObservedAt) || payload.Proof.ObservedAt.After(now.Add(30*time.Second)) || p.Status != "healthy" {
		return unknown
	}
	return proxyEvidence{State: "verification_recorded", Message: "Service and repository checks passed at the recorded time. This is historical evidence, not continuous health or production sign-off.", JobID: &job.ID}
}

func (s *Server) proxyListEvidence(ctx context.Context, p storage.SquidProxy) proxyEvidence {
	store, ok := s.store.(interface {
		GetProxyManagement(context.Context, uuid.UUID, uuid.UUID) (*storage.ProxyManagement, error)
	})
	if !ok {
		return proxyEvidence{State: "unavailable", Message: "Lifecycle evidence unavailable; saved status cannot establish progress or health."}
	}
	policy, err := store.GetProxyManagement(ctx, p.TenantID, p.ID)
	if err != nil {
		return proxyEvidence{State: "unavailable", Message: "Management evidence could not be loaded."}
	}
	var job *storage.Job
	if policy != nil && policy.LastJobID != nil {
		job, err = s.store.GetJob(ctx, *policy.LastJobID)
		if err != nil {
			return proxyEvidence{State: "unavailable", Message: "Latest job evidence could not be loaded."}
		}
	}
	return proxyRecordEvidence(p, policy, job, time.Now().UTC())
}
