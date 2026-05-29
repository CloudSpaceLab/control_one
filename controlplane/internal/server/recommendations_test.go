package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

// TestRecommendationsBridgeWritesPortObservations is the closure test for
// bugs §1.3.
//
// Before this fix the code path was:
//
//	agent POST /nodes/<id>/services
//	  -> ReplaceNodeServices  (writes node_services rows)
//	-- nothing else --
//
// Result: port_observations had zero writers in the codebase, the
// diagnostic SQL "SELECT count(*) FROM port_observations" always returned
// 0, and the Recommendations tab was permanently empty for every tenant.
//
// The test exercises the full ingest -> bridge path with a synthetic
// service inventory and asserts:
//  1. After a single agent ingest, the fake store has > 0 port_observations
//     rows (mirroring the diagnostic SQL).
//  2. After enough ingest cycles to clear the 50-sample / 95%-dominant
//     threshold, handleRecommendations returns at least one recommendation
//     with non-empty evidence — proving the aggregator actually runs
//     against the bridged rows.
func TestRecommendationsBridgeWritesPortObservations(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "recos-tenant", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID:        nodeID,
			TenantID:  tenantID,
			Hostname:  "recos-host",
			State:     storage.NodeStateActive,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}},
	}

	cfg := &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		Auth: authWithTokens("admin", "test-token"),
	}
	srv := New(zap.NewNop(), cfg, store, &stubQueue{})

	body := nodeServicesRequest{
		Services: []nodeServiceItem{
			{PID: 100, Process: "nginx", Port: 443, ServiceKind: "https"},
			{PID: 200, Process: "sshd", Port: 22, ServiceKind: "ssh"},
			{PID: 300, Process: "postgres", Port: 5432, ServiceKind: "postgresql"},
		},
	}
	performIngest(t, srv, nodeID, body)

	if got := len(store.portObservations); got == 0 {
		t.Fatalf("port_observations row count = 0 after ingest; bridge not wired (bugs §1.3)")
	}
	if got, want := len(store.portObservations), len(body.Services); got != want {
		t.Fatalf("port_observations row count = %d, want one per service (%d)", got, want)
	}
	for _, obs := range store.portObservations {
		if obs.TenantID != tenantID {
			t.Errorf("observation tenant = %s, want %s", obs.TenantID, tenantID)
		}
		if obs.NodeID == nil || *obs.NodeID != nodeID {
			t.Errorf("observation node_id missing or mismatched (want %s, got %v)", nodeID, obs.NodeID)
		}
		if obs.State != "open" {
			t.Errorf("observation state = %q, want open (listening service)", obs.State)
		}
		if obs.Protocol == "" {
			t.Errorf("observation protocol is empty")
		}
	}

	// Drive the aggregator over the 50-sample / 95%-dominant gate.
	// Each ingest writes one row per service, so each (port, protocol)
	// pair needs >= 50 cycles to clear the minSamples threshold. We do
	// 59 more (60 total including the first) so every port crosses the
	// gate with a 100%-dominant "open" state.
	for i := 0; i < 59; i++ {
		performIngest(t, srv, nodeID, body)
	}
	if got := len(store.portObservations); got < 60*len(body.Services) {
		t.Fatalf("port_observations row count = %d, want >= %d to clear minSamples per port", got, 60*len(body.Services))
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/recommendations?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, &auth.Principal{
		Type:  "user",
		Name:  "viewer@example.com",
		Roles: []string{"viewer"},
	})
	rec := httptest.NewRecorder()
	srv.handleRecommendations(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("recommendations status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []recommendationResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Data) == 0 {
		t.Fatalf("recommendations returned 0 rules; bridge wrote rows but aggregator did not surface them")
	}
	for _, r := range resp.Data {
		if r.Kind != "port_rule" {
			t.Errorf("rec kind = %q, want port_rule", r.Kind)
		}
		if samples, ok := r.Evidence["samples"]; !ok || samples == nil {
			t.Errorf("rec evidence missing samples: %+v", r.Evidence)
		}
	}
}

