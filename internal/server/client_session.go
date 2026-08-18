package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

type clientSessionContextKey struct{}
type clientTurnContextKey struct{}

type codexTurnMetadata struct {
	SessionID string
	ThreadID  string
	TurnID    string
}

func clientSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, withClientSessionContext(r))
	})
}

func withClientSessionContext(r *http.Request) *http.Request {
	if r == nil {
		return r
	}
	ctx := r.Context()
	if key := clientSessionKeyFromHeaders(r); key != "" {
		ctx = context.WithValue(ctx, clientSessionContextKey{}, key)
	}
	if key := clientTurnKeyFromHeaders(r); key != "" {
		ctx = context.WithValue(ctx, clientTurnContextKey{}, key)
	}
	if ctx == r.Context() {
		return r
	}
	return r.WithContext(ctx)
}

func clientSessionKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(clientSessionContextKey{}).(string)
	return strings.TrimSpace(key)
}

func clientTurnKeyFromContext(ctx context.Context) string {
	key, _ := ctx.Value(clientTurnContextKey{}).(string)
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
		metadata := parseCodexTurnMetadata(r.Header.Get("X-Codex-Turn-Metadata"))
		// Codex exposes both a session lineage and the CURRENT thread. Subagents
		// have their own thread, while parent-thread metadata points back to the
		// parent. Prefer the current thread so subagents never share Qoder state.
		if threadID := firstSessionHeader(r, "Thread-Id", "Thread_id"); threadID != "" {
			return "codex/thread/" + threadID
		}
		if metadata.ThreadID != "" {
			return "codex/thread/" + metadata.ThreadID
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
		if metadata.SessionID != "" {
			return "codex/session/" + metadata.SessionID
		}
	}
	return ""
}

func clientTurnKeyFromHeaders(r *http.Request) string {
	if r == nil || (r.URL.Path != "/v1/responses" && r.URL.Path != "/v1/chat/completions") {
		return ""
	}
	metadata := parseCodexTurnMetadata(r.Header.Get("X-Codex-Turn-Metadata"))
	if metadata.TurnID == "" {
		return ""
	}
	return "codex/turn/" + metadata.TurnID
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

func parseCodexTurnMetadata(raw string) codexTurnMetadata {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 8<<10 {
		return codexTurnMetadata{}
	}
	var metadata map[string]any
	if json.Unmarshal([]byte(raw), &metadata) != nil {
		return codexTurnMetadata{}
	}
	return codexTurnMetadata{
		SessionID: normalizedMetadataString(metadata, "session_id"),
		ThreadID:  normalizedMetadataString(metadata, "thread_id"),
		TurnID:    normalizedMetadataString(metadata, "turn_id"),
	}
}

func normalizedMetadataString(metadata map[string]any, key string) string {
	value, _ := metadata[key].(string)
	return normalizedSessionValue(value)
}
