package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

const proxyToolSearchMaxHops = 3

type proxyResponsesSearchCall struct {
	CallID string
	Alias  string
	Args   string
	Query  string
}

// chatResponsesWithToolDiscovery keeps synthetic tool_search calls inside the
// proxy. The client supplied the full registry, but only the proxy knows which
// leaves it deferred in the Qoder-facing projection; asking the client to run
// that synthetic search would therefore query an empty client-side registry.
//
// Each internal hop reuses the same request context, so server-side session
// binding remains stable. If discovery fails to converge after a few hops, the
// final request fails open by restoring the complete tool registry. Usage from
// hidden internal hops is accumulated and later reported with the final result.
func chatResponsesWithToolDiscovery(ctx context.Context, backend Backend, req ResponsesRequest, preq protocol.Request) (*http.Response, protocol.Request, protocol.Usage, error) {
	if !responsesNeedsProxyToolSearch(req.Tools) {
		resp, err := backend.Chat(ctx, preq)
		return resp, preq, protocol.Usage{}, err
	}

	projected := virtualizeResponsesTools(req.Tools, false)
	current := cloneProtocolRequestForDiscovery(preq)
	discovered := make([]map[string]any, 0)
	priorUsage := protocol.Usage{}

	for hop := 0; hop < proxyToolSearchMaxHops; hop++ {
		resp, err := backend.Chat(ctx, current)
		if err != nil {
			return nil, current, priorUsage, err
		}
		data, aggregate, err := inspectResponsesUpstream(resp)
		if err != nil {
			return nil, current, priorUsage, err
		}

		searchCalls, otherToolCalls := proxyResponsesSearchCalls(aggregate, current.ToolRoutes)
		if len(searchCalls) == 0 {
			restoreResponseBody(resp, data)
			return resp, current, priorUsage, nil
		}

		priorUsage = addProtocolUsage(priorUsage, aggregate.Usage)
		_ = resp.Body.Close()

		// A synthetic search is an internal control-plane action and cannot be
		// mixed safely with a real client-executed function call: the proxy has no
		// result for that real call yet. Retry once with the complete registry
		// instead of dropping or fabricating the real function call.
		if otherToolCalls > 0 {
			current.Tools, current.ToolRoutes = normalizeResponsesToolSets(req.Tools, nil)
			slog.Warn("openai responses proxy tool search fail-open",
				"reason", "mixed_search_and_function_calls",
				"search_calls", len(searchCalls),
				"function_calls", otherToolCalls,
				"full_tools_visible", len(current.Tools),
			)
			resp, err := backend.Chat(ctx, current)
			return resp, current, priorUsage, err
		}

		if text := strings.TrimSpace(aggregate.Text.String()); text != "" {
			current.Messages = append(current.Messages, map[string]any{"role": "assistant", "content": text})
		}
		for _, call := range searchCalls {
			matches := searchDeferredResponsesTools(req.Tools, call.Query, proxyToolSearchResultMax)
			matchedTools := responsesSearchResultTools(matches)
			discovered = append(discovered, matchedTools...)

			// Keep each internal tool call adjacent to its result. This mirrors the
			// normal assistant-tool message ordering expected by Qoder and avoids
			// exposing any synthetic search item to the downstream Responses client.
			current.Messages = append(current.Messages,
				canonicalToolCallMessage(call.CallID, call.Alias, call.Args),
				map[string]any{
					"role":         "tool",
					"tool_call_id": call.CallID,
					"content":      proxyResponsesSearchResult(call.Query, matches),
				},
			)
			slog.Info("openai responses proxy tool search",
				"hop", hop+1,
				"query", call.Query,
				"matches", len(matches),
				"matched_tools", responsesCandidateNames(matches, nil, false, proxyToolSearchResultMax),
			)
		}
		current.Tools, current.ToolRoutes = normalizeResponsesToolSets(projected, discovered)
	}

	// Correctness beats cost in the pathological case. A repeated search loop is
	// rare, but a hidden tool must never become permanently unreachable.
	current.Tools, current.ToolRoutes = normalizeResponsesToolSets(req.Tools, nil)
	slog.Warn("openai responses proxy tool search fail-open",
		"reason", "search_hop_limit",
		"hops", proxyToolSearchMaxHops,
		"full_tools_visible", len(current.Tools),
	)
	resp, err := backend.Chat(ctx, current)
	return resp, current, priorUsage, err
}

