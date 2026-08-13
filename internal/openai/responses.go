package openai

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func HandleResponses(w http.ResponseWriter, r *http.Request, backend Backend) {
	var req ResponsesRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	dec.UseNumber()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if req.Model == "" || len(req.Input) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "model and input are required")
		return
	}

	model, err := backend.ResolveModel(r.Context(), req.Model)
	if err != nil {
		writeError(w, http.StatusNotFound, "model_not_found", err.Error())
		return
	}
	preq, err := NormalizeResponses(req, model)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	resp, err := backend.Chat(r.Context(), preq)
	if err != nil {
		writeBackendError(w, err, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if req.Stream {
		streamResponses(w, resp, req, preq.ToolRoutes)
		return
	}
	a, err := collectEvents(resp)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_stream_error", err.Error())
		return
	}
	state := newResponseState(req, preq.ToolRoutes)
	state.applyAggregate(a)
	writeJSON(w, http.StatusOK, state.responseObject("completed"))
}

type responseTool struct {
	ID          string
	CallID      string
	Name        string
	Args        string
	OutputIndex int
	Route       protocol.ToolRoute
	Added       bool
}

type responseState struct {
	Req     ResponsesRequest
	ID      string
	Created int64
	Seq     int

	TextID      string
	TextIndex   int
	Text        string
	TextStarted bool

	Tools      map[int]*responseTool
	Routes     map[string]protocol.ToolRoute
	NextOutput int
	Usage      protocol.Usage
}

func newResponseState(req ResponsesRequest, routes map[string]protocol.ToolRoute) *responseState {
	return &responseState{
		Req:       req,
		ID:        newID("resp_"),
		Created:   time.Now().Unix(),
		TextIndex: -1,
		Tools:     map[int]*responseTool{},
		Routes:    routes,
	}
}

func (s *responseState) nextSeq() int {
	s.Seq++
	return s.Seq
}

func (s *responseState) ensureText() {
	if s.TextStarted {
		return
	}
	s.TextStarted = true
	s.TextID = newID("msg_")
	s.TextIndex = s.NextOutput
	s.NextOutput++
}

func (s *responseState) routeFor(name string) protocol.ToolRoute {
	if route, ok := s.Routes[name]; ok {
		return route
	}
	return protocol.ToolRoute{Kind: "function", Name: name}
}

func (s *responseState) ensureTool(index int, callID, name string) *responseTool {
	if t := s.Tools[index]; t != nil {
		if callID != "" {
			t.CallID = callID
		}
		if name != "" {
			t.Name = name
			t.Route = s.routeFor(name)
		}
		return t
	}
	t := &responseTool{
		ID:          newID("fc_"),
		CallID:      callID,
		Name:        name,
		OutputIndex: s.NextOutput,
		Route:       s.routeFor(name),
	}
	if t.CallID == "" {
		t.CallID = newID("call_")
	}
	s.NextOutput++
	s.Tools[index] = t
	return t
}

func (t *responseTool) clientRoute() protocol.ToolRoute {
	route := t.Route
	if route.Kind == "" {
		route.Kind = "function"
	}
	if route.Name == "" {
		route.Name = t.Name
	}
	return route
}

func (t *responseTool) clientItem(status string) map[string]any {
	route := t.clientRoute()
	if route.Kind == "tool_search" {
		return map[string]any{
			"id":        t.ID,
			"type":      "tool_search_call",
			"status":    status,
			"call_id":   t.CallID,
			"execution": "client",
			"arguments": parseResponseToolArguments(t.Args),
		}
	}
	item := map[string]any{
		"id":        t.ID,
		"type":      "function_call",
		"status":    status,
		"call_id":   t.CallID,
		"name":      route.Name,
		"arguments": t.Args,
	}
	if route.Namespace != "" {
		item["namespace"] = route.Namespace
	}
	return item
}

func parseResponseToolArguments(raw string) any {
	if raw == "" {
		return map[string]any{}
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		return value
	}
	return map[string]any{"query": raw}
}

