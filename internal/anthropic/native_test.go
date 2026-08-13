package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestMessagesCompatiblePreservesNativeToolHistoryAndWorkspace(t *testing.T) {
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

	if !strings.Contains(backend.last.System, "Working directory: C:\\work\\repo") {
		t.Fatalf("original workspace missing from system: %q", backend.last.System)
	}
	if !strings.Contains(backend.last.System, "[qoder-proxy workspace]") || !strings.Contains(backend.last.System, "active project root") {
		t.Fatalf("workspace constraint missing from system: %q", backend.last.System)
	}
	if len(backend.last.Messages) != 3 {
		t.Fatalf("messages=%#v", backend.last.Messages)
	}

	assistant := backend.last.Messages[1]
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant=%#v", assistant)
	}
	if _, exists := assistant["tool_calls"]; exists {
		t.Fatalf("Anthropic tool history must not be converted to OpenAI tool_calls: %#v", assistant)
	}
	assistantBlocks, ok := assistant["content"].([]any)
	if !ok || len(assistantBlocks) != 2 {
		t.Fatalf("assistant content=%#v", assistant["content"])
	}
	toolUse, ok := assistantBlocks[1].(map[string]any)
	if !ok || toolUse["type"] != "tool_use" || toolUse["id"] != "call_1" || toolUse["name"] != "Read" {
		t.Fatalf("tool_use=%#v", assistantBlocks[1])
	}

	user := backend.last.Messages[2]
	if user["role"] != "user" {
		t.Fatalf("user=%#v", user)
	}
	userBlocks, ok := user["content"].([]any)
	if !ok || len(userBlocks) != 2 {
		t.Fatalf("user content=%#v", user["content"])
	}
	result, ok := userBlocks[0].(map[string]any)
	if !ok || result["type"] != "tool_result" || result["tool_use_id"] != "call_1" || result["is_error"] != false {
		t.Fatalf("tool_result=%#v", userBlocks[0])
	}
	if backend.last.LastUserText != "Continue" {
		t.Fatalf("last user text=%q", backend.last.LastUserText)
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
