package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestMessagesCompatibleCanonicalizesToolHistoryAndWorkspace(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "native-id", DisplayName: "Native Model", Raw: map[string]any{"key": "native-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Native Model",
		"max_tokens":512,
		"system":[{"type":"text","text":"You are Claude Code.\n<env>\nWorking directory: C:\\work\\repo\nPlatform: win32\n</env>","cache_control":{"type":"ephemeral"}}],
		"tools":[{"name":"Read","description":"Read a file by absolute path","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}],
		"messages":[
			{"role":"user","content":[{"type":"text","text":"Inspect the project"}]},
			{"role":"assistant","content":[{"type":"text","text":"I will read it."},{"type":"tool_use","id":"call_1","name":"Read","input":{"file_path":"C:\\work\\repo\\go.mod"}}]},
			{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"module example"}],"is_error":false},{"type":"text","text":"Continue"}]}
		]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !backend.last.ClaudeToolCompatibility {
		t.Fatal("Anthropic Messages must opt into Claude Code tool compatibility")
	}

	if !strings.Contains(backend.last.System, "Working directory: C:\\work\\repo") {
		t.Fatalf("original workspace missing from system: %q", backend.last.System)
	}
	if !strings.Contains(backend.last.System, "[qoder-proxy workspace]") || !strings.Contains(backend.last.System, "active project root") {
		t.Fatalf("workspace constraint missing from system: %q", backend.last.System)
	}
	if len(backend.last.Messages) != 4 {
		t.Fatalf("messages=%#v", backend.last.Messages)
	}

	assistant := backend.last.Messages[1]
	if assistant["role"] != "assistant" || assistant["content"] != "I will read it." {
		t.Fatalf("assistant=%#v", assistant)
	}
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls=%#v", assistant["tool_calls"])
	}
	call, ok := calls[0].(map[string]any)
	if !ok || call["id"] != "call_1" || call["type"] != "function" {
		t.Fatalf("tool call=%#v", calls[0])
	}
	fn, ok := call["function"].(map[string]any)
	if !ok || fn["name"] != "Read" || fn["arguments"] != `{"file_path":"C:\\work\\repo\\go.mod"}` {
		t.Fatalf("tool function=%#v", call["function"])
	}

	toolResult := backend.last.Messages[2]
	if toolResult["role"] != "tool" || toolResult["tool_call_id"] != "call_1" || toolResult["content"] != "module example" {
		t.Fatalf("tool result=%#v", toolResult)
	}
	user := backend.last.Messages[3]
	if user["role"] != "user" || user["content"] != "Continue" {
		t.Fatalf("follow-up user=%#v", user)
	}
	if backend.last.LastUserText != "Continue" {
		t.Fatalf("last user text=%q", backend.last.LastUserText)
	}
}

func TestMessagesCompatibleStreamsQoderToolCallAsAnthropicToolUse(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "stream-tool-id", DisplayName: "Stream Tool", Raw: map[string]any{"key": "stream-tool-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"/repo/README.md\"}"}}]},"finish_reason":"tool_calls"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Stream Tool",
		"max_tokens":256,
		"stream":true,
		"tools":[{"name":"Read","description":"Read a file","input_schema":{"type":"object","properties":{"file_path":{"type":"string"}},"required":["file_path"]}}],
		"messages":[{"role":"user","content":"Inspect the README"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		"event: content_block_start",
		`"type":"tool_use"`,
		`"id":"call_1"`,
		`"name":"Read"`,
		"event: content_block_delta",
		`"type":"input_json_delta"`,
		`"stop_reason":"tool_use"`,
		"event: message_stop",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in stream:\n%s", want, body)
		}
	}
}

func TestMessagesCompatibleFindsWorkspaceInToolDescription(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "tool-cwd-id", DisplayName: "Tool CWD", Raw: map[string]any{"key": "tool-cwd-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Tool CWD","max_tokens":128,
		"system":"Follow the coding-agent instructions.",
		"tools":[{"name":"Read","description":"Read files for the active project.\nCurrent working directory: /srv/repos/app","input_schema":{"type":"object"}}],
		"messages":[{"role":"user","content":"Read the project README"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(backend.last.System, "Current working directory: /srv/repos/app") {
		t.Fatalf("workspace from tool description not bridged: %q", backend.last.System)
	}
}

func TestExtractWorkingDirectoryRejectsRelativeGuess(t *testing.T) {
	if got := extractWorkingDirectoryFromText("CWD: project"); got != "" {
		t.Fatalf("relative workspace should not be guessed, got %q", got)
	}
	if got := extractWorkingDirectoryFromText("Working directory: /repo/app"); got != "/repo/app" {
		t.Fatalf("unix cwd=%q", got)
	}
	if got := extractWorkingDirectoryFromText(`Current working directory: C:\repo\app`); got != `C:\repo\app` {
		t.Fatalf("windows cwd=%q", got)
	}
}
