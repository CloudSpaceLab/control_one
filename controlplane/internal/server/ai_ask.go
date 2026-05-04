package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// FEATURE_AI_ASK gates every endpoint in this file. Off by default — the
// admin opts in by setting the env var. UI also gates /ask via
// window.__C1_FLAGS__.ai_ask.
func aiAskEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("FEATURE_AI_ASK")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// ── /api/v1/ai/config ─────────────────────────────────────────────────────

type aiConfigResponse struct {
	TenantID  string `json:"tenant_id"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	BaseURL   string `json:"base_url"`
	HasAPIKey bool   `json:"has_api_key"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type aiConfigPutRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	// APIKey: when empty, server preserves the previously stored key.
	APIKey string `json:"api_key"`
}

func (s *Server) handleAIConfig(w http.ResponseWriter, r *http.Request) {
	if !aiAskEnabled() {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleAIConfigGet(w, r)
	case http.MethodPut:
		s.handleAIConfigPut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleAIConfigGet(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	cfg, err := s.store.GetAIConfig(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("get ai config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	resp := aiConfigResponse{TenantID: tenantID.String(), Provider: "anthropic", Model: "claude-sonnet-4-6"}
	if cfg != nil {
		resp.Provider = cfg.Provider
		resp.Model = cfg.Model
		resp.BaseURL = cfg.BaseURL
		resp.HasAPIKey = cfg.APIKey != ""
		resp.UpdatedAt = cfg.UpdatedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAIConfigPut(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var body aiConfigPutRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if s.store == nil {
		http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
		return
	}
	cfg := storage.AIConfig{
		TenantID: tenantID,
		Provider: body.Provider,
		Model:    body.Model,
		BaseURL:  body.BaseURL,
		APIKey:   body.APIKey,
	}
	if err := s.store.UpsertAIConfig(r.Context(), cfg); err != nil {
		s.logger.Error("upsert ai config", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ── /api/v1/ai/test ───────────────────────────────────────────────────────

func (s *Server) handleAITest(w http.ResponseWriter, r *http.Request) {
	if !aiAskEnabled() {
		http.NotFound(w, r)
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
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cfg, err := s.store.GetAIConfig(r.Context(), tenantID)
	if err != nil || cfg == nil || cfg.APIKey == "" {
		http.Error(w, "ai config not set", http.StatusBadRequest)
		return
	}
	if cfg.Provider != "anthropic" {
		http.Error(w, "only anthropic is wired in this version", http.StatusBadRequest)
		return
	}
	out, err := anthropicMessage(r.Context(), *cfg, []anthropicMessageBlock{
		{Type: "text", Text: "Reply with exactly: ok"},
	}, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reply": out})
}

// ── /api/v1/ai/ask ────────────────────────────────────────────────────────

type aiAskRequest struct {
	Question string `json:"question"`
}

type aiAskResponse struct {
	Answer    string   `json:"answer"`
	Citations []string `json:"citations,omitempty"`
}

func (s *Server) handleAIAsk(w http.ResponseWriter, r *http.Request) {
	if !aiAskEnabled() {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var body aiAskRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Question) == "" {
		http.Error(w, "question required", http.StatusBadRequest)
		return
	}

	cfg, err := s.store.GetAIConfig(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("get ai config for ask", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if cfg == nil || cfg.APIKey == "" {
		http.Error(w, "ai config not set; configure provider + key in Settings → AI", http.StatusBadRequest)
		return
	}
	if cfg.Provider != "anthropic" {
		http.Error(w, "only anthropic is wired in this version", http.StatusBadRequest)
		return
	}

	// Build grounded context: knowledge_graph.md per tenant. Cached 5 min,
	// served as a single big string we mark cache_control: ephemeral so
	// repeat asks within the cache window are dirt cheap.
	kg, err := s.buildKnowledgeGraph(r, tenantID)
	if err != nil {
		s.logger.Error("build kg for ask", zap.Error(err))
		http.Error(w, "could not build context", http.StatusInternalServerError)
		return
	}

	systemBlocks := []anthropicMessageBlock{
		{
			Type: "text",
			Text: "You are a CISO assistant for the Control One security platform. " +
				"Answer questions strictly from the GROUNDED CONTEXT below. " +
				"When you reference a node, append [node:<hostname>] inline. " +
				"When you reference an IP, append [ip:<addr>]. " +
				"If the context does not answer the question, say so plainly. " +
				"Be concise — security operators value brevity.",
		},
		{
			Type:         "text",
			Text:         "GROUNDED CONTEXT (knowledge graph for this tenant):\n\n" + kg,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		},
	}

	userBlocks := []anthropicMessageBlock{
		{Type: "text", Text: body.Question},
	}

	answer, err := anthropicMessage(r.Context(), *cfg, userBlocks, systemBlocks)
	if err != nil {
		s.logger.Error("anthropic call", zap.Error(err))
		http.Error(w, fmt.Sprintf("provider error: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, aiAskResponse{Answer: answer})
}

// ── Anthropic HTTP wrapper ────────────────────────────────────────────────

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicMessageBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessageObj struct {
	Role    string                  `json:"role"`
	Content []anthropicMessageBlock `json:"content"`
}

type anthropicRequest struct {
	Model     string                  `json:"model"`
	MaxTokens int                     `json:"max_tokens"`
	System    []anthropicMessageBlock `json:"system,omitempty"`
	Messages  []anthropicMessageObj   `json:"messages"`
}

type anthropicResponse struct {
	Content []anthropicMessageBlock `json:"content"`
	Error   *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// anthropicMessage POSTs to /v1/messages and returns the concatenated text
// of the response. Honors cfg.BaseURL for self-hosted / proxy setups.
func anthropicMessage(ctx context.Context, cfg storage.AIConfig, userBlocks, systemBlocks []anthropicMessageBlock) (string, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	model := cfg.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	req := anthropicRequest{
		Model:     model,
		MaxTokens: 1024,
		System:    systemBlocks,
		Messages:  []anthropicMessageObj{{Role: "user", Content: userBlocks}},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", cfg.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("decode (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return "", fmt.Errorf("anthropic error: %s — %s", out.Error.Type, out.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(raw))
	}

	var b strings.Builder
	for _, blk := range out.Content {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
		}
	}
	return b.String(), nil
}

// tenantIDFromQuery resolves the tenant id from ?tenant_id=. Required for
// every /ai/* call so admins must explicitly scope which tenant's KG +
// config the request applies to.
func tenantIDFromQuery(r *http.Request, _ any) (uuid.UUID, error) {
	q := r.URL.Query().Get("tenant_id")
	if q == "" {
		return uuid.Nil, fmt.Errorf("tenant_id required")
	}
	id, err := uuid.Parse(q)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid tenant_id")
	}
	return id, nil
}
