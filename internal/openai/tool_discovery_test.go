package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type discoverySequenceBackend struct {
	model    qoder.Model
	bodies   []string
	requests []protocol.Request
}

func (b *discoverySequenceBackend) ResolveModel(_ context.Context, public string) (qoder.Model, error) {
	if public != b.model.DisplayName {
		return qoder.Model{}, fmt.Errorf("not found")
	}
	return b.model, nil
}

func (b *discoverySequenceBackend) Chat(_ context.Context, req protocol.Request) (*http.Response, error) {
	b.requests = append(b.requests, req)
	index := len(b.requests) - 1
	if index >= len(b.bodies) {
		return nil, fmt.Errorf("unexpected backend call %d", index+1)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(b.bodies[index])),
	}, nil
}

func namespacedToolsWithHiddenDatabase(n int) []map[string]any {
	tools := makeNamespacedResponseTools(n)
	services := mapsFromAny(tools[1]["tools"])
	last := services[len(services)-1]
	last["name"] = "database_query"
	last["description"] = "Run SQL queries against the customer database."
	last["parameters"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"sql": map[string]any{"type": "string"},
		},
		"required": []any{"sql"},
	}
	return tools
}

func TestDeferredToolSearchReturnsCandidatesInsteadOfEmptyRegistry(t *testing.T) {
	tools := namespacedToolsWithHiddenDatabase(120)
	matches := searchDeferredResponsesTools(tools, "database", proxyToolSearchResultMax)
	if len(matches) == 0 {
		t.Fatal("database search returned no deferred tools")
	}
	if got := responsesCandidateDisplayName(matches[0]); got != "services__database_query" {
		t.Fatalf("top search result=%q want services__database_query", got)
	}

	catalog := searchDeferredResponsesTools(tools, "all available tools", proxyToolSearchResultMax)
	if len(catalog) == 0 {
		t.Fatal("generic discovery must return a deterministic catalog slice")
	}
}

func TestResponsesProxyDiscoveryPromotesHiddenToolWithoutClientToolSearch(t *testing.T) {
	tools := namespacedToolsWithHiddenDatabase(120)
	backend := &discoverySequenceBackend{
		model: qoder.Model{UpstreamID: "lite", DisplayName: "Lite", Raw: map[string]any{"key": "lite"}},
		bodies: []string{
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"database\"}"}}]},"finish_reason":"tool_calls"}]}`) + "data: [DONE]\n\n",
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_database","type":"function","function":{"name":"services__database_query","arguments":"{\"sql\":\"select 1\"}"}}]},"finish_reason":"tool_calls"}]}`) + "data: [DONE]\n\n",
		},
	}
	payload, err := json.Marshal(map[string]any{
		"model":  "Lite",
		"input":  "query the database",
		"stream": true,
		"tools":  tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(payload)))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(backend.requests) != 2 {
		t.Fatalf("backend calls=%d want 2", len(backend.requests))
	}
	if len(backend.requests[0].Tools) > autoToolSearchCoreLimit+1 {
		t.Fatalf("first hop tools=%d want <=%d", len(backend.requests[0].Tools), autoToolSearchCoreLimit+1)
	}
	if len(backend.requests[1].Tools) > autoToolSearchCoreLimit+2 {
		t.Fatalf("second hop tools=%d want <=%d", len(backend.requests[1].Tools), autoToolSearchCoreLimit+2)
	}
	if !qoderVisibleToolNamed(backend.requests[1].Tools, "services__database_query") {
		t.Fatalf("discovered database tool not promoted: %#v", backend.requests[1].Tools)
	}
	if !hasInternalSearchResult(backend.requests[1].Messages, "call_search", "database_query") {
		t.Fatalf("second hop missing internal search result: %#v", backend.requests[1].Messages)
	}

	body := rr.Body.String()
	if strings.Contains(body, "tool_search_call") {
		t.Fatalf("proxy-managed tool_search leaked to client: %s", body)
	}
	for _, want := range []string{`"type":"function_call"`, `"name":"database_query"`, `"namespace":"services"`, `"call_id":"call_database"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q: %s", want, body)
		}
	}
}

func TestResponsesProxyDiscoveryFailsOpenToFullRegistry(t *testing.T) {
	tools := namespacedToolsWithHiddenDatabase(120)
	searchFrame := qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"keep searching\"}"}}]},"finish_reason":"tool_calls"}]}`) + "data: [DONE]\n\n"
	backend := &discoverySequenceBackend{
		model: qoder.Model{UpstreamID: "lite", DisplayName: "Lite", Raw: map[string]any{"key": "lite"}},
		bodies: []string{
			searchFrame,
			searchFrame,
			searchFrame,
			qoderFrame(`{"choices":[{"delta":{"content":"fallback reached"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
		},
	}
	payload, err := json.Marshal(map[string]any{
		"model": "Lite",
		"input": "find a very unusual tool",
		"tools": tools,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(payload)))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if len(backend.requests) != proxyToolSearchMaxHops+1 {
		t.Fatalf("backend calls=%d want %d", len(backend.requests), proxyToolSearchMaxHops+1)
	}
	last := backend.requests[len(backend.requests)-1]
	if len(last.Tools) != 120 {
		t.Fatalf("fail-open tools=%d want full registry 120", len(last.Tools))
	}
	if !strings.Contains(rr.Body.String(), "fallback reached") {
		t.Fatalf("missing fail-open result: %s", rr.Body.String())
	}
}

func qoderVisibleToolNamed(tools []any, name string) bool {
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		fn, _ := tool["function"].(map[string]any)
		if asString(fn["name"]) == name {
			return true
		}
	}
	return false
}

func hasInternalSearchResult(messages []map[string]any, callID, contains string) bool {
	for _, message := range messages {
		if asString(message["role"]) != "tool" || asString(message["tool_call_id"]) != callID {
			continue
		}
		if strings.Contains(valueText(message["content"]), contains) {
			return true
		}
	}
	return false
}
