package qoder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestChatUsesDistinctQoderSessionsAcrossRequests(t *testing.T) {
	var sessions []string
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(decodeQoderBodyForTest(t, b), &body); err != nil {
			t.Fatal(err)
		}
		session, _ := body["session_id"].(string)
		sessions = append(sessions, session)
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	req := protocol.Request{
		PublicModel: "Display", ModelID: "model-id",
		ModelConfig: map[string]any{"key": "model-id", "max_output_tokens": 1024},
		Messages:    []map[string]any{{"role": "user", "content": "hello"}}, LastUserText: "hello",
	}
	for i := 0; i < 2; i++ {
		resp, err := c.Chat(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if len(sessions) != 2 || sessions[0] == "" || sessions[1] == "" {
		t.Fatalf("sessions=%#v", sessions)
	}
	if sessions[0] == sessions[1] {
		t.Fatalf("separate API requests reused Qoder session %q", sessions[0])
	}
}

func TestChatWithQueueReusesQoderSessionAcrossRetries(t *testing.T) {
	queuePayload, _ := json.Marshal(map[string]any{
		"isQueued": true, "modelKey": "model-id", "queueCount": 1,
		"queueType": "slow", "retryAfterSeconds": 1, "serviceAvailable": true,
	})
	queueMiddle, _ := json.Marshal(map[string]any{"code": "10605", "message": string(queuePayload)})
	queueBody, _ := json.Marshal(map[string]any{"code": 403, "message": string(queueMiddle)})
	queueEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 403, "body": string(queueBody)})
	successEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 200, "body": `{"choices":[{"delta":{"content":"ok"}}]}`})

	var sessions []string
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]any
		if err := json.Unmarshal(decodeQoderBodyForTest(t, b), &payload); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, payload["session_id"].(string))
		body := "data: " + string(successEnvelope) + "\n\n"
		if len(sessions) == 1 {
			body = "data: " + string(queueEnvelope) + "\n\n"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	c.QueueRetry = QueueRetryPolicy{MaxRetries: 2, MaxWait: time.Minute}
	c.queueWait = func(context.Context, time.Duration) error { return nil }
	resp, err := c.ChatWithQueue(context.Background(), protocol.Request{
		PublicModel: "Display", ModelID: "model-id",
		ModelConfig: map[string]any{"key": "model-id", "max_output_tokens": 1024},
		Messages:    []map[string]any{{"role": "user", "content": "hello"}}, LastUserText: "hello",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if len(sessions) != 2 || sessions[0] == "" || sessions[0] != sessions[1] {
		t.Fatalf("queue retry must reuse one Qoder session, got %#v", sessions)
	}
}

func TestClientConversationSessionStableAcrossModelSwitch(t *testing.T) {
	first, err := sessionIDForRequest(protocol.Request{ModelID: "lite", ClientSessionKey: "codex/thread/thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionIDForRequest(protocol.Request{ModelID: "pro", ClientSessionKey: "codex/thread/thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("same downstream conversation changed Qoder session across model switch: %q != %q", first, second)
	}
}

func TestDistinctClientConversationsUseDistinctQoderSessions(t *testing.T) {
	first, err := sessionIDForRequest(protocol.Request{ClientSessionKey: "codex/thread/thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionIDForRequest(protocol.Request{ClientSessionKey: "codex/thread/thread-2"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("distinct downstream conversations reused Qoder session %q", first)
	}
}
