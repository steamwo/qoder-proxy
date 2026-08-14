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

func TestQueueRetryMarksRetryAndKeepsTaskIdentity(t *testing.T) {
	queuePayload, _ := json.Marshal(map[string]any{"isQueued": true, "retryAfterSeconds": 1})
	queueMiddle, _ := json.Marshal(map[string]any{"code": "10605", "message": string(queuePayload)})
	queueBody, _ := json.Marshal(map[string]any{"code": 403, "message": string(queueMiddle)})
	queueEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 403, "body": string(queueBody)})
	successEnvelope, _ := json.Marshal(map[string]any{"statusCodeValue": 200, "body": `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`})

	var bodies []map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain := decodeQoderBodyForTest(t, encoded)
		var body map[string]any
		if err := json.Unmarshal(plain, &body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		frame := successEnvelope
		if len(bodies) == 1 {
			frame = queueEnvelope
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: " + string(frame) + "\n\n")),
			Request:    r,
		}, nil
	})}
	client := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	client.QueueRetry = QueueRetryPolicy{MaxRetries: 1, MaxWait: time.Minute}
	client.queueWait = func(context.Context, time.Duration) error { return nil }

	resp, err := client.Chat(context.Background(), protocol.Request{
		PublicModel:     "Lite",
		ModelID:         "lite",
		ModelConfig:     map[string]any{"key": "lite", "max_output_tokens": 1024},
		MaxTokens:       1024,
		ClientSessionKey: "claude-code/session/session-123",
		Messages:        []map[string]any{{"role": "user", "content": "hello"}},
		LastUserText:    "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(bodies) != 2 {
		t.Fatalf("calls=%d", len(bodies))
	}
	if got, _ := bodies[0]["is_retry"].(bool); got {
		t.Fatalf("first attempt marked retry: %#v", bodies[0]["is_retry"])
	}
	if got, _ := bodies[1]["is_retry"].(bool); !got {
		t.Fatalf("second attempt not marked retry: %#v", bodies[1]["is_retry"])
	}
	for _, key := range []string{"session_id", "request_set_id", "chat_record_id"} {
		if bodies[0][key] != bodies[1][key] {
			t.Fatalf("%s changed across queue retry: %v != %v", key, bodies[0][key], bodies[1][key])
		}
	}
	if bodies[0]["request_id"] == bodies[1]["request_id"] {
		t.Fatal("wire request_id must remain unique per retry attempt")
	}
}
