package qoder

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

const maxTrackedPayloadRequestSets = 512

type payloadCostIdentity struct {
	SessionID      string
	RequestSetID   string
	ChatRecordID   string
	UpstreamModel  string
	IsRetry        bool
}

type payloadCostMetrics struct {
	SystemBytes           int
	MessagesCount         int
	MessagesBytes         int
	UserMessageBytes      int
	AssistantMessageBytes int
	ToolMessageBytes      int
	OtherMessageBytes     int
	ToolsCount            int
	ToolSchemaBytes       int
	PromptSurfaceBytes    int
	PlainBodyBytes        int
	EncodedBodyBytes      int
	ToolSchemaHash        [32]byte
}

type payloadCostSetState struct {
	LastChatRecordID             string
	Turn                         int
	Previous                     payloadCostMetrics
	CumulativePromptSurfaceBytes int64
	CumulativePlainBodyBytes     int64
	CumulativeEncodedWireBytes   int64
	LastSeen                     time.Time
}

type payloadCostObservation struct {
	Turn                         int
	DuplicateChatRecord          bool
	ToolSchemaReused             bool
	SystemBytesDelta             int
	MessagesBytesDelta           int
	ToolSchemaBytesDelta         int
	PromptSurfaceBytesDelta      int
	CumulativePromptSurfaceBytes int64
	CumulativePlainBodyBytes     int64
	CumulativeEncodedWireBytes   int64
}

type payloadCostTracker struct {
	mu   sync.Mutex
	sets map[string]payloadCostSetState
}

var qoderPayloadCosts = payloadCostTracker{sets: make(map[string]payloadCostSetState)}

// observeQoderPayloadCost measures the exact serialized message/tool arrays plus
// the surrounding request body without changing the upstream payload. The
// resulting metrics are deliberately byte-oriented: they are not token counts,
// but make repeated tool schemas, history replay, and tool-result growth visible
// before any optimization is attempted.
func observeQoderPayloadCost(plaintext, encoded []byte) {
	if !slog.Default().Enabled(context.Background(), slog.LevelInfo) {
		return
	}
	identity, metrics, ok := measureQoderPayloadCost(plaintext, encoded)
	if !ok {
		return
	}
	observation := qoderPayloadCosts.observe(identity, metrics)

	slog.Info("qoder payload cost",
		"upstream_model", identity.UpstreamModel,
		"session_id", identity.SessionID,
		"request_set_id", identity.RequestSetID,
		"chat_record_id", identity.ChatRecordID,
		"is_retry", identity.IsRetry,
		"turn", observation.Turn,
		"duplicate_chat_record", observation.DuplicateChatRecord,
		"system_bytes", metrics.SystemBytes,
		"system_bytes_delta", observation.SystemBytesDelta,
		"messages", metrics.MessagesCount,
		"messages_bytes", metrics.MessagesBytes,
		"messages_bytes_delta", observation.MessagesBytesDelta,
		"user_message_bytes", metrics.UserMessageBytes,
		"assistant_message_bytes", metrics.AssistantMessageBytes,
		"tool_message_bytes", metrics.ToolMessageBytes,
		"other_message_bytes", metrics.OtherMessageBytes,
		"tools", metrics.ToolsCount,
		"tool_schema_bytes", metrics.ToolSchemaBytes,
		"tool_schema_bytes_delta", observation.ToolSchemaBytesDelta,
		"tool_schema_reused", observation.ToolSchemaReused,
		"prompt_surface_bytes", metrics.PromptSurfaceBytes,
		"prompt_surface_bytes_delta", observation.PromptSurfaceBytesDelta,
		"plain_body_bytes", metrics.PlainBodyBytes,
		"encoded_body_bytes", metrics.EncodedBodyBytes,
		"request_set_prompt_surface_bytes", observation.CumulativePromptSurfaceBytes,
		"request_set_plain_body_bytes", observation.CumulativePlainBodyBytes,
		"request_set_encoded_wire_bytes", observation.CumulativeEncodedWireBytes,
	)
}

