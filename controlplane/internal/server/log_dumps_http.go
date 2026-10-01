package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const (
	JobTypeLogDump          = "log_dump"
	agentLogDumpCapability  = "log_dump.v1"
	maxLogDumpWindow        = 24 * time.Hour
	defaultLogDumpRetention = 7
	maxLogDumpPreviewBytes  = 256 << 10
	maxLogDumpPreviewLines  = 200
)

type logDumpStore interface {
	CreateLogDump(context.Context, storage.LogDump) (*storage.LogDump, error)
	CreateAgentLogDumpWithJob(context.Context, storage.LogDump, storage.Job) (*storage.LogDump, *storage.Job, error)
	GetLogDump(context.Context, uuid.UUID, uuid.UUID) (*storage.LogDump, error)
	ListLogDumps(context.Context, storage.LogDumpFilter, int, int) ([]storage.LogDump, int, error)
	ExpireLogDump(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Time) (bool, error)
	MarkControlPlaneLogDumpCaptured(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, string, int64, int64, bool, time.Time) error
	FailControlPlaneLogDump(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error
}

type tenantRoleStore interface {
	UserHasTenantRole(context.Context, uuid.UUID, uuid.UUID, []string) (bool, error)
}

type logDumpRequest struct {
	TenantID     string         `json:"tenant_id"`
	NodeID       string         `json:"node_id"`
	Source       string         `json:"source"`
	WindowStart  time.Time      `json:"window_start"`
	WindowEnd    time.Time      `json:"window_end"`
	EntityFilter map[string]any `json:"entity_filter,omitempty"`
	Retention    int            `json:"retention_days,omitempty"`
}

type logDumpResponse struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	NodeID          string         `json:"node_id"`
	JobID           *string        `json:"job_id,omitempty"`
	Source          string         `json:"source"`
	EntityFilter    map[string]any `json:"entity_filter"`
	WindowStart     string         `json:"window_start"`
	WindowEnd       string         `json:"window_end"`
	RetentionDays   int            `json:"retention_days"`
	Status          string         `json:"status"`
	ArtifactSHA256  string         `json:"artifact_sha256,omitempty"`
	RowCount        int64          `json:"row_count"`
	SizeBytes       int64          `json:"size_bytes"`
	Truncated       bool           `json:"truncated"`
	SourceAvailable *bool          `json:"source_available,omitempty"`
	SourceReason    string         `json:"source_reason,omitempty"`
	Error           string         `json:"error,omitempty"`
	CreatedAt       string         `json:"created_at"`
	CapturedAt      *string        `json:"captured_at,omitempty"`
	ExpiresAt       string         `json:"expires_at"`
	Expired         bool           `json:"expired"`
	DownloadURL     string         `json:"download_url,omitempty"`
	PreviewURL      string         `json:"preview_url,omitempty"`
}

func (s *Server) logDumpStorage() (logDumpStore, bool) {
	store, ok := s.store.(logDumpStore)
	return store, ok
}

func (s *Server) logDumpNow() time.Time {
	if s != nil && s.clockOverride != nil {
		return s.clockOverride().UTC()
	}
	return time.Now().UTC()
}

