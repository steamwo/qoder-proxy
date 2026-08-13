package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestResponsesStreamRestoresToolSearchCallForCodex(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "mcp-search-id", DisplayName: "MCP Search", Raw: map[string]any{"key": "mcp-search-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"search_1","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"search_graph\",\"limit\":8}"}}]},"finish_reason":"tool_calls"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"MCP Search",
		"stream":true,
		"input":"Find the code graph tool",
		"tools":[{
			"type":"tool_search",
			"execution":"client",
			"description":"Search deferred MCP tools.",
			"parameters":{"type":"object","properties":{"query":{"type":"string"},"limit":{"type":"number"}},"required":["query"],"additionalProperties":false}
		}]
	}`))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"event: response.output_item.done",
		`"type":"tool_search_call"`,
		`"call_id":"search_1"`,
		`"execution":"client"`,
		`"arguments":{"limit":8,"query":"search_graph"}`,
		"event: response.completed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "event: response.function_call_arguments.done") {
		t.Fatalf("tool_search must not be exposed as a normal function-call argument event:\n%s", body)
	}
}

func TestResponsesStreamRestoresNamespacedMCPFunctionForCodex(t *testing.T) {
	const namespace = "mcp__codebase-memory-mcp"
	const alias = namespace + "__search_graph"
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "mcp-namespace-id", DisplayName: "MCP Namespace", Raw: map[string]any{"key": "mcp-namespace-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_graph","type":"function","function":{"name":"` + alias + `","arguments":"{\"query\":\"symbols\"}"}}]},"finish_reason":"tool_calls"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"MCP Namespace",
		"stream":true,
		"input":"Search the code graph",
		"tools":[{
			"type":"namespace",
			"name":"`+namespace+`",
			"description":"Code graph tools.",
			"tools":[{
				"type":"function",
				"name":"search_graph",
				"description":"Search the code graph.",
				"parameters":{"type":"object","properties":{"query":{"type":"string"}},"required":["query"]}
			}]
		}]
	}`))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"event: response.output_item.done",
		`"type":"function_call"`,
		`"call_id":"call_graph"`,
		`"namespace":"` + namespace + `"`,
		`"name":"search_graph"`,
		`"arguments":"{\"query\":\"symbols\"}"`,
		"event: response.completed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q:\n%s", want, body)
		}
	}
	if len(backend.last.Tools) != 1 {
		t.Fatalf("qoder tools=%#v", backend.last.Tools)
	}
	tool := backend.last.Tools[0].(map[string]any)
	fn := tool["function"].(map[string]any)
	if fn["name"] != alias {
		t.Fatalf("Qoder did not receive flattened MCP alias: %#v", fn)
	}
}
