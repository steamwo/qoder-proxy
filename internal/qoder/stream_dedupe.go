package qoder

import (
	"encoding/json"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// streamOutputState reconciles complete-message snapshots with incremental
// deltas. Qoder can mix both shapes on one SSE stream; complete snapshots must
// contribute only content that has not already been emitted.
type streamOutputState struct {
	text     string
	toolArgs map[int]string
}

func newStreamOutputState() *streamOutputState {
	return &streamOutputState{toolArgs: make(map[int]string)}
}

func (s *streamOutputState) filter(ev protocol.Event) (protocol.Event, bool) {
	snapshot := qoderRawIsSnapshot(ev.Raw)
	switch ev.Kind {
	case protocol.EventTextDelta:
		if ev.Text == "" {
			return ev, false
		}
		if snapshot {
			ev.Text = unseenSnapshotSuffix(s.text, ev.Text)
			if ev.Text == "" {
				return ev, false
			}
		}
		s.text += ev.Text
		return ev, true

	case protocol.EventToolDelta:
		if snapshot && ev.ToolArguments != "" {
			ev.ToolArguments = unseenSnapshotSuffix(s.toolArgs[ev.ToolIndex], ev.ToolArguments)
		}
		if ev.ToolArguments != "" {
			s.toolArgs[ev.ToolIndex] += ev.ToolArguments
		}
		// Keep identity/name-only fragments because downstream assemblers use
		// them to initialize a tool call even when the argument suffix is empty.
		return ev, ev.ToolID != "" || ev.ToolName != "" || ev.ToolArguments != ""
	default:
		return ev, true
	}
}

// normalizeOpenAIChunk performs the same snapshot reconciliation before a Qoder
// inner chunk is exposed through the OpenAI streaming API, then converts a
// full-message choice to delta form.
func (s *streamOutputState) normalizeOpenAIChunk(obj map[string]any) bool {
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
		if delta, ok := choice["delta"].(map[string]any); ok {
			if text := contentText(delta["content"]); text != "" {
				s.text += text
			}
			s.recordToolPayload(delta, false)
			continue
		}
		if message, ok := choice["message"].(map[string]any); ok {
			ensureToolCallIndexes(message)
			if text := contentText(message["content"]); text != "" {
				suffix := unseenSnapshotSuffix(s.text, text)
				if suffix != text {
					message["content"] = suffix
					changed = true
				}
				if suffix != "" {
					s.text += suffix
				}
			}
			if s.recordToolPayload(message, true) {
				changed = true
			}
			continue
		}
		if text := contentText(choice["text"]); text != "" {
			suffix := unseenSnapshotSuffix(s.text, text)
			if suffix != text {
				choice["text"] = suffix
				changed = true
			}
			if suffix != "" {
				s.text += suffix
			}
		}
	}
	if normalizeStreamingChoices(obj) {
		changed = true
	}
	return changed
}

func (s *streamOutputState) recordToolPayload(payload map[string]any, snapshot bool) bool {
	calls, ok := payload["tool_calls"].([]any)
	if !ok {
		return false
	}
	changed := false
	for position, rawCall := range calls {
		call, ok := rawCall.(map[string]any)
		if !ok {
			continue
		}
		idx := toolCallIndex(call, position)
		fn, _ := call["function"].(map[string]any)
		args := stringValue(fn["arguments"])
		if args == "" {
			continue
		}
		if snapshot {
			suffix := unseenSnapshotSuffix(s.toolArgs[idx], args)
			if suffix != args {
				fn["arguments"] = suffix
				changed = true
			}
			if suffix != "" {
				s.toolArgs[idx] += suffix
			}
			continue
		}
		s.toolArgs[idx] += args
	}
	return changed
}

func unseenSnapshotSuffix(emitted, snapshot string) string {
	if snapshot == "" {
		return ""
	}
	if emitted == "" {
		return snapshot
	}
	if strings.HasPrefix(snapshot, emitted) {
		return snapshot[len(emitted):]
	}
	if strings.HasPrefix(emitted, snapshot) {
		return ""
	}
	// If a snapshot starts in the middle of the last emitted chunk, emit only
	// the non-overlapping suffix. This handles edge chunking without assuming
	// rune boundaries or rewriting content already delivered to the client.
	max := len(emitted)
	if len(snapshot) < max {
		max = len(snapshot)
	}
	for n := max; n > 0; n-- {
		if strings.HasSuffix(emitted, snapshot[:n]) {
			return snapshot[n:]
		}
	}
	return snapshot
}

func qoderRawIsSnapshot(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var obj map[string]any
	if json.Unmarshal(raw, &obj) != nil {
		return false
	}
	return qoderObjectIsSnapshot(obj)
}

func qoderObjectIsSnapshot(obj map[string]any) bool {
	if choices, ok := obj["choices"].([]any); ok {
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}
			if _, exists := choice["delta"]; exists {
				return false
			}
			if _, exists := choice["message"]; exists {
				return true
			}
			if _, exists := choice["text"]; exists {
				return true
			}
		}
	}
	for _, key := range []string{"llm_model_result", "data", "result", "payload", "body"} {
		value, exists := obj[key]
		if !exists || value == nil {
			continue
		}
		switch nested := value.(type) {
		case string:
			var child map[string]any
			if json.Unmarshal([]byte(nested), &child) == nil && qoderObjectIsSnapshot(child) {
				return true
			}
		case map[string]any:
			if qoderObjectIsSnapshot(nested) {
				return true
			}
		}
	}
	return false
}
