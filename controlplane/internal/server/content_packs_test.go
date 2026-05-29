package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/connectordiscovery"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

func TestSyncOfflineContentPacksPersistsRegistrySnapshot(t *testing.T) {
	root := t.TempDir()
	writeServerActiveContentPack(t, root, false)

	tenantID := uuid.New()
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	srv := &Server{
		logger:             zap.NewNop(),
		store:              store,
		offlineContentRoot: root,
	}
	now := time.Date(2026, 5, 27, 20, 0, 0, 0, time.UTC)
	srv.syncOfflineContentPacks(context.Background(), tenantID, &offlinebundle.Receipt{
		BundleID:   "siem-pack",
		Sequence:   4,
		ImportedAt: now,
		Contents: []offlinebundle.ContentReceipt{{
			Type: offlinebundle.ContentTypeSIEMContentPack,
			Name: "controlone-server-replay",
		}},
	})

	if len(store.saved) != 1 {
		t.Fatalf("saved snapshots = %#v, want one", store.saved)
	}
	saved := store.saved[0]
	if saved.Source != "offline_bundle:siem-pack:4" || saved.PackCount != 1 {
		t.Fatalf("saved snapshot metadata = %#v", saved)
	}
	registry, err := contentpacks.NewRegistryFromSnapshot(saved.Snapshot, "")
	if err != nil {
		t.Fatalf("restore saved snapshot: %v", err)
	}
	resolved, ok := registry.ResolveSource("controlone.server_pack")
	if !ok || resolved.ContentStatus != contentpacks.PackStatusEnabled {
		t.Fatalf("resolved = %#v ok=%v, want enabled server pack", resolved, ok)
	}
}

func TestSyncOfflineContentPacksPersistsQuarantinedReplayFailure(t *testing.T) {
	root := t.TempDir()
	writeServerActiveContentPack(t, root, true)

	tenantID := uuid.New()
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	srv := &Server{
		logger:             zap.NewNop(),
		store:              store,
		offlineContentRoot: root,
	}
	now := time.Date(2026, 5, 27, 20, 30, 0, 0, time.UTC)
	srv.syncOfflineContentPacks(context.Background(), tenantID, &offlinebundle.Receipt{
		BundleID:   "siem-pack",
		Sequence:   5,
		ImportedAt: now,
		Contents: []offlinebundle.ContentReceipt{{
			Type: offlinebundle.ContentTypeSIEMContentPack,
			Name: "controlone-server-replay",
		}},
	})

	if len(store.saved) != 1 {
		t.Fatalf("saved snapshots = %#v, want one", store.saved)
	}
	record := store.saved[0].Snapshot.Packs[0]
	if record.Status != contentpacks.PackStatusQuarantined || record.QuarantineReason == "" {
		t.Fatalf("snapshot record = %#v, want quarantined replay failure", record)
	}
}

func TestContentPacksAPIListsActiveRegistrySnapshot(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 0, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:7",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "content-pack-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs?tenant_id="+tenantID.String(), nil)
	req.Header.Set("Authorization", "Bearer content-pack-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Source != "offline_bundle:siem-pack:7" || resp.Totals.Packs != 1 || resp.Totals.Sources != 1 {
		t.Fatalf("response summary = %#v", resp)
	}
	if len(resp.Items) != 1 || resp.Items[0].PackID != "controlone.server_pack" || resp.Items[0].Status != string(contentpacks.PackStatusEnabled) {
		t.Fatalf("items = %#v", resp.Items)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].OperationalStatus != contentpacks.CoverageProposed {
		t.Fatalf("sources = %#v", resp.Sources)
	}
}

func TestContentPackSourcesAPIOmitsPackInventoryButKeepsSources(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 30, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:8",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "content-pack-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/sources?tenant_id="+tenantID.String(), nil)
	req.Header.Set("Authorization", "Bearer content-pack-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %#v, want omitted inventory slice", resp.Items)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].SourceID != "controlone.server_pack" {
		t.Fatalf("sources = %#v", resp.Sources)
	}
}

func TestContentPackOTelConfigAPIRendersEnabledSource(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 45, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:10",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	body := strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-1",
		"source_ids":["controlone.server_pack"],
		"headers":{"x-controlone-token":"redacted"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TenantID != tenantID.String() || len(resp.Sources) != 1 {
		t.Fatalf("response metadata = %#v", resp)
	}
	if resp.Sources[0].PipelineID != "logs/controlone.controlone.server_pack" {
		t.Fatalf("pipeline id = %q", resp.Sources[0].PipelineID)
	}
	if !strings.HasPrefix(resp.ConfigVersion, "sha256:") || len(resp.ConfigVersion) != len("sha256:")+64 {
		t.Fatalf("config version = %q, want sha256 digest", resp.ConfigVersion)
	}
	for _, want := range []string{
		"filelog/controlone.controlone.server_pack:",
		"resource/controlone.source.controlone.server_pack:",
		"file_storage/controlone:",
		"storage: file_storage/controlone",
		"x-controlone-token: redacted",
		"/var/log/controlone/server-pack.log",
	} {
		if !strings.Contains(resp.YAML, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, resp.YAML)
		}
	}
}

func TestContentPackOTelConfigAPIRejectsUnapprovedSensitiveSource(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 50, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	manifest.Sources[0].RiskClass = contentpacks.RiskHigh
	manifest.Sources[0].DataSensitivity = contentpacks.SensitivityHigh
	manifest.Sources[0].ApprovalRequired = true
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:11",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	body := strings.NewReader(`{"endpoint":"controlone.local:4317","source_ids":["controlone.server_pack"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "requires approval") {
		t.Fatalf("status = %d body=%s, want approval rejection", rec.Code, rec.Body.String())
	}
}

func TestContentPackOTelConfigCandidateAPICreatesFromApprovedSourceProposal(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	proposalID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 52, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:                  proposalID,
			TenantID:            tenantID,
			NodeID:              nodeID,
			ProposalID:          "local-log:controlone.server_pack",
			Kind:                connectordiscovery.KindLocalLog,
			Program:             "controlone-server",
			SourceID:            "controlone.server_pack",
			CollectorType:       connectordiscovery.CollectorTypeFile,
			Formatter:           "generic",
			Status:              storage.ContentPackSourceProposalStatusApproved,
			Confidence:          95,
			Risk:                "high",
			AutoConnectEligible: false,
			RequiresApproval:    true,
			Paths:               []string{"/var/log/controlone/server-pack.log"},
			Labels:              map[string]string{"discovery_source": "local"},
			FirstSeenAt:         now,
			LastSeenAt:          now,
			ApprovedAt:          &now,
			CreatedAt:           now,
			UpdatedAt:           now,
		}},
	}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	manifest.Sources[0].RiskClass = contentpacks.RiskHigh
	manifest.Sources[0].DataSensitivity = contentpacks.SensitivityHigh
	manifest.Sources[0].ApprovalRequired = true
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:11b",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	body := strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-proposal-1",
		"source_proposal_ids":["` + proposalID.String() + `"]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.CandidateID == "" || created.CandidateStatus != storage.ContentPackCollectorConfigStatusRendered {
		t.Fatalf("candidate fields = id:%q status:%q", created.CandidateID, created.CandidateStatus)
	}
	if len(created.Sources) != 1 || created.Sources[0].SourceID != "controlone.server_pack" || created.Sources[0].ApprovalRef != proposalID.String() {
		t.Fatalf("rendered sources = %#v", created.Sources)
	}
	if created.Sources[0].Mode != contentpacks.CollectorOTelFileLog {
		t.Fatalf("rendered mode = %q, want otel_filelog", created.Sources[0].Mode)
	}
	if len(store.candidates) != 1 || len(store.candidates[0].SourceIDs) != 1 || store.candidates[0].SourceIDs[0] != "controlone.server_pack" {
		t.Fatalf("stored candidates = %#v", store.candidates)
	}
	if !strings.Contains(created.YAML, "c1.approval_ref") || !strings.Contains(created.YAML, proposalID.String()) {
		t.Fatalf("rendered YAML missing proposal approval ref:\n%s", created.YAML)
	}
	if len(store.sourceStates) != 1 {
		t.Fatalf("source runtime states = %#v, want one config_rendered state", store.sourceStates)
	}
	runtimeState := store.sourceStates[0].State
	if runtimeState.CoverageState != contentpacks.CoverageState(contentpacks.CoverageConfigRendered) ||
		runtimeState.ConfigVersion != created.ConfigVersion ||
		runtimeState.ApprovalID != proposalID.String() ||
		runtimeState.CollectorID != "edge-proposal-1" {
		t.Fatalf("source runtime state = %#v", runtimeState)
	}
}

func TestContentPackOTelConfigCandidateAPIResolvesApprovedProposalSourceHint(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	proposalID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 55, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:                  proposalID,
			TenantID:            tenantID,
			NodeID:              nodeID,
			ProposalID:          "local-log:controlone.server_pack",
			Kind:                connectordiscovery.KindLocalLog,
			Program:             "controlone-server",
			SourceID:            "controlone.server_pack",
			CollectorType:       connectordiscovery.CollectorTypeFile,
			Formatter:           "generic",
			Status:              storage.ContentPackSourceProposalStatusApproved,
			Confidence:          95,
			Risk:                "medium",
			AutoConnectEligible: false,
			RequiresApproval:    true,
			Paths:               []string{"/var/log/controlone/server-pack.log"},
			Labels:              map[string]string{"parser_profile": "controlone.server_pack"},
			FirstSeenAt:         now,
			LastSeenAt:          now,
			ApprovedAt:          &now,
			CreatedAt:           now,
			UpdatedAt:           now,
		}},
	}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	manifest.Sources[0].SourceID = "controlone.server_pack.file"
	manifest.Sources[0].Labels = map[string]string{"parser_profile": "controlone.server_pack"}
	manifest.Samples[0].SourceID = "controlone.server_pack.file"
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:11c",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	body := strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-proposal-hint-1",
		"source_proposal_ids":["` + proposalID.String() + `"]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if len(created.Sources) != 1 || created.Sources[0].SourceID != "controlone.server_pack.file" || created.Sources[0].ApprovalRef != proposalID.String() {
		t.Fatalf("rendered sources = %#v", created.Sources)
	}
	if len(store.candidates) != 1 || len(store.candidates[0].SourceIDs) != 1 || store.candidates[0].SourceIDs[0] != "controlone.server_pack.file" {
		t.Fatalf("stored candidates = %#v", store.candidates)
	}
	if len(store.sourceStates) != 1 || store.sourceStates[0].State.SourceID != "controlone.server_pack.file" || store.sourceStates[0].State.ApprovalID != proposalID.String() {
		t.Fatalf("source states = %#v", store.sourceStates)
	}
}

