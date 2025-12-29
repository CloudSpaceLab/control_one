package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
)

// Store defines persistence operations used by the server.
type Store interface {
	CreateTenant(context.Context, *storage.Tenant) (*storage.Tenant, error)
	ListTenants(context.Context, string, int, int) ([]storage.Tenant, int, error)
	GetTenant(context.Context, uuid.UUID) (*storage.Tenant, error)
	EnsureTenant(context.Context, uuid.UUID, string) (*storage.Tenant, error)
	GetNodeByHostname(context.Context, uuid.UUID, string) (*storage.Node, error)
	CreateNode(context.Context, *storage.Node) (*storage.Node, error)
	ListNodes(context.Context, uuid.UUID, string, int, int) ([]storage.Node, int, error)
	GetNode(context.Context, uuid.UUID) (*storage.Node, error)
	GetUserByExternalID(context.Context, string) (*storage.User, error)
	ListUserRoles(context.Context, uuid.UUID) ([]string, error)
	ListJobs(context.Context, uuid.UUID, string, storage.JobStatus, int, int) ([]storage.Job, int, error)
	CreateJob(context.Context, *storage.Job, *storage.JobEvent) (*storage.Job, error)
	UpdateJobStatus(context.Context, uuid.UUID, storage.JobStatus, string, map[string]any) error
	GetJob(context.Context, uuid.UUID) (*storage.Job, error)
	ListJobEvents(context.Context, uuid.UUID) ([]storage.JobEvent, error)
	ListProvisioningTemplates(context.Context, storage.ProvisioningTemplateFilter, int, int) ([]storage.ProvisioningTemplate, int, error)
	CreateProvisioningTemplate(context.Context, *storage.ProvisioningTemplate) (*storage.ProvisioningTemplate, error)
	GetProvisioningTemplate(context.Context, uuid.UUID) (*storage.ProvisioningTemplate, error)
	CreateProvisioningTemplateVersion(context.Context, storage.CreateTemplateVersionParams) (*storage.ProvisioningTemplateVersion, error)
	PromoteProvisioningTemplateVersion(context.Context, uuid.UUID, int) (*storage.ProvisioningTemplateVersion, error)
	GetProvisioningTemplateVersion(context.Context, uuid.UUID, int) (*storage.ProvisioningTemplateVersion, error)
	GetPromotedProvisioningTemplateVersion(context.Context, uuid.UUID) (*storage.ProvisioningTemplateVersion, error)
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, allowedRoles ...string) (*auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return nil, false
	}

	if principal.Type == "agent" {
		return principal, true
	}

	if len(allowedRoles) == 0 {
		return principal, true
	}

	for _, role := range principal.Roles {
		for _, allowed := range allowedRoles {
			if strings.EqualFold(strings.TrimSpace(role), strings.TrimSpace(allowed)) {
				return principal, true
			}
		}
	}

	for _, role := range principal.Roles {
		if strings.EqualFold(strings.TrimSpace(role), roleAdmin) {
			return principal, true
		}
	}

	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	return nil, false
}

type registerNodeRequest struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	TenantName     string    `json:"tenant_name"`
	Hostname       string    `json:"hostname"`
	OS             *string   `json:"os"`
	Arch           *string   `json:"arch"`
	PublicIP       *string   `json:"public_ip"`
	BootstrapToken string    `json:"bootstrap_token"`
}

