package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/llm"
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
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
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
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
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
	if !s.requireTenantAccess(w, r, principal, tenantID, roleAdmin) {
		return
	}
	cfg, err := s.store.GetAIConfig(r.Context(), tenantID)
	if err != nil || cfg == nil || cfg.APIKey == "" {
		http.Error(w, "ai config not set", http.StatusBadRequest)
		return
	}
	client, err := s.aiClientForConfig(*cfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("provider error: %v", err), http.StatusBadRequest)
		return
	}
	out, err := client.Generate(r.Context(), llm.Request{
		Messages:  []llm.Message{llm.TextMessage(llm.RoleUser, "Reply with exactly: ok")},
		MaxTokens: 32,
	})
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "reply": llm.TextFromMessage(out.Message)})
}

// ── /api/v1/ai/ask ────────────────────────────────────────────────────────

type aiAskRequest struct {
	Question string `json:"question"`
}

type aiAskResponse struct {
	Answer          string             `json:"answer"`
	Citations       []string           `json:"citations,omitempty"`
	SourceCitations []string           `json:"source_citations,omitempty"`
	ToolTrace       []aiToolTraceEntry `json:"tool_trace,omitempty"`
	Confidence      string             `json:"confidence,omitempty"`
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
	principal, ok := s.authorize(w, r, roleOperator, roleInvestigator, roleAdmin)
	if !ok {
		return
	}
	tenantID, err := tenantIDFromQuery(r, principal)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !s.requireTenantAccess(w, r, principal, tenantID, roleOperator, roleInvestigator, roleAdmin) {
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
	client, err := s.aiClientForConfig(*cfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("provider error: %v", err), http.StatusBadRequest)
		return
	}

	// Build grounded context: per-tenant knowledge graph, compressed to
	// an 8K-token budget for this specific question. Stage 1 (cached
	// 5 min) dedupes nodes by (os, arch, agent, state); stage 2 scores
	// the cached sections against the question and greedy-packs them.
	// See kg_compress.go for the per-request compressor.
	sections, err := s.getCachedKGSections(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("build kg for ask", zap.Error(err))
		http.Error(w, "could not build context", http.StatusInternalServerError)
		return
	}
	kg := compressForQuery(sections, body.Question, 8192)

	resp, err := s.runAIAskToolLoop(r.Context(), principal, tenantID, client, strings.TrimSpace(body.Question), kg)
	if err != nil {
		s.logger.Error("ai ask tool loop", zap.Error(err))
		http.Error(w, fmt.Sprintf("provider error: %v", err), http.StatusBadGateway)
		return
	}

	writeJSON(w, http.StatusOK, resp)
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

func (s *Server) aiClientForConfig(cfg storage.AIConfig) (llm.Client, error) {
	if s.aiClientFactory != nil {
		return s.aiClientFactory(cfg)
	}
	return llm.NewClient(llm.ProviderConfig{
		Provider: cfg.Provider,
		Model:    cfg.Model,
		BaseURL:  cfg.BaseURL,
		APIKey:   cfg.APIKey,
	})
}

func (s *Server) runAIAskToolLoop(ctx context.Context, principal any, tenantID uuid.UUID, client llm.Client, question, kg string) (aiAskResponse, error) {
	authPrincipal, _ := principal.(*auth.Principal)
	loopCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	system := "You are a CISO assistant for the Control One security platform. " +
		"Answer questions strictly from the grounded context and tool results below. " +
		"When you use a tool result, cite its tool citation id inline like [tool:node_documentation:1], " +
		"and prefer source-row citation ids from the tool payload when a raw event, evidence row, policy, posture receipt, or finding supports the claim. " +
		"If evidence is unavailable, say which evidence is unavailable. " +
		"Never claim that an operator action executed unless a tool result says it executed. " +
		"Be concise; security operators value brevity.\n\n" +
		"GROUNDED CONTEXT (compressed knowledge graph for this tenant):\n\n" + kg
	messages := []llm.Message{llm.TextMessage(llm.RoleUser, question)}
	tools := s.aiToolSpecs()
	var traces []aiToolTraceEntry
	var citations []string
	var sourceCitations []string
	citationSeq := 0

	for step := 0; step < 12; step++ {
		resp, err := client.Generate(loopCtx, llm.Request{System: system, Messages: messages, Tools: tools, MaxTokens: 1024})
		if err != nil {
			return aiAskResponse{}, err
		}
		messages = append(messages, resp.Message)
		calls := llm.ToolCalls(resp.Message)
		if resp.StopReason != llm.StopToolUse || len(calls) == 0 {
			answer := llm.TextFromMessage(resp.Message)
			if strings.TrimSpace(answer) == "" {
				answer = "No answer was produced."
			}
			return aiAskResponse{Answer: answer, Citations: citations, SourceCitations: sourceCitations, ToolTrace: traces, Confidence: confidenceFromTrace(traces)}, nil
		}

		resultBlocks := make([]llm.ContentBlock, 0, len(calls))
		for _, call := range calls {
			start := time.Now()
			exec, err := s.executeAITool(loopCtx, authPrincipal, tenantID, call)
			trace := aiToolTraceEntry{Name: call.Name, DurationMS: time.Since(start).Milliseconds()}
			if err != nil {
				trace.OK = false
				trace.Error = err.Error()
				traces = append(traces, trace)
				resultBlocks = append(resultBlocks, llm.ContentBlock{
					Type: llm.ContentToolResult,
					ToolResult: &llm.ToolResult{
						ToolCallID: call.ID,
						Content:    `{"error":` + strconv.Quote(err.Error()) + `}`,
						IsError:    true,
					},
				})
				continue
			}
			citationSeq++
			exec.Citation.ID = fmt.Sprintf("tool:%s:%d", call.Name, citationSeq)
			payload, err := encodeToolPayload(exec)
			if err != nil {
				return aiAskResponse{}, err
			}
			trace.OK = true
			trace.CitationID = exec.Citation.ID
			traces = append(traces, trace)
			citations = append(citations, exec.Citation.ID)
			sourceCitations = appendUniqueStrings(sourceCitations, sourceCitationIDsFromToolPayload(exec.Payload)...)
			s.recordAudit(loopCtx, authPrincipal, tenantID, "ai.tool_call", "ai_tool", call.Name, map[string]any{
				"tool":        call.Name,
				"citation_id": exec.Citation.ID,
			})
			resultBlocks = append(resultBlocks, llm.ContentBlock{
				Type: llm.ContentToolResult,
				ToolResult: &llm.ToolResult{
					ToolCallID: call.ID,
					Content:    payload,
				},
			})
		}
		messages = append(messages, llm.Message{Role: llm.RoleTool, Content: resultBlocks})
	}
	return aiAskResponse{}, fmt.Errorf("tool-use loop exceeded 12 steps")
}

func confidenceFromTrace(traces []aiToolTraceEntry) string {
	if len(traces) == 0 {
		return "context_only"
	}
	for _, trace := range traces {
		if !trace.OK {
			return "partial"
		}
	}
	return "grounded"
}

func appendUniqueStrings(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		values = appendUniqueString(values, candidate)
	}
	return values
}

