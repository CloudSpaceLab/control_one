package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
)

type proxyLifecycleReceipts interface {
	RecordProxyLifecycleReceipt(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, string) error
}

// Delivery is read from durable jobs on every heartbeat, never a memory queue.
// Capability is deliberately not advertised by the current unqualified agent.
func (s *Server) proxyLifecycleActions(ctx context.Context, node *storage.Node) []string {
	if node == nil || node.State != storage.NodeStateActive {
		return nil
	}
	runtime, err := nodePatchRuntime(node, time.Now().UTC())
	if err != nil || runtime.OS != "linux" || (runtime.ProxyLifecycleVersion != 1 && runtime.ProxyLifecycleVersion != 2) {
		return nil
	}
	store, ok := s.store.(interface {
		PendingProxyLifecycle(context.Context, uuid.UUID, uuid.UUID) ([]storage.Job, error)
	})
	if !ok {
		return nil
	}
	jobs, err := store.PendingProxyLifecycle(ctx, node.TenantID, node.ID)
	if err != nil {
		return nil
	}
	var out []string
	for _, job := range jobs {
		var p patchsupport.ProxyLifecycle
		if job.ID == uuid.Nil || job.TenantID != node.TenantID || job.Status != storage.JobStatusRunning || json.Unmarshal(job.Payload, &p) != nil || p.NodeID != node.ID.String() || p.Validate(job.Type, time.Now().UTC()) != nil {
			continue
		}
		proxyID, _ := uuid.Parse(p.ProxyID)
		proxy, err := s.store.GetSquidProxy(ctx, proxyID)
		if err != nil || proxy == nil || proxy.ID != proxyID || proxy.TenantID != node.TenantID || proxy.Status == "removed" {
			continue
		}
		if p.Revision > 0 {
			bound, ok := s.store.(proxyManagementStore)
			if !ok || runtime.ProxyLifecycleVersion != 2 {
				continue
			}
			policy, e := bound.GetProxyManagement(ctx, node.TenantID, proxyID)
			if e != nil || policy == nil || policy.NodeID != node.ID || policy.Revision != p.Revision || policy.LastJobID == nil || *policy.LastJobID != job.ID {
				continue
			}
		}
		out = append(out, job.Type+":"+job.ID.String())
	}
	return out
}

func (s *Server) completeProxyLifecycle(ctx context.Context, nodeID uuid.UUID, c heartbeatCompletedAction) {
	if c.Status != "succeeded" && c.Status != "failed" {
		return
	}
	id, err := uuid.Parse(c.JobID)
	if err != nil {
		return
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil || node == nil {
		return
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil || job == nil || job.Type != c.Action || job.TenantID != node.TenantID || jobPayloadNodeID(job.Payload) != nodeID {
		return
	}
	store, ok := s.store.(proxyLifecycleReceipts)
	if !ok {
		return
	}
	message := "Target reported local proxy action completion; upstream readiness remains unverified"
	if c.Status == "failed" {
		message = c.Error
	}
	var payload patchsupport.ProxyLifecycle
	if json.Unmarshal(job.Payload, &payload) != nil {
		return
	}
	if payload.Revision > 0 {
		managed, ok := s.store.(interface {
			RecordManagedProxyReceipt(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, string, patchsupport.ProxyLifecycleProof) error
		})
		if !ok {
			return
		}
		var proof patchsupport.ProxyLifecycleProof
		body, _ := json.Marshal(c.Metadata["proxy_proof"])
		_ = json.Unmarshal(body, &proof)
		_ = managed.RecordManagedProxyReceipt(ctx, node.TenantID, nodeID, id, c.Action, c.Status, message, proof)
		return
	}
	_ = store.RecordProxyLifecycleReceipt(ctx, node.TenantID, nodeID, id, c.Action, c.Status, message)
}

// A heartbeat HTTP 200 is not a durable acknowledgment. Read back the receipt
// after the transaction committed; never acknowledge another target's result.
func (s *Server) proxyReceiptPersisted(ctx context.Context, nodeID uuid.UUID, c heartbeatCompletedAction) bool {
	if c.Status != "succeeded" && c.Status != "failed" {
		return false
	}
	id, err := uuid.Parse(c.JobID)
	if err != nil {
		return false
	}
	node, err := s.store.GetNode(ctx, nodeID)
	if err != nil || node == nil {
		return false
	}
	job, err := s.store.GetJob(ctx, id)
	if err != nil || job == nil || job.TenantID != node.TenantID || job.Type != c.Action || jobPayloadNodeID(job.Payload) != nodeID {
		return false
	}
	var receipt struct {
		Status string `json:"_proxy_receipt"`
	}
	return json.Unmarshal(job.Payload, &receipt) == nil && receipt.Status == c.Status
}
