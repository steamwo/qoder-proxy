package server

import (
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestNormalizeQoderRequestMapsToolRoleToUserToolResult(t *testing.T) {
	req := protocol.Request{Messages: []map[string]any{
		{"role": "assistant", "content": "calling tool", "tool_calls": []any{map[string]any{"id": "call_1"}}},
		{"role": "tool", "tool_call_id": "call_1", "content": "sunny", "is_error": false},
		{"role": "user", "content": "continue"},
	}}
	got := normalizeQoderRequest(req)
	if len(got.Messages) != 3 {
		t.Fatalf("messages=%#v", got.Messages)
	}
	if got.Messages[0]["role"] != "assistant" {
		t.Fatalf("assistant message changed: %#v", got.Messages[0])
	}
	toolFeedback := got.Messages[1]
	if toolFeedback["role"] != "user" {
		t.Fatalf("tool feedback role=%#v", toolFeedback["role"])
	}
	blocks, ok := toolFeedback["content"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("tool feedback content=%#v", toolFeedback["content"])
	}
	block, ok := blocks[0].(map[string]any)
	if !ok {
		t.Fatalf("tool result block=%#v", blocks[0])
	}
	if block["type"] != "tool_result" || block["tool_use_id"] != "call_1" || block["content"] != "sunny" || block["is_error"] != false {
		t.Fatalf("tool result=%#v", block)
	}
	if got.Messages[2]["role"] != "user" || got.Messages[2]["content"] != "continue" {
		t.Fatalf("user message changed: %#v", got.Messages[2])
	}
}

func TestNormalizeQoderRequestLeavesUserAssistantConversationUntouched(t *testing.T) {
	original := []map[string]any{{"role": "user", "content": "hello"}, {"role": "assistant", "content": "hi"}}
	got := normalizeQoderRequest(protocol.Request{Messages: original})
	if got.Messages[0]["role"] != "user" || got.Messages[1]["role"] != "assistant" {
		t.Fatalf("messages=%#v", got.Messages)
	}
}
