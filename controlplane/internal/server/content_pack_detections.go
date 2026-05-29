package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/internal/contentpacks"
	"github.com/CloudSpaceLab/control_one/internal/detections"
)

const contentPackDetectionCacheTTL = 5 * time.Minute

type contentPackDetectionOverrideStore interface {
	UpsertContentPackDetectionOverride(context.Context, storage.UpsertContentPackDetectionOverrideParams) (*storage.ContentPackDetectionOverride, error)
	ListContentPackDetectionOverrides(context.Context, uuid.UUID, storage.ContentPackDetectionOverrideFilter, int, int) ([]storage.ContentPackDetectionOverride, int, error)
}

type contentPackDetectionArtifactStore interface {
	ReplaceContentPackDetectionArtifacts(context.Context, storage.ReplaceContentPackDetectionArtifactsParams) error
	ListContentPackDetectionArtifacts(context.Context, uuid.UUID, uuid.UUID) ([]storage.ContentPackDetectionArtifact, error)
}

type contentPackDetectionRule struct {
	PackID      string
	PackVersion string
	SourceID    string
	Detection   contentpacks.Detection
	Rule        detections.Rule
}

type contentPackDetectionCacheEntry struct {
	SnapshotID    uuid.UUID
	Root          string
	LoadedAt      time.Time
	RulesBySource map[string][]contentPackDetectionRule
}

type contentPackDetectionListResponse struct {
	TenantID          string                     `json:"tenant_id"`
	GeneratedAt       string                     `json:"generated_at"`
	Source            string                     `json:"source"`
	SnapshotID        string                     `json:"snapshot_id,omitempty"`
	SnapshotCreatedAt string                     `json:"snapshot_created_at,omitempty"`
	Items             []contentPackDetectionDTO  `json:"items"`
	Totals            contentPackDetectionTotals `json:"totals"`
}

type contentPackDetectionDTO struct {
	PackID            string                            `json:"pack_id"`
	PackVersion       string                            `json:"pack_version"`
	PackDisplayName   string                            `json:"pack_display_name,omitempty"`
	ContentStatus     string                            `json:"content_status"`
	SourceID          string                            `json:"source_id"`
	SourceDisplayName string                            `json:"source_display_name,omitempty"`
	DetectionID       string                            `json:"detection_id"`
	Title             string                            `json:"title,omitempty"`
	Kind              string                            `json:"kind,omitempty"`
	Severity          string                            `json:"severity,omitempty"`
	RiskScore         int                               `json:"risk_score,omitempty"`
	Tags              []string                          `json:"tags,omitempty"`
	Path              string                            `json:"path,omitempty"`
	LoadStatus        string                            `json:"load_status"`
	LoadError         string                            `json:"load_error,omitempty"`
	LogSource         *contentPackDetectionLogSourceDTO `json:"log_source,omitempty"`
	EffectiveState    string                            `json:"effective_state"`
	Override          *contentPackDetectionOverrideDTO  `json:"override,omitempty"`
	Temporal          *contentPackDetectionTemporalDTO  `json:"temporal,omitempty"`
}

type contentPackDetectionLogSourceDTO struct {
	Product  string `json:"product,omitempty"`
	Service  string `json:"service,omitempty"`
	Category string `json:"category,omitempty"`
}

type contentPackDetectionTotals struct {
	Detections   int            `json:"detections"`
	Loaded       int            `json:"loaded"`
	MetadataOnly int            `json:"metadata_only"`
	Inactive     int            `json:"inactive"`
	Errors       int            `json:"errors"`
	Disabled     int            `json:"disabled"`
	Suppressed   int            `json:"suppressed"`
	ByLoadStatus map[string]int `json:"by_load_status"`
	BySeverity   map[string]int `json:"by_severity,omitempty"`
	ByState      map[string]int `json:"by_state,omitempty"`
}

type contentPackDetectionOverrideDTO struct {
	ID               string `json:"id,omitempty"`
	PackID           string `json:"pack_id"`
	PackVersion      string `json:"pack_version"`
	SourceID         string `json:"source_id,omitempty"`
	DetectionID      string `json:"detection_id"`
	State            string `json:"state"`
	SuppressUntil    string `json:"suppress_until,omitempty"`
	Reason           string `json:"reason,omitempty"`
	CreatedBySubject string `json:"created_by_subject,omitempty"`
	UpdatedBySubject string `json:"updated_by_subject,omitempty"`
	CreatedAt        string `json:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty"`
}

type contentPackDetectionOverrideListResponse struct {
	TenantID    string                            `json:"tenant_id"`
	GeneratedAt string                            `json:"generated_at"`
	Items       []contentPackDetectionOverrideDTO `json:"items"`
	Total       int                               `json:"total"`
}

