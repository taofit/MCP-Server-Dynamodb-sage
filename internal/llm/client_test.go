package llm

import (
	"encoding/json"
	"testing"
)

func TestMessagesToAnthropic(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
		{Role: "user", Content: "How are you?"},
	}

	result := constructMessages(messages)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
}

func TestMessagesToAnthropic_SkipsEmpty(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: ""},
		{Role: "user", Content: "   "},
		{Role: "user", Content: "World"},
	}

	result := constructMessages(messages)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages (empty skipped), got %d", len(result))
	}
}

func TestMessagesToAnthropic_ToolCalls(t *testing.T) {
	messages := []Message{
		{Role: "user", Content: "List my tables"},
		{
			Role:      "assistant",
			ToolCalls: []ToolCall{{ID: "tc_1", Name: "list_tables", Arguments: json.RawMessage(`{}`)}},
		},
		{
			Role: "user",
			ToolResults: []ToolResult{{ToolCallID: "tc_1", DisplayName: "list_tables", Result: `["users"]`}},
		},
		{Role: "assistant", Content: "You have one table: users"},
	}

	result := constructMessages(messages)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
}

func TestMessagesToAnthropic_ToolResultIsError(t *testing.T) {
	messages := []Message{
		{
			Role: "user",
			ToolResults: []ToolResult{{ToolCallID: "tc_1", DisplayName: "get_item", Result: "table not found", IsError: true}},
		},
	}

	result := constructMessages(messages)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
}
