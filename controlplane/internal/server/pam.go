package server

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/sshca"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// ---------- access requests ----------

type accessRequestResponse struct {
	ID                 string  `json:"id"`
	TenantID           string  `json:"tenant_id"`
	UserID             *string `json:"user_id,omitempty"`
	TargetNodeID       *string `json:"target_node_id,omitempty"`
	TargetResourceType string  `json:"target_resource_type"`
	RequestedAccess    string  `json:"requested_access"`
	Justification      *string `json:"justification,omitempty"`
	Status             string  `json:"status"`
	TTLSeconds         int     `json:"ttl_seconds"`
	RequestedAt        string  `json:"requested_at"`
	DecidedAt          *string `json:"decided_at,omitempty"`
	DecidedBy          *string `json:"decided_by,omitempty"`
	DecisionReason     *string `json:"decision_reason,omitempty"`
	ExpiresAt          *string `json:"expires_at,omitempty"`
}

func newAccessRequestResponse(a storage.AccessRequest) accessRequestResponse {
	out := accessRequestResponse{
		ID:                 a.ID.String(),
		TenantID:           a.TenantID.String(),
		TargetResourceType: a.TargetResourceType,
		RequestedAccess:    a.RequestedAccess,
		Status:             a.Status,
		TTLSeconds:         a.TTLSeconds,
		RequestedAt:        formatTime(a.RequestedAt),
	}
	if a.UserID.Valid {
		s := a.UserID.UUID.String()
		out.UserID = &s
	}
	if a.TargetNodeID.Valid {
		s := a.TargetNodeID.UUID.String()
		out.TargetNodeID = &s
	}
	if a.Justification.Valid {
		s := a.Justification.String
		out.Justification = &s
	}
	if a.DecidedAt.Valid {
		s := formatTime(a.DecidedAt.Time)
		out.DecidedAt = &s
	}
	if a.DecidedBy.Valid {
		s := a.DecidedBy.UUID.String()
		out.DecidedBy = &s
	}
	if a.DecisionReason.Valid {
		s := a.DecisionReason.String
		out.DecisionReason = &s
	}
	if a.ExpiresAt.Valid {
		s := formatTime(a.ExpiresAt.Time)
		out.ExpiresAt = &s
	}
	return out
}

type createAccessRequestRequest struct {
	TenantID           string  `json:"tenant_id"`
	TargetNodeID       *string `json:"target_node_id"`
	TargetResourceType string  `json:"target_resource_type"`
	RequestedAccess    string  `json:"requested_access"`
	Justification      string  `json:"justification"`
	TTLSeconds         int     `json:"ttl_seconds"`
}

type decideAccessRequestRequest struct {
	Reason string `json:"reason"`
}

