package server

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// normalizeQoderRequest keeps the adapter-produced OpenAI tool protocol intact.
// Qoder's agent chat endpoint accepts assistant.tool_calls followed by role=tool
// messages. This pass repairs malformed history for every compatible adapter.
// Claude Code's extra tool-namespace reminder is opt-in and must never leak into
// the OpenAI protocol adapters.
func normalizeQoderRequest(req protocol.Request) protocol.Request {
	if req.SourceProtocol == "" && req.ClaudeToolCompatibility {
		// The production /v1/messages compatibility path predates SourceProtocol.
		// Infer it here so core Qoder cost/cache diagnostics remain protocol-aware
		// without coupling the Qoder client to a particular downstream handler.
		req.SourceProtocol = "anthropic"
	}
	if len(req.Messages) > 0 {
		messagesBefore := len(req.Messages)
		messages, stats := canonicalizeQoderToolHistory(req.Messages)
		if stats.missingToolResults > 0 || stats.reorderedToolResults > 0 {
			req.Messages = messages
			slog.Warn("qoder tool history repaired",
				"missing_tool_results", stats.missingToolResults,
				"reordered_tool_results", stats.reorderedToolResults,
				"tool_calls", stats.toolCalls,
				"tool_results", stats.toolResults,
				"messages_before", messagesBefore,
				"messages_after", len(messages),
				"tail_roles", tailMessageRoles(messages, 8),
			)
		}
	}

	if req.ClaudeToolCompatibility {
		toolNames := qoderToolNames(req.Tools)
		if len(toolNames) > 0 {
			req.System = appendQoderToolConstraint(req.System, toolNames)
			slog.Info("qoder tool availability",
				"tools", len(toolNames),
				"bash_available", containsString(toolNames, "Bash"),
				"tool_search_available", containsString(toolNames, "ToolSearch"),
			)
			slog.Debug("qoder advertised tools", "tool_names", toolNames)
		}
	}
	return req
}

func qoderToolNames(tools []any) []string {
	seen := make(map[string]struct{}, len(tools))
	names := make([]string, 0, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		name := strings.TrimSpace(stringValue(fn["name"]))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func appendQoderToolConstraint(system string, toolNames []string) string {
	if len(toolNames) == 0 {
		return system
	}
	constraint := "[qoder-proxy tool availability]\nOnly call tools present in the current request's tool schemas. If a desired tool is not present and ToolSearch is available, call ToolSearch first to load it. Do not invent or use Qoder-only tools."
	if strings.TrimSpace(system) == "" {
		return constraint
	}
	return system + "\n\n" + constraint
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type toolHistoryStats struct {
	toolCalls            int
	toolResults          int
	missingToolResults   int
	reorderedToolResults int
}

// canonicalizeQoderToolHistory enforces the OpenAI tool-call ordering contract:
// every tool response for an assistant tool_calls turn must appear immediately
// after that assistant turn, before any user text or later assistant turn. If a
// compacted transcript dropped a response entirely, synthesize the same neutral
// placeholder used by mature Claude/OpenAI compatibility bridges.
func canonicalizeQoderToolHistory(input []map[string]any) ([]map[string]any, toolHistoryStats) {
	stats := toolHistoryStats{}
	for _, message := range input {
		if messageRole(message) == "tool" {
			stats.toolResults++
		}
	}

	out := make([]map[string]any, 0, len(input))
	for i := 0; i < len(input); {
		message := input[i]
		ids := assistantToolCallIDs(message)
		if messageRole(message) != "assistant" || len(ids) == 0 {
			out = append(out, message)
			i++
			continue
		}

		stats.toolCalls += len(ids)
		out = append(out, message)

		expected := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if id != "" {
				expected[id] = struct{}{}
			}
		}
		responded := make(map[string]struct{}, len(expected))
		matched := make([]map[string]any, 0, len(expected))
		deferred := make([]map[string]any, 0)

		j := i + 1
		for ; j < len(input); j++ {
			next := input[j]
			if messageRole(next) == "assistant" {
				break
			}
			if messageRole(next) == "tool" {
				toolID := strings.TrimSpace(stringValue(next["tool_call_id"]))
				if _, wanted := expected[toolID]; wanted {
					if _, duplicate := responded[toolID]; !duplicate {
						if len(deferred) > 0 {
							stats.reorderedToolResults++
						}
						responded[toolID] = struct{}{}
						matched = append(matched, next)
						continue
					}
				}
			}
			deferred = append(deferred, next)
		}

		out = append(out, matched...)
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := responded[id]; ok {
				continue
			}
			out = append(out, map[string]any{
				"role":         "tool",
				"tool_call_id": id,
				"content":      "[No response received]",
			})
			responded[id] = struct{}{}
			stats.missingToolResults++
		}
		out = append(out, deferred...)
		i = j
	}

	return out, stats
}

func assistantToolCallIDs(message map[string]any) []string {
	if messageRole(message) != "assistant" {
		return nil
	}
	calls, ok := message["tool_calls"].([]any)
	if !ok || len(calls) == 0 {
		return nil
	}
	ids := make([]string, 0, len(calls))
	seen := make(map[string]struct{}, len(calls))
	for _, raw := range calls {
		call, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(stringValue(call["id"]))
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func messageRole(message map[string]any) string {
	return strings.ToLower(strings.TrimSpace(stringValue(message["role"])))
}

func stringValue(value any) string {
	s, _ := value.(string)
	return s
}

func tailMessageRoles(messages []map[string]any, max int) []string {
	if max <= 0 || len(messages) == 0 {
		return nil
	}
	start := len(messages) - max
	if start < 0 {
		start = 0
	}
	roles := make([]string, 0, len(messages)-start)
	for _, message := range messages[start:] {
		roles = append(roles, messageRole(message))
	}
	return roles
}