type contentPackDetectionOverrideRequest struct {
	PackID        string `json:"pack_id"`
	PackVersion   string `json:"pack_version"`
	SourceID      string `json:"source_id,omitempty"`
	DetectionID   string `json:"detection_id"`
	State         string `json:"state"`
	SuppressUntil string `json:"suppress_until,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type contentPackDetectionTemporalDTO struct {
	Kind               string                                `json:"kind"`
	WindowSeconds      int                                   `json:"window_seconds,omitempty"`
	Threshold          int                                   `json:"threshold,omitempty"`
	GroupBy            []string                              `json:"group_by,omitempty"`
	SuppressForSeconds int                                   `json:"suppress_for_seconds,omitempty"`
	Sequence           []contentPackDetectionTemporalStepDTO `json:"sequence,omitempty"`
	Join               []contentPackDetectionTemporalStepDTO `json:"join,omitempty"`
}

type contentPackDetectionTemporalStepDTO struct {
	Field         string `json:"field"`
	Op            string `json:"op,omitempty"`
	Values        []any  `json:"values,omitempty"`
	CaseSensitive bool   `json:"case_sensitive,omitempty"`
}

type contentPackDetectionReplayResponse struct {
	TenantID          string                           `json:"tenant_id"`
	GeneratedAt       string                           `json:"generated_at"`
	Source            string                           `json:"source"`
	SnapshotID        string                           `json:"snapshot_id,omitempty"`
	SnapshotCreatedAt string                           `json:"snapshot_created_at,omitempty"`
	Items             []contentPackDetectionReplayDTO  `json:"items"`
	Totals            contentPackDetectionReplayTotals `json:"totals"`
}

type contentPackDetectionReplayDTO struct {
	PackID          string                             `json:"pack_id"`
	PackVersion     string                             `json:"pack_version"`
	PackDisplayName string                             `json:"pack_display_name,omitempty"`
	ContentStatus   string                             `json:"content_status"`
	Receipt         *contentPackDetectionReplayReceipt `json:"receipt,omitempty"`
	Report          contentpacks.DetectionReplayReport `json:"report"`
}

type contentPackDetectionReplayReceipt struct {
	BundleID       string `json:"bundle_id,omitempty"`
	BundleVersion  string `json:"bundle_version,omitempty"`
	BundleSequence int64  `json:"bundle_sequence,omitempty"`
	SHA256         string `json:"sha256,omitempty"`
	Stale          bool   `json:"stale,omitempty"`
}

type contentPackDetectionReplayTotals struct {
	Packs            int `json:"packs"`
	EnabledPacks     int `json:"enabled_packs"`
	PassedPacks      int `json:"passed_packs"`
	FailedPacks      int `json:"failed_packs"`
	TotalRules       int `json:"total_rules"`
	TotalCases       int `json:"total_cases"`
	TotalEvents      int `json:"total_events"`
	TotalEvaluations int `json:"total_evaluations"`
	TotalMatches     int `json:"total_matches"`
}

func (s *Server) handleContentPackDetections(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	resp, err := s.activeContentPackDetectionListResponse(r.Context(), tenantID)
	if err != nil {
		logger := s.logger
		if logger == nil {
			logger = zap.NewNop()
		}
		logger.Warn("load active content pack detection list", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContentPackDetectionReplay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleViewer)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	root := strings.TrimSpace(s.offlineContentRootDir())
	if root == "" {
		http.Error(w, "offline content root unavailable", http.StatusServiceUnavailable)
		return
	}
	resp, err := s.activeContentPackDetectionReplayResponse(r.Context(), tenantID, root)
	if err != nil {
		logger := s.logger
		if logger == nil {
			logger = zap.NewNop()
		}
		logger.Warn("replay active content pack detections", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleContentPackDetectionOverrides(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
		if !ok {
			return
		}
		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		store, ok := s.store.(contentPackDetectionOverrideStore)
		if !ok || store == nil {
			writeJSON(w, http.StatusOK, contentPackDetectionOverrideListResponse{
				TenantID:    tenantID.String(),
				GeneratedAt: time.Now().UTC().Format(time.RFC3339),
				Items:       []contentPackDetectionOverrideDTO{},
			})
			return
		}
		rows, total, err := store.ListContentPackDetectionOverrides(r.Context(), tenantID, storage.ContentPackDetectionOverrideFilter{
			IncludeExpired: parseBoolQueryParam(r, "include_expired"),
		}, limit, offset)
		if err != nil {
			s.logger.Warn("list content pack detection overrides", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		items := make([]contentPackDetectionOverrideDTO, 0, len(rows))
		for _, row := range rows {
			items = append(items, newContentPackDetectionOverrideDTO(row))
		}
		writeJSON(w, http.StatusOK, contentPackDetectionOverrideListResponse{
			TenantID:    tenantID.String(),
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Items:       items,
			Total:       total,
		})
	case http.MethodPost:
		s.handleUpsertContentPackDetectionOverride(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUpsertContentPackDetectionOverride(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, ok := s.contentPackTenantFromQuery(w, r, principal)
	if !ok {
		return
	}
	store, ok := s.store.(contentPackDetectionOverrideStore)
	if !ok || store == nil {
		http.Error(w, "content pack detection override store unavailable", http.StatusServiceUnavailable)
		return
	}
	reader, ok := s.store.(contentPackRegistrySnapshotReader)
	if !ok || reader == nil {
		http.Error(w, "content pack registry snapshot store unavailable", http.StatusServiceUnavailable)
		return
	}
	var req contentPackDetectionOverrideRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	state := strings.ToLower(strings.TrimSpace(req.State))
	var suppressUntil *time.Time
	if strings.TrimSpace(req.SuppressUntil) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.SuppressUntil))
		if err != nil {
			http.Error(w, "invalid suppress_until", http.StatusBadRequest)
			return
		}
		suppressUntil = &parsed
	}
	active, err := reader.ActiveContentPackRegistrySnapshot(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("load active content pack registry snapshot", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if active == nil {
		http.Error(w, "no active content pack registry snapshot", http.StatusNotFound)
		return
	}
	if !contentPackDetectionDeclared(active.Snapshot, req.PackID, req.PackVersion, req.SourceID, req.DetectionID) {
		http.Error(w, "detection is not declared in the active content pack registry snapshot", http.StatusNotFound)
		return
	}
	updated, err := store.UpsertContentPackDetectionOverride(r.Context(), storage.UpsertContentPackDetectionOverrideParams{
		TenantID:         tenantID,
		PackID:           req.PackID,
		PackVersion:      req.PackVersion,
		SourceID:         req.SourceID,
		DetectionID:      req.DetectionID,
		State:            state,
		SuppressUntil:    suppressUntil,
		Reason:           req.Reason,
		UpdatedBySubject: strings.TrimSpace(principal.Subject),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "content_pack.detection_override."+updated.State, "content_pack_detection", contentPackDetectionResourceID(updated.PackID, updated.PackVersion, updated.SourceID, updated.DetectionID), map[string]any{
		"pack_id":        updated.PackID,
		"pack_version":   updated.PackVersion,
		"source_id":      updated.SourceID,
		"detection_id":   updated.DetectionID,
		"state":          updated.State,
		"suppress_until": formatContentPackTimePtr(updated.SuppressUntil),
		"reason":         updated.Reason,
	})
	writeJSON(w, http.StatusOK, newContentPackDetectionOverrideDTO(*updated))
}

func (s *Server) activeContentPackDetectionListResponse(ctx context.Context, tenantID uuid.UUID) (contentPackDetectionListResponse, error) {
	resp := newEmptyContentPackDetectionListResponse(tenantID)
	reader, ok := s.store.(contentPackRegistrySnapshotReader)
	if !ok || reader == nil {
		return resp, nil
	}
	record, err := reader.ActiveContentPackRegistrySnapshot(ctx, tenantID)
	if err != nil {
		return resp, err
	}
	if record == nil {
		return resp, nil
	}
	resp.Source = firstNonEmptyContentPack(record.Source, "database")
	resp.SnapshotID = record.ID.String()
	resp.SnapshotCreatedAt = record.CreatedAt.UTC().Format(time.RFC3339)
	resp.Items = contentPackDetectionMetadataItems(record.Snapshot)
	if len(resp.Items) == 0 {
		return resp, nil
	}
	overrideByKey, err := s.activeContentPackDetectionOverrideMap(ctx, tenantID, false)
	if err != nil {
		return resp, err
	}

	root := strings.TrimSpace(s.offlineContentRootDir())
	loadErr := ""
	loadedByKey := map[string]contentPackDetectionRule{}
	if root != "" && contentPackDetectionListHasEnabled(resp.Items) {
		rulesBySource, err := s.activeContentPackDetectionRules(ctx, tenantID)
		if err != nil {
			loadErr = err.Error()
		} else {
			for sourceID, rules := range rulesBySource {
				for _, rule := range rules {
					loadedByKey[contentPackDetectionRuntimeKey(sourceID, rule.Detection.DetectionID)] = rule
				}
			}
		}
	}

	now := time.Now().UTC()
	for i := range resp.Items {
		item := &resp.Items[i]
		switch {
		case item.ContentStatus != string(contentpacks.PackStatusEnabled):
			item.LoadStatus = "inactive"
			resp.Totals.Inactive++
		case loadErr != "":
			item.LoadStatus = "error"
			item.LoadError = loadErr
			resp.Totals.Errors++
		case root == "":
			item.LoadStatus = "metadata_only"
			resp.Totals.MetadataOnly++
		default:
			loaded, ok := loadedByKey[contentPackDetectionRuntimeKey(item.SourceID, item.DetectionID)]
			if !ok {
				item.LoadStatus = "metadata_only"
				resp.Totals.MetadataOnly++
				break
			}
			item.LoadStatus = "loaded"
			if strings.TrimSpace(item.Title) == "" {
				item.Title = loaded.Rule.Title
			}
			if strings.TrimSpace(item.Severity) == "" {
				item.Severity = loaded.Rule.Severity
			}
			if item.RiskScore == 0 {
				item.RiskScore = loaded.Rule.RiskScore
			}
			item.LogSource = contentPackDetectionLogSource(loaded.Rule.LogSource)
			resp.Totals.Loaded++
		}
		resp.Totals.Detections++
		resp.Totals.ByLoadStatus[item.LoadStatus]++
		severity := strings.TrimSpace(item.Severity)
		if severity == "" {
			severity = "unspecified"
		}
		resp.Totals.BySeverity[severity]++
		item.EffectiveState = contentPackDetectionEffectiveState(*item, overrideByKey, now)
		if override := contentPackDetectionOverrideFor(overrideByKey, item.PackID, item.PackVersion, item.SourceID, item.DetectionID, now); override != nil {
			item.Override = contentPackDetectionOverrideDTOPtr(*override)
		}
		resp.Totals.ByState[item.EffectiveState]++
		switch item.EffectiveState {
		case storage.ContentPackDetectionOverrideStateDisabled:
			resp.Totals.Disabled++
		case storage.ContentPackDetectionOverrideStateSuppressed:
			resp.Totals.Suppressed++
		}
	}
	return resp, nil
}

func (s *Server) activeContentPackDetectionReplayResponse(ctx context.Context, tenantID uuid.UUID, root string) (contentPackDetectionReplayResponse, error) {
	resp := contentPackDetectionReplayResponse{
		TenantID:    tenantID.String(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      "none",
		Items:       []contentPackDetectionReplayDTO{},
	}
	reader, ok := s.store.(contentPackRegistrySnapshotReader)
	if !ok || reader == nil {
		return resp, nil
	}
	record, err := reader.ActiveContentPackRegistrySnapshot(ctx, tenantID)
	if err != nil {
		return resp, err
	}
	if record == nil {
		return resp, nil
	}
	resp.Source = firstNonEmptyContentPack(record.Source, "database")
	resp.SnapshotID = record.ID.String()
	resp.SnapshotCreatedAt = record.CreatedAt.UTC().Format(time.RFC3339)

	registryRecords := map[string]contentpacks.PackRecord{}
	for _, pack := range record.Snapshot.Packs {
		registryRecords[contentPackKey(pack.PackID, pack.PackVersion)] = pack
	}
	if len(registryRecords) == 0 {
		return resp, nil
	}

	activePacks, err := offlinebundle.LoadActiveContentPacks(root)
	if err != nil {
		return resp, err
	}
	for _, pack := range activePacks {
		if err := ctx.Err(); err != nil {
			return resp, err
		}
		record, ok := registryRecords[contentPackKey(pack.Manifest.PackID, pack.Manifest.PackVersion)]
		if !ok {
			continue
		}
		report, err := contentpacks.ReplayManifestDetections(ctx, pack.Manifest, pack.Root, contentpacks.DetectionReplayOptions{
			DetectionLoadOptions: contentpacks.DetectionLoadOptions{
				SigmaFieldMap: contentpacks.DefaultSigmaFieldMap(),
			},
		})
		if err != nil {
			report = contentpacks.DetectionReplayReport{
				PackID:      strings.TrimSpace(pack.Manifest.PackID),
				PackVersion: strings.TrimSpace(pack.Manifest.PackVersion),
				Failures: []contentpacks.DetectionReplayFailure{{
					Index: -1,
					Error: err.Error(),
				}},
			}
		}
		resp.Items = append(resp.Items, contentPackDetectionReplayDTO{
			PackID:          record.PackID,
			PackVersion:     record.PackVersion,
			PackDisplayName: record.DisplayName,
			ContentStatus:   string(record.Status),
			Receipt:         contentPackDetectionReplayReceiptDTO(pack.ContentReceipt),
			Report:          report,
		})
		resp.Totals.Packs++
		if record.Status == contentpacks.PackStatusEnabled {
			resp.Totals.EnabledPacks++
		}
		if report.Passed() {
			resp.Totals.PassedPacks++
		} else {
			resp.Totals.FailedPacks++
		}
		resp.Totals.TotalRules += report.TotalRules
		resp.Totals.TotalCases += report.TotalCases
		resp.Totals.TotalEvents += report.TotalEvents
		resp.Totals.TotalEvaluations += report.TotalEvaluations
		resp.Totals.TotalMatches += report.TotalMatches
	}
	sort.Slice(resp.Items, func(i, j int) bool {
		left, right := resp.Items[i], resp.Items[j]
		if left.PackID != right.PackID {
			return left.PackID < right.PackID
		}
		return contentpacks.CompareSemver(left.PackVersion, right.PackVersion) > 0
	})
	return resp, nil
}

func (s *Server) replayActiveContentPackDetectionsForLifecycle(ctx context.Context, packID, packVersion string) (contentpacks.DetectionReplayReport, error) {
	root := strings.TrimSpace(s.offlineContentRootDir())
	if root == "" {
		return contentpacks.DetectionReplayReport{}, fmt.Errorf("offline content root unavailable")
	}
	activePacks, err := offlinebundle.LoadActiveContentPacks(root)
	if err != nil {
		return contentpacks.DetectionReplayReport{}, err
	}
	for _, pack := range activePacks {
		if strings.TrimSpace(pack.Manifest.PackID) != strings.TrimSpace(packID) || strings.TrimSpace(pack.Manifest.PackVersion) != strings.TrimSpace(packVersion) {
			continue
		}
		return contentpacks.ReplayManifestDetections(ctx, pack.Manifest, pack.Root, contentpacks.DetectionReplayOptions{
			DetectionLoadOptions: contentpacks.DetectionLoadOptions{
				SigmaFieldMap: contentpacks.DefaultSigmaFieldMap(),
			},
		})
	}
	return contentpacks.DetectionReplayReport{}, fmt.Errorf("active signed content pack artifact %s@%s not found", strings.TrimSpace(packID), strings.TrimSpace(packVersion))
}

func newEmptyContentPackDetectionListResponse(tenantID uuid.UUID) contentPackDetectionListResponse {
	return contentPackDetectionListResponse{
		TenantID:    tenantID.String(),
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Source:      "none",
		Items:       []contentPackDetectionDTO{},
		Totals: contentPackDetectionTotals{
			ByLoadStatus: map[string]int{},
			BySeverity:   map[string]int{},
			ByState:      map[string]int{},
		},
	}
}

func contentPackDetectionReplayReceiptDTO(receipt offlinebundle.ContentReceipt) *contentPackDetectionReplayReceipt {
	if strings.TrimSpace(receipt.BundleID) == "" && strings.TrimSpace(receipt.SHA256) == "" && receipt.BundleSequence == 0 && !receipt.Stale {
		return nil
	}
	return &contentPackDetectionReplayReceipt{
		BundleID:       receipt.BundleID,
		BundleVersion:  receipt.BundleVersion,
		BundleSequence: receipt.BundleSequence,
		SHA256:         receipt.SHA256,
		Stale:          receipt.Stale,
	}
}

func newContentPackDetectionOverrideDTO(record storage.ContentPackDetectionOverride) contentPackDetectionOverrideDTO {
	return contentPackDetectionOverrideDTO{
		ID:               record.ID.String(),
		PackID:           record.PackID,
		PackVersion:      record.PackVersion,
		SourceID:         record.SourceID,
		DetectionID:      record.DetectionID,
		State:            record.State,
		SuppressUntil:    formatContentPackTimePtr(record.SuppressUntil),
		Reason:           record.Reason,
		CreatedBySubject: record.CreatedBySubject,
		UpdatedBySubject: record.UpdatedBySubject,
		CreatedAt:        formatContentPackTime(record.CreatedAt),
		UpdatedAt:        formatContentPackTime(record.UpdatedAt),
	}
}

func contentPackDetectionOverrideDTOPtr(record storage.ContentPackDetectionOverride) *contentPackDetectionOverrideDTO {
	dto := newContentPackDetectionOverrideDTO(record)
	return &dto
}

func contentPackDetectionDeclared(snapshot contentpacks.RegistrySnapshot, packID, packVersion, sourceID, detectionID string) bool {
	packID = strings.TrimSpace(packID)
	packVersion = strings.TrimSpace(packVersion)
	sourceID = strings.TrimSpace(sourceID)
	detectionID = strings.TrimSpace(detectionID)
	if packID == "" || packVersion == "" || detectionID == "" {
		return false
	}
	for _, record := range snapshot.Packs {
		if strings.TrimSpace(record.PackID) != packID || strings.TrimSpace(record.PackVersion) != packVersion {
			continue
		}
		declared := false
		for _, detection := range record.Manifest.Detections {
			if strings.TrimSpace(detection.DetectionID) == detectionID {
				declared = true
				break
			}
		}
		if !declared {
			return false
		}
		for _, source := range record.Manifest.Sources {
			if sourceID != "" && strings.TrimSpace(source.SourceID) != sourceID {
				continue
			}
			for _, linked := range source.Detections {
				if strings.TrimSpace(linked) == detectionID {
					return true
				}
			}
		}
	}
	return false
}

func (s *Server) activeContentPackDetectionOverrideMap(ctx context.Context, tenantID uuid.UUID, includeExpired bool) (map[string]storage.ContentPackDetectionOverride, error) {
	store, ok := s.store.(contentPackDetectionOverrideStore)
	if !ok || store == nil {
		return nil, nil
	}
	rows, _, err := store.ListContentPackDetectionOverrides(ctx, tenantID, storage.ContentPackDetectionOverrideFilter{IncludeExpired: includeExpired}, 5000, 0)
	if err != nil {
		return nil, err
	}
	out := make(map[string]storage.ContentPackDetectionOverride, len(rows))
	now := time.Now().UTC()
	for _, row := range rows {
		if !includeExpired && !contentPackDetectionOverrideActive(row, now) {
			continue
		}
		out[contentPackDetectionOverrideKey(row.PackID, row.PackVersion, row.SourceID, row.DetectionID)] = row
	}
	return out, nil
}

func contentPackDetectionEffectiveState(item contentPackDetectionDTO, overrides map[string]storage.ContentPackDetectionOverride, now time.Time) string {
	if item.ContentStatus != string(contentpacks.PackStatusEnabled) {
		return "inactive"
	}
	if override := contentPackDetectionOverrideFor(overrides, item.PackID, item.PackVersion, item.SourceID, item.DetectionID, now); override != nil {
		return override.State
	}
	return storage.ContentPackDetectionOverrideStateEnabled
}

func contentPackDetectionOverrideFor(overrides map[string]storage.ContentPackDetectionOverride, packID, packVersion, sourceID, detectionID string, now time.Time) *storage.ContentPackDetectionOverride {
	if len(overrides) == 0 {
		return nil
	}
	for _, key := range []string{
		contentPackDetectionOverrideKey(packID, packVersion, sourceID, detectionID),
		contentPackDetectionOverrideKey(packID, packVersion, "", detectionID),
	} {
		if override, ok := overrides[key]; ok && contentPackDetectionOverrideActive(override, now) {
			return &override
		}
	}
	return nil
}

func contentPackDetectionOverrideActive(override storage.ContentPackDetectionOverride, now time.Time) bool {
	switch override.State {
	case storage.ContentPackDetectionOverrideStateDisabled:
		return true
	case storage.ContentPackDetectionOverrideStateEnabled:
		return true
	case storage.ContentPackDetectionOverrideStateSuppressed:
		return override.SuppressUntil != nil && override.SuppressUntil.After(now)
	default:
		return false
	}
}

func contentPackDetectionRuleSuppressed(overrides map[string]storage.ContentPackDetectionOverride, rule contentPackDetectionRule, sourceID string, now time.Time) bool {
	override := contentPackDetectionOverrideFor(overrides, rule.PackID, rule.PackVersion, sourceID, rule.Detection.DetectionID, now)
	if override == nil {
		return false
	}
	return override.State == storage.ContentPackDetectionOverrideStateDisabled || override.State == storage.ContentPackDetectionOverrideStateSuppressed
}

func contentPackDetectionOverrideKey(packID, packVersion, sourceID, detectionID string) string {
	return strings.TrimSpace(packID) + "\x00" + strings.TrimSpace(packVersion) + "\x00" + strings.TrimSpace(sourceID) + "\x00" + strings.TrimSpace(detectionID)
}

func contentPackDetectionResourceID(packID, packVersion, sourceID, detectionID string) string {
	parts := []string{strings.TrimSpace(packID) + "@" + strings.TrimSpace(packVersion)}
	if strings.TrimSpace(sourceID) != "" {
		parts = append(parts, strings.TrimSpace(sourceID))
	}
	parts = append(parts, strings.TrimSpace(detectionID))
	return strings.Join(parts, ":")
}

func contentPackDetectionMetadataItems(snapshot contentpacks.RegistrySnapshot) []contentPackDetectionDTO {
	items := []contentPackDetectionDTO{}
	for _, record := range snapshot.Packs {
		detectionsByID := map[string]contentpacks.Detection{}
		for _, detection := range record.Manifest.Detections {
			id := strings.TrimSpace(detection.DetectionID)
			if id == "" {
				continue
			}
			detectionsByID[id] = detection
		}
		for _, source := range record.Manifest.Sources {
			sourceID := strings.TrimSpace(source.SourceID)
			if sourceID == "" {
				continue
			}
			for _, rawDetectionID := range source.Detections {
				detectionID := strings.TrimSpace(rawDetectionID)
				if detectionID == "" {
					continue
				}
				detection := detectionsByID[detectionID]
				if strings.TrimSpace(detection.DetectionID) == "" {
					detection.DetectionID = detectionID
				}
				items = append(items, contentPackDetectionDTO{
					PackID:            record.PackID,
					PackVersion:       record.PackVersion,
					PackDisplayName:   record.DisplayName,
					ContentStatus:     string(record.Status),
					SourceID:          sourceID,
					SourceDisplayName: source.DisplayName,
					DetectionID:       detection.DetectionID,
					Title:             detection.Title,
					Kind:              detection.Kind,
					Severity:          detection.Severity,
					RiskScore:         detection.RiskScore,
					Tags:              append([]string(nil), detection.Tags...),
					Path:              detection.Path,
					Temporal:          contentPackDetectionTemporalDTOFor(detection.Temporal),
				})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		left, right := items[i], items[j]
		for _, cmp := range []int{
			strings.Compare(left.SourceID, right.SourceID),
			strings.Compare(left.DetectionID, right.DetectionID),
			strings.Compare(left.PackID, right.PackID),
			strings.Compare(left.PackVersion, right.PackVersion),
		} {
			if cmp < 0 {
				return true
			}
			if cmp > 0 {
				return false
			}
		}
		return false
	})
	return items
}

func contentPackDetectionTemporalDTOFor(temporal *contentpacks.DetectionTemporal) *contentPackDetectionTemporalDTO {
	if temporal == nil {
		return nil
	}
	return &contentPackDetectionTemporalDTO{
		Kind:               strings.TrimSpace(temporal.Kind),
		WindowSeconds:      temporal.WindowSeconds,
		Threshold:          temporal.Threshold,
		GroupBy:            append([]string(nil), temporal.GroupBy...),
		SuppressForSeconds: temporal.SuppressForSeconds,
		Sequence:           contentPackDetectionTemporalStepDTOs(temporal.Sequence),
		Join:               contentPackDetectionTemporalStepDTOs(temporal.Join),
	}
}

func contentPackDetectionTemporalStepDTOs(steps []contentpacks.DetectionTemporalStep) []contentPackDetectionTemporalStepDTO {
	if len(steps) == 0 {
		return nil
	}
	out := make([]contentPackDetectionTemporalStepDTO, len(steps))
	for i, step := range steps {
		out[i] = contentPackDetectionTemporalStepDTO{
			Field:         strings.TrimSpace(step.Field),
			Op:            strings.TrimSpace(step.Op),
			Values:        append([]any(nil), step.Values...),
			CaseSensitive: step.CaseSensitive,
		}
	}
	return out
}

func contentPackDetectionListHasEnabled(items []contentPackDetectionDTO) bool {
	for _, item := range items {
		if item.ContentStatus == string(contentpacks.PackStatusEnabled) {
			return true
		}
	}
	return false
}

func contentPackDetectionRuntimeKey(sourceID, detectionID string) string {
	return strings.TrimSpace(sourceID) + "\x00" + strings.TrimSpace(detectionID)
}

func contentPackDetectionLogSource(logSource detections.LogSource) *contentPackDetectionLogSourceDTO {
	if strings.TrimSpace(logSource.Product) == "" && strings.TrimSpace(logSource.Service) == "" && strings.TrimSpace(logSource.Category) == "" {
		return nil
	}
	return &contentPackDetectionLogSourceDTO{
		Product:  logSource.Product,
		Service:  logSource.Service,
		Category: logSource.Category,
	}
}

func (s *Server) evaluateContentPackDetections(ctx context.Context, tenantID, nodeID uuid.UUID, events []IngestedEvent) {
	if s == nil || s.store == nil || len(events) == 0 {
		return
	}
	logger := s.logger
	if logger == nil {
		logger = zap.NewNop()
	}
	rulesBySource, err := s.activeContentPackDetectionRules(ctx, tenantID)
	if err != nil {
		logger.Warn("load active content-pack detections", zap.Error(err), zap.String("tenant_id", tenantID.String()))
		return
	}
	if len(rulesBySource) == 0 {
		return
	}
	overrideByKey, err := s.activeContentPackDetectionOverrideMap(ctx, tenantID, false)
	if err != nil {
		logger.Warn("load content-pack detection overrides", zap.Error(err), zap.String("tenant_id", tenantID.String()))
	}
	now := time.Now().UTC()
	for i := range events {
		ev := &events[i]
		sourceID := contentPackSourceIDForEvent(ev)
		if sourceID == "" {
			continue
		}
		rules := rulesBySource[sourceID]
		if len(rules) == 0 {
			continue
		}
		detectionEvent := detectionEventFromIngestedEvent(ev)
		for _, rule := range rules {
			if contentPackDetectionRuleSuppressed(overrideByKey, rule, sourceID, now) {
				continue
			}
			match := s.evaluateContentPackDetectionRule(tenantID, sourceID, rule, detectionEvent)
			if !match.Matched {
				continue
			}
			if err := s.createContentPackDetectionAlert(ctx, tenantID, nodeID, sourceID, rule, match, ev, detectionEvent.Fields); err != nil && !errors.Is(err, storage.ErrAlertDeduped) {
				logger.Warn("create content-pack detection alert",
					zap.Error(err),
					zap.String("tenant_id", tenantID.String()),
					zap.String("source_id", sourceID),
					zap.String("detection_id", match.RuleID),
					zap.String("event_id", ev.EventID))
			}
		}
	}
}

func (s *Server) evaluateContentPackDetectionRule(tenantID uuid.UUID, sourceID string, rule contentPackDetectionRule, event detections.Event) detections.Match {
	temporalRule := contentpacks.TemporalRuleForDetection(rule.Detection, rule.Rule)
	if !temporalRule.Temporal.Enabled() {
		return rule.Rule.Evaluate(event)
	}
	temporalRule.Scope = contentPackDetectionTemporalScope(tenantID, sourceID, rule)
	return s.contentPackTemporalEvaluator().Evaluate(temporalRule, event)
}

func contentPackDetectionTemporalScope(tenantID uuid.UUID, sourceID string, rule contentPackDetectionRule) string {
	return strings.Join([]string{
		tenantID.String(),
		strings.TrimSpace(rule.PackID),
		strings.TrimSpace(rule.PackVersion),
		strings.TrimSpace(sourceID),
		strings.TrimSpace(rule.Detection.DetectionID),
	}, "\x00")
}

func (s *Server) contentPackTemporalEvaluator() *detections.StatefulEvaluator {
	if s == nil {
		return detections.NewStatefulEvaluator()
	}
	s.contentPackTemporalMu.Lock()
	defer s.contentPackTemporalMu.Unlock()
	if s.contentPackTemporalEval == nil {
		s.contentPackTemporalEval = detections.NewStatefulEvaluator()
	}
	return s.contentPackTemporalEval
}

func (s *Server) activeContentPackDetectionRules(ctx context.Context, tenantID uuid.UUID) (map[string][]contentPackDetectionRule, error) {
	root := s.offlineContentRootDir()
	reader, ok := s.store.(contentPackRegistrySnapshotReader)
	if !ok || reader == nil {
		return nil, nil
	}
	snapshot, err := reader.ActiveContentPackRegistrySnapshot(ctx, tenantID)
	if err != nil || snapshot == nil {
		return nil, err
	}
	if strings.TrimSpace(root) == "" {
		return s.persistedContentPackDetectionRules(ctx, tenantID, snapshot.ID)
	}
	if cached, ok := s.cachedContentPackDetectionRules(tenantID, snapshot.ID, root); ok {
		return cached, nil
	}
	rulesBySource, err := loadContentPackDetectionRules(ctx, root, snapshot.Snapshot)
	if err != nil {
		persisted, persistedErr := s.persistedContentPackDetectionRules(ctx, tenantID, snapshot.ID)
		if persistedErr == nil && len(persisted) > 0 {
			return persisted, nil
		}
		return nil, err
	}
	s.persistContentPackDetectionArtifacts(ctx, tenantID, snapshot.ID, rulesBySource)
	s.cacheContentPackDetectionRules(tenantID, contentPackDetectionCacheEntry{
		SnapshotID:    snapshot.ID,
		Root:          root,
		LoadedAt:      time.Now().UTC(),
		RulesBySource: rulesBySource,
	})
	return rulesBySource, nil
}

func (s *Server) cachedContentPackDetectionRules(tenantID, snapshotID uuid.UUID, root string) (map[string][]contentPackDetectionRule, bool) {
	s.contentPackDetectionsMu.Lock()
	defer s.contentPackDetectionsMu.Unlock()
	if s.contentPackDetectionsCache == nil {
		return nil, false
	}
	entry, ok := s.contentPackDetectionsCache[tenantID]
	if !ok || entry.SnapshotID != snapshotID || entry.Root != root {
		return nil, false
	}
	if time.Since(entry.LoadedAt) > contentPackDetectionCacheTTL {
		return nil, false
	}
	return entry.RulesBySource, true
}

func (s *Server) cacheContentPackDetectionRules(tenantID uuid.UUID, entry contentPackDetectionCacheEntry) {
	s.contentPackDetectionsMu.Lock()
	defer s.contentPackDetectionsMu.Unlock()
	if s.contentPackDetectionsCache == nil {
		s.contentPackDetectionsCache = map[uuid.UUID]contentPackDetectionCacheEntry{}
	}
	s.contentPackDetectionsCache[tenantID] = entry
}

func (s *Server) persistContentPackDetectionArtifacts(ctx context.Context, tenantID, snapshotID uuid.UUID, rulesBySource map[string][]contentPackDetectionRule) {
	store, ok := s.store.(contentPackDetectionArtifactStore)
	if !ok || store == nil || len(rulesBySource) == 0 {
		return
	}
	artifacts := []storage.ContentPackDetectionArtifact{}
	now := time.Now().UTC()
	for sourceID, rules := range rulesBySource {
		for _, rule := range rules {
			artifacts = append(artifacts, storage.ContentPackDetectionArtifact{
				TenantID:           tenantID,
				RegistrySnapshotID: snapshotID,
				PackID:             rule.PackID,
				PackVersion:        rule.PackVersion,
				SourceID:           sourceID,
				DetectionID:        rule.Detection.DetectionID,
				Detection:          rule.Detection,
				Rule:               rule.Rule,
				LoadedAt:           now,
			})
		}
	}
	if err := store.ReplaceContentPackDetectionArtifacts(ctx, storage.ReplaceContentPackDetectionArtifactsParams{
		TenantID:           tenantID,
		RegistrySnapshotID: snapshotID,
		Artifacts:          artifacts,
	}); err != nil {
		logger := s.logger
		if logger == nil {
			logger = zap.NewNop()
		}
		logger.Warn("persist content-pack detection artifacts", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("snapshot_id", snapshotID.String()))
	}
}

func (s *Server) persistActiveContentPackDetectionArtifactsForSnapshot(ctx context.Context, tenantID, snapshotID uuid.UUID, snapshot contentpacks.RegistrySnapshot) {
	root := strings.TrimSpace(s.offlineContentRootDir())
	if root == "" {
		return
	}
	rulesBySource, err := loadContentPackDetectionRules(ctx, root, snapshot)
	if err != nil {
		logger := s.logger
		if logger == nil {
			logger = zap.NewNop()
		}
		logger.Warn("load content-pack detections for artifact persistence", zap.Error(err), zap.String("tenant_id", tenantID.String()), zap.String("snapshot_id", snapshotID.String()))
		return
	}
	s.persistContentPackDetectionArtifacts(ctx, tenantID, snapshotID, rulesBySource)
}

func (s *Server) persistedContentPackDetectionRules(ctx context.Context, tenantID, snapshotID uuid.UUID) (map[string][]contentPackDetectionRule, error) {
	store, ok := s.store.(contentPackDetectionArtifactStore)
	if !ok || store == nil {
		return nil, nil
	}
	artifacts, err := store.ListContentPackDetectionArtifacts(ctx, tenantID, snapshotID)
	if err != nil {
		return nil, err
	}
	if len(artifacts) == 0 {
		return nil, nil
	}
	rulesBySource := map[string][]contentPackDetectionRule{}
	for _, artifact := range artifacts {
		sourceID := strings.TrimSpace(artifact.SourceID)
		rulesBySource[sourceID] = append(rulesBySource[sourceID], contentPackDetectionRule{
			PackID:      artifact.PackID,
			PackVersion: artifact.PackVersion,
			SourceID:    sourceID,
			Detection:   artifact.Detection,
			Rule:        artifact.Rule,
		})
	}
	return rulesBySource, nil
}

func loadContentPackDetectionRules(ctx context.Context, root string, snapshot contentpacks.RegistrySnapshot) (map[string][]contentPackDetectionRule, error) {
	enabled := map[string]contentpacks.PackRecord{}
	for _, record := range snapshot.Packs {
		if record.Status != contentpacks.PackStatusEnabled {
			continue
		}
		enabled[contentPackKey(record.PackID, record.PackVersion)] = record
	}
	if len(enabled) == 0 {
		return nil, nil
	}
	active, err := offlinebundle.LoadActiveContentPacks(root)
	if err != nil {
		return nil, err
	}
	rulesBySource := map[string][]contentPackDetectionRule{}
	for _, pack := range active {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return rulesBySource, ctxErr
		}
		key := contentPackKey(pack.Manifest.PackID, pack.Manifest.PackVersion)
		record, ok := enabled[key]
		if !ok || record.DetectionCount == 0 {
			continue
		}
		loaded, err := contentpacks.LoadManifestDetections(ctx, pack.Manifest, pack.Root, contentpacks.DetectionLoadOptions{
			SigmaFieldMap: contentpacks.DefaultSigmaFieldMap(),
		})
		if err != nil {
			return rulesBySource, fmt.Errorf("load detections for %s@%s: %w", pack.Manifest.PackID, pack.Manifest.PackVersion, err)
		}
		detectionByID := map[string]contentpacks.LoadedDetection{}
		for _, item := range loaded {
			detectionByID[strings.TrimSpace(item.Manifest.DetectionID)] = item
		}
		for _, source := range pack.Manifest.Sources {
			sourceID := strings.TrimSpace(source.SourceID)
			for _, detectionID := range source.Detections {
				item, ok := detectionByID[strings.TrimSpace(detectionID)]
				if !ok {
					continue
				}
				rulesBySource[sourceID] = append(rulesBySource[sourceID], contentPackDetectionRule{
					PackID:      pack.Manifest.PackID,
					PackVersion: pack.Manifest.PackVersion,
					SourceID:    sourceID,
					Detection:   item.Manifest,
					Rule:        item.Rule,
				})
			}
		}
	}
	return rulesBySource, nil
}

func (s *Server) createContentPackDetectionAlert(ctx context.Context, tenantID, nodeID uuid.UUID, sourceID string, rule contentPackDetectionRule, match detections.Match, ev *IngestedEvent, fields map[string]any) error {
	if ev == nil {
		return nil
	}
	severity := strings.TrimSpace(match.Severity)
	if severity == "" {
		severity = "medium"
	}
	title := strings.TrimSpace(match.Title)
	if title == "" {
		title = strings.TrimSpace(match.RuleID)
	}
	eventID := strings.TrimSpace(ev.EventID)
	if eventID == "" {
		eventID = deterministicEventID(tenantID, nodeID, ev)
	}
	nodePtr := &nodeID
	alert, err := s.store.CreateAlert(ctx, storage.CreateAlertParams{
		TenantID: tenantID,
		NodeID:   nodePtr,
		Source:   "content_pack_detection",
		Severity: severity,
		Title:    title,
		Summary:  fmt.Sprintf("Detection %s matched %s event %s", match.RuleID, ev.Type, eventID),
		DedupKey: "content_pack_detection:" + tenantID.String() + ":" + sourceID + ":" + match.RuleID + ":" + eventID,
		Context: map[string]any{
			"pack_id":        rule.PackID,
			"pack_version":   rule.PackVersion,
			"source_id":      sourceID,
			"detection_id":   match.RuleID,
			"detection_tags": append([]string(nil), match.Tags...),
			"risk_score":     match.RiskScore,
			"temporal": map[string]any{
				"count":          match.Count,
				"threshold":      match.Threshold,
				"window_seconds": match.WindowSeconds,
				"group_key":      match.GroupKey,
			},
			"event_id":      eventID,
			"event_type":    ev.Type,
			"event_ts":      ev.TS.Format(time.RFC3339Nano),
			"collector":     ev.Collector,
			"parser":        ev.Parser,
			"parser_status": ev.ParserStatus,
			"raw_ref":       ev.RawRef,
			"normalized":    fields,
			"citations": []map[string]any{{
				"type":         "content_pack_detection",
				"pack_id":      rule.PackID,
				"pack_version": rule.PackVersion,
				"source_id":    sourceID,
				"detection_id": match.RuleID,
				"event_id":     eventID,
			}},
		},
	})
	if err != nil {
		return err
	}
	if alert != nil {
		s.publishContentPackDetectionAlert(tenantID, alert)
	}
	return nil
}

func (s *Server) publishContentPackDetectionAlert(tenantID uuid.UUID, alert *storage.Alert) {
	if s == nil || alert == nil {
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"alert_id": alert.ID.String(),
		"severity": alert.Severity,
		"source":   alert.Source,
		"title":    alert.Title,
	})
	var nodeFilter *uuid.UUID
	if alert.NodeID.Valid {
		n := alert.NodeID.UUID
		nodeFilter = &n
	}
	s.publishEvent(eventbus.Event{
		Topic:    eventbus.TopicAlertOpened,
		TenantID: tenantID,
		NodeID:   nodeFilter,
		Payload:  payload,
	})
}

func detectionEventFromIngestedEvent(ev *IngestedEvent) detections.Event {
	fields := normalizedFieldsForIngestedEvent(ev)
	if fields == nil {
		fields = map[string]any{}
	}
	if strings.TrimSpace(ev.Message) != "" {
		fields["message"] = strings.TrimSpace(ev.Message)
	}
	return detections.Event{
		Raw:       strings.TrimSpace(ev.Message),
		Fields:    fields,
		Timestamp: ev.TS,
	}
}

func contentPackSourceIDForEvent(ev *IngestedEvent) string {
	if ev == nil || len(ev.Details) == 0 {
		return ""
	}
	for _, key := range []string{"control_one.content_pack_source_id", "content_pack_source_id", "source_id"} {
		if value := strings.TrimSpace(fmt.Sprint(ev.Details[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	if labels, ok := ev.Details["labels"].(map[string]string); ok {
		for _, key := range []string{"control_one.content_pack_source_id", "content_pack_source_id", "source_id"} {
			if value := strings.TrimSpace(labels[key]); value != "" {
				return value
			}
		}
	}
	if labels, ok := ev.Details["labels"].(map[string]any); ok {
		for _, key := range []string{"control_one.content_pack_source_id", "content_pack_source_id", "source_id"} {
			if value := strings.TrimSpace(fmt.Sprint(labels[key])); value != "" && value != "<nil>" {
				return value
			}
		}
	}
	return ""
}

func contentPackKey(packID, packVersion string) string {
	return strings.TrimSpace(packID) + "@" + strings.TrimSpace(packVersion)
}
