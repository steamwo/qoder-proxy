package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestClaudeCodeMCPToolsRemainCallableFunctions(t *testing.T) {
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
	if len(backend.last.Tools) != 2 {
		t.Fatalf("tools=%#v", backend.last.Tools)
	}
	want := map[string]bool{
		"ToolSearch": true,
		"mcp__codebase-memory-mcp__search_graph": true,
	}
	for _, raw := range backend.last.Tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("tool=%#v", raw)
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			t.Fatalf("function=%#v", tool)
		}
		name, _ := fn["name"].(string)
		if !want[name] {
			t.Fatalf("unexpected or rewritten Claude MCP tool name %q", name)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("missing Claude MCP tools: %#v", want)
	}
}

func TestClaudeCodeMCPToolCallStreamsWithOriginalName(t *testing.T) {
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
