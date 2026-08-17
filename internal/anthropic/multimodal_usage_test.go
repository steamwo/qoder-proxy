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

func TestNormalizeAnthropicImagesStayInUserMessage(t *testing.T) {
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
	if got.SourceProtocol != "anthropic" {
		t.Fatalf("source protocol=%q", got.SourceProtocol)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("messages=%#v", got.Messages)
	}
	parts, ok := got.Messages[0]["content"].([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("content=%#v", got.Messages[0]["content"])
	}
	assertAnthropicCanonicalImage(t, parts[1], "data:image/png;base64,YWJj")
	assertAnthropicCanonicalImage(t, parts[2], "https://example.com/cat.jpg")
	if got.LastUserText != "describe these" {
		t.Fatalf("last user=%q", got.LastUserText)
	}
}

func TestNormalizeAnthropicPreservesImagesAroundToolResult(t *testing.T) {
	req := MessageRequest{
		Model:     "Vision Model",
		MaxTokens: 128,
		Messages: []map[string]any{{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "before"},
				map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/before.png"}},
				map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": []any{
					map[string]any{"type": "text", "text": "tool screenshot"},
					map[string]any{"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "dG9vbA=="}},
				}},
				map[string]any{"type": "text", "text": "after"},
				map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": "https://example.com/after.png"}},
			},
		}},
	}
	got, err := normalize(req, qoder.Model{UpstreamID: "vision-id", Raw: map[string]any{"key": "vision-id", "is_vl": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages=%#v", got.Messages)
	}
	if got.Messages[0]["role"] != "user" || got.Messages[1]["role"] != "tool" || got.Messages[2]["role"] != "user" {
		t.Fatalf("roles=%#v", got.Messages)
	}
	before := got.Messages[0]["content"].([]any)
	tool := got.Messages[1]["content"].([]any)
	after := got.Messages[2]["content"].([]any)
	assertAnthropicCanonicalImage(t, before[1], "https://example.com/before.png")
	assertAnthropicCanonicalImage(t, tool[1], "data:image/png;base64,dG9vbA==")
	assertAnthropicCanonicalImage(t, after[1], "https://example.com/after.png")
	if got.LastUserText != "after" {
		t.Fatalf("last user=%q", got.LastUserText)
	}
}

func assertAnthropicCanonicalImage(t *testing.T, raw any, want string) {
	t.Helper()
	part, ok := raw.(map[string]any)
	if !ok || part["type"] != "image_url" {
		t.Fatalf("image part=%#v", raw)
	}
	imageURL, ok := part["image_url"].(map[string]any)
	if !ok || imageURL["url"] != want {
		t.Fatalf("image url=%#v want %q", part["image_url"], want)
	}
}