func (s *Server) handleAccessRequestsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.listAccessRequests(w, r)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleViewer)
		if !ok {
			return
		}
		s.createAccessRequest(w, r, principal)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) listAccessRequests(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f := storage.AccessRequestFilter{Status: strings.TrimSpace(r.URL.Query().Get("status"))}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		f.TenantID = id
	}
	items, total, err := s.store.ListAccessRequests(r.Context(), f, limit, offset)
	if err != nil {
		s.logger.Error("list access requests", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := make([]accessRequestResponse, 0, len(items))
	for _, a := range items {
		resp = append(resp, newAccessRequestResponse(a))
	}
	writeJSON(w, http.StatusOK, paginatedResponse[accessRequestResponse]{Data: resp, Pagination: newPaginationMeta(total, limit, offset, len(resp))})
}

func (s *Server) createAccessRequest(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createAccessRequestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	params := storage.CreateAccessRequestParams{
		TenantID:           tenantID,
		TargetResourceType: req.TargetResourceType,
		RequestedAccess:    req.RequestedAccess,
		Justification:      req.Justification,
		TTLSeconds:         req.TTLSeconds,
	}
	userID := s.userIDForPrincipalCtx(r.Context(), principal)
	if userID != uuid.Nil {
		params.UserID = &userID
	}
	if req.TargetNodeID != nil && strings.TrimSpace(*req.TargetNodeID) != "" {
		id, err := uuid.Parse(*req.TargetNodeID)
		if err != nil {
			http.Error(w, "invalid target_node_id", http.StatusBadRequest)
			return
		}
		params.TargetNodeID = &id
	}
	ar, err := s.store.CreateAccessRequest(r.Context(), params)
	if err != nil {
		http.Error(w, fmt.Sprintf("create failed: %v", err), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, newAccessRequestResponse(*ar))
	s.recordAudit(r.Context(), principal, tenantID, "access.request.create", "access_request", ar.ID.String(), nil)
}

func (s *Server) handleAccessRequestSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/access-requests/")
	parts := strings.SplitN(rest, "/", 2)
	id, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}
	switch action {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		ar, err := s.store.GetAccessRequest(r.Context(), id)
		if err != nil {
			s.logger.Error("get access request", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if ar == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newAccessRequestResponse(*ar))
	case "approve", "deny":
		if r.Method != http.MethodPost {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		var req decideAccessRequestRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		status := "approved"
		if action == "deny" {
			status = "denied"
		}
		existing, err := s.store.GetAccessRequest(r.Context(), id)
		if err != nil || existing == nil {
			http.NotFound(w, r)
			return
		}
		var exp *time.Time
		if status == "approved" {
			t := time.Now().UTC().Add(time.Duration(existing.TTLSeconds) * time.Second)
			exp = &t
		}
		decidedBy := s.userIDForPrincipalCtx(r.Context(), principal)
		updated, err := s.store.DecideAccessRequest(r.Context(), id, status, decidedBy, req.Reason, exp)
		if err != nil {
			http.Error(w, fmt.Sprintf("decide failed: %v", err), http.StatusBadRequest)
			return
		}
		if updated == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newAccessRequestResponse(*updated))
		s.recordAudit(r.Context(), principal, updated.TenantID, "access.request."+status, "access_request", id.String(), map[string]any{"reason": req.Reason})
	default:
		http.NotFound(w, r)
	}
}

// ---------- ssh ca + cert issuance ----------

type sshCAResponse struct {
	ID         string  `json:"id"`
	TenantID   string  `json:"tenant_id"`
	PublicKey  string  `json:"public_key"`
	KeyType    string  `json:"key_type"`
	Active     bool    `json:"active"`
	CreatedAt  string  `json:"created_at"`
	RotatedAt  *string `json:"rotated_at,omitempty"`
}

func (s *Server) handleSSHCA(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.getSSHCA(w, r)
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		s.rotateSSHCA(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) getSSHCA(w http.ResponseWriter, r *http.Request) {
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ca, err := s.store.GetActiveSSHCA(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("get ssh ca", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if ca == nil {
		http.NotFound(w, r)
		return
	}
	resp := sshCAResponse{
		ID: ca.ID.String(), TenantID: ca.TenantID.String(),
		PublicKey: ca.PublicKey, KeyType: ca.KeyType, Active: ca.Active,
		CreatedAt: formatTime(ca.CreatedAt),
	}
	if ca.RotatedAt.Valid {
		t := formatTime(ca.RotatedAt.Time)
		resp.RotatedAt = &t
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) rotateSSHCA(w http.ResponseWriter, r *http.Request) {
	if s.sealer == nil {
		http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	kp, err := sshca.Generate()
	if err != nil {
		http.Error(w, fmt.Sprintf("generate ca: %v", err), http.StatusInternalServerError)
		return
	}
	sealed, nonce, err := sealCAPrivate(s.sealer, kp.PrivateKey)
	if err != nil {
		http.Error(w, fmt.Sprintf("seal: %v", err), http.StatusInternalServerError)
		return
	}
	ca, err := s.store.CreateSSHCA(r.Context(), storage.CreateSSHCAParams{
		TenantID:      tenantID,
		PublicKey:     strings.TrimSpace(kp.PublicKeyAuthorizedKey),
		PrivateSealed: sealed,
		Nonce:         nonce,
		KeyType:       "ed25519",
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("persist ca: %v", err), http.StatusInternalServerError)
		return
	}
	resp := sshCAResponse{
		ID: ca.ID.String(), TenantID: ca.TenantID.String(),
		PublicKey: ca.PublicKey, KeyType: ca.KeyType, Active: ca.Active,
		CreatedAt: formatTime(ca.CreatedAt),
	}
	writeJSON(w, http.StatusCreated, resp)
}

type signCertRequest struct {
	AccessRequestID string   `json:"access_request_id"`
	PublicKeyText   string   `json:"public_key"` // OpenSSH authorized_keys format
	Principals      []string `json:"principals"`
	KeyID           string   `json:"key_id"`
	TTLSeconds      int      `json:"ttl_seconds"`
}

type signCertResponse struct {
	Serial     int64    `json:"serial"`
	SignedCert string   `json:"signed_cert"`
	Principals []string `json:"principals"`
	ExpiresAt  string   `json:"expires_at"`
}

func (s *Server) handleSSHCASignCert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.sealer == nil {
		http.Error(w, "secrets encryption not configured", http.StatusServiceUnavailable)
		return
	}
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := requiredTenantID(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req signCertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if len(req.Principals) == 0 || strings.TrimSpace(req.PublicKeyText) == "" {
		http.Error(w, "principals and public_key required", http.StatusBadRequest)
		return
	}
	userPub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(req.PublicKeyText))
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid public_key: %v", err), http.StatusBadRequest)
		return
	}

	ca, err := s.store.GetActiveSSHCA(r.Context(), tenantID)
	if err != nil || ca == nil {
		http.Error(w, "no active ssh ca — rotate first", http.StatusBadRequest)
		return
	}
	priv, err := unsealCAPrivate(s.sealer, ca.PrivateSealed, ca.Nonce)
	if err != nil {
		s.logger.Error("unseal ca private", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	defer zeroPrivateKey(priv)

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}

	var accessReqID *uuid.UUID
	if strings.TrimSpace(req.AccessRequestID) != "" {
		id, err := uuid.Parse(req.AccessRequestID)
		if err != nil {
			http.Error(w, "invalid access_request_id", http.StatusBadRequest)
			return
		}
		ar, err := s.store.GetAccessRequest(r.Context(), id)
		if err != nil || ar == nil {
			http.Error(w, "access_request not found", http.StatusBadRequest)
			return
		}
		if ar.Status != "approved" {
			http.Error(w, "access_request not approved", http.StatusForbidden)
			return
		}
		if ar.ExpiresAt.Valid && ar.ExpiresAt.Time.Before(time.Now()) {
			http.Error(w, "access_request expired", http.StatusForbidden)
			return
		}
		accessReqID = &id
	}

	serial, err := s.store.NextCertSerial(r.Context(), ca.ID)
	if err != nil {
		s.logger.Error("next serial", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	keyID := strings.TrimSpace(req.KeyID)
	if keyID == "" {
		if principal != nil {
			keyID = principal.Name
		} else {
			keyID = "anonymous"
		}
	}

	cert, err := sshca.SignUserCert(sshca.SignUserCertParams{
		CAPrivate:  priv,
		UserPubKey: userPub,
		KeyID:      keyID,
		Principals: req.Principals,
		Serial:     uint64(serial),
		ValidFor:   ttl,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("sign cert: %v", err), http.StatusInternalServerError)
		return
	}
	wire := sshca.MarshalCert(cert)
	expiresAt := time.Now().UTC().Add(ttl)

	issued, err := s.store.CreateIssuedCert(r.Context(), storage.CreateIssuedCertParams{
		TenantID:        tenantID,
		AccessRequestID: accessReqID,
		CAID:            ca.ID,
		SubjectUser:     keyID,
		Principals:      req.Principals,
		Serial:          serial,
		PublicKey:       req.PublicKeyText,
		SignedCert:      wire,
		ExpiresAt:       expiresAt,
	})
	if err != nil {
		s.logger.Error("persist issued cert", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	s.recordAudit(r.Context(), principal, tenantID, "ssh.cert.sign", "issued_cert", issued.ID.String(), map[string]any{
		"principals": req.Principals, "ttl_seconds": int(ttl.Seconds()), "serial": serial,
	})
	writeJSON(w, http.StatusCreated, signCertResponse{
		Serial:     serial,
		SignedCert: wire,
		Principals: req.Principals,
		ExpiresAt:  formatTime(expiresAt),
	})
}

// ---------- command acl ----------

type commandACLResponse struct {
	ID                string         `json:"id"`
	TenantID          string         `json:"tenant_id"`
	Name              string         `json:"name"`
	Role              string         `json:"role"`
	NodeLabelSelector map[string]any `json:"node_label_selector"`
	AllowCommands     []string       `json:"allow_commands"`
	DenyCommands      []string       `json:"deny_commands"`
	Enabled           bool           `json:"enabled"`
	CreatedAt         string         `json:"created_at"`
}

func newCommandACLResponse(a storage.CommandACL) commandACLResponse {
	out := commandACLResponse{
		ID:                a.ID.String(),
		TenantID:          a.TenantID.String(),
		Name:              a.Name,
		Role:              a.Role,
		NodeLabelSelector: a.NodeLabelSelector,
		AllowCommands:     a.AllowCommands,
		DenyCommands:      a.DenyCommands,
		Enabled:           a.Enabled,
		CreatedAt:         formatTime(a.CreatedAt),
	}
	if out.NodeLabelSelector == nil {
		out.NodeLabelSelector = map[string]any{}
	}
	if out.AllowCommands == nil {
		out.AllowCommands = []string{}
	}
	if out.DenyCommands == nil {
		out.DenyCommands = []string{}
	}
	return out
}

type createCommandACLRequest struct {
	TenantID          string         `json:"tenant_id"`
	Name              string         `json:"name"`
	Role              string         `json:"role"`
	NodeLabelSelector map[string]any `json:"node_label_selector"`
	AllowCommands     []string       `json:"allow_commands"`
	DenyCommands      []string       `json:"deny_commands"`
	Enabled           *bool          `json:"enabled"`
}

func (s *Server) handleCommandACLCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		limit, offset, err := parseLimitOffset(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tenantID, err := requiredTenantID(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		items, total, err := s.store.ListCommandACLs(r.Context(), tenantID, limit, offset)
		if err != nil {
			s.logger.Error("list acls", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		resp := make([]commandACLResponse, 0, len(items))
		for _, a := range items {
			resp = append(resp, newCommandACLResponse(a))
		}
		writeJSON(w, http.StatusOK, paginatedResponse[commandACLResponse]{Data: resp, Pagination: newPaginationMeta(total, limit, offset, len(resp))})
	case http.MethodPost:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		var req createCommandACLRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
			return
		}
		tenantID, err := uuid.Parse(req.TenantID)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		enabled := true
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		acl, err := s.store.CreateCommandACL(r.Context(), storage.CreateCommandACLParams{
			TenantID:          tenantID,
			Name:              req.Name,
			Role:              req.Role,
			NodeLabelSelector: req.NodeLabelSelector,
			AllowCommands:     req.AllowCommands,
			DenyCommands:      req.DenyCommands,
			Enabled:           enabled,
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("create failed: %v", err), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, newCommandACLResponse(*acl))
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCommandACLSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/command-acl/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		a, err := s.store.GetCommandACL(r.Context(), id)
		if err != nil {
			s.logger.Error("get acl", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if a == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusOK, newCommandACLResponse(*a))
	case http.MethodDelete:
		if _, ok := s.authorize(w, r, roleAdmin); !ok {
			return
		}
		if err := s.store.DeleteCommandACL(r.Context(), id); err != nil {
			http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

// --- helpers ---

func requiredTenantID(r *http.Request) (uuid.UUID, error) {
	v := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if v == "" {
		return uuid.Nil, fmt.Errorf("tenant_id is required")
	}
	id, err := uuid.Parse(v)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid tenant_id")
	}
	return id, nil
}

// sealCAPrivate encrypts an ed25519 private key using the configured sealer.
// The sealer is the same secretbox.Sealer used for provider credentials —
// rotating its key re-keys every CA on next rotation.
func sealCAPrivate(sealer *secretbox.Sealer, priv ed25519.PrivateKey) ([]byte, []byte, error) {
	sealed, nonce, err := sealer.Seal(priv)
	if err != nil {
		return nil, nil, err
	}
	return sealed, nonce, nil
}

func unsealCAPrivate(sealer *secretbox.Sealer, sealed, nonce []byte) (ed25519.PrivateKey, error) {
	plain, err := sealer.Open(sealed, nonce)
	if err != nil {
		return nil, err
	}
	if len(plain) != ed25519.PrivateKeySize {
		// Defensive: zero the buffer we decrypted into before returning on error.
		for i := range plain {
			plain[i] = 0
		}
		return nil, fmt.Errorf("invalid ed25519 key size: %d", len(plain))
	}
	return ed25519.PrivateKey(plain), nil
}

// zeroPrivateKey clears the backing bytes of an ed25519 private key.
// Callers should defer this after signing to limit the window in which the
// plaintext key sits in heap memory.
func zeroPrivateKey(priv ed25519.PrivateKey) {
	for i := range priv {
		priv[i] = 0
	}
}

