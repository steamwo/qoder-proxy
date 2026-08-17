package qoder

import (
	"encoding/json"
	"strings"
)

// addQoderPromptCacheMetadata mirrors the Qoder CLI request shape used for
// prompt-prefix caching. Qoder messages keep their normal OpenAI-compatible
// content field, while a structured contents shadow carries cache_control
// markers. The marker is transport metadata only: downstream protocol adapters
// do not need to know about Qoder's cache representation.
func addQoderPromptCacheMetadata(plaintext []byte) []byte {
	var body map[string]any
	if json.Unmarshal(plaintext, &body) != nil {
		return plaintext
	}
	rawMessages, ok := body["messages"].([]any)
	if !ok || len(rawMessages) == 0 {
		return plaintext
	}

	hasMarker := false
	for _, raw := range rawMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// The official CLI does not attach contents metadata to role=tool
		// messages. Tool-result content remains in the canonical content field.
		if strings.EqualFold(strings.TrimSpace(stringValue(message["role"])), "tool") {
			continue
		}
		contents := qoderMessageContents(message)
		if len(contents) == 0 {
			continue
		}
		message["contents"] = contents
		if qoderContentsHaveCacheMarker(contents) {
			hasMarker = true
		}
	}

	// Qoder CLI places one ephemeral cache breakpoint on the last cacheable
	// content part when the client did not supply one. That makes the entire
	// stable prompt prefix (system/tools/earlier messages) eligible for reuse on
	// the next agent turn instead of billing it as uncached input every time.
	if !hasMarker {
		for i := len(rawMessages) - 1; i >= 0 && !hasMarker; i-- {
			message, ok := rawMessages[i].(map[string]any)
			if !ok || strings.EqualFold(strings.TrimSpace(stringValue(message["role"])), "tool") {
				continue
			}
			contents, ok := message["contents"].([]any)
			if !ok {
				continue
			}
			for j := len(contents) - 1; j >= 0; j-- {
				part, ok := contents[j].(map[string]any)
				if !ok || !qoderCacheableContentPart(part) {
					continue
				}
				part["cache_control"] = map[string]any{"type": "ephemeral"}
				contents[j] = part
				message["contents"] = contents
				hasMarker = true
				break
			}
		}
	}

	encoded, err := json.Marshal(body)
	if err != nil {
		return plaintext
	}
	return encoded
}

func qoderMessageContents(message map[string]any) []any {
	if existing, ok := message["contents"].([]any); ok && len(existing) > 0 {
		return existing
	}

	switch content := message["content"].(type) {
	case string:
		if content == "" {
			return nil
		}
		return []any{map[string]any{"type": "text", "text": content}}
	case []any:
		return qoderContentsFromParts(content)
	case []map[string]any:
		parts := make([]any, 0, len(content))
		for _, part := range content {
			parts = append(parts, part)
		}
		return qoderContentsFromParts(parts)
	default:
		return nil
	}
}

func qoderContentsFromParts(parts []any) []any {
	out := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := strings.ToLower(strings.TrimSpace(stringValue(part["type"])))
		switch typ {
		case "text", "input_text", "output_text":
			text := stringValue(part["text"])
			if text == "" {
				continue
			}
			projected := map[string]any{"type": "text", "text": text}
			copyCacheControl(projected, part)
			out = append(out, projected)
		case "image_url", "input_image", "image":
			url := contentPartImageURL(part)
			if url == "" {
				// Responses-style canonical parts may carry image_url as a string.
				if value, ok := part["image_url"].(string); ok {
					url = strings.TrimSpace(value)
				}
			}
			if url == "" {
				continue
			}
			projected := map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": url},
			}
			copyCacheControl(projected, part)
			out = append(out, projected)
		}
	}
	return out
}

func copyCacheControl(dst, src map[string]any) {
	control, ok := src["cache_control"].(map[string]any)
	if !ok || strings.TrimSpace(stringValue(control["type"])) == "" {
		return
	}
	cloned := make(map[string]any, len(control))
	for key, value := range control {
		cloned[key] = value
	}
	dst["cache_control"] = cloned
}

func qoderContentsHaveCacheMarker(contents []any) bool {
	for _, raw := range contents {
		part, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		control, _ := part["cache_control"].(map[string]any)
		if strings.EqualFold(strings.TrimSpace(stringValue(control["type"])), "ephemeral") {
			return true
		}
	}
	return false
}

func qoderCacheableContentPart(part map[string]any) bool {
	switch strings.ToLower(strings.TrimSpace(stringValue(part["type"]))) {
	case "text", "image_url":
		return true
	default:
		return false
	}
}
