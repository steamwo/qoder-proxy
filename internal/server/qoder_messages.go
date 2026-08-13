package server

import (
	"encoding/json"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// normalizeQoderRequest converts adapter-internal OpenAI-style tool history to
// Qoder's user/assistant transcript shape. Qoder represents assistant tool calls
// as tool_use content blocks and tool feedback as tool_result blocks inside a
// user message; it does not accept role=tool in conversation history.
func normalizeQoderRequest(req protocol.Request) protocol.Request {
	if len(req.Messages) == 0 {
		return req
	}

	changed := false
	messages := make([]map[string]any, 0, len(req.Messages))
	pendingToolResults := make([]any, 0)

	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		blocks := append([]any(nil), pendingToolResults...)
		messages = append(messages, map[string]any{"role": "user", "content": blocks})
		pendingToolResults = pendingToolResults[:0]
		changed = true
	}

	for _, message := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(stringField(message, "role")))
		switch role {
		case "tool":
			toolID := strings.TrimSpace(stringField(message, "tool_call_id"))
			if toolID == "" {
				flushToolResults()
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
			pendingToolResults = append(pendingToolResults, block)
			changed = true

		case "user":
			if len(pendingToolResults) == 0 {
				messages = append(messages, message)
				continue
			}
			blocks := append([]any(nil), pendingToolResults...)
			blocks = appendUserContentBlocks(blocks, message["content"])
			merged := cloneMessage(message)
			merged["role"] = "user"
			merged["content"] = blocks
			messages = append(messages, merged)
			pendingToolResults = pendingToolResults[:0]
			changed = true

		case "assistant":
			flushToolResults()
			normalized, didChange := normalizeAssistantToolCalls(message)
			messages = append(messages, normalized)
			changed = changed || didChange

		default:
			flushToolResults()
			messages = append(messages, message)
		}
	}
	flushToolResults()

	if changed {
		req.Messages = messages
	}
	return req
}

func normalizeAssistantToolCalls(message map[string]any) (map[string]any, bool) {
	calls, ok := message["tool_calls"].([]any)
	if !ok || len(calls) == 0 {
		return message, false
	}

	blocks := make([]any, 0, len(calls)+1)
	blocks = appendAssistantContentBlocks(blocks, message["content"])
	converted := 0
	for _, raw := range calls {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := call["function"].(map[string]any)
		id := strings.TrimSpace(stringField(call, "id"))
		name := strings.TrimSpace(stringField(fn, "name"))
		if id == "" || name == "" {
			continue
		}
		input := any(map[string]any{})
		if args := strings.TrimSpace(stringField(fn, "arguments")); args != "" {
			var parsed any
			if json.Unmarshal([]byte(args), &parsed) == nil && parsed != nil {
				input = parsed
			}
		}
		blocks = append(blocks, map[string]any{
			"type":  "tool_use",
			"id":    id,
			"name":  name,
			"input": input,
		})
		converted++
	}
	if converted == 0 {
		return message, false
	}

	normalized := cloneMessage(message)
	delete(normalized, "tool_calls")
	normalized["role"] = "assistant"
	normalized["content"] = blocks
	return normalized, true
}

func appendAssistantContentBlocks(blocks []any, content any) []any {
	switch value := content.(type) {
	case string:
		if value != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": value})
		}
	case []any:
		blocks = append(blocks, value...)
	}
	return blocks
}

func appendUserContentBlocks(blocks []any, content any) []any {
	switch value := content.(type) {
	case string:
		if value != "" {
			blocks = append(blocks, map[string]any{"type": "text", "text": value})
		}
	case []any:
		blocks = append(blocks, value...)
	case nil:
	default:
		blocks = append(blocks, map[string]any{"type": "text", "text": value})
	}
	return blocks
}

func cloneMessage(message map[string]any) map[string]any {
	cloned := make(map[string]any, len(message))
	for key, value := range message {
		cloned[key] = value
	}
	return cloned
}

func stringField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	value, _ := m[key].(string)
	return value
}
