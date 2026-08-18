package openai

import (
	"encoding/json"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestResponsesPromptCacheKeyBindsStableClientSession(t *testing.T) {
	model := qoder.Model{
		UpstreamID:  "model-internal",
		DisplayName: "Model",
		Raw:         map[string]any{"key": "model-internal"},
	}

	var first ResponsesRequest
	if err := json.Unmarshal([]byte(`{"model":"Model","input":"first turn","prompt_cache_key":"thread-123"}`), &first); err != nil {
		t.Fatal(err)
	}
	firstNormalized, err := NormalizeResponses(first, model)
	if err != nil {
		t.Fatal(err)
	}

	var second ResponsesRequest
	if err := json.Unmarshal([]byte(`{"model":"Model","input":"later turn","prompt_cache_key":"thread-123"}`), &second); err != nil {
		t.Fatal(err)
	}
	secondNormalized, err := NormalizeResponses(second, model)
	if err != nil {
		t.Fatal(err)
	}

	want := "openai-responses/prompt-cache/thread-123"
	if firstNormalized.ClientSessionKey != want {
		t.Fatalf("first ClientSessionKey=%q want %q", firstNormalized.ClientSessionKey, want)
	}
	if secondNormalized.ClientSessionKey != firstNormalized.ClientSessionKey {
		t.Fatalf("same prompt_cache_key must survive changing turns: %q vs %q", firstNormalized.ClientSessionKey, secondNormalized.ClientSessionKey)
	}
}

func TestResponsesPromptCacheKeyTrimsWhitespace(t *testing.T) {
	if got := responsesPromptCacheSessionKey("  thread-123  "); got != "openai-responses/prompt-cache/thread-123" {
		t.Fatalf("session key=%q", got)
	}
}

func TestResponsesWithoutPromptCacheKeyRemainsUnbound(t *testing.T) {
	model := qoder.Model{
		UpstreamID:  "model-internal",
		DisplayName: "Model",
		Raw:         map[string]any{"key": "model-internal"},
	}
	normalized, err := NormalizeResponses(ResponsesRequest{
		Model: "Model",
		Input: json.RawMessage(`"hello"`),
	}, model)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.ClientSessionKey != "" {
		t.Fatalf("request without prompt_cache_key unexpectedly bound to %q", normalized.ClientSessionKey)
	}
}
