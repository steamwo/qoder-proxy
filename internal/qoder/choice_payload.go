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
		return message, false
	}
	return nil, false
}
