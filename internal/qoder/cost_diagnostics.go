package qoder

import (
	"context"
	"log"
	"log/slog"
	"sync"
)

// costDiagnosticHandler enriches the existing content-safe Qoder request/usage
// logs with cross-turn deltas. It never sees or logs prompt text, tool schemas,
// message bodies, or image data; it only correlates byte/token counts and stable
// hashes that the Qoder client already emits.
type costDiagnosticHandler struct {
	next  slog.Handler
	state *costDiagnosticState
}

type costDiagnosticState struct {
	mu       sync.Mutex
	sessions map[string]costSessionSnapshot
}

type costSessionSnapshot struct {
	messagesBytes    int64
	messageCount     int64
	promptPrefixHash string
}

func init() {
	base := slog.Default()

	// slog.SetDefault bridges the standard log package back through the new slog
	// handler when the handler is not Go's built-in default handler. Because this
	// wrapper delegates to that built-in handler, leaving the bridge installed
	// would form a log -> slog -> defaultHandler -> log recursion. Preserve and
	// restore the standard logger state around SetDefault to keep the original
	// output behavior while safely enriching slog records.
	standardWriter := log.Writer()
	standardFlags := log.Flags()
	standardPrefix := log.Prefix()

	slog.SetDefault(slog.New(&costDiagnosticHandler{
		next: base.Handler(),
		state: &costDiagnosticState{
			sessions: make(map[string]costSessionSnapshot),
		},
	}))

	log.SetOutput(standardWriter)
	log.SetFlags(standardFlags)
	log.SetPrefix(standardPrefix)
}

func (h *costDiagnosticHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *costDiagnosticHandler) Handle(ctx context.Context, record slog.Record) error {
	switch record.Message {
	case "qoder request shape":
		h.enrichRequestShape(&record)
	case "qoder usage":
		enrichUsageRatios(&record)
	}
	return h.next.Handle(ctx, record)
}

func (h *costDiagnosticHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &costDiagnosticHandler{next: h.next.WithAttrs(attrs), state: h.state}
}

func (h *costDiagnosticHandler) WithGroup(name string) slog.Handler {
	return &costDiagnosticHandler{next: h.next.WithGroup(name), state: h.state}
}

func (h *costDiagnosticHandler) enrichRequestShape(record *slog.Record) {
	protocolLabel, _ := recordString(record, "protocol")
	sessionID, _ := recordString(record, "session_id")
	messagesBytes, haveMessagesBytes := recordInt64(record, "messages_json_bytes")
	messageCount, haveMessageCount := recordInt64(record, "messages")
	promptPrefixHash, _ := recordString(record, "prompt_prefix_hash")
	if sessionID == "" || !haveMessagesBytes {
		return
	}

	key := protocolLabel + "\x00" + sessionID
	next := costSessionSnapshot{
		messagesBytes:    messagesBytes,
		messageCount:     messageCount,
		promptPrefixHash: promptPrefixHash,
	}

	h.state.mu.Lock()
	previous, found := h.state.sessions[key]
	h.state.sessions[key] = next
	h.state.mu.Unlock()

	if !found {
		record.AddAttrs(slog.Bool("history_baseline", true))
		return
	}

	record.AddAttrs(
		slog.Bool("history_baseline", false),
		slog.Int64("history_previous_bytes", previous.messagesBytes),
		slog.Int64("history_growth_bytes", messagesBytes-previous.messagesBytes),
	)
	if haveMessageCount {
		record.AddAttrs(slog.Int64("message_count_growth", messageCount-previous.messageCount))
	}
	if promptPrefixHash != "" && previous.promptPrefixHash != "" {
		record.AddAttrs(slog.Bool("prompt_prefix_reused", promptPrefixHash == previous.promptPrefixHash))
	}
}

func enrichUsageRatios(record *slog.Record) {
	input, haveInput := recordInt64(record, "input_tokens")
	cached, haveCached := recordInt64(record, "cached_tokens")
	uncached, haveUncached := recordInt64(record, "uncached_input_tokens")
	if !haveInput || input <= 0 {
		return
	}
	if !haveCached {
		cached = 0
	}
	if !haveUncached {
		uncached = input - cached
		if uncached < 0 {
			uncached = 0
		}
	}

	record.AddAttrs(
		slog.Bool("cache_hit", cached > 0),
		slog.Float64("cached_ratio_pct", float64(cached)*100/float64(input)),
		slog.Float64("uncached_ratio_pct", float64(uncached)*100/float64(input)),
	)
}

func recordString(record *slog.Record, key string) (string, bool) {
	var value string
	found := false
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key != key {
			return true
		}
		resolved := attr.Value.Resolve()
		if resolved.Kind() != slog.KindString {
			return false
		}
		value = resolved.String()
		found = true
		return false
	})
	return value, found
}

func recordInt64(record *slog.Record, key string) (int64, bool) {
	var value int64
	found := false
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key != key {
			return true
		}
		resolved := attr.Value.Resolve()
		switch resolved.Kind() {
		case slog.KindInt64:
			value = resolved.Int64()
			found = true
		case slog.KindUint64:
			u := resolved.Uint64()
			if u <= uint64(^uint64(0)>>1) {
				value = int64(u)
				found = true
			}
		}
		return false
	})
	return value, found
}