func TestContentPackSourceIDForProposalRejectsAmbiguousHint(t *testing.T) {
	now := time.Date(2026, 5, 27, 21, 57, 0, 0, time.UTC)
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	manifest.Sources[0].SourceID = "controlone.server_pack.access"
	manifest.Sources[0].Labels = map[string]string{"parser_profile": "controlone.server_pack"}
	manifest.Sources[0].Parsers = []string{"controlone.server_pack.access"}
	manifest.Samples[0].SourceID = "controlone.server_pack.access"
	manifest.Samples[0].ParserID = "controlone.server_pack.access"
	manifest.Parsers[0].ParserID = "controlone.server_pack.access"
	second := manifest.Sources[0]
	second.SourceID = "controlone.server_pack.error"
	second.Parsers = []string{"controlone.server_pack.error"}
	secondParser := manifest.Parsers[0]
	secondParser.ParserID = "controlone.server_pack.error"
	manifest.Parsers = append(manifest.Parsers, secondParser)
	secondSample := manifest.Samples[0]
	secondSample.CaseID = "controlone.server_pack.error.good"
	secondSample.SourceID = "controlone.server_pack.error"
	secondSample.ParserID = "controlone.server_pack.error"
	second.Samples = []string{secondSample.CaseID}
	manifest.Sources = append(manifest.Sources, second)
	manifest.Samples = append(manifest.Samples, secondSample)
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	_, err := contentPackSourceIDForProposal(registry, storage.ContentPackSourceProposalRecord{
		ID:       uuid.New(),
		Program:  "controlone-server",
		SourceID: "controlone.server_pack",
		Labels:   map[string]string{"parser_profile": "controlone.server_pack"},
	})
	if err == nil || !strings.Contains(err.Error(), "matches multiple enabled content-pack sources") {
		t.Fatalf("err = %v, want ambiguous source error", err)
	}
}

func TestContentPackOTelConfigAPIRejectsUnapprovedSourceProposal(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	proposalID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 53, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:            proposalID,
			TenantID:      tenantID,
			NodeID:        nodeID,
			ProposalID:    "local-log:controlone.server_pack",
			Kind:          connectordiscovery.KindLocalLog,
			Program:       "controlone-server",
			SourceID:      "controlone.server_pack",
			CollectorType: connectordiscovery.CollectorTypeFile,
			Status:        storage.ContentPackSourceProposalStatusApprovalRequired,
			FirstSeenAt:   now,
			LastSeenAt:    now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:11c",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	body := strings.NewReader(`{"endpoint":"controlone.local:4317","source_proposal_ids":["` + proposalID.String() + `"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "must be approved") {
		t.Fatalf("status = %d body=%s, want approval rejection", rec.Code, rec.Body.String())
	}
}

func TestContentPackOTelConfigAPIRejectsNonDeployableApprovedSourceProposal(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	proposalID := uuid.New()
	now := time.Date(2026, 5, 28, 12, 20, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:            proposalID,
			TenantID:      tenantID,
			NodeID:        nodeID,
			ProposalID:    "local-log:controlone.server_pack",
			Kind:          connectordiscovery.KindLocalLog,
			Program:       "controlone-server",
			SourceID:      "controlone.server_pack",
			CollectorType: connectordiscovery.CollectorTypeFile,
			Status:        storage.ContentPackSourceProposalStatusApproved,
			CollectMode:   storage.ContentPackSourceProposalCollectModeMetadataOnly,
			FirstSeenAt:   now,
			LastSeenAt:    now,
			ApprovedAt:    &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:11d",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	body := strings.NewReader(`{"endpoint":"controlone.local:4317","source_proposal_ids":["` + proposalID.String() + `"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "only collect_raw or collect_parsed approvals can be rendered") {
		t.Fatalf("status = %d body=%s, want collect mode rejection", rec.Code, rec.Body.String())
	}
}

func TestContentPackOTelConfigAPIRendersCollectParsedSourceProposalWithRawRedaction(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	proposalID := uuid.New()
	now := time.Date(2026, 5, 28, 12, 25, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{
		sourceProposals: []storage.ContentPackSourceProposalRecord{{
			ID:            proposalID,
			TenantID:      tenantID,
			NodeID:        nodeID,
			ProposalID:    "local-log:controlone.server_pack",
			Kind:          connectordiscovery.KindLocalLog,
			Program:       "controlone-server",
			SourceID:      "controlone.server_pack",
			CollectorType: connectordiscovery.CollectorTypeFile,
			Status:        storage.ContentPackSourceProposalStatusApproved,
			CollectMode:   storage.ContentPackSourceProposalCollectModeCollectParsed,
			FirstSeenAt:   now,
			LastSeenAt:    now,
			ApprovedAt:    &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}},
	}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:11e",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	body := strings.NewReader(`{"endpoint":"controlone.local:4317","source_proposal_ids":["` + proposalID.String() + `"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Sources) != 1 || resp.Sources[0].CollectMode != contentpacks.OTelCollectModeCollectParsed {
		t.Fatalf("rendered sources = %#v", resp.Sources)
	}
	for _, want := range []string{
		"transform/controlone.source.controlone.server_pack.redact_raw:",
		`set(attributes["control_one.raw_message_retained"], "false")`,
		`set(body, "raw log omitted by collect_parsed")`,
	} {
		if !strings.Contains(resp.YAML, want) {
			t.Fatalf("rendered YAML missing %q:\n%s", want, resp.YAML)
		}
	}
}

func TestContentPackOTelConfigCandidateAPICreatesAndListsCandidate(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 21, 55, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:12",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	body := strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-1",
		"source_ids":["controlone.server_pack"],
		"storage_directory":"/var/lib/controlone/otelcol/storage"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.CandidateID == "" || created.CandidateStatus != storage.ContentPackCollectorConfigStatusRendered {
		t.Fatalf("candidate fields = id:%q status:%q", created.CandidateID, created.CandidateStatus)
	}
	if len(store.candidates) != 1 || store.candidates[0].ConfigVersion != created.ConfigVersion {
		t.Fatalf("stored candidates = %#v, response version=%s", store.candidates, created.ConfigVersion)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), nil)
	listReq.Header.Set("Authorization", "Bearer content-pack-admin")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed paginatedResponse[contentPackOTelConfigCandidateDTO]
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].ID != created.CandidateID || listed.Data[0].ConfigVersion != created.ConfigVersion {
		t.Fatalf("listed candidates = %#v", listed.Data)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"?tenant_id="+tenantID.String(), nil)
	detailReq.Header.Set("Authorization", "Bearer content-pack-admin")
	detailRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status = %d body=%s", detailRec.Code, detailRec.Body.String())
	}
	var detail contentPackOTelConfigCandidateDetailDTO
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode detail response: %v", err)
	}
	if detail.ID != created.CandidateID || detail.YAML == "" || len(detail.Sources) != 1 {
		t.Fatalf("detail = %#v", detail)
	}
	if !strings.Contains(detail.YAML, "/var/lib/controlone/otelcol/storage") || !strings.Contains(detail.YAML, "filelog/controlone.controlone.server_pack") {
		t.Fatalf("detail YAML missing expected rendered config:\n%s", detail.YAML)
	}

	otherTenantReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"?tenant_id="+uuid.New().String(), nil)
	otherTenantReq.Header.Set("Authorization", "Bearer content-pack-admin")
	otherTenantRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(otherTenantRec, otherTenantReq)
	if otherTenantRec.Code != http.StatusNotFound {
		t.Fatalf("other tenant detail status = %d body=%s, want 404", otherTenantRec.Code, otherTenantRec.Body.String())
	}
}

func TestContentPackOTelConfigCandidateAPIApprovesCandidate(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 22, 5, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:13",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	body := strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-approval-1",
		"source_ids":["controlone.server_pack"]
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	missingReviewReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/approve?tenant_id="+tenantID.String(), strings.NewReader(`{"note":"missing reviewed config version"}`))
	missingReviewReq.Header.Set("Authorization", "Bearer content-pack-admin")
	missingReviewRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missingReviewRec, missingReviewReq)
	if missingReviewRec.Code != http.StatusBadRequest || !strings.Contains(missingReviewRec.Body.String(), "reviewed_config_version is required") {
		t.Fatalf("missing review status = %d body=%s, want reviewed_config_version rejection", missingReviewRec.Code, missingReviewRec.Body.String())
	}

	approveBody := strings.NewReader(`{"note":"CAB-42 approved for lab collector","reviewed_config_version":"` + created.ConfigVersion + `"}`)
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/approve?tenant_id="+tenantID.String(), approveBody)
	approveReq.Header.Set("Authorization", "Bearer content-pack-admin")
	approveRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", approveRec.Code, approveRec.Body.String())
	}
	var approved contentPackOTelConfigCandidateDTO
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approved); err != nil {
		t.Fatalf("decode approve response: %v", err)
	}
	if approved.Status != storage.ContentPackCollectorConfigStatusApproved || approved.ApprovedBySubject != "content-pack-admin" || approved.ApprovedAt == "" {
		t.Fatalf("approved candidate = %#v", approved)
	}
	if approved.ApprovalNote != "CAB-42 approved for lab collector" {
		t.Fatalf("approval note = %q", approved.ApprovalNote)
	}
	if approved.ReviewedConfigVersion != created.ConfigVersion || approved.ReviewedYAMLSHA256 != strings.TrimPrefix(created.ConfigVersion, "sha256:") {
		t.Fatalf("review ack = version:%q yaml:%q", approved.ReviewedConfigVersion, approved.ReviewedYAMLSHA256)
	}
	if len(store.candidates) != 1 || store.candidates[0].Status != storage.ContentPackCollectorConfigStatusApproved {
		t.Fatalf("stored candidates = %#v", store.candidates)
	}
	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "content_pack.otel_config_candidate.approved" {
		t.Fatalf("audit logs = %#v", store.auditLogs)
	}
}

