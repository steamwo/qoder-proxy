package openai

import (
	"encoding/json"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func mcpTestModel() qoder.Model {
	return qoder.Model{UpstreamID: "mcp-model", DisplayName: "MCP Model", Raw: map[string]any{"key": "mcp-model"}}
}

func TestNormalizeResponsesExposesToolSearchToQoder(t *testing.T) {
	req := ResponsesRequest{
		Model: "MCP Model",
		Input: json.RawMessage(`"Inspect the code graph"`),
		Tools: []map[string]any{{
			"type": "tool_search",
			"execution": "client",
			"description": "Search deferred MCP tools.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{"query": map[string]any{"type": "string"}},
				"required": []any{"query"},
			},
		}},
	}
	got, err := NormalizeResponses(req, mcpTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools=%#v", got.Tools)
	}
	fn := got.Tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "tool_search" {
		t.Fatalf("qoder tool=%#v", fn)
	}
	route := got.ToolRoutes["tool_search"]
	if route.Kind != "tool_search" || route.Name != "tool_search" {
		t.Fatalf("route=%#v", route)
	}
	if got.ClaudeToolCompatibility {
		t.Fatal("Responses MCP support must not enable Claude-specific compatibility")
	}
}

func TestNormalizeResponsesOmitsDeferredNamespaceUntilToolSearchLoadsIt(t *testing.T) {
	req := ResponsesRequest{
		Model: "MCP Model",
		Input: json.RawMessage(`"Search the code graph"`),
		Tools: []map[string]any{
			{
				"type": "tool_search", "execution": "client", "description": "Search deferred tools.",
				"parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
			},
			{
				"type": "namespace", "name": "mcp__codebase-memory-mcp", "defer_loading": true,
				"description": "Large deferred code graph tool namespace.",
				"tools": []any{map[string]any{
					"type": "function", "name": "search_graph", "description": "Search the graph.",
					"parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
				}},
			},
		},
	}
	got, err := NormalizeResponses(req, mcpTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("deferred namespace leaked into Qoder tools: %#v", got.Tools)
	}
	fn := got.Tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "tool_search" {
		t.Fatalf("expected only tool_search, got %#v", got.Tools)
	}
}

func TestNormalizeResponsesKeepsDeferredToolWithoutToolSearch(t *testing.T) {
	req := ResponsesRequest{
		Model: "MCP Model",
		Input: json.RawMessage(`"Call the tool"`),
		Tools: []map[string]any{{
			"type": "function", "name": "expensive_tool", "defer_loading": true,
			"parameters": map[string]any{"type": "object", "properties": map[string]any{}},
		}},
	}
	got, err := NormalizeResponses(req, mcpTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("deferred tool became unreachable without tool_search: %#v", got.Tools)
	}
}

func TestNormalizeResponsesFlattensMCPNamespaceAndRestoresRoute(t *testing.T) {
	req := ResponsesRequest{
		Model: "MCP Model",
		Input: json.RawMessage(`"Search the code graph"`),
		Tools: []map[string]any{{
			"type": "namespace",
			"name": "mcp__codebase-memory-mcp",
			"description": "Code graph tools.",
			"tools": []any{
				map[string]any{
					"type": "function",
					"name": "search_graph",
					"description": "Search the graph.",
					"parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
				},
			},
		}},
	}
	got, err := NormalizeResponses(req, mcpTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools=%#v", got.Tools)
	}
	fn := got.Tools[0].(map[string]any)["function"].(map[string]any)
	alias, _ := fn["name"].(string)
	if alias == "" || alias == "search_graph" {
		t.Fatalf("namespace was not preserved in qoder alias: %#v", fn)
	}
	route := got.ToolRoutes[alias]
	if route != (protocol.ToolRoute{Kind: "function", Namespace: "mcp__codebase-memory-mcp", Name: "search_graph"}) {
		t.Fatalf("route=%#v", route)
	}

	state := newResponseState(req, got.ToolRoutes)
	tool := state.ensureTool(0, "call_graph", alias)
	tool.Args = `{"query":"symbols"}`
	item := tool.clientItem("completed")
	if item["type"] != "function_call" || item["namespace"] != "mcp__codebase-memory-mcp" || item["name"] != "search_graph" {
		t.Fatalf("restored item=%#v", item)
	}
}

func TestNormalizeResponsesLoadsToolsFromToolSearchOutput(t *testing.T) {
	input := json.RawMessage(`[
		{"type":"tool_search_call","id":"ts_item","call_id":"ts_1","execution":"client","arguments":{"query":"code graph"}},
		{"type":"tool_search_output","call_id":"ts_1","status":"completed","execution":"client","tools":[
			{"type":"namespace","name":"mcp__codebase-memory-mcp","description":"Code graph tools.","defer_loading":true,"tools":[
				{"type":"function","name":"get_architecture","description":"Get architecture","defer_loading":true,"parameters":{"type":"object","properties":{}}}
			]}
		]},
		{"type":"message","role":"user","content":[{"type":"input_text","text":"Use it now"}]}
	]`)
	req := ResponsesRequest{
		Model: "MCP Model",
		Input: input,
		Tools: []map[string]any{{
			"type": "tool_search", "execution": "client", "description": "Search deferred tools.",
			"parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}}},
		}},
	}
	got, err := NormalizeResponses(req, mcpTestModel())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Tools) != 2 {
		t.Fatalf("expected tool_search plus discovered MCP tool, got %#v", got.Tools)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("canonical history=%#v", got.Messages)
	}
	assistant := got.Messages[0]
	calls := assistant["tool_calls"].([]any)
	searchFn := calls[0].(map[string]any)["function"].(map[string]any)
	if searchFn["name"] != "tool_search" || searchFn["arguments"] != `{"query":"code graph"}` {
		t.Fatalf("tool_search history=%#v", searchFn)
	}
	if got.Messages[1]["role"] != "tool" || got.Messages[1]["tool_call_id"] != "ts_1" {
		t.Fatalf("tool_search result history=%#v", got.Messages[1])
	}
	found := false
	for alias, route := range got.ToolRoutes {
		if route.Namespace == "mcp__codebase-memory-mcp" && route.Name == "get_architecture" {
			found = true
			state := newResponseState(req, got.ToolRoutes)
			tool := state.ensureTool(0, "call_arch", alias)
			tool.Args = `{}`
			item := tool.clientItem("completed")
			if item["namespace"] != route.Namespace || item["name"] != route.Name {
				t.Fatalf("restored discovered tool=%#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("discovered route missing: %#v", got.ToolRoutes)
	}
}

func TestResponsesToolSearchCallRestoresClientExecutionShape(t *testing.T) {
	routes := map[string]protocol.ToolRoute{
		"tool_search": {Kind: "tool_search", Name: "tool_search"},
	}
	state := newResponseState(ResponsesRequest{Model: "MCP Model"}, routes)
	tool := state.ensureTool(0, "search_1", "tool_search")
	tool.Args = `{"query":"search_graph","limit":8}`
	item := tool.clientItem("completed")
	if item["type"] != "tool_search_call" || item["execution"] != "client" || item["call_id"] != "search_1" {
		t.Fatalf("tool_search item=%#v", item)
	}
	args, ok := item["arguments"].(map[string]any)
	if !ok || args["query"] != "search_graph" {
		t.Fatalf("tool_search arguments=%#v", item["arguments"])
	}
}