func measureQoderPayloadCost(plaintext, encoded []byte) (payloadCostIdentity, payloadCostMetrics, bool) {
	var body map[string]any
	if json.Unmarshal(plaintext, &body) != nil {
		return payloadCostIdentity{}, payloadCostMetrics{}, false
	}

	identity := payloadCostIdentity{
		SessionID:    stringValue(body["session_id"]),
		RequestSetID: stringValue(body["request_set_id"]),
		ChatRecordID: stringValue(body["chat_record_id"]),
	}
	identity.IsRetry, _ = body["is_retry"].(bool)
	if modelConfig, ok := body["model_config"].(map[string]any); ok {
		identity.UpstreamModel = firstNonEmpty(stringValue(modelConfig["key"]), stringValue(modelConfig["model"]))
	}
	// The observer is specific to agent chat payloads. Avoid emitting noise for
	// unrelated encoded blobs used by tests or future endpoints.
	if identity.RequestSetID == "" && identity.ChatRecordID == "" {
		return payloadCostIdentity{}, payloadCostMetrics{}, false
	}

	metrics := payloadCostMetrics{
		PlainBodyBytes:   len(plaintext),
		EncodedBodyBytes: len(encoded),
	}
	if system, ok := body["system"].(string); ok {
		metrics.SystemBytes = len(system)
	}
	metrics.MessagesBytes = jsonSize(body["messages"])
	metrics.ToolSchemaBytes = jsonSize(body["tools"])
	metrics.ToolSchemaHash = sha256.Sum256(jsonBytes(body["tools"]))

	if messages, ok := body["messages"].([]any); ok {
		metrics.MessagesCount = len(messages)
		for _, raw := range messages {
			message, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			n := jsonSize(message)
			switch stringValue(message["role"]) {
			case "user":
				metrics.UserMessageBytes += n
			case "assistant":
				metrics.AssistantMessageBytes += n
			case "tool":
				metrics.ToolMessageBytes += n
			default:
				metrics.OtherMessageBytes += n
			}
		}
	}
	if tools, ok := body["tools"].([]any); ok {
		metrics.ToolsCount = len(tools)
	}
	metrics.PromptSurfaceBytes = metrics.SystemBytes + metrics.MessagesBytes + metrics.ToolSchemaBytes
	return identity, metrics, true
}

func (t *payloadCostTracker) observe(identity payloadCostIdentity, metrics payloadCostMetrics) payloadCostObservation {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sets == nil {
		t.sets = make(map[string]payloadCostSetState)
	}

	key := identity.RequestSetID
	if key == "" {
		key = "session:" + identity.SessionID
	}
	now := time.Now()
	state, exists := t.sets[key]
	if !exists {
		if len(t.sets) >= maxTrackedPayloadRequestSets {
			t.evictOldestLocked()
		}
		state = payloadCostSetState{
			LastChatRecordID:             identity.ChatRecordID,
			Turn:                         1,
			Previous:                     metrics,
			CumulativePromptSurfaceBytes: int64(metrics.PromptSurfaceBytes),
			CumulativePlainBodyBytes:     int64(metrics.PlainBodyBytes),
			CumulativeEncodedWireBytes:   int64(metrics.EncodedBodyBytes),
			LastSeen:                     now,
		}
		t.sets[key] = state
		return observationFromState(state)
	}

	observation := payloadCostObservation{Turn: state.Turn}
	state.CumulativeEncodedWireBytes += int64(metrics.EncodedBodyBytes)
	state.LastSeen = now

	sameChatRecord := identity.ChatRecordID != "" && identity.ChatRecordID == state.LastChatRecordID
	if sameChatRecord {
		observation.DuplicateChatRecord = !identity.IsRetry
		observation.ToolSchemaReused = metrics.ToolSchemaHash == state.Previous.ToolSchemaHash
	} else if identity.IsRetry {
		// Retry identity should normally keep the same chat_record_id. If an
		// upstream-compatible client ever changes it, do not count the retry as a
		// new logical model turn or inflate logical context totals.
		observation.ToolSchemaReused = metrics.ToolSchemaHash == state.Previous.ToolSchemaHash
	} else {
		state.Turn++
		observation.Turn = state.Turn
		observation.SystemBytesDelta = metrics.SystemBytes - state.Previous.SystemBytes
		observation.MessagesBytesDelta = metrics.MessagesBytes - state.Previous.MessagesBytes
		observation.ToolSchemaBytesDelta = metrics.ToolSchemaBytes - state.Previous.ToolSchemaBytes
		observation.PromptSurfaceBytesDelta = metrics.PromptSurfaceBytes - state.Previous.PromptSurfaceBytes
		observation.ToolSchemaReused = metrics.ToolSchemaHash == state.Previous.ToolSchemaHash
		state.CumulativePromptSurfaceBytes += int64(metrics.PromptSurfaceBytes)
		state.CumulativePlainBodyBytes += int64(metrics.PlainBodyBytes)
		state.LastChatRecordID = identity.ChatRecordID
		state.Previous = metrics
	}

	t.sets[key] = state
	observation.CumulativePromptSurfaceBytes = state.CumulativePromptSurfaceBytes
	observation.CumulativePlainBodyBytes = state.CumulativePlainBodyBytes
	observation.CumulativeEncodedWireBytes = state.CumulativeEncodedWireBytes
	return observation
}

func observationFromState(state payloadCostSetState) payloadCostObservation {
	return payloadCostObservation{
		Turn:                         state.Turn,
		CumulativePromptSurfaceBytes: state.CumulativePromptSurfaceBytes,
		CumulativePlainBodyBytes:     state.CumulativePlainBodyBytes,
		CumulativeEncodedWireBytes:   state.CumulativeEncodedWireBytes,
	}
}

func (t *payloadCostTracker) evictOldestLocked() {
	var oldestKey string
	var oldest time.Time
	for key, state := range t.sets {
		if oldestKey == "" || state.LastSeen.Before(oldest) {
			oldestKey = key
			oldest = state.LastSeen
		}
	}
	if oldestKey != "" {
		delete(t.sets, oldestKey)
	}
}

func jsonSize(value any) int {
	return len(jsonBytes(value))
}

func jsonBytes(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
}
