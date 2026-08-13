package server

import (
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestNormalizeQoderRequestPreservesCanonicalToolRoundTrip(t *testing.T) {
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
	if len(got.Messages) != 4 {
		t.Fatalf("messages=%#v", got.Messages)
	}
	assistant := got.Messages[0]
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant=%#v", assistant)
	}
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 2 {
		t.Fatalf("canonical tool_calls were changed: %#v", assistant)
	}
	if got.Messages[1]["role"] != "tool" || got.Messages[1]["tool_call_id"] != "call_1" {
		t.Fatalf("first tool result=%#v", got.Messages[1])
	}
	if got.Messages[2]["role"] != "tool" || got.Messages[2]["tool_call_id"] != "call_2" {
		t.Fatalf("second tool result=%#v", got.Messages[2])
	}
	if got.Messages[3]["role"] != "user" || got.Messages[3]["content"] != "continue" {
		t.Fatalf("following user=%#v", got.Messages[3])
	}
}

func TestNormalizeQoderRequestConstrainsAdvertisedTools(t *testing.T) {
	req := protocol.Request{
		System:   "base system",
		Messages: []map[string]any{{"role": "user", "content": "inspect"}},
		Tools: []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "ToolSearch", "parameters": map[string]any{"type": "object"}}},
			map[string]any{"type": "function", "function": map[string]any{"name": "Read", "parameters": map[string]any{"type": "object"}}},
		},
	}
	got := normalizeQoderRequest(req)
	for _, want := range []string{
		"base system",
		"[qoder-proxy tool availability]",
		"Only call tools present in the current request's tool schemas.",
		"If a desired tool is not present and ToolSearch is available, call ToolSearch first to load it.",
		"Do not invent or use Qoder-only tools.",
	} {
		if !strings.Contains(got.System, want) {
			t.Fatalf("system missing %q: %s", want, got.System)
		}
	}
	if strings.Contains(got.System, "Read, ToolSearch") {
		t.Fatalf("tool names should not be duplicated into the system prompt: %s", got.System)
	}
}

func TestNormalizeQoderRequestMovesToolResultBeforeInterleavedUserText(t *testing.T) {
	req := protocol.Request{Messages: []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "grep", "arguments": `{}`}}}},
		{"role": "user", "content": "<system-reminder>continue exploring</system-reminder>"},
		{"role": "tool", "tool_call_id": "call_1", "content": "match"},
		{"role": "assistant", "content": "next"},
	}}

	got := normalizeQoderRequest(req)
	if len(got.Messages) != 4 {
		t.Fatalf("messages=%#v", got.Messages)
	}
	if got.Messages[1]["role"] != "tool" || got.Messages[1]["tool_call_id"] != "call_1" {
		t.Fatalf("tool result was not moved next to tool_calls: %#v", got.Messages)
	}
	if got.Messages[2]["role"] != "user" {
		t.Fatalf("user reminder should follow tool result: %#v", got.Messages)
	}
}

func TestNormalizeQoderRequestFillsMissingToolResult(t *testing.T) {
	req := protocol.Request{Messages: []map[string]any{
		{
			"role": "assistant",
			"tool_calls": []any{
				map[string]any{"id": "call_1", "type": "function", "function": map[string]any{"name": "grep", "arguments": `{}`}},
				map[string]any{"id": "call_2", "type": "function", "function": map[string]any{"name": "glob", "arguments": `{}`}},
			},
		},
		{"role": "tool", "tool_call_id": "call_1", "content": "match"},
		{"role": "user", "content": "continue"},
		{"role": "assistant", "content": "next"},
	}}

	got := normalizeQoderRequest(req)
	if len(got.Messages) != 5 {
		t.Fatalf("messages=%#v", got.Messages)
	}
	if got.Messages[1]["role"] != "tool" || got.Messages[1]["tool_call_id"] != "call_1" {
		t.Fatalf("existing tool result changed: %#v", got.Messages)
	}
	missing := got.Messages[2]
	if missing["role"] != "tool" || missing["tool_call_id"] != "call_2" || missing["content"] != "[No response received]" {
		t.Fatalf("missing tool result not repaired: %#v", missing)
	}
	if got.Messages[3]["role"] != "user" {
		t.Fatalf("user message should remain after tool results: %#v", got.Messages)
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
