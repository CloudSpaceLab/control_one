package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
)

// Job-type constants for the Finacle integration.
//
//	finacle.sync         — pulls a profile snapshot from Finacle into
//	                       finacle_profiles. Idempotent (UpsertFinacleProfile).
//	finacle.shift_rotate — at a shift boundary: enables incoming-staff access,
//	                       disables outgoing-staff access. Failure modes are
//	                       asymmetric: enables fail closed (no un-auditable
//	                       grant); disables fail open (revoke unconditionally,
//	                       continue retries on the connector).
const (
	JobTypeFinacleSync        = "finacle.sync"
	JobTypeFinacleShiftRotate = "finacle.shift_rotate"
)

// finacleClient abstracts the outbound Finacle connector so handlers and the
// shift_rotate worker can be unit-tested without a real Finacle. The default
// implementation is a stub that always returns ErrFinacleUnconfigured; tests
// inject their own.
type finacleClient interface {
	Ping(ctx context.Context, conn storage.FinacleConnection) error
	EnableProfile(ctx context.Context, conn storage.FinacleConnection, finacleUID string) error
	DisableProfile(ctx context.Context, conn storage.FinacleConnection, finacleUID string) error
	ListProfiles(ctx context.Context, conn storage.FinacleConnection) ([]storage.UpsertFinacleProfileParams, error)
}

// errFinacleUnconfigured ─ no in-tree Finacle client. The handler swallows it
// for the connection test endpoint (returns a 503 message) and the worker
// converts it into the appropriate fail-open / fail-closed branch.
var errFinacleUnconfigured = errors.New("finacle client not configured")

// stubFinacleClient is the default; production deployments wire a real client
// via Server.finacleClient before calling configureJobIntegrations.
type stubFinacleClient struct{}

func (stubFinacleClient) Ping(context.Context, storage.FinacleConnection) error {
	return errFinacleUnconfigured
}
func (stubFinacleClient) EnableProfile(context.Context, storage.FinacleConnection, string) error {
	return errFinacleUnconfigured
}
func (stubFinacleClient) DisableProfile(context.Context, storage.FinacleConnection, string) error {
	return errFinacleUnconfigured
}
func (stubFinacleClient) ListProfiles(context.Context, storage.FinacleConnection) ([]storage.UpsertFinacleProfileParams, error) {
	return nil, errFinacleUnconfigured
}

// finacleClientOrStub returns s.finacleClient when set, else a stub. Keeps
// the worker handler body free of nil checks.
func (s *Server) finacleClientOrStub() finacleClient {
	if s == nil || s.finacleClient == nil {
		return stubFinacleClient{}
	}
	return s.finacleClient
}

// ── REST handlers ───────────────────────────────────────────────────────────

