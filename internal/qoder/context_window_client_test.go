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

func TestChatSendsContextWindowParameter(t *testing.T) {
	var body map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain := decodeQoderBodyForTest(t, b)
		if err := json.Unmarshal(plain, &body); err != nil {
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
		PublicModel: "Context Model", ModelID: "context-id",
		ModelConfig: map[string]any{"key": "context-id", "max_output_tokens": 1024},
		ContextWindow: 1000000,
		Messages:      []map[string]any{{"role": "user", "content": "hello"}},
		LastUserText:  "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	params, ok := body["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters=%#v", body["parameters"])
	}
	if got := int(params["context_length"].(float64)); got != 1000000 {
		t.Fatalf("context_length=%d parameters=%#v", got, params)
	}
}

func TestChatOmitsContextWindowInAutoMode(t *testing.T) {
	var body map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain := decodeQoderBodyForTest(t, b)
		if err := json.Unmarshal(plain, &body); err != nil {
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
		PublicModel: "Context Model", ModelID: "context-id",
		ModelConfig: map[string]any{"key": "context-id", "max_output_tokens": 1024},
		Messages:    []map[string]any{{"role": "user", "content": "hello"}},
		LastUserText: "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	params, ok := body["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("parameters=%#v", body["parameters"])
	}
	if _, exists := params["context_length"]; exists {
		t.Fatalf("context_length must be omitted in auto mode: %#v", params)
	}
	if _, exists := params["contextWindow"]; exists {
		t.Fatalf("legacy contextWindow must be omitted in auto mode: %#v", params)
	}
}
