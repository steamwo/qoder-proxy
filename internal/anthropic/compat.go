package anthropic

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const maxMessageRequestBytes = 4 << 20

// HandleMessagesCompatible accepts canonical Anthropic Messages payloads and a
// few OpenAI-style role variants emitted by Claude-compatible IDEs/gateways.
// Canonical user/assistant content is then normalized through the Anthropic-
// native Qoder path so tool_use/tool_result blocks are not round-tripped through
// OpenAI tool_calls/role=tool history.
func HandleMessagesCompatible(w http.ResponseWriter, r *http.Request, backend Backend) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxMessageRequestBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "read request body: "+err.Error())
		return
	}
	if len(body) > maxMessageRequestBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "invalid_request_error", "request body exceeds 4 MiB")
		return
	}

	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		restoreMessageBody(r, body)
		handleMessagesNative(w, r, backend)
		return
	}

	rawMessages, ok := payload["messages"].([]any)
	if !ok {
		restoreMessageBody(r, body)
		handleMessagesNative(w, r, backend)
		return
	}

	systemParts := make([]string, 0, 3)
	if existing := payload["system"]; existing != nil {
		text, err := textContent(existing, false)
		if err != nil {
			restoreMessageBody(r, body)
			handleMessagesNative(w, r, backend)
			return
		}
		if strings.TrimSpace(text) != "" {
			systemParts = append(systemParts, text)
		}
	}

	changed := false
	normalized := make([]any, 0, len(rawMessages))
	for i, raw := range rawMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			normalized = append(normalized, raw)
			continue
		}
		role := strings.ToLower(strings.TrimSpace(stringValue(message["role"])))
		switch role {
		case "user", "assistant":
			normalized = append(normalized, message)
		case "system", "developer":
			text, err := textContent(message["content"], false)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("messages[%d]: %s content: %v", i, role, err))
				return
			}
			if strings.TrimSpace(text) != "" {
				systemParts = append(systemParts, text)
			}
			changed = true
		case "tool":
			toolID := strings.TrimSpace(stringValue(message["tool_call_id"]))
			if toolID == "" {
				toolID = strings.TrimSpace(stringValue(message["tool_use_id"]))
			}
			if toolID == "" {
				writeError(w, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("messages[%d].tool_call_id is required for role tool", i))
				return
			}
			content := message["content"]
			if content == nil {
				content = ""
			}
			result := map[string]any{
				"type":        "tool_result",
				"tool_use_id": toolID,
				"content":     content,
			}
			if isError, ok := message["is_error"].(bool); ok {
				result["is_error"] = isError
			}
			normalized = append(normalized, map[string]any{
				"role":    "user",
				"content": []any{result},
			})
			changed = true
		default:
			// Preserve unknown roles so the strict native adapter returns the
			// normal Anthropic validation error instead of changing semantics.
			normalized = append(normalized, message)
		}
	}

	if !changed {
		restoreMessageBody(r, body)
		handleMessagesNative(w, r, backend)
		return
	}

	payload["messages"] = normalized
	if len(systemParts) > 0 {
		payload["system"] = strings.Join(systemParts, "\n\n")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "normalize request: "+err.Error())
		return
	}
	restoreMessageBody(r, encoded)
	handleMessagesNative(w, r, backend)
}

func restoreMessageBody(r *http.Request, body []byte) {
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
}