// TestBridgePortObservationsRejectsInvalidPort guards the silent-skip path:
// out-of-range ports in the agent payload must NOT generate rows.
func TestBridgePortObservationsRejectsInvalidPort(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "recos-tenant", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	body := nodeServicesRequest{
		Services: []nodeServiceItem{
			{PID: 1, Process: "x", Port: 0, ServiceKind: "http"},
			{PID: 2, Process: "y", Port: 70000, ServiceKind: "http"},
			{PID: 3, Process: "nginx", Port: 80, ServiceKind: "http"},
		},
	}
	performIngest(t, srv, nodeID, body)

	if got, want := len(store.portObservations), 1; got != want {
		t.Fatalf("port_observations rows = %d, want %d (only valid ports persisted)", got, want)
	}
	if store.portObservations[0].Port != 80 {
		t.Errorf("persisted port = %d, want 80", store.portObservations[0].Port)
	}
}

func TestNodeServicesIngestPersistsConnectorProposalLabels(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "connector-tenant", CreatedAt: time.Now()}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	body := nodeServicesRequest{
		Services: []nodeServiceItem{{PID: 1, Process: "nginx", Port: 80, ServiceKind: "nginx"}},
		ConnectorProposals: []connectordiscovery.Proposal{{
			ID:                  "local-log:nginx",
			Kind:                connectordiscovery.KindLocalLog,
			Program:             "nginx",
			CollectorType:       connectordiscovery.CollectorTypeFile,
			Formatter:           "nginx",
			Confidence:          90,
			Risk:                "low",
			AutoConnectEligible: true,
			Paths:               []string{"/var/log/nginx/access.log"},
			Labels:              map[string]string{"discovery_source": "local"},
		}},
	}
	performIngest(t, srv, nodeID, body)

	labels := store.nodes[0].Labels
	if labels["agent.connector_proposal_count"] != 1 {
		t.Fatalf("connector proposal count label = %#v", labels["agent.connector_proposal_count"])
	}
	if labels["agent.connector_auto_eligible_count"] != 1 {
		t.Fatalf("connector auto eligible count label = %#v", labels["agent.connector_auto_eligible_count"])
	}
	proposals, ok := labels["agent.connector_proposals"].([]map[string]any)
	if !ok || len(proposals) != 1 {
		t.Fatalf("connector proposals label = %#v", labels["agent.connector_proposals"])
	}
	if proposals[0]["program"] != "nginx" || proposals[0]["auto_connect_eligible"] != true {
		t.Fatalf("unexpected connector proposal label: %#v", proposals[0])
	}
	if len(store.sourceProposals) != 1 {
		t.Fatalf("durable source proposals = %d, want 1", len(store.sourceProposals))
	}
	if store.sourceProposals[0].Status != storage.ContentPackSourceProposalStatusAutoEligible || store.sourceProposals[0].Program != "nginx" {
		t.Fatalf("unexpected durable source proposal: %#v", store.sourceProposals[0])
	}
	if len(store.sourceStates) != 1 {
		t.Fatalf("source runtime states = %d, want 1", len(store.sourceStates))
	}
	if store.sourceStates[0].State.CoverageState != contentpacks.CoverageState(contentpacks.CoverageProposed) || store.sourceStates[0].State.NodeID != nodeID.String() {
		t.Fatalf("unexpected source runtime state from proposal: %#v", store.sourceStates[0].State)
	}
}

