package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/offlinebundle"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

type offlineContentStore interface {
	ActiveOfflineContentBundle(context.Context, uuid.UUID, string) (*storage.OfflineContentBundle, error)
	ListOfflineContentBundles(context.Context, storage.OfflineContentBundleFilter, int, int) ([]storage.OfflineContentBundle, int, error)
	RecordOfflineContentBundle(context.Context, storage.RecordOfflineContentBundleParams) (*storage.OfflineContentBundle, error)
	RecordOfflineContentBundleAudit(context.Context, storage.OfflineContentBundleAuditParams) error
}

func (s *Server) handleOfflineBundles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		s.listOfflineBundles(w, r)
	case http.MethodPost:
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.importOfflineBundle(w, r, principal)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleOfflineBundleSubroutes(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/rollback") {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		principal, ok := s.authorize(w, r, roleAdmin)
		if !ok {
			return
		}
		s.rollbackOfflineBundle(w, r, principal)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	s.listOfflineBundles(w, r)
}

func (s *Server) listOfflineBundles(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := parseOfflineBundleTenant(w, r)
	if !ok {
		return
	}
	limit := parsePositiveIntQuery(r, "limit", 50)
	offset := parsePositiveIntQuery(r, "offset", 0)
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	bundleID := strings.TrimSpace(r.URL.Query().Get("bundle_id"))
	resp := map[string]any{
		"tenant_id": tenantID.String(),
		"items":     []offlineBundleResponse{},
		"total":     0,
		"source":    "filesystem",
	}
	if store, ok := s.store.(offlineContentStore); ok && store != nil {
		rows, total, err := store.ListOfflineContentBundles(r.Context(), storage.OfflineContentBundleFilter{TenantID: tenantID, BundleID: bundleID, Status: status}, limit, offset)
		if err != nil {
			s.logger.Warn("list offline content bundles", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		items := make([]offlineBundleResponse, 0, len(rows))
		for _, row := range rows {
			items = append(items, newOfflineBundleResponse(row))
		}
		resp["items"] = items
		resp["total"] = total
		resp["source"] = "database"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if root := s.offlineContentRootDir(); root != "" {
		receipts, err := offlinebundle.ListStatus(root)
		if err != nil {
			s.logger.Warn("list offline content filesystem status", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		items := make([]offlineBundleReceiptResponse, 0, len(receipts))
		for _, receipt := range receipts {
			if bundleID != "" && receipt.BundleID != bundleID {
				continue
			}
			items = append(items, newOfflineBundleReceiptResponse(receipt))
		}
		resp["items"] = items
		resp["total"] = len(items)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) importOfflineBundle(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	tenantID, ok := parseOfflineBundleTenant(w, r)
	if !ok {
		return
	}
	pub, err := s.offlineContentPublicKey()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	root := s.offlineContentRootDir()
	if root == "" {
		http.Error(w, "offline content root unavailable", http.StatusServiceUnavailable)
		return
	}
	maxBytes := s.offlineContentMaxBytes()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBytes+1))
	if err != nil {
		http.Error(w, "read bundle", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > maxBytes {
		http.Error(w, "bundle too large", http.StatusRequestEntityTooLarge)
		return
	}
	now := time.Now().UTC()
	verified, err := offlinebundle.VerifyArchive(bytes.NewReader(body), offlinebundle.ImportOptions{PublicKey: pub, Now: now, MaxBytes: maxBytes})
	if err != nil {
		s.auditOfflineBundleImport(r.Context(), tenantID, nil, principal, "rejected", err.Error(), map[string]any{"error": err.Error()})
		writeOfflineBundleImportError(w, err)
		return
	}
	currentSequence := int64(0)
	if store, ok := s.store.(offlineContentStore); ok && store != nil {
		active, err := store.ActiveOfflineContentBundle(r.Context(), tenantID, verified.Manifest.BundleID)
		if err != nil {
			s.logger.Warn("lookup active offline bundle", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		if active != nil {
			currentSequence = active.Sequence
		}
	}
	if currentSequence > 0 && verified.Manifest.Sequence < currentSequence {
		err := offlinebundle.ErrDowngrade
		s.auditOfflineBundleImport(r.Context(), tenantID, nil, principal, "rejected", err.Error(), map[string]any{
			"bundle_id": verified.Manifest.BundleID,
			"sequence":  verified.Manifest.Sequence,
			"active":    currentSequence,
		})
		writeOfflineBundleImportError(w, err)
		return
	}
	receipt, err := offlinebundle.InstallVerified(r.Context(), verified, offlinebundle.ImportOptions{RootDir: root, PublicKey: pub, Now: now, CurrentSequence: currentSequence, MaxBytes: maxBytes})
	if err != nil {
		s.auditOfflineBundleImport(r.Context(), tenantID, nil, principal, "failed", err.Error(), map[string]any{"bundle_id": verified.Manifest.BundleID})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var row *storage.OfflineContentBundle
	if store, ok := s.store.(offlineContentStore); ok && store != nil {
		manifest := map[string]any{}
		_ = json.Unmarshal(verified.ManifestBytes, &manifest)
		contents := offlineBundleContentsForStorage(receipt.Contents)
		importedBy := principalUUID(principal)
		row, err = store.RecordOfflineContentBundle(r.Context(), storage.RecordOfflineContentBundleParams{
			TenantID:             tenantID,
			BundleID:             receipt.BundleID,
			Version:              receipt.Version,
			Sequence:             receipt.Sequence,
			Status:               receipt.Status,
			PublicKeyFingerprint: receipt.PublicKeyFingerprint,
			Signature:            receipt.Signature,
			ManifestSHA256:       receipt.ManifestSHA256,
			StoragePath:          receipt.StoragePath,
			Manifest:             manifest,
			Contents:             contents,
			Warnings:             receipt.Warnings,
			ImportedBy:           importedBy,
			ImportedAt:           receipt.ImportedAt,
			IssuedAt:             receipt.IssuedAt,
			ExpiresAt:            receipt.ExpiresAt,
		})
		if err != nil {
			s.logger.Warn("record offline content bundle", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		var rowID *uuid.UUID
		if row != nil {
			rowID = &row.ID
		}
		s.auditOfflineBundleImport(r.Context(), tenantID, rowID, principal, "imported", "", map[string]any{
			"bundle_id": receipt.BundleID,
			"version":   receipt.Version,
			"sequence":  receipt.Sequence,
			"warnings":  receipt.Warnings,
		})
		s.importOfflineVulnerabilityFeedFindings(r.Context(), tenantID, receipt)
		writeJSON(w, http.StatusCreated, newOfflineBundleResponse(*row))
		return
	}
	s.auditOfflineBundleImport(r.Context(), tenantID, nil, principal, "imported", "", map[string]any{
		"bundle_id": receipt.BundleID,
		"version":   receipt.Version,
		"sequence":  receipt.Sequence,
		"warnings":  receipt.Warnings,
	})
	s.importOfflineVulnerabilityFeedFindings(r.Context(), tenantID, receipt)
	writeJSON(w, http.StatusCreated, newOfflineBundleReceiptResponse(*receipt))
}

func (s *Server) rollbackOfflineBundle(w http.ResponseWriter, r *http.Request, principal *auth.Principal) {
	tenantID, ok := parseOfflineBundleTenant(w, r)
	if !ok {
		return
	}
	var req struct {
		BundleID string `json:"bundle_id"`
		Sequence int64  `json:"sequence"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid rollback payload", http.StatusBadRequest)
		return
	}
	req.BundleID = strings.TrimSpace(req.BundleID)
	if req.BundleID == "" || req.Sequence <= 0 {
		http.Error(w, "bundle_id and sequence are required", http.StatusBadRequest)
		return
	}
	receipt, err := offlinebundle.Activate(s.offlineContentRootDir(), req.BundleID, req.Sequence)
	if err != nil {
		s.auditOfflineBundleAction(r.Context(), tenantID, nil, principal, "rollback", "failed", err.Error(), map[string]any{"bundle_id": req.BundleID, "sequence": req.Sequence})
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if store, ok := s.store.(offlineContentStore); ok && store != nil {
		importedBy := principalUUID(principal)
		row, err := store.RecordOfflineContentBundle(r.Context(), storage.RecordOfflineContentBundleParams{
			TenantID:             tenantID,
			BundleID:             receipt.BundleID,
			Version:              receipt.Version,
			Sequence:             receipt.Sequence,
			Status:               "active",
			PublicKeyFingerprint: receipt.PublicKeyFingerprint,
			Signature:            receipt.Signature,
			ManifestSHA256:       receipt.ManifestSHA256,
			StoragePath:          receipt.StoragePath,
			Contents:             offlineBundleContentsForStorage(receipt.Contents),
			Warnings:             receipt.Warnings,
			ImportedBy:           importedBy,
			ImportedAt:           time.Now().UTC(),
			IssuedAt:             receipt.IssuedAt,
			ExpiresAt:            receipt.ExpiresAt,
		})
		if err != nil {
			s.logger.Warn("record offline content rollback", zap.Error(err))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		s.auditOfflineBundleAction(r.Context(), tenantID, &row.ID, principal, "rollback", "ok", "", map[string]any{"bundle_id": receipt.BundleID, "sequence": receipt.Sequence})
		s.importOfflineVulnerabilityFeedFindings(r.Context(), tenantID, receipt)
		writeJSON(w, http.StatusOK, newOfflineBundleResponse(*row))
		return
	}
	s.auditOfflineBundleAction(r.Context(), tenantID, nil, principal, "rollback", "ok", "", map[string]any{"bundle_id": receipt.BundleID, "sequence": receipt.Sequence})
	s.importOfflineVulnerabilityFeedFindings(r.Context(), tenantID, receipt)
	writeJSON(w, http.StatusOK, newOfflineBundleReceiptResponse(*receipt))
}

func (s *Server) auditOfflineBundleImport(ctx context.Context, tenantID uuid.UUID, rowID *uuid.UUID, principal *auth.Principal, status, reason string, metadata map[string]any) {
	s.auditOfflineBundleAction(ctx, tenantID, rowID, principal, "import", status, reason, metadata)
}

func (s *Server) auditOfflineBundleAction(ctx context.Context, tenantID uuid.UUID, rowID *uuid.UUID, principal *auth.Principal, action, status, reason string, metadata map[string]any) {
	store, ok := s.store.(offlineContentStore)
	if !ok || store == nil {
		return
	}
	if metadata == nil {
		metadata = map[string]any{}
	}
	actorID := principalUUID(principal)
	if err := store.RecordOfflineContentBundleAudit(ctx, storage.OfflineContentBundleAuditParams{
		TenantID:    tenantID,
		BundleRowID: rowID,
		Action:      action,
		Status:      status,
		Reason:      reason,
		ActorID:     actorID,
		Metadata:    metadata,
	}); err != nil && s.logger != nil {
		s.logger.Warn("audit offline content bundle", zap.Error(err))
	}
}

func (s *Server) offlineContentPublicKey() (ed25519.PublicKey, error) {
	if s == nil || s.cfg == nil || !s.cfg.OfflineContent.Enabled {
		return nil, errors.New("offline content is disabled")
	}
	return offlinebundle.LoadPublicKeyFile(s.cfg.OfflineContent.PublicKeyFile)
}

func (s *Server) offlineContentRootDir() string {
	if s == nil {
		return ""
	}
	if strings.TrimSpace(s.offlineContentRoot) != "" {
		return strings.TrimSpace(s.offlineContentRoot)
	}
	if s.cfg != nil && s.cfg.OfflineContent.Enabled {
		return strings.TrimSpace(s.cfg.OfflineContent.RootDir)
	}
	return ""
}

func (s *Server) offlineContentMaxBytes() int64 {
	if s != nil && s.cfg != nil && s.cfg.OfflineContent.MaxBundleBytes > 0 {
		return s.cfg.OfflineContent.MaxBundleBytes
	}
	return 256 * 1024 * 1024
}

func writeOfflineBundleImportError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, offlinebundle.ErrInvalidSignature), errors.Is(err, offlinebundle.ErrUnsignedBundle):
		status = http.StatusUnauthorized
	case errors.Is(err, offlinebundle.ErrDowngrade):
		status = http.StatusConflict
	case errors.Is(err, offlinebundle.ErrExpired):
		status = http.StatusGone
	}
	http.Error(w, err.Error(), status)
}

func parseOfflineBundleTenant(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if raw == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return uuid.Nil, false
	}
	tenantID, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return uuid.Nil, false
	}
	return tenantID, true
}

func parsePositiveIntQuery(r *http.Request, key string, fallback int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}

func principalUUID(p *auth.Principal) *uuid.UUID {
	if p == nil {
		return nil
	}
	for _, raw := range []string{p.Subject, p.Name} {
		raw = strings.TrimPrefix(strings.TrimSpace(raw), "user:")
		if id, err := uuid.Parse(raw); err == nil {
			return &id
		}
	}
	return nil
}

type offlineBundleResponse struct {
	ID                   string           `json:"id"`
	TenantID             string           `json:"tenant_id"`
	BundleID             string           `json:"bundle_id"`
	Version              string           `json:"version"`
	Sequence             int64            `json:"sequence"`
	Status               string           `json:"status"`
	PublicKeyFingerprint string           `json:"public_key_fingerprint"`
	ManifestSHA256       string           `json:"manifest_sha256"`
	StoragePath          string           `json:"storage_path"`
	Contents             []map[string]any `json:"contents"`
	Warnings             []string         `json:"warnings"`
	Error                string           `json:"error,omitempty"`
	ImportedAt           string           `json:"imported_at"`
	IssuedAt             *string          `json:"issued_at,omitempty"`
	ExpiresAt            *string          `json:"expires_at,omitempty"`
}

func newOfflineBundleResponse(row storage.OfflineContentBundle) offlineBundleResponse {
	out := offlineBundleResponse{
		ID:                   row.ID.String(),
		TenantID:             row.TenantID.String(),
		BundleID:             row.BundleID,
		Version:              row.Version,
		Sequence:             row.Sequence,
		Status:               row.Status,
		PublicKeyFingerprint: row.PublicKeyFingerprint,
		ManifestSHA256:       row.ManifestSHA256,
		StoragePath:          row.StoragePath,
		Contents:             row.Contents,
		Warnings:             row.Warnings,
		Error:                row.Error,
		ImportedAt:           row.ImportedAt.UTC().Format(time.RFC3339),
	}
	if row.IssuedAt.Valid {
		v := row.IssuedAt.Time.UTC().Format(time.RFC3339)
		out.IssuedAt = &v
	}
	if row.ExpiresAt.Valid {
		v := row.ExpiresAt.Time.UTC().Format(time.RFC3339)
		out.ExpiresAt = &v
	}
	return out
}

type offlineBundleReceiptResponse struct {
	BundleID             string                         `json:"bundle_id"`
	Version              string                         `json:"version"`
	Sequence             int64                          `json:"sequence"`
	Status               string                         `json:"status"`
	PublicKeyFingerprint string                         `json:"public_key_fingerprint"`
	ManifestSHA256       string                         `json:"manifest_sha256"`
	StoragePath          string                         `json:"storage_path"`
	Contents             []offlinebundle.ContentReceipt `json:"contents"`
	Warnings             []string                       `json:"warnings"`
	ImportedAt           string                         `json:"imported_at"`
	ExpiresAt            string                         `json:"expires_at"`
}

func newOfflineBundleReceiptResponse(receipt offlinebundle.Receipt) offlineBundleReceiptResponse {
	return offlineBundleReceiptResponse{
		BundleID:             receipt.BundleID,
		Version:              receipt.Version,
		Sequence:             receipt.Sequence,
		Status:               receipt.Status,
		PublicKeyFingerprint: receipt.PublicKeyFingerprint,
		ManifestSHA256:       receipt.ManifestSHA256,
		StoragePath:          receipt.StoragePath,
		Contents:             receipt.Contents,
		Warnings:             receipt.Warnings,
		ImportedAt:           receipt.ImportedAt.UTC().Format(time.RFC3339),
		ExpiresAt:            receipt.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func offlineBundleContentsForStorage(contents []offlinebundle.ContentReceipt) []map[string]any {
	out := make([]map[string]any, 0, len(contents))
	for _, content := range contents {
		row := map[string]any{
			"type":            content.Type,
			"name":            content.Name,
			"version":         content.Version,
			"bundle_id":       content.BundleID,
			"bundle_version":  content.BundleVersion,
			"bundle_sequence": content.BundleSequence,
			"path":            content.Path,
			"active_path":     content.Active,
			"sha256":          content.SHA256,
			"stale":           content.Stale,
		}
		if content.ExpiresAt != nil {
			row["expires_at"] = content.ExpiresAt.UTC().Format(time.RFC3339)
		}
		out = append(out, row)
	}
	return out
}
