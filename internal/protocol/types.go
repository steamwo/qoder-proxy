package protocol

import "encoding/json"

type Request struct {
	PublicModel string
	ModelID     string
	ModelConfig map[string]any
	// ReasoningEffort is the per-request Qoder thinking-depth override.
	// Empty means use the model/account default. Supported values are validated
	// against the selected model's thinking_config before reaching the client.
	ReasoningEffort string
	System          string
	Messages        []map[string]any
	Tools           []any
	MaxTokens       int
	Temperature     *float64
	TopP            *float64
	Stop            any
	LastUserText    string
	// ClaudeToolCompatibility enables the Claude Code-specific Qoder tool
	// namespace guard and history repair. OpenAI protocol adapters intentionally
	// leave this false so Anthropic compatibility behavior cannot leak into
	// /v1/chat/completions or /v1/responses.
	ClaudeToolCompatibility bool
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	CachedTokens int `json:"cached_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens"`
}

type EventKind int

const (
	EventTextDelta EventKind = iota + 1
	EventToolDelta
	EventUsage
	EventFinish
	EventError
)

type Event struct {
	Kind EventKind

	Text string

	ToolIndex     int
	ToolID        string
	ToolName      string
	ToolArguments string

	Usage Usage

	FinishReason string
	Error        string
	Raw          json.RawMessage
}
