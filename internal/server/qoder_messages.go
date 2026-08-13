package server

import (
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// normalizeQoderRequest keeps Qoder's conversation roles within the
// user/assistant contract. Public adapters may use OpenAI's role=tool
// internally, but Qoder represents tool feedback as a user message carrying a
// tool_result content block.
func normalizeQoderRequest(req protocol.Request) protocol.Request {
	if len(req.Messages) == 0 {
		return req
	}

	changed := false
	messages := make([]map[string]any, 0, len(req.Messages))
	for _, message := range req.Messages {
		role, _ := message["role"].(string)
		if !strings.EqualFold(strings.TrimSpace(role), "tool") {
			messages = append(messages, message)
			continue
		}

		toolID, _ := message["tool_call_id"].(string)
		if strings.TrimSpace(toolID) == "" {
			// Keep malformed input untouched so the upstream error remains
			// diagnostic instead of inventing a tool-call relationship.
			messages = append(messages, message)
			continue
		}
		content := message["content"]
		if content == nil {
			content = ""
		}
		block := map[string]any{
			"type":        "tool_result",
			"tool_use_id": toolID,
			"content":     content,
		}
		if isError, ok := message["is_error"].(bool); ok {
			block["is_error"] = isError
		}
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": []any{block},
		})
		changed = true
	}
	if changed {
		req.Messages = messages
	}
	return req
}