func (s *responseState) orderedOutputs() []any {
	type pair struct {
		index int
		value any
	}
	pairs := make([]pair, 0, len(s.Tools)+1)
	if s.TextStarted {
		pairs = append(pairs, pair{s.TextIndex, map[string]any{
			"id":     s.TextID,
			"type":   "message",
			"status": "completed",
			"role":   "assistant",
			"content": []any{map[string]any{
				"type":        "output_text",
				"text":        s.Text,
				"annotations": []any{},
			}},
		}})
	}
	for _, t := range s.Tools {
		pairs = append(pairs, pair{t.OutputIndex, t.clientItem("completed")})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].index < pairs[j].index })
	out := make([]any, len(pairs))
	for i, p := range pairs {
		out[i] = p.value
	}
	return out
}

func (s *responseState) responseObject(status string) map[string]any {
	obj := map[string]any{
		"id":                   s.ID,
		"object":               "response",
		"created_at":           s.Created,
		"completed_at":         nil,
		"status":               status,
		"error":                nil,
		"incomplete_details":   nil,
		"instructions":         rawOrNil(s.Req.Instructions),
		"model":                s.Req.Model,
		"output":               s.orderedOutputs(),
		"parallel_tool_calls":  true,
		"previous_response_id": nil,
		"reasoning": map[string]any{
			"effort":  defaultValue(s.Req.Reasoning["effort"], nil),
			"summary": nil,
		},
		"store":       false,
		"temperature": valueOrDefaultFloat(s.Req.Temperature, 1),
		"text": map[string]any{
			"format": map[string]any{"type": "text"},
		},
		"tool_choice": defaultValue(s.Req.ToolChoice, "auto"),
		"tools":       s.Req.Tools,
		"top_p":       valueOrDefaultFloat(s.Req.TopP, 1),
		"truncation":  "disabled",
		"usage":       nil,
		"user":        nil,
		"metadata":    s.Req.Metadata,
	}
	if s.Req.MaxOutputTokens > 0 {
		obj["max_output_tokens"] = s.Req.MaxOutputTokens
	} else {
		obj["max_output_tokens"] = nil
	}
	if status == "completed" {
		obj["completed_at"] = time.Now().Unix()
		obj["usage"] = responsesUsage(s.Usage)
	}
	return obj
}

func (s *responseState) applyAggregate(a *aggregate) {
	s.Text = a.Text.String()
	if s.Text != "" {
		s.ensureText()
	}
	for idx, t := range a.Tools {
		rt := s.ensureTool(idx, t.ID, t.Name)
		rt.Args = t.Args
	}
	s.Usage = a.Usage
}

