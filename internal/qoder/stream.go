package qoder

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func ParseStream(r io.Reader, emit func(protocol.Event) error) error {
	stats := streamStats{}
	output := newStreamOutputState()
	err := readSSE(r, func(data string) error {
		stats.Envelopes++
		if data == "[DONE]" {
			return nil
		}
		var envelope map[string]any
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			stats.MalformedEnvelopes++
			return nil // upstream keepalive/malformed line
		}
		status := 200
		if n, ok := envelope["statusCodeValue"].(float64); ok {
			status = int(n)
		}
		inner, _ := envelopeBodyText(envelope)
		if status != 200 {
			return emit(protocol.Event{Kind: protocol.EventError, Error: firstNonEmpty(inner, fmt.Sprintf("Qoder status %d", status))})
		}
		if strings.TrimSpace(inner) == "" {
			return nil
		}
		stats.InnerFrames++
		recognized, err := parseInner([]byte(inner), func(ev protocol.Event) error {
			filtered, keep := output.filter(ev)
			if !keep {
				return nil
			}
			ev = filtered
			stats.Events++
			switch ev.Kind {
			case protocol.EventTextDelta:
				stats.TextEvents++
				stats.TextBytes += len(ev.Text)
			case protocol.EventToolDelta:
				stats.ToolEvents++
			case protocol.EventUsage:
				stats.UsageEvents++
			case protocol.EventFinish:
				stats.FinishEvents++
			case protocol.EventError:
				stats.ErrorEvents++
			}
			return emit(ev)
		})
		if !recognized {
			stats.UnknownFrames++
		}
		return err
	})
	slog.Debug("qoder stream parsed",
		"envelopes", stats.Envelopes,
		"inner_frames", stats.InnerFrames,
		"events", stats.Events,
		"text_events", stats.TextEvents,
		"text_bytes", stats.TextBytes,
		"tool_events", stats.ToolEvents,
		"usage_events", stats.UsageEvents,
		"finish_events", stats.FinishEvents,
		"error_events", stats.ErrorEvents,
		"unknown_frames", stats.UnknownFrames,
		"malformed_envelopes", stats.MalformedEnvelopes,
	)
	return err
}

func envelopeBodyText(envelope map[string]any) (string, bool) {
	v, exists := envelope["body"]
	if !exists || v == nil {
		return "", false
	}
	switch body := v.(type) {
	case string:
		return body, true
	case map[string]any, []any:
		b, err := json.Marshal(body)
		if err != nil {
			return "", false
		}
		return string(b), true
	default:
		return fmt.Sprint(body), true
	}
}

type streamStats struct {
	Envelopes          int
	InnerFrames        int
	Events             int
	TextEvents         int
	TextBytes          int
	ToolEvents         int
	UsageEvents        int
	FinishEvents       int
	ErrorEvents        int
	UnknownFrames      int
	MalformedEnvelopes int
}