func TestContentPackSourceProposalsAPIListsDurableConnectorProposals(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "connector-tenant", CreatedAt: now}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: now, UpdatedAt: now,
		}},
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:               uuid.New(),
			TenantID:         tenantID,
			NodeID:           nodeID,
			ProposalID:       "local-log:temenos-t24",
			Kind:             connectordiscovery.KindLocalLog,
			Program:          "temenos-t24",
			SourceID:         "temenos-t24",
			CollectorType:    connectordiscovery.CollectorTypeFile,
			Formatter:        "generic",
			Status:           storage.ContentPackSourceProposalStatusApprovalRequired,
			Confidence:       95,
			Risk:             "high",
			RequiresApproval: true,
			Reason:           "running local service matches a sensitive catalog profile and needs operator approval",
			Paths:            []string{"/opt/temenos/*/logs/*.log"},
			Evidence:         []string{"service:kind=weblogic"},
			Labels:           map[string]string{"discovery_source": "local"},
			FirstSeenAt:      now,
			LastSeenAt:       now,
			CreatedAt:        now,
			UpdatedAt:        now,
		}, {
			ID:            uuid.New(),
			TenantID:      tenantID,
			NodeID:        nodeID,
			ProposalID:    "local-log:nginx",
			Kind:          connectordiscovery.KindLocalLog,
			Program:       "nginx",
			SourceID:      "nginx.access",
			CollectorType: connectordiscovery.CollectorTypeFile,
			Status:        storage.ContentPackSourceProposalStatusApproved,
			Risk:          "medium",
			FirstSeenAt:   now,
			LastSeenAt:    now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-proposals?tenant_id="+tenantID.String()+"&limit=1", nil)
	req = withPrincipal(req, &auth.Principal{Type: "user", Name: "viewer@example.com", Roles: []string{"viewer"}})
	rec := httptest.NewRecorder()
	srv.handleContentPackSourceProposals(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("source proposals status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp contentPackSourceProposalListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode source proposals: %v; body=%s", err, rec.Body.String())
	}
	if len(resp.Data) != 1 || resp.Pagination.Total != 2 || resp.Pagination.NextOffset == nil {
		t.Fatalf("source proposals response = %#v", resp.Data)
	}
	if resp.Summary == nil || resp.Summary.Total != 2 || resp.Summary.ByStatus[storage.ContentPackSourceProposalStatusApprovalRequired] != 1 || resp.Summary.ByStatus[storage.ContentPackSourceProposalStatusApproved] != 1 {
		t.Fatalf("source proposals summary = %#v", resp.Summary)
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-proposals?tenant_id="+tenantID.String()+"&q=temenos&status=approval_required", nil)
	filterReq = withPrincipal(filterReq, &auth.Principal{Type: "user", Name: "viewer@example.com", Roles: []string{"viewer"}})
	filterRec := httptest.NewRecorder()
	srv.handleContentPackSourceProposals(filterRec, filterReq)
	if filterRec.Code != http.StatusOK {
		t.Fatalf("filtered source proposals status = %d, want 200; body = %s", filterRec.Code, filterRec.Body.String())
	}
	var filterResp contentPackSourceProposalListResponse
	if err := json.Unmarshal(filterRec.Body.Bytes(), &filterResp); err != nil {
		t.Fatalf("decode filtered source proposals: %v; body=%s", err, filterRec.Body.String())
	}
	if len(filterResp.Data) != 1 || filterResp.Data[0].Status != storage.ContentPackSourceProposalStatusApprovalRequired || filterResp.Data[0].Program != "temenos-t24" {
		t.Fatalf("filtered source proposals response = %#v", filterResp.Data)
	}
	if filterResp.Summary == nil || filterResp.Summary.Total != 1 || filterResp.Summary.ByStatus[storage.ContentPackSourceProposalStatusApprovalRequired] != 1 {
		t.Fatalf("filtered source proposals summary = %#v", filterResp.Summary)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-proposals?tenant_id="+tenantID.String()+"&status=not_real", nil)
	invalidReq = withPrincipal(invalidReq, &auth.Principal{Type: "user", Name: "viewer@example.com", Roles: []string{"viewer"}})
	invalidRec := httptest.NewRecorder()
	srv.handleContentPackSourceProposals(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status response = %d, want 400; body = %s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestContentPackSourceProposalDecisionAPIs(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	approveID := uuid.New()
	rejectID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "connector-tenant", CreatedAt: now}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: now, UpdatedAt: now,
		}},
		sourceProposals: []storage.ContentPackSourceProposalRecord{
			{
				ID:          approveID,
				TenantID:    tenantID,
				NodeID:      nodeID,
				ProposalID:  "local-log:nginx",
				Kind:        connectordiscovery.KindLocalLog,
				Program:     "nginx",
				Status:      storage.ContentPackSourceProposalStatusAutoEligible,
				FirstSeenAt: now,
				LastSeenAt:  now,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
			{
				ID:          rejectID,
				TenantID:    tenantID,
				NodeID:      nodeID,
				ProposalID:  "local-log:temenos-t24",
				Kind:        connectordiscovery.KindLocalLog,
				Program:     "temenos-t24",
				Status:      storage.ContentPackSourceProposalStatusApprovalRequired,
				FirstSeenAt: now,
				LastSeenAt:  now,
				CreatedAt:   now,
				UpdatedAt:   now,
			},
		},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/source-proposals/"+approveID.String()+"/approve?tenant_id="+tenantID.String(), bytes.NewReader([]byte(`{"note":"CAB-91","collect_mode":"metadata_only"}`)))
	approveReq = withPrincipal(approveReq, &auth.Principal{Type: "user", Name: "admin@example.com", Subject: "admin@example.com", Roles: []string{"admin"}})
	approveRec := httptest.NewRecorder()
	srv.handleApproveContentPackSourceProposal(approveRec, approveReq, approveID.String())
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want 200; body = %s", approveRec.Code, approveRec.Body.String())
	}
	var approved contentPackSourceProposalDTO
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approved); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	if approved.Status != storage.ContentPackSourceProposalStatusApproved || approved.ApprovedBySubject != "admin@example.com" || approved.ApprovalNote != "CAB-91" || approved.CollectMode != storage.ContentPackSourceProposalCollectModeMetadataOnly {
		t.Fatalf("approved proposal = %#v", approved)
	}
	if len(store.sourceStates) != 1 || store.sourceStates[0].State.CoverageState != contentpacks.CoverageState(contentpacks.CoverageApproved) {
		t.Fatalf("approved source runtime state = %#v", store.sourceStates)
	}
	approvedStateLabels := store.sourceStates[0].State.Labels
	if approvedStateLabels["collect_mode"] != storage.ContentPackSourceProposalCollectModeMetadataOnly ||
		approvedStateLabels["runtime_evidence"] != "metadata_observed" ||
		approvedStateLabels["metadata_observed"] != "true" ||
		approvedStateLabels["log_collection_started"] != "false" ||
		approvedStateLabels["control_one.raw_message_retained"] != "false" {
		t.Fatalf("metadata-only source runtime labels = %#v", approvedStateLabels)
	}

	rejectReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/source-proposals/"+rejectID.String()+"/reject?tenant_id="+tenantID.String(), bytes.NewReader([]byte(`{"reason":"customer data path requires DPO review","privacy_blocked":true}`)))
	rejectReq = withPrincipal(rejectReq, &auth.Principal{Type: "user", Name: "admin@example.com", Subject: "admin@example.com", Roles: []string{"admin"}})
	rejectRec := httptest.NewRecorder()
	srv.handleRejectContentPackSourceProposal(rejectRec, rejectReq, rejectID.String())
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("reject status = %d, want 200; body = %s", rejectRec.Code, rejectRec.Body.String())
	}
	var rejected contentPackSourceProposalDTO
	if err := json.Unmarshal(rejectRec.Body.Bytes(), &rejected); err != nil {
		t.Fatalf("decode reject response: %v", err)
	}
	if rejected.Status != storage.ContentPackSourceProposalStatusPrivacyBlocked || rejected.RejectedBySubject != "admin@example.com" || rejected.RejectionReason == "" {
		t.Fatalf("rejected proposal = %#v", rejected)
	}
	if len(store.sourceStates) != 2 {
		t.Fatalf("source runtime states after reject = %#v", store.sourceStates)
	}
	var privacyStateSeen bool
	for _, row := range store.sourceStates {
		if row.State.CoverageState == contentpacks.CoverageState(contentpacks.CoveragePrivacyBlocked) {
			privacyStateSeen = true
		}
	}
	if !privacyStateSeen {
		t.Fatalf("privacy-blocked source runtime state missing: %#v", store.sourceStates)
	}
}

