package openai

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func virtualizationTestModel() qoder.Model {
	return qoder.Model{UpstreamID: "lite", DisplayName: "Lite", Raw: map[string]any{"key": "lite"}}
}

func makeResponseTools(n int) []map[string]any {
	tools := make([]map[string]any, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("mcp_tool_%03d", i)
		if i == 0 {
			name = "read_file"
		} else if i == 1 {
			name = "apply_patch"
		} else if i == 2 {
			name = "shell_command"
		}
		tools = append(tools, map[string]any{
			"type":        "function",
			"name":        name,
			"description": "test tool",
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		})
	}
	return tools
}

func makeNamespacedResponseTools(n int) []map[string]any {
	left := n / 2
	return []map[string]any{
		{
			"type":        "namespace",
			"name":        "workspace",
			"description": "workspace tools",
			"tools":       makeResponseTools(left),
		},
		{
			"type":        "namespace",
			"name":        "services",
			"description": "service tools",
			"tools":       makeResponseTools(n - left),
		},
	}
}

func countDeferredFunctions(tools []map[string]any) int {
	count := 0
	for _, tool := range tools {
		switch asString(tool["type"]) {
		case "function":
			if deferred, _ := tool["defer_loading"].(bool); deferred {
				count++
			}
		case "namespace":
			count += countDeferredFunctions(mapsFromAny(tool["tools"]))
		}
	}
	return count
}

func TestAutoVirtualizeLargeResponsesRegistry(t *testing.T) {
	original := makeResponseTools(270)
	projected := autoVirtualizeResponsesTools(original)
	if len(projected) != len(original)+1 {
		t.Fatalf("projected=%d want %d", len(projected), len(original)+1)
	}
	if got := asString(projected[0]["type"]); got != "tool_search" {
		t.Fatalf("first type=%q", got)
	}
	if _, exists := original[10]["defer_loading"]; exists {
		t.Fatal("original tool registry was mutated")
	}

	visible, routes := normalizeResponsesToolSets(projected, nil)
	if len(visible) > autoToolSearchCoreLimit+1 {
		t.Fatalf("visible tools=%d, expected <=%d", len(visible), autoToolSearchCoreLimit+1)
	}
	if route := routes["tool_search"]; route.Kind != "tool_search" {
		t.Fatalf("tool_search route=%#v", route)
	}
}

func TestAutoVirtualizeLeavesSmallRegistryUntouched(t *testing.T) {
	original := makeResponseTools(12)
	projected := autoVirtualizeResponsesTools(original)
	if len(projected) != len(original) {
		t.Fatalf("projected=%d want %d", len(projected), len(original))
	}
	for _, tool := range projected {
		if asString(tool["type"]) == "tool_search" {
			t.Fatal("unexpected injected tool_search")
		}
	}
}

func TestAutoVirtualizeReusesClientToolSearchAcrossNamespaces(t *testing.T) {
	originalSearch := map[string]any{
		"type":          "tool_search",
		"execution":     "client",
		"description":   "native codex search",
		"defer_loading": true,
		"parameters":    map[string]any{"type": "object", "properties": map[string]any{}},
	}
	original := append([]map[string]any{originalSearch}, makeNamespacedResponseTools(284)...)
	if len(original) >= autoToolSearchThreshold {
		t.Fatalf("test must exercise nested threshold; top-level tools=%d", len(original))
	}
	if got := len(responsesFunctionCandidates(original)); got != 284 {
		t.Fatalf("function leaves=%d want 284", got)
	}

	projected := autoVirtualizeResponsesTools(original)
	if len(projected) != len(original) {
		t.Fatalf("projected=%d want %d", len(projected), len(original))
	}
	if _, exists := original[0]["defer_loading"]; !exists {
		t.Fatal("original native tool_search was mutated")
	}

	searchCount := 0
	for _, tool := range projected {
		if asString(tool["type"]) != "tool_search" {
			continue
		}
		searchCount++
		if got := asString(tool["description"]); got != "native codex search" {
			t.Fatalf("native tool_search was replaced: %q", got)
		}
		if _, deferred := tool["defer_loading"]; deferred {
			t.Fatal("native tool_search remained deferred")
		}
	}
	if searchCount != 1 {
		t.Fatalf("tool_search count=%d want 1", searchCount)
	}
	if deferred := countDeferredFunctions(projected); deferred < 250 {
		t.Fatalf("deferred=%d, expected most nested functions to be deferred", deferred)
	}

	visible, routes := normalizeResponsesToolSets(projected, nil)
	if len(visible) > autoToolSearchCoreLimit+1 {
		t.Fatalf("visible tools=%d, expected <=%d", len(visible), autoToolSearchCoreLimit+1)
	}
	if len(visible) < autoToolSearchCoreFloor+1 {
		t.Fatalf("visible tools=%d, expected search plus at least %d core tools", len(visible), autoToolSearchCoreFloor)
	}
	if route := routes["tool_search"]; route.Kind != "tool_search" {
		t.Fatalf("tool_search route=%#v", route)
	}
}

func TestNormalizeResponsesPromotesToolSearchOutputAfterVirtualization(t *testing.T) {
	tools := makeResponseTools(270)
	discovered := map[string]any{
		"type": "function", "name": "special_database_tool",
		"description": "query the database",
		"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
	}
	input, err := json.Marshal([]any{
		map[string]any{"type": "tool_search_call", "call_id": "search_1", "arguments": map[string]any{"query": "database"}},
		map[string]any{"type": "tool_search_output", "call_id": "search_1", "tools": []any{discovered}},
		map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_text", "text": "use it"}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	preq, err := NormalizeResponses(ResponsesRequest{
		Model: "Lite",
		Input: input,
		Tools: tools,
	}, virtualizationTestModel())
	if err != nil {
		t.Fatal(err)
	}

	foundSearch := false
	foundDiscovered := false
	for _, raw := range preq.Tools {
		tool := raw.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		switch asString(fn["name"]) {
		case "tool_search":
			foundSearch = true
		case "special_database_tool":
			foundDiscovered = true
		}
	}
	if !foundSearch || !foundDiscovered {
		t.Fatalf("tool_search=%v discovered=%v tools=%#v", foundSearch, foundDiscovered, preq.Tools)
	}
}

func TestNormalizeChatKeepsClientToolsUnchanged(t *testing.T) {
	tools := []any{
		map[string]any{"type": "function", "function": map[string]any{"name": "read_file", "parameters": map[string]any{"type": "object"}}},
		map[string]any{"type": "function", "function": map[string]any{"name": "database_lookup", "parameters": map[string]any{"type": "object"}}},
	}
	preq, err := NormalizeChat(ChatRequest{
		Model:    "Lite",
		Messages: []map[string]any{{"role": "user", "content": "hello"}},
		Tools:    tools,
	}, virtualizationTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if len(preq.Tools) != len(tools) {
		t.Fatalf("chat tools=%d want %d", len(preq.Tools), len(tools))
	}
}
