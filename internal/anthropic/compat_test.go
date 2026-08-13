package anthropic

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func TestMessagesCompatibleFoldsSystemAndDeveloperRoles(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "compat-id", DisplayName: "Compat Model", Raw: map[string]any{"key": "compat-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Compat Model",
		"max_tokens":128,
		"system":"top-level",
		"messages":[
			{"role":"system","content":"inline-system"},
			{"role":"developer","content":[{"type":"text","text":"inline-developer"}]},
			{"role":"user","content":"hello"}
		]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if backend.last.System != "top-level\n\ninline-system\n\ninline-developer" {
		t.Fatalf("system=%q", backend.last.System)
	}
	if len(backend.last.Messages) != 1 || backend.last.Messages[0]["role"] != "user" || backend.last.Messages[0]["content"] != "hello" {
		t.Fatalf("messages=%#v", backend.last.Messages)
	}
}

func TestMessagesCompatibleMapsOpenAIToolRoleToNativeToolResult(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "compat-tool-id", DisplayName: "Compat Tool", Raw: map[string]any{"key": "compat-tool-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Compat Tool",
		"max_tokens":128,
		"messages":[
			{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"lookup","input":{"q":"x"}}]},
			{"role":"tool","tool_call_id":"call_1","content":"result"},
			{"role":"user","content":"continue"}
		]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	foundToolResult := false
	for _, msg := range backend.last.Messages {
		if msg["role"] != "user" {
			continue
		}
		blocks, ok := msg["content"].([]any)
		if !ok || len(blocks) != 1 {
			continue
		}
		block, ok := blocks[0].(map[string]any)
		if ok && block["type"] == "tool_result" && block["tool_use_id"] == "call_1" && block["content"] == "result" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		t.Fatalf("tool role was not normalized to native tool_result: %#v", backend.last.Messages)
	}
}

func TestMessagesCompatibleStillRejectsUnknownRole(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "compat-id", DisplayName: "Compat Model", Raw: map[string]any{"key": "compat-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Compat Model","max_tokens":64,
		"messages":[{"role":"moderator","content":"hello"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessagesCompatible(rr, req, backend)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "role must be user or assistant") {
		t.Fatalf("unexpected error: %s", rr.Body.String())
	}
}
