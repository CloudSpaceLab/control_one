package llm

import "testing"

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
