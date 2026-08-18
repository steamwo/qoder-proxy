package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestResponsesProxyDiscoveryReportsInternalUsage(t *testing.T) {
	tools := namespacedToolsWithHiddenDatabase(120)
	backend := &discoverySequenceBackend{
		model: qoder.Model{UpstreamID: "lite", DisplayName: "Lite", Raw: map[string]any{"key": "lite"}},
		bodies: []string{
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"database\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`) + "data: [DONE]\n\n",
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_database","type":"function","function":{"name":"services__database_query","arguments":"{\"sql\":\"select 1\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`) + "data: [DONE]\n\n",
		},
	}
	payload, err := json.Marshal(map[string]any{
		"model": "Lite",
		"input": "query the database",
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
	body := rr.Body.String()
	for _, want := range []string{`"input_tokens":13`, `"output_tokens":3`, `"total_tokens":16`} {
		if !strings.Contains(body, want) {
			t.Fatalf("combined usage missing %q: %s", want, body)
		}
	}
}

func TestResponsesProxyDiscoveryMixedToolCallsFailOpen(t *testing.T) {
	tools := namespacedToolsWithHiddenDatabase(120)
	backend := &discoverySequenceBackend{
		model: qoder.Model{UpstreamID: "lite", DisplayName: "Lite", Raw: map[string]any{"key": "lite"}},
		bodies: []string{
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_search","type":"function","function":{"name":"tool_search","arguments":"{\"query\":\"database\"}"}},{"index":1,"id":"call_read","type":"function","function":{"name":"workspace__read_file","arguments":"{}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`) + "data: [DONE]\n\n",
			qoderFrame(`{"choices":[{"delta":{"content":"retried with full registry"},"finish_reason":"stop"}],"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`) + "data: [DONE]\n\n",
		},
	}
	payload, err := json.Marshal(map[string]any{
		"model": "Lite",
		"input": "do the task",
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
	if len(backend.requests) != 2 {
		t.Fatalf("backend calls=%d want 2", len(backend.requests))
	}
	if got := len(backend.requests[1].Tools); got != 120 {
		t.Fatalf("mixed-call fail-open tools=%d want 120", got)
	}
	body := rr.Body.String()
	if strings.Contains(body, "tool_search_call") {
		t.Fatalf("synthetic search leaked after fail-open: %s", body)
	}
	if !strings.Contains(body, "retried with full registry") {
		t.Fatalf("missing fail-open response: %s", body)
	}
	for _, want := range []string{`"input_tokens":14`, `"output_tokens":3`, `"total_tokens":17`} {
		if !strings.Contains(body, want) {
			t.Fatalf("combined fail-open usage missing %q: %s", want, body)
		}
	}
}