func TestContentPackOTelConfigCandidateAPIQueuesApprovedCandidateForCollector(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 22, 10, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:14",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors?tenant_id="+tenantID.String(), strings.NewReader(`{"collector_id":"edge-queue-1"}`))
	registerReq.Header.Set("Authorization", "Bearer content-pack-admin")
	registerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", registerRec.Code, registerRec.Body.String())
	}

	createBody := strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-queue-1",
		"source_ids":["controlone.server_pack"]
	}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), createBody)
	createReq.Header.Set("Authorization", "Bearer content-pack-admin")
	createRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/approve?tenant_id="+tenantID.String(), strings.NewReader(`{"note":"CAB-43","reviewed_config_version":"`+created.ConfigVersion+`"}`))
	approveReq.Header.Set("Authorization", "Bearer content-pack-admin")
	approveRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", approveRec.Code, approveRec.Body.String())
	}

	missingExpectedReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/queue?tenant_id="+tenantID.String(), strings.NewReader(`{"note":"missing expected version"}`))
	missingExpectedReq.Header.Set("Authorization", "Bearer content-pack-admin")
	missingExpectedRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(missingExpectedRec, missingExpectedReq)
	if missingExpectedRec.Code != http.StatusBadRequest || !strings.Contains(missingExpectedRec.Body.String(), "expected_config_version is required") {
		t.Fatalf("missing expected queue status = %d body=%s, want expected_config_version rejection", missingExpectedRec.Code, missingExpectedRec.Body.String())
	}

	queueReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/queue?tenant_id="+tenantID.String(), strings.NewReader(`{"note":"maintenance window 1","expected_config_version":"`+created.ConfigVersion+`"}`))
	queueReq.Header.Set("Authorization", "Bearer content-pack-admin")
	queueRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(queueRec, queueReq)
	if queueRec.Code != http.StatusOK {
		t.Fatalf("queue status = %d body=%s", queueRec.Code, queueRec.Body.String())
	}
	var queued contentPackOTelConfigCandidateDTO
	if err := json.Unmarshal(queueRec.Body.Bytes(), &queued); err != nil {
		t.Fatalf("decode queue response: %v", err)
	}
	if queued.Status != storage.ContentPackCollectorConfigStatusQueued || queued.TargetCollectorID != "edge-queue-1" || queued.QueuedAt == "" {
		t.Fatalf("queued candidate = %#v", queued)
	}
	if queued.QueueNote != "maintenance window 1" || queued.QueuedBySubject != "content-pack-admin" {
		t.Fatalf("queue audit fields = %#v", queued)
	}
	if len(store.collectors) != 1 || store.collectors[0].DesiredConfigVersion != created.ConfigVersion {
		t.Fatalf("collectors = %#v, wanted desired config %s", store.collectors, created.ConfigVersion)
	}
	if len(store.sourceStates) != 1 || store.sourceStates[0].State.CoverageState != contentpacks.CoverageState(contentpacks.CoverageConfigRendered) || store.sourceStates[0].State.CollectorID != "edge-queue-1" {
		t.Fatalf("source runtime state after queue = %#v", store.sourceStates)
	}
	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "content_pack.otel_config_candidate.queued" {
		t.Fatalf("audit logs = %#v", store.auditLogs)
	}

	desiredReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/collectors/edge-queue-1/desired-config?tenant_id="+tenantID.String(), nil)
	desiredReq.Header.Set("Authorization", "Bearer content-pack-admin")
	desiredRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(desiredRec, desiredReq)
	if desiredRec.Code != http.StatusOK {
		t.Fatalf("desired config status = %d body=%s", desiredRec.Code, desiredRec.Body.String())
	}
	var desired contentPackEdgeCollectorDesiredConfigResponse
	if err := json.Unmarshal(desiredRec.Body.Bytes(), &desired); err != nil {
		t.Fatalf("decode desired config response: %v", err)
	}
	if desired.CandidateID != created.CandidateID || desired.ConfigVersion != created.ConfigVersion || !strings.Contains(desired.YAML, "filelog/controlone.controlone.server_pack") {
		t.Fatalf("desired config = %#v", desired)
	}

	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-queue-1/apply-result?tenant_id="+tenantID.String(), strings.NewReader(`{"config_version":"`+created.ConfigVersion+`","status":"deployed"}`))
	applyReq.Header.Set("Authorization", "Bearer content-pack-admin")
	applyRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply result status = %d body=%s", applyRec.Code, applyRec.Body.String())
	}
	var deployed contentPackOTelConfigCandidateDTO
	if err := json.Unmarshal(applyRec.Body.Bytes(), &deployed); err != nil {
		t.Fatalf("decode apply result response: %v", err)
	}
	if deployed.Status != storage.ContentPackCollectorConfigStatusDeployed || deployed.DeployedAt == "" {
		t.Fatalf("deployed candidate = %#v", deployed)
	}
	if store.collectors[0].RunningConfigVersion != created.ConfigVersion || store.collectors[0].Status != storage.ContentPackEdgeCollectorStatusHealthy {
		t.Fatalf("collector after apply = %#v", store.collectors[0])
	}
	if len(store.sourceStates) != 1 || store.sourceStates[0].State.CoverageState != contentpacks.CoverageState(contentpacks.CoverageDeployed) || store.sourceStates[0].State.ConfigVersion != created.ConfigVersion {
		t.Fatalf("source runtime state after deploy = %#v", store.sourceStates)
	}
	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "content_pack.otel_config_candidate.deployed" {
		t.Fatalf("audit logs after apply = %#v", store.auditLogs)
	}
}

