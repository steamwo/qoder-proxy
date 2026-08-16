package qoder

import (
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// requestSetIDForRequest maps one downstream user task to one Qoder request set.
// Client-side agent loops append assistant/tool messages after the latest real
// user turn, so hashing only the conversation prefix through that user turn
// keeps request_set_id stable across tool round-trips while a subsequent user
// instruction naturally starts a new request set. Explicit context-window
// selection and image inputs are part of the task identity so different Qoder
// context or multimodal requests cannot collide in one request set.
func requestSetIDForRequest(req protocol.Request, sessionID string) string {
	return stableHash("qoder-request-set", sessionID, req.ModelID, req.ContextWindow, requestSetMessagePrefix(req.Messages), req.ImageURLs)
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
