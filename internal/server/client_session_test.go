package server

import (
	"net/http/httptest"
	"testing"
)

func TestClaudeClientSessionUsesCurrentAgentID(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Claude-Code-Session-Id", "session-main")
	r.Header.Set("X-Claude-Code-Agent-Id", "agent-child")
	r.Header.Set("X-Claude-Code-Parent-Agent-Id", "agent-parent")

	got := clientSessionKeyFromHeaders(r)
	want := "claude-code/session/session-main/agent/agent-child"
	if got != want {
		t.Fatalf("session key=%q want=%q", got, want)
	}
	if got == "claude-code/session/session-main/agent/agent-parent" {
		t.Fatal("parent agent id must never become the current Qoder session")
	}
}

func TestClaudeMainSessionDoesNotInventAgent(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/messages", nil)
	r.Header.Set("X-Claude-Code-Session-Id", "session-main")
	if got, want := clientSessionKeyFromHeaders(r), "claude-code/session/session-main"; got != want {
		t.Fatalf("session key=%q want=%q", got, want)
	}
}

func TestCodexClientSessionUsesCurrentWindowNotParentThread(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/responses", nil)
	r.Header.Set("X-Codex-Window-Id", "child-thread:0")
	r.Header.Set("X-Codex-Parent-Thread-Id", "parent-thread")

	got := clientSessionKeyFromHeaders(r)
	want := "codex/window/child-thread:0"
	if got != want {
		t.Fatalf("session key=%q want=%q", got, want)
	}
}

func TestUnknownOpenAIClientHasNoSyntheticStableSession(t *testing.T) {
	r := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	r.Header.Set("X-Codex-Parent-Thread-Id", "parent-thread")
	if got := clientSessionKeyFromHeaders(r); got != "" {
		t.Fatalf("unexpected generic OpenAI session key %q", got)
	}
}
