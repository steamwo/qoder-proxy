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

func TestMessagesCompatibleMapsOpenAIToolRole(t *testing.T) {
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
	foundTool := false
	for _, msg := range backend.last.Messages {
		if msg["role"] == "tool" && msg["tool_call_id"] == "call_1" && msg["content"] == "result" {
			foundTool = true
		}
	}
	if !foundTool {
		t.Fatalf("tool role was not normalized: %#v", backend.last.Messages)
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
