package qoder

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

type captureHandler struct {
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, record slog.Record) error {
	h.records = append(h.records, record.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func TestCostDiagnosticsTrackHistoryGrowth(t *testing.T) {
	capture := &captureHandler{}
	handler := &costDiagnosticHandler{
		next: capture,
		state: &costDiagnosticState{
			sessions: make(map[string]costSessionSnapshot),
		},
	}

	first := slog.NewRecord(testTime(), slog.LevelInfo, "qoder request shape", 0)
	first.AddAttrs(
		slog.String("protocol", "openai_responses"),
		slog.String("session_id", "session-1"),
		slog.Int("messages_json_bytes", 1200),
		slog.Int("messages", 4),
		slog.String("prompt_prefix_hash", "prefix-a"),
	)
	if err := handler.Handle(context.Background(), first); err != nil {
		t.Fatal(err)
	}

	second := slog.NewRecord(testTime(), slog.LevelInfo, "qoder request shape", 0)
	second.AddAttrs(
		slog.String("protocol", "openai_responses"),
		slog.String("session_id", "session-1"),
		slog.Int("messages_json_bytes", 3500),
		slog.Int("messages", 6),
		slog.String("prompt_prefix_hash", "prefix-a"),
	)
	if err := handler.Handle(context.Background(), second); err != nil {
		t.Fatal(err)
	}

	if len(capture.records) != 2 {
		t.Fatalf("records=%d want 2", len(capture.records))
	}
	attrs := recordAttrs(capture.records[1])
	if got := attrs["history_growth_bytes"]; got != int64(2300) {
		t.Fatalf("history_growth_bytes=%#v", got)
	}
	if got := attrs["message_count_growth"]; got != int64(2) {
		t.Fatalf("message_count_growth=%#v", got)
	}
	if got := attrs["prompt_prefix_reused"]; got != true {
		t.Fatalf("prompt_prefix_reused=%#v", got)
	}
}

func TestCostDiagnosticsAddCacheRatios(t *testing.T) {
	record := slog.NewRecord(testTime(), slog.LevelInfo, "qoder usage", 0)
	record.AddAttrs(
		slog.Int("input_tokens", 1000),
		slog.Int("cached_tokens", 900),
		slog.Int("uncached_input_tokens", 100),
	)
	enrichUsageRatios(&record)
	attrs := recordAttrs(record)
	if got := attrs["cache_hit"]; got != true {
		t.Fatalf("cache_hit=%#v", got)
	}
	if got := attrs["cached_ratio_pct"]; got != float64(90) {
		t.Fatalf("cached_ratio_pct=%#v", got)
	}
	if got := attrs["uncached_ratio_pct"]; got != float64(10) {
		t.Fatalf("uncached_ratio_pct=%#v", got)
	}
}

func recordAttrs(record slog.Record) map[string]any {
	out := make(map[string]any)
	record.Attrs(func(attr slog.Attr) bool {
		value := attr.Value.Resolve()
		switch value.Kind() {
		case slog.KindString:
			out[attr.Key] = value.String()
		case slog.KindBool:
			out[attr.Key] = value.Bool()
		case slog.KindInt64:
			out[attr.Key] = value.Int64()
		case slog.KindUint64:
			out[attr.Key] = value.Uint64()
		case slog.KindFloat64:
			out[attr.Key] = value.Float64()
		default:
			out[attr.Key] = value.Any()
		}
		return true
	})
	return out
}

func testTime() (zeroTime time.Time) { return zeroTime }