func TestContentPackOTelConfigApplyFailureKeepsSourceConfigRenderedWithError(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 22, 12, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:14b",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors?tenant_id="+tenantID.String(), strings.NewReader(`{"collector_id":"edge-fail-1"}`))
	registerReq.Header.Set("Authorization", "Bearer content-pack-admin")
	registerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", registerRec.Code, registerRec.Body.String())
	}
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates?tenant_id="+tenantID.String(), strings.NewReader(`{
		"endpoint":"controlone.local:4317",
		"collector_id":"edge-fail-1",
		"source_ids":["controlone.server_pack"]
	}`))
	createReq.Header.Set("Authorization", "Bearer content-pack-admin")
	createRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", createRec.Code, createRec.Body.String())
	}
	var created contentPackOTelConfigRenderResponse
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/approve?tenant_id="+tenantID.String(), strings.NewReader(`{"note":"CAB-44","reviewed_config_version":"`+created.ConfigVersion+`"}`))
	approveReq.Header.Set("Authorization", "Bearer content-pack-admin")
	approveRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", approveRec.Code, approveRec.Body.String())
	}
	queueReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/otel-config/candidates/"+created.CandidateID+"/queue?tenant_id="+tenantID.String(), strings.NewReader(`{"expected_config_version":"`+created.ConfigVersion+`"}`))
	queueReq.Header.Set("Authorization", "Bearer content-pack-admin")
	queueRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(queueRec, queueReq)
	if queueRec.Code != http.StatusOK {
		t.Fatalf("queue status = %d body=%s", queueRec.Code, queueRec.Body.String())
	}
	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-fail-1/apply-result?tenant_id="+tenantID.String(), strings.NewReader(`{"config_version":"`+created.ConfigVersion+`","status":"failed","error":"receiver bind failed"}`))
	applyReq.Header.Set("Authorization", "Bearer content-pack-admin")
	applyRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply failure status = %d body=%s", applyRec.Code, applyRec.Body.String())
	}
	if len(store.sourceStates) != 1 {
		t.Fatalf("source runtime states after failure = %#v", store.sourceStates)
	}
	state := store.sourceStates[0].State
	if state.CoverageState != contentpacks.CoverageState(contentpacks.CoverageConfigRendered) || state.LastError != "receiver bind failed" || state.Labels["candidate_status"] != storage.ContentPackCollectorConfigStatusFailed {
		t.Fatalf("source runtime state after failure = %#v", state)
	}
}

func TestContentPackEdgeCollectorsAPIRegistersListsAndRecordsHeartbeat(t *testing.T) {
	tenantID := uuid.New()
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	registerBody := strings.NewReader(`{
		"collector_id":"edge-otel-1",
		"kind":"otel",
		"display_name":"Primary edge collector",
		"endpoint":"edge-otel-1.bank.local:4317",
		"version":"otelcol-contrib 0.129.0",
		"desired_config_version":"sha256:desired"
	}`)
	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors?tenant_id="+tenantID.String(), registerBody)
	registerReq.Header.Set("Authorization", "Bearer content-pack-admin")
	registerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", registerRec.Code, registerRec.Body.String())
	}
	var registered contentPackEdgeCollectorDTO
	if err := json.Unmarshal(registerRec.Body.Bytes(), &registered); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if registered.CollectorID != "edge-otel-1" || registered.Status != storage.ContentPackEdgeCollectorStatusRegistered {
		t.Fatalf("registered collector = %#v", registered)
	}

	heartbeatBody := strings.NewReader(`{
		"status":"healthy",
		"running_config_version":"sha256:desired",
		"health":{"receivers":{"syslog/controlone.firewall":"ok"},"exporter_queue_depth":0}
	}`)
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-otel-1/heartbeat?tenant_id="+tenantID.String(), heartbeatBody)
	heartbeatReq.Header.Set("Authorization", "Bearer content-pack-admin")
	heartbeatRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(heartbeatRec, heartbeatReq)
	if heartbeatRec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s", heartbeatRec.Code, heartbeatRec.Body.String())
	}
	var heartbeat contentPackEdgeCollectorDTO
	if err := json.Unmarshal(heartbeatRec.Body.Bytes(), &heartbeat); err != nil {
		t.Fatalf("decode heartbeat response: %v", err)
	}
	if heartbeat.Status != storage.ContentPackEdgeCollectorStatusHealthy || heartbeat.RunningConfigVersion != "sha256:desired" || heartbeat.LastHeartbeatAt == "" {
		t.Fatalf("heartbeat collector = %#v", heartbeat)
	}
	if heartbeat.Health["exporter_queue_depth"] != float64(0) {
		t.Fatalf("health = %#v", heartbeat.Health)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/collectors?tenant_id="+tenantID.String(), nil)
	listReq.Header.Set("Authorization", "Bearer content-pack-admin")
	listRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var listed paginatedResponse[contentPackEdgeCollectorDTO]
	if err := json.Unmarshal(listRec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Status != storage.ContentPackEdgeCollectorStatusHealthy {
		t.Fatalf("listed collectors = %#v", listed.Data)
	}
	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "content_pack.edge_collector.registered" {
		t.Fatalf("audit logs = %#v", store.auditLogs)
	}
}

func TestContentPackEdgeCollectorTokenAuthorizesSelfServiceCalls(t *testing.T) {
	tenantID := uuid.New()
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	registerReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors?tenant_id="+tenantID.String(), strings.NewReader(`{"collector_id":"edge-token-1"}`))
	registerReq.Header.Set("Authorization", "Bearer content-pack-admin")
	registerRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", registerRec.Code, registerRec.Body.String())
	}

	tokenReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-token-1/token?tenant_id="+tenantID.String(), nil)
	tokenReq.Header.Set("Authorization", "Bearer content-pack-admin")
	tokenRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(tokenRec, tokenReq)
	if tokenRec.Code != http.StatusOK {
		t.Fatalf("token status = %d body=%s", tokenRec.Code, tokenRec.Body.String())
	}
	var tokenResp contentPackEdgeCollectorTokenResponse
	if err := json.Unmarshal(tokenRec.Body.Bytes(), &tokenResp); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	if !strings.HasPrefix(tokenResp.Token, storage.ContentPackEdgeCollectorTokenPrefix) || tokenResp.Collector.TokenLastFour == "" {
		t.Fatalf("token response = %#v", tokenResp)
	}

	heartbeatReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-token-1/heartbeat?tenant_id="+tenantID.String(), strings.NewReader(`{"status":"healthy","running_config_version":"sha256:runtime"}`))
	heartbeatReq.Header.Set("X-ControlOne-Collector-Token", tokenResp.Token)
	heartbeatRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(heartbeatRec, heartbeatReq)
	if heartbeatRec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s", heartbeatRec.Code, heartbeatRec.Body.String())
	}

	now := time.Now().UTC()
	configVersion := "sha256:queued"
	store.candidates = append(store.candidates, storage.ContentPackCollectorConfigCandidate{
		ID:                uuid.New(),
		TenantID:          tenantID,
		Status:            storage.ContentPackCollectorConfigStatusQueued,
		ConfigVersion:     configVersion,
		TargetCollectorID: "edge-token-1",
		Plan:              contentpacks.OTelCollectorConfigPlan{},
		RenderedYAML:      "receivers: {}\n",
		QueuedAt:          &now,
		CreatedAt:         now,
		UpdatedAt:         now,
	})

	desiredReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/collectors/edge-token-1/desired-config?tenant_id="+tenantID.String(), nil)
	desiredReq.Header.Set("X-ControlOne-Collector-Token", tokenResp.Token)
	desiredRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(desiredRec, desiredReq)
	if desiredRec.Code != http.StatusOK {
		t.Fatalf("desired status = %d body=%s", desiredRec.Code, desiredRec.Body.String())
	}

	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-token-1/apply-result?tenant_id="+tenantID.String(), strings.NewReader(`{"config_version":"`+configVersion+`","status":"deployed"}`))
	applyReq.Header.Set("X-ControlOne-Collector-Token", tokenResp.Token)
	applyRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply status = %d body=%s", applyRec.Code, applyRec.Body.String())
	}
	if store.collectors[0].RunningConfigVersion != configVersion {
		t.Fatalf("collector running config = %q, want %q", store.collectors[0].RunningConfigVersion, configVersion)
	}
	lastAudit := store.auditLogs[len(store.auditLogs)-1]
	if lastAudit.Action != "content_pack.otel_config_candidate.deployed" || lastAudit.Metadata["collector_auth"] != true {
		t.Fatalf("last audit = %#v", lastAudit)
	}

	badReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-token-1/heartbeat?tenant_id="+tenantID.String(), strings.NewReader(`{"status":"healthy"}`))
	badReq.Header.Set("X-ControlOne-Collector-Token", storage.ContentPackEdgeCollectorTokenPrefix+"wrong")
	badRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(badRec, badReq)
	if badRec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d body=%s, want 401", badRec.Code, badRec.Body.String())
	}
}

func TestContentPackSourceHealthAPIListsCollectorEvidence(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	fresh := now.Add(-1 * time.Minute)
	store := &contentPackSnapshotFakeStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		collectors: []storage.ContentPackEdgeCollector{{
			ID:              uuid.New(),
			TenantID:        tenantID,
			CollectorID:     "edge-health-1",
			Kind:            storage.ContentPackEdgeCollectorKindOTel,
			Status:          storage.ContentPackEdgeCollectorStatusHealthy,
			LastHeartbeatAt: &fresh,
			Health: map[string]any{
				"sources": map[string]any{
					"linux.auth": map[string]any{
						"source_instance_id": "edge-health-1/linux.auth",
						"coverage_state":     contentpacks.CoverageParserHealthy,
						"approval_required":  true,
						"approval_id":        "proposal-linux-auth",
						"events_received":    12,
						"events_parsed":      12,
						"last_parsed_at":     fresh.Format(time.RFC3339),
						"labels": map[string]any{
							"collect_mode":         "collect_parsed",
							"raw_message_retained": "false",
						},
					},
					"fortinet.firewall": map[string]any{
						"status":         contentpacks.CoverageBackpressured,
						"queue_depth":    7,
						"events_dropped": 1,
					},
				},
			},
			CreatedAt: now.Add(-30 * time.Minute),
			UpdatedAt: fresh,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "content-pack-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-health?tenant_id="+tenantID.String(), nil)
	req.Header.Set("Authorization", "Bearer content-pack-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("source health status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackSourceHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode source health response: %v", err)
	}
	if resp.Totals.Sources != 2 || resp.Totals.CollectorsReporting != 1 {
		t.Fatalf("totals = %#v", resp.Totals)
	}
	if resp.Totals.ByState[contentpacks.CoverageParserHealthy] != 1 || resp.Totals.ByState[contentpacks.CoverageBackpressured] != 1 {
		t.Fatalf("by state = %#v", resp.Totals.ByState)
	}
	if resp.Items[0].CollectorID != "edge-health-1" || resp.Items[0].SourceID == "" {
		t.Fatalf("items = %#v", resp.Items)
	}
	var linuxAuth contentPackSourceHealthDTO
	for _, item := range resp.Items {
		if item.SourceID == "linux.auth" {
			linuxAuth = item
			break
		}
	}
	if linuxAuth.SourceID == "" || linuxAuth.Labels["collect_mode"] != "collect_parsed" || linuxAuth.Labels["raw_message_retained"] != "false" {
		t.Fatalf("linux.auth labels = %#v", linuxAuth.Labels)
	}
	if linuxAuth.SourceInstanceID != "edge-health-1/linux.auth" || !linuxAuth.ApprovalRequired || linuxAuth.ApprovalID != "proposal-linux-auth" {
		t.Fatalf("linux.auth approval/source instance = %#v", linuxAuth)
	}
}

func TestContentPackSourceHealthAPIAppliesFreshnessWindow(t *testing.T) {
	tenantID := uuid.New()
	now := time.Now().UTC()
	stale := now.Add(-2 * coverageHeartbeatFreshnessWindow)
	store := &contentPackSnapshotFakeStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		sourceStates: []storage.ContentPackSourceRuntimeStateRecord{{
			ID:       uuid.New(),
			TenantID: tenantID,
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "edge-stale-1/linux.auth",
				SourceID:         "linux.auth",
				CollectorID:      "edge-stale-1",
				CollectorMode:    "syslog",
				ParserID:         "linux.auth.syslog",
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageParserHealthy),
				Metrics: contentpacks.SourceRuntimeMetrics{
					EventsReceived: 10,
					EventsParsed:   10,
				},
				LastHealthAt: &stale,
				Labels: map[string]string{
					"collect_mode": "collect_raw",
				},
			},
			CreatedAt: stale,
			UpdatedAt: stale,
		}, {
			ID:       uuid.New(),
			TenantID: tenantID,
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "edge-fresh-1/postgres",
				SourceID:         "postgres.audit",
				CollectorID:      "edge-fresh-1",
				CollectorMode:    "filelog",
				ParserID:         "postgres.audit",
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageCollecting),
				LastHealthAt:     &now,
			},
			CreatedAt: now,
			UpdatedAt: now,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "content-pack-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-health?tenant_id="+tenantID.String()+"&limit=1&offset=0", nil)
	req.Header.Set("Authorization", "Bearer content-pack-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("source health status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackSourceHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode source health response: %v", err)
	}
	if resp.Totals.Sources != 2 || resp.Items[0].CoverageState != contentpacks.CoverageStale {
		t.Fatalf("source health response = %#v", resp)
	}
	if resp.Totals.ByState[contentpacks.CoverageStale] != 1 || resp.Totals.ByState[contentpacks.CoverageCollecting] != 1 || resp.Totals.Metrics.EventsReceived != 10 {
		t.Fatalf("source health totals = %#v", resp.Totals)
	}
	if resp.Pagination == nil || resp.Pagination.Total != 2 || resp.Pagination.Count != 1 || resp.Pagination.NextOffset == nil || *resp.Pagination.NextOffset != 1 {
		t.Fatalf("source health pagination = %#v", resp.Pagination)
	}
	if resp.Items[0].Labels["collect_mode"] != "collect_raw" {
		t.Fatalf("source health labels = %#v", resp.Items[0].Labels)
	}
	if resp.Items[0].RuntimeStateID == "" || len(resp.Items[0].RecommendedActions) != 1 || resp.Items[0].RecommendedActions[0].Action != "source_health.investigate" {
		t.Fatalf("source health investigation action = %#v", resp.Items[0])
	}

	filterReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-health?tenant_id="+tenantID.String()+"&q=postgres&state=collecting", nil)
	filterReq.Header.Set("Authorization", "Bearer content-pack-viewer")
	filterRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(filterRec, filterReq)
	if filterRec.Code != http.StatusOK {
		t.Fatalf("filtered source health status = %d body=%s", filterRec.Code, filterRec.Body.String())
	}
	var filterResp contentPackSourceHealthResponse
	if err := json.Unmarshal(filterRec.Body.Bytes(), &filterResp); err != nil {
		t.Fatalf("decode filtered source health response: %v", err)
	}
	if filterResp.Pagination == nil || filterResp.Pagination.Total != 1 || len(filterResp.Items) != 1 || filterResp.Items[0].SourceID != "postgres.audit" {
		t.Fatalf("filtered source health response = %#v", filterResp)
	}

	staleReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-health?tenant_id="+tenantID.String()+"&state=stale", nil)
	staleReq.Header.Set("Authorization", "Bearer content-pack-viewer")
	staleRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusOK {
		t.Fatalf("stale source health status = %d body=%s", staleRec.Code, staleRec.Body.String())
	}
	var staleResp contentPackSourceHealthResponse
	if err := json.Unmarshal(staleRec.Body.Bytes(), &staleResp); err != nil {
		t.Fatalf("decode stale source health response: %v", err)
	}
	if staleResp.Pagination == nil || staleResp.Pagination.Total != 1 || len(staleResp.Items) != 1 || staleResp.Items[0].CoverageState != contentpacks.CoverageStale {
		t.Fatalf("stale source health response = %#v", staleResp)
	}

	invalidReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-health?tenant_id="+tenantID.String()+"&state=not_real", nil)
	invalidReq.Header.Set("Authorization", "Bearer content-pack-viewer")
	invalidRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(invalidRec, invalidReq)
	if invalidRec.Code != http.StatusBadRequest {
		t.Fatalf("invalid state status = %d body=%s", invalidRec.Code, invalidRec.Body.String())
	}
}

func TestContentPackSourceHealthInvestigationCreatesSOCCase(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Now().UTC()
	runtimeStateID := uuid.New()
	store := &contentPackSnapshotFakeStore{
		fakeStore: &fakeStore{tenants: []storage.Tenant{{ID: tenantID, Name: "Acme Security"}}},
		sourceStates: []storage.ContentPackSourceRuntimeStateRecord{{
			ID:       runtimeStateID,
			TenantID: tenantID,
			State: contentpacks.SourceRuntimeState{
				SourceInstanceID: "edge-parser-1/postgres.audit",
				SourceID:         "postgres.audit",
				DisplayName:      "Postgres audit",
				NodeID:           nodeID.String(),
				CollectorID:      "edge-parser-1",
				CollectorMode:    "filelog",
				ParserID:         "postgres.audit.otel",
				CoverageState:    contentpacks.CoverageState(contentpacks.CoverageParserFailed),
				ConfigVersion:    "sha256:collector",
				ContentVersion:   "sha256:content",
				LastError:        "grok parse failed on timestamp",
				Metrics: contentpacks.SourceRuntimeMetrics{
					EventsReceived: 42,
					ParseFailures:  7,
				},
				LastHealthAt: &now,
				Labels: map[string]string{
					"collect_mode": "collect_parsed",
				},
			},
			CreatedAt: now.Add(-time.Minute),
			UpdatedAt: now,
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	body := bytes.NewBufferString(`{"runtime_state_id":"` + runtimeStateID.String() + `","note":"triage parser"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/source-health/investigate?tenant_id="+tenantID.String(), body)
	req.Header.Set("Authorization", "Bearer content-pack-admin")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("investigate status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackSourceHealthInvestigationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode investigation response: %v", err)
	}
	if resp.CaseID == "" || resp.Case.TriggerType != "siem_source_health" || resp.Case.TriggerEventType != "content_pack.source_health.parser_failed" {
		t.Fatalf("investigation response = %#v", resp)
	}
	if len(resp.Case.EvidenceRefs) != 1 || resp.Case.EvidenceRefs[0].Kind != "content_pack_source_runtime_state" || !strings.Contains(resp.Case.EvidenceRefs[0].ID, runtimeStateID.String()) {
		t.Fatalf("investigation evidence refs = %#v", resp.Case.EvidenceRefs)
	}
	if len(store.aiInvestigations) != 1 {
		t.Fatalf("expected one stored investigation, got %d", len(store.aiInvestigations))
	}
	created := store.aiInvestigations[0]
	if created.NodeID != nodeID || created.Severity != "high" || !strings.Contains(created.Summary, "Parser failure") {
		t.Fatalf("created investigation = %#v", created)
	}
	var evidence map[string]any
	if err := json.Unmarshal(created.Evidence, &evidence); err != nil {
		t.Fatalf("decode evidence: %v", err)
	}
	if evidence["runtime_state_id"] != runtimeStateID.String() || evidence["source_id"] != "postgres.audit" || evidence["operator_note"] != "triage parser" {
		t.Fatalf("evidence = %#v", evidence)
	}
	noteBody := bytes.NewBufferString(`{"note":"Parser pack owner is reviewing the failing timestamp pattern.","citations":["` + resp.Case.EvidenceRefs[0].ID + `"]}`)
	noteReq := httptest.NewRequest(http.MethodPost, "/api/v1/soc/cases/"+resp.CaseID+"/notes?tenant_id="+tenantID.String(), noteBody)
	noteReq.Header.Set("Authorization", "Bearer content-pack-admin")
	noteRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(noteRec, noteReq)
	if noteRec.Code != http.StatusCreated {
		t.Fatalf("source health case note status = %d body=%s", noteRec.Code, noteRec.Body.String())
	}
	var note socCaseNoteResponse
	if err := json.Unmarshal(noteRec.Body.Bytes(), &note); err != nil {
		t.Fatalf("decode source health case note: %v", err)
	}
	if len(note.Citations) != 1 || note.Citations[0].ID != resp.Case.EvidenceRefs[0].ID {
		t.Fatalf("source health note citations = %#v", note.Citations)
	}
}

