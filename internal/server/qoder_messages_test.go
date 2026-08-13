package server

import (
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestNormalizeQoderRequestMapsCompleteToolRoundTrip(t *testing.T) {
	req := protocol.Request{Messages: []map[string]any{
		{
			"role":    "assistant",
			"content": "Checking weather",
			"tool_calls": []any{
				map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "weather", "arguments": `{"city":"Tokyo"}`}},
				map[string]any{"id": "call_2", "type": "function", "function": map[string]any{"name": "time", "arguments": `{}`}},
			},
		},
		{"role": "tool", "tool_call_id": "call_1", "content": "sunny", "is_error": false},
		{"role": "tool", "tool_call_id": "call_2", "content": "14:00"},
		{"role": "user", "content": "continue"},
	}}

	got := normalizeQoderRequest(req)
	if len(got.Messages) != 2 {
		t.Fatalf("messages=%#v", got.Messages)
	}

	assistant := got.Messages[0]
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant role=%#v", assistant["role"])
	}
	if _, exists := assistant["tool_calls"]; exists {
		t.Fatalf("OpenAI tool_calls leaked to Qoder: %#v", assistant)
	}
	assistantBlocks, ok := assistant["content"].([]any)
	if !ok || len(assistantBlocks) != 3 {
		t.Fatalf("assistant content=%#v", assistant["content"])
	}
	textBlock := assistantBlocks[0].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "Checking weather" {
		t.Fatalf("assistant text=%#v", textBlock)
	}
	firstUse := assistantBlocks[1].(map[string]any)
	if firstUse["type"] != "tool_use" || firstUse["id"] != "call_1" || firstUse["name"] != "weather" {
		t.Fatalf("first tool use=%#v", firstUse)
	}
	input, ok := firstUse["input"].(map[string]any)
	if !ok || input["city"] != "Tokyo" {
		t.Fatalf("first tool input=%#v", firstUse["input"])
	}

	user := got.Messages[1]
	if user["role"] != "user" {
		t.Fatalf("user role=%#v", user["role"])
	}
	userBlocks, ok := user["content"].([]any)
	if !ok || len(userBlocks) != 3 {
		t.Fatalf("user content=%#v", user["content"])
	}
	firstResult := userBlocks[0].(map[string]any)
	secondResult := userBlocks[1].(map[string]any)
	followingText := userBlocks[2].(map[string]any)
	if firstResult["type"] != "tool_result" || firstResult["tool_use_id"] != "call_1" || firstResult["content"] != "sunny" || firstResult["is_error"] != false {
		t.Fatalf("first result=%#v", firstResult)
	}
	if secondResult["type"] != "tool_result" || secondResult["tool_use_id"] != "call_2" || secondResult["content"] != "14:00" {
		t.Fatalf("second result=%#v", secondResult)
	}
	if followingText["type"] != "text" || followingText["text"] != "continue" {
		t.Fatalf("following text=%#v", followingText)
	}
}

func TestNormalizeQoderRequestMapsToolRoleWithoutFollowingUser(t *testing.T) {
	req := protocol.Request{Messages: []map[string]any{
		{"role": "assistant", "content": "calling", "tool_calls": []any{map[string]any{"id": "call_1", "function": map[string]any{"name": "weather", "arguments": `{}`}}}},
		{"role": "tool", "tool_call_id": "call_1", "content": "sunny"},
	}}
	got := normalizeQoderRequest(req)
	if len(got.Messages) != 2 || got.Messages[1]["role"] != "user" {
		t.Fatalf("messages=%#v", got.Messages)
	}
	blocks, ok := got.Messages[1]["content"].([]any)
	if !ok || len(blocks) != 1 {
		t.Fatalf("tool feedback=%#v", got.Messages[1])
	}
}

func TestNormalizeQoderRequestLeavesUserAssistantConversationUntouched(t *testing.T) {
	original := []map[string]any{{"role": "user", "content": "hello"}, {"role": "assistant", "content": "hi"}}
	got := normalizeQoderRequest(protocol.Request{Messages: original})
	if got.Messages[0]["role"] != "user" || got.Messages[1]["role"] != "assistant" {
		t.Fatalf("messages=%#v", got.Messages)
	}
	if got.Messages[0]["content"] != "hello" || got.Messages[1]["content"] != "hi" {
		t.Fatalf("plain conversation changed: %#v", got.Messages)
	}
}
