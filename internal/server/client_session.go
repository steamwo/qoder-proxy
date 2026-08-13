package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type clientSessionContextKey struct{}

func clientSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, withClientSessionContext(r))
	})
}

func withClientSessionContext(r *http.Request) *http.Request {
	key := clientSessionKeyFromHeaders(r)
	if key == "" {
		return r
	}
	return r.WithContext(context.WithValue(r.Context(), clientSessionContextKey{}, key))
}

func clientSessionKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(clientSessionContextKey{}).(string)
	return strings.TrimSpace(key)
}

func clientSessionKeyFromHeaders(r *http.Request) string {
	if r == nil {
		return ""
	}

	if r.URL.Path == "/v1/messages" {
		sessionID := firstSessionHeader(r,
			"X-Claude-Code-Session-Id",
			"Session-Id",
			"Session_id",
			"X-Session-ID",
		)
		agentID := normalizedSessionValue(r.Header.Get("X-Claude-Code-Agent-Id"))
		if agentID != "" {
			// Claude Code subagents share the parent Claude session lineage but
			// carry their own current agent ID. Parent-agent is relationship data
			// only and must never become the current Qoder session identity.
			if sessionID != "" {
				return "claude-code/session/" + sessionID + "/agent/" + agentID
			}
			return "claude-code/agent/" + agentID
		}
		if sessionID != "" {
			return "claude-code/session/" + sessionID
		}
		return ""
	}

	if r.URL.Path == "/v1/responses" || r.URL.Path == "/v1/chat/completions" {
		// Codex exposes both a session and the CURRENT thread. Subagents have
		// their own thread, while x-codex-parent-thread-id points back to the
		// parent. Prefer the current thread so subagents never share Qoder state.
		if threadID := firstSessionHeader(r, "Thread-Id", "Thread_id"); threadID != "" {
			return "codex/thread/" + threadID
		}
		if windowID := normalizedSessionValue(r.Header.Get("X-Codex-Window-Id")); windowID != "" {
			return "codex/window/" + windowID
		}
		if sessionID := firstSessionHeader(r, "Session-Id", "Session_id"); sessionID != "" {
			return "codex/session/" + sessionID
		}
		if sessionID := normalizedSessionValue(r.Header.Get("X-Session-ID")); sessionID != "" {
			return "openai/session/" + sessionID
		}
		if sessionID := codexTurnMetadataSessionID(r.Header.Get("X-Codex-Turn-Metadata")); sessionID != "" {
			return "codex/session/" + sessionID
		}
	}
	return ""
}

func firstSessionHeader(r *http.Request, names ...string) string {
	for _, name := range names {
		if value := normalizedSessionValue(r.Header.Get(name)); value != "" {
			return value
		}
	}
	return ""
}

func normalizedSessionValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 {
		return ""
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return value
}

func codexTurnMetadataSessionID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 8<<10 {
		return ""
	}
	var metadata map[string]any
	if json.Unmarshal([]byte(raw), &metadata) != nil {
		return ""
	}
	value, _ := metadata["session_id"].(string)
	return normalizedSessionValue(value)
}
