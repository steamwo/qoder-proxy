package qoder

import (
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// requestSetIDForRequest maps one downstream user/agent turn to one Qoder
// request set. Prefer a trustworthy downstream turn ID when the client exposes
// one (for example Codex x-codex-turn-metadata.turn_id). Otherwise fall back to
// the canonical message prefix through the latest meaningful user message so
// generic OpenAI and Claude clients preserve the previous behavior.
func requestSetIDForRequest(req protocol.Request, sessionID string) string {
	if key := strings.TrimSpace(req.ClientTurnKey); key != "" {
		return stableHash("qoder-client-turn", sessionID, key)
	}
	return stableHash("qoder-request-set", sessionID, req.ModelID, req.ContextWindow, requestSetMessagePrefix(req.Messages))
}

func requestSetMessagePrefix(messages []map[string]any) []map[string]any {
	lastUser := -1
	for i, message := range messages {
		role, _ := message["role"].(string)
		if role == "user" && meaningfulTaskUserContent(message["content"]) {
			lastUser = i
		}
	}
	if lastUser < 0 {
		return messages
	}
	return messages[:lastUser+1]
}

func meaningfulTaskUserContent(content any) bool {
	switch value := content.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(value) != ""
	case []any:
		return len(value) > 0
	case []map[string]any:
		return len(value) > 0
	default:
		return true
	}
}
