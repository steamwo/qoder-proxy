package qoder

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestChatKeepsSessionAndRequestSetAcrossToolContinuation(t *testing.T) {
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
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	})}
	client := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})

	base := protocol.Request{
		PublicModel:     "Lite",
		ModelID:         "lite",
		ModelConfig:     map[string]any{"key": "lite", "max_output_tokens": 4096},
		MaxTokens:       4096,
		ClientSessionKey: "claude-code/session/session-123",
		Messages:        []map[string]any{{"role": "user", "content": "analyze project"}},
		LastUserText:    "analyze project",
	}
	resp, err := client.Chat(context.Background(), base)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	continuation := base
	continuation.Messages = []map[string]any{
		{"role": "user", "content": "analyze project"},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
			"id": "call_1", "type": "function", "function": map[string]any{"name": "Read", "arguments": `{"file_path":"README.md"}`},
		}}},
		{"role": "tool", "tool_call_id": "call_1", "content": "contents"},
	}
	resp, err = client.Chat(context.Background(), continuation)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	newTask := continuation
	newTask.Messages = append(append([]map[string]any{}, continuation.Messages...), map[string]any{"role": "user", "content": "now fix it"})
	newTask.LastUserText = "now fix it"
	resp, err = client.Chat(context.Background(), newTask)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(bodies) != 3 {
		t.Fatalf("bodies=%d", len(bodies))
	}
	first, second, third := bodies[0], bodies[1], bodies[2]
	if first["session_id"] != second["session_id"] {
		t.Fatalf("tool continuation changed session: %v != %v", first["session_id"], second["session_id"])
	}
	if first["request_set_id"] != second["request_set_id"] {
		t.Fatalf("tool continuation changed request set: %v != %v", first["request_set_id"], second["request_set_id"])
	}
	if first["chat_record_id"] == second["chat_record_id"] {
		t.Fatalf("distinct model calls reused chat record: %v", first["chat_record_id"])
	}
	if second["request_set_id"] == third["request_set_id"] {
		t.Fatalf("new user task reused request set: %v", third["request_set_id"])
	}
	if second["session_id"] != third["session_id"] {
		t.Fatalf("new task should stay in the same client session: %v != %v", second["session_id"], third["session_id"])
	}
}
