package qoder

import (
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestRequestSetIDStableAcrossToolRoundTrip(t *testing.T) {
	sessionID := "session-1"
	initial := protocol.Request{
		ModelID: "model-id",
		Messages: []map[string]any{
			{"role": "user", "content": "inspect the failing test"},
		},
	}
	continued := protocol.Request{
		ModelID: "model-id",
		Messages: []map[string]any{
			{"role": "user", "content": "inspect the failing test"},
			{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "Bash", "arguments": `{"command":"go test ./..."}`}}}},
			{"role": "tool", "tool_call_id": "call-1", "content": "FAIL"},
		},
	}

	first := requestSetIDForRequest(initial, sessionID)
	second := requestSetIDForRequest(continued, sessionID)
	if first != second {
		t.Fatalf("tool round-trip changed request_set_id: %s != %s", first, second)
	}
}

func TestRequestSetIDChangesForNextUserTask(t *testing.T) {
	sessionID := "session-1"
	firstTask := protocol.Request{
		ModelID: "model-id",
		Messages: []map[string]any{
			{"role": "user", "content": "inspect the failing test"},
			{"role": "assistant", "content": "fixed"},
		},
	}
	secondTask := protocol.Request{
		ModelID: "model-id",
		Messages: []map[string]any{
			{"role": "user", "content": "inspect the failing test"},
			{"role": "assistant", "content": "fixed"},
			{"role": "user", "content": "now update the documentation"},
		},
	}

	first := requestSetIDForRequest(firstTask, sessionID)
	second := requestSetIDForRequest(secondTask, sessionID)
	if first == second {
		t.Fatalf("new user task reused request_set_id %s", first)
	}
}

func TestRequestSetIDSeparatesRepeatedPromptInSameSession(t *testing.T) {
	sessionID := "session-1"
	firstTask := protocol.Request{
		ModelID: "model-id",
		Messages: []map[string]any{{"role": "user", "content": "run the tests"}},
	}
	secondTask := protocol.Request{
		ModelID: "model-id",
		Messages: []map[string]any{
			{"role": "user", "content": "run the tests"},
			{"role": "assistant", "content": "all green"},
			{"role": "user", "content": "run the tests"},
		},
	}

	first := requestSetIDForRequest(firstTask, sessionID)
	second := requestSetIDForRequest(secondTask, sessionID)
	if first == second {
		t.Fatalf("repeated later prompt reused request_set_id %s", first)
	}
}

func TestRequestSetIDChangesWithExplicitContextWindow(t *testing.T) {
	sessionID := "session-1"
	base := protocol.Request{
		ModelID:  "model-id",
		Messages: []map[string]any{{"role": "user", "content": "inspect the repository"}},
	}
	explicit := base
	explicit.ContextWindow = 1000000

	if requestSetIDForRequest(base, sessionID) == requestSetIDForRequest(explicit, sessionID) {
		t.Fatal("different context-window policies reused request_set_id")
	}
}

func TestRequestSetIDUsesDownstreamTurnAcrossToolHistoryAndModelSwitch(t *testing.T) {
	sessionID := "session-1"
	initial := protocol.Request{
		ModelID:       "lite",
		ClientTurnKey: "codex/turn/turn-42",
		Messages:      []map[string]any{{"role": "user", "content": "fix it"}},
	}
	continued := protocol.Request{
		ModelID:       "pro",
		ContextWindow: 1000000,
		ClientTurnKey: "codex/turn/turn-42",
		Messages: []map[string]any{
			{"role": "user", "content": "fix it"},
			{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call-1", "type": "function", "function": map[string]any{"name": "Bash", "arguments": `{}`}}}},
			{"role": "tool", "tool_call_id": "call-1", "content": "done"},
		},
	}
	if first, second := requestSetIDForRequest(initial, sessionID), requestSetIDForRequest(continued, sessionID); first != second {
		t.Fatalf("same downstream turn changed request_set_id: %s != %s", first, second)
	}
}

func TestRequestSetIDChangesForDistinctDownstreamTurns(t *testing.T) {
	sessionID := "session-1"
	first := protocol.Request{ClientTurnKey: "codex/turn/turn-1"}
	second := protocol.Request{ClientTurnKey: "codex/turn/turn-2"}
	if requestSetIDForRequest(first, sessionID) == requestSetIDForRequest(second, sessionID) {
		t.Fatal("distinct downstream turns reused request_set_id")
	}
}

func TestRequestSetPrefixIgnoresEmptySyntheticUserMessage(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "task"},
		{"role": "assistant", "content": ""},
		{"role": "user", "content": "   "},
		{"role": "tool", "tool_call_id": "call-1", "content": "result"},
	}
	prefix := requestSetMessagePrefix(messages)
	if len(prefix) != 1 {
		t.Fatalf("prefix length=%d, want 1", len(prefix))
	}
}
