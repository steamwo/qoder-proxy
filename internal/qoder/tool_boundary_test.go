package qoder

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestGuardToolResponsePreservesAdvertisedTool(t *testing.T) {
	resp := toolBoundaryResponse(qoderBoundaryFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_read","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"/repo/README.md\"}"}}]},"finish_reason":"tool_calls"}]}`))
	GuardToolResponse(resp, []any{functionTool("Read"), functionTool("ToolSearch")})

	var names []string
	var args []string
	if err := ParseStream(resp.Body, func(ev protocol.Event) error {
		if ev.Kind == protocol.EventToolDelta {
			names = append(names, ev.ToolName)
			args = append(args, ev.ToolArguments)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Read" {
		t.Fatalf("tool names=%#v", names)
	}
	if len(args) != 1 || !strings.Contains(args[0], "README.md") {
		t.Fatalf("tool args=%#v", args)
	}
}

func TestGuardToolResponseBridgesUnknownToolToToolSearch(t *testing.T) {
	resp := toolBoundaryResponse(qoderBoundaryFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bash","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"go test ./...\"}"}}]},"finish_reason":"tool_calls"}]}`))
	GuardToolResponse(resp, []any{functionTool("Read"), functionTool("ToolSearch")})

	var toolName string
	var toolArgs string
	var finish string
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
}

func TestGuardToolResponseDropsUnknownToolWithoutToolSearch(t *testing.T) {
	resp := toolBoundaryResponse(qoderBoundaryFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bash","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"pwd\"}"}}]},"finish_reason":"tool_calls"}]}`))
	GuardToolResponse(resp, []any{functionTool("Read")})

	toolEvents := 0
	finish := ""
	if err := ParseStream(resp.Body, func(ev protocol.Event) error {
		if ev.Kind == protocol.EventToolDelta {
			toolEvents++
		}
		if ev.Kind == protocol.EventFinish {
			finish = ev.FinishReason
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if toolEvents != 0 {
		t.Fatalf("tool events=%d", toolEvents)
	}
	if finish != "stop" {
		t.Fatalf("finish=%q", finish)
	}
}

func TestGuardToolResponseDropsOriginalArgumentFragmentsAfterBridge(t *testing.T) {
	body := qoderBoundaryFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_bash","type":"function","function":{"name":"Bash","arguments":"{\"command\":\"go "}}]},"finish_reason":null}]}`) +
		qoderBoundaryFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"test ./...\"}"}}]},"finish_reason":"tool_calls"}]}`)
	resp := toolBoundaryResponse(body)
	GuardToolResponse(resp, []any{functionTool("ToolSearch")})

	var args string
	if err := ParseStream(resp.Body, func(ev protocol.Event) error {
		if ev.Kind == protocol.EventToolDelta {
			args += ev.ToolArguments
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if args != `{"query":"select:Bash"}` {
		t.Fatalf("bridged args contaminated by Qoder args: %q", args)
	}
}

func toolBoundaryResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream;charset=UTF-8"}},
		Body:       io.NopCloser(strings.NewReader(body + "data: [DONE]\n\n")),
	}
}

func qoderBoundaryFrame(inner string) string {
	return fmt.Sprintf("data: {\"statusCodeValue\":200,\"body\":%q}\n\n", inner)
}

func functionTool(name string) any {
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":       name,
			"parameters": map[string]any{"type": "object"},
		},
	}
}
