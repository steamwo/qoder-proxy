package anthropic

import (
	"fmt"
	"strings"
)

// normalizeAnthropicTools maps Anthropic tool schemas to the OpenAI-shaped
// functions Qoder's agent endpoint expects while honoring Claude's deferred-tool
// contract. When the client advertises ToolSearch, tools marked defer_loading
// are intentionally omitted from the model-visible Qoder tool list until the
// client loads them on a later turn. Without ToolSearch we keep deferred tools
// visible as a safe compatibility fallback rather than making them unreachable.
func normalizeAnthropicTools(input []map[string]any) ([]any, int, error) {
	hasToolSearch := false
	for _, tool := range input {
		if isAnthropicToolSearch(tool) && !toolDeferLoading(tool) {
			hasToolSearch = true
			break
		}
	}

	tools := make([]any, 0, len(input))
	deferredOmitted := 0
	for i, tool := range input {
		name := strings.TrimSpace(stringValue(tool["name"]))
		if name == "" {
			return nil, deferredOmitted, fmt.Errorf("tools[%d].name is required", i)
		}
		if hasToolSearch && !isAnthropicToolSearch(tool) && toolDeferLoading(tool) {
			deferredOmitted++
			continue
		}
		schema := tool["input_schema"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		fn := map[string]any{"name": name, "parameters": schema}
		if desc := stringValue(tool["description"]); desc != "" {
			fn["description"] = desc
		}
		tools = append(tools, map[string]any{"type": "function", "function": fn})
	}
	return tools, deferredOmitted, nil
}

func isAnthropicToolSearch(tool map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(stringValue(tool["name"])), "ToolSearch")
}

func toolDeferLoading(tool map[string]any) bool {
	value, _ := tool["defer_loading"].(bool)
	return value
}