func (s *Server) handleLogDumps(w http.ResponseWriter, r *http.Request) {
	setLogDumpResponseHeaders(w)
	switch r.Method {
	case http.MethodPost:
		s.handleCreateLogDump(w, r)
	case http.MethodGet:
		s.handleListLogDumps(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLogDumpResource(w http.ResponseWriter, r *http.Request) {
	setLogDumpResponseHeaders(w)
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/log-dumps/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || len(parts) > 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	dumpID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid log dump id", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "preview":
			s.handleLogDumpPreview(w, r, dumpID)
		case "download":
			s.handleLogDumpDownload(w, r, dumpID)
		default:
			http.NotFound(w, r)
		}
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	tenantID, ok := s.logDumpTenantFromQuery(w, r)
	if !ok {
		return
	}
	if _, ok := s.authorizeTenantLogDump(w, r, tenantID, roleViewer, roleOperator, roleCISO, roleAdmin); !ok {
		return
	}
	dump, ok := s.loadReadableLogDump(w, r, tenantID, dumpID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.logDumpResponse(*dump))
}

func (s *Server) handleCreateLogDump(w http.ResponseWriter, r *http.Request) {
	store, ok := s.logDumpStorage()
	if !ok {
		http.Error(w, "log dump storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req logDumpRequest
	if err := decodeStrictJSONDocument(r, &req, 32<<10); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	principal, ok := s.authorizeTenantLogDump(w, r, tenantID, roleOperator, roleAdmin)
	if !ok {
		return
	}
	nodeID, err := uuid.Parse(strings.TrimSpace(req.NodeID))
	if err != nil {
		http.Error(w, "invalid node_id", http.StatusBadRequest)
		return
	}
	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node for log dump", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil || node.TenantID != tenantID {
		http.NotFound(w, r)
		return
	}

	now := s.logDumpNow()
	source := strings.ToLower(strings.TrimSpace(req.Source))
	if source == "" {
		source = storage.LogDumpSourceControlPlane
	}
	if source != storage.LogDumpSourceControlPlane && source != storage.LogDumpSourceNodeAgent {
		http.Error(w, "source must be control_plane or node_agent", http.StatusBadRequest)
		return
	}
	if req.WindowStart.IsZero() || req.WindowEnd.IsZero() || !req.WindowEnd.After(req.WindowStart) {
		http.Error(w, "window_start and window_end must define a positive window", http.StatusBadRequest)
		return
	}
	if req.WindowEnd.Sub(req.WindowStart) > maxLogDumpWindow {
		http.Error(w, "log dump window cannot exceed 24 hours", http.StatusBadRequest)
		return
	}
	if req.WindowEnd.After(now.Add(time.Minute)) {
		http.Error(w, "window_end cannot be in the future", http.StatusBadRequest)
		return
	}
	retention := req.Retention
	if retention == 0 {
		retention = defaultLogDumpRetention
	}
	if !validLogDumpRetention(retention) {
		http.Error(w, "retention_days must be one of 1, 3, 7, 14, 30", http.StatusBadRequest)
		return
	}
	if source == storage.LogDumpSourceNodeAgent && !nodeAdvertisesCapability(node, agentLogDumpCapability) {
		http.Error(w, "node agent does not support raw log dumps", http.StatusConflict)
		return
	}

	requestedBy := uuid.NullUUID{}
	if user, uerr := s.store.GetUserByExternalID(r.Context(), principal.Subject); uerr == nil && user != nil {
		requestedBy = uuid.NullUUID{UUID: user.ID, Valid: true}
	}
	dump := storage.LogDump{
		ID:            uuid.New(),
		TenantID:      tenantID,
		NodeID:        nodeID,
		Source:        source,
		EntityFilter:  req.EntityFilter,
		WindowStart:   req.WindowStart.UTC(),
		WindowEnd:     req.WindowEnd.UTC(),
		RetentionDays: retention,
		Status:        storage.LogDumpStatusRequested,
		RequestedBy:   requestedBy,
		CreatedAt:     now,
		ExpiresAt:     now.Add(time.Duration(retention) * 24 * time.Hour),
	}

	var created *storage.LogDump
	if source == storage.LogDumpSourceNodeAgent {
		jobID := uuid.New()
		payload, _ := json.Marshal(map[string]any{
			"dump_id":      dump.ID.String(),
			"node_id":      nodeID.String(),
			"window_start": dump.WindowStart.Format(time.RFC3339Nano),
			"window_end":   dump.WindowEnd.Format(time.RFC3339Nano),
		})
		var job *storage.Job
		created, job, err = store.CreateAgentLogDumpWithJob(r.Context(), dump, storage.Job{
			ID: jobID, TenantID: tenantID, Type: JobTypeLogDump, Status: storage.JobStatusQueued,
			Payload: payload, MaxRetries: 0,
		})
		_ = job
	} else {
		created, err = store.CreateLogDump(r.Context(), dump)
	}
	if err != nil {
		s.logger.Error("create log dump", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	if source == storage.LogDumpSourceControlPlane {
		if err := s.captureControlPlaneLogDump(r.Context(), created); err != nil {
			s.logger.Warn("capture control-plane log dump", zap.String("dump_id", created.ID.String()), zap.Error(err))
		}
		if refreshed, gerr := store.GetLogDump(r.Context(), tenantID, created.ID); gerr == nil && refreshed != nil {
			created = refreshed
		}
	}

	s.recordAudit(r.Context(), principal, tenantID, "log_dump.requested", "log_dump", created.ID.String(), map[string]any{
		"node_id":        nodeID.String(),
		"source":         source,
		"window_start":   dump.WindowStart.Format(time.RFC3339),
		"window_end":     dump.WindowEnd.Format(time.RFC3339),
		"retention_days": retention,
		"job_id":         nullUUIDString(created.JobID),
	})
	writeJSON(w, http.StatusAccepted, s.logDumpResponse(*created))
}

func (s *Server) handleListLogDumps(w http.ResponseWriter, r *http.Request) {
	store, ok := s.logDumpStorage()
	if !ok {
		http.Error(w, "log dump storage unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, ok := s.logDumpTenantFromQuery(w, r)
	if !ok {
		return
	}
	if _, ok := s.authorizeTenantLogDump(w, r, tenantID, roleViewer, roleOperator, roleCISO, roleAdmin); !ok {
		return
	}
	filter := storage.LogDumpFilter{TenantID: tenantID, Source: r.URL.Query().Get("source"), Status: r.URL.Query().Get("status")}
	if raw := strings.TrimSpace(r.URL.Query().Get("node_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		filter.NodeID = id
	}
	limit, offset, err := logDumpPagination(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	dumps, total, err := store.ListLogDumps(r.Context(), filter, limit, offset)
	if err != nil {
		s.logger.Error("list log dumps", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]logDumpResponse, 0, len(dumps))
	for i := range dumps {
		d := s.applyLogDumpReadExpiry(r.Context(), store, dumps[i])
		resp = append(resp, s.logDumpResponse(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data":       resp,
		"pagination": map[string]any{"limit": limit, "offset": offset, "total": total},
	})
}

func (s *Server) handleLogDumpPreview(w http.ResponseWriter, r *http.Request, dumpID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	tenantID, ok := s.logDumpTenantFromQuery(w, r)
	if !ok {
		return
	}
	if _, ok := s.authorizeTenantLogDump(w, r, tenantID, roleViewer, roleOperator, roleCISO, roleAdmin); !ok {
		return
	}
	dump, ok := s.loadReadableLogDump(w, r, tenantID, dumpID)
	if !ok {
		return
	}
	if dump.Status != storage.LogDumpStatusCaptured || strings.TrimSpace(dump.ArtifactPath) == "" {
		http.Error(w, "log dump is not available", http.StatusConflict)
		return
	}
	f, err := openScopedLogDumpFile(dump.ArtifactPath)
	if err != nil {
		s.logger.Warn("open log dump preview", zap.Error(err))
		http.Error(w, "log dump artifact unavailable", http.StatusGone)
		return
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxLogDumpPreviewBytes+1))
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	truncated := len(data) > maxLogDumpPreviewBytes
	if truncated {
		data = data[:maxLogDumpPreviewBytes]
	}
	rawLines := bytes.Split(data, []byte("\n"))
	lines := make([]string, 0, minInt(len(rawLines), maxLogDumpPreviewLines))
	for _, raw := range rawLines {
		if len(lines) >= maxLogDumpPreviewLines {
			truncated = true
			break
		}
		if len(raw) == 0 {
			continue
		}
		lines = append(lines, string(raw))
	}
	setLogDumpArtifactHeaders(w)
	writeJSON(w, http.StatusOK, map[string]any{"lines": lines, "truncated": truncated || dump.Truncated})
}

func (s *Server) handleLogDumpDownload(w http.ResponseWriter, r *http.Request, dumpID uuid.UUID) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	tenantID, ok := s.logDumpTenantFromQuery(w, r)
	if !ok {
		return
	}
	if _, ok := s.authorizeTenantLogDump(w, r, tenantID, roleViewer, roleOperator, roleCISO, roleAdmin); !ok {
		return
	}
	dump, ok := s.loadReadableLogDump(w, r, tenantID, dumpID)
	if !ok {
		return
	}
	if dump.Status != storage.LogDumpStatusCaptured || strings.TrimSpace(dump.ArtifactPath) == "" {
		http.Error(w, "log dump is not available", http.StatusConflict)
		return
	}
	f, err := openScopedLogDumpFile(dump.ArtifactPath)
	if err != nil {
		http.Error(w, "log dump artifact unavailable", http.StatusGone)
		return
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	setLogDumpArtifactHeaders(w)
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="control-one-log-dump-%s.ndjson"`, dump.ID.String()))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	buf := make([]byte, 64<<10)
	for {
		if !s.logDumpNow().Before(dump.ExpiresAt) {
			return
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
		}
		if errors.Is(readErr, io.EOF) {
			return
		}
		if readErr != nil {
			return
		}
	}
}

func (s *Server) loadReadableLogDump(w http.ResponseWriter, r *http.Request, tenantID, dumpID uuid.UUID) (*storage.LogDump, bool) {
	store, ok := s.logDumpStorage()
	if !ok {
		http.Error(w, "log dump storage unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	dump, err := store.GetLogDump(r.Context(), tenantID, dumpID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			s.logger.Error("get log dump", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
		return nil, false
	}
	d := s.applyLogDumpReadExpiry(r.Context(), store, *dump)
	if d.Status == storage.LogDumpStatusExpired || d.Expired(s.logDumpNow()) {
		http.Error(w, "log dump expired", http.StatusGone)
		return nil, false
	}
	return &d, true
}

func (s *Server) applyLogDumpReadExpiry(ctx context.Context, store logDumpStore, d storage.LogDump) storage.LogDump {
	now := s.logDumpNow()
	if d.Expired(now) && d.Status != storage.LogDumpStatusExpired && d.Status != storage.LogDumpStatusDeleting {
		if changed, err := store.ExpireLogDump(ctx, d.TenantID, d.NodeID, d.ID, now); err == nil && changed {
			d.Status = storage.LogDumpStatusExpired
		}
	}
	return d
}

func (s *Server) authorizeTenantLogDump(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID, roles ...string) (*auth.Principal, bool) {
	principal, ok := s.authorize(w, r, roles...)
	if !ok {
		return nil, false
	}
	if principal.Type == "agent" {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return nil, false
	}
	user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject)
	if err != nil {
		s.logger.Warn("resolve log dump user", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return nil, false
	}
	if user == nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return nil, false
	}
	roleStore, ok := s.store.(tenantRoleStore)
	if !ok {
		http.Error(w, "tenant authorization unavailable", http.StatusServiceUnavailable)
		return nil, false
	}
	allowed, err := roleStore.UserHasTenantRole(r.Context(), user.ID, tenantID, roles)
	if err != nil {
		s.logger.Warn("check log dump tenant role", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return nil, false
	}
	if !allowed {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return nil, false
	}
	return principal, true
}

func (s *Server) logDumpTenantFromQuery(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, "valid tenant_id query parameter is required", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return tenantID, true
}

func (s *Server) logDumpResponse(d storage.LogDump) logDumpResponse {
	now := s.logDumpNow()
	resp := logDumpResponse{
		ID: d.ID.String(), TenantID: d.TenantID.String(), NodeID: d.NodeID.String(),
		Source: d.Source, EntityFilter: d.EntityFilter, WindowStart: d.WindowStart.UTC().Format(time.RFC3339Nano),
		WindowEnd: d.WindowEnd.UTC().Format(time.RFC3339Nano), RetentionDays: d.RetentionDays,
		Status: d.Status, ArtifactSHA256: d.ArtifactSHA256, RowCount: d.RowCount, SizeBytes: d.SizeBytes,
		Truncated: d.Truncated, SourceAvailable: d.SourceAvailable, SourceReason: d.SourceReason, Error: d.Error,
		CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339Nano), ExpiresAt: d.ExpiresAt.UTC().Format(time.RFC3339Nano),
		Expired: d.Expired(now) || d.Status == storage.LogDumpStatusExpired || d.Status == storage.LogDumpStatusDeleting,
	}
	if d.JobID.Valid {
		v := d.JobID.UUID.String()
		resp.JobID = &v
	}
	if d.CapturedAt != nil {
		v := d.CapturedAt.UTC().Format(time.RFC3339Nano)
		resp.CapturedAt = &v
	}
	if d.Status == storage.LogDumpStatusCaptured && !resp.Expired {
		q := "?tenant_id=" + d.TenantID.String()
		resp.PreviewURL = "/api/v1/log-dumps/" + d.ID.String() + "/preview" + q
		resp.DownloadURL = "/api/v1/log-dumps/" + d.ID.String() + "/download" + q
	}
	return resp
}

func decodeStrictJSONDocument(r *http.Request, dst any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBytes)
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON document")
		}
		return err
	}
	return nil
}

func validLogDumpRetention(days int) bool {
	switch days {
	case 1, 3, 7, 14, 30:
		return true
	default:
		return false
	}
}

func logDumpPagination(r *http.Request) (int, int, error) {
	limit, offset := 50, 0
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 200 {
			return 0, 0, errors.New("limit must be between 1 and 200")
		}
		limit = v
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("offset")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 0 {
			return 0, 0, errors.New("offset must be non-negative")
		}
		offset = v
	}
	return limit, offset, nil
}

func setLogDumpResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func setLogDumpArtifactHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}

func nullUUIDString(v uuid.NullUUID) any {
	if !v.Valid {
		return nil
	}
	return v.UUID.String()
}
