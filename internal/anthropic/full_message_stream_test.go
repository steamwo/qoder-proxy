package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestMessagesStreamPreservesFullMessagePrefixBeforeDeltas(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "stream-id", DisplayName: "Stream Model", Raw: map[string]any{"key": "stream-id"}},
		body: qoderFrame(`{"choices":[{"message":{"role":"assistant","content":"prefix "},"finish_reason":null}]}`) +
			qoderFrame(`{"choices":[{"delta":{"content":"suffix"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":8,"total_tokens":11}}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"Stream Model","max_tokens":4096,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)

	body := rr.Body.String()
	for _, want := range []string{
		`"text":"prefix ","type":"text_delta"`,
		`"text":"suffix","type":"text_delta"`,
		`"stop_reason":"end_turn"`,
		`"output_tokens":8`,
		"event: message_stop",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q:\n%s", want, body)
		}
	}
	if backend.last.MaxTokens != 4096 {
		t.Fatalf("Anthropic max_tokens was altered before Qoder client: %d", backend.last.MaxTokens)
	}
}
