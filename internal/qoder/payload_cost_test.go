package qoder

import (
	"encoding/json"
	"testing"
)

func TestMeasureQoderPayloadCostBreaksDownContext(t *testing.T) {
	body := map[string]any{
		"session_id":     "session-1",
		"request_set_id": "request-set-1",
		"chat_record_id": "chat-1",
		"is_retry":       false,
		"system":         "system prompt",
		"messages": []any{
			map[string]any{"role": "user", "content": "inspect the project"},
			map[string]any{"role": "assistant", "content": "", "tool_calls": []any{map[string]any{"id": "call-1"}}},
			map[string]any{"role": "tool", "tool_call_id": "call-1", "content": "large tool result"},
		},
		"tools": []any{
			map[string]any{"type": "function", "function": map[string]any{"name": "Read", "parameters": map[string]any{"type": "object"}}},
			map[string]any{"type": "function", "function": map[string]any{"name": "Bash", "parameters": map[string]any{"type": "object"}}},
		},
		"model_config": map[string]any{"key": "lite"},
	}
	plain, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	encoded := make([]byte, len(plain)+17)

	identity, metrics, ok := measureQoderPayloadCost(plain, encoded)
	if !ok {
		t.Fatal("payload was not recognized")
	}
	if identity.SessionID != "session-1" || identity.RequestSetID != "request-set-1" || identity.ChatRecordID != "chat-1" {
		t.Fatalf("identity=%#v", identity)
	}
	if identity.UpstreamModel != "lite" || identity.IsRetry {
		t.Fatalf("identity=%#v", identity)
	}
	if metrics.MessagesCount != 3 || metrics.ToolsCount != 2 {
		t.Fatalf("counts=%#v", metrics)
	}
	if metrics.SystemBytes != len("system prompt") {
		t.Fatalf("system bytes=%d", metrics.SystemBytes)
	}
	if metrics.MessagesBytes <= 0 || metrics.ToolSchemaBytes <= 0 {
		t.Fatalf("missing serialized component sizes: %#v", metrics)
	}
	if metrics.UserMessageBytes <= 0 || metrics.AssistantMessageBytes <= 0 || metrics.ToolMessageBytes <= 0 {
		t.Fatalf("missing role breakdown: %#v", metrics)
	}
	if metrics.PromptSurfaceBytes != metrics.SystemBytes+metrics.MessagesBytes+metrics.ToolSchemaBytes {
		t.Fatalf("prompt surface=%d metrics=%#v", metrics.PromptSurfaceBytes, metrics)
	}
	if metrics.PlainBodyBytes != len(plain) || metrics.EncodedBodyBytes != len(encoded) {
		t.Fatalf("body sizes=%#v", metrics)
	}
}

func TestPayloadCostTrackerSeparatesLogicalGrowthFromWireRetries(t *testing.T) {
	tracker := payloadCostTracker{sets: make(map[string]payloadCostSetState)}
	toolHash := [32]byte{1}

	first := payloadCostMetrics{
		SystemBytes:        100,
		MessagesBytes:      1000,
		ToolSchemaBytes:    600,
		PromptSurfaceBytes: 1700,
		PlainBodyBytes:     2000,
		EncodedBodyBytes:   2700,
		ToolSchemaHash:     toolHash,
	}
	id1 := payloadCostIdentity{SessionID: "s", RequestSetID: "r", ChatRecordID: "c1"}
	obs1 := tracker.observe(id1, first)
	if obs1.Turn != 1 || obs1.MessagesBytesDelta != 0 || obs1.CumulativePromptSurfaceBytes != 1700 || obs1.CumulativeEncodedWireBytes != 2700 {
		t.Fatalf("first observation=%#v", obs1)
	}

	second := payloadCostMetrics{
		SystemBytes:        100,
		MessagesBytes:      1400,
		ToolSchemaBytes:    600,
		PromptSurfaceBytes: 2100,
		PlainBodyBytes:     2400,
		EncodedBodyBytes:   3200,
		ToolSchemaHash:     toolHash,
	}
	id2 := payloadCostIdentity{SessionID: "s", RequestSetID: "r", ChatRecordID: "c2"}
	obs2 := tracker.observe(id2, second)
	if obs2.Turn != 2 {
		t.Fatalf("turn=%d", obs2.Turn)
	}
	if obs2.MessagesBytesDelta != 400 || obs2.ToolSchemaBytesDelta != 0 || obs2.PromptSurfaceBytesDelta != 400 {
		t.Fatalf("growth=%#v", obs2)
	}
	if !obs2.ToolSchemaReused {
		t.Fatal("unchanged tool schema should be reported as reused")
	}
	if obs2.CumulativePromptSurfaceBytes != 3800 || obs2.CumulativePlainBodyBytes != 4400 || obs2.CumulativeEncodedWireBytes != 5900 {
		t.Fatalf("logical totals=%#v", obs2)
	}

	retryID := id2
	retryID.IsRetry = true
	retry := tracker.observe(retryID, second)
	if retry.Turn != 2 {
		t.Fatalf("retry advanced logical turn: %#v", retry)
	}
	if retry.CumulativePromptSurfaceBytes != 3800 || retry.CumulativePlainBodyBytes != 4400 {
		t.Fatalf("retry inflated logical totals: %#v", retry)
	}
	if retry.CumulativeEncodedWireBytes != 9100 {
		t.Fatalf("retry wire bytes=%d want=9100", retry.CumulativeEncodedWireBytes)
	}
}

func TestPayloadCostTrackerFlagsExactReplayWithoutAdvancingTurn(t *testing.T) {
	tracker := payloadCostTracker{sets: make(map[string]payloadCostSetState)}
	metrics := payloadCostMetrics{MessagesBytes: 50, PromptSurfaceBytes: 50, PlainBodyBytes: 80, EncodedBodyBytes: 108}
	identity := payloadCostIdentity{SessionID: "s", RequestSetID: "r", ChatRecordID: "same"}
	_ = tracker.observe(identity, metrics)

	replay := tracker.observe(identity, metrics)
	if replay.Turn != 1 || !replay.DuplicateChatRecord {
		t.Fatalf("replay=%#v", replay)
	}
	if replay.CumulativePromptSurfaceBytes != 50 || replay.CumulativePlainBodyBytes != 80 {
		t.Fatalf("replay inflated logical totals: %#v", replay)
	}
	if replay.CumulativeEncodedWireBytes != 216 {
		t.Fatalf("wire total=%d want=216", replay.CumulativeEncodedWireBytes)
	}
}