func readSSE(r io.Reader, onData func(string) error) error {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	var data []string
	firstDataLogged := false

	logFirstData := func(value string) {
		if firstDataLogged {
			return
		}
		firstDataLogged = true
		trimmed := strings.TrimSpace(value)
		slog.Debug("qoder first sse data line",
			"bytes", len(value),
			"json_valid", json.Valid([]byte(trimmed)),
			"done", trimmed == "[DONE]",
		)
	}

	flush := func() error {
		if len(data) == 0 {
			return nil
		}
		joined := strings.Join(data, "\n")
		data = data[:0]
		return onData(joined)
	}

	// Qoder normally emits standards-compliant SSE frames separated by an empty
	// line. Its HTTP edge can also emit one complete data line followed by only a
	// single newline, so dispatch complete JSON values immediately.
	isCompleteData := func(value string) bool {
		trimmed := strings.TrimSpace(value)
		return trimmed == "[DONE]" || json.Valid([]byte(trimmed))
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		value := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		logFirstData(value)
		if len(data) == 0 && isCompleteData(value) {
			if err := onData(value); err != nil {
				return err
			}
			continue
		}
		data = append(data, value)
		if isCompleteData(strings.Join(data, "\n")) {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func parseInner(raw []byte, emit func(protocol.Event) error) (bool, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		slog.Debug("qoder inner frame is not json", "bytes", len(raw))
		return false, nil
	}
	recognized := false
	if er, ok := obj["error"]; ok && er != nil {
		b, _ := json.Marshal(er)
		return true, emit(protocol.Event{Kind: protocol.EventError, Error: string(b), Raw: append([]byte(nil), raw...)})
	}
	if usage, ok := parseUsage(obj); ok {
		recognized = true
		if err := emit(protocol.Event{Kind: protocol.EventUsage, Usage: usage, Raw: append([]byte(nil), raw...)}); err != nil {
			return true, err
		}
	}
	if choices, ok := obj["choices"].([]any); ok {
		recognized = true
		for _, rawChoice := range choices {
			choice, ok := rawChoice.(map[string]any)
			if !ok {
				continue
			}

			payload, _ := choicePayload(choice)
			if payload != nil {
				if text := contentText(payload["content"]); text != "" {
					if err := emit(protocol.Event{Kind: protocol.EventTextDelta, Text: text, Raw: append([]byte(nil), raw...)}); err != nil {
						return true, err
					}
				}
				if calls, ok := payload["tool_calls"].([]any); ok {
					for _, rawCall := range calls {
						call, ok := rawCall.(map[string]any)
						if !ok {
							continue
						}
						idx := numberAsInt(call["index"])
						fn, _ := call["function"].(map[string]any)
						if err := emit(protocol.Event{Kind: protocol.EventToolDelta, ToolIndex: idx,
							ToolID: stringValue(call["id"]), ToolName: stringValue(fn["name"]), ToolArguments: stringValue(fn["arguments"]), Raw: append([]byte(nil), raw...)}); err != nil {
							return true, err
						}
					}
				}
			} else if _, deltaExists := choice["delta"]; !deltaExists {
				// Some non-stream-shaped Qoder frames use choices[].text instead of
				// either delta.content or message.content. Only use this fallback when
				// delta is absent so a final aggregate snapshot cannot be duplicated.
				if text := contentText(choice["text"]); text != "" {
					if err := emit(protocol.Event{Kind: protocol.EventTextDelta, Text: text, Raw: append([]byte(nil), raw...)}); err != nil {
						return true, err
					}
				}
			}

			if reason := stringValue(choice["finish_reason"]); reason != "" {
				if err := emit(protocol.Event{Kind: protocol.EventFinish, FinishReason: reason, Raw: append([]byte(nil), raw...)}); err != nil {
					return true, err
				}
			}
		}
		return true, nil
	}

	// Be tolerant if Qoder ever starts returning Responses-style inner events.
	if typ := stringValue(obj["type"]); typ != "" {
		switch typ {
		case "response.output_text.delta":
			return true, emit(protocol.Event{Kind: protocol.EventTextDelta, Text: stringValue(obj["delta"]), Raw: append([]byte(nil), raw...)})
		case "response.function_call_arguments.delta":
			return true, emit(protocol.Event{Kind: protocol.EventToolDelta, ToolID: stringValue(obj["item_id"]), ToolArguments: stringValue(obj["delta"]), Raw: append([]byte(nil), raw...)})
		case "response.completed":
			return true, emit(protocol.Event{Kind: protocol.EventFinish, FinishReason: "stop", Raw: append([]byte(nil), raw...)})
		}
	}

	// Qoder has changed the inner envelope shape between client versions. Keep
	// OpenAI protocol handling above strict, but recursively unwrap common
	// transport/container fields instead of silently discarding them.
	for _, key := range []string{"llm_model_result", "data", "result", "payload", "body"} {
		v, exists := obj[key]
		if !exists || v == nil {
			continue
		}
		switch nested := v.(type) {
		case string:
			if strings.TrimSpace(nested) == "" {
				continue
			}
			ok, err := parseInner([]byte(nested), emit)
			if err != nil {
				return true, err
			}
			if ok {
				return true, nil
			}
		case map[string]any, []any:
			b, err := json.Marshal(nested)
			if err != nil {
				continue
			}
			ok, err := parseInner(b, emit)
			if err != nil {
				return true, err
			}
			if ok {
				return true, nil
			}
		}
	}

	// Some gateways emit a simple delta object instead of a full OpenAI chunk.
	if delta, ok := obj["delta"].(map[string]any); ok {
		if text := contentText(delta["content"]); text != "" {
			return true, emit(protocol.Event{Kind: protocol.EventTextDelta, Text: text, Raw: append([]byte(nil), raw...)})
		}
		if text := firstNonEmpty(stringValue(delta["text"]), stringValue(delta["content"])); text != "" {
			return true, emit(protocol.Event{Kind: protocol.EventTextDelta, Text: text, Raw: append([]byte(nil), raw...)})
		}
	}

	if text := firstNonEmpty(stringValue(obj["output_text"]), stringValue(obj["text"])); text != "" {
		return true, emit(protocol.Event{Kind: protocol.EventTextDelta, Text: text, Raw: append([]byte(nil), raw...)})
	}

	if !recognized {
		slog.Debug("qoder inner frame unrecognized", "keys", mapKeys(obj), "bytes", len(raw))
	}
	return recognized, nil
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func parseUsage(obj map[string]any) (protocol.Usage, bool) {
	raw, ok := obj["usage"].(map[string]any)
	if !ok {
		return protocol.Usage{}, false
	}
	in := firstNumber(raw, "prompt_tokens", "input_tokens", "promptTokens", "inputTokens")
	out := firstNumber(raw, "completion_tokens", "output_tokens", "completionTokens", "outputTokens")
	total := firstNumber(raw, "total_tokens", "totalTokens")
	if total == 0 {
		total = in + out
	}
	cached := 0
	if details, ok := raw["prompt_tokens_details"].(map[string]any); ok {
		cached = firstNumber(details, "cached_tokens", "cachedTokens")
	}
	if details, ok := raw["input_tokens_details"].(map[string]any); ok && cached == 0 {
		cached = firstNumber(details, "cached_tokens", "cachedTokens")
	}
	return protocol.Usage{InputTokens: in, OutputTokens: out, CachedTokens: cached, TotalTokens: total}, true
}

func contentText(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	arr, ok := v.([]any)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, raw := range arr {
		if s, ok := raw.(string); ok {
			b.WriteString(s)
			continue
		}
		if m, ok := raw.(map[string]any); ok {
			if s := firstNonEmpty(stringValue(m["text"]), stringValue(m["content"])); s != "" {
				b.WriteString(s)
			}
		}
	return b.String()
}

func firstNumber(m map[string]any, keys ...string) int {
	for _, k := range keys {
		if n := numberAsInt(m[k]); n != 0 {
			return n
		}
	}
	return 0
}

func numberAsInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case json.Number:
		i, _ := strconv.Atoi(n.String())
		return i
	case int:
		return n
	}
	return 0
}

func stringValue(v any) string { s, _ := v.(string); return s }

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func qoderErrorDetails(inner string, status int) (code string, message string) {
	message = strings.TrimSpace(inner)
	if message == "" {
		message = fmt.Sprintf("Qoder status %d", status)
		return "", message
	}
	var body map[string]any
	if json.Unmarshal([]byte(message), &body) != nil {
		return "", message
	}
	code = firstNonEmpty(stringValue(body["code"]), stringValue(body["error_code"]))
	if code == "" {
		if n := numberAsInt(body["code"]); n != 0 {
			code = strconv.Itoa(n)
		}
	}
	if msg := firstNonEmpty(stringValue(body["message"]), stringValue(body["msg"])); msg != "" {
		message = msg
	}
	return code, message
}

func logEnvelopeShape(envelope map[string]any, inner string, hasBody bool) {
	attrs := []any{
		"keys", mapKeys(envelope),
		"status", numberAsInt(envelope["statusCodeValue"]),
		"body_present", hasBody,
		"body_type", fmt.Sprintf("%T", envelope["body"]),
		"body_bytes", len(inner),
		"body_json_valid", json.Valid([]byte(strings.TrimSpace(inner))),
	}
	if strings.TrimSpace(inner) != "" && json.Valid([]byte(strings.TrimSpace(inner))) {
		var body map[string]any
		if json.Unmarshal([]byte(inner), &body) == nil {
			attrs = append(attrs, "body_keys", mapKeys(body))
			if typ := stringValue(body["type"]); typ != "" {
				attrs = append(attrs, "body_event_type", typ)
			}
			if code := firstNonEmpty(stringValue(body["code"]), stringValue(body["error_code"])); code != "" {
				attrs = append(attrs, "body_code", code)
			}
			if _, ok := body["error"]; ok {
				attrs = append(attrs, "body_has_error", true)
			}
			if choices, ok := body["choices"].([]any); ok {
				attrs = append(attrs, "choices", len(choices))
				if len(choices) > 0 {
					if choice, ok := choices[0].(map[string]any); ok {
						attrs = append(attrs, "choice_keys", mapKeys(choice))
						if reason := stringValue(choice["finish_reason"]); reason != "" {
							attrs = append(attrs, "finish_reason", reason)
						}
						if payload, incremental := choicePayload(choice); payload != nil {
							kind := "message"
							if incremental {
								kind = "delta"
							}
							attrs = append(attrs, "choice_payload", kind, "payload_keys", mapKeys(payload))
							if content, exists := payload["content"]; exists {
								attrs = append(attrs, "content_type", fmt.Sprintf("%T", content), "content_bytes", len(contentText(content)))
							}
						}
					}
				}
			}
		}
	}
	slog.Debug("qoder first envelope shape", attrs...)
}

// RelayChatStream mirrors CFlareAIProxy's qoderChatStream behavior: parse the
// outer Qoder SSE envelope and forward envelope.body as OpenAI chat SSE data.
// Full-message choices are normalized to delta form because Qoder occasionally
// mixes non-stream and stream-shaped chunks on the same SSE connection.
func RelayChatStream(r io.Reader, publicModel string, writeData func(string) error) error {
	doneSent := false
	envelopes := 0
	innerFrames := 0
	emptyBodies := 0
	firstEnvelopeLogged := false
	output := newStreamOutputState()
	err := readSSE(r, func(data string) error {
		envelopes++
		if data == "[DONE]" {
			doneSent = true
			return writeData("[DONE]")
		}
		var envelope map[string]any
		if err := json.Unmarshal([]byte(data), &envelope); err != nil {
			slog.Debug("qoder relay ignored malformed envelope", "bytes", len(data))
			return nil
		}
		status := 200
		if n, ok := envelope["statusCodeValue"].(float64); ok {
			status = int(n)
		}
		inner, hasBody := envelopeBodyText(envelope)
		if !firstEnvelopeLogged {
			firstEnvelopeLogged = true
			logEnvelopeShape(envelope, inner, hasBody)
		}
		if status != 200 {
			code, msg := qoderErrorDetails(inner, status)
			upstreamErr := classifyUpstreamError(status, code, msg)
			slog.Warn("qoder stream rejected", "status", status, "business_code", code, "message", msg)
			b, _ := json.Marshal(map[string]any{"error": map[string]any{
				"message": upstreamErr.Message, "type": upstreamErr.Type, "code": upstreamErr.PublicCode,
			}})
			if err := writeData(string(b)); err != nil {
				return err
			}
			return upstreamErr
		}
		if !hasBody || strings.TrimSpace(inner) == "" {
			emptyBodies++
			slog.Debug("qoder relay envelope has no body",
				"status", status,
				"keys", mapKeys(envelope),
				"body_type", fmt.Sprintf("%T", envelope["body"]),
				"message", firstNonEmpty(stringValue(envelope["message"]), stringValue(envelope["msg"]), stringValue(envelope["error"])),
			)
			return nil
		}
		innerFrames++

		forward := inner
		var chunk map[string]any
		if json.Unmarshal([]byte(inner), &chunk) == nil {
			changed := output.normalizeOpenAIChunk(chunk)
			if publicModel != "" {
				if _, exists := chunk["model"]; exists {
					chunk["model"] = publicModel
					changed = true
				}
			}
			if changed {
				if b, err := json.Marshal(chunk); err == nil {
					forward = string(b)
				}
			}
		}
		return writeData(forward)
	})
	if err == nil && !doneSent {
		err = writeData("[DONE]")
	}
	slog.Debug("qoder chat relay completed", "envelopes", envelopes, "inner_frames", innerFrames, "empty_bodies", emptyBodies, "done_sent", doneSent)
	return err
}

type replayReadCloser struct {
	io.Reader
	io.Closer
}

// inspectQueueResponse consumes only the first complete Qoder SSE data event.
// For non-queue responses it replays the consumed bytes so downstream parsing
// sees the original stream unchanged.
func inspectQueueResponse(resp *http.Response) (QueueInfo, bool, error) {
	if resp == nil || resp.Body == nil {
		return QueueInfo{}, false, nil
	}
	br := bufio.NewReader(resp.Body)
	prefix, data, found, err := readFirstSSEData(br)
	if err != nil && err != io.EOF {
		return QueueInfo{}, false, err
	}
	resp.Body = &replayReadCloser{Reader: io.MultiReader(bytes.NewReader(prefix), br), Closer: resp.Body}
	if !found || data == "[DONE]" {
		return QueueInfo{}, false, nil
	}
	info, queued := queueInfoFromEnvelope(data)
	if queued {
		return info, true, nil
	}
	if upstreamErr := upstreamErrorFromEnvelope(data); upstreamErr != nil {
		return QueueInfo{}, false, upstreamErr
	}
	return QueueInfo{}, false, nil
}

func readFirstSSEData(br *bufio.Reader) ([]byte, string, bool, error) {
	var raw bytes.Buffer
	var data []string
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			raw.WriteString(line)
			trimmed := strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(trimmed, "data:") {
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if len(data) == 0 && (value == "[DONE]" || json.Valid([]byte(value))) {
					return raw.Bytes(), value, true, nil
				}
				data = append(data, value)
			}
			if trimmed == "" && len(data) > 0 {
				return raw.Bytes(), strings.Join(data, "\n"), true, nil
			}
		}
		if err != nil {
			if len(data) > 0 {
				return raw.Bytes(), strings.Join(data, "\n"), true, nil
			}
			return raw.Bytes(), "", false, err
		}
	}
}

