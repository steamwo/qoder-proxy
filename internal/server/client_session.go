package server

import (
	"context"
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
	switch r.URL.Path {
	case "/v1/messages":
		sessionID := strings.TrimSpace(r.Header.Get("X-Claude-Code-Session-Id"))
		agentID := strings.TrimSpace(r.Header.Get("X-Claude-Code-Agent-Id"))
		if agentID != "" {
			if sessionID != "" {
				return "claude-code/session/" + sessionID + "/agent/" + agentID
			}
			return "claude-code/agent/" + agentID
		}
		if sessionID != "" {
			return "claude-code/session/" + sessionID
		}
	case "/v1/responses":
		// Codex also sends x-codex-parent-thread-id for subagents. Do not use
		// the parent ID here: each current window/thread must stay isolated.
		if windowID := strings.TrimSpace(r.Header.Get("X-Codex-Window-Id")); windowID != "" {
			return "codex/window/" + windowID
		}
	}
	return ""
}