func TestNodeApprovedLogSourcesReturnsApprovedLocalFileProposals(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	otherNodeID := uuid.New()
	now := time.Now().UTC()
	approvedID := uuid.New()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "connector-tenant", CreatedAt: now}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: now, UpdatedAt: now,
		}},
		sourceProposals: []storage.ContentPackSourceProposalRecord{
			{
				ID:            approvedID,
				TenantID:      tenantID,
				NodeID:        nodeID,
				ProposalID:    "local-log:nginx",
				Kind:          connectordiscovery.KindLocalLog,
				Program:       "nginx",
				SourceID:      "nginx-access",
				CollectorType: connectordiscovery.CollectorTypeFile,
				Formatter:     "nginx",
				Status:        storage.ContentPackSourceProposalStatusApproved,
				CollectMode:   storage.ContentPackSourceProposalCollectModeCollectParsed,
				Paths:         []string{"/var/log/nginx/access.log", "/var/log/nginx/access.log"},
				Labels:        map[string]string{"discovery_source": "local", "parser_profile": "nginx"},
				FirstSeenAt:   now,
				LastSeenAt:    now,
				ApprovedAt:    &now,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				NodeID:        nodeID,
				ProposalID:    "local-log:postgres",
				Kind:          connectordiscovery.KindLocalLog,
				Program:       "postgres",
				CollectorType: connectordiscovery.CollectorTypeFile,
				Status:        storage.ContentPackSourceProposalStatusApproved,
				CollectMode:   storage.ContentPackSourceProposalCollectModeMetadataOnly,
				Paths:         []string{"/var/log/postgresql/postgresql*.log"},
				FirstSeenAt:   now,
				LastSeenAt:    now,
				ApprovedAt:    &now,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				NodeID:        nodeID,
				ProposalID:    "local-log:temenos-t24",
				Kind:          connectordiscovery.KindLocalLog,
				Program:       "temenos-t24",
				CollectorType: connectordiscovery.CollectorTypeFile,
				Status:        storage.ContentPackSourceProposalStatusApprovalRequired,
				Paths:         []string{"/opt/temenos/*/logs/*.log"},
				FirstSeenAt:   now,
				LastSeenAt:    now,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
			{
				ID:            uuid.New(),
				TenantID:      tenantID,
				NodeID:        otherNodeID,
				ProposalID:    "local-log:postgres",
				Kind:          connectordiscovery.KindLocalLog,
				Program:       "postgres",
				CollectorType: connectordiscovery.CollectorTypeFile,
				Status:        storage.ContentPackSourceProposalStatusApproved,
				Paths:         []string{"/var/log/postgresql/postgresql*.log"},
				FirstSeenAt:   now,
				LastSeenAt:    now,
				CreatedAt:     now,
				UpdatedAt:     now,
			},
		},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/log-sources/approved", nil)
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleNodeApprovedLogSources(rec, req, nodeID)

	if rec.Code != http.StatusOK {
		t.Fatalf("approved log sources status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var resp nodeApprovedLogSourcesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode approved log sources: %v; body=%s", err, rec.Body.String())
	}
	if resp.NodeID != nodeID.String() || len(resp.Sources) != 1 {
		t.Fatalf("approved log sources response = %#v", resp)
	}
	source := resp.Sources[0]
	if source.Program != "nginx" || source.Type != connectordiscovery.CollectorTypeFile || source.Formatter != "nginx" {
		t.Fatalf("approved log source = %#v", source)
	}
	if source.CollectMode != storage.ContentPackSourceProposalCollectModeCollectParsed {
		t.Fatalf("approved log source collect mode = %q", source.CollectMode)
	}
	if len(source.Paths) != 1 || source.Paths[0] != "/var/log/nginx/access.log" {
		t.Fatalf("approved source paths = %#v", source.Paths)
	}
	if source.Labels["control_one.source_proposal_id"] != approvedID.String() ||
		source.Labels["control_one.connector_decision"] != storage.ContentPackSourceProposalStatusApproved ||
		source.Labels["control_one.collect_mode"] != storage.ContentPackSourceProposalCollectModeCollectParsed {
		t.Fatalf("approved source labels = %#v", source.Labels)
	}

	denyReq := httptest.NewRequest(http.MethodGet, "/api/v1/nodes/"+nodeID.String()+"/log-sources/approved", nil)
	denyReq = withPrincipal(denyReq, agentPrincipal(uuid.New()))
	denyRec := httptest.NewRecorder()
	srv.handleNodeApprovedLogSources(denyRec, denyReq, nodeID)
	if denyRec.Code != http.StatusForbidden {
		t.Fatalf("mismatched agent status = %d, want 403; body = %s", denyRec.Code, denyRec.Body.String())
	}
}

