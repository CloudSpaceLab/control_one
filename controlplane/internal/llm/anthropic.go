package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ProviderConfig struct {
	Provider string
	Model    string
	BaseURL  string
	APIKey   string
}

func NewClient(cfg ProviderConfig) (Client, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "anthropic":
		return &AnthropicClient{Config: cfg, HTTPClient: &http.Client{Timeout: 60 * time.Second}}, nil
	default:
		return nil, ErrUnsupportedProvider
	}
}

type AnthropicClient struct {
	Config     ProviderConfig
	HTTPClient *http.Client
}

type anthropicCacheControl struct {
	Type string `json:"type"`
}

type anthropicBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text,omitempty"`
	ID           string                 `json:"id,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Input        map[string]any         `json:"input,omitempty"`
	ToolUseID    string                 `json:"tool_use_id,omitempty"`
	Content      string                 `json:"content,omitempty"`
	IsError      bool                   `json:"is_error,omitempty"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    []anthropicBlock   `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicBlock `json:"content"`
	StopReason string           `json:"stop_reason"`
	Error      *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (c *AnthropicClient) Generate(ctx context.Context, req Request) (Response, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	base := strings.TrimRight(c.Config.BaseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	model := c.Config.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	payload := anthropicRequest{
		Model:     model,
		MaxTokens: req.MaxTokens,
		System:    []anthropicBlock{{Type: "text", Text: req.System, CacheControl: &anthropicCacheControl{Type: "ephemeral"}}},
		Messages:  make([]anthropicMessage, 0, len(req.Messages)),
		Tools:     make([]anthropicTool, 0, len(req.Tools)),
	}
	if payload.MaxTokens == 0 {
		payload.MaxTokens = 1024
	}
	for _, msg := range req.Messages {
		role := string(msg.Role)
		if msg.Role == RoleTool {
			role = string(RoleUser)
		}
		payload.Messages = append(payload.Messages, anthropicMessage{Role: role, Content: toAnthropicBlocks(msg.Content)})
	}
	for _, tool := range req.Tools {
		payload.Tools = append(payload.Tools, anthropicTool(tool))
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", c.Config.APIKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read body: %w", err)
	}
	var out anthropicResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("decode (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return Response{}, fmt.Errorf("anthropic error: %s - %s", out.Error.Type, out.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return Response{}, fmt.Errorf("anthropic status %d: %s", resp.StatusCode, string(raw))
	}

	msg := Message{Role: RoleAssistant, Content: fromAnthropicBlocks(out.Content)}
	stop := StopReason(out.StopReason)
	if stop == "" {
		if len(ToolCalls(msg)) > 0 {
			stop = StopToolUse
		} else {
			stop = StopEndTurn
		}
	}
	return Response{Message: msg, StopReason: stop}, nil
}

func toAnthropicBlocks(blocks []ContentBlock) []anthropicBlock {
	out := make([]anthropicBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentToolCall:
			if block.ToolCall != nil {
				out = append(out, anthropicBlock{Type: "tool_use", ID: block.ToolCall.ID, Name: block.ToolCall.Name, Input: block.ToolCall.Input})
			}
		case ContentToolResult:
			if block.ToolResult != nil {
				out = append(out, anthropicBlock{Type: "tool_result", ToolUseID: block.ToolResult.ToolCallID, Content: block.ToolResult.Content, IsError: block.ToolResult.IsError})
			}
		default:
			out = append(out, anthropicBlock{Type: "text", Text: block.Text})
		}
	}
	return out
}

func fromAnthropicBlocks(blocks []anthropicBlock) []ContentBlock {
	out := make([]ContentBlock, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case "tool_use":
			out = append(out, ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{ID: block.ID, Name: block.Name, Input: block.Input}})
		default:
			out = append(out, ContentBlock{Type: ContentText, Text: block.Text})
		}
	}
	return out
}
