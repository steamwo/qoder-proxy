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

func TestChatDerivesLegacyImageURLsFromLatestUserMessage(t *testing.T) {
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
	messages := []map[string]any{{
		"role": "user",
		"content": []any{
			map[string]any{"type": "text", "text": "describe"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": images[0]}},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": images[1]}},
		},
	}}
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel:    "Vision Model",
		ModelID:        "vision-id",
		ModelConfig:    map[string]any{"key": "vision-id", "max_output_tokens": 1024, "is_vl": true},
		SourceProtocol: "openai_responses",
		Messages:       messages,
		LastUserText:   "describe",
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
	if chatContext["imageUrls"] != nil {
		t.Fatalf("chat_context.imageUrls=%#v, want nil to avoid a third image copy", chatContext["imageUrls"])
	}
	extra := chatContext["extra"].(map[string]any)
	modelConfig := extra["modelConfig"].(map[string]any)
	if modelConfig["is_vl"] != true {
		t.Fatalf("chat_context modelConfig=%#v", modelConfig)
	}

	gotMessages, ok := body["messages"].([]any)
	if !ok || len(gotMessages) != 1 {
		t.Fatalf("messages=%#v", body["messages"])
	}
	user := gotMessages[0].(map[string]any)
	content, ok := user["content"].([]any)
	if !ok || len(content) != 3 {
		t.Fatalf("user content=%#v", user["content"])
	}
	for i, want := range images {
		part := content[i+1].(map[string]any)
		imageURL := part["image_url"].(map[string]any)
		if imageURL["url"] != want {
			t.Fatalf("image part[%d] url=%#v want %q", i, imageURL["url"], want)
		}
	}
}

func TestChatDoesNotPromoteHistoricalImageToLatestUser(t *testing.T) {
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
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: r}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	messages := []map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "first"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/old.png"}},
		}},
		{"role": "assistant", "content": "noted"},
		{"role": "user", "content": "second"},
	}
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel: "Vision Model", ModelID: "vision-id",
		ModelConfig: map[string]any{"key": "vision-id", "is_vl": true}, Messages: messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if body["image_urls"] != nil {
		t.Fatalf("image_urls=%#v, want nil because latest user turn has no image", body["image_urls"])
	}
	gotMessages := body["messages"].([]any)
	first := gotMessages[0].(map[string]any)
	if len(contentImageURLs(first["content"])) != 1 {
		t.Fatalf("historical image lost: %#v", first)
	}
	if gotMessages[2].(map[string]any)["content"] != "second" {
		t.Fatalf("latest user changed: %#v", gotMessages[2])
	}
}

func TestChatProjectsToolResultImageWithoutRepeatingPreviousUserImage(t *testing.T) {
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
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: [DONE]\n\n")), Request: r}, nil
	})}
	c := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	messages := []map[string]any{
		{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "inspect this"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/original.png"}},
		}},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{
			"id": "call_1", "type": "function", "function": map[string]any{"name": "screenshot", "arguments": "{}"},
		}}},
		{"role": "tool", "tool_call_id": "call_1", "content": []any{
			map[string]any{"type": "text", "text": "latest screenshot"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,dG9vbA=="}},
		}},
	}
	resp, err := c.Chat(context.Background(), protocol.Request{
		PublicModel: "Vision Model", ModelID: "vision-id",
		ModelConfig: map[string]any{"key": "vision-id", "is_vl": true}, Messages: messages,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	images, ok := body["image_urls"].([]any)
	if !ok || len(images) != 1 || images[0] != "data:image/png;base64,dG9vbA==" {
		t.Fatalf("active tool image projection=%#v", body["image_urls"])
	}
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
		Messages: []map[string]any{{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "describe"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "https://example.com/cat.jpg"}},
		}}},
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

func TestRequestSetIDChangesWithInlineImages(t *testing.T) {
	request := func(url string) protocol.Request {
		return protocol.Request{ModelID: "vision-id", Messages: []map[string]any{{"role": "user", "content": []any{
			map[string]any{"type": "text", "text": "describe"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": url}},
		}}}}
	}
	if requestSetIDForRequest(request("https://example.com/a.png"), "session") == requestSetIDForRequest(request("https://example.com/b.png"), "session") {
		t.Fatal("different inline image inputs reused request_set_id")
	}
}
