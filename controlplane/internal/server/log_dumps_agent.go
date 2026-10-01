package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

const logDumpClaimTTL = 2 * time.Minute

type agentLogDumpStore interface {
	GetLogDumpByScope(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) (*storage.LogDump, error)
	ClaimLogDump(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string, time.Time, time.Time) (*storage.LogDump, error)
	RenewLogDumpClaim(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string, time.Time, time.Time) error
	PutLogDumpChunk(context.Context, storage.LogDumpChunk, string, time.Time) (bool, error)
	ListLogDumpChunks(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) ([]storage.LogDumpChunk, error)
	CompleteLogDumpAndJob(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string, string, string, int64, int64, bool, *bool, string, time.Time) error
	FailClaimedLogDumpAndJob(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, string, string, *bool, string, time.Time) error
	DeleteLogDumpChunks(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
}

type logDumpClaimRequest struct {
	JobID string `json:"job_id"`
}

type logDumpCompleteRequest struct {
	JobID           string `json:"job_id"`
	ChunkCount      int    `json:"chunk_count"`
	SHA256          string `json:"sha256"`
	RowCount        int64  `json:"row_count"`
	Truncated       bool   `json:"truncated"`
	SourceAvailable *bool  `json:"source_available,omitempty"`
	SourceReason    string `json:"source_reason,omitempty"`
}

type logDumpFailRequest struct {
	JobID           string `json:"job_id"`
	Error           string `json:"error"`
	SourceAvailable *bool  `json:"source_available,omitempty"`
	SourceReason    string `json:"source_reason,omitempty"`
}

func (s *Server) handleAgentLogDumpResource(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store.(agentLogDumpStore)
	if !ok {
		http.Error(w, "log dump storage unavailable", http.StatusServiceUnavailable)
		return
	}
	tenantID, nodeID, ok := s.agentLogDumpScope(w, r)
	if !ok {
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/agent/log-dumps/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	dumpID, err := uuid.Parse(parts[0])
	if err != nil {
		http.Error(w, "invalid log dump id", http.StatusBadRequest)
		return
	}
	switch {
	case len(parts) == 2 && parts[1] == "claim":
		s.handleAgentLogDumpClaim(w, r, store, tenantID, nodeID, dumpID)
	case len(parts) == 3 && parts[1] == "chunks":
		ordinal, err := strconv.Atoi(parts[2])
		if err != nil || ordinal < 0 {
			http.Error(w, "invalid chunk ordinal", http.StatusBadRequest)
			return
		}
		s.handleAgentLogDumpChunk(w, r, store, tenantID, nodeID, dumpID, ordinal)
	case len(parts) == 2 && parts[1] == "complete":
		s.handleAgentLogDumpComplete(w, r, store, tenantID, nodeID, dumpID)
	case len(parts) == 2 && parts[1] == "fail":
		s.handleAgentLogDumpFail(w, r, store, tenantID, nodeID, dumpID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) agentLogDumpScope(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok || principal == nil || principal.Type != "agent" {
		http.Error(w, "agent principal required", http.StatusForbidden)
		return uuid.Nil, uuid.Nil, false
	}
	tenantID, nodeID, err := s.tenantNodeForAgent(r.Context(), principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return uuid.Nil, uuid.Nil, false
	}
	return tenantID, nodeID, true
}

func (s *Server) handleAgentLogDumpClaim(w http.ResponseWriter, r *http.Request, store agentLogDumpStore, tenantID, nodeID, dumpID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	var req logDumpClaimRequest
	if err := decodeStrictJSONDocument(r, &req, 8<<10); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	jobID, err := uuid.Parse(strings.TrimSpace(req.JobID))
	if err != nil {
		http.Error(w, "invalid job_id", http.StatusBadRequest)
		return
	}
	token, tokenSHA, err := newLogDumpClaimToken()
	if err != nil {
		s.logger.Error("generate log dump claim token", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	now := s.logDumpNow()
	leaseUntil := now.Add(logDumpClaimTTL)
	dump, err := store.ClaimLogDump(r.Context(), tenantID, nodeID, dumpID, jobID, tokenSHA, now, leaseUntil)
	if err != nil {
		writeAgentLogDumpError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dump_id":          dump.ID.String(),
		"job_id":           jobID.String(),
		"upload_token":     token,
		"claim_generation": dump.ClaimGeneration,
		"claim_expires_at": leaseUntil.Format(time.RFC3339Nano),
		"window_start":     dump.WindowStart.UTC().Format(time.RFC3339Nano),
		"window_end":       dump.WindowEnd.UTC().Format(time.RFC3339Nano),
		"entity_filter":    dump.EntityFilter,
		"max_chunk_bytes":  maxLogDumpChunkBytes,
		"max_artifact_bytes": maxLogDumpArtifactBytes,
	})
}

func (s *Server) handleAgentLogDumpChunk(w http.ResponseWriter, r *http.Request, store agentLogDumpStore, tenantID, nodeID, dumpID uuid.UUID, ordinal int) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	jobID, token, tokenSHA, ok := logDumpUploadIdentity(w, r)
	if !ok {
		return
	}
	expectedSHA := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Chunk-SHA256")))
	if len(expectedSHA) != sha256.Size*2 {
		http.Error(w, "valid X-Chunk-SHA256 header is required", http.StatusBadRequest)
		return
	}
	now := s.logDumpNow()
	if err := store.RenewLogDumpClaim(r.Context(), tenantID, nodeID, dumpID, jobID, tokenSHA, now, now.Add(logDumpClaimTTL)); err != nil {
		writeAgentLogDumpError(w, err)
		return
	}

	path, size, actualSHA, err := writeLogDumpChunk(dumpID, jobID, ordinal, r.Body, expectedSHA)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	idempotent, err := store.PutLogDumpChunk(r.Context(), storage.LogDumpChunk{
		DumpID: dumpID, TenantID: tenantID, NodeID: nodeID,
		JobID: uuid.NullUUID{UUID: jobID, Valid: true}, Ordinal: ordinal,
		SHA256: actualSHA, SizeBytes: size, TempPath: path, CreatedAt: now,
	}, tokenSHA, now)
	if err != nil {
		writeAgentLogDumpError(w, err)
		return
	}
	if idempotent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	remove = false
	w.Header().Set("X-Log-Dump-Upload-Token", token)
	w.WriteHeader(http.StatusCreated)
}

func (s *Server) handleAgentLogDumpComplete(w http.ResponseWriter, r *http.Request, store agentLogDumpStore, tenantID, nodeID, dumpID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	jobID, _, tokenSHA, ok := logDumpUploadIdentity(w, r)
	if !ok {
		return
	}
	var req logDumpCompleteRequest
	if err := decodeStrictJSONDocument(r, &req, 16<<10); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	bodyJobID, err := uuid.Parse(strings.TrimSpace(req.JobID))
	if err != nil || bodyJobID != jobID {
		http.Error(w, "job_id does not match X-Log-Dump-Job-ID", http.StatusBadRequest)
		return
	}
	if req.ChunkCount < 0 || req.ChunkCount > 10000 || req.RowCount < 0 {
		http.Error(w, "invalid completion counters", http.StatusBadRequest)
		return
	}
	expectedSHA := strings.ToLower(strings.TrimSpace(req.SHA256))
	if len(expectedSHA) != sha256.Size*2 {
		http.Error(w, "valid sha256 is required", http.StatusBadRequest)
		return
	}
	now := s.logDumpNow()
	if err := store.RenewLogDumpClaim(r.Context(), tenantID, nodeID, dumpID, jobID, tokenSHA, now, now.Add(logDumpClaimTTL)); err != nil {
		writeAgentLogDumpError(w, err)
		return
	}
	chunks, err := store.ListLogDumpChunks(r.Context(), tenantID, nodeID, dumpID, jobID)
	if err != nil {
		s.logger.Error("list log dump chunks", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if len(chunks) != req.ChunkCount {
		http.Error(w, "chunk_count does not match uploaded chunks", http.StatusConflict)
		return
	}
	artifacts := make([]logDumpChunkArtifact, 0, len(chunks))
	for _, c := range chunks {
		artifacts = append(artifacts, logDumpChunkArtifact{Ordinal: c.Ordinal, Path: c.TempPath, SHA256: c.SHA256, SizeBytes: c.SizeBytes})
	}
	artifact, err := assembleLogDumpChunks(tenantID, nodeID, dumpID, artifacts, expectedSHA)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	available := req.SourceAvailable
	if available == nil {
		v := true
		available = &v
	}
	if err := store.CompleteLogDumpAndJob(r.Context(), tenantID, nodeID, dumpID, jobID, tokenSHA,
		artifact.Path, artifact.SHA256, req.RowCount, artifact.SizeBytes, req.Truncated, available,
		strings.TrimSpace(req.SourceReason), now); err != nil {
		_ = removeScopedLogDumpFile(artifact.Path)
		writeAgentLogDumpError(w, err)
		return
	}
	if err := removeLogDumpChunkFiles(dumpID); err != nil {
		s.logger.Warn("remove completed log dump chunk files", zap.String("dump_id", dumpID.String()), zap.Error(err))
	}
	if err := store.DeleteLogDumpChunks(r.Context(), tenantID, nodeID, dumpID); err != nil {
		s.logger.Warn("remove completed log dump chunk metadata", zap.String("dump_id", dumpID.String()), zap.Error(err))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAgentLogDumpFail(w http.ResponseWriter, r *http.Request, store agentLogDumpStore, tenantID, nodeID, dumpID uuid.UUID) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	jobID, _, tokenSHA, ok := logDumpUploadIdentity(w, r)
	if !ok {
		return
	}
	var req logDumpFailRequest
	if err := decodeStrictJSONDocument(r, &req, 16<<10); err != nil {
		http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
		return
	}
	bodyJobID, err := uuid.Parse(strings.TrimSpace(req.JobID))
	if err != nil || bodyJobID != jobID {
		http.Error(w, "job_id does not match X-Log-Dump-Job-ID", http.StatusBadRequest)
		return
	}
	message := strings.TrimSpace(req.Error)
	if message == "" {
		message = "agent raw log capture failed"
	}
	if len(message) > 2048 {
		message = message[:2048]
	}
	now := s.logDumpNow()
	if err := store.FailClaimedLogDumpAndJob(r.Context(), tenantID, nodeID, dumpID, jobID, tokenSHA,
		message, req.SourceAvailable, strings.TrimSpace(req.SourceReason), now); err != nil {
		writeAgentLogDumpError(w, err)
		return
	}
	_ = removeLogDumpChunkFiles(dumpID)
	_ = store.DeleteLogDumpChunks(r.Context(), tenantID, nodeID, dumpID)
	w.WriteHeader(http.StatusNoContent)
}

func logDumpUploadIdentity(w http.ResponseWriter, r *http.Request) (uuid.UUID, string, string, bool) {
	jobID, err := uuid.Parse(strings.TrimSpace(r.Header.Get("X-Log-Dump-Job-ID")))
	if err != nil {
		http.Error(w, "valid X-Log-Dump-Job-ID header is required", http.StatusBadRequest)
		return uuid.Nil, "", "", false
	}
	token := strings.TrimSpace(r.Header.Get("X-Log-Dump-Token"))
	if token == "" {
		http.Error(w, "X-Log-Dump-Token header is required", http.StatusUnauthorized)
		return uuid.Nil, "", "", false
	}
	sum := sha256.Sum256([]byte(token))
	return jobID, token, hex.EncodeToString(sum[:]), true
}

func newLogDumpClaimToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func writeAgentLogDumpError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrLogDumpExpired):
		http.Error(w, "log dump expired", http.StatusGone)
	case errors.Is(err, storage.ErrLogDumpClaimHeld):
		http.Error(w, "log dump is already claimed", http.StatusConflict)
	case errors.Is(err, storage.ErrLogDumpClaimInvalid):
		http.Error(w, "log dump claim is invalid or expired", http.StatusConflict)
	case errors.Is(err, storage.ErrLogDumpChunkConflict):
		http.Error(w, "log dump chunk conflicts with existing upload", http.StatusConflict)
	case errors.Is(err, sql.ErrNoRows):
		http.Error(w, "log dump not found", http.StatusNotFound)
	default:
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

var _ = json.Valid