func TestLogIngestProjectsAgentLocalSourceRuntimeState(t *testing.T) {
	t.Parallel()

	tenantID := uuid.New()
	nodeID := uuid.New()
	proposalID := uuid.New()
	now := time.Now().UTC()
	store := &fakeStore{
		tenants: []storage.Tenant{{ID: tenantID, Name: "connector-tenant", CreatedAt: now}},
		nodes: []storage.Node{{
			ID: nodeID, TenantID: tenantID, Hostname: "host",
			State: storage.NodeStateActive, CreatedAt: now, UpdatedAt: now,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}, Auth: authWithTokens("admin", "t")}, store, &stubQueue{})

	raw := []byte(fmt.Sprintf(`{
		"node_id":%q,
		"program":"nginx",
		"collector_type":"file",
		"count":1,
		"labels":{
			"control_one.source_proposal_id":%q,
			"control_one.content_pack_source_id":"nginx.access",
			"control_one.collect_mode":"collect_parsed"
		},
		"paths":["/var/log/nginx/access.log"],
		"entries":[{
			"timestamp":"2026-05-28T12:00:00Z",
			"program":"nginx",
			"message":"raw log omitted by collect_parsed",
			"severity":"info",
			"labels":{"control_one.raw_message_retained":"false"},
			"fields":{"status":200,"path":"/customers/123"}
		}]
	}`, nodeID.String(), proposalID.String()))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/logs", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, &auth.Principal{
		Type:    "agent",
		Name:    nodeID.String(),
		Subject: nodeID.String(),
		Roles:   []string{"agent"},
	})
	rec := httptest.NewRecorder()
	srv.handleLogIngest(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("log ingest status = %d, want 202; body = %s", rec.Code, rec.Body.String())
	}
	if len(store.sourceStates) != 1 {
		t.Fatalf("source states = %#v", store.sourceStates)
	}
	state := store.sourceStates[0].State
	if state.SourceID != "nginx.access" || state.CoverageState != contentpacks.CoverageState(contentpacks.CoverageCollecting) {
		t.Fatalf("runtime state identity/status = %#v", state)
	}
	if state.ApprovalID != proposalID.String() || state.CollectorMode != contentpacks.CollectorNodeFileLog {
		t.Fatalf("runtime state approval/collector = %#v", state)
	}
	if state.Metrics.EventsReceived != 1 || state.Metrics.EventsParsed != 1 || state.LastEventAt == nil || state.LastParsedAt == nil {
		t.Fatalf("runtime state metrics/timestamps = %#v", state)
	}
	if state.Labels["control_one.collect_mode"] != storage.ContentPackSourceProposalCollectModeCollectParsed ||
		state.Labels["control_one.raw_message_retained"] != "false" {
		t.Fatalf("runtime state labels = %#v", state.Labels)
	}
}

