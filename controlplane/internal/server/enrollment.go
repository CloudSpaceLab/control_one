package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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
	Token              string `json:"token"`
	Hostname           string `json:"hostname"`
	OS                 string `json:"os"`
	Arch               string `json:"arch"`
	PublicIP           string `json:"public_ip"`
	Fingerprint        string `json:"fingerprint"`
	MachineID          string `json:"machine_id"`
	CompliancePolicyID string `json:"compliance_policy_id,omitempty"`
}

type enrollResponse struct {
	NodeID    string       `json:"node_id"`
	TenantID  string       `json:"tenant_id"`
	NodeToken string       `json:"node_token,omitempty"` // bearer token for agent auth
	TLS       enrollTLS    `json:"tls"`
	Config    enrollConfig `json:"config"`
	Policy    enrollPolicy `json:"policy,omitempty"`
}

type enrollTLS struct {
	CertPEM   string `json:"client_cert"`
	KeyPEM    string `json:"client_key"`
	CACertPEM string `json:"ca_cert"`
}

type enrollConfig struct {
	Intervals map[string]int64 `json:"intervals"`
}

// enrollPolicy ships the policy public key (PEM-encoded) so the agent can
// verify signed policy bundles. Empty when the server isn't configured with
// a policy signing key — in that case the agent skips signature verification.
type enrollPolicy struct {
	PublicKeyPEM string `json:"public_key_pem,omitempty"`
}

const defaultHardeningPolicyID = "control-one-default-hardening"

// policyPublicKeyPEM returns the PEM-encoded ed25519 public key the server
// uses to sign policy bundles, loaded from cfg.Policy.PublicKeyFile. Returns
// nil when no path is configured or the file can't be read — the missing
// case is logged once at WARN and treated as "policy signing not configured."
// Cached after first call so we don't re-read on every enrollment.
func (s *Server) policyPublicKeyPEM() []byte {
	s.policyKeyOnce.Do(func() {
		path := strings.TrimSpace(s.cfg.Policy.PublicKeyFile)
		if path == "" {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			s.logger.Warn("policy public key not loaded; enrolling agents will skip signature verification",
				zap.String("path", path),
				zap.Error(err),
			)
			return
		}
		s.policyKeyPEM = data
	})
	return s.policyKeyPEM
}

func selectedCompliancePolicyID(req enrollRequest, token *storage.EnrollmentToken) string {
	selected := strings.TrimSpace(req.CompliancePolicyID)
	if selected == "" && token != nil && token.Labels != nil {
		selected = strings.TrimSpace(token.Labels["compliance_policy_id"])
	}
	if selected == "" {
		selected = defaultHardeningPolicyID
	}
	return selected
}

