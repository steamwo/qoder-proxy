package openai

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestResponsesStreamPreservesQoderFullMessagePrefix(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "stream-id", DisplayName: "Stream Model", Raw: map[string]any{"key": "stream-id"}},
		body: qoderFrame(`{"choices":[{"message":{"role":"assistant","content":"prefix "},"finish_reason":null}]}`) +
			qoderFrame(`{"choices":[{"delta":{"content":"suffix"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":8,"total_tokens":11}}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"Stream Model","input":"hello","stream":true}`))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)

	body := rr.Body.String()
	for _, want := range []string{
		`"delta":"prefix "`,
		`"delta":"suffix"`,
		"event: response.completed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q:\n%s", want, body)
		}
	}
}

func TestChatStreamReconcilesQoderFinalMessageSnapshot(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "stream-id", DisplayName: "Stream Model", Raw: map[string]any{"key": "stream-id"}},
		body: qoderFrame(`{"id":"chatcmpl-upstream","model":"stream-id","choices":[{"delta":{"content":"prefix "},"finish_reason":null}]}`) +
			qoderFrame(`{"id":"chatcmpl-upstream","model":"stream-id","choices":[{"delta":{"content":"suffix"},"finish_reason":null}]}`) +
			qoderFrame(`{"id":"chatcmpl-upstream","model":"stream-id","choices":[{"message":{"role":"assistant","content":"prefix suffix"},"finish_reason":"stop"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"Stream Model","stream":true,"messages":[{"role":"user","content":"hello"}]}`))
	rr := httptest.NewRecorder()
	HandleChat(rr, req, backend)

	body := rr.Body.String()
	if strings.Count(body, `"content":"prefix "`) != 1 {
		t.Fatalf("prefix duplicated or lost:\n%s", body)
	}
	if strings.Count(body, `"content":"suffix"`) != 1 {
		t.Fatalf("suffix duplicated or lost:\n%s", body)
	}
	if strings.Contains(body, `"content":"prefix suffix"`) {
		t.Fatalf("final aggregate snapshot leaked as duplicate content:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing stream terminator:\n%s", body)
	}
}
