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

func TestMessagesCompatibleCanonicalizesOpenAIToolRoleForQoder(t *testing.T) {
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
	if len(backend.last.Messages) != 3 {
		t.Fatalf("messages=%#v", backend.last.Messages)
	}
	assistant := backend.last.Messages[0]
	if assistant["role"] != "assistant" {
		t.Fatalf("assistant=%#v", assistant)
	}
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls=%#v", assistant["tool_calls"])
	}
	toolResult := backend.last.Messages[1]
	if toolResult["role"] != "tool" || toolResult["tool_call_id"] != "call_1" || toolResult["content"] != "result" {
		t.Fatalf("tool role was not canonicalized for Qoder: %#v", backend.last.Messages)
	}
	if backend.last.Messages[2]["role"] != "user" || backend.last.Messages[2]["content"] != "continue" {
		t.Fatalf("follow-up user=%#v", backend.last.Messages[2])
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
