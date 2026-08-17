package qoder

import (
	"encoding/json"
	"testing"
)

func TestQoderEncodeBodyAddsPromptCacheMarker(t *testing.T) {
	plain := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	encoded := qoderEncodeBody(plain)
	decoded := decodeQoderBodyForTest(t, encoded)

	var body map[string]any
	if err := json.Unmarshal(decoded, &body); err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]any)
	message := messages[0].(map[string]any)
	if message["content"] != "hello" {
		t.Fatalf("content=%#v", message["content"])
	}
	contents := message["contents"].([]any)
	if len(contents) != 1 {
		t.Fatalf("contents=%#v", contents)
	}
	part := contents[0].(map[string]any)
	if part["type"] != "text" || part["text"] != "hello" {
		t.Fatalf("part=%#v", part)
	}
	control := part["cache_control"].(map[string]any)
	if control["type"] != "ephemeral" {
		t.Fatalf("cache_control=%#v", control)
	}
}

func TestQoderEncodeBodyPreservesExistingCacheMarker(t *testing.T) {
	plain := []byte(`{"messages":[{"role":"user","content":"first","contents":[{"type":"text","text":"first","cache_control":{"type":"ephemeral"}}]},{"role":"assistant","content":"reply"},{"role":"user","content":"second"}]}`)
	encoded := qoderEncodeBody(plain)
	decoded := decodeQoderBodyForTest(t, encoded)

	var body map[string]any
	if err := json.Unmarshal(decoded, &body); err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]any)
	markers := 0
	for _, rawMessage := range messages {
		message := rawMessage.(map[string]any)
		contents, _ := message["contents"].([]any)
		for _, rawPart := range contents {
			part := rawPart.(map[string]any)
			if control, ok := part["cache_control"].(map[string]any); ok && control["type"] == "ephemeral" {
				markers++
			}
		}
	}
	if markers != 1 {
		t.Fatalf("markers=%d want 1", markers)
	}
	last := messages[2].(map[string]any)["contents"].([]any)[0].(map[string]any)
	if _, exists := last["cache_control"]; exists {
		t.Fatalf("unexpected second marker: %#v", last)
	}
}

func TestQoderEncodeBodyProjectsImageContents(t *testing.T) {
	plain := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJj"}}]}]}`)
	encoded := qoderEncodeBody(plain)
	decoded := decodeQoderBodyForTest(t, encoded)

	var body map[string]any
	if err := json.Unmarshal(decoded, &body); err != nil {
		t.Fatal(err)
	}
	message := body["messages"].([]any)[0].(map[string]any)
	contents := message["contents"].([]any)
	if len(contents) != 2 {
		t.Fatalf("contents=%#v", contents)
	}
	image := contents[1].(map[string]any)
	imageURL := image["image_url"].(map[string]any)
	if image["type"] != "image_url" || imageURL["url"] != "data:image/png;base64,YWJj" {
		t.Fatalf("image=%#v", image)
	}
	control := image["cache_control"].(map[string]any)
	if control["type"] != "ephemeral" {
		t.Fatalf("image cache_control=%#v", control)
	}
}

func TestQoderEncodeBodySkipsToolTailForCacheMarker(t *testing.T) {
	plain := []byte(`{"messages":[{"role":"user","content":"run"},{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"Read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":"result"}]}`)
	encoded := qoderEncodeBody(plain)
	decoded := decodeQoderBodyForTest(t, encoded)

	var body map[string]any
	if err := json.Unmarshal(decoded, &body); err != nil {
		t.Fatal(err)
	}
	messages := body["messages"].([]any)
	user := messages[0].(map[string]any)
	part := user["contents"].([]any)[0].(map[string]any)
	if control, ok := part["cache_control"].(map[string]any); !ok || control["type"] != "ephemeral" {
		t.Fatalf("user marker missing: %#v", part)
	}
	tool := messages[2].(map[string]any)
	if _, exists := tool["contents"]; exists {
		t.Fatalf("tool message unexpectedly has contents: %#v", tool)
	}
}