func TestContentPackEdgeCollectorHeartbeatPersistsSourceRuntimeState(t *testing.T) {
	tenantID := uuid.New()
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})

	heartbeatBody := strings.NewReader(`{
		"status":"healthy",
		"running_config_version":"sha256:runtime",
		"health":{
			"sources":{
				"linux.auth":{
					"coverage_state":"parser_healthy",
					"source_instance_id":"edge-persist-1/linux.auth",
					"approval_required":true,
					"approval_id":"proposal-linux-auth",
					"parser_id":"linux.auth.syslog",
					"collector_mode":"syslog",
					"events_received":42,
					"events_parsed":42,
					"labels":{
						"collect_mode":"collect_raw",
						"pipeline_id":"logs/controlone.linux.auth"
					}
				}
			}
		}
	}`)
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/edge-persist-1/heartbeat?tenant_id="+tenantID.String(), heartbeatBody)
	heartbeatReq.Header.Set("Authorization", "Bearer content-pack-admin")
	heartbeatRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(heartbeatRec, heartbeatReq)
	if heartbeatRec.Code != http.StatusOK {
		t.Fatalf("heartbeat status = %d body=%s", heartbeatRec.Code, heartbeatRec.Body.String())
	}
	if len(store.sourceStates) != 1 {
		t.Fatalf("source states = %#v, want one", store.sourceStates)
	}
	state := store.sourceStates[0].State
	if state.SourceInstanceID != "edge-persist-1/linux.auth" || state.CoverageState != contentpacks.CoverageState(contentpacks.CoverageParserHealthy) {
		t.Fatalf("persisted source state = %#v", state)
	}
	if state.Metrics.EventsReceived != 42 || state.ConfigVersion != "sha256:runtime" {
		t.Fatalf("persisted source metrics/config = %#v", state)
	}
	if !state.ApprovalRequired || state.ApprovalID != "proposal-linux-auth" {
		t.Fatalf("persisted source approval = %#v", state)
	}
	if state.Labels["collect_mode"] != "collect_raw" || state.Labels["pipeline_id"] != "logs/controlone.linux.auth" {
		t.Fatalf("persisted source labels = %#v", state.Labels)
	}

	sourceHealthReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/source-health?tenant_id="+tenantID.String(), nil)
	sourceHealthReq.Header.Set("Authorization", "Bearer content-pack-admin")
	sourceHealthRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(sourceHealthRec, sourceHealthReq)
	if sourceHealthRec.Code != http.StatusOK {
		t.Fatalf("source health status = %d body=%s", sourceHealthRec.Code, sourceHealthRec.Body.String())
	}
	var resp contentPackSourceHealthResponse
	if err := json.Unmarshal(sourceHealthRec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode source health response: %v", err)
	}
	if resp.Totals.Sources != 1 || resp.Items[0].ConfigVersion != "sha256:runtime" || resp.Items[0].ParserID != "linux.auth.syslog" {
		t.Fatalf("source health response = %#v", resp)
	}
	if resp.Items[0].SourceInstanceID != "edge-persist-1/linux.auth" || !resp.Items[0].ApprovalRequired || resp.Items[0].ApprovalID != "proposal-linux-auth" {
		t.Fatalf("source health approval/source instance = %#v", resp.Items[0])
	}
	if resp.Items[0].Labels["collect_mode"] != "collect_raw" || resp.Items[0].Labels["pipeline_id"] != "logs/controlone.linux.auth" {
		t.Fatalf("source health labels = %#v", resp.Items[0].Labels)
	}
}

func TestContentPackEdgeCollectorRollbackQueuesSupersededConfig(t *testing.T) {
	tenantID := uuid.New()
	collectorID := "edge-rollback-1"
	now := time.Date(2026, 5, 27, 22, 20, 0, 0, time.UTC)
	firstID := uuid.New()
	secondID := uuid.New()
	firstVersion := "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	secondVersion := "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	store := &contentPackSnapshotFakeStore{
		fakeStore: &fakeStore{},
		candidates: []storage.ContentPackCollectorConfigCandidate{
			{
				ID:                firstID,
				TenantID:          tenantID,
				Status:            storage.ContentPackCollectorConfigStatusSuperseded,
				ConfigVersion:     firstVersion,
				CollectorID:       collectorID,
				TargetCollectorID: collectorID,
				SourceIDs:         []string{"controlone.server_pack"},
				Plan: contentpacks.OTelCollectorConfigPlan{
					Sources: []contentpacks.OTelCollectorSourcePlan{{SourceID: "controlone.server_pack"}},
				},
				RenderedYAML: "receivers:\n  filelog/rollback: {}\n",
				CreatedAt:    now.Add(-2 * time.Hour),
				UpdatedAt:    now.Add(-30 * time.Minute),
				DeployedAt:   ptrContentPackTime(now.Add(-90 * time.Minute)),
			},
			{
				ID:                secondID,
				TenantID:          tenantID,
				Status:            storage.ContentPackCollectorConfigStatusDeployed,
				ConfigVersion:     secondVersion,
				CollectorID:       collectorID,
				TargetCollectorID: collectorID,
				SourceIDs:         []string{"controlone.server_pack"},
				Plan: contentpacks.OTelCollectorConfigPlan{
					Sources: []contentpacks.OTelCollectorSourcePlan{{SourceID: "controlone.server_pack"}},
				},
				RenderedYAML: "receivers:\n  filelog/current: {}\n",
				CreatedAt:    now.Add(-1 * time.Hour),
				UpdatedAt:    now.Add(-5 * time.Minute),
				DeployedAt:   ptrContentPackTime(now.Add(-5 * time.Minute)),
			},
		},
		collectors: []storage.ContentPackEdgeCollector{{
			ID:                   uuid.New(),
			TenantID:             tenantID,
			CollectorID:          collectorID,
			Kind:                 storage.ContentPackEdgeCollectorKindOTel,
			Status:               storage.ContentPackEdgeCollectorStatusHealthy,
			DesiredConfigVersion: secondVersion,
			RunningConfigVersion: secondVersion,
			Health:               map[string]any{},
			CreatedAt:            now.Add(-2 * time.Hour),
			UpdatedAt:            now.Add(-5 * time.Minute),
		}},
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("admin", "content-pack-admin"),
	}, store, &stubQueue{})
	srv.auditAsync = false

	rollbackReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/"+collectorID+"/rollback?tenant_id="+tenantID.String(), strings.NewReader(`{"config_version":"`+firstVersion+`","note":"rollback v2 parser regression"}`))
	rollbackReq.Header.Set("Authorization", "Bearer content-pack-admin")
	rollbackRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rollbackRec, rollbackReq)
	if rollbackRec.Code != http.StatusOK {
		t.Fatalf("rollback status = %d body=%s", rollbackRec.Code, rollbackRec.Body.String())
	}
	var rollback contentPackOTelConfigCandidateDTO
	if err := json.Unmarshal(rollbackRec.Body.Bytes(), &rollback); err != nil {
		t.Fatalf("decode rollback response: %v", err)
	}
	if rollback.ID != firstID.String() || rollback.Status != storage.ContentPackCollectorConfigStatusQueued || rollback.QueuedAt == "" {
		t.Fatalf("rollback candidate = %#v", rollback)
	}
	if rollback.QueueNote != "rollback v2 parser regression" || store.collectors[0].DesiredConfigVersion != firstVersion || store.collectors[0].RunningConfigVersion != secondVersion {
		t.Fatalf("rollback queue state candidate=%#v collector=%#v", rollback, store.collectors[0])
	}
	if len(store.sourceStates) != 1 ||
		store.sourceStates[0].State.CoverageState != contentpacks.CoverageState(contentpacks.CoverageConfigRendered) ||
		store.sourceStates[0].State.CollectorID != collectorID ||
		store.sourceStates[0].State.ConfigVersion != firstVersion {
		t.Fatalf("source runtime state after rollback queue = %#v", store.sourceStates)
	}
	if len(store.auditLogs) == 0 || store.auditLogs[len(store.auditLogs)-1].Action != "content_pack.otel_config_candidate.rollback_queued" {
		t.Fatalf("audit logs after rollback = %#v", store.auditLogs)
	}

	desiredReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/collectors/"+collectorID+"/desired-config?tenant_id="+tenantID.String(), nil)
	desiredReq.Header.Set("Authorization", "Bearer content-pack-admin")
	desiredRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(desiredRec, desiredReq)
	if desiredRec.Code != http.StatusOK {
		t.Fatalf("desired config status = %d body=%s", desiredRec.Code, desiredRec.Body.String())
	}
	var desired contentPackEdgeCollectorDesiredConfigResponse
	if err := json.Unmarshal(desiredRec.Body.Bytes(), &desired); err != nil {
		t.Fatalf("decode desired config response: %v", err)
	}
	if desired.CandidateID != firstID.String() || desired.ConfigVersion != firstVersion || !strings.Contains(desired.YAML, "filelog/rollback") {
		t.Fatalf("desired rollback config = %#v", desired)
	}

	applyReq := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/collectors/"+collectorID+"/apply-result?tenant_id="+tenantID.String(), strings.NewReader(`{"config_version":"`+firstVersion+`","status":"deployed"}`))
	applyReq.Header.Set("Authorization", "Bearer content-pack-admin")
	applyRec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply rollback status = %d body=%s", applyRec.Code, applyRec.Body.String())
	}
	if store.candidates[0].Status != storage.ContentPackCollectorConfigStatusDeployed || store.candidates[1].Status != storage.ContentPackCollectorConfigStatusSuperseded {
		t.Fatalf("candidate statuses after rollback apply = %#v", store.candidates)
	}
	if store.collectors[0].RunningConfigVersion != firstVersion || store.collectors[0].Status != storage.ContentPackEdgeCollectorStatusHealthy {
		t.Fatalf("collector after rollback apply = %#v", store.collectors[0])
	}
	if len(store.sourceStates) != 1 ||
		store.sourceStates[0].State.CoverageState != contentpacks.CoverageState(contentpacks.CoverageDeployed) ||
		store.sourceStates[0].State.CollectorID != collectorID ||
		store.sourceStates[0].State.ConfigVersion != firstVersion {
		t.Fatalf("source runtime state after rollback apply = %#v", store.sourceStates)
	}
}

