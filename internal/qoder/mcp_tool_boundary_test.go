package qoder

import (
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestGuardToolResponseBridgesDeferredMCPToolToClaudeToolSearch(t *testing.T) {
	const toolName = "mcp__codebase-memory-mcp__search_graph"
	resp := toolBoundaryResponse(qoderBoundaryFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_graph","type":"function","function":{"name":"` + toolName + `","arguments":"{\"query\":\"symbols\"}"}}]},"finish_reason":"tool_calls"}]}`))

	// This models Claude Code's deferred-tool state: ToolSearch is loaded in the
	// current request, while the MCP schema itself has not been loaded yet.
	GuardToolResponse(resp, []any{functionTool("Read"), functionTool("ToolSearch")})

	var gotName string
	var gotArgs string
	var finish string
	if err := ParseStream(resp.Body, func(ev protocol.Event) error {
		switch ev.Kind {
		case protocol.EventToolDelta:
			gotName = ev.ToolName
			gotArgs += ev.ToolArguments
		case protocol.EventFinish:
			finish = ev.FinishReason
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if gotName != "ToolSearch" {
		t.Fatalf("tool name=%q", gotName)
	}
	if gotArgs != `{"query":"select:`+toolName+`"}` {
		t.Fatalf("tool args=%q", gotArgs)
	}
	if finish != "tool_calls" {
		t.Fatalf("finish=%q", finish)
	}
}