func cloneProtocolRequestForDiscovery(req protocol.Request) protocol.Request {
	clone := req
	clone.Messages = append([]map[string]any(nil), req.Messages...)
	clone.Tools = append([]any(nil), req.Tools...)
	clone.ToolRoutes = make(map[string]protocol.ToolRoute, len(req.ToolRoutes))
	for name, route := range req.ToolRoutes {
		clone.ToolRoutes[name] = route
	}
	return clone
}

func inspectResponsesUpstream(resp *http.Response) ([]byte, *aggregate, error) {
	if resp == nil || resp.Body == nil {
		return nil, nil, fmt.Errorf("empty Qoder response")
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		_ = resp.Body.Close()
		return nil, nil, fmt.Errorf("read Qoder response: %w", err)
	}
	_ = resp.Body.Close()

	copyResp := *resp
	copyResp.Body = io.NopCloser(bytes.NewReader(data))
	aggregate, err := collectEvents(&copyResp)
	if err != nil {
		return nil, nil, err
	}
	return data, aggregate, nil
}

func restoreResponseBody(resp *http.Response, data []byte) {
	resp.Body = io.NopCloser(bytes.NewReader(data))
}

func proxyResponsesSearchCalls(a *aggregate, routes map[string]protocol.ToolRoute) ([]proxyResponsesSearchCall, int) {
	if a == nil || len(a.Tools) == 0 {
		return nil, 0
	}
	keys := make([]int, 0, len(a.Tools))
	for index := range a.Tools {
		keys = append(keys, index)
	}
	sort.Ints(keys)

	calls := make([]proxyResponsesSearchCall, 0)
	otherToolCalls := 0
	for _, index := range keys {
		tool := a.Tools[index]
		if tool == nil {
			continue
		}
		route, ok := routes[tool.Name]
		if !ok || route.Kind != "tool_search" {
			otherToolCalls++
			continue
		}
		callID := strings.TrimSpace(tool.ID)
		if callID == "" {
			callID = newID("call_")
		}
		calls = append(calls, proxyResponsesSearchCall{
			CallID: callID,
			Alias:  tool.Name,
			Args:   tool.Args,
			Query:  proxyResponsesSearchQuery(tool.Args),
		})
	}
	return calls, otherToolCalls
}

func proxyResponsesSearchQuery(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "all available tools"
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) == nil {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) != "" {
				return strings.TrimSpace(typed)
			}
		case map[string]any:
			for _, key := range []string{"query", "search", "capability", "tool", "name"} {
				if candidate := strings.TrimSpace(asString(typed[key])); candidate != "" {
					return candidate
				}
			}
		}
	}
	return raw
}

func proxyResponsesSearchResult(query string, matches []responsesFunctionCandidate) string {
	tools := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		entry := map[string]any{"name": asString(match.tool["name"])}
		if match.namespace != "" {
			entry["namespace"] = match.namespace
		}
		tools = append(tools, entry)
	}
	payload := map[string]any{
		"query":         query,
		"count":         len(tools),
		"matched_tools": tools,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return `{"count":0,"matched_tools":[]}`
	}
	return string(encoded)
}

func addProtocolUsage(a, b protocol.Usage) protocol.Usage {
	return protocol.Usage{
		InputTokens:  a.InputTokens + b.InputTokens,
		OutputTokens: a.OutputTokens + b.OutputTokens,
		CachedTokens: a.CachedTokens + b.CachedTokens,
		TotalTokens:  a.TotalTokens + b.TotalTokens,
	}
}