func TestContentPacksAPIRequiresTenantID(t *testing.T) {
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "content-pack-viewer"),
	}, &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs", nil)
	req.Header.Set("Authorization", "Bearer content-pack-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestCoverageMatrixIncludesContentPackRegistryOverlay(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 27, 22, 0, 0, 0, time.UTC)
	store := &contentPackSnapshotFakeStore{fakeStore: &fakeStore{}}
	registry := contentpacks.NewRegistry("1.0.0")
	manifest := serverContentPackManifest()
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("install manifest: %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("enable manifest: %v", err)
	}
	if _, err := store.SaveContentPackRegistrySnapshot(context.Background(), storage.SaveContentPackRegistrySnapshotParams{
		TenantID: tenantID,
		Source:   "offline_bundle:siem-pack:9",
		Snapshot: registry.Snapshot(now),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	srv := New(zap.NewNop(), &config.Config{
		HTTP: config.HTTPConfig{Address: ":0"},
		TLS:  config.TLSConfig{RequireClientTLS: false},
		Auth: authWithTokens("viewer", "content-pack-viewer"),
	}, store, &stubQueue{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/coverage/matrix?tenant_id="+tenantID.String()+"&domain=parser", nil)
	req.Header.Set("Authorization", "Bearer content-pack-viewer")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp coverageMatrixResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode coverage response: %v", err)
	}
	row := findCoverageRow(resp.Matrix, "Tenant SIEM content-pack registry")
	if row == nil {
		t.Fatalf("content-pack overlay missing from parser coverage rows: %#v", resp.Matrix)
	}
	if row.State != coverageStatePartial {
		t.Fatalf("overlay state = %q, want partial", row.State)
	}
	if !contentPackTestContainsString(row.Signals, "packs_enabled=1") || !contentPackTestContainsString(row.Signals, "sources_declared=1") {
		t.Fatalf("overlay signals = %#v", row.Signals)
	}
}

type contentPackSnapshotFakeStore struct {
	*fakeStore
	active             *storage.ContentPackRegistrySnapshotRecord
	saved              []storage.ContentPackRegistrySnapshotRecord
	candidates         []storage.ContentPackCollectorConfigCandidate
	collectors         []storage.ContentPackEdgeCollector
	sourceStates       []storage.ContentPackSourceRuntimeStateRecord
	collectorTokens    map[string]string
	detectionOverrides []storage.ContentPackDetectionOverride
	detectionArtifacts []storage.ContentPackDetectionArtifact
}

func contentPackTestContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ptrContentPackTime(t time.Time) *time.Time {
	return &t
}

func (f *contentPackSnapshotFakeStore) ActiveContentPackRegistrySnapshot(context.Context, uuid.UUID) (*storage.ContentPackRegistrySnapshotRecord, error) {
	return f.active, nil
}

func (f *contentPackSnapshotFakeStore) SaveContentPackRegistrySnapshot(_ context.Context, p storage.SaveContentPackRegistrySnapshotParams) (*storage.ContentPackRegistrySnapshotRecord, error) {
	record := storage.ContentPackRegistrySnapshotRecord{
		ID:                uuid.New(),
		TenantID:          p.TenantID,
		Status:            storage.ContentPackRegistrySnapshotStatusActive,
		Source:            p.Source,
		ControlOneVersion: p.Snapshot.ControlOneVersion,
		PackCount:         len(p.Snapshot.Packs),
		Snapshot:          p.Snapshot,
		CreatedAt:         p.Snapshot.ExportedAt,
		UpdatedAt:         p.Snapshot.ExportedAt,
	}
	f.active = &record
	f.saved = append(f.saved, record)
	return &record, nil
}

func (f *contentPackSnapshotFakeStore) CreateContentPackCollectorConfigCandidate(_ context.Context, p storage.CreateContentPackCollectorConfigCandidateParams) (*storage.ContentPackCollectorConfigCandidate, error) {
	now := time.Now().UTC()
	record := storage.ContentPackCollectorConfigCandidate{
		ID:                 uuid.New(),
		TenantID:           p.TenantID,
		RegistrySnapshotID: p.RegistrySnapshotID,
		Status:             storage.ContentPackCollectorConfigStatusRendered,
		ConfigVersion:      p.ConfigVersion,
		CollectorID:        strings.TrimSpace(p.CollectorID),
		Endpoint:           strings.TrimSpace(p.Endpoint),
		SourceIDs:          append([]string(nil), p.SourceIDs...),
		Plan:               p.Plan,
		RenderedYAML:       p.RenderedYAML,
		CreatedBySubject:   strings.TrimSpace(p.CreatedBySubject),
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	f.candidates = append(f.candidates, record)
	return &record, nil
}

func (f *contentPackSnapshotFakeStore) ApproveContentPackCollectorConfigCandidate(_ context.Context, p storage.ApproveContentPackCollectorConfigCandidateParams) (*storage.ContentPackCollectorConfigCandidate, error) {
	if strings.TrimSpace(p.ApprovedBySubject) == "" {
		return nil, errors.New("approved by subject is required")
	}
	reviewedConfigVersion := strings.TrimSpace(p.ReviewedConfigVersion)
	if reviewedConfigVersion == "" {
		return nil, errors.New("reviewed config version is required")
	}
	for i := range f.candidates {
		candidate := &f.candidates[i]
		if candidate.TenantID != p.TenantID || candidate.ID != p.CandidateID {
			continue
		}
		if candidate.Status != storage.ContentPackCollectorConfigStatusRendered {
			return nil, errors.New("content pack collector config candidate is not rendered or not found")
		}
		if candidate.ConfigVersion != reviewedConfigVersion {
			return nil, errors.New("content pack collector config candidate is not rendered, not found, or reviewed config version does not match")
		}
		now := time.Now().UTC()
		candidate.Status = storage.ContentPackCollectorConfigStatusApproved
		candidate.ApprovedBySubject = strings.TrimSpace(p.ApprovedBySubject)
		candidate.ApprovalNote = strings.TrimSpace(p.ApprovalNote)
		candidate.ReviewedConfigVersion = reviewedConfigVersion
		candidate.ReviewedYAMLSHA256 = strings.TrimPrefix(reviewedConfigVersion, "sha256:")
		candidate.ApprovedAt = &now
		candidate.UpdatedAt = now
		out := *candidate
		return &out, nil
	}
	return nil, errors.New("content pack collector config candidate is not rendered or not found")
}

func (f *contentPackSnapshotFakeStore) QueueContentPackCollectorConfigCandidate(_ context.Context, p storage.QueueContentPackCollectorConfigCandidateParams) (*storage.ContentPackCollectorConfigCandidate, error) {
	if strings.TrimSpace(p.QueuedBySubject) == "" {
		return nil, errors.New("queued by subject is required")
	}
	expectedConfigVersion := strings.TrimSpace(p.ExpectedConfigVersion)
	if expectedConfigVersion == "" {
		return nil, errors.New("expected config version is required")
	}
	for i := range f.candidates {
		candidate := &f.candidates[i]
		if candidate.TenantID != p.TenantID || candidate.ID != p.CandidateID {
			continue
		}
		if candidate.Status != storage.ContentPackCollectorConfigStatusApproved {
			return nil, errors.New("content pack collector config candidate is not approved or not found")
		}
		if candidate.ConfigVersion != expectedConfigVersion {
			return nil, errors.New("expected config version does not match approved candidate")
		}
		if strings.TrimSpace(candidate.ReviewedConfigVersion) != candidate.ConfigVersion {
			return nil, errors.New("approved candidate has no matching reviewed config version")
		}
		targetCollectorID := strings.TrimSpace(p.TargetCollectorID)
		if targetCollectorID == "" {
			targetCollectorID = strings.TrimSpace(candidate.CollectorID)
		}
		if targetCollectorID == "" {
			return nil, errors.New("target collector id is required")
		}
		var collector *storage.ContentPackEdgeCollector
		for j := range f.collectors {
			if f.collectors[j].TenantID == p.TenantID && f.collectors[j].CollectorID == targetCollectorID && f.collectors[j].Status != storage.ContentPackEdgeCollectorStatusDisabled {
				collector = &f.collectors[j]
				break
			}
		}
		if collector == nil {
			return nil, errors.New("target edge collector is not registered or is disabled")
		}
		now := time.Now().UTC()
		collector.DesiredConfigVersion = candidate.ConfigVersion
		collector.UpdatedAt = now
		candidate.Status = storage.ContentPackCollectorConfigStatusQueued
		candidate.TargetCollectorID = targetCollectorID
		candidate.QueuedBySubject = strings.TrimSpace(p.QueuedBySubject)
		candidate.QueueNote = strings.TrimSpace(p.QueueNote)
		candidate.QueuedAt = &now
		candidate.UpdatedAt = now
		out := *candidate
		return &out, nil
	}
	return nil, errors.New("content pack collector config candidate is not approved or not found")
}

func (f *contentPackSnapshotFakeStore) QueueContentPackCollectorConfigRollback(_ context.Context, p storage.QueueContentPackCollectorConfigRollbackParams) (*storage.ContentPackCollectorConfigCandidate, error) {
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	if strings.TrimSpace(p.QueuedBySubject) == "" {
		return nil, errors.New("queued by subject is required")
	}
	targetIndex := -1
	for i := range f.candidates {
		candidate := &f.candidates[i]
		if candidate.TenantID != p.TenantID || candidate.TargetCollectorID != collectorID || candidate.Status != storage.ContentPackCollectorConfigStatusSuperseded {
			continue
		}
		if p.CandidateID != uuid.Nil && candidate.ID != p.CandidateID {
			continue
		}
		if strings.TrimSpace(p.ConfigVersion) != "" && candidate.ConfigVersion != strings.TrimSpace(p.ConfigVersion) {
			continue
		}
		targetIndex = i
		break
	}
	if targetIndex < 0 {
		return nil, errors.New("rollback target config candidate is not superseded or not found")
	}
	var collector *storage.ContentPackEdgeCollector
	for i := range f.collectors {
		if f.collectors[i].TenantID == p.TenantID && f.collectors[i].CollectorID == collectorID && f.collectors[i].Status != storage.ContentPackEdgeCollectorStatusDisabled {
			collector = &f.collectors[i]
			break
		}
	}
	if collector == nil {
		return nil, errors.New("target edge collector is not registered or is disabled")
	}
	now := time.Now().UTC()
	candidate := &f.candidates[targetIndex]
	collector.DesiredConfigVersion = candidate.ConfigVersion
	collector.UpdatedAt = now
	candidate.Status = storage.ContentPackCollectorConfigStatusQueued
	candidate.QueuedBySubject = strings.TrimSpace(p.QueuedBySubject)
	candidate.QueueNote = strings.TrimSpace(p.QueueNote)
	candidate.QueuedAt = &now
	candidate.FailedAt = nil
	candidate.DeploymentError = ""
	candidate.UpdatedAt = now
	out := *candidate
	return &out, nil
}

func (f *contentPackSnapshotFakeStore) GetContentPackCollectorConfigCandidate(_ context.Context, id uuid.UUID) (*storage.ContentPackCollectorConfigCandidate, error) {
	for i := range f.candidates {
		if f.candidates[i].ID == id {
			out := f.candidates[i]
			return &out, nil
		}
	}
	return nil, nil
}

func (f *contentPackSnapshotFakeStore) QueuedContentPackCollectorConfigForCollector(_ context.Context, tenantID uuid.UUID, collectorID string) (*storage.ContentPackCollectorConfigCandidate, error) {
	collectorID = strings.TrimSpace(collectorID)
	for i := range f.candidates {
		candidate := f.candidates[i]
		if candidate.TenantID == tenantID && candidate.TargetCollectorID == collectorID && candidate.Status == storage.ContentPackCollectorConfigStatusQueued {
			return &candidate, nil
		}
	}
	return nil, nil
}

func (f *contentPackSnapshotFakeStore) RecordContentPackCollectorConfigApplyResult(_ context.Context, p storage.RecordContentPackCollectorConfigApplyResultParams) (*storage.ContentPackCollectorConfigCandidate, error) {
	collectorID := strings.TrimSpace(p.CollectorID)
	configVersion := strings.TrimSpace(p.ConfigVersion)
	status := strings.TrimSpace(strings.ToLower(p.Status))
	if status != storage.ContentPackCollectorConfigStatusDeployed && status != storage.ContentPackCollectorConfigStatusFailed {
		return nil, errors.New("unsupported collector config apply status")
	}
	for i := range f.candidates {
		candidate := &f.candidates[i]
		if candidate.TenantID != p.TenantID || candidate.TargetCollectorID != collectorID || candidate.ConfigVersion != configVersion || candidate.Status != storage.ContentPackCollectorConfigStatusQueued {
			continue
		}
		now := time.Now().UTC()
		candidate.Status = status
		candidate.UpdatedAt = now
		if status == storage.ContentPackCollectorConfigStatusDeployed {
			for j := range f.candidates {
				if f.candidates[j].TenantID == p.TenantID && f.candidates[j].TargetCollectorID == collectorID && f.candidates[j].ID != candidate.ID && f.candidates[j].Status == storage.ContentPackCollectorConfigStatusDeployed {
					f.candidates[j].Status = storage.ContentPackCollectorConfigStatusSuperseded
					f.candidates[j].UpdatedAt = now
				}
			}
			candidate.DeployedAt = &now
			candidate.FailedAt = nil
			candidate.DeploymentError = ""
		} else {
			candidate.FailedAt = &now
			candidate.DeploymentError = strings.TrimSpace(p.ErrorMessage)
			if candidate.DeploymentError == "" {
				candidate.DeploymentError = "collector reported config apply failure"
			}
		}
		for j := range f.collectors {
			if f.collectors[j].TenantID != p.TenantID || f.collectors[j].CollectorID != collectorID {
				continue
			}
			f.collectors[j].UpdatedAt = now
			if status == storage.ContentPackCollectorConfigStatusDeployed {
				f.collectors[j].RunningConfigVersion = configVersion
				f.collectors[j].Status = storage.ContentPackEdgeCollectorStatusHealthy
				f.collectors[j].LastError = ""
			} else {
				f.collectors[j].Status = storage.ContentPackEdgeCollectorStatusDegraded
				f.collectors[j].LastError = candidate.DeploymentError
			}
		}
		out := *candidate
		return &out, nil
	}
	return nil, errors.New("queued collector config candidate is not found")
}

func (f *contentPackSnapshotFakeStore) ListContentPackCollectorConfigCandidates(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.ContentPackCollectorConfigCandidate, int, error) {
	var filtered []storage.ContentPackCollectorConfigCandidate
	for _, candidate := range f.candidates {
		if candidate.TenantID == tenantID {
			filtered = append(filtered, candidate)
		}
	}
	total := len(filtered)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return append([]storage.ContentPackCollectorConfigCandidate(nil), filtered[offset:end]...), total, nil
}

func (f *contentPackSnapshotFakeStore) UpsertContentPackEdgeCollectorRegistration(_ context.Context, p storage.UpsertContentPackEdgeCollectorRegistrationParams) (*storage.ContentPackEdgeCollector, error) {
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	kind := strings.TrimSpace(p.Kind)
	if kind == "" {
		kind = storage.ContentPackEdgeCollectorKindOTel
	}
	now := time.Now().UTC()
	for i := range f.collectors {
		if f.collectors[i].TenantID != p.TenantID || f.collectors[i].CollectorID != collectorID {
			continue
		}
		f.collectors[i].Kind = kind
		f.collectors[i].DisplayName = strings.TrimSpace(p.DisplayName)
		f.collectors[i].Endpoint = strings.TrimSpace(p.Endpoint)
		f.collectors[i].Version = strings.TrimSpace(p.Version)
		if strings.TrimSpace(p.DesiredConfigVersion) != "" {
			f.collectors[i].DesiredConfigVersion = strings.TrimSpace(p.DesiredConfigVersion)
		}
		if f.collectors[i].LastHeartbeatAt == nil {
			f.collectors[i].Status = storage.ContentPackEdgeCollectorStatusRegistered
		}
		f.collectors[i].UpdatedAt = now
		out := f.collectors[i]
		return &out, nil
	}
	record := storage.ContentPackEdgeCollector{
		ID:                   uuid.New(),
		TenantID:             p.TenantID,
		CollectorID:          collectorID,
		Kind:                 kind,
		DisplayName:          strings.TrimSpace(p.DisplayName),
		Endpoint:             strings.TrimSpace(p.Endpoint),
		Version:              strings.TrimSpace(p.Version),
		Status:               storage.ContentPackEdgeCollectorStatusRegistered,
		DesiredConfigVersion: strings.TrimSpace(p.DesiredConfigVersion),
		Health:               map[string]any{},
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	f.collectors = append(f.collectors, record)
	return &record, nil
}

func (f *contentPackSnapshotFakeStore) RecordContentPackEdgeCollectorHeartbeat(_ context.Context, p storage.RecordContentPackEdgeCollectorHeartbeatParams) (*storage.ContentPackEdgeCollector, error) {
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	kind := strings.TrimSpace(p.Kind)
	if kind == "" {
		kind = storage.ContentPackEdgeCollectorKindOTel
	}
	status := strings.TrimSpace(p.Status)
	if status == "" {
		if strings.TrimSpace(p.LastError) != "" {
			status = storage.ContentPackEdgeCollectorStatusDegraded
		} else {
			status = storage.ContentPackEdgeCollectorStatusHealthy
		}
	}
	now := time.Now().UTC()
	for i := range f.collectors {
		if f.collectors[i].TenantID != p.TenantID || f.collectors[i].CollectorID != collectorID {
			continue
		}
		f.collectors[i].Kind = kind
		if strings.TrimSpace(p.Version) != "" {
			f.collectors[i].Version = strings.TrimSpace(p.Version)
		}
		f.collectors[i].Status = status
		if strings.TrimSpace(p.DesiredConfigVersion) != "" {
			f.collectors[i].DesiredConfigVersion = strings.TrimSpace(p.DesiredConfigVersion)
		}
		f.collectors[i].RunningConfigVersion = strings.TrimSpace(p.RunningConfigVersion)
		f.collectors[i].Health = p.Health
		if f.collectors[i].Health == nil {
			f.collectors[i].Health = map[string]any{}
		}
		f.collectors[i].LastError = strings.TrimSpace(p.LastError)
		f.collectors[i].LastHeartbeatAt = &now
		f.collectors[i].UpdatedAt = now
		out := f.collectors[i]
		return &out, nil
	}
	record := storage.ContentPackEdgeCollector{
		ID:                   uuid.New(),
		TenantID:             p.TenantID,
		CollectorID:          collectorID,
		Kind:                 kind,
		Version:              strings.TrimSpace(p.Version),
		Status:               status,
		DesiredConfigVersion: strings.TrimSpace(p.DesiredConfigVersion),
		RunningConfigVersion: strings.TrimSpace(p.RunningConfigVersion),
		Health:               p.Health,
		LastError:            strings.TrimSpace(p.LastError),
		LastHeartbeatAt:      &now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if record.Health == nil {
		record.Health = map[string]any{}
	}
	f.collectors = append(f.collectors, record)
	return &record, nil
}

func (f *contentPackSnapshotFakeStore) ListContentPackEdgeCollectors(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.ContentPackEdgeCollector, int, error) {
	var filtered []storage.ContentPackEdgeCollector
	for _, collector := range f.collectors {
		if collector.TenantID == tenantID {
			filtered = append(filtered, collector)
		}
	}
	total := len(filtered)
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return append([]storage.ContentPackEdgeCollector(nil), filtered[offset:end]...), total, nil
}

func (f *contentPackSnapshotFakeStore) UpsertContentPackSourceRuntimeState(_ context.Context, p storage.UpsertContentPackSourceRuntimeStateParams) (*storage.ContentPackSourceRuntimeStateRecord, error) {
	if p.TenantID == uuid.Nil {
		return nil, errors.New("tenant id is required")
	}
	state := p.State
	if strings.TrimSpace(state.SourceInstanceID) == "" {
		state.SourceInstanceID = strings.TrimSpace(state.SourceID)
	}
	if strings.TrimSpace(state.SourceInstanceID) == "" {
		return nil, errors.New("source instance id is required")
	}
	now := time.Now().UTC()
	for i := range f.sourceStates {
		if f.sourceStates[i].TenantID != p.TenantID || f.sourceStates[i].State.SourceInstanceID != state.SourceInstanceID {
			continue
		}
		f.sourceStates[i].State = state
		f.sourceStates[i].State.UpdatedAt = now
		f.sourceStates[i].UpdatedAt = now
		out := f.sourceStates[i]
		return &out, nil
	}
	record := storage.ContentPackSourceRuntimeStateRecord{
		ID:        uuid.New(),
		TenantID:  p.TenantID,
		State:     state,
		CreatedAt: now,
		UpdatedAt: now,
	}
	record.State.UpdatedAt = now
	f.sourceStates = append(f.sourceStates, record)
	return &record, nil
}

func (f *contentPackSnapshotFakeStore) ListContentPackSourceRuntimeStates(_ context.Context, tenantID uuid.UUID, limit, offset int) ([]storage.ContentPackSourceRuntimeStateRecord, int, error) {
	return f.ListContentPackSourceRuntimeStatesFiltered(context.Background(), tenantID, storage.ContentPackSourceRuntimeStateFilter{}, limit, offset)
}

func (f *contentPackSnapshotFakeStore) GetContentPackSourceRuntimeState(_ context.Context, id uuid.UUID) (*storage.ContentPackSourceRuntimeStateRecord, error) {
	for _, row := range f.sourceStates {
		if row.ID == id {
			out := row
			return &out, nil
		}
	}
	return nil, nil
}

func (f *contentPackSnapshotFakeStore) ListContentPackSourceRuntimeStatesFiltered(_ context.Context, tenantID uuid.UUID, filter storage.ContentPackSourceRuntimeStateFilter, limit, offset int) ([]storage.ContentPackSourceRuntimeStateRecord, int, error) {
	var filtered []storage.ContentPackSourceRuntimeStateRecord
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	for _, row := range f.sourceStates {
		if row.TenantID == tenantID &&
			contentPackTestSourceRuntimeStateMatchesQuery(row.State, query) &&
			contentPackTestSourceRuntimeStateMatchesCoverageStates(row.State, filter.CoverageStates, filter.StaleBefore) {
			filtered = append(filtered, row)
		}
	}
	total := len(filtered)
	if limit <= 0 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return nil, total, nil
	}
	end := offset + limit
	if end > len(filtered) {
		end = len(filtered)
	}
	return append([]storage.ContentPackSourceRuntimeStateRecord(nil), filtered[offset:end]...), total, nil
}

func (f *contentPackSnapshotFakeStore) ContentPackSourceRuntimeStateSummaryFiltered(_ context.Context, tenantID uuid.UUID, filter storage.ContentPackSourceRuntimeStateFilter) (storage.ContentPackSourceRuntimeStateSummary, error) {
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	summary := storage.ContentPackSourceRuntimeStateSummary{ByState: map[string]int{}}
	collectors := map[string]struct{}{}
	for _, row := range f.sourceStates {
		if row.TenantID != tenantID ||
			!contentPackTestSourceRuntimeStateMatchesQuery(row.State, query) ||
			!contentPackTestSourceRuntimeStateMatchesCoverageStates(row.State, filter.CoverageStates, filter.StaleBefore) {
			continue
		}
		effectiveState := string(contentPackTestSourceRuntimeStateEffectiveState(row.State, filter.StaleBefore))
		summary.Total++
		summary.ByState[effectiveState]++
		summary.Metrics.EventsReceived += row.State.Metrics.EventsReceived
		summary.Metrics.EventsParsed += row.State.Metrics.EventsParsed
		summary.Metrics.EventsDropped += row.State.Metrics.EventsDropped
		summary.Metrics.ParseFailures += row.State.Metrics.ParseFailures
		summary.Metrics.QueueDepth += row.State.Metrics.QueueDepth
		summary.Metrics.RetryCount += row.State.Metrics.RetryCount
		if row.State.Metrics.LagMillis > summary.Metrics.LagMillis {
			summary.Metrics.LagMillis = row.State.Metrics.LagMillis
		}
		if row.State.Metrics.CursorAgeMillis > summary.Metrics.CursorAgeMillis {
			summary.Metrics.CursorAgeMillis = row.State.Metrics.CursorAgeMillis
		}
		if collectorID := strings.TrimSpace(row.State.CollectorID); collectorID != "" {
			collectors[collectorID] = struct{}{}
		}
	}
	summary.CollectorsReporting = len(collectors)
	return summary, nil
}

func contentPackTestSourceRuntimeStateMatchesQuery(state contentpacks.SourceRuntimeState, query string) bool {
	if query == "" {
		return true
	}
	values := []string{
		state.SourceInstanceID,
		state.SourceID,
		state.DisplayName,
		state.NodeID,
		state.CollectorID,
		state.CollectorMode,
		state.ParserID,
		state.ApprovalID,
		state.ConfigVersion,
		state.ContentVersion,
		state.LastError,
	}
	for _, value := range values {
		if strings.Contains(strings.ToLower(strings.TrimSpace(value)), query) {
			return true
		}
	}
	for key, value := range state.Labels {
		if strings.Contains(strings.ToLower(strings.TrimSpace(key)), query) ||
			strings.Contains(strings.ToLower(strings.TrimSpace(value)), query) {
			return true
		}
	}
	return false
}

func contentPackTestSourceRuntimeStateMatchesCoverageStates(state contentpacks.SourceRuntimeState, states []contentpacks.CoverageState, staleBefore *time.Time) bool {
	if len(states) == 0 {
		return true
	}
	effectiveState := contentPackTestSourceRuntimeStateEffectiveState(state, staleBefore)
	for _, stateFilter := range states {
		if effectiveState == contentpacks.NormalizeCoverageState(string(stateFilter)) {
			return true
		}
	}
	return false
}

func contentPackTestSourceRuntimeStateEffectiveState(state contentpacks.SourceRuntimeState, staleBefore *time.Time) contentpacks.CoverageState {
	effectiveState := contentpacks.NormalizeCoverageState(string(state.CoverageState))
	if staleBefore != nil && state.LastHealthAt != nil && state.LastHealthAt.UTC().Before(staleBefore.UTC()) {
		effectiveState = contentpacks.CoverageState(contentpacks.CoverageStale)
	}
	return effectiveState
}

func (f *contentPackSnapshotFakeStore) RotateContentPackEdgeCollectorToken(_ context.Context, p storage.RotateContentPackEdgeCollectorTokenParams) (*storage.ContentPackEdgeCollectorToken, error) {
	collectorID := strings.TrimSpace(p.CollectorID)
	if collectorID == "" {
		return nil, errors.New("collector id is required")
	}
	now := time.Now().UTC()
	token := storage.ContentPackEdgeCollectorTokenPrefix + "test_" + collectorID
	for i := range f.collectors {
		if f.collectors[i].TenantID != p.TenantID || f.collectors[i].CollectorID != collectorID || f.collectors[i].Status == storage.ContentPackEdgeCollectorStatusDisabled {
			continue
		}
		if f.collectorTokens == nil {
			f.collectorTokens = map[string]string{}
		}
		f.collectorTokens[contentPackTestCollectorTokenKey(p.TenantID, collectorID)] = token
		f.collectors[i].TokenLastFour = token[len(token)-4:]
		f.collectors[i].TokenIssuedAt = &now
		f.collectors[i].UpdatedAt = now
		out := f.collectors[i]
		return &storage.ContentPackEdgeCollectorToken{Collector: out, Token: token}, nil
	}
	return nil, errors.New("edge collector is not registered or is disabled")
}

func (f *contentPackSnapshotFakeStore) ValidateContentPackEdgeCollectorToken(_ context.Context, tenantID uuid.UUID, collectorID, token string) (*storage.ContentPackEdgeCollector, error) {
	collectorID = strings.TrimSpace(collectorID)
	token = strings.TrimSpace(token)
	if collectorID == "" || token == "" {
		return nil, nil
	}
	if f.collectorTokens[contentPackTestCollectorTokenKey(tenantID, collectorID)] != token {
		return nil, nil
	}
	for i := range f.collectors {
		if f.collectors[i].TenantID == tenantID && f.collectors[i].CollectorID == collectorID && f.collectors[i].Status != storage.ContentPackEdgeCollectorStatusDisabled {
			out := f.collectors[i]
			return &out, nil
		}
	}
	return nil, nil
}

func contentPackTestCollectorTokenKey(tenantID uuid.UUID, collectorID string) string {
	return tenantID.String() + "/" + strings.TrimSpace(collectorID)
}

func writeServerActiveContentPack(t *testing.T, root string, badGolden bool) {
	t.Helper()
	dir := filepath.Join(root, "active", offlinebundle.ContentTypeSIEMContentPack)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create active pack dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "controlone-server-replay.c1pack"), makeServerContentPackArchive(t, badGolden), 0o644); err != nil {
		t.Fatalf("write active content pack: %v", err)
	}
}

func makeServerContentPackArchive(t *testing.T, badGolden bool) []byte {
	t.Helper()
	manifestBytes, err := json.Marshal(serverContentPackManifest())
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	golden := []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":200,"user":"alice"}}` + "\n")
	if badGolden {
		golden = []byte(`{"status":"parsed","fields":{"event":{"kind":"event"},"status":403,"user":"alice"}}` + "\n")
	}
	files := map[string][]byte{
		"manifest.yaml":             manifestBytes,
		"samples/test.jsonl":        []byte(`{"raw":"{\"status\":200,\"user\":\"alice\"}"}` + "\n"),
		"samples/test.golden.jsonl": golden,
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func serverContentPackManifest() contentpacks.Manifest {
	return contentpacks.Manifest{
		SchemaVersion: contentpacks.SchemaVersion,
		PackID:        "controlone.server_pack",
		PackVersion:   "1.0.0",
		DisplayName:   "Server Content Pack Test",
		License: contentpacks.LicenseMetadata{
			SPDX: "Apache-2.0",
		},
		Provenance: contentpacks.Provenance{
			Author: "Control One",
		},
		Sources: []contentpacks.SourceProfile{{
			SourceID:        "controlone.server_pack",
			DisplayName:     "Control One Server Pack",
			Product:         "control_one",
			SourceClass:     "test",
			RiskClass:       contentpacks.RiskLow,
			DataSensitivity: contentpacks.SensitivityLow,
			CollectorModes:  []string{contentpacks.CollectorNodeFileLog, contentpacks.CollectorOTelFileLog},
			CollectorRecipes: []contentpacks.CollectorRecipe{{
				Mode:     contentpacks.CollectorOTelFileLog,
				Receiver: "filelog",
				Config: map[string]any{
					"include": []string{"/var/log/controlone/server-pack.log"},
				},
			}},
			Schemas: contentpacks.SchemaBinding{
				Primary: contentpacks.SchemaOCSF,
				OCSF: contentpacks.OCSFBinding{
					Category: "application_activity",
					Class:    "application_event",
				},
			},
			Parsers: []string{"controlone.server_pack.json"},
			Samples: []string{"controlone.server_pack.good"},
		}},
		Parsers: []contentpacks.ParserProfile{{
			ParserID:    "controlone.server_pack.json",
			DisplayName: "Control One Server Pack JSON",
			Version:     "1.0.0",
			Stages: []contentpacks.ParserStage{
				{Type: contentpacks.StageJSON},
				{Type: contentpacks.StageFieldMap, Config: map[string]any{
					"set": map[string]any{"event.kind": "event"},
				}},
			},
		}},
		Samples: []contentpacks.SampleCase{{
			CaseID:     "controlone.server_pack.good",
			SourceID:   "controlone.server_pack",
			ParserID:   "controlone.server_pack.json",
			InputPath:  "samples/test.jsonl",
			GoldenPath: "samples/test.golden.jsonl",
		}},
	}
}