// TestPortStateFromServiceClassification verifies the probe-status -> state
// mapping used by the bridge.
func TestPortStateFromServiceClassification(t *testing.T) {
	t.Parallel()

	ok := 200
	bad := 502
	zero := 0
	cases := []struct {
		name string
		svc  storage.NodeService
		want string
	}{
		{"no probe", storage.NodeService{}, "open"},
		{"healthy probe", storage.NodeService{ProbeStatus: &ok}, "open"},
		{"5xx probe", storage.NodeService{ProbeStatus: &bad}, "filtered"},
		{"zero probe", storage.NodeService{ProbeStatus: &zero}, "filtered"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portStateFromService(tc.svc); got != tc.want {
				t.Fatalf("portStateFromService(%+v) = %q, want %q", tc.svc, got, tc.want)
			}
		})
	}
}

// performIngest runs one agent inventory POST against handleNodeServicesIngest
// with an mTLS principal whose CN matches nodeID.
func performIngest(t *testing.T, srv *Server, nodeID uuid.UUID, body nodeServicesRequest) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/v1/nodes/%s/services", nodeID), bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withPrincipal(req, agentPrincipal(nodeID))
	rec := httptest.NewRecorder()
	srv.handleNodeServicesIngest(rec, req, nodeID)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ingest status = %d, want 204; body = %s", rec.Code, rec.Body.String())
	}
}
