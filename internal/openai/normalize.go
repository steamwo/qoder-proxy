package openai

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type ChatRequest struct {
	Model               string           `json:"model"`
	Messages            []map[string]any `json:"messages"`
	Stream              bool             `json:"stream"`
	StreamOptions       map[string]any   `json:"stream_options,omitempty"`
	Tools               []any            `json:"tools,omitempty"`
	ToolChoice          any              `json:"tool_choice,omitempty"`
	Temperature         *float64         `json:"temperature,omitempty"`
	TopP                *float64         `json:"top_p,omitempty"`
	MaxTokens           int              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int              `json:"max_completion_tokens,omitempty"`
	Stop                any              `json:"stop,omitempty"`
	ReasoningEffort     string           `json:"reasoning_effort,omitempty"`
}

type ResponsesRequest struct {
	Model           string           `json:"model"`
	Input           json.RawMessage  `json:"input"`
	Instructions    json.RawMessage  `json:"instructions,omitempty"`
	Stream          bool             `json:"stream"`
	Tools           []map[string]any `json:"tools,omitempty"`
	ToolChoice      any              `json:"tool_choice,omitempty"`
	Temperature     *float64         `json:"temperature,omitempty"`
	TopP            *float64         `json:"top_p,omitempty"`
	MaxOutputTokens int              `json:"max_output_tokens,omitempty"`
	Reasoning       map[string]any   `json:"reasoning,omitempty"`
	Metadata        map[string]any   `json:"metadata,omitempty"`
}

func NormalizeChat(req ChatRequest, model qoder.Model) (protocol.Request, error) {
	messages, system, lastUser := normalizeMessages(req.Messages)
	reasoningEffort, err := model.NormalizeReasoningEffort(req.ReasoningEffort)
	if err != nil {
		return protocol.Request{}, err
	}
	max := req.MaxCompletionTokens
	if max <= 0 {
		max = req.MaxTokens
	}
	return protocol.Request{
		PublicModel: req.Model, ModelID: model.UpstreamID, ModelConfig: model.Raw,
		ReasoningEffort: reasoningEffort,
		System:          system, Messages: messages, Tools: req.Tools, MaxTokens: max,
		Temperature: req.Temperature, TopP: req.TopP, Stop: req.Stop, LastUserText: lastUser,
	}, nil
}

func NormalizeResponses(req ResponsesRequest, model qoder.Model) (protocol.Request, error) {
	reasoningEffort, err := model.NormalizeReasoningEffort(asString(req.Reasoning["effort"]))
	if err != nil {
		return protocol.Request{}, err
	}
	var rawMessages []map[string]any
	var systemParts []string
	var inputString string
	if len(req.Input) == 0 || string(req.Input) == "null" {
		return protocol.Request{}, fmt.Errorf("input is required")
	}
	if err := json.Unmarshal(req.Input, &inputString); err == nil {
		rawMessages = append(rawMessages, map[string]any{"role": "user", "content": inputString})
	} else {
		var items []map[string]any
		if err := json.Unmarshal(req.Input, &items); err != nil {
			return protocol.Request{}, fmt.Errorf("unsupported responses input: %w", err)
		}
		for _, item := range items {
			typeName := asString(item["type"])
			role := asString(item["role"])
			if typeName == "function_call" {
				rawMessages = append(rawMessages, map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
					"id": firstString(item, "call_id", "id"), "type": "function", "function": map[string]any{"name": asString(item["name"]), "arguments": asString(item["arguments"])},
				}}})
				continue
			}
			if typeName == "function_call_output" {
				rawMessages = append(rawMessages, map[string]any{"role": "tool", "tool_call_id": asString(item["call_id"]), "content": valueText(item["output"])})
				continue
			}
			if role == "" && typeName == "message" {
				role = "user"
			}
			if role == "" {
				continue
			}
			rawMessages = append(rawMessages, map[string]any{"role": role, "content": item["content"]})
		}
	}
	if len(req.Instructions) > 0 && string(req.Instructions) != "null" {
		var s string
		if json.Unmarshal(req.Instructions, &s) == nil && s != "" {
			systemParts = append(systemParts, s)
		}
		var arr []any
		if json.Unmarshal(req.Instructions, &arr) == nil {
			if t := contentText(arr); t != "" {
				systemParts = append(systemParts, t)
			}
		}
	}
	messages, system, lastUser := normalizeMessages(rawMessages)
	if len(systemParts) > 0 {
		system = strings.Join(append(systemParts, system), "\n\n")
		system = strings.TrimSpace(system)
	}
	tools := make([]any, 0, len(req.Tools))
	for _, tool := range req.Tools {
		if asString(tool["type"]) != "function" {
			continue
		}
		fn := map[string]any{"name": asString(tool["name"]), "description": asString(tool["description"]), "parameters": tool["parameters"]}
		if strict, ok := tool["strict"].(bool); ok {
			fn["strict"] = strict
		}
		tools = append(tools, map[string]any{"type": "function", "function": fn})
	}
	return protocol.Request{
		PublicModel: req.Model, ModelID: model.UpstreamID, ModelConfig: model.Raw, ReasoningEffort: reasoningEffort, System: system, Messages: messages, Tools: tools,
		MaxTokens: req.MaxOutputTokens, Temperature: req.Temperature, TopP: req.TopP, LastUserText: lastUser,
	}, nil
}

func normalizeMessages(input []map[string]any) ([]map[string]any, string, string) {
	out := make([]map[string]any, 0, len(input))
	var systemParts []string
	lastUser := ""
	for _, raw := range input {
		role := asString(raw["role"])
		if role == "" {
			role = "user"
		}
		text := contentText(raw["content"])
		if role == "system" || role == "developer" {
			if text != "" {
				systemParts = append(systemParts, text)
			}
			continue
		}
		if role == "user" && text != "" {
			lastUser = text
		}
		m := make(map[string]any, len(raw)+2)
		for k, v := range raw {
			m[k] = v
		}
		m["role"] = role
		m["content"] = text
		out = append(out, m)
	}
	return out, strings.Join(systemParts, "\n\n"), lastUser
}

func contentText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	arr, ok := v.([]any)
	if !ok {
		return valueText(v)
	}
	var parts []string
	for _, raw := range arr {
		switch p := raw.(type) {
		case string:
			if p != "" {
				parts = append(parts, p)
			}
		case map[string]any:
			if s := firstString(p, "text", "content"); s != "" {
				parts = append(parts, s)
			}
		}
	}
	return strings.Join(parts, "\n")
}
func valueText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func asString(v any) string { s, _ := v.(string); return s }
func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := asString(m[k]); s != "" {
			return s
		}
	}
	return ""
}