func sourceCitationIDsFromToolPayload(payload any) []string {
	if payload == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil
	}
	out := []string{}
	walkSourceCitationIDs(decoded, &out)
	return out
}

func walkSourceCitationIDs(value any, out *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			lowerKey := strings.ToLower(strings.TrimSpace(key))
			if lowerKey == "id" || lowerKey == "citation_id" || lowerKey == "source_record_id" {
				if text, ok := child.(string); ok && looksLikeSourceCitationID(text) {
					*out = appendUniqueString(*out, text)
					continue
				}
			}
			walkSourceCitationIDs(child, out)
		}
	case []any:
		for _, child := range v {
			walkSourceCitationIDs(child, out)
		}
	case string:
		if looksLikeSourceCitationID(v) {
			*out = appendUniqueString(*out, v)
		}
	}
}

func looksLikeSourceCitationID(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "tool:") || !strings.Contains(value, ":") {
		return false
	}
	prefix := strings.ToLower(strings.TrimSpace(strings.SplitN(value, ":", 2)[0]))
	switch prefix {
	case "alerts",
		"audit_logs",
		"compliance_evidence",
		"compliance_results",
		"db_queries",
		"events",
		"file_accesses",
		"ip_behavior_findings",
		"lifecycle",
		"network_connections",
		"node_health_scores",
		"node_services",
		"node_vulnerability_findings",
		"normalized_events",
		"process_connections",
		"process_events",
		"saved_searches",
		"security_events",
		"tenant_event_filters",
		"ai_investigations":
		return true
	default:
		return false
	}
}