func queueInfoFromEnvelope(data string) (QueueInfo, bool) {
	var envelope map[string]any
	if json.Unmarshal([]byte(data), &envelope) != nil {
		return QueueInfo{}, false
	}
	status := numberAsInt(envelope["statusCodeValue"])
	if status == 0 {
		status = 200
	}
	inner, _ := envelopeBodyText(envelope)
	info := QueueInfo{Status: status, Message: strings.TrimSpace(inner)}
	maps := nestedJSONMaps(inner, 5)
	for _, m := range maps {
		if code := anyCode(m["code"]); code != "" {
			info.Code = code
		}
		if model := firstNonEmpty(stringValue(m["modelKey"]), stringValue(m["model_key"])); model != "" {
			info.ModelKey = model
		}
		if n := numberAsInt(m["queueCount"]); n != 0 {
			info.QueueCount = n
		}
		if typ := stringValue(m["queueType"]); typ != "" {
			info.QueueType = typ
		}
		if n := numberAsInt(m["retryAfterSeconds"]); n != 0 {
			info.RetryAfterSeconds = n
		}
		if n := numberAsInt(m["waitTime"]); n != 0 {
			info.WaitTime = n
		}
		if v, ok := m["serviceAvailable"].(bool); ok {
			info.ServiceAvailable = v
		}
		if msg := firstNonEmpty(stringValue(m["message"]), stringValue(m["msg"])); msg != "" {
			info.Message = msg
		}
		if queued, _ := m["isQueued"].(bool); queued {
			if info.RetryAfterSeconds <= 0 {
				info.RetryAfterSeconds = 30
			}
			return info, true
		}
	}
	if info.Code == "10605" {
		if info.RetryAfterSeconds <= 0 {
			info.RetryAfterSeconds = 30
		}
		return info, true
	}
	return info, false
}

func nestedJSONMaps(input string, maxDepth int) []map[string]any {
	var out []map[string]any
	current := strings.TrimSpace(input)
	for i := 0; i < maxDepth && current != ""; i++ {
		var m map[string]any
		if json.Unmarshal([]byte(current), &m) != nil {
			break
		}
		out = append(out, m)
		next := firstNonEmpty(stringValue(m["message"]), stringValue(m["msg"]), stringValue(m["body"]), stringValue(m["data"]))
		if !json.Valid([]byte(strings.TrimSpace(next))) {
			break
		}
		current = next
	}
	return out
}

func anyCode(v any) string {
	if s := stringValue(v); s != "" {
		return s
	}
	if n := numberAsInt(v); n != 0 {
		return strconv.Itoa(n)
	}
	return ""
}
