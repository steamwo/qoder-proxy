package qoder

import (
	"fmt"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func outer(inner string) string {
	return fmt.Sprintf("data: {\"statusCodeValue\":200,\"body\":%q}\n\n", inner)
}

func TestParseStreamChatChunks(t *testing.T) {
	stream := outer(`{"choices":[{"delta":{"content":"Hel"},"finish_reason":null}]}`) +
		outer(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"weather","arguments":"{\\\"city\\\":"}}]},"finish_reason":null}]}`) +
		outer(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\\\"Tokyo\\\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`) +
		"data: [DONE]\n\n"

	var events []protocol.Event
	if err := ParseStream(strings.NewReader(stream), func(ev protocol.Event) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 5 {
		t.Fatalf("events=%d: %#v", len(events), events)
	}
	if events[0].Kind != protocol.EventTextDelta || events[0].Text != "Hel" {
		t.Fatalf("text event=%#v", events[0])
	}
	if events[1].Kind != protocol.EventToolDelta || events[1].ToolID != "call_1" || events[1].ToolName != "weather" {
		t.Fatalf("tool event=%#v", events[1])
	}
	if events[2].Kind != protocol.EventUsage || events[2].Usage.TotalTokens != 12 {
		t.Fatalf("usage event=%#v", events[2])
	}
	if events[3].Kind != protocol.EventToolDelta || events[3].ToolArguments == "" {
		t.Fatalf("tool args event=%#v", events[3])
	}
	if events[4].Kind != protocol.EventFinish || events[4].FinishReason != "tool_calls" {
		t.Fatalf("finish event=%#v", events[4])
	}
}

func TestParseStreamNestedLLMModelResult(t *testing.T) {
	stream := outer(`{"llm_model_result":{"choices":[{"delta":{"content":"Nested"},"finish_reason":null}]}}`) +
		outer(`{"data":"{\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}"}`) +
		"data: [DONE]\n\n"

	var events []protocol.Event
	if err := ParseStream(strings.NewReader(stream), func(ev protocol.Event) error {
		events = append(events, ev)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events=%d: %#v", len(events), events)
	}
	if events[0].Kind != protocol.EventTextDelta || events[0].Text != "Nested" {
		t.Fatalf("text event=%#v", events[0])
	}
	if events[1].Kind != protocol.EventFinish || events[1].FinishReason != "stop" {
		t.Fatalf("finish event=%#v", events[1])
	}
}

func TestRelayChatStreamMirrorsCFlareEnvelopeUnwrap(t *testing.T) {
	stream := outer(`{"id":"chatcmpl-upstream","model":"qmodel_hidden","choices":[{"delta":{"content":"Hello"},"finish_reason":null}]}`) +
		outer(`{"id":"chatcmpl-upstream","model":"qmodel_hidden","choices":[{"delta":{},"finish_reason":"stop"}]}`) +
		"data: [DONE]\n\n"

	var got []string
	if err := RelayChatStream(strings.NewReader(stream), "Qwen3.8-Max", func(data string) error {
		got = append(got, data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d frames: %#v", len(got), got)
	}
	if !strings.Contains(got[0], `"content":"Hello"`) {
		t.Fatalf("first frame=%s", got[0])
	}
	if !strings.Contains(got[0], `"model":"Qwen3.8-Max"`) {
		t.Fatalf("public model not rewritten: %s", got[0])
	}
	if got[2] != "[DONE]" {
		t.Fatalf("last frame=%q", got[2])
	}
}

func TestRelayChatStreamAcceptsSingleNewlineFrames(t *testing.T) {
	// Some Qoder edge responses terminate each complete data line with a single
	// newline rather than the SSE-standard blank line. The proxy must not wait
	// for EOF/client cancellation before forwarding these chunks.
	stream := "data: {\"statusCodeValue\":200,\"body\":\"{\\\"model\\\":\\\"qmodel_hidden\\\",\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"Hello\\\"},\\\"finish_reason\\\":null}]}\"}\n" +
		"data: {\"statusCodeValue\":200,\"body\":\"{\\\"model\\\":\\\"qmodel_hidden\\\",\\\"choices\\\":[{\\\"delta\\\":{},\\\"finish_reason\\\":\\\"stop\\\"}]}\"}\n" +
		"data: [DONE]\n"

	var got []string
	if err := RelayChatStream(strings.NewReader(stream), "Qwen3.8-Max", func(data string) error {
		got = append(got, data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d frames: %#v", len(got), got)
	}
	if !strings.Contains(got[0], `"content":"Hello"`) {
		t.Fatalf("first frame=%s", got[0])
	}
	if got[2] != "[DONE]" {
		t.Fatalf("last frame=%q", got[2])
	}
}

func TestRelayChatStreamStopsImmediatelyOnBusinessError(t *testing.T) {
	stream := "data: {\"statusCodeValue\":403,\"body\":\"{\\\"code\\\":112,\\\"message\\\":\\\"quota or permission denied\\\"}\"}\n" +
		"data: {\"statusCodeValue\":200,\"body\":\"{\\\"choices\\\":[{\\\"delta\\\":{\\\"content\\\":\\\"SHOULD_NOT_REACH\\\"}}]}\"}\n"
	var got []string
	err := RelayChatStream(strings.NewReader(stream), "Qwen3.8-Max", func(data string) error {
		got = append(got, data)
		return nil
	})
	if err == nil {
		t.Fatal("expected business error")
	}
	if len(got) != 1 {
		t.Fatalf("got %d frames: %#v", len(got), got)
	}
	if !strings.Contains(got[0], `"code":"insufficient_quota"`) || !strings.Contains(got[0], `no available quota`) {
		t.Fatalf("unexpected error frame: %s", got[0])
	}
	if strings.Contains(got[0], "SHOULD_NOT_REACH") {
		t.Fatalf("relay did not stop on business error: %s", got[0])
	}
}