func (s *Server) applyEnrollmentCompliancePolicy(ctx context.Context, tenantID, nodeID uuid.UUID, selected string) error {
	selected = strings.TrimSpace(selected)
	if selected == "" || selected == defaultHardeningPolicyID {
		s.ensureDefaultPolicies(ctx, tenantID)
		return nil
	}

	policyID, err := uuid.Parse(selected)
	if err != nil {
		return fmt.Errorf("invalid compliance_policy_id")
	}
	policy, err := s.store.GetPolicy(ctx, policyID)
	if err != nil {
		return fmt.Errorf("load compliance policy: %w", err)
	}
	if policy == nil || policy.TenantID != tenantID {
		return fmt.Errorf("compliance policy not found for tenant")
	}
	if !policy.Enabled || policy.ArchivedAt.Valid {
		return fmt.Errorf("compliance policy is not active")
	}
	if _, err := s.store.CreatePolicyAssignment(ctx, storage.CreatePolicyAssignmentParams{
		PolicyID: policyID,
		TenantID: tenantID,
		NodeID:   nodeID,
	}); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate") ||
			strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil
		}
		return fmt.Errorf("assign compliance policy: %w", err)
	}
	return nil
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
	compliancePolicyID := selectedCompliancePolicyID(req, token)
	if compliancePolicyID != "" && compliancePolicyID != defaultHardeningPolicyID {
		policyID, parseErr := uuid.Parse(compliancePolicyID)
		if parseErr != nil {
			http.Error(w, "invalid compliance_policy_id", http.StatusBadRequest)
			return
		}
		policy, policyErr := s.store.GetPolicy(r.Context(), policyID)
		if policyErr != nil {
			s.logger.Error("lookup enrollment compliance policy", zap.Error(policyErr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if policy == nil || policy.TenantID != tenant.ID || !policy.Enabled || policy.ArchivedAt.Valid {
			http.Error(w, "compliance policy not found or inactive", http.StatusBadRequest)
			return
		}
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

		// Reset nodes that previously failed enrollment (or were retired) so
		// the state machine can run again from scratch on the next heartbeat +
		// first-scan cycle. Active and enrollment_pending nodes are left alone.
		if existing.State == storage.NodeStateEnrollmentFailed || existing.State == storage.NodeStateRetired {
			if resetErr := s.store.ResetNodeForReenrollment(r.Context(), existing.ID); resetErr != nil {
				s.logger.Warn("reset node for re-enrollment", zap.Error(resetErr),
					zap.String("node_id", existing.ID.String()))
			}
		}

		// Generate certs for re-enrollment
		certPEM, keyPEM, caCertPEM, certErr := s.generateClientCertificate(hostname, existing.ID.String())
		if certErr != nil {
			s.logger.Error("generate client certificate for re-enrollment", zap.Error(certErr))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		reNodeToken := generateNodeAuthToken()
		if err := s.store.SetNodeAuthToken(r.Context(), existing.ID, reNodeToken); err != nil {
			s.logger.Warn("set node auth token (re-enrollment)", zap.Error(err))
		}
		if err := s.applyEnrollmentCompliancePolicy(r.Context(), tenant.ID, existing.ID, compliancePolicyID); err != nil {
			s.logger.Warn("apply enrollment compliance policy", zap.Error(err),
				zap.String("node_id", existing.ID.String()),
				zap.String("policy", compliancePolicyID))
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, enrollResponse{
			NodeID:    existing.ID.String(),
			TenantID:  tenant.ID.String(),
			NodeToken: reNodeToken,
			TLS: enrollTLS{
				CertPEM:   string(certPEM),
				KeyPEM:    string(keyPEM),
				CACertPEM: string(caCertPEM),
			},
			Config: enrollConfig{
				Intervals: defaultNodeIntervals(),
			},
			Policy: enrollPolicy{
				PublicKeyPEM: string(s.policyPublicKeyPEM()),
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

	// Generate per-node auth token (used as Bearer token — no mTLS required).
	nodeToken := generateNodeAuthToken()
	if err := s.store.SetNodeAuthToken(r.Context(), created.ID, nodeToken); err != nil {
		s.logger.Warn("set node auth token", zap.Error(err))
		// non-fatal: node can still use mTLS
	}
	if err := s.applyEnrollmentCompliancePolicy(r.Context(), tenant.ID, created.ID, compliancePolicyID); err != nil {
		s.logger.Warn("apply enrollment compliance policy", zap.Error(err),
			zap.String("node_id", created.ID.String()),
			zap.String("policy", compliancePolicyID))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	resp := enrollResponse{
		NodeID:    created.ID.String(),
		TenantID:  tenant.ID.String(),
		NodeToken: nodeToken,
		TLS: enrollTLS{
			CertPEM:   string(certPEM),
			KeyPEM:    string(keyPEM),
			CACertPEM: string(caCertPEM),
		},
		Config: enrollConfig{
			Intervals: defaultNodeIntervals(),
		},
		Policy: enrollPolicy{
			PublicKeyPEM: string(s.policyPublicKeyPEM()),
		},
	}
	writeJSON(w, http.StatusCreated, resp)

	// Audit log with system actor since there is no authenticated principal
	s.recordAudit(r.Context(), s.systemActor(), tenant.ID, "node.enrolled", "node", created.ID.String(), map[string]any{
		"hostname":             hostname,
		"machine_id":           machineID,
		"token_id":             token.ID.String(),
		"token_name":           token.Name,
		"fingerprint":          req.Fingerprint,
		"compliance_policy_id": compliancePolicyID,
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

// generateNodeAuthToken returns a random 32-byte hex token for per-node Bearer auth.
func generateNodeAuthToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// fallback: use sha256 of a UUID — not cryptographically ideal but never nil
		sum := sha256.Sum256([]byte(uuid.New().String()))
		return hex.EncodeToString(sum[:])
	}
	return hex.EncodeToString(b)
}
