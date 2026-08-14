package qoder

import (
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestParseStreamPreservesFullMessagePrefixBeforeDeltas(t *testing.T) {
	stream := outer(`{"choices":[{"message":{"role":"assistant","content":"prefix "},"finish_reason":null}]}`) +
		outer(`{"choices":[{"delta":{"content":"suffix"},"finish_reason":null}]}`) +
		outer(`{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18}}`) +
		"data: [DONE]\n\n"

	var text strings.Builder
	var finish string
	var usage protocol.Usage
	if err := ParseStream(strings.NewReader(stream), func(ev protocol.Event) error {
		switch ev.Kind {
		case protocol.EventTextDelta:
			text.WriteString(ev.Text)
		case protocol.EventFinish:
			finish = ev.FinishReason
		case protocol.EventUsage:
			usage = ev.Usage
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if text.String() != "prefix suffix" {
		t.Fatalf("text=%q", text.String())
	}
	if finish != "stop" {
		t.Fatalf("finish=%q", finish)
	}
	if usage.OutputTokens != 8 {
		t.Fatalf("usage=%#v", usage)
	}
}

func TestParseStreamDoesNotDuplicateMessageSnapshotWhenDeltaExists(t *testing.T) {
	stream := outer(`{"choices":[{"delta":{"content":"tail"},"message":{"role":"assistant","content":"full snapshot that must not be replayed"},"finish_reason":"stop"}]}`) +
		"data: [DONE]\n\n"

	var text strings.Builder
	if err := ParseStream(strings.NewReader(stream), func(ev protocol.Event) error {
		if ev.Kind == protocol.EventTextDelta {
			text.WriteString(ev.Text)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if text.String() != "tail" {
		t.Fatalf("text=%q", text.String())
	}
}

func TestParseStreamAcceptsFullMessageToolCalls(t *testing.T) {
	stream := outer(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"index":0,"id":"call_read","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"README.md\"}"}}]},"finish_reason":"tool_calls"}]}`) +
		"data: [DONE]\n\n"

	var toolName, toolArgs, finish string
	if err := ParseStream(strings.NewReader(stream), func(ev protocol.Event) error {
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
	if toolName != "Read" || !strings.Contains(toolArgs, "README.md") {
		t.Fatalf("tool=%q args=%q", toolName, toolArgs)
	}
	if finish != "tool_calls" {
		t.Fatalf("finish=%q", finish)
	}
}
