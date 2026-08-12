package anthropic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type Backend interface {
	ResolveModel(context.Context, string) (qoder.Model, error)
	Chat(context.Context, protocol.Request) (*http.Response, error)
}

type queueAwareBackend interface {
	ChatWithQueue(context.Context, protocol.Request, func(qoder.QueueInfo) error) (*http.Response, error)
}

type MessageRequest struct {
	Model         string           `json:"model"`
	MaxTokens     int              `json:"max_tokens"`
	Messages      []map[string]any `json:"messages"`
	System        any              `json:"system,omitempty"`
	Stream        bool             `json:"stream,omitempty"`
	Tools         []map[string]any `json:"tools,omitempty"`
	ToolChoice    any              `json:"tool_choice,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
	OutputConfig  map[string]any   `json:"output_config,omitempty"`
	Thinking      map[string]any   `json:"thinking,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty"`
}

type toolState struct {
	ID    string
	Name  string
	Args  strings.Builder
	Index int
}

type aggregate struct {
	Text   strings.Builder
	Tools  map[int]*toolState
	Usage  protocol.Usage
	Finish string
}

func HandleMessages(w http.ResponseWriter, r *http.Request, backend Backend) {
	var req MessageRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "invalid JSON: "+err.Error())
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "model is required")
		return
	}
	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "messages is required")
		return
	}
	if req.MaxTokens <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request_error", "max_tokens must be greater than 0")
		return
	}

	model, err := backend.ResolveModel(r.Context(), req.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found_error", err.Error())
		return
	}
	preq, err := normalize(req, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	if req.Stream {
		streamMessages(w, r, backend, preq, req)
		return
	}

	resp, err := chatWithQueue(r.Context(), backend, preq, nil)
	if err != nil {
		writeBackendError(w, err, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	a, err := collectEvents(resp)
	if err != nil {
		writeError(w, http.StatusBadGateway, "api_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, messageObject(req, a))
}

func chatWithQueue(ctx context.Context, backend Backend, req protocol.Request, onQueue func(qoder.QueueInfo) error) (*http.Response, error) {
	if qb, ok := backend.(queueAwareBackend); ok {
		return qb.ChatWithQueue(ctx, req, onQueue)
	}
	return backend.Chat(ctx, req)
}

func normalize(req MessageRequest, model qoder.Model) (protocol.Request, error) {
	requestedEffort := stringValue(req.OutputConfig["effort"])
	thinkingType := strings.ToLower(strings.TrimSpace(stringValue(req.Thinking["type"])))
	if thinkingType == "disabled" && requestedEffort != "" {
		return protocol.Request{}, fmt.Errorf("output_config.effort cannot be combined with thinking.type=disabled when proxying to Qoder")
	}
	if requestedEffort == "" && thinkingType == "disabled" {
		requestedEffort = "none"
	}
	reasoningEffort, err := model.NormalizeReasoningEffort(requestedEffort)
	if err != nil {
		return protocol.Request{}, err
	}
	system, err := textContent(req.System, false)
	if err != nil {
		return protocol.Request{}, fmt.Errorf("system: %w", err)
	}

	messages := make([]map[string]any, 0, len(req.Messages))
	lastUser := ""
	for i, raw := range req.Messages {
		role, _ := raw["role"].(string)
		if role != "user" && role != "assistant" {
			return protocol.Request{}, fmt.Errorf("messages[%d].role must be user or assistant", i)
		}
		converted, userText, err := convertMessage(role, raw["content"])
		if err != nil {
			return protocol.Request{}, fmt.Errorf("messages[%d]: %w", i, err)
		}
		messages = append(messages, converted...)
		if userText != "" {
			lastUser = userText
		}
	}

	tools := make([]any, 0, len(req.Tools))
	for i, tool := range req.Tools {
		name := stringValue(tool["name"])
		if name == "" {
			return protocol.Request{}, fmt.Errorf("tools[%d].name is required", i)
		}
		schema := tool["input_schema"]
		if schema == nil {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		fn := map[string]any{
			"name":       name,
			"parameters": schema,
		}
		if desc := stringValue(tool["description"]); desc != "" {
			fn["description"] = desc
		}
		tools = append(tools, map[string]any{"type": "function", "function": fn})
	}

	var stop any
	if len(req.StopSequences) == 1 {
		stop = req.StopSequences[0]
	} else if len(req.StopSequences) > 1 {
		vals := make([]any, len(req.StopSequences))
		for i, s := range req.StopSequences {
			vals[i] = s
		}
		stop = vals
	}

	return protocol.Request{
		PublicModel:     req.Model,
		ModelID:         model.UpstreamID,
		ModelConfig:     model.Raw,
		ReasoningEffort: reasoningEffort,
		System:          system,
		Messages:        messages,
		Tools:           tools,
		MaxTokens:       req.MaxTokens,
		Temperature:     req.Temperature,
		TopP:            req.TopP,
		Stop:            stop,
		LastUserText:    lastUser,
	}, nil
}

func convertMessage(role string, content any) ([]map[string]any, string, error) {
	if s, ok := content.(string); ok {
		return []map[string]any{{"role": role, "content": s}}, ternary(role == "user", s, ""), nil
	}
	blocks, ok := content.([]any)
	if !ok {
		return nil, "", fmt.Errorf("content must be a string or content-block array")
	}

	if role == "assistant" {
		var texts []string
		calls := make([]any, 0)
		for _, raw := range blocks {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch stringValue(block["type"]) {
			case "text":
				if text := stringValue(block["text"]); text != "" {
					texts = append(texts, text)
				}
			case "tool_use":
				id := stringValue(block["id"])
				if id == "" {
					id = newID("toolu_")
				}
				name := stringValue(block["name"])
				if name == "" {
					return nil, "", fmt.Errorf("tool_use.name is required")
				}
				input := block["input"]
				if input == nil {
					input = map[string]any{}
				}
				b, err := json.Marshal(input)
				if err != nil {
					return nil, "", fmt.Errorf("invalid tool_use.input: %w", err)
				}
				calls = append(calls, map[string]any{
					"id": id, "type": "function",
					"function": map[string]any{"name": name, "arguments": string(b)},
				})
			case "thinking", "redacted_thinking":
				// Qoder's current OpenAI-style chat adapter does not accept Anthropic
				// thinking blocks as conversation history. Ignore them rather than
				// leaking hidden chain-of-thought into the prompt.
			case "":
				continue
			default:
				return nil, "", fmt.Errorf("unsupported assistant content block type %q", stringValue(block["type"]))
			}
		}
		msg := map[string]any{"role": "assistant", "content": strings.Join(texts, "\n")}
		if len(calls) > 0 {
			msg["tool_calls"] = calls
		}
		return []map[string]any{msg}, "", nil
	}

	// Anthropic tool_result blocks are carried inside a user message. Qoder's
	// OpenAI-style request expects tool results as separate role=tool messages.
	// Preserve the block order as far as possible by flushing adjacent text
	// before each tool_result.
	out := make([]map[string]any, 0, len(blocks))
	var textParts []string
	lastUser := ""
	flushText := func() {
		if len(textParts) == 0 {
			return
		}
		text := strings.Join(textParts, "\n")
		out = append(out, map[string]any{"role": "user", "content": text})
		lastUser = text
		textParts = nil
	}
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := stringValue(block["type"])
		switch typ {
		case "text":
			if text := stringValue(block["text"]); text != "" {
				textParts = append(textParts, text)
			}
		case "tool_result":
			flushText()
			toolID := stringValue(block["tool_use_id"])
			if toolID == "" {
				return nil, "", fmt.Errorf("tool_result.tool_use_id is required")
			}
			text, err := textContent(block["content"], true)
			if err != nil {
				return nil, "", fmt.Errorf("tool_result content: %w", err)
			}
			if text == "" {
				text = ""
			}
			out = append(out, map[string]any{"role": "tool", "tool_call_id": toolID, "content": text})
		case "":
			continue
		default:
			return nil, "", fmt.Errorf("unsupported user content block type %q", typ)
		}
	}
	flushText()
	if len(out) == 0 {
		out = append(out, map[string]any{"role": "user", "content": ""})
	}
	return out, lastUser, nil
}

func textContent(v any, allowJSON bool) (string, error) {
	if v == nil {
		return "", nil
	}
	if s, ok := v.(string); ok {
		return s, nil
	}
	blocks, ok := v.([]any)
	if !ok {
		if allowJSON {
			b, err := json.Marshal(v)
			return string(b), err
		}
		return "", fmt.Errorf("must be a string or text-block array")
	}
	var parts []string
	for _, raw := range blocks {
		block, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		typ := stringValue(block["type"])
		if typ == "text" || typ == "" {
			if text := stringValue(block["text"]); text != "" {
				parts = append(parts, text)
			}
			continue
		}
		if allowJSON {
			b, err := json.Marshal(block)
			if err != nil {
				return "", err
			}
			parts = append(parts, string(b))
			continue
		}
		return "", fmt.Errorf("unsupported content block type %q", typ)
	}
	return strings.Join(parts, "\n"), nil
}

func collectEvents(resp *http.Response) (*aggregate, error) {
	a := &aggregate{Tools: map[int]*toolState{}}
	err := qoder.ParseStream(resp.Body, func(ev protocol.Event) error {
		switch ev.Kind {
		case protocol.EventTextDelta:
			a.Text.WriteString(ev.Text)
		case protocol.EventToolDelta:
			t := a.Tools[ev.ToolIndex]
			if t == nil {
				t = &toolState{Index: ev.ToolIndex}
				a.Tools[ev.ToolIndex] = t
			}
			if ev.ToolID != "" {
				t.ID = ev.ToolID
			}
			if ev.ToolName != "" {
				t.Name = ev.ToolName
			}
			t.Args.WriteString(ev.ToolArguments)
		case protocol.EventUsage:
			a.Usage = ev.Usage
		case protocol.EventFinish:
			a.Finish = ev.FinishReason
		case protocol.EventError:
			return fmt.Errorf("%s", ev.Error)
		}
		return nil
	})
	return a, err
}

func messageObject(req MessageRequest, a *aggregate) map[string]any {
	content := make([]any, 0, len(a.Tools)+1)
	if a.Text.Len() > 0 {
		content = append(content, map[string]any{"type": "text", "text": a.Text.String()})
	}
	for _, idx := range sortedToolIndexes(a.Tools) {
		t := a.Tools[idx]
		id := t.ID
		if id == "" {
			id = newID("toolu_")
		}
		input := map[string]any{}
		args := strings.TrimSpace(t.Args.String())
		if args != "" {
			var parsed map[string]any
			if json.Unmarshal([]byte(args), &parsed) == nil && parsed != nil {
				input = parsed
			}
		}
		content = append(content, map[string]any{
			"type": "tool_use", "id": id, "name": t.Name, "input": input,
		})
	}
	return map[string]any{
		"id":            newID("msg_"),
		"type":          "message",
		"role":          "assistant",
		"model":         req.Model,
		"content":       content,
		"stop_reason":   anthropicStopReason(a.Finish, len(a.Tools) > 0),
		"stop_sequence": nil,
		"usage":         anthropicUsage(a.Usage),
	}
}

func streamMessages(w http.ResponseWriter, r *http.Request, backend Backend, preq protocol.Request, req MessageRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "api_error", "HTTP streaming unsupported")
		return
	}

	started := false
	startSSE := func() {
		if started {
			return
		}
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-store")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		started = true
	}
	onQueue := func(info qoder.QueueInfo) error {
		startSSE()
		_, err := fmt.Fprintf(w, ": qoder queued queue_type=%s queue_count=%d retry_after=%ds wait_time=%ds\n\n",
			info.QueueType, info.QueueCount, info.RetryAfterSeconds, info.WaitTime)
		if err == nil {
			flusher.Flush()
		}
		return err
	}

	resp, err := chatWithQueue(r.Context(), backend, preq, onQueue)
	if err != nil {
		if !started {
			writeBackendError(w, err, http.StatusBadGateway)
			return
		}
		emitStreamError(w, flusher, err)
		return
	}
	defer resp.Body.Close()
	startSSE()

	id := newID("msg_")
	emit := func(event string, payload map[string]any) error {
		payload["type"] = event
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if err := emit("message_start", map[string]any{"message": map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": req.Model,
		"content": []any{}, "stop_reason": nil, "stop_sequence": nil,
		"usage": map[string]any{"input_tokens": 0, "output_tokens": 0},
	}}); err != nil {
		return
	}

	nextBlock := 0
	textBlock := -1
	tools := map[int]*toolState{}
	usage := protocol.Usage{}
	finish := ""
	meaningful := 0

	err = qoder.ParseStream(resp.Body, func(ev protocol.Event) error {
		switch ev.Kind {
		case protocol.EventTextDelta:
			meaningful++
			if textBlock < 0 {
				textBlock = nextBlock
				nextBlock++
				if err := emit("content_block_start", map[string]any{
					"index":         textBlock,
					"content_block": map[string]any{"type": "text", "text": ""},
				}); err != nil {
					return err
				}
			}
			return emit("content_block_delta", map[string]any{
				"index": textBlock,
				"delta": map[string]any{"type": "text_delta", "text": ev.Text},
			})

		case protocol.EventToolDelta:
			// Buffer Qoder/OpenAI tool-call fragments and emit each Anthropic
			// tool_use block sequentially after the upstream stream finishes.
			// OpenAI may interleave fragments for parallel tool calls, while
			// Anthropic content blocks need a clean start/delta/stop lifecycle.
			meaningful++
			t := tools[ev.ToolIndex]
			if t == nil {
				t = &toolState{ID: ev.ToolID, Name: ev.ToolName}
				tools[ev.ToolIndex] = t
			}
			if ev.ToolID != "" {
				t.ID = ev.ToolID
			}
			if ev.ToolName != "" {
				t.Name = ev.ToolName
			}
			t.Args.WriteString(ev.ToolArguments)

		case protocol.EventUsage:
			usage = ev.Usage
		case protocol.EventFinish:
			finish = ev.FinishReason
		case protocol.EventError:
			return fmt.Errorf("%s", ev.Error)
		}
		return nil
	})
	if err != nil {
		emitStreamError(w, flusher, err)
		return
	}
	if meaningful == 0 {
		slog.Warn("qoder stream produced no anthropic output", "model", req.Model)
	}

	// Close live text first, then emit buffered tool_use blocks one by one.
	if textBlock >= 0 {
		if err := emit("content_block_stop", map[string]any{"index": textBlock}); err != nil {
			return
		}
	}
	for _, qoderIndex := range sortedToolIndexes(tools) {
		t := tools[qoderIndex]
		t.Index = nextBlock
		nextBlock++
		if t.ID == "" {
			t.ID = newID("toolu_")
		}
		if err := emit("content_block_start", map[string]any{
			"index":         t.Index,
			"content_block": map[string]any{"type": "tool_use", "id": t.ID, "name": t.Name, "input": map[string]any{}},
		}); err != nil {
			return
		}
		if args := t.Args.String(); args != "" {
			if err := emit("content_block_delta", map[string]any{
				"index": t.Index,
				"delta": map[string]any{"type": "input_json_delta", "partial_json": args},
			}); err != nil {
				return
			}
		}
		if err := emit("content_block_stop", map[string]any{"index": t.Index}); err != nil {
			return
		}
	}

	stop := anthropicStopReason(finish, len(tools) > 0)
	if err := emit("message_delta", map[string]any{
		"delta": map[string]any{"stop_reason": stop, "stop_sequence": nil},
		"usage": map[string]any{"output_tokens": usage.OutputTokens},
	}); err != nil {
		return
	}
	_ = emit("message_stop", map[string]any{})
}

func anthropicStopReason(finish string, hasTools bool) string {
	if hasTools || finish == "tool_calls" {
		return "tool_use"
	}
	switch finish {
	case "length":
		return "max_tokens"
	case "stop_sequence":
		return "stop_sequence"
	case "refusal", "content_filter":
		return "refusal"
	case "max_tokens":
		return "max_tokens"
	default:
		return "end_turn"
	}
}

func anthropicUsage(u protocol.Usage) map[string]any {
	out := map[string]any{
		"input_tokens":  u.InputTokens,
		"output_tokens": u.OutputTokens,
	}
	if u.CachedTokens > 0 {
		out["cache_read_input_tokens"] = u.CachedTokens
	}
	return out
}

func sortedToolIndexes(m map[int]*toolState) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, typ, message string) {
	writeJSON(w, status, map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}})
}

func writeBackendError(w http.ResponseWriter, err error, fallbackStatus int) {
	var upstreamErr *qoder.UpstreamError
	if errors.As(err, &upstreamErr) {
		writeError(w, upstreamErr.HTTPStatus, anthropicErrorType(upstreamErr), upstreamErr.Message)
		return
	}
	writeError(w, fallbackStatus, "api_error", err.Error())
}

func emitStreamError(w http.ResponseWriter, flusher http.Flusher, err error) {
	typ := "api_error"
	message := err.Error()
	var upstreamErr *qoder.UpstreamError
	if errors.As(err, &upstreamErr) {
		typ = anthropicErrorType(upstreamErr)
		message = upstreamErr.Message
	}
	b, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": message}})
	_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", b)
	flusher.Flush()
}

func anthropicErrorType(e *qoder.UpstreamError) string {
	switch e.HTTPStatus {
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_error"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusRequestEntityTooLarge:
		return "request_too_large"
	case http.StatusTooManyRequests:
		return "rate_limit_error"
	default:
		if e.HTTPStatus >= 500 {
			return "api_error"
		}
		return "invalid_request_error"
	}
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func ternary[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
