package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
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

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
)

func TestHandleContentPackDetectionsListsLoadedRuntimeRules(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	srv := &Server{
		store: &contentPackSnapshotFakeStore{
			fakeStore: &fakeStore{},
			active: &storage.ContentPackRegistrySnapshotRecord{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Status:    storage.ContentPackRegistrySnapshotStatusActive,
				Source:    "test",
				Snapshot:  registry.Snapshot(now),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/detections?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	srv.handleContentPackSubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackDetectionListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.TenantID != tenantID.String() || resp.SnapshotID == "" {
		t.Fatalf("response metadata = %#v", resp)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1: %#v", len(resp.Items), resp.Items)
	}
	item := resp.Items[0]
	if item.SourceID != "windows.sysmon" || item.DetectionID != "windows.powershell.encoded" || item.LoadStatus != "loaded" {
		t.Fatalf("item = %#v", item)
	}
	if item.Severity != "high" || item.RiskScore != 83 || item.Title != "Encoded PowerShell" || item.LogSource == nil || item.LogSource.Product != "windows" {
		t.Fatalf("item metadata = %#v", item)
	}
	if resp.Totals.Loaded != 1 || resp.Totals.Detections != 1 || resp.Totals.ByLoadStatus["loaded"] != 1 || resp.Totals.BySeverity["high"] != 1 {
		t.Fatalf("totals = %#v", resp.Totals)
	}
}

func TestHandleContentPackDetectionReplayReturnsReports(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	srv := &Server{
		store: &contentPackSnapshotFakeStore{
			fakeStore: &fakeStore{},
			active: &storage.ContentPackRegistrySnapshotRecord{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Status:    storage.ContentPackRegistrySnapshotStatusActive,
				Source:    "test",
				Snapshot:  registry.Snapshot(now),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/detections/replay?tenant_id="+tenantID.String(), nil)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()

	srv.handleContentPackSubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackDetectionReplayResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1: %#v", len(resp.Items), resp.Items)
	}
	report := resp.Items[0].Report
	if !report.Passed() || report.TotalRules != 1 || report.TotalCases != 1 || report.TotalEvents != 1 || report.TotalMatches != 1 {
		t.Fatalf("report = %#v", report)
	}
	if resp.Totals.PassedPacks != 1 || resp.Totals.FailedPacks != 0 || resp.Totals.TotalMatches != 1 {
		t.Fatalf("totals = %#v", resp.Totals)
	}
}

func TestHandleContentPackLifecycleEnableReplaysAndAudits(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	activeSnapshotID := uuid.New()
	store := &contentPackSnapshotFakeStore{
		fakeStore: &fakeStore{},
		active: &storage.ContentPackRegistrySnapshotRecord{
			ID:        activeSnapshotID,
			TenantID:  tenantID,
			Status:    storage.ContentPackRegistrySnapshotStatusActive,
			Source:    "test",
			Snapshot:  registry.Snapshot(now),
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	srv := &Server{
		store:              store,
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}
	body := bytes.NewBufferString(`{
		"pack_id":"controlone.live_detection_test",
		"pack_version":"1.0.0",
		"action":"enable",
		"expected_snapshot_id":"` + activeSnapshotID.String() + `",
		"note":"pilot approval"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/lifecycle?tenant_id="+tenantID.String(), body)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "admin", Roles: []string{roleAdmin}})
	rec := httptest.NewRecorder()

	srv.handleContentPackSubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp contentPackLifecycleResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Action != "enable" || resp.Pack.Status != string(contentpacks.PackStatusEnabled) || resp.DetectionReplay == nil || !resp.DetectionReplay.Passed() {
		t.Fatalf("response = %#v", resp)
	}
	if store.active == nil || store.active.ID == activeSnapshotID || len(store.saved) != 1 {
		t.Fatalf("snapshot store active=%#v saved=%d", store.active, len(store.saved))
	}
	enabled := false
	for _, pack := range store.active.Snapshot.Packs {
		if pack.PackID == manifest.PackID && pack.PackVersion == manifest.PackVersion && pack.Status == contentpacks.PackStatusEnabled {
			enabled = true
		}
	}
	if !enabled {
		t.Fatalf("active snapshot did not enable pack: %#v", store.active.Snapshot.Packs)
	}
	if len(store.auditLogs) != 1 || store.auditLogs[0].Action != "content_pack.pack.enable" {
		t.Fatalf("audit logs = %#v", store.auditLogs)
	}
}

func TestHandleContentPackDetectionOverrideSuppressesAndAudits(t *testing.T) {
	tenantID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	store := &contentPackSnapshotFakeStore{
		fakeStore: &fakeStore{},
		active: &storage.ContentPackRegistrySnapshotRecord{
			ID:        uuid.New(),
			TenantID:  tenantID,
			Status:    storage.ContentPackRegistrySnapshotStatusActive,
			Source:    "test",
			Snapshot:  registry.Snapshot(now),
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
	srv := &Server{
		store:              store,
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}
	suppressUntil := time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)
	body := bytes.NewBufferString(`{
		"pack_id":"controlone.live_detection_test",
		"pack_version":"1.0.0",
		"source_id":"windows.sysmon",
		"detection_id":"windows.powershell.encoded",
		"state":"suppressed",
		"suppress_until":"` + suppressUntil + `",
		"reason":"pilot tuning"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/content-packs/detections/overrides?tenant_id="+tenantID.String(), body)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "admin", Roles: []string{roleAdmin}})
	rec := httptest.NewRecorder()

	srv.handleContentPackSubroutes(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var override contentPackDetectionOverrideDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &override); err != nil {
		t.Fatalf("decode override: %v", err)
	}
	if override.State != storage.ContentPackDetectionOverrideStateSuppressed || override.SuppressUntil == "" {
		t.Fatalf("override = %#v", override)
	}
	if len(store.auditLogs) != 1 || store.auditLogs[0].Action != "content_pack.detection_override.suppressed" {
		t.Fatalf("audit logs = %#v", store.auditLogs)
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/content-packs/detections?tenant_id="+tenantID.String(), nil)
	listReq = withPrincipal(listReq, &auth.Principal{Type: "user", Subject: "viewer", Roles: []string{roleViewer}})
	listRec := httptest.NewRecorder()
	srv.handleContentPackSubroutes(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", listRec.Code, listRec.Body.String())
	}
	var list contentPackDetectionListResponse
	if err := json.Unmarshal(listRec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 1 || list.Items[0].EffectiveState != storage.ContentPackDetectionOverrideStateSuppressed || list.Items[0].Override == nil {
		t.Fatalf("list = %#v", list)
	}
	if list.Totals.Suppressed != 1 || list.Totals.ByState[storage.ContentPackDetectionOverrideStateSuppressed] != 1 {
		t.Fatalf("totals = %#v", list.Totals)
	}
}

func TestEvaluateContentPackDetectionsHonorsDisabledOverride(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	store := &contentPackDetectionAlertStore{
		contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{
			fakeStore: &fakeStore{},
			active: &storage.ContentPackRegistrySnapshotRecord{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Status:    storage.ContentPackRegistrySnapshotStatusActive,
				Source:    "test",
				Snapshot:  registry.Snapshot(now),
				CreatedAt: now,
				UpdatedAt: now,
			},
			detectionOverrides: []storage.ContentPackDetectionOverride{{
				ID:          uuid.New(),
				TenantID:    tenantID,
				PackID:      manifest.PackID,
				PackVersion: manifest.PackVersion,
				DetectionID: "windows.powershell.encoded",
				State:       storage.ContentPackDetectionOverrideStateDisabled,
				Reason:      "pilot tuning",
				CreatedAt:   now,
				UpdatedAt:   now,
			}},
		},
	}
	srv := &Server{
		store:              store,
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}
	event := IngestedEvent{
		Type:    "log.line",
		TS:      now,
		Message: "sysmon process creation",
		Details: map[string]any{
			"labels": map[string]string{
				"control_one.content_pack_source_id": "windows.sysmon",
			},
			"fields": map[string]any{
				"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				"process.command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
				"event.kind":           "event",
			},
		},
	}
	normalizeIngestedEventMetadata(&event, tenantID, nodeID)

	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{event})

	if len(store.createdAlerts) != 0 {
		t.Fatalf("created alerts = %d, want suppressed by override", len(store.createdAlerts))
	}
}

func TestEvaluateContentPackDetectionsTemporalThreshold(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	manifest.Detections[0].Temporal = &contentpacks.DetectionTemporal{
		Kind:               "threshold",
		WindowSeconds:      60,
		Threshold:          2,
		GroupBy:            []string{"user.name"},
		SuppressForSeconds: 120,
	}
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	store := &contentPackDetectionAlertStore{
		contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{
			fakeStore: &fakeStore{},
			active: &storage.ContentPackRegistrySnapshotRecord{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Status:    storage.ContentPackRegistrySnapshotStatusActive,
				Source:    "test",
				Snapshot:  registry.Snapshot(now),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	srv := &Server{
		store:              store,
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}
	newEvent := func(ts time.Time) IngestedEvent {
		event := IngestedEvent{
			Type:    "log.line",
			TS:      ts,
			Message: "sysmon process creation " + ts.Format(time.RFC3339Nano),
			Details: map[string]any{
				"labels": map[string]string{
					"control_one.content_pack_source_id": "windows.sysmon",
				},
				"fields": map[string]any{
					"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
					"process.command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
					"user.name":            "alice",
					"event.kind":           "event",
				},
			},
		}
		normalizeIngestedEventMetadata(&event, tenantID, nodeID)
		return event
	}

	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{newEvent(now)})
	if len(store.createdAlerts) != 0 {
		t.Fatalf("created alerts = %d, want first threshold hit to stay quiet", len(store.createdAlerts))
	}
	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{newEvent(now.Add(10 * time.Second))})
	if len(store.createdAlerts) != 1 {
		t.Fatalf("created alerts = %d, want threshold alert", len(store.createdAlerts))
	}
	temporal, ok := store.createdAlerts[0].Context["temporal"].(map[string]any)
	if !ok {
		t.Fatalf("temporal context = %#v", store.createdAlerts[0].Context["temporal"])
	}
	if temporal["count"] != 2 || temporal["threshold"] != 2 || temporal["window_seconds"] != 60 || temporal["group_key"] != "user.name=alice" {
		t.Fatalf("temporal context = %#v", temporal)
	}

	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{newEvent(now.Add(20 * time.Second))})
	if len(store.createdAlerts) != 1 {
		t.Fatalf("created alerts = %d, want third hit suppressed by temporal window", len(store.createdAlerts))
	}
}

func TestActiveContentPackDetectionRulesPersistAndFallback(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	store := &contentPackDetectionAlertStore{
		contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{
			fakeStore: &fakeStore{},
			active: &storage.ContentPackRegistrySnapshotRecord{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Status:    storage.ContentPackRegistrySnapshotStatusActive,
				Source:    "test",
				Snapshot:  registry.Snapshot(now),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	srv := &Server{
		store:              store,
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}
	rules, err := srv.activeContentPackDetectionRules(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("activeContentPackDetectionRules() error = %v", err)
	}
	if len(rules["windows.sysmon"]) != 1 || len(store.detectionArtifacts) != 1 {
		t.Fatalf("rules=%#v artifacts=%#v", rules, store.detectionArtifacts)
	}

	srv.offlineContentRoot = ""
	event := IngestedEvent{
		Type:    "log.line",
		TS:      now,
		Message: "sysmon process creation",
		Details: map[string]any{
			"labels": map[string]string{
				"control_one.content_pack_source_id": "windows.sysmon",
			},
			"fields": map[string]any{
				"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				"process.command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
				"event.kind":           "event",
			},
		},
	}
	normalizeIngestedEventMetadata(&event, tenantID, nodeID)

	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{event})

	if len(store.createdAlerts) != 1 {
		t.Fatalf("created alerts = %d, want persisted artifact fallback alert", len(store.createdAlerts))
	}
}

func TestEvaluateContentPackDetectionsCreatesDedupedAlert(t *testing.T) {
	tenantID := uuid.New()
	nodeID := uuid.New()
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	manifest := liveDetectionTestManifest()
	root := t.TempDir()
	writeActiveDetectionPack(t, root, manifest)

	registry := contentpacks.NewRegistry("1.0.0")
	if _, err := registry.Install(manifest, now); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if _, err := registry.Enable(manifest.PackID, manifest.PackVersion, now); err != nil {
		t.Fatalf("Enable() error = %v", err)
	}
	store := &contentPackDetectionAlertStore{
		contentPackSnapshotFakeStore: &contentPackSnapshotFakeStore{
			fakeStore: &fakeStore{},
			active: &storage.ContentPackRegistrySnapshotRecord{
				ID:        uuid.New(),
				TenantID:  tenantID,
				Status:    storage.ContentPackRegistrySnapshotStatusActive,
				Source:    "test",
				Snapshot:  registry.Snapshot(now),
				CreatedAt: now,
				UpdatedAt: now,
			},
		},
	}
	srv := &Server{
		store:              store,
		logger:             zap.NewNop(),
		offlineContentRoot: root,
	}
	event := IngestedEvent{
		Type:    "log.line",
		TS:      now,
		Message: "sysmon process creation",
		Details: map[string]any{
			"labels": map[string]string{
				"control_one.content_pack_source_id": "windows.sysmon",
			},
			"fields": map[string]any{
				"process.executable":   `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
				"process.command_line": "powershell.exe -NoProfile -enc SQBFAFgA",
				"event.kind":           "event",
			},
		},
	}
	normalizeIngestedEventMetadata(&event, tenantID, nodeID)

	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{event})
	srv.evaluateContentPackDetections(context.Background(), tenantID, nodeID, []IngestedEvent{event})

	if len(store.createdAlerts) != 1 {
		t.Fatalf("created alerts = %d, want 1 deduped alert", len(store.createdAlerts))
	}
	alert := store.createdAlerts[0]
	if alert.Source != "content_pack_detection" || alert.Severity != "high" || alert.Title != "Encoded PowerShell" {
		t.Fatalf("alert = %#v", alert)
	}
	if !strings.Contains(alert.DedupKey, event.EventID) {
		t.Fatalf("dedup key = %q, want event id %q", alert.DedupKey, event.EventID)
	}
	if alert.Context["detection_id"] != "windows.powershell.encoded" || alert.Context["source_id"] != "windows.sysmon" {
		t.Fatalf("context = %#v", alert.Context)
	}
	if alert.Context["risk_score"] != 83 {
		t.Fatalf("risk score context = %#v, want 83", alert.Context["risk_score"])
	}
	normalized, ok := alert.Context["normalized"].(map[string]any)
	if !ok || normalized["process.executable"] == nil {
		t.Fatalf("normalized context = %#v", alert.Context["normalized"])
	}
}

type contentPackDetectionAlertStore struct {
	*contentPackSnapshotFakeStore
	createdAlerts []storage.CreateAlertParams
	alertsByDedup map[string]storage.Alert
}

func (s *contentPackDetectionAlertStore) CreateAlert(_ context.Context, p storage.CreateAlertParams) (*storage.Alert, error) {
	if s.alertsByDedup == nil {
		s.alertsByDedup = map[string]storage.Alert{}
	}
	if strings.TrimSpace(p.DedupKey) != "" {
		if existing, ok := s.alertsByDedup[p.DedupKey]; ok {
			return &existing, storage.ErrAlertDeduped
		}
	}
	alert := alertFromCreateAlertParams(p)
	s.createdAlerts = append(s.createdAlerts, p)
	s.alertsByDedup[p.DedupKey] = *alert
	return alert, nil
}

func (s *contentPackSnapshotFakeStore) UpsertContentPackDetectionOverride(_ context.Context, p storage.UpsertContentPackDetectionOverrideParams) (*storage.ContentPackDetectionOverride, error) {
	now := time.Now().UTC()
	state := strings.ToLower(strings.TrimSpace(p.State))
	if state == "" {
		return nil, errors.New("state is required")
	}
	if p.TenantID == uuid.Nil || strings.TrimSpace(p.PackID) == "" || strings.TrimSpace(p.PackVersion) == "" || strings.TrimSpace(p.DetectionID) == "" {
		return nil, errors.New("tenant_id, pack_id, pack_version, and detection_id are required")
	}
	if state == storage.ContentPackDetectionOverrideStateSuppressed {
		if p.SuppressUntil == nil || !p.SuppressUntil.After(now) {
			return nil, errors.New("suppress_until must be in the future")
		}
	} else {
		p.SuppressUntil = nil
	}
	record := storage.ContentPackDetectionOverride{
		ID:               uuid.New(),
		TenantID:         p.TenantID,
		PackID:           strings.TrimSpace(p.PackID),
		PackVersion:      strings.TrimSpace(p.PackVersion),
		SourceID:         strings.TrimSpace(p.SourceID),
		DetectionID:      strings.TrimSpace(p.DetectionID),
		State:            state,
		SuppressUntil:    p.SuppressUntil,
		Reason:           strings.TrimSpace(p.Reason),
		CreatedBySubject: strings.TrimSpace(p.UpdatedBySubject),
		UpdatedBySubject: strings.TrimSpace(p.UpdatedBySubject),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	for i := range s.detectionOverrides {
		existing := s.detectionOverrides[i]
		if existing.TenantID == record.TenantID && existing.PackID == record.PackID && existing.PackVersion == record.PackVersion && existing.SourceID == record.SourceID && existing.DetectionID == record.DetectionID {
			record.ID = existing.ID
			record.CreatedAt = existing.CreatedAt
			record.CreatedBySubject = existing.CreatedBySubject
			s.detectionOverrides[i] = record
			return &s.detectionOverrides[i], nil
		}
	}
	s.detectionOverrides = append(s.detectionOverrides, record)
	return &s.detectionOverrides[len(s.detectionOverrides)-1], nil
}

func (s *contentPackSnapshotFakeStore) ListContentPackDetectionOverrides(_ context.Context, tenantID uuid.UUID, filter storage.ContentPackDetectionOverrideFilter, limit, offset int) ([]storage.ContentPackDetectionOverride, int, error) {
	now := time.Now().UTC()
	var filtered []storage.ContentPackDetectionOverride
	for _, row := range s.detectionOverrides {
		if row.TenantID != tenantID {
			continue
		}
		if strings.TrimSpace(filter.PackID) != "" && row.PackID != strings.TrimSpace(filter.PackID) {
			continue
		}
		if strings.TrimSpace(filter.PackVersion) != "" && row.PackVersion != strings.TrimSpace(filter.PackVersion) {
			continue
		}
		if strings.TrimSpace(filter.SourceID) != "" && row.SourceID != strings.TrimSpace(filter.SourceID) {
			continue
		}
		if strings.TrimSpace(filter.DetectionID) != "" && row.DetectionID != strings.TrimSpace(filter.DetectionID) {
			continue
		}
		if strings.TrimSpace(filter.State) != "" && row.State != strings.TrimSpace(filter.State) {
			continue
		}
		if !filter.IncludeExpired && row.State == storage.ContentPackDetectionOverrideStateSuppressed && (row.SuppressUntil == nil || !row.SuppressUntil.After(now)) {
			continue
		}
		filtered = append(filtered, row)
	}
	total := len(filtered)
	if limit <= 0 {
		limit = total
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []storage.ContentPackDetectionOverride{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return append([]storage.ContentPackDetectionOverride(nil), filtered[offset:end]...), total, nil
}

func (s *contentPackSnapshotFakeStore) ReplaceContentPackDetectionArtifacts(_ context.Context, p storage.ReplaceContentPackDetectionArtifactsParams) error {
	if p.TenantID == uuid.Nil || p.RegistrySnapshotID == uuid.Nil {
		return errors.New("tenant_id and registry_snapshot_id are required")
	}
	filtered := s.detectionArtifacts[:0]
	for _, existing := range s.detectionArtifacts {
		if existing.TenantID == p.TenantID && existing.RegistrySnapshotID == p.RegistrySnapshotID {
			continue
		}
		filtered = append(filtered, existing)
	}
	s.detectionArtifacts = filtered
	now := time.Now().UTC()
	for _, artifact := range p.Artifacts {
		if err := artifact.Rule.Validate(); err != nil {
			return err
		}
		artifact.ID = uuid.New()
		artifact.TenantID = p.TenantID
		artifact.RegistrySnapshotID = p.RegistrySnapshotID
		if artifact.LoadedAt.IsZero() {
			artifact.LoadedAt = now
		}
		artifact.CreatedAt = now
		artifact.UpdatedAt = now
		s.detectionArtifacts = append(s.detectionArtifacts, artifact)
	}
	return nil
}

func (s *contentPackSnapshotFakeStore) ListContentPackDetectionArtifacts(_ context.Context, tenantID, registrySnapshotID uuid.UUID) ([]storage.ContentPackDetectionArtifact, error) {
	var out []storage.ContentPackDetectionArtifact
	for _, artifact := range s.detectionArtifacts {
		if artifact.TenantID == tenantID && artifact.RegistrySnapshotID == registrySnapshotID {
			out = append(out, artifact)
		}
	}
	return append([]storage.ContentPackDetectionArtifact(nil), out...), nil
}

func alertFromCreateAlertParams(p storage.CreateAlertParams) *storage.Alert {
	alert := &storage.Alert{
		ID:       uuid.New(),
		TenantID: p.TenantID,
		Source:   p.Source,
		Severity: p.Severity,
		Title:    p.Title,
		State:    "open",
		Context:  p.Context,
		OpenedAt: time.Now().UTC(),
	}
	if p.NodeID != nil {
		alert.NodeID = uuid.NullUUID{UUID: *p.NodeID, Valid: true}
	}
	if strings.TrimSpace(p.Summary) != "" {
		alert.Summary = sql.NullString{String: p.Summary, Valid: true}
	}
	if strings.TrimSpace(p.DedupKey) != "" {
		alert.DedupKey = sql.NullString{String: p.DedupKey, Valid: true}
	}
	return alert
}

func liveDetectionTestManifest() contentpacks.Manifest {
	return contentpacks.Manifest{
		SchemaVersion: contentpacks.SchemaVersion,
		PackID:        "controlone.live_detection_test",
		PackVersion:   "1.0.0",
		DisplayName:   "Live Detection Test",
		License:       contentpacks.LicenseMetadata{SPDX: "Apache-2.0"},
		Provenance:    contentpacks.Provenance{Author: "Control One"},
		Sources: []contentpacks.SourceProfile{{
			SourceID:         "windows.sysmon",
			DisplayName:      "Windows Sysmon",
			Product:          "sysmon",
			SourceClass:      "endpoint",
			RiskClass:        contentpacks.RiskMedium,
			DataSensitivity:  contentpacks.SensitivityHigh,
			CollectorModes:   []string{contentpacks.CollectorWindowsEvent},
			ApprovalRequired: true,
			Schemas: contentpacks.SchemaBinding{
				Primary: contentpacks.SchemaOCSF,
				OCSF:    contentpacks.OCSFBinding{Category: "system_activity", Class: "process_activity"},
			},
			Parsers:    []string{"windows.sysmon.json"},
			Detections: []string{"windows.powershell.encoded"},
			Samples:    []string{"windows.sysmon.good"},
		}},
		Parsers: []contentpacks.ParserProfile{{
			ParserID:    "windows.sysmon.json",
			DisplayName: "Windows Sysmon JSON",
			Version:     "1.0.0",
			Stages:      []contentpacks.ParserStage{{Type: contentpacks.StageJSON}},
		}},
		Detections: []contentpacks.Detection{{
			DetectionID: "windows.powershell.encoded",
			Title:       "Encoded PowerShell",
			Kind:        contentpacks.DetectionKindSigma,
			Path:        "detections/powershell.yml",
			Severity:    "high",
			RiskScore:   83,
			Tags:        []string{"attack.t1059.001"},
		}},
		Samples: []contentpacks.SampleCase{{
			CaseID:     "windows.sysmon.good",
			SourceID:   "windows.sysmon",
			ParserID:   "windows.sysmon.json",
			InputPath:  "samples/sysmon.jsonl",
			GoldenPath: "samples/sysmon.golden.jsonl",
		}},
	}
}

func writeActiveDetectionPack(t *testing.T, root string, manifest contentpacks.Manifest) {
	t.Helper()
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	files := map[string][]byte{
		"manifest.json": manifestBytes,
		"detections/powershell.yml": []byte(`
title: Encoded PowerShell
logsource:
  product: windows
  category: process_creation
detection:
  selection:
    Image|endswith: '\powershell.exe'
    CommandLine|contains: '-enc'
  condition: selection
level: high
`),
		"samples/sysmon.jsonl":        []byte(`{"Image":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","CommandLine":"powershell.exe -NoProfile -enc SQBFAFgA"}` + "\n"),
		"samples/sysmon.golden.jsonl": []byte(`{"parser_id":"windows.sysmon.json","status":"parsed","fields":{"process.executable":"C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe","process.command_line":"powershell.exe -NoProfile -enc SQBFAFgA","event.kind":"event"}}` + "\n"),
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatalf("write tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	activeDir := filepath.Join(root, "active", "siem_content_pack")
	if err := os.MkdirAll(activeDir, 0o755); err != nil {
		t.Fatalf("mkdir active content dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "controlone-live-detection.c1pack"), buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write active content pack: %v", err)
	}
}
