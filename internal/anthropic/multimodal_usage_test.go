package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestMessagesStreamPublishesFinalInputUsage(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "stream-id", DisplayName: "Stream Model", Raw: map[string]any{"key": "stream-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":2,"total_tokens":9,"prompt_tokens_details":{"cached_tokens":3}}}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"Stream Model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)

	body := rr.Body.String()
	delta := strings.LastIndex(body, "event: message_delta")
	if delta < 0 {
		t.Fatalf("missing message_delta:\n%s", body)
	}
	final := body[delta:]
	for _, want := range []string{`"input_tokens":7`, `"output_tokens":2`, `"cache_read_input_tokens":3`} {
		if !strings.Contains(final, want) {
			t.Fatalf("final usage missing %q:\n%s", want, final)
		}
	}
}

func TestNormalizeAnthropicImages(t *testing.T) {
	req := MessageRequest{
		Model:     "Vision Model",
		MaxTokens: 128,
		Messages: []map[string]any{{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "describe these"},
				map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "YWJj"}},
				map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/cat.jpg"}},
			},
		}},
	}
	got, err := normalize(req, qoder.Model{UpstreamID: "vision-id", DisplayName: "Vision Model", Raw: map[string]any{"key": "vision-id", "is_vl": true}})
	if err != nil {
		t.Fatal(err)
	}
	wantImages := []string{"data:image/png;base64,YWJj", "https://example.com/cat.jpg"}
	if len(got.ImageURLs) != len(wantImages) {
		t.Fatalf("images=%#v", got.ImageURLs)
	}
	for i, want := range wantImages {
		if got.ImageURLs[i] != want {
			t.Fatalf("image[%d]=%q want %q", i, got.ImageURLs[i], want)
		}
	}
	if len(got.Messages) != 1 || got.Messages[0]["content"] != "describe these" {
		t.Fatalf("messages=%#v", got.Messages)
	}
	if got.LastUserText != "describe these" {
		t.Fatalf("last user=%q", got.LastUserText)
	}
}