func streamResponses(w http.ResponseWriter, resp *http.Response, req ResponsesRequest, routes map[string]protocol.ToolRoute) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "HTTP streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	s := newResponseState(req, routes)
	meaningfulEvents := 0
	emit := func(typ string, payload map[string]any) {
		payload["type"] = typ
		payload["sequence_number"] = s.nextSeq()
		b, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", typ, b)
		flusher.Flush()
	}

	emit("response.created", map[string]any{"response": s.responseObject("in_progress")})
	emit("response.in_progress", map[string]any{"response": s.responseObject("in_progress")})

	err := qoder.ParseStream(resp.Body, func(ev protocol.Event) error {
		switch ev.Kind {
		case protocol.EventTextDelta:
			meaningfulEvents++
			if !s.TextStarted {
				s.ensureText()
				emit("response.output_item.added", map[string]any{
					"output_index": s.TextIndex,
					"item": map[string]any{
						"id":      s.TextID,
						"type":    "message",
						"status":  "in_progress",
						"role":    "assistant",
						"content": []any{},
					},
				})
				emit("response.content_part.added", map[string]any{
					"item_id":       s.TextID,
					"output_index":  s.TextIndex,
					"content_index": 0,
					"part": map[string]any{
						"type":        "output_text",
						"text":        "",
						"annotations": []any{},
					},
				})
			}
			s.Text += ev.Text
			emit("response.output_text.delta", map[string]any{
				"item_id":       s.TextID,
				"output_index":  s.TextIndex,
				"content_index": 0,
				"delta":         ev.Text,
				"logprobs":      []any{},
			})

		case protocol.EventToolDelta:
			meaningfulEvents++
			t := s.ensureTool(ev.ToolIndex, ev.ToolID, ev.ToolName)
			if ev.ToolName != "" {
				t.Name = ev.ToolName
				t.Route = s.routeFor(ev.ToolName)
			}
			if ev.ToolID != "" {
				t.CallID = ev.ToolID
			}
			if !t.Added && t.Name != "" {
				t.Added = true
				emit("response.output_item.added", map[string]any{
					"output_index": t.OutputIndex,
					"item":         t.clientItem("in_progress"),
				})
			}
			if ev.ToolArguments != "" {
				t.Args += ev.ToolArguments
				if t.clientRoute().Kind != "tool_search" {
					emit("response.function_call_arguments.delta", map[string]any{
						"item_id":      t.ID,
						"output_index": t.OutputIndex,
						"delta":        ev.ToolArguments,
					})
				}
			}

		case protocol.EventUsage:
			s.Usage = ev.Usage
		case protocol.EventError:
			emit("error", map[string]any{
				"code":    "QODER_STREAM_ERROR",
				"message": ev.Error,
				"param":   nil,
			})
			return fmt.Errorf("%s", ev.Error)
		}
		return nil
	})
	if err != nil {
		return
	}
	if meaningfulEvents == 0 {
		slog.Warn("qoder stream produced no responses output", "model", req.Model)
	}

	if s.TextStarted {
		part := map[string]any{
			"type":        "output_text",
			"text":        s.Text,
			"annotations": []any{},
		}
		emit("response.output_text.done", map[string]any{
			"item_id":       s.TextID,
			"output_index":  s.TextIndex,
			"content_index": 0,
			"text":          s.Text,
			"logprobs":      []any{},
		})
		emit("response.content_part.done", map[string]any{
			"item_id":       s.TextID,
			"output_index":  s.TextIndex,
			"content_index": 0,
			"part":          part,
		})
		emit("response.output_item.done", map[string]any{
			"output_index": s.TextIndex,
			"item": map[string]any{
				"id":      s.TextID,
				"type":    "message",
				"status":  "completed",
				"role":    "assistant",
				"content": []any{part},
			},
		})
	}

	keys := make([]int, 0, len(s.Tools))
	for k := range s.Tools {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		t := s.Tools[k]
		if !t.Added {
			t.Added = true
			emit("response.output_item.added", map[string]any{
				"output_index": t.OutputIndex,
				"item":         t.clientItem("in_progress"),
			})
		}
		if t.clientRoute().Kind != "tool_search" {
			route := t.clientRoute()
			done := map[string]any{
				"item_id":      t.ID,
				"output_index": t.OutputIndex,
				"name":         route.Name,
				"arguments":    t.Args,
			}
			if route.Namespace != "" {
				done["namespace"] = route.Namespace
			}
			emit("response.function_call_arguments.done", done)
		}
		emit("response.output_item.done", map[string]any{
			"output_index": t.OutputIndex,
			"item":         t.clientItem("completed"),
		})
	}

	emit("response.completed", map[string]any{
		"response": s.responseObject("completed"),
	})
}

func responsesUsage(u protocol.Usage) map[string]any {
	return map[string]any{
		"input_tokens": u.InputTokens,
		"input_tokens_details": map[string]any{
			"cached_tokens": u.CachedTokens,
		},
		"output_tokens": u.OutputTokens,
		"output_tokens_details": map[string]any{
			"reasoning_tokens": 0,
		},
		"total_tokens": u.TotalTokens,
	}
}

func rawOrNil(raw json.RawMessage) any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func defaultValue(v any, d any) any {
	if v == nil {
		return d
	}
	return v
}

func valueOrDefaultFloat(v *float64, d float64) float64 {
	if v == nil {
		return d
	}
	return *v
}
