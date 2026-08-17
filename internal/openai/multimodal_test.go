package openai

import (
	"encoding/json"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestNormalizeChatKeepsImageBoundToUserMessage(t *testing.T) {
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
	if got.SourceProtocol != "openai_chat" {
		t.Fatalf("source protocol=%q", got.SourceProtocol)
	}
	assertCanonicalImageContent(t, got.Messages[0]["content"], "describe this", "data:image/png;base64,YWJj")
}

func TestNormalizeChatDoesNotMoveHistoricalImageToLatestUser(t *testing.T) {
	req := ChatRequest{
		Model: "Vision Model",
		Messages: []map[string]any{
			{
				"role": "user",
				"content": []any{
					map[string]any{"type": "text", "text": "first"},
					map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/old.png"}},
				},
			},
			{"role": "assistant", "content": "noted"},
			{"role": "user", "content": "second"},
		},
	}
	got, err := NormalizeChat(req, qoder.Model{UpstreamID: "vision-id", Raw: map[string]any{"key": "vision-id", "is_vl": true}})
	if err != nil {
		t.Fatal(err)
	}
	assertCanonicalImageContent(t, got.Messages[0]["content"], "first", "https://example.com/old.png")
	if got.Messages[2]["content"] != "second" {
		t.Fatalf("latest user content=%#v", got.Messages[2]["content"])
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
	if got.SourceProtocol != "openai_responses" {
		t.Fatalf("source protocol=%q", got.SourceProtocol)
	}
	assertCanonicalImageContent(t, got.Messages[0]["content"], "what is shown?", "https://example.com/screenshot.png")
}

func TestNormalizeResponsesPreservesToolOutputImage(t *testing.T) {
	input, err := json.Marshal([]map[string]any{
		{"type": "message", "role": "user", "content": "inspect the screenshot"},
		{"type": "function_call_output", "call_id": "call_1", "output": []any{
			map[string]any{"type": "input_text", "text": "tool screenshot"},
			map[string]any{"type": "input_image", "image_url": "data:image/png;base64,YWJj"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := NormalizeResponses(ResponsesRequest{Model: "Vision Model", Input: input}, qoder.Model{UpstreamID: "vision-id", Raw: map[string]any{"key": "vision-id", "is_vl": true}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 2 || got.Messages[1]["role"] != "tool" {
		t.Fatalf("messages=%#v", got.Messages)
	}
	assertCanonicalImageContent(t, got.Messages[1]["content"], "tool screenshot", "data:image/png;base64,YWJj")
}

func assertCanonicalImageContent(t *testing.T, raw any, wantText, wantImage string) {
	t.Helper()
	parts, ok := raw.([]any)
	if !ok || len(parts) != 2 {
		t.Fatalf("content=%#v", raw)
	}
	text, ok := parts[0].(map[string]any)
	if !ok || text["type"] != "text" || text["text"] != wantText {
		t.Fatalf("text part=%#v", parts[0])
	}
	image, ok := parts[1].(map[string]any)
	if !ok || image["type"] != "image_url" {
		t.Fatalf("image part=%#v", parts[1])
	}
	imageURL, ok := image["image_url"].(map[string]any)
	if !ok || imageURL["url"] != wantImage {
		t.Fatalf("image url=%#v", image["image_url"])
	}
}
