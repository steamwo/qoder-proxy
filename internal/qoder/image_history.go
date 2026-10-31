package qoder

const maxQoderHistoryImages = 50

// trimMessageImages returns the original messages unchanged when the image
// count is within the limit. When trimming is needed, it copies only the
// affected message/content containers and removes the oldest image_url parts
// in conversation order, preserving all other content and message ordering.
func trimMessageImages(messages []map[string]any, limit int) []map[string]any {
	if limit < 0 {
		limit = 0
	}
	total := len(messageImageURLs(messages))
	if total <= limit {
		return messages
	}

	remainingToDrop := total - limit
	trimmed := make([]map[string]any, len(messages))
	copy(trimmed, messages)
	for i, message := range messages {
		if remainingToDrop == 0 {
			break
		}
		content, changed := trimContentImages(message["content"], &remainingToDrop)
		if !changed {
			continue
		}
		cloned := make(map[string]any, len(message))
		for key, value := range message {
			cloned[key] = value
		}
		cloned["content"] = content
		trimmed[i] = cloned
	}
	return trimmed
}

func trimContentImages(content any, remainingToDrop *int) (any, bool) {
	if remainingToDrop == nil || *remainingToDrop <= 0 {
		return content, false
	}

	switch parts := content.(type) {
	case []any:
		out := make([]any, 0, len(parts))
		changed := false
		for _, raw := range parts {
			if *remainingToDrop > 0 {
				if part, ok := raw.(map[string]any); ok && contentPartImageURL(part) != "" {
					*remainingToDrop = *remainingToDrop - 1
					changed = true
					continue
				}
			}
			out = append(out, raw)
		}
		if !changed {
			return content, false
		}
		return out, true

	case []map[string]any:
		out := make([]map[string]any, 0, len(parts))
		changed := false
		for _, part := range parts {
			if *remainingToDrop > 0 && contentPartImageURL(part) != "" {
				*remainingToDrop = *remainingToDrop - 1
				changed = true
				continue
			}
			out = append(out, part)
		}
		if !changed {
			return content, false
		}
		return out, true
	default:
		return content, false
	}
}
