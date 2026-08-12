package anthropic

import (
	"context"
	"encoding/json"
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
	err   error
	queue *qoder.QueueInfo
}

func (f *fakeBackend) ResolveModel(_ context.Context, public string) (qoder.Model, error) {
	if public != f.model.DisplayName {
		return qoder.Model{}, fmt.Errorf("not found")
	}
	return f.model, nil
}

func (f *fakeBackend) Chat(_ context.Context, req protocol.Request) (*http.Response, error) {
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(f.body)), Header: make(http.Header)}, nil
}

func (f *fakeBackend) ChatWithQueue(_ context.Context, req protocol.Request, onQueue func(qoder.QueueInfo) error) (*http.Response, error) {
	f.last = req
	if f.queue != nil && onQueue != nil {
		if err := onQueue(*f.queue); err != nil {
			return nil, err
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(f.body)), Header: make(http.Header)}, nil
}

func qoderFrame(inner string) string {
	return fmt.Sprintf("data: {\"statusCodeValue\":200,\"body\":%q}\n\n", inner)
}

func TestMessagesNonStreamText(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "model-internal", DisplayName: "Claude Display", Raw: map[string]any{"key": "model-internal"}},
		body: qoderFrame(`{"choices":[{"delta":{"content":"Hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
        "model":"Claude Display",
        "max_tokens":1024,
        "system":"Be useful",
        "messages":[{"role":"user","content":"Hi"}]
    }`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{`"type":"message"`, `"role":"assistant"`, `"model":"Claude Display"`, `"text":"Hello"`, `"stop_reason":"end_turn"`, `"input_tokens":4`, `"output_tokens":2`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if backend.last.ModelID != "model-internal" || backend.last.System != "Be useful" {
		t.Fatalf("normalized request=%#v", backend.last)
	}
}

func TestMessagesStreamTextUsesAnthropicSSE(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "stream-id", DisplayName: "Stream Model", Raw: map[string]any{"key": "stream-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"content":"Hel"},"finish_reason":null}]}`) +
			qoderFrame(`{"choices":[{"delta":{"content":"lo"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"Stream Model","max_tokens":256,"stream":true,"messages":[{"role":"user","content":"Hi"}]}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	body := rr.Body.String()
	for _, want := range []string{
		"event: message_start",
		"event: content_block_start",
		`"text":"Hel","type":"text_delta"`,
		`"text":"lo","type":"text_delta"`,
		"event: content_block_stop",
		"event: message_delta",
		`"stop_reason":"end_turn"`,
		"event: message_stop",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q:\n%s", want, body)
		}
	}
}

func TestMessagesToolUseAndToolResultMapping(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "tool-id", DisplayName: "Tool Model", Raw: map[string]any{"key": "tool-id"}},
		body:  qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_weather","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}]}`) + "data: [DONE]\n\n",
	}
	reqBody := `{
      "model":"Tool Model",
      "max_tokens":512,
      "tools":[{"name":"get_weather","description":"Weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}}}}],
      "messages":[
        {"role":"user","content":"Weather?"},
        {"role":"assistant","content":[{"type":"tool_use","id":"old_call","name":"get_weather","input":{"city":"Osaka"}}]},
        {"role":"user","content":[{"type":"tool_result","tool_use_id":"old_call","content":"sunny"},{"type":"text","text":"Now Tokyo"}]}
      ]
    }`
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(reqBody))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	if rr.Code != 200 {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{`"type":"tool_use"`, `"id":"call_weather"`, `"name":"get_weather"`, `"city":"Tokyo"`, `"stop_reason":"tool_use"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if len(backend.last.Tools) != 1 {
		t.Fatalf("tools=%#v", backend.last.Tools)
	}
	foundToolResult := false
	for _, m := range backend.last.Messages {
		if m["role"] == "tool" && m["tool_call_id"] == "old_call" && m["content"] == "sunny" {
			foundToolResult = true
		}
	}
	if !foundToolResult {
		b, _ := json.Marshal(backend.last.Messages)
		t.Fatalf("tool_result not normalized: %s", b)
	}
}

func TestMessagesStreamQueueHeartbeatThenSuccess(t *testing.T) {
	q := qoder.QueueInfo{Code: "10605", QueueType: "slow", QueueCount: 99, RetryAfterSeconds: 30, WaitTime: 120, ServiceAvailable: true}
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "free-id", DisplayName: "Free Model", Raw: map[string]any{"key": "free-id"}},
		queue: &q,
		body:  qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"Free Model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	body := rr.Body.String()
	if !strings.Contains(body, ": qoder queued queue_type=slow queue_count=99 retry_after=30s wait_time=120s") {
		t.Fatalf("missing queue heartbeat: %s", body)
	}
	if !strings.Contains(body, `"text":"ok","type":"text_delta"`) || !strings.Contains(body, "event: message_stop") {
		t.Fatalf("missing success after queue: %s", body)
	}
}

func TestMessagesQuotaErrorReturnsAnthropic429(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "quota-id", DisplayName: "Quota Model", Raw: map[string]any{"key": "quota-id"}},
		err:   &qoder.UpstreamError{HTTPStatus: http.StatusTooManyRequests, QoderStatus: http.StatusForbidden, QoderCode: "112", PublicCode: "insufficient_quota", Type: "insufficient_quota", Message: "Qoder account has no available quota"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"Quota Model","max_tokens":64,"stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"type":"error"`) || !strings.Contains(body, `"type":"rate_limit_error"`) || strings.Contains(body, "event:") {
		t.Fatalf("unexpected anthropic quota response: %s", body)
	}
}

func TestMessagesStreamToolUseHasSequentialBlockLifecycle(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{UpstreamID: "tool-stream-id", DisplayName: "Tool Stream", Raw: map[string]any{"key": "tool-stream-id"}},
		body: qoderFrame(`{"choices":[{"delta":{"content":"Checking "},"finish_reason":null}]}`) +
			qoderFrame(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_one","function":{"name":"one","arguments":"{\\\"a\\\":1}"}},{"index":1,"id":"call_two","function":{"name":"two","arguments":"{\\\"b\\\":2}"}}]},"finish_reason":"tool_calls"}]}`) +
			"data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"Tool Stream","max_tokens":128,"stream":true,"messages":[{"role":"user","content":"go"}]}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	body := rr.Body.String()
	textStop := strings.Index(body, "event: content_block_stop\ndata: {\"index\":0")
	toolOneStart := strings.Index(body, `"id":"call_one"`)
	toolOneStopRel := strings.Index(body[toolOneStart:], "event: content_block_stop")
	toolOneStop := -1
	if toolOneStart >= 0 && toolOneStopRel >= 0 {
		toolOneStop = toolOneStart + toolOneStopRel
	}
	toolTwoStart := strings.Index(body, `"id":"call_two"`)
	if textStop < 0 || toolOneStart < 0 || toolOneStop < 0 || toolTwoStart < 0 {
		t.Fatalf("missing tool block lifecycle:\n%s", body)
	}
	if !(textStop < toolOneStart && toolOneStart < toolOneStop && toolOneStop < toolTwoStart) {
		t.Fatalf("content blocks must be emitted sequentially:\n%s", body)
	}
}

func TestMessagesOutputConfigEffortMapping(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{
			UpstreamID: "reason-id", DisplayName: "Reason Model",
			Raw: map[string]any{
				"key": "reason-id",
				"thinking_config": map[string]any{
					"disabled": map[string]any{},
					"enabled":  map[string]any{"efforts": map[string]any{"low": map[string]any{}, "high": map[string]any{}}},
				},
			},
		},
		body: qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Reason Model","max_tokens":64,"output_config":{"effort":"high"},
		"messages":[{"role":"user","content":"hi"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if backend.last.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort=%q", backend.last.ReasoningEffort)
	}
}

func TestMessagesThinkingDisabledMapsToNone(t *testing.T) {
	backend := &fakeBackend{
		model: qoder.Model{
			UpstreamID: "reason-id", DisplayName: "Reason Model",
			Raw: map[string]any{
				"key": "reason-id",
				"thinking_config": map[string]any{
					"disabled": map[string]any{},
					"enabled":  map[string]any{"efforts": map[string]any{"high": map[string]any{}}},
				},
			},
		},
		body: qoderFrame(`{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`) + "data: [DONE]\n\n",
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{
		"model":"Reason Model","max_tokens":64,"thinking":{"type":"disabled"},
		"messages":[{"role":"user","content":"hi"}]
	}`))
	rr := httptest.NewRecorder()
	HandleMessages(rr, req, backend)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if backend.last.ReasoningEffort != "none" {
		t.Fatalf("reasoning effort=%q", backend.last.ReasoningEffort)
	}
}
