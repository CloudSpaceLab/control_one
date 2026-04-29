package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// --- Token CRUD types ---

type createEnrollmentTokenRequest struct {
	Name         string            `json:"name"`
	TenantID     uuid.UUID         `json:"tenant_id"`
	MaxNodes     int               `json:"max_nodes"`
	TTL          string            `json:"ttl"`
	Labels       map[string]string `json:"labels"`
	Capabilities []string          `json:"capabilities"`
}

type enrollmentTokenResponse struct {
	ID            string            `json:"id"`
	TenantID      string            `json:"tenant_id"`
	Name          string            `json:"name"`
	Token         string            `json:"token,omitempty"`
	MaxNodes      int               `json:"max_nodes"`
	NodesEnrolled int               `json:"nodes_enrolled"`
	Labels        map[string]string `json:"labels"`
	Capabilities  []string          `json:"capabilities"`
	ExpiresAt     string            `json:"expires_at"`
	RevokedAt     *string           `json:"revoked_at,omitempty"`
	CreatedBy     *string           `json:"created_by,omitempty"`
	CreatedAt     string            `json:"created_at"`
}

// --- Enroll types ---

type enrollRequest struct {
	Token       string `json:"token"`
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	PublicIP    string `json:"public_ip"`
	Fingerprint string `json:"fingerprint"`
	MachineID   string `json:"machine_id"`
}

type enrollResponse struct {
	NodeID   string       `json:"node_id"`
	TenantID string       `json:"tenant_id"`
	TLS      enrollTLS    `json:"tls"`
	Config   enrollConfig `json:"config"`
}

type enrollTLS struct {
	CertPEM   string `json:"cert_pem"`
	KeyPEM    string `json:"key_pem"`
	CACertPEM string `json:"ca_cert_pem"`
}

type enrollConfig struct {
	Intervals map[string]int64 `json:"intervals"`
}

// --- Token CRUD handlers ---

func (s *Server) handleEnrollmentTokensCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.handleListEnrollmentTokens(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleCreateEnrollmentToken(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleEnrollmentTokenSubroutes(w http.ResponseWriter, r *http.Request) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/enrollment-tokens/")
	trimmed = strings.Trim(trimmed, "/")
	if trimmed == "" {
		http.NotFound(w, r)
		return
	}

	tokenID, err := uuid.Parse(trimmed)
	if err != nil {
		http.Error(w, "invalid token id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.handleRevokeEnrollmentToken(w, r, tokenID)
	default:
		w.Header().Set("Allow", http.MethodDelete)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListEnrollmentTokens(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	tenantIDStr := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantIDStr == "" {
		http.Error(w, "tenant_id query parameter is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantIDStr)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tokens, total, err := s.store.ListEnrollmentTokens(r.Context(), tenantID, limit, offset)
	if err != nil {
		s.logger.Error("list enrollment tokens", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respItems := make([]enrollmentTokenResponse, 0, len(tokens))
	for _, t := range tokens {
		respItems = append(respItems, newEnrollmentTokenResponse(t, ""))
	}

	resp := paginatedResponse[enrollmentTokenResponse]{
		Data:       respItems,
		Pagination: newPaginationMeta(total, limit, offset, len(respItems)),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleCreateEnrollmentToken(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req createEnrollmentTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if req.TenantID == uuid.Nil {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.TTL) == "" {
		http.Error(w, "ttl is required", http.StatusBadRequest)
		return
	}

	ttl, err := time.ParseDuration(req.TTL)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid ttl: %v", err), http.StatusBadRequest)
		return
	}
	if ttl <= 0 {
		http.Error(w, "ttl must be positive", http.StatusBadRequest)
		return
	}

	// Generate raw token: cot_ + 32 random hex chars
	rawBytes := make([]byte, 16)
	if _, err := rand.Read(rawBytes); err != nil {
		s.logger.Error("generate enrollment token", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	rawToken := "cot_" + hex.EncodeToString(rawBytes)

	// Hash with SHA-256
	h := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(h[:])

	var createdBy *uuid.UUID
	if principal.Subject != "" {
		if id, err := uuid.Parse(principal.Subject); err == nil {
			createdBy = &id
		}
	}

	params := storage.CreateEnrollmentTokenParams{
		TenantID:     req.TenantID,
		Name:         req.Name,
		TokenHash:    tokenHash,
		MaxNodes:     req.MaxNodes,
		Labels:       req.Labels,
		Capabilities: req.Capabilities,
		ExpiresAt:    time.Now().Add(ttl),
		CreatedBy:    createdBy,
	}

	created, err := s.store.CreateEnrollmentToken(r.Context(), params)
	if err != nil {
		s.logger.Error("create enrollment token", zap.Error(err))
		http.Error(w, fmt.Sprintf("create enrollment token failed: %v", err), http.StatusBadRequest)
		return
	}

	// Return the raw token only on creation
	resp := newEnrollmentTokenResponse(*created, rawToken)
	writeJSON(w, http.StatusCreated, resp)

	s.recordAudit(r.Context(), principal, req.TenantID, "enrollment_token.created", "enrollment_token", created.ID.String(), map[string]any{
		"name":      req.Name,
		"max_nodes": req.MaxNodes,
	})
}

func (s *Server) handleRevokeEnrollmentToken(w http.ResponseWriter, r *http.Request, tokenID uuid.UUID) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	if err := s.store.RevokeEnrollmentToken(r.Context(), tokenID); err != nil {
		s.logger.Error("revoke enrollment token", zap.Error(err))
		http.Error(w, fmt.Sprintf("revoke failed: %v", err), http.StatusBadRequest)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked"})

	s.recordAudit(r.Context(), principal, uuid.Nil, "enrollment_token.revoked", "enrollment_token", tokenID.String(), map[string]any{})
}

// --- Node enrollment handler (public, no auth) ---

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}

	var req enrollRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Token) == "" {
		http.Error(w, "token is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Hostname) == "" {
		http.Error(w, "hostname is required", http.StatusBadRequest)
		return
	}

	// Hash the provided token and look it up
	h := sha256.Sum256([]byte(strings.TrimSpace(req.Token)))
	tokenHash := hex.EncodeToString(h[:])

	token, err := s.store.GetEnrollmentTokenByHash(r.Context(), tokenHash)
	if err != nil {
		s.logger.Error("lookup enrollment token", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if token == nil {
		http.Error(w, "invalid enrollment token", http.StatusUnauthorized)
		return
	}

	// Validate: not revoked
	if token.RevokedAt.Valid {
		http.Error(w, "enrollment token has been revoked", http.StatusUnauthorized)
		return
	}

	// Validate: not expired
	if time.Now().After(token.ExpiresAt) {
		http.Error(w, "enrollment token has expired", http.StatusUnauthorized)
		return
	}

	// Validate: max_nodes (0 = unlimited)
	if token.MaxNodes > 0 && token.NodesEnrolled >= token.MaxNodes {
		http.Error(w, "enrollment token has reached its node limit", http.StatusForbidden)
		return
	}

	// Ensure tenant exists
	tenant, err := s.store.EnsureTenant(r.Context(), token.TenantID, "")
	if err != nil {
		s.logger.Error("ensure tenant for enrollment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hostname := strings.TrimSpace(req.Hostname)
	machineID := strings.TrimSpace(req.MachineID)

	// Prefer machine_id dedup (stable across hostname changes / reimages).
	// Fall back to hostname for legacy agents that don't send machine_id.
	var existing *storage.Node
	if machineID != "" {
		found, lookupErr := s.store.GetNodeByMachineID(r.Context(), tenant.ID, machineID)
		if lookupErr != nil {
			s.logger.Error("lookup existing node by machine id", zap.Error(lookupErr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		existing = found
	}
	if existing == nil {
		found, lookupErr := s.store.GetNodeByHostname(r.Context(), tenant.ID, hostname)
		if lookupErr != nil {
			s.logger.Error("lookup existing node for enrollment", zap.Error(lookupErr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		existing = found
	}

	if existing != nil {
		s.logger.Info("node already enrolled",
			zap.String("tenant_id", tenant.ID.String()),
			zap.String("node_id", existing.ID.String()),
			zap.String("hostname", hostname),
		)

		// Re-enrollment: if we matched by machine_id but hostname changed
		// (common after reimage), update the hostname + backfill machine_id.
		needsUpdate := false
		if hostname != "" && existing.Hostname != hostname {
			existing.Hostname = hostname
			needsUpdate = true
		}
		if machineID != "" && (!existing.MachineID.Valid || existing.MachineID.String != machineID) {
			existing.MachineID = toNullString(&machineID)
			needsUpdate = true
		}
		if needsUpdate {
			if _, updErr := s.store.UpdateNode(r.Context(), existing); updErr != nil {
				s.logger.Warn("update node on re-enrollment", zap.Error(updErr))
			}
		}

		// Generate certs for re-enrollment
		certPEM, keyPEM, caCertPEM, certErr := s.generateClientCertificate(hostname, existing.ID.String())
		if certErr != nil {
			s.logger.Error("generate client certificate for re-enrollment", zap.Error(certErr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, enrollResponse{
			NodeID:   existing.ID.String(),
			TenantID: tenant.ID.String(),
			TLS: enrollTLS{
				CertPEM:   string(certPEM),
				KeyPEM:    string(keyPEM),
				CACertPEM: string(caCertPEM),
			},
			Config: enrollConfig{
				Intervals: defaultNodeIntervals(),
			},
		})
		return
	}

	// Create node. New enrollments land in 'enrollment_pending' and only
	// flip to 'active' once a heartbeat AND the first compliance scan both
	// arrive (see heartbeat.go + MarkNodeFirstScan hook in compliance.go).
	// The reaper job `enrollment.pending_timeout` flips stragglers to
	// 'enrollment_failed' after 10m.
	node := &storage.Node{
		ID:        uuid.New(),
		TenantID:  tenant.ID,
		Hostname:  hostname,
		OS:        toNullString(&req.OS),
		Arch:      toNullString(&req.Arch),
		PublicIP:  toNullString(&req.PublicIP),
		MachineID: toNullString(&machineID),
		State:     storage.NodeStateEnrollmentPending,
	}

	created, err := s.store.CreateNode(r.Context(), node)
	if err != nil {
		s.logger.Error("create node for enrollment", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	// The enrollment reaper (started in Server.Start) will flip this node to
	// enrollment_failed if heartbeat + first scan don't both land in time.

	// Increment enrollment count
	if err := s.store.IncrementEnrollmentCount(r.Context(), token.ID); err != nil {
		s.logger.Warn("increment enrollment count", zap.Error(err))
	}

	// Generate client certificate
	certPEM, keyPEM, caCertPEM, err := s.generateClientCertificate(hostname, created.ID.String())
	if err != nil {
		s.logger.Error("generate client certificate", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := enrollResponse{
		NodeID:   created.ID.String(),
		TenantID: tenant.ID.String(),
		TLS: enrollTLS{
			CertPEM:   string(certPEM),
			KeyPEM:    string(keyPEM),
			CACertPEM: string(caCertPEM),
		},
		Config: enrollConfig{
			Intervals: defaultNodeIntervals(),
		},
	}
	writeJSON(w, http.StatusCreated, resp)

	// Seed default security compliance policies for this tenant if not yet done.
	// Runs in a goroutine so it never blocks the enrollment response.
	go s.ensureDefaultPolicies(context.Background(), tenant.ID)

	// Audit log with system actor since there is no authenticated principal
	s.recordAudit(r.Context(), s.systemActor(), tenant.ID, "node.enrolled", "node", created.ID.String(), map[string]any{
		"hostname":    hostname,
		"machine_id":  machineID,
		"token_id":    token.ID.String(),
		"token_name":  token.Name,
		"fingerprint": req.Fingerprint,
	})
}

// --- helpers ---

func newEnrollmentTokenResponse(t storage.EnrollmentToken, rawToken string) enrollmentTokenResponse {
	resp := enrollmentTokenResponse{
		ID:            t.ID.String(),
		TenantID:      t.TenantID.String(),
		Name:          t.Name,
		Token:         rawToken,
		MaxNodes:      t.MaxNodes,
		NodesEnrolled: t.NodesEnrolled,
		Labels:        t.Labels,
		Capabilities:  t.Capabilities,
		ExpiresAt:     formatTime(t.ExpiresAt),
		CreatedAt:     formatTime(t.CreatedAt),
	}
	if t.RevokedAt.Valid {
		revoked := formatTime(t.RevokedAt.Time)
		resp.RevokedAt = &revoked
	}
	if t.CreatedBy != nil {
		cb := t.CreatedBy.String()
		resp.CreatedBy = &cb
	}
	if resp.Labels == nil {
		resp.Labels = make(map[string]string)
	}
	if resp.Capabilities == nil {
		resp.Capabilities = []string{}
	}
	return resp
}
