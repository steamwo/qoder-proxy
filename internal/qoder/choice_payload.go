package qoder

// choicePayload selects the incremental payload carried by one OpenAI-style
// choice. Qoder normally streams `delta`, but some CLI/edge responses send a
// complete `message` frame before later deltas. An explicitly present delta map
// always wins, including an empty final delta, so a mirrored full-message
// snapshot cannot be replayed and duplicated at the end of a stream.
func choicePayload(choice map[string]any) (payload map[string]any, incremental bool) {
	if raw, exists := choice["delta"]; exists {
		if delta, ok := raw.(map[string]any); ok {
			return delta, true
		}
		if raw != nil {
			return nil, true
		}
	}
	if message, ok := choice["message"].(map[string]any); ok {
		ensureToolCallIndexes(message)
		return message, false
	}
	return nil, false
}

// toolCallIndex uses the explicit streaming index when present. Complete
// message.tool_calls arrays normally omit that field, so their array position
// becomes the stable index used by downstream assemblers and boundary state.
func toolCallIndex(call map[string]any, position int) int {
	if raw, exists := call["index"]; exists && raw != nil {
		return numberAsInt(raw)
	}
	return position
}

func ensureToolCallIndexes(payload map[string]any) {
	calls, ok := payload["tool_calls"].([]any)
	if !ok {
		return
	}
	for position, rawCall := range calls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := call["index"]; !exists {
			call["index"] = position
		}
	}
}

// normalizeStreamingChoices converts non-stream-shaped Qoder choices into the
// delta form expected by OpenAI streaming clients. Existing delta choices are
// never touched, even when Qoder also mirrors a full message snapshot beside
// them, which prevents duplicate output at stream completion.
func normalizeStreamingChoices(obj map[string]any) bool {
	choices, ok := obj["choices"].([]any)
	if !ok {
		return false
	}
	changed := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			continue
		}
		if _, exists := choice["delta"]; exists {
			continue
		}
		if message, ok := choice["message"].(map[string]any); ok {
			ensureToolCallIndexes(message)
			choice["delta"] = message
			delete(choice, "message")
			changed = true
			continue
		}
		if text, exists := choice["text"]; exists {
			choice["delta"] = map[string]any{"content": text}
			delete(choice, "text")
			changed = true
		}
	}
	return changed
}
