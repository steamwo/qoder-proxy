package openai

import (
	"encoding/json"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestNormalizeChatPreservesImageURLs(t *testing.T) {
	req := ChatRequest{
		Model: "Vision Model",
		Messages: []map[string]any{{
			"role": "user",
			"content": []any{
				map[string]any{"type": "text", "text": "describe this"},
				map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,YWJj"}},
			},
		}},
	}
	got, err := NormalizeChat(req, qoder.Model{UpstreamID: "vision-id", DisplayName: "Vision Model", Raw: map[string]any{"key": "vision-id", "is_vl": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ImageURLs) != 1 || got.ImageURLs[0] != "data:image/png;base64,YWJj" {
		t.Fatalf("images=%#v", got.ImageURLs)
	}
	if len(got.Messages) != 1 || got.Messages[0]["content"] != "describe this" {
		t.Fatalf("messages=%#v", got.Messages)
	}
}

func TestNormalizeResponsesPreservesInputImage(t *testing.T) {
	input, err := json.Marshal([]map[string]any{{
		"type": "message",
		"role": "user",
		"content": []any{
			map[string]any{"type": "input_text", "text": "what is shown?"},
			map[string]any{"type": "input_image", "image_url": "https://example.com/screenshot.png"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NormalizeResponses(ResponsesRequest{Model: "Vision Model", Input: input}, qoder.Model{UpstreamID: "vision-id", DisplayName: "Vision Model", Raw: map[string]any{"key": "vision-id", "is_vl": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ImageURLs) != 1 || got.ImageURLs[0] != "https://example.com/screenshot.png" {
		t.Fatalf("images=%#v", got.ImageURLs)
	}
	if len(got.Messages) != 1 || got.Messages[0]["content"] != "what is shown?" {
		t.Fatalf("messages=%#v", got.Messages)
	}
}
