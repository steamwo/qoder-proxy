package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type fakeBackend struct {
	model qoder.Model
	last  protocol.Request
	body  string
}

func (f *fakeBackend) ResolveModel(_ context.Context, public string) (qoder.Model, error) {
	if public != f.model.DisplayName {
		return qoder.Model{}, fmt.Errorf("not found")
	}
	return f.model, nil
}

func (f *fakeBackend) Chat(_ context.Context, req protocol.Request) (*http.Response, error) {
	f.last = req
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(f.body)), Header: make(http.Header)}, nil
}

func qoderFrame(inner string) string {
	return fmt.Sprintf("data: {\"statusCodeValue\":200,\"body\":%q}\n\n", inner)
}

func TestResponsesStreamUsesDisplayNameAndStandardEvents(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "anon-42", DisplayName: "Claude Display", Raw: map[string]any{"key": "anon-42"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"Claude Display","input":"hello","stream":true}`))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	body := rr.Body.String()
	for _, want := range []string{"event: response.created", "event: response.output_text.delta", `"delta":"Hi"`, "event: response.completed", `"model":"Claude Display"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q:\n%s", want, body)
		}
	}
	if backend.last.ModelID != "anon-42" || backend.last.PublicModel != "Claude Display" {
		t.Fatalf("backend mapping=%#v", backend.last)
	}
}

func TestChatNonStreamUsesDisplayName(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "real-id", DisplayName: "Pretty Model", Raw: map[string]any{"key": "real-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":1,"total_tokens":3}}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"Pretty Model","messages":[{"role":"user","content":"hi"}]}`))
	rr := httptest.NewRecorder()
	HandleChat(rr, req, backend)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"model":"Pretty Model"`) || !strings.Contains(rr.Body.String(), `"content":"Hello"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
	if backend.last.ModelID != "real-id" {
		t.Fatalf("upstream model=%q", backend.last.ModelID)
	}
}

func TestResponsesFunctionCallStream(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "tool-model-id", DisplayName: "Tool Model", Raw: map[string]any{"key": "tool-model-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\\\"city\\\":"}}]},"finish_reason":null}]}`) +
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\\\"Tokyo\\\"}"}}]},"finish_reason":"tool_calls"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"Tool Model",
		"stream":true,
		"input":"weather in Tokyo?",
		"tools":[{"type":"function","name":"get_weather","description":"weather","parameters":{"type":"object"}}]
	}`))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	body := rr.Body.String()
	for _, want := range []string{
		"event: response.output_item.added",
		`"type":"function_call"`,
		`"call_id":"call_weather"`,
		"event: response.function_call_arguments.delta",
		"event: response.function_call_arguments.done",
		"event: response.completed",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("response missing %q:\n%s", want, body)
		}
	}
}

type fakeQueueBackend struct {
	fakeBackend
	queue qoder.QueueInfo
}

func (f *fakeQueueBackend) ChatWithQueue(_ context.Context, req protocol.Request, onQueue func(qoder.QueueInfo) error) (*http.Response, error) {
	f.last = req
	if onQueue != nil {
		if err := onQueue(f.queue); err != nil {
			return nil, err
		}
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(f.body)), Header: make(http.Header)}, nil
}

func TestChatStreamKeepsConnectionAliveWhileQoderQueued(t *testing.T) {
	backend := &fakeQueueBackend{
		fakeBackend: fakeBackend{
			model: qoder.Model{UpstreamID: "free-id", DisplayName: "Free Model", Raw: map[string]any{"key": "free-id"}},
			body:  qoderFrame(`{"model":"free-id","choices":[{"delta":{"content":"Hello"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
		},
		queue: qoder.QueueInfo{Code: "10605", QueueType: "slow", QueueCount: 5742, RetryAfterSeconds: 30, WaitTime: 196, ServiceAvailable: true},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"Free Model","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rr := httptest.NewRecorder()
	HandleChat(rr, req, backend)
	body := rr.Body.String()
	if !strings.Contains(body, ": qoder queued queue_type=slow queue_count=5742 retry_after=30s wait_time=196s") {
		t.Fatalf("missing queue heartbeat: %s", body)
	}
	if !strings.Contains(body, `"content":"Hello"`) || !strings.Contains(body, "data: [DONE]") {
		t.Fatalf("missing successful stream after queue heartbeat: %s", body)
	}
}

type fakeQuotaBackend struct {
	fakeBackend
}

func (f *fakeQuotaBackend) ChatWithQueue(_ context.Context, req protocol.Request, _ func(qoder.QueueInfo) error) (*http.Response, error) {
	f.last = req
	return nil, &qoder.UpstreamError{
		HTTPStatus:  http.StatusTooManyRequests,
		QoderStatus: http.StatusForbidden,
		QoderCode:   "112",
		PublicCode:  "insufficient_quota",
		Type:        "insufficient_quota",
		Message:     "Qoder account has no available quota for this request. Pricing: https://qoder.com/pricing?client=qoder",
	}
}

func TestChatStreamQuotaErrorReturnsHTTP429BeforeSSEStarts(t *testing.T) {
	backend := &fakeQuotaBackend{fakeBackend: fakeBackend{
		model: qoder.Model{UpstreamID: "quota-id", DisplayName: "Quota Model", Raw: map[string]any{"key": "quota-id"}},
	}}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"Quota Model","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rr := httptest.NewRecorder()
	HandleChat(rr, req, backend)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type=%q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"code":"insufficient_quota"`) || !strings.Contains(body, `"type":"insufficient_quota"`) {
		t.Fatalf("body=%s", body)
	}
	if strings.Contains(body, "data:") {
		t.Fatalf("quota error must not start SSE: %s", body)
	}
}

func TestChatReasoningEffortRejectedWhenModelDoesNotAdvertiseIt(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{
			UpstreamID: "kimi-k3-id", DisplayName: "Kimi-K3",
			Raw: map[string]any{"key": "kimi-k3-id", "context_config": map[string]any{"200K": map[string]any{"token_count": 200000}}},
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{
		"model":"Kimi-K3","reasoning_effort":"high","messages":[{"role":"user","content":"hi"}]
	}`))
	rr := httptest.NewRecorder()
	HandleChat(rr, req, backend)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "does not support configurable reasoning effort") {
		t.Fatalf("body=%s", rr.Body.String())
	}
}

func TestResponsesReasoningEffortEchoAndForward(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{
			UpstreamID: "reason-id", DisplayName: "Reason Model",
			Raw: map[string]any{
				"key":             "reason-id",
				"thinking_config": map[string]any{"enabled": map[string]any{"efforts": map[string]any{"high": map[string]any{}}}},
			},
		},
		body: qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{
		"model":"Reason Model","input":"hi","reasoning":{"effort":"high"}
	}`))
	rr := httptest.NewRecorder()
	HandleResponses(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if backend.last.ReasoningEffort != "high" {
		t.Fatalf("forwarded effort=%q", backend.last.ReasoningEffort)
	}
	if !strings.Contains(rr.Body.String(), `"reasoning":{"effort":"high"`) {
		t.Fatalf("body=%s", rr.Body.String())
	}
}
