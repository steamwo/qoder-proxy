package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func imagePartForTest(index int) map[string]any {
	return map[string]any{
		"type": "image_url",
		"image_url": map[string]any{
			"url": fmt.Sprintf("https://images.test/%03d.png", index),
		},
	}
}

func TestTrimMessageImagesKeepsMostRecentFifty(t *testing.T) {
	for _, total := range []int{49, 50, 51, 73} {
		t.Run(fmt.Sprintf("total_%d", total), func(t *testing.T) {
			messages := make([]map[string]any, 0, total+1)
			messages = append(messages, map[string]any{
				"role": "assistant",
				"content": "tool boundary",
				"tool_calls": []any{map[string]any{"id": "call-1", "type": "function"}},
			})
			for i := 0; i < total; i++ {
				role := "user"
				message := map[string]any{
					"role": role,
					"content": []any{
						map[string]any{"type": "text", "text": fmt.Sprintf("text-%03d", i)},
						imagePartForTest(i),
					},
				}
				if i%3 == 1 {
					message["role"] = "tool"
					message["tool_call_id"] = fmt.Sprintf("call-%03d", i)
				}
				messages = append(messages, message)
			}

			before, err := json.Marshal(messages)
			if err != nil {
				t.Fatal(err)
			}
			trimmed := trimMessageImages(messages, maxQoderHistoryImages)
			urls := messageImageURLs(trimmed)
			wantCount := total
			if wantCount > maxQoderHistoryImages {
				wantCount = maxQoderHistoryImages
			}
			if len(urls) != wantCount {
				t.Fatalf("image count=%d want=%d", len(urls), wantCount)
			}
			first := total - wantCount
			for i, url := range urls {
				want := fmt.Sprintf("https://images.test/%03d.png", first+i)
				if url != want {
					t.Fatalf("image[%d]=%q want=%q", i, url, want)
				}
			}
			if len(trimmed) != len(messages) {
				t.Fatalf("message count=%d want=%d", len(trimmed), len(messages))
			}
			if len(messageImageURLs(messages)) != total {
				t.Fatal("trimMessageImages mutated the caller's messages")
			}
			if total <= maxQoderHistoryImages {
				after, err := json.Marshal(trimmed)
				if err != nil {
					t.Fatal(err)
				}
				if string(after) != string(before) {
					t.Fatalf("payload changed at or below limit:\n%s\n%s", before, after)
				}
			}
			if trimmed[0]["content"] != "tool boundary" {
				t.Fatalf("assistant content changed: %#v", trimmed[0])
			}
		})
	}
}

func TestTrimMessageImagesPreservesToolResultAndTypedContent(t *testing.T) {
	oldContent := []map[string]any{
		{"type": "text", "text": "keep this text"},
		imagePartForTest(0),
		imagePartForTest(1),
	}
	toolContent := make([]any, 0, 50)
	toolContent = append(toolContent, map[string]any{"type": "text", "text": "tool output"})
	for i := 2; i < 51; i++ {
		toolContent = append(toolContent, imagePartForTest(i))
	}
	messages := []map[string]any{
		{"role": "user", "content": oldContent},
		{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call-1"}}},
		{"role": "tool", "tool_call_id": "call-1", "content": toolContent},
	}

	trimmed := trimMessageImages(messages, maxQoderHistoryImages)
	urls := messageImageURLs(trimmed)
	if len(urls) != 50 {
		t.Fatalf("image count=%d", len(urls))
	}
	if urls[0] != "https://images.test/001.png" || urls[len(urls)-1] != "https://images.test/050.png" {
		t.Fatalf("unexpected retained range: first=%q last=%q", urls[0], urls[len(urls)-1])
	}
	parts, ok := trimmed[0]["content"].([]map[string]any)
	if !ok {
		t.Fatalf("typed content changed type: %T", trimmed[0]["content"])
	}
	if len(parts) != 2 || parts[0]["text"] != "keep this text" || contentPartImageURL(parts[1]) != "https://images.test/001.png" {
		t.Fatalf("old user content not preserved correctly: %#v", parts)
	}
	if trimmed[2]["tool_call_id"] != "call-1" {
		t.Fatalf("tool_call_id changed: %#v", trimmed[2])
	}
	if len(messageImageURLs(messages)) != 51 {
		t.Fatal("original tool-result history was mutated")
	}
}

func TestChatCapsFinalPayloadAndIdentityToFiftyImages(t *testing.T) {
	parts := make([]any, 0, 52)
	parts = append(parts, map[string]any{"type": "text", "text": "inspect images"})
	for i := 0; i < 51; i++ {
		parts = append(parts, imagePartForTest(i))
	}
	messages := []map[string]any{{"role": "user", "content": parts}}

	var body map[string]any
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		encoded, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(decodeQoderBodyForTest(t, encoded), &body); err != nil {
			t.Fatal(err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
			Request:    r,
		}, nil
	})}
	client := NewClient(hc, credential.Credential{Token: "token", UserID: "u1", MachineID: "m1"})
	req := protocol.Request{
		PublicModel:      "Vision Model",
		ModelID:          "vision-id",
		ModelConfig:      map[string]any{"key": "vision-id", "source": "system", "max_output_tokens": 1024, "is_vl": true},
		Messages:         messages,
		LastUserText:     "inspect images",
		ClientSessionKey: "image-history-test",
	}
	resp, err := client.Chat(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	rawMessages, ok := body["messages"].([]any)
	if !ok {
		t.Fatalf("messages=%T", body["messages"])
	}
	decodedMessages := make([]map[string]any, 0, len(rawMessages))
	for _, raw := range rawMessages {
		message, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("message=%T", raw)
		}
		decodedMessages = append(decodedMessages, message)
	}
	urls := messageImageURLs(decodedMessages)
	if len(urls) != maxQoderHistoryImages {
		t.Fatalf("payload images=%d", len(urls))
	}
	if urls[0] != "https://images.test/001.png" || urls[len(urls)-1] != "https://images.test/050.png" {
		t.Fatalf("unexpected payload image range: first=%q last=%q", urls[0], urls[len(urls)-1])
	}

	topLevel, ok := body["image_urls"].([]any)
	if !ok || len(topLevel) != maxQoderHistoryImages {
		t.Fatalf("image_urls=%#v", body["image_urls"])
	}
	if topLevel[0] != "https://images.test/001.png" || topLevel[len(topLevel)-1] != "https://images.test/050.png" {
		t.Fatalf("unexpected top-level image range: %#v", topLevel)
	}

	trimmed := trimMessageImages(req.Messages, maxQoderHistoryImages)
	identityReq := req
	identityReq.Messages = trimmed
	sessionID, err := sessionIDForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	wantRequestSetID := requestSetIDForRequest(identityReq, sessionID)
	if body["request_set_id"] != wantRequestSetID {
		t.Fatalf("request_set_id=%v want=%s", body["request_set_id"], wantRequestSetID)
	}
	wantChatRecordID := stableHash("qoder-chat-record", sessionID, req.ModelID, trimmed, []any{}, 1024, req.ReasoningEffort, req.ContextWindow)
	if body["chat_record_id"] != wantChatRecordID {
		t.Fatalf("chat_record_id=%v want=%s", body["chat_record_id"], wantChatRecordID)
	}
	if len(messageImageURLs(req.Messages)) != 51 {
		t.Fatal("Chat mutated the caller's request history")
	}
}
