package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/protocol"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChatSendsEmptyToolsArrayWhenOmitted(t *testing.T) {
	var body map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel:  "Display",
		ModelID:      "model-id",
		ModelConfig:  map[string]any{"key": "model-id", "max_output_tokens": 1024},
		Messages:     []map[string]any{{"role": "user", "content": "hello"}},
		LastUserText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	tools, ok := body["tools"].([]any)
	if !ok {
		t.Fatalf("tools must be JSON array, got %#v", body["tools"])
	}
	if len(tools) != 0 {
		t.Fatalf("tools=%#v", tools)
	}
}

func TestChatWithQueueRetries10605AndReplaysSuccess(t *testing.T) {
	queuePayload, _ := json.Marshal(map[string]any{
		"isQueued": true, "modelKey": "model-id", "queueCount": 42,
		"queueType": "slow", "retryAfterSeconds": 30, "serviceAvailable": true, "waitTime": 90,
	})
	queueMiddle, _ := json.Marshal(map[string]any{"code": "10605", "message": string(queuePayload)})
	queueBody, _ := json.Marshal(map[string]any{"code": 403, "message": string(queueMiddle)})
	queueEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 403, "body": string(queueBody)})
	successInner := `{"model":"model-id","choices":[{"delta":{"content":"ok"},"finish_reason":null}]}`
	successEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 200, "body": successInner})

	calls := 0
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		body := "data: " + string(successEnvelope) + "\n\n"
		if calls == 1 {
			body = "data: " + string(queueEnvelope) + "\n\n"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	c.QueueRetry = QueueRetryPolicy{MaxRetries: 2, MaxWait: time.Minute}
	var waited time.Duration
	c.queueWait = func(ctx context.Context, d time.Duration) error { waited += d; return nil }
	var queued []QueueInfo
	resp, err := c.ChatWithQueue(context.Background(), protocol.Request{
		PublicModel: "Display", ModelID: "model-id", ModelConfig: map[string]any{"key": "model-id", "max_output_tokens": 1024},
		Messages: []map[string]any{{"role": "user", "content": "hello"}}, LastUserText: "hello",
	}, func(info QueueInfo) error { queued = append(queued, info); return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if calls != 2 {
		t.Fatalf("calls=%d", calls)
	}
	if len(queued) != 1 || queued[0].Code != "10605" || queued[0].QueueCount != 42 || queued[0].RetryAfterSeconds != 30 {
		t.Fatalf("queued=%#v", queued)
	}
	if waited != 30*time.Second {
		t.Fatalf("waited=%s", waited)
	}
	if !strings.Contains(string(got), `\"content\":\"ok\"`) {
		t.Fatalf("successful first frame not replayed: %s", got)
	}
}

func TestChatWithQueueReturnsTypedInsufficientQuotaBeforeStreaming(t *testing.T) {
	quotaBody, _ := json.Marshal(map[string]any{"code": 112, "message": `{"pricingUrl":"https://qoder.com/pricing?client=qoder"}`})
	quotaEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 403, "body": string(quotaBody)})
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: " + string(quotaEnvelope) + "\n\n")),
			Request:    r,
		}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	resp, err := c.ChatWithQueue(context.Background(), protocol.Request{
		PublicModel: "Display", ModelID: "model-id", ModelConfig: map[string]any{"key": "model-id", "max_output_tokens": 1024},
		Messages: []map[string]any{{"role": "user", "content": "hello"}}, LastUserText: "hello",
	}, nil)
	if resp != nil {
		resp.Body.Close()
		t.Fatal("expected no response for quota rejection")
	}
	var upstreamErr *UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("expected UpstreamError, got %T: %v", err, err)
	}
	if upstreamErr.HTTPStatus != http.StatusTooManyRequests || upstreamErr.PublicCode != "insufficient_quota" || upstreamErr.Type != "insufficient_quota" {
		t.Fatalf("unexpected upstream error: %#v", upstreamErr)
	}
	if !strings.Contains(upstreamErr.Message, "https://qoder.com/pricing?client=qoder") {
		t.Fatalf("message=%q", upstreamErr.Message)
	}
}

func TestChatSendsReasoningEffortParameter(t *testing.T) {
	var body map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &body); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel: "Reason Model", ModelID: "reason-id",
		ModelConfig: map[string]any{
			"key": "reason-id", "max_output_tokens": 1024, "is_reasoning": true,
			"thinking_config": map[string]any{
				"disabled": map[string]any{},
				"enabled":  map[string]any{"efforts": map[string]any{"high": map[string]any{}}},
			},
		},
		ReasoningEffort: "high",
		Messages:        []map[string]any{{"role": "user", "content": "hello"}}, LastUserText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	params, ok := body["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters=%#v", body["parameters"])
	}
	if params["reasoningEffort"] != "high" {
		t.Fatalf("parameters=%#v", params)
	}
	extra := body["chat_context"].(map[string]any)["extra"].(map[string]any)
	modelConfig := extra["modelConfig"].(map[string]any)
	if modelConfig["is_reasoning"] != true {
		t.Fatalf("chat_context modelConfig=%#v", modelConfig)
	}
}
