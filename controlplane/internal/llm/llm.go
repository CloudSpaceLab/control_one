package llm

import (
	"context"
	"errors"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentType string

const (
	ContentText       ContentType = "text"
	ContentToolCall   ContentType = "tool_call"
	ContentToolResult ContentType = "tool_result"
)

type ContentBlock struct {
	Type       ContentType    `json:"type"`
	Text       string         `json:"text,omitempty"`
	ToolCall   *ToolCall      `json:"tool_call,omitempty"`
	ToolResult *ToolResult    `json:"tool_result,omitempty"`
	Citations  []Citation     `json:"citations,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Message struct {
	Role    Role           `json:"role"`
	Content []ContentBlock `json:"content"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type ToolCall struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

type ToolResult struct {
	ToolCallID string `json:"tool_call_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

type Citation struct {
	ID     string `json:"id"`
	Tool   string `json:"tool,omitempty"`
	Label  string `json:"label,omitempty"`
	Detail string `json:"detail,omitempty"`
}

type Request struct {
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	Tools     []Tool    `json:"tools,omitempty"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

type StopReason string

const (
	StopEndTurn StopReason = "end_turn"
	StopToolUse StopReason = "tool_use"
)

type Response struct {
	Message    Message    `json:"message"`
	StopReason StopReason `json:"stop_reason"`
}

type Client interface {
	Generate(context.Context, Request) (Response, error)
}

var ErrUnsupportedProvider = errors.New("unsupported llm provider")

func TextMessage(role Role, text string) Message {
	return Message{Role: role, Content: []ContentBlock{{Type: ContentText, Text: text}}}
}

func TextFromMessage(msg Message) string {
	out := ""
	for _, block := range msg.Content {
		if block.Type == ContentText {
			out += block.Text
		}
	}
	return out
}

func ToolCalls(msg Message) []ToolCall {
	var out []ToolCall
	for _, block := range msg.Content {
		if block.Type == ContentToolCall && block.ToolCall != nil {
			out = append(out, *block.ToolCall)
		}
	}
	return out
}
