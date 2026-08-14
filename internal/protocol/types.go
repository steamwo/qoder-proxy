package protocol

import "encoding/json"

type ToolRoute struct {
	// Kind is the downstream Responses API tool kind represented by the
	// OpenAI-function-shaped tool sent to Qoder. Currently "function" and
	// "tool_search" are used.
	Kind      string
	Name      string
	Namespace string
}

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
	// ToolRoutes maps Qoder-visible function names back to downstream protocol
	// tool identities. It lets the Responses adapter flatten namespace/tool_search
	// tools for Qoder without losing the client-visible tool kind or namespace.
	ToolRoutes       map[string]ToolRoute
	MaxTokens        int
	Temperature      *float64
	TopP             *float64
	Stop             any
	LastUserText     string
	// ClientSessionKey identifies the current client-side conversation or agent.
	// It is never forwarded verbatim; Qoder derives a stable opaque session ID
	// from it. Empty means no trustworthy client session identifier was supplied.
	ClientSessionKey string
	// ClaudeToolCompatibility enables the Claude Code-specific Qoder tool
	// namespace reminder and response guard. OpenAI protocol adapters
	// intentionally leave this false so Claude ToolSearch behavior cannot leak
	// into /v1/chat/completions or /v1/responses.
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
