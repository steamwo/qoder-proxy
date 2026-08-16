package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestChatForwardsImageURLs(t *testing.T) {
	var body map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain := decodeQoderBodyForTest(t, encoded)
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
	images := []string{"data:image/png;base64,YWJj", "https://example.com/cat.jpg"}
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel:  "Vision Model",
		ModelID:      "vision-id",
		ModelConfig:  map[string]any{"key": "vision-id", "max_output_tokens": 1024, "is_vl": true},
		Messages:     []map[string]any{{"role": "user", "content": "describe"}},
		ImageURLs:    images,
		LastUserText: "describe",
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	assertImageArray := func(name string, raw any) {
		t.Helper()
		arr, ok := raw.([]any)
		if !ok || len(arr) != len(images) {
			t.Fatalf("%s=%#v", name, raw)
		}
		for i, want := range images {
			if arr[i] != want {
				t.Fatalf("%s[%d]=%#v want %q", name, i, arr[i], want)
			}
		}
	}
	assertImageArray("image_urls", body["image_urls"])
	chatContext, ok := body["chat_context"].(map[string]any)
	if !ok {
		t.Fatalf("chat_context=%#v", body["chat_context"])
	}
	assertImageArray("chat_context.imageUrls", chatContext["imageUrls"])
}

func TestChatRejectsImagesForNonVisionModel(t *testing.T) {
	called := false
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return nil, errors.New("transport must not be called")
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel: "Text Model",
		ModelID:     "text-id",
		ModelConfig: map[string]any{"key": "text-id", "is_vl": false},
		Messages:    []map[string]any{{"role": "user", "content": "describe"}},
		ImageURLs:   []string{"https://example.com/cat.jpg"},
	})
	if resp != nil {
		resp.Body.Close()
		t.Fatal("expected no response for unsupported image input")
	}
	var upstreamErr *UpstreamError
	if !errors.As(err, &upstreamErr) {
		t.Fatalf("expected UpstreamError, got %T: %v", err, err)
	}
	if upstreamErr.HTTPStatus != http.StatusBadRequest || upstreamErr.PublicCode != "unsupported_image_input" || upstreamErr.Type != "invalid_request_error" {
		t.Fatalf("unexpected error: %#v", upstreamErr)
	}
	if !strings.Contains(upstreamErr.Message, `model "Text Model" does not support image input`) {
		t.Fatalf("message=%q", upstreamErr.Message)
	}
	if called {
		t.Fatal("non-vision image request reached upstream transport")
	}
}

func TestRequestSetIDChangesWithImages(t *testing.T) {
	base := protocol.Request{ModelID: "vision-id", Messages: []map[string]any{{"role": "user", "content": "describe"}}}
	first := base
	first.ImageURLs = []string{"https://example.com/a.png"}
	second := base
	second.ImageURLs = []string{"https://example.com/b.png"}
	if requestSetIDForRequest(first, "session") == requestSetIDForRequest(second, "session") {
		t.Fatal("different image inputs reused request_set_id")
	}
}
