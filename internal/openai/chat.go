package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

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

func chatWithQueue(ctx context.Context, backend Backend, req protocol.Request, onQueue func(qoder.QueueInfo) error) (*http.Response, error) {
	if qb, ok := backend.(queueAwareBackend); ok {
		return qb.ChatWithQueue(ctx, req, onQueue)
	}
	return backend.Chat(ctx, req)
}

func HandleChat(w http.ResponseWriter, r *http.Request, backend Backend) {
	var req ChatRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "model and messages are required")
		return
	}

	model, err := backend.ResolveModel(r.Context(), req.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, "model_not_found", err.Error())
		return
	}
	preq, err := NormalizeChat(req, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if req.Stream {
		streamChatRequest(w, r, backend, preq, req)
		return
	}
	resp, err := chatWithQueue(r.Context(), backend, preq, nil)
	if err != nil {
		writeBackendError(w, err, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	collectChat(w, resp, req)
}

type toolState struct {
	ID    string
	Name  string
	Args  string
	Index int
}

type aggregate struct {
	Text   strings.Builder
	Tools  map[int]*toolState
	Usage  protocol.Usage
	Finish string
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
			t.Args += ev.ToolArguments
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

func collectChat(w http.ResponseWriter, resp *http.Response, req ChatRequest) {
	a, err := collectEvents(resp)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_stream_error", err.Error())
		return
	}

	message := map[string]any{
		"role":    "assistant",
		"content": a.Text.String(),
	}
	if len(a.Tools) > 0 {
		message["content"] = nil
		message["tool_calls"] = chatToolCalls(a.Tools)
	}
	finish := a.Finish
	if finish == "" {
		if len(a.Tools) > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":      newID("chatcmpl-"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
		"usage": chatUsage(a.Usage),
	})
}

func streamChatRequest(w http.ResponseWriter, r *http.Request, backend Backend, preq protocol.Request, req ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "HTTP streaming unsupported")
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
		// Once a request is genuinely queued we intentionally start the SSE
		// response so heartbeat comments can keep browser/proxy idle timers alive.
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
			// The first Qoder SSE event can itself be a business-layer rejection
			// (for example code 112 / no quota). Because no HTTP response has been
			// committed yet, return a real OpenAI JSON error status instead of 200.
			writeBackendError(w, err, http.StatusBadGateway)
			return
		}
		b, _ := json.Marshal(backendErrorPayload(err))
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
		flusher.Flush()
		slog.Warn("qoder queued request failed", "model", req.Model, "error", err)
		return
	}
	defer resp.Body.Close()
	startSSE()
	relayChat(w, flusher, resp, req)
}

func relayChat(w http.ResponseWriter, flusher http.Flusher, resp *http.Response, req ChatRequest) {
	wrotePayload := false
	err := qoder.RelayChatStream(resp.Body, req.Model, func(data string) error {
		if data != "[DONE]" {
			wrotePayload = true
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})
	if err != nil {
		slog.Warn("qoder chat relay failed", "model", req.Model, "error", err)
		return
	}
	if !wrotePayload {
		slog.Warn("qoder stream produced no chat output", "model", req.Model)
	}
}

func includeUsage(m map[string]any) bool {
	v, _ := m["include_usage"].(bool)
	return v
}

func chatUsage(u protocol.Usage) map[string]any {
	return map[string]any{
		"prompt_tokens":     u.InputTokens,
		"completion_tokens": u.OutputTokens,
		"total_tokens":      u.TotalTokens,
		"prompt_tokens_details": map[string]any{
			"cached_tokens": u.CachedTokens,
		},
	}
}

func chatToolCalls(tools map[int]*toolState) []any {
	keys := make([]int, 0, len(tools))
	for k := range tools {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	out := make([]any, 0, len(keys))
	for _, k := range keys {
		t := tools[k]
		id := t.ID
		if id == "" {
			id = newID("call_")
		}
		out = append(out, map[string]any{
			"id":   id,
			"type": "function",
			"function": map[string]any{
				"name":      t.Name,
				"arguments": t.Args,
			},
		})
	}
	return out
}