type finacleConnectionResponse struct {
	ID            string  `json:"id"`
	TenantID      string  `json:"tenant_id"`
	Host          string  `json:"host"`
	AuthMethod    string  `json:"auth_method"`
	CredentialRef *string `json:"credential_ref,omitempty"`
	LastSyncAt    *string `json:"last_sync_at,omitempty"`
	LastError     *string `json:"last_error,omitempty"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func newFinacleConnectionResponse(c storage.FinacleConnection) finacleConnectionResponse {
	out := finacleConnectionResponse{
		ID:         c.ID.String(),
		TenantID:   c.TenantID.String(),
		Host:       c.Host,
		AuthMethod: c.AuthMethod,
		CreatedAt:  c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:  c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.CredentialRef.Valid {
		v := c.CredentialRef.String
		out.CredentialRef = &v
	}
	if c.LastSyncAt.Valid {
		v := c.LastSyncAt.Time.UTC().Format(time.RFC3339)
		out.LastSyncAt = &v
	}
	if c.LastError.Valid {
		v := c.LastError.String
		out.LastError = &v
	}
	return out
}

type createFinacleConnectionRequest struct {
	TenantID      string  `json:"tenant_id"`
	Host          string  `json:"host"`
	AuthMethod    string  `json:"auth_method"`
	CredentialRef *string `json:"credential_ref,omitempty"`
}

type updateFinacleConnectionRequest struct {
	Host          *string `json:"host,omitempty"`
	AuthMethod    *string `json:"auth_method,omitempty"`
	CredentialRef *string `json:"credential_ref,omitempty"`
}

func (s *Server) handleFinacleConnections(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleListFinacleConnections(w, r)
	case http.MethodPost:
		s.handleCreateFinacleConnection(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListFinacleConnections(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	conns, err := s.store.ListFinacleConnections(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("list finacle connections", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]finacleConnectionResponse, 0, len(conns))
	for _, c := range conns {
		resp = append(resp, newFinacleConnectionResponse(c))
	}
	writeJSON(w, http.StatusOK, struct {
		Connections []finacleConnectionResponse `json:"connections"`
	}{Connections: resp})
}

func (s *Server) handleCreateFinacleConnection(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	var req createFinacleConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		http.Error(w, "host is required", http.StatusBadRequest)
		return
	}
	conn, err := s.store.CreateFinacleConnection(r.Context(), storage.CreateFinacleConnectionParams{
		TenantID:      tenantID,
		Host:          req.Host,
		AuthMethod:    req.AuthMethod,
		CredentialRef: req.CredentialRef,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create finacle connection: %v", err), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "finacle.connection.created", "finacle_connection", conn.ID.String(), map[string]any{
		"host":        conn.Host,
		"auth_method": conn.AuthMethod,
	})
	writeJSON(w, http.StatusCreated, newFinacleConnectionResponse(*conn))
}

func (s *Server) handleFinacleConnectionSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/finacle/connections/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "connection id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) == 2 && parts[1] == "test" {
		s.handleFinacleConnectionTest(w, r, id)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGetFinacleConnection(w, r, id)
	case http.MethodPatch:
		s.handlePatchFinacleConnection(w, r, id)
	case http.MethodDelete:
		s.handleDeleteFinacleConnection(w, r, id)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetFinacleConnection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	conn, err := s.store.GetFinacleConnection(r.Context(), id)
	if err != nil {
		s.logger.Warn("get finacle connection", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if conn == nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, newFinacleConnectionResponse(*conn))
}

func (s *Server) handlePatchFinacleConnection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	var req updateFinacleConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	conn, err := s.store.UpdateFinacleConnection(r.Context(), id, storage.UpdateFinacleConnectionParams{
		Host:          req.Host,
		AuthMethod:    req.AuthMethod,
		CredentialRef: req.CredentialRef,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("update finacle connection: %v", err), http.StatusBadRequest)
		return
	}
	if conn == nil {
		http.NotFound(w, r)
		return
	}
	s.recordAudit(r.Context(), principal, conn.TenantID, "finacle.connection.updated", "finacle_connection", conn.ID.String(), map[string]any{
		"host": conn.Host,
	})
	writeJSON(w, http.StatusOK, newFinacleConnectionResponse(*conn))
}

func (s *Server) handleDeleteFinacleConnection(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	conn, err := s.store.GetFinacleConnection(r.Context(), id)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if conn == nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.DeleteFinacleConnection(r.Context(), id); err != nil {
		s.logger.Warn("delete finacle connection", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, conn.TenantID, "finacle.connection.deleted", "finacle_connection", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleFinacleConnectionTest reaches out to Finacle and reports reachability
// + auth, then stamps last_sync_at / last_error on the connection row.
func (s *Server) handleFinacleConnectionTest(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	conn, err := s.store.GetFinacleConnection(r.Context(), id)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if conn == nil {
		http.NotFound(w, r)
		return
	}

	pingErr := s.finacleClientOrStub().Ping(r.Context(), *conn)
	now := time.Now().UTC()
	patch := storage.UpdateFinacleConnectionParams{LastSyncAt: &now}
	status := "ok"
	var msg string
	if pingErr != nil {
		errMsg := pingErr.Error()
		patch.LastError = &errMsg
		status = "failed"
		msg = errMsg
	} else {
		empty := ""
		patch.LastError = &empty
	}
	updated, _ := s.store.UpdateFinacleConnection(r.Context(), id, patch)
	s.recordAudit(r.Context(), principal, conn.TenantID, "finacle.connection.tested", "finacle_connection", id.String(), map[string]any{
		"status": status,
		"error":  msg,
	})

	resp := struct {
		Status     string                    `json:"status"`
		Message    string                    `json:"message,omitempty"`
		Connection finacleConnectionResponse `json:"connection"`
	}{
		Status:  status,
		Message: msg,
	}
	if updated != nil {
		resp.Connection = newFinacleConnectionResponse(*updated)
	} else {
		resp.Connection = newFinacleConnectionResponse(*conn)
	}
	httpStatus := http.StatusOK
	if pingErr != nil {
		httpStatus = http.StatusBadGateway
	}
	writeJSON(w, httpStatus, resp)
}

// ── shift configs ──────────────────────────────────────────────────────────

type finacleShiftConfigResponse struct {
	ID           string                     `json:"id"`
	TenantID     string                     `json:"tenant_id"`
	BranchID     *string                    `json:"branch_id,omitempty"`
	Model        string                     `json:"model"`
	Shifts       []storage.FinacleShiftBand `json:"shifts"`
	GraceMinutes int                        `json:"grace_minutes"`
	CreatedAt    string                     `json:"created_at"`
	UpdatedAt    string                     `json:"updated_at"`
}

func newFinacleShiftConfigResponse(c storage.FinacleShiftConfig) finacleShiftConfigResponse {
	out := finacleShiftConfigResponse{
		ID:           c.ID.String(),
		TenantID:     c.TenantID.String(),
		Model:        c.Model,
		Shifts:       c.Shifts,
		GraceMinutes: c.GraceMinutes,
		CreatedAt:    c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    c.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if c.BranchID.Valid {
		v := c.BranchID.String
		out.BranchID = &v
	}
	return out
}

type createFinacleShiftConfigRequest struct {
	TenantID     string                     `json:"tenant_id"`
	BranchID     *string                    `json:"branch_id,omitempty"`
	Model        string                     `json:"model"`
	Shifts       []storage.FinacleShiftBand `json:"shifts"`
	GraceMinutes int                        `json:"grace_minutes"`
}

type updateFinacleShiftConfigRequest struct {
	BranchID     *string                    `json:"branch_id,omitempty"`
	Model        *string                    `json:"model,omitempty"`
	Shifts       []storage.FinacleShiftBand `json:"shifts,omitempty"`
	GraceMinutes *int                       `json:"grace_minutes,omitempty"`
}

func (s *Server) handleFinacleShiftConfigs(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleListFinacleShiftConfigs(w, r)
	case http.MethodPost:
		s.handleCreateFinacleShiftConfig(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListFinacleShiftConfigs(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	cfgs, err := s.store.ListFinacleShiftConfigs(r.Context(), tenantID)
	if err != nil {
		s.logger.Warn("list finacle shift configs", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]finacleShiftConfigResponse, 0, len(cfgs))
	for _, c := range cfgs {
		resp = append(resp, newFinacleShiftConfigResponse(c))
	}
	writeJSON(w, http.StatusOK, struct {
		Configs []finacleShiftConfigResponse `json:"configs"`
	}{Configs: resp})
}

func (s *Server) handleCreateFinacleShiftConfig(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	var req createFinacleShiftConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	cfg, err := s.store.CreateFinacleShiftConfig(r.Context(), storage.CreateFinacleShiftConfigParams{
		TenantID:     tenantID,
		BranchID:     req.BranchID,
		Model:        req.Model,
		Shifts:       req.Shifts,
		GraceMinutes: req.GraceMinutes,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("create finacle shift config: %v", err), http.StatusBadRequest)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "finacle.shift_config.created", "finacle_shift_config", cfg.ID.String(), map[string]any{
		"model": cfg.Model,
	})
	writeJSON(w, http.StatusCreated, newFinacleShiftConfigResponse(*cfg))
}

func (s *Server) handleFinacleShiftConfigSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/finacle/shift-configs/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "shift config id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
			return
		}
		cfg, err := s.store.GetFinacleShiftConfig(r.Context(), id)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if cfg == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newFinacleShiftConfigResponse(*cfg))
	case http.MethodPatch:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		var req updateFinacleShiftConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		cfg, err := s.store.UpdateFinacleShiftConfig(r.Context(), id, storage.UpdateFinacleShiftConfigParams{
			BranchID:     req.BranchID,
			Model:        req.Model,
			Shifts:       req.Shifts,
			GraceMinutes: req.GraceMinutes,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("update finacle shift config: %v", err), http.StatusBadRequest)
			return
		}
		if cfg == nil {
			http.NotFound(w, r)
			return
		}
		s.recordAudit(r.Context(), principal, cfg.TenantID, "finacle.shift_config.updated", "finacle_shift_config", cfg.ID.String(), nil)
		writeJSON(w, http.StatusOK, newFinacleShiftConfigResponse(*cfg))
	case http.MethodDelete:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		cfg, err := s.store.GetFinacleShiftConfig(r.Context(), id)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if cfg == nil {
			http.NotFound(w, r)
			return
		}
		if err := s.store.DeleteFinacleShiftConfig(r.Context(), id); err != nil {
			s.logger.Warn("delete finacle shift config", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.recordAudit(r.Context(), principal, cfg.TenantID, "finacle.shift_config.deleted", "finacle_shift_config", id.String(), nil)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ── profiles ───────────────────────────────────────────────────────────────

type finacleProfileResponse struct {
	ID            string  `json:"id"`
	TenantID      string  `json:"tenant_id"`
	FinacleUID    string  `json:"finacle_uid"`
	BranchID      *string `json:"branch_id,omitempty"`
	Role          *string `json:"role,omitempty"`
	ShiftID       *string `json:"shift_id,omitempty"`
	Status        string  `json:"status"`
	LastRotatedAt *string `json:"last_rotated_at,omitempty"`
}

func newFinacleProfileResponse(p storage.FinacleProfile) finacleProfileResponse {
	out := finacleProfileResponse{
		ID:         p.ID.String(),
		TenantID:   p.TenantID.String(),
		FinacleUID: p.FinacleUID,
		Status:     p.Status,
	}
	if p.BranchID.Valid {
		v := p.BranchID.String
		out.BranchID = &v
	}
	if p.Role.Valid {
		v := p.Role.String
		out.Role = &v
	}
	if p.ShiftID.Valid {
		v := p.ShiftID.UUID.String()
		out.ShiftID = &v
	}
	if p.LastRotatedAt.Valid {
		v := p.LastRotatedAt.Time.UTC().Format(time.RFC3339)
		out.LastRotatedAt = &v
	}
	return out
}

type updateFinacleProfileRequest struct {
	BranchID *string `json:"branch_id,omitempty"`
	Role     *string `json:"role,omitempty"`
	ShiftID  *string `json:"shift_id,omitempty"`
	Status   *string `json:"status,omitempty"`
}

func (s *Server) handleFinacleProfiles(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(r.URL.Query().Get("tenant_id")))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	profs, total, err := s.store.ListFinacleProfiles(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Warn("list finacle profiles", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]finacleProfileResponse, 0, len(profs))
	for _, p := range profs {
		resp = append(resp, newFinacleProfileResponse(p))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[finacleProfileResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	})
}

func (s *Server) handleFinacleProfileSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/finacle/profiles/")
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "profile id must be a UUID", http.StatusBadRequest)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer, roleOperator, roleAdmin); !ok {
			return
		}
		prof, err := s.store.GetFinacleProfile(r.Context(), id)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if prof == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newFinacleProfileResponse(*prof))
	case http.MethodPatch:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		var req updateFinacleProfileRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		params := storage.UpdateFinacleProfileParams{
			BranchID: req.BranchID,
			Role:     req.Role,
			Status:   req.Status,
		}
		if req.ShiftID != nil {
			trimmed := strings.TrimSpace(*req.ShiftID)
			if trimmed == "" {
				zero := uuid.Nil
				params.ShiftID = &zero
			} else {
				parsed, err := uuid.Parse(trimmed)
				if err != nil {
					http.Error(w, "shift_id must be a UUID", http.StatusBadRequest)
					return
				}
				params.ShiftID = &parsed
			}
		}
		prof, err := s.store.UpdateFinacleProfile(r.Context(), id, params)
		if err != nil {
			http.Error(w, fmt.Sprintf("update finacle profile: %v", err), http.StatusBadRequest)
			return
		}
		if prof == nil {
			http.NotFound(w, r)
			return
		}
		s.recordAudit(r.Context(), principal, prof.TenantID, "finacle.profile.updated", "finacle_profile", prof.ID.String(), nil)
		writeJSON(w, http.StatusOK, newFinacleProfileResponse(*prof))
	default:
		w.Header().Set("Allow", "GET, PATCH")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// ── shift rotate trigger ──────────────────────────────────────────────────

type finacleShiftRotateRequest struct {
	TenantID  string `json:"tenant_id"`
	ShiftID   string `json:"shift_id"`
	Direction string `json:"direction"` // "enable" | "disable"
}

// handleFinacleShiftRotate is the admin manual override. The body of the work
// goes through triggerAutoRemediation (the 4-gate engine) so the override
// lands in remediation_approvals when policy demands it. We synthesise a
// compliance.Result whose RuleID encodes the shift+direction so the audit
// trail and approval rows are self-describing.
func (s *Server) handleFinacleShiftRotate(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req finacleShiftRotateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(strings.TrimSpace(req.TenantID))
	if err != nil {
		http.Error(w, "tenant_id must be a UUID", http.StatusBadRequest)
		return
	}
	shiftID, err := uuid.Parse(strings.TrimSpace(req.ShiftID))
	if err != nil {
		http.Error(w, "shift_id must be a UUID", http.StatusBadRequest)
		return
	}
	direction := strings.ToLower(strings.TrimSpace(req.Direction))
	if direction != "enable" && direction != "disable" {
		http.Error(w, `direction must be "enable" or "disable"`, http.StatusBadRequest)
		return
	}

	// Synthesised "rule" so triggerAutoRemediation has something to anchor.
	// The 4-gate engine queues a remediation_approvals row when severity
	// crosses the configured threshold; admins approve it via the existing
	// approvals API.
	ruleID := fmt.Sprintf("finacle.shift_rotate:%s:%s", shiftID, direction)
	result := compliance.Result{
		RuleID:    ruleID,
		Passed:    false, // false => the gate engine runs
		Severity:  "high",
		Details:   fmt.Sprintf("admin shift override: %s on shift %s", direction, shiftID),
		CheckedAt: time.Now().UTC(),
	}

	jobID := s.triggerAutoRemediation(r.Context(), tenantID, uuid.Nil, result, true)

	// Either way, also enqueue the actual rotate job. If the gate held it,
	// the approval API will drive a separate dispatch when an admin approves.
	rotateJobID, enqueueErr := s.enqueueFinacleShiftRotateJob(r.Context(), tenantID, shiftID, direction)
	if enqueueErr != nil {
		s.logger.Warn("enqueue finacle shift_rotate", zap.Error(enqueueErr))
	}

	s.recordAudit(r.Context(), principal, tenantID, "finacle.shift_rotate.requested", "finacle_shift_config", shiftID.String(), map[string]any{
		"direction":     direction,
		"approval_path": jobID != nil,
		"rotate_job_id": rotateJobID,
	})

	resp := struct {
		Approval    *string `json:"approval_path_job_id,omitempty"`
		RotateJobID *string `json:"rotate_job_id,omitempty"`
	}{}
	if jobID != nil {
		v := jobID.String()
		resp.Approval = &v
	}
	if rotateJobID != "" {
		resp.RotateJobID = &rotateJobID
	}
	writeJSON(w, http.StatusAccepted, resp)
}

// enqueueFinacleShiftRotateJob creates the storage.Job + worker.Task pair for
// the rotate job. Returns the job ID as a string for audit logging.
func (s *Server) enqueueFinacleShiftRotateJob(ctx context.Context, tenantID, shiftID uuid.UUID, direction string) (string, error) {
	if s.store == nil || s.worker == nil {
		return "", errors.New("store or worker unavailable")
	}
	payload := map[string]any{
		"tenant_id": tenantID.String(),
		"shift_id":  shiftID.String(),
		"direction": direction,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal rotate payload: %w", err)
	}
	job := &storage.Job{
		Type:     JobTypeFinacleShiftRotate,
		TenantID: tenantID,
		Payload:  payloadBytes,
		Status:   storage.JobStatusQueued,
	}
	job, err = s.store.CreateJob(ctx, job, nil)
	if err != nil {
		return "", fmt.Errorf("create rotate job: %w", err)
	}
	task := worker.Task{
		Name:         fmt.Sprintf("finacle-shift-rotate-%s", job.ID),
		Job:          s.buildFinacleShiftRotateExecution(job.ID, tenantID, shiftID, direction),
		MaxAttempts:  3,
		RetryBackoff: s.cfg.Worker.RetryBackoff,
	}
	if err := s.worker.Enqueue(task); err != nil {
		_ = s.store.UpdateJobStatus(ctx, job.ID, storage.JobStatusFailed, "failed to enqueue rotate", nil)
		return "", fmt.Errorf("enqueue rotate: %w", err)
	}
	return job.ID.String(), nil
}

// ── job handlers ───────────────────────────────────────────────────────────

// handleFinacleSyncJob pulls a profile snapshot from Finacle and upserts it.
// On Finacle errors the connection's last_error is stamped but the worker
// returns nil so the job is not retried into oblivion (the next scheduled
// invocation will retry).
func (s *Server) handleFinacleSyncJob(ctx context.Context, job *storage.Job) error {
	if s == nil || s.store == nil {
		return errors.New("server or store unavailable")
	}
	var payload struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		return fmt.Errorf("decode finacle.sync payload: %w", err)
	}
	connID, err := uuid.Parse(strings.TrimSpace(payload.ConnectionID))
	if err != nil {
		return fmt.Errorf("connection_id must be UUID: %w", err)
	}
	conn, err := s.store.GetFinacleConnection(ctx, connID)
	if err != nil {
		return fmt.Errorf("load finacle connection: %w", err)
	}
	if conn == nil {
		return fmt.Errorf("finacle connection %s not found", connID)
	}

	profiles, err := s.finacleClientOrStub().ListProfiles(ctx, *conn)
	now := time.Now().UTC()
	if err != nil {
		errMsg := err.Error()
		_, _ = s.store.UpdateFinacleConnection(ctx, connID, storage.UpdateFinacleConnectionParams{
			LastSyncAt: &now,
			LastError:  &errMsg,
		})
		s.logger.Warn("finacle.sync failed", zap.String("connection_id", connID.String()), zap.Error(err))
		return nil
	}

	for _, p := range profiles {
		if _, err := s.store.UpsertFinacleProfile(ctx, p); err != nil {
			s.logger.Warn("upsert finacle profile", zap.String("uid", p.FinacleUID), zap.Error(err))
		}
	}
	empty := ""
	_, _ = s.store.UpdateFinacleConnection(ctx, connID, storage.UpdateFinacleConnectionParams{
		LastSyncAt: &now,
		LastError:  &empty,
	})
	s.recordAudit(ctx, s.systemActor(), conn.TenantID, "finacle.sync.completed", "finacle_connection", connID.String(), map[string]any{
		"profiles": len(profiles),
	})
	return nil
}

// buildFinacleShiftRotateExecution returns the worker function that performs
// the rotation. Behaviour:
//   - direction=enable, Finacle reachable: enable each profile, mark active.
//   - direction=enable, Finacle unreachable: skip enables (fail closed), raise
//     critical alert. Mid-session staff are unaffected — disables run later.
//   - direction=disable, Finacle reachable: disable each profile, mark revoked.
//   - direction=disable, Finacle unreachable: still mark revoked locally so the
//     control plane considers access withdrawn (fail open). The next
//     reachability success retries the upstream call. Raises a high alert.
func (s *Server) buildFinacleShiftRotateExecution(jobID, tenantID, shiftID uuid.UUID, direction string) func(context.Context) error {
	return func(ctx context.Context) error {
		if s.store == nil {
			return errors.New("store unavailable")
		}
		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "rotating shift "+direction, nil); err != nil {
			return fmt.Errorf("update rotate job status: %w", err)
		}

		profiles, err := s.store.ListFinacleProfilesByShift(ctx, shiftID)
		if err != nil {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, "list profiles: "+err.Error(), nil)
			return fmt.Errorf("list profiles by shift: %w", err)
		}

		conns, err := s.store.ListFinacleConnections(ctx, tenantID)
		if err != nil {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, "list connections: "+err.Error(), nil)
			return fmt.Errorf("list finacle connections: %w", err)
		}
		if len(conns) == 0 {
			_ = s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, "no finacle connection configured", nil)
			return errors.New("no finacle connection configured")
		}
		conn := conns[0]

		client := s.finacleClientOrStub()
		var failures, successes int
		var alertSeverity string
		var alertSummary string

		for _, prof := range profiles {
			var callErr error
			if direction == "enable" {
				callErr = client.EnableProfile(ctx, conn, prof.FinacleUID)
				if callErr != nil {
					// fail closed — do NOT mark active. Skip this profile, alert.
					failures++
					continue
				}
				_ = s.store.MarkFinacleProfileRotated(ctx, prof.ID, "active")
				successes++
			} else {
				callErr = client.DisableProfile(ctx, conn, prof.FinacleUID)
				// fail open — always mark revoked locally even when the upstream
				// call failed. The fact that we recorded the revoke here keeps
				// the control plane's view aligned with operator intent; the
				// connector retry loop will catch up the upstream state.
				_ = s.store.MarkFinacleProfileRotated(ctx, prof.ID, "revoked")
				if callErr != nil {
					failures++
				} else {
					successes++
				}
			}
		}

		if failures > 0 {
			if direction == "enable" {
				alertSeverity = "critical"
				alertSummary = fmt.Sprintf("Finacle unreachable on incoming staff shift %s: %d enable calls failed (fail-closed)", shiftID, failures)
			} else {
				alertSeverity = "high"
				alertSummary = fmt.Sprintf("Finacle unreachable on outgoing staff shift %s: %d disable calls failed (revoked locally)", shiftID, failures)
			}
			s.emitFinacleAlert(ctx, tenantID, alertSeverity, alertSummary, map[string]any{
				"shift_id":  shiftID.String(),
				"direction": direction,
				"failures":  failures,
				"successes": successes,
			})
		}

		finalStatus := storage.JobStatusSucceeded
		msg := fmt.Sprintf("rotated %d profiles (failures=%d)", successes, failures)
		if direction == "enable" && failures > 0 {
			// fail-closed enables are treated as a partial failure of the job
			// itself so retries can pick up profiles when Finacle recovers.
			finalStatus = storage.JobStatusFailed
		}
		if err := s.store.UpdateJobStatus(ctx, jobID, finalStatus, msg, nil); err != nil {
			return fmt.Errorf("update rotate job status: %w", err)
		}
		s.recordAudit(ctx, s.systemActor(), tenantID, "finacle.shift_rotate.completed", "finacle_shift_config", shiftID.String(), map[string]any{
			"direction": direction,
			"failures":  failures,
			"successes": successes,
		})
		if direction == "enable" && failures > 0 {
			return fmt.Errorf("%d enable calls failed", failures)
		}
		return nil
	}
}

// emitFinacleAlert is the thin wrapper around the alerts store. We use a
// best-effort dedup key keyed on shift+direction so repeated boundary failures
// do not spam.
func (s *Server) emitFinacleAlert(ctx context.Context, tenantID uuid.UUID, severity, summary string, contextPayload map[string]any) {
	if s == nil || s.store == nil {
		return
	}
	dedup := fmt.Sprintf("finacle:%v:%v", contextPayload["shift_id"], contextPayload["direction"])
	_, err := s.store.CreateAlert(ctx, storage.CreateAlertParams{
		TenantID: tenantID,
		Source:   "finacle",
		Severity: severity,
		Title:    "Finacle shift rotation",
		Summary:  summary,
		DedupKey: dedup,
		Context:  contextPayload,
	})
	if err != nil && !errors.Is(err, storage.ErrAlertDeduped) {
		s.logger.Warn("emit finacle alert", zap.Error(err))
	}
}
