package qoder

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
)

// GuardToolResponse keeps Qoder's server-side agent tool namespace from leaking
// into clients such as Claude Code. The request's function schemas remain the
// source of truth. Unknown Qoder tools are redirected through Claude Code's
// ToolSearch when it is advertised, so deferred tools can be loaded through the
// normal client tool loop without issuing a hidden extra model request.
func GuardToolResponse(resp *http.Response, tools []any) *http.Response {
	if resp == nil || resp.Body == nil || len(tools) == 0 {
		return resp
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		return resp
	}

	allowed := advertisedFunctionTools(tools)
	if len(allowed) == 0 {
		return resp
	}
	_, hasToolSearch := allowed["ToolSearch"]
	state := &toolBoundaryState{
		allowed:       allowed,
		hasToolSearch: hasToolSearch,
		redirected:    make(map[int]string),
		blocked:       make(map[int]string),
	}

	source := resp.Body
	reader, writer := io.Pipe()
	go func() {
		defer source.Close()
		err := readSSE(source, func(data string) error {
			rewritten, rewriteErr := state.rewriteEnvelope(data)
			if rewriteErr != nil {
				slog.Warn("qoder tool namespace rewrite failed", "error", rewriteErr)
				rewritten = data
			}
			_, writeErr := fmt.Fprintf(writer, "data: %s\n\n", rewritten)
			return writeErr
		})
		_ = writer.CloseWithError(err)
	}()

	resp.Body = &toolBoundaryBody{PipeReader: reader, source: source}
	resp.ContentLength = -1
	resp.Header.Del("Content-Length")
	return resp
}

type toolBoundaryBody struct {
	*io.PipeReader
	source io.Closer
}

func (b *toolBoundaryBody) Close() error {
	_ = b.source.Close()
	return b.PipeReader.Close()
}

type toolBoundaryState struct {
	allowed       map[string]struct{}
	hasToolSearch bool
	redirected    map[int]string
	blocked       map[int]string
	forwardedTool bool
}

func (s *toolBoundaryState) rewriteEnvelope(data string) (string, error) {
	if strings.TrimSpace(data) == "[DONE]" {
		return data, nil
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return data, nil
	}

	body, exists := envelope["body"]
	if !exists || body == nil {
		return data, nil
	}

	var raw []byte
	bodyWasString := false
	switch value := body.(type) {
	case string:
		bodyWasString = true
		raw = []byte(value)
	case map[string]any, []any:
		encoded, err := json.Marshal(value)
		if err != nil {
			return data, err
		}
		raw = encoded
	default:
		return data, nil
	}

	rewritten, changed, err := s.rewriteInner(raw)
	if err != nil || !changed {
		return data, err
	}
	if bodyWasString {
		envelope["body"] = string(rewritten)
	} else {
		var decoded any
		if err := json.Unmarshal(rewritten, &decoded); err != nil {
			return data, err
		}
		envelope["body"] = decoded
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return data, err
	}
	return string(encoded), nil
}

func (s *toolBoundaryState) rewriteInner(raw []byte) ([]byte, bool, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return raw, false, nil
	}
	choices, ok := obj["choices"].([]any)
	if !ok {
		return raw, false, nil
	}

	changed := false
	for _, rawChoice := range choices {
		choice, ok := rawChoice.(map[string]any)
		if !ok {
			continue
		}
		payload, _ := choicePayload(choice)
		if payload != nil {
			if calls, ok := payload["tool_calls"].([]any); ok {
				kept := make([]any, 0, len(calls))
				for _, rawCall := range calls {
					call, ok := rawCall.(map[string]any)
					if !ok {
						kept = append(kept, rawCall)
						continue
					}
					idx := numberAsInt(call["index"])
					if _, redirected := s.redirected[idx]; redirected {
						// The first fragment was replaced with a complete ToolSearch input;
						// discard later argument fragments from the original Qoder tool.
						changed = true
						continue
					}
					if _, blocked := s.blocked[idx]; blocked {
						changed = true
						continue
					}

					fn, _ := call["function"].(map[string]any)
					name := strings.TrimSpace(stringValue(fn["name"]))
					if name == "" {
						// Continuation fragment for a previously accepted tool call.
						kept = append(kept, call)
						continue
					}
					if _, advertised := s.allowed[name]; advertised {
						s.forwardedTool = true
						kept = append(kept, call)
						continue
					}

					changed = true
					if s.hasToolSearch {
						s.redirected[idx] = name
						fn["name"] = "ToolSearch"
						query, _ := json.Marshal(map[string]any{"query": "select:" + name})
						fn["arguments"] = string(query)
						s.forwardedTool = true
						kept = append(kept, call)
						slog.Warn("qoder tool bridged to client ToolSearch",
							"qoder_tool", name,
							"advertised_tools", len(s.allowed),
						)
						continue
					}

					s.blocked[idx] = name
					slog.Warn("qoder tool blocked by client namespace",
						"qoder_tool", name,
						"advertised_tools", len(s.allowed),
					)
				}
				if len(kept) == 0 {
					delete(payload, "tool_calls")
				} else {
					payload["tool_calls"] = kept
				}
			}
		}

		if reason := stringValue(choice["finish_reason"]); reason == "tool_calls" && !s.forwardedTool {
			choice["finish_reason"] = "stop"
			changed = true
		}
	}

	if !changed {
		return raw, false, nil
	}
	encoded, err := json.Marshal(obj)
	if err != nil {
		return raw, false, err
	}
	return encoded, true, nil
}

func advertisedFunctionTools(tools []any) map[string]struct{} {
	out := make(map[string]struct{}, len(tools))
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		name := strings.TrimSpace(stringValue(fn["name"]))
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out
}

func advertisedToolNames(tools []any) []string {
	set := advertisedFunctionTools(tools)
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
