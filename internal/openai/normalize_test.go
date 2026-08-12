package openai

import (
	"encoding/json"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestNormalizeChatKeepsPublicAndUpstreamModelSeparate(t *testing.T) {
	req := ChatRequest{
		Model: "Claude Sonnet 4",
		Messages: []map[string]any{
			{"role": "system", "content": "be concise"},
			{"role": "user", "content": "hello"},
		},
	}
	got, err := NormalizeChat(req, qoder.Model{UpstreamID: "anon-model-123", DisplayName: "Claude Sonnet 4", Raw: map[string]any{"key": "anon-model-123"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicModel != "Claude Sonnet 4" || got.ModelID != "anon-model-123" {
		t.Fatalf("model mapping wrong: %#v", got)
	}
	if got.System != "be concise" || got.LastUserText != "hello" {
		t.Fatalf("normalization wrong: %#v", got)
	}
}

func TestNormalizeResponsesTools(t *testing.T) {
	req := ResponsesRequest{
		Model:        "Display Model",
		Input:        json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"weather?"}]}]`),
		Instructions: json.RawMessage(`"helpful"`),
		Tools: []map[string]any{{
			"type": "function", "name": "weather", "description": "Get weather",
			"parameters": map[string]any{"type": "object"},
		}},
	}
	got, err := NormalizeResponses(req, qoder.Model{UpstreamID: "m1", Raw: map[string]any{"key": "m1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.ModelID != "m1" || got.System != "helpful" || got.LastUserText != "weather?" {
		t.Fatalf("normalized request=%#v", got)
	}
	if len(got.Tools) != 1 {
		t.Fatalf("tools=%#v", got.Tools)
	}
	tool := got.Tools[0].(map[string]any)
	fn := tool["function"].(map[string]any)
	if fn["name"] != "weather" {
		t.Fatalf("tool=%#v", tool)
	}
}

func reasoningModel() qoder.Model {
	return qoder.Model{
		UpstreamID:  "reason-id",
		DisplayName: "Reason Model",
		Raw: map[string]any{
			"key": "reason-id",
			"thinking_config": map[string]any{
				"disabled": map[string]any{},
				"enabled":  map[string]any{"efforts": map[string]any{"low": map[string]any{}, "high": map[string]any{}}},
			},
		},
	}
}

func TestNormalizeChatReasoningEffort(t *testing.T) {
	got, err := NormalizeChat(ChatRequest{
		Model: "Reason Model", ReasoningEffort: "high",
		Messages: []map[string]any{{"role": "user", "content": "solve"}},
	}, reasoningModel())
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort=%q", got.ReasoningEffort)
	}
	if _, err := NormalizeChat(ChatRequest{
		Model: "Reason Model", ReasoningEffort: "medium",
		Messages: []map[string]any{{"role": "user", "content": "solve"}},
	}, reasoningModel()); err == nil {
		t.Fatal("expected unsupported effort error")
	}
}

func TestNormalizeResponsesReasoningEffort(t *testing.T) {
	got, err := NormalizeResponses(ResponsesRequest{
		Model: "Reason Model", Input: json.RawMessage(`"solve"`),
		Reasoning: map[string]any{"effort": "low"},
	}, reasoningModel())
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "low" {
		t.Fatalf("reasoning effort=%q", got.ReasoningEffort)
	}
}
