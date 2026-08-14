package qoder

import (
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestGuardToolResponseBridgesUnknownFullMessageToolToToolSearch(t *testing.T) {
	resp := toolBoundaryResponse(qoderBoundaryFrame(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"index":0,"id":"call_bash","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`))
	GuardToolResponse(resp, []any{functionTool("Read"), functionTool("ToolSearch")})

	var toolName, toolArgs, finish string
	if err := ParseStream(resp.Body, func(ev protocol.Event) error {
		switch ev.Kind {
		case protocol.EventToolDelta:
			toolName = ev.ToolName
			toolArgs += ev.ToolArguments
		case protocol.EventFinish:
			finish = ev.FinishReason
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if toolName != "ToolSearch" {
		t.Fatalf("tool name=%q", toolName)
	}
	if toolArgs != `{"query":"select:Bash"}` {
		t.Fatalf("tool args=%q", toolArgs)
	}
	if finish != "tool_calls" {
		t.Fatalf("finish=%q", finish)
	}
	if strings.Contains(toolArgs, "pwd") {
		t.Fatalf("original Qoder arguments leaked after bridge: %q", toolArgs)
	}
}
