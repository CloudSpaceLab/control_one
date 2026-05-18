package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GeminiClient struct {
	Config     ProviderConfig
	HTTPClient *http.Client
}

type geminiRequest struct {
	SystemInstruction *geminiContent          `json:"system_instruction,omitempty"`
	Contents          []geminiContent         `json:"contents"`
	Tools             []geminiTool            `json:"tools,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDeclaration `json:"functionDeclarations"`
}

type geminiFunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func (c *GeminiClient) Generate(ctx context.Context, req Request) (Response, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	base := strings.TrimRight(c.Config.BaseURL, "/")
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}
	model := strings.TrimPrefix(c.Config.Model, "models/")
	if model == "" {
		model = "gemini-2.5-flash"
	}

	payload := geminiRequest{
		Contents: toGeminiContents(req.Messages),
		Tools:    toGeminiTools(req.Tools),
	}
	if strings.TrimSpace(req.System) != "" {
		payload.SystemInstruction = &geminiContent{Parts: []geminiPart{{Text: req.System}}}
	}
	if req.MaxTokens > 0 {
		payload.GenerationConfig = &geminiGenerationConfig{MaxOutputTokens: req.MaxTokens}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("marshal: %w", err)
	}
	endpoint := fmt.Sprintf("%s/v1beta/models/%s:generateContent?key=%s", base, url.PathEscape(model), url.QueryEscape(c.Config.APIKey))
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read body: %w", err)
	}
	var out geminiResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("decode (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return Response{}, ProviderError{Provider: "google", StatusCode: resp.StatusCode, Message: firstNonEmpty(out.Error.Message, out.Error.Status)}
	}
	if resp.StatusCode >= 400 {
		return Response{}, ProviderError{Provider: "google", StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if len(out.Candidates) == 0 {
		return Response{}, ProviderError{Provider: "google", Message: "empty candidates"}
	}

	choice := out.Candidates[0]
	msg := Message{Role: RoleAssistant, Content: fromGeminiParts(choice.Content.Parts)}
	stop := StopReason(strings.ToLower(choice.FinishReason))
	if len(ToolCalls(msg)) > 0 {
		stop = StopToolUse
	} else if stop == "" || stop == "stop" {
		stop = StopEndTurn
	}
	return Response{Message: msg, StopReason: stop}, nil
}

func toGeminiContents(messages []Message) []geminiContent {
	out := make([]geminiContent, 0, len(messages))
	for _, msg := range messages {
		content := geminiContent{Role: "user"}
		switch msg.Role {
		case RoleAssistant:
			content.Role = "model"
		case RoleTool:
			content.Role = "function"
		}
		content.Parts = toGeminiParts(msg.Content)
		if len(content.Parts) > 0 {
			out = append(out, content)
		}
	}
	return out
}

func toGeminiParts(blocks []ContentBlock) []geminiPart {
	out := make([]geminiPart, 0, len(blocks))
	for _, block := range blocks {
		switch block.Type {
		case ContentToolCall:
			if block.ToolCall != nil {
				out = append(out, geminiPart{FunctionCall: &geminiFunctionCall{Name: block.ToolCall.Name, Args: block.ToolCall.Input}})
			}
		case ContentToolResult:
			if block.ToolResult != nil {
				response := map[string]any{}
				if err := json.Unmarshal([]byte(block.ToolResult.Content), &response); err != nil {
					response = map[string]any{"content": block.ToolResult.Content}
				}
				out = append(out, geminiPart{FunctionResponse: &geminiFunctionResponse{Name: block.ToolResult.ToolCallID, Response: response}})
			}
		default:
			if block.Text != "" {
				out = append(out, geminiPart{Text: block.Text})
			}
		}
	}
	return out
}

func toGeminiTools(tools []Tool) []geminiTool {
	if len(tools) == 0 {
		return nil
	}
	declarations := make([]geminiFunctionDeclaration, 0, len(tools))
	for _, tool := range tools {
		declarations = append(declarations, geminiFunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  tool.InputSchema,
		})
	}
	return []geminiTool{{FunctionDeclarations: declarations}}
}

func fromGeminiParts(parts []geminiPart) []ContentBlock {
	out := make([]ContentBlock, 0, len(parts))
	for _, part := range parts {
		if part.FunctionCall != nil {
			out = append(out, ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{
				ID:    part.FunctionCall.Name,
				Name:  part.FunctionCall.Name,
				Input: part.FunctionCall.Args,
			}})
			continue
		}
		if part.Text != "" {
			out = append(out, ContentBlock{Type: ContentText, Text: part.Text})
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
