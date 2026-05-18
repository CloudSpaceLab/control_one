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

type OpenAIClient struct {
	Config     ProviderConfig
	HTTPClient *http.Client
}

type openAIRequest struct {
	Model               string          `json:"model"`
	Messages            []openAIMessage `json:"messages"`
	Tools               []openAITool    `json:"tools,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
}

type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}

type openAITool struct {
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Arguments   string         `json:"arguments,omitempty"`
}

type openAIToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function openAIFunction `json:"function"`
}

type openAIResponse struct {
	Choices []struct {
		Message      openAIMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

func (c *OpenAIClient) Generate(ctx context.Context, req Request) (Response, error) {
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	base := strings.TrimRight(c.Config.BaseURL, "/")
	if base == "" {
		base = "https://api.openai.com"
	}
	model := c.Config.Model
	if model == "" {
		model = "gpt-4o-mini"
	}

	payload := openAIRequest{
		Model:               model,
		Messages:            toOpenAIMessages(req),
		Tools:               toOpenAITools(req.Tools),
		MaxCompletionTokens: req.MaxTokens,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Response{}, fmt.Errorf("marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.Config.APIKey)

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("post: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, fmt.Errorf("read body: %w", err)
	}
	var out openAIResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return Response{}, fmt.Errorf("decode (status %d): %w", resp.StatusCode, err)
	}
	if out.Error != nil {
		return Response{}, ProviderError{Provider: "openai", StatusCode: resp.StatusCode, Message: firstNonEmpty(out.Error.Message, out.Error.Type)}
	}
	if resp.StatusCode >= 400 {
		return Response{}, ProviderError{Provider: "openai", StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if len(out.Choices) == 0 {
		return Response{}, ProviderError{Provider: "openai", Message: "empty choices"}
	}

	choice := out.Choices[0]
	msg := Message{Role: RoleAssistant, Content: fromOpenAIMessage(choice.Message)}
	stop := StopReason(choice.FinishReason)
	if len(ToolCalls(msg)) > 0 || choice.FinishReason == "tool_calls" {
		stop = StopToolUse
	} else if stop == "" || stop == "stop" {
		stop = StopEndTurn
	}
	return Response{Message: msg, StopReason: stop}, nil
}

func toOpenAIMessages(req Request) []openAIMessage {
	out := make([]openAIMessage, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		out = append(out, openAIMessage{Role: "system", Content: req.System})
	}
	for _, msg := range req.Messages {
		switch msg.Role {
		case RoleTool:
			for _, block := range msg.Content {
				if block.Type == ContentToolResult && block.ToolResult != nil {
					out = append(out, openAIMessage{
						Role:       "tool",
						ToolCallID: block.ToolResult.ToolCallID,
						Content:    block.ToolResult.Content,
					})
				}
			}
		case RoleAssistant:
			out = append(out, openAIMessage{
				Role:      "assistant",
				Content:   textFromBlocks(msg.Content),
				ToolCalls: toOpenAIToolCalls(msg.Content),
			})
		default:
			out = append(out, openAIMessage{Role: "user", Content: textFromBlocks(msg.Content)})
		}
	}
	return out
}

func toOpenAITools(tools []Tool) []openAITool {
	out := make([]openAITool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, openAITool{
			Type: "function",
			Function: openAIFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}
	return out
}

func toOpenAIToolCalls(blocks []ContentBlock) []openAIToolCall {
	out := make([]openAIToolCall, 0)
	for _, block := range blocks {
		if block.Type != ContentToolCall || block.ToolCall == nil {
			continue
		}
		args, _ := json.Marshal(block.ToolCall.Input)
		out = append(out, openAIToolCall{
			ID:   block.ToolCall.ID,
			Type: "function",
			Function: openAIFunction{
				Name:      block.ToolCall.Name,
				Arguments: string(args),
			},
		})
	}
	return out
}

func fromOpenAIMessage(msg openAIMessage) []ContentBlock {
	out := make([]ContentBlock, 0, 1+len(msg.ToolCalls))
	if msg.Content != "" {
		out = append(out, ContentBlock{Type: ContentText, Text: msg.Content})
	}
	for _, call := range msg.ToolCalls {
		input := map[string]any{}
		if strings.TrimSpace(call.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(call.Function.Arguments), &input); err != nil {
				input = map[string]any{"_raw": call.Function.Arguments}
			}
		}
		out = append(out, ContentBlock{Type: ContentToolCall, ToolCall: &ToolCall{
			ID:    call.ID,
			Name:  call.Function.Name,
			Input: input,
		}})
	}
	return out
}

func textFromBlocks(blocks []ContentBlock) string {
	var out strings.Builder
	for _, block := range blocks {
		if block.Type == ContentText {
			out.WriteString(block.Text)
		}
	}
	return out.String()
}