func (r registerNodeRequest) validate() error {
	if strings.TrimSpace(r.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	if strings.TrimSpace(r.BootstrapToken) == "" {
		return fmt.Errorf("bootstrap_token is required")
	}
	if r.TenantID == uuid.Nil && strings.TrimSpace(r.TenantName) == "" {
		return fmt.Errorf("tenant_name is required when tenant_id is not provided")
	}
	return nil
}

type registerNodeResponse struct {
	NodeID            string           `json:"node_id"`
	TenantID          string           `json:"tenant_id"`
	Intervals         map[string]int64 `json:"intervals"`
	ProvisioningHints string           `json:"provisioning_hints"`
}

func defaultNodeIntervals() map[string]int64 {
	return map[string]int64{
		"heartbeat":   60,
		"scan":        300,
		"provision":   3600,
		"policy_sync": 600,
	}
}

func (s *Server) handleNodeRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req registerNodeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenantID := req.TenantID
	if tenantID == uuid.Nil {
		if s.cfg.Registration.DefaultTenantID != "" {
			parsed, err := uuid.Parse(s.cfg.Registration.DefaultTenantID)
			if err != nil {
				http.Error(w, "invalid default tenant id", http.StatusInternalServerError)
				return
			}
			tenantID = parsed
		}
	}

	if tenantID == uuid.Nil {
		if strings.TrimSpace(req.TenantName) == "" {
			http.Error(w, "tenant_id or tenant_name required", http.StatusBadRequest)
			return
		}
		tenantID = uuid.New()
	}

	var bootstrapAllowed bool
	if len(s.cfg.Registration.BootstrapTokens) == 0 {
		bootstrapAllowed = true
	} else {
		token := strings.TrimSpace(req.BootstrapToken)
		for _, t := range s.cfg.Registration.BootstrapTokens {
			if token == strings.TrimSpace(t) {
				bootstrapAllowed = true
				break
			}
		}
	}
	if !bootstrapAllowed {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	if s.store == nil {
		http.Error(w, "registration store unavailable", http.StatusServiceUnavailable)
		return
	}

	tenant, err := s.store.EnsureTenant(r.Context(), tenantID, req.TenantName)
	if err != nil {
		s.logger.Error("ensure tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hostname := strings.TrimSpace(req.Hostname)
	if existing, err := s.store.GetNodeByHostname(r.Context(), tenant.ID, hostname); err != nil {
		s.logger.Error("lookup existing node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	} else if existing != nil {
		s.logger.Info("node already registered",
			zap.String("tenant_id", tenant.ID.String()),
			zap.String("node_id", existing.ID.String()),
			zap.String("hostname", hostname),
		)
		respondRegistration(w, s.logger, registerNodeResponse{
			NodeID:            existing.ID.String(),
			TenantID:          tenant.ID.String(),
			Intervals:         defaultNodeIntervals(),
			ProvisioningHints: tenant.Name,
		})
		return
	}

	node := &storage.Node{
		ID:       uuid.New(),
		TenantID: tenant.ID,
		Hostname: hostname,
		OS:       toNullString(req.OS),
		Arch:     toNullString(req.Arch),
		PublicIP: toNullString(req.PublicIP),
	}

	created, err := s.store.CreateNode(r.Context(), node)
	if err != nil {
		s.logger.Error("register node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respondRegistration(w, s.logger, registerNodeResponse{
		NodeID:            created.ID.String(),
		TenantID:          tenant.ID.String(),
		Intervals:         defaultNodeIntervals(),
		ProvisioningHints: tenant.Name,
	})
}

func respondRegistration(w http.ResponseWriter, logger *zap.Logger, resp registerNodeResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil && logger != nil {
		logger.Warn("encode registration response", zap.Error(err))
	}
}

const (
	defaultListLimit = 100
	maxListLimit     = 500

	roleViewer   = "viewer"
	roleOperator = "operator"
	roleAdmin    = "admin"

	requestIDHeader = "X-Request-Id"
)

type contextKey string

const (
	contextKeyRequestID contextKey = "controlone.request_id"
)

func parseLimitOffset(values map[string][]string) (int, int, error) {
	limit := defaultListLimit
	if v := firstQueryValue(values, "limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return 0, 0, fmt.Errorf("invalid limit")
		}
		if parsed > maxListLimit {
			parsed = maxListLimit
		}
		limit = parsed
	}

	offset := 0
	if v := firstQueryValue(values, "offset"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("invalid offset")
		}
		offset = parsed
	}

	return limit, offset, nil
}

func firstQueryValue(values map[string][]string, key string) string {
	if values == nil {
		return ""
	}
	if vals, ok := values[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

func paginate[T any](items []T, offset, limit int) []T {
	if offset >= len(items) {
		return []T{}
	}
	end := offset + limit
	if end > len(items) {
		end = len(items)
	}
	return items[offset:end]
}

// TaskQueue defines minimal worker manager contract for enqueuing asynchronous tasks.
type TaskQueue interface {
	Enqueue(worker.Task) error
}

// Server wraps the HTTP server lifecycle for the control plane API.
type Server struct {
	logger             *zap.Logger
	cfg                *config.Config
	http               *http.Server
	store              Store
	worker             TaskQueue
	authMW             *auth.Middleware
	baseRouter         *http.ServeMux
	jobHandlers        map[string]jobHandler
	provisioningEngine *provisioning.Engine
	complianceEngine   *compliance.Engine
}

// Handler exposes the HTTP handler for testing.
func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

func (s *Server) registerRoutes() {
	s.baseRouter.HandleFunc("/api/v1/ping", s.handlePing)
	s.baseRouter.HandleFunc("/api/v1/nodes", s.handleNodesCollection)
	s.baseRouter.HandleFunc("/api/v1/nodes/", s.handleNodeResource)
	s.baseRouter.HandleFunc("/api/v1/tenants", s.handleTenantsCollection)
	s.baseRouter.HandleFunc("/api/v1/jobs", s.handleJobsCollection)
	s.baseRouter.HandleFunc("/api/v1/jobs/", s.handleJobResource)
	s.baseRouter.HandleFunc("/api/v1/templates", s.handleTemplatesCollection)
	s.baseRouter.HandleFunc("/api/v1/templates/", s.handleTemplateSubroutes)
	s.baseRouter.HandleFunc("/api/v1/me", s.handleProfile)
	s.baseRouter.HandleFunc("/api/v1/register", s.handleNodeRegistration)
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	resp := profileResponse{
		Subject: principal.Subject,
		Name:    principal.Name,
		Email:   principal.Email,
		Type:    principal.Type,
		Roles:   principal.Roles,
		Groups:  principal.Groups,
	}

	if s.store != nil && strings.TrimSpace(principal.Subject) != "" {
		user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject)
		if err != nil {
			s.logger.Warn("lookup profile user", zap.Error(err))
		} else if user != nil {
			resp.User = &profileUserDetails{
				ID:          user.ID.String(),
				DisplayName: nullStringPtr(user.DisplayName),
				Email:       nullStringPtr(user.Email),
				CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339),
			}
			if roles, err := s.store.ListUserRoles(r.Context(), user.ID); err != nil {
				s.logger.Warn("list profile roles", zap.Error(err))
			} else if len(roles) > 0 {
				resp.StoredRoles = append([]string{}, roles...)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode profile response", zap.Error(err))
	}
}

type profileResponse struct {
	Subject     string              `json:"subject"`
	Name        string              `json:"name"`
	Email       string              `json:"email"`
	Type        string              `json:"type"`
	Roles       []string            `json:"roles"`
	Groups      []string            `json:"groups"`
	StoredRoles []string            `json:"stored_roles,omitempty"`
	User        *profileUserDetails `json:"user,omitempty"`
}

type profileUserDetails struct {
	ID          string  `json:"id"`
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r)
	if !ok {
		return
	}

	resp := map[string]any{
		"message":   "pong",
		"principal": principal,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode ping response", zap.Error(err))
	}
}

func (s *Server) handleTenantsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTenants(w, r)
	case http.MethodPost:
		s.handleCreateTenant(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "tenant store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	namePrefix := strings.TrimSpace(r.URL.Query().Get("name_prefix"))

	tenants, total, err := s.store.ListTenants(r.Context(), namePrefix, limit, offset)
	if err != nil {
		s.logger.Error("list tenants", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]tenantResponse, 0, len(tenants))
	for _, t := range tenants {
		resp = append(resp, tenantResponseFromModel(t))
	}

	payload := paginatedResponse[tenantResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Warn("encode tenants response", zap.Error(err))
	}
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "tenant store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
		return
	}

	var req createTenantRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenant := &storage.Tenant{
		Name: strings.TrimSpace(req.Name),
	}

	created, err := s.store.CreateTenant(r.Context(), tenant)
	if err != nil {
		s.logger.Error("create tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(tenantResponseFromModel(*created)); err != nil {
		s.logger.Warn("encode tenant response", zap.Error(err))
	}
}

type createTenantRequest struct {
	Name string `json:"name"`
}

func (r createTenantRequest) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

type tenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func tenantResponseFromModel(t storage.Tenant) tenantResponse {
	return tenantResponse{
		ID:        t.ID.String(),
		Name:      t.Name,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func (s *Server) handleNodesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListNodes(w, r)
	case http.MethodPost:
		s.handleCreateNode(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "node store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	hostnamePrefix := strings.TrimSpace(r.URL.Query().Get("hostname_prefix"))

	nodes, total, err := s.store.ListNodes(r.Context(), tenantID, hostnamePrefix, limit, offset)
	if err != nil {
		s.logger.Error("list nodes", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]nodeResponse, 0, len(nodes))
	for _, n := range nodes {
		resp = append(resp, nodeResponseFromModel(n))
	}

	payload := paginatedResponse[nodeResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Warn("encode nodes response", zap.Error(err))
	}
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "node store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleOperator, roleAdmin); !ok {
		return
	}

	var req createNodeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("get tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		http.Error(w, "tenant not found", http.StatusBadRequest)
		return
	}

	node := &storage.Node{
		TenantID: tenantID,
		Hostname: strings.TrimSpace(req.Hostname),
		OS:       toNullString(req.OS),
		Arch:     toNullString(req.Arch),
		PublicIP: toNullString(req.PublicIP),
	}

	created, err := s.store.CreateNode(r.Context(), node)
	if err != nil {
		s.logger.Error("create node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(nodeResponseFromModel(*created)); err != nil {
		s.logger.Warn("encode node response", zap.Error(err))
	}
}

func (s *Server) handleNodeResource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		http.Error(w, "node store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	idStr := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/api/v1/nodes/")
	nodeID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodeResponseFromModel(*node)); err != nil {
		s.logger.Warn("encode node response", zap.Error(err))
	}
}

type createNodeRequest struct {
	TenantID string  `json:"tenant_id"`
	Hostname string  `json:"hostname"`
	OS       *string `json:"os"`
	Arch     *string `json:"arch"`
	PublicIP *string `json:"public_ip"`
}

func (r createNodeRequest) validate() error {
	if _, err := uuid.Parse(r.TenantID); err != nil {
		return fmt.Errorf("invalid tenant_id: %w", err)
	}
	if strings.TrimSpace(r.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	return nil
}

type nodeResponse struct {
	ID        string  `json:"id"`
	TenantID  string  `json:"tenant_id"`
	Hostname  string  `json:"hostname"`
	OS        *string `json:"os,omitempty"`
	Arch      *string `json:"arch,omitempty"`
	PublicIP  *string `json:"public_ip,omitempty"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

func nodeResponseFromModel(n storage.Node) nodeResponse {
	return nodeResponse{
		ID:        n.ID.String(),
		TenantID:  n.TenantID.String(),
		Hostname:  n.Hostname,
		OS:        nullStringPtr(n.OS),
		Arch:      nullStringPtr(n.Arch),
		PublicIP:  nullStringPtr(n.PublicIP),
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func toNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	value := ns.String
	return &value
}

func (s *Server) buildJobExecution(jobID uuid.UUID, jobType string) func(context.Context) error {
	return func(ctx context.Context) error {
		s.configureJobIntegrations()
		handler, ok := s.jobHandlers[jobType]
		if !ok {
			return fmt.Errorf("no handler registered for job type %s", jobType)
		}

		finish := metricsTrackJob(jobType)

		job, err := s.store.GetJob(ctx, jobID)
		if err != nil {
			finish(metricsStatusError)
			return fmt.Errorf("load job: %w", err)
		}
		if job == nil {
			finish(metricsStatusFailure)
			return fmt.Errorf("job %s not found", jobID)
		}

		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, "job started", map[string]any{"started_at": time.Now()}); err != nil {
			finish(metricsStatusError)
			return fmt.Errorf("update job running: %w", err)
		}

		if err := handler(ctx, job); err != nil {
			s.logger.Error("job execution failed", zap.String("job_type", jobType), zap.String("job_id", jobID.String()), zap.Error(err))
			finish(metricsStatusFailure)
			if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusFailed, err.Error(), map[string]any{"finished_at": time.Now()}); err != nil {
				s.logger.Error("update job failed", zap.Error(err))
			}
			return err
		}

		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, "job completed", map[string]any{"finished_at": time.Now()}); err != nil {
			finish(metricsStatusError)
			return fmt.Errorf("update job success: %w", err)
		}

		finish(metricsStatusSuccess)
		return nil
	}
}

// New constructs a Server with default routes and middleware.
func New(logger *zap.Logger, cfg *config.Config, store Store, worker TaskQueue) *Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	if cfg.Observability.EnableMetrics {
		path := cfg.Observability.MetricsPath
		if path == "" {
			path = "/metrics"
		}
		initServerMetrics()
		mux.Handle(path, promhttp.Handler())
	}

	var identityStore auth.IdentityStore
	if store != nil {
		if typed, ok := store.(auth.IdentityStore); ok {
			identityStore = typed
		}
	}

	authMW := auth.NewMiddleware(logger, cfg.TLS.RequireClientTLS, cfg.Auth, identityStore)

	httpServer := &http.Server{
		Addr: cfg.HTTP.Address,
		Handler: loggingMiddleware(logger,
			requestIDMiddleware(authMW.Wrap(mux))),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	s := &Server{logger: logger, cfg: cfg, http: httpServer, store: store, worker: worker, authMW: authMW, baseRouter: mux}
	s.configureJobIntegrations()
	s.registerRoutes()
	return s
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	if !s.cfg.TLS.Enabled {
		return s.http.ListenAndServe()
	}

	tlsConfig, err := s.buildTLSConfig()
	if err != nil {
		return err
	}
	s.http.TLSConfig = tlsConfig

	return s.http.ListenAndServeTLS(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop(ctx context.Context) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.http.Shutdown(shutdownCtx)
}

func loggingMiddleware(logger *zap.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		fields := []zap.Field{
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", ww.status),
			zap.Int64("bytes", ww.bytes),
			zap.Duration("duration", time.Since(start)),
		}
		if requestID, ok := requestIDFromContext(r.Context()); ok {
			fields = append(fields, zap.String("request_id", requestID))
		}
		logger.Info("http request",
			fields...,
		)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), contextKeyRequestID, requestID)
		w.Header().Set(requestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	val := ctx.Value(contextKeyRequestID)
	if requestID, ok := val.(string); ok && strings.TrimSpace(requestID) != "" {
		return requestID, true
	}
	return "", false
}

func (s *Server) buildTLSConfig() (*tls.Config, error) {
	if s.cfg.TLS.CertFile == "" || s.cfg.TLS.KeyFile == "" {
		return nil, fmt.Errorf("tls enabled but cert/key not configured")
	}

	cert, err := tls.LoadX509KeyPair(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server key pair: %w", err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	if s.cfg.TLS.RequireClientTLS {
		if s.cfg.TLS.ClientCAFile == "" {
			return nil, fmt.Errorf("client TLS required but client_ca_file missing")
		}
		caPEM, err := os.ReadFile(s.cfg.TLS.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("append client ca certs failed")
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return tlsCfg, nil
}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}
