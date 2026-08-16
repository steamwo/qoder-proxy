package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestClaudeCodeDeferredMCPToolsStayOutOfQoderContext(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "claude-mcp", DisplayName: "Claude MCP", Raw: map[string]any{"key": "claude-mcp"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Claude MCP",
		"max_tokens":256,
		"tools":[
			{"name":"ToolSearch","description":"Load deferred tools","input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}},
			{"name":"mcp__codebase-memory-mcp__search_graph","description":"Search the code graph","defer_loading":true,"input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}
		],
		"messages":[{"role":"user","content":"Search the code graph"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !backend.last.ClaudeToolCompatibility {
		t.Fatal("Claude MCP requests must retain Claude tool compatibility")
	}
	if len(backend.last.Tools) != 1 {
		t.Fatalf("deferred tool leaked into Qoder context: %#v", backend.last.Tools)
	}
	tool, ok := backend.last.Tools[0].(map[string]any)
	if !ok {
		t.Fatalf("tool=%#v", backend.last.Tools[0])
	}
	fn, ok := tool["function"].(map[string]any)
	if !ok || fn["name"] != "ToolSearch" {
		t.Fatalf("expected only ToolSearch, got %#v", backend.last.Tools)
	}
}

func TestClaudeCodeDeferredToolFallsBackWithoutToolSearch(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "claude-mcp-fallback", DisplayName: "Claude MCP Fallback", Raw: map[string]any{"key": "claude-mcp-fallback"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Claude MCP Fallback",
		"max_tokens":256,
		"tools":[
			{"name":"mcp__codebase-memory-mcp__search_graph","description":"Search the code graph","defer_loading":true,"input_schema":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}}
		],
		"messages":[{"role":"user","content":"Search the code graph"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(backend.last.Tools) != 1 {
		t.Fatalf("deferred tool became unreachable without ToolSearch: %#v", backend.last.Tools)
	}
	fn := backend.last.Tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "mcp__codebase-memory-mcp__search_graph" {
		t.Fatalf("unexpected fallback tool=%#v", fn)
	}
}

func TestClaudeCodeLoadedMCPToolCallStreamsWithOriginalName(t *testing.T) {
	const toolName = "mcp__codebase-memory-mcp__get_architecture"
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "claude-mcp-stream", DisplayName: "Claude MCP Stream", Raw: map[string]any{"key": "claude-mcp-stream"}},
		body: qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_mcp_1","type":"function","function":{"name":"` + toolName + `","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Claude MCP Stream",
		"max_tokens":256,
		"stream":true,
		"tools":[{"name":"`+toolName+`","description":"Get architecture","input_schema":{"type":"object","properties":{}}}],
		"messages":[{"role":"user","content":"Inspect architecture"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"type":"tool_use"`) || !strings.Contains(body, `"name":"`+toolName+`"`) {
		t.Fatalf("MCP tool_use name was not preserved:\n%s", body)
	}
}
