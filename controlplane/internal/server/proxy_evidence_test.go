package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/patchsupport"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestProxyEvidenceNeverInfersActivityFromSavedStatus(t *testing.T) {
	now := time.Now().UTC()
	p := storage.SquidProxy{ID: uuid.New(), TenantID: uuid.New(), Status: "installing"}
	require.Equal(t, "legacy_unverified", proxyRecordEvidence(p, nil, nil, now).State)
	p.Status = "registered"
	require.Equal(t, "registered", proxyRecordEvidence(p, nil, nil, now).State)
	id, node := uuid.New(), uuid.New()
	policy := &storage.ProxyManagement{ProxyID: p.ID, NodeID: node, Revision: 2, LastJobID: &id}
	request := patchsupport.ProxyLifecycle{ProxyID: p.ID.String(), NodeID: node.String(), Revision: 2, Whitelist: []string{"archive.ubuntu.com"}, ClientCIDRs: []string{"127.0.0.1/32"}, ExpiresAt: now.Add(time.Minute)}
	body, _ := json.Marshal(request)
	job := &storage.Job{ID: id, TenantID: p.TenantID, Type: "squid.reconfigure", Status: storage.JobStatusRunning, Payload: body}
	require.Equal(t, "pending", proxyRecordEvidence(p, policy, job, now).State)
	require.Equal(t, "outcome_unknown", proxyRecordEvidence(p, policy, job, now.Add(2*time.Minute)).State)
	job.Status = storage.JobStatusFailed
	require.Equal(t, "failed_or_unknown", proxyRecordEvidence(p, policy, job, now).State)
	job.Status = storage.JobStatusSucceeded
	p.Status = "healthy"
	p.LastValidatedAt = &now
	require.Equal(t, "failed_or_unknown", proxyRecordEvidence(p, policy, job, now).State, "job success without target receipt cannot certify readiness")
	digest, err := patchsupport.ProxyConfigSHA(request.Whitelist, request.ClientCIDRs)
	require.NoError(t, err)
	proof := patchsupport.ProxyLifecycleProof{ConfigSHA256: digest, ServiceActive: true, UpstreamVerified: true, ObservedAt: now}
	raw, _ := json.Marshal(request)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(raw, &fields))
	fields["_proxy_receipt"] = "succeeded"
	fields["_proxy_proof"] = proof
	job.Payload, _ = json.Marshal(fields)
	require.Equal(t, "verification_recorded", proxyRecordEvidence(p, policy, job, now).State)
	require.Equal(t, "verification_recorded", proxyRecordEvidence(p, policy, job, now.Add(24*time.Hour)).State, "past success must be labelled historical, not current healthy")
	for _, change := range []func(*storage.Job){func(j *storage.Job) { j.TenantID = uuid.New() }, func(j *storage.Job) { j.ID = uuid.New() }, func(j *storage.Job) { j.Type = "other" }} {
		bad := *job
		change(&bad)
		require.Equal(t, "unverified", proxyRecordEvidence(p, policy, &bad, now).State)
	}
	proof.UpstreamVerified = false
	fields["_proxy_proof"] = proof
	job.Payload, _ = json.Marshal(fields)
	require.Equal(t, "unverified", proxyRecordEvidence(p, policy, job, now).State)
}
