package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestToolCallsExtractsProviderNeutralCalls(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: []ContentBlock{
		{Type: ContentText, Text: "looking"},
		{Type: ContentToolCall, ToolCall: &ToolCall{ID: "toolu_1", Name: "node_documentation", Input: map[string]any{"node_id": "n1"}}},
	}}
	calls := ToolCalls(msg)
	if len(calls) != 1 || calls[0].Name != "node_documentation" || calls[0].ID != "toolu_1" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestTextFromMessageConcatenatesOnlyTextBlocks(t *testing.T) {
	msg := Message{Role: RoleAssistant, Content: []ContentBlock{
		{Type: ContentText, Text: "first "},
		{Type: ContentToolCall, ToolCall: &ToolCall{ID: "toolu_1", Name: "node_documentation"}},
		{Type: ContentText, Text: "second"},
	}}
	if got := TextFromMessage(msg); got != "first second" {
		t.Fatalf("TextFromMessage() = %q", got)
	}
}

func TestNewClientRejectsUnsupportedProviders(t *testing.T) {
	if _, err := NewClient(ProviderConfig{Provider: "unknown"}); err != ErrUnsupportedProvider {
		t.Fatalf("expected ErrUnsupportedProvider, got %v", err)
	}
}

func TestNewClientAcceptsSupportedProviders(t *testing.T) {
	for _, provider := range []string{"anthropic", "openai", "google", "gemini"} {
		t.Run(provider, func(t *testing.T) {
			client, err := NewClient(ProviderConfig{Provider: provider, APIKey: "test"})
			if err != nil {
				t.Fatalf("NewClient(%q) error = %v", provider, err)
			}
			if client == nil {
				t.Fatalf("NewClient(%q) returned nil client", provider)
			}
		})
	}
}

func TestOpenAIClientEncodesToolsAndParsesToolCalls(t *testing.T) {
	var sawBearer bool
	var sawTool bool
	var sawToolResult bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got == "Bearer test-key" {
			sawBearer = true
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if tools, ok := req["tools"].([]any); ok && len(tools) == 1 {
			sawTool = true
		}
		if messages, ok := req["messages"].([]any); ok {
			for _, raw := range messages {
				msg, _ := raw.(map[string]any)
				if msg["role"] == "tool" && msg["tool_call_id"] == "call_1" {
					sawToolResult = true
				}
			}
		}
		_, _ = w.Write([]byte(`{
			"choices": [{
				"finish_reason": "tool_calls",
				"message": {
					"role": "assistant",
					"tool_calls": [{
						"id": "call_2",
						"type": "function",
						"function": {
							"name": "node_health",
							"arguments": "{\"node_id\":\"node-1\"}"
						}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	client := &OpenAIClient{Config: ProviderConfig{Provider: "openai", BaseURL: server.URL, APIKey: "test-key"}, HTTPClient: server.Client()}
	resp, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Messages: []Message{
			TextMessage(RoleUser, "investigate"),
			{Role: RoleTool, Content: []ContentBlock{{Type: ContentToolResult, ToolResult: &ToolResult{ToolCallID: "call_1", Content: `{"ok":true}`}}}},
		},
		Tools: []Tool{{Name: "node_health", Description: "health", InputSchema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if !sawBearer || !sawTool || !sawToolResult {
		t.Fatalf("request missing bearer=%v tool=%v toolResult=%v", sawBearer, sawTool, sawToolResult)
	}
	if resp.StopReason != StopToolUse {
		t.Fatalf("StopReason = %q", resp.StopReason)
	}
	calls := ToolCalls(resp.Message)
	if len(calls) != 1 || calls[0].ID != "call_2" || calls[0].Name != "node_health" || calls[0].Input["node_id"] != "node-1" {
		t.Fatalf("unexpected tool calls: %+v", calls)
	}
}

func TestGeminiClientEncodesToolsAndParsesFunctionCalls(t *testing.T) {
	var sawKey bool
	var sawTool bool
	var sawFunctionResponse bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "key=test-key") {
			sawKey = true
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if tools, ok := req["tools"].([]any); ok && len(tools) == 1 {
			sawTool = true
		}
		if contents, ok := req["contents"].([]any); ok {
			for _, raw := range contents {
				content, _ := raw.(map[string]any)
				parts, _ := content["parts"].([]any)
				for _, partRaw := range parts {
					part, _ := partRaw.(map[string]any)
					if _, ok := part["functionResponse"]; ok {
						sawFunctionResponse = true
					}
				}
			}
		}
		_, _ = w.Write([]byte(`{
			"candidates": [{
				"finishReason": "STOP",
				"content": {
					"role": "model",
					"parts": [{
						"functionCall": {
							"name": "node_health",
							"args": {"node_id": "node-1"}
						}
					}]
				}
			}]
		}`))
	}))
	defer server.Close()

	client := &GeminiClient{Config: ProviderConfig{Provider: "google", BaseURL: server.URL, APIKey: "test-key"}, HTTPClient: server.Client()}
	resp, err := client.Generate(context.Background(), Request{
		System: "system prompt",
		Messages: []Message{
			TextMessage(RoleUser, "investigate"),
			{Role: RoleTool, Content: []ContentBlock{{Type: ContentToolResult, ToolResult: &ToolResult{ToolCallID: "node_health", Content: `{"ok":true}`}}}},
		},
		Tools: []Tool{{Name: "node_health", Description: "health", InputSchema: map[string]any{"type": "object"}}},
	})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if !sawKey || !sawTool || !sawFunctionResponse {
		t.Fatalf("request missing key=%v tool=%v functionResponse=%v", sawKey, sawTool, sawFunctionResponse)
	}
	if resp.StopReason != StopToolUse {
		t.Fatalf("StopReason = %q", resp.StopReason)
	}
	calls := ToolCalls(resp.Message)
	if len(calls) != 1 || calls[0].ID != "node_health" || calls[0].Name != "node_health" || calls[0].Input["node_id"] != "node-1" {
		t.Fatalf("unexpected tool calls: %+v", calls)
	}
}

func TestFallbackClientRetriesRetryableProviderErrors(t *testing.T) {
	client := &FallbackClient{Clients: []Client{
		stubClient{err: ProviderError{Provider: "anthropic", StatusCode: http.StatusBadGateway, Message: "upstream"}},
		stubClient{resp: Response{Message: TextMessage(RoleAssistant, "ok"), StopReason: StopEndTurn}},
	}}
	resp, err := client.Generate(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err != nil {
		t.Fatalf("Generate error = %v", err)
	}
	if got := TextFromMessage(resp.Message); got != "ok" {
		t.Fatalf("fallback response = %q", got)
	}
}

func TestFallbackClientStopsOnNonRetryableErrors(t *testing.T) {
	client := &FallbackClient{Clients: []Client{
		stubClient{err: ProviderError{Provider: "anthropic", StatusCode: http.StatusBadRequest, Message: "bad request"}},
		stubClient{resp: Response{Message: TextMessage(RoleAssistant, "ok"), StopReason: StopEndTurn}},
	}}
	_, err := client.Generate(context.Background(), Request{Messages: []Message{TextMessage(RoleUser, "hi")}})
	if err == nil {
		t.Fatal("expected non-retryable error")
	}
}

type stubClient struct {
	resp Response
	err  error
}

func (c stubClient) Generate(context.Context, Request) (Response, error) {
	return c.resp, c.err
}
