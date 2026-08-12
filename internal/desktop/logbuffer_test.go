package desktop

import "testing"

// TestLogBufferEntriesAndStats verifies reasoning metadata remains visible without changing request statistics.
// TestLogBufferEntriesAndStats 验证思考元数据保持可见，且不改变请求统计结果。
func TestLogBufferEntriesAndStats(t *testing.T) {
	buffer := NewLogBuffer(4096)
	_, _ = buffer.Write([]byte("time=2026-08-13T10:24:28Z level=INFO msg=\"qoder response\" method=POST path=/v1/chat/completions model=gpt-4o reasoning_effort=high status=200 duration_ms=286 remote=127.0.0.1:53421\n"))
	_, _ = buffer.Write([]byte("time=2026-08-13T10:24:29Z level=ERROR msg=\"request completed\" method=POST path=/v1/chat/completions model=gpt-4o status=500 duration_ms=1862 remote=127.0.0.1:53422\n"))

	entries := buffer.Entries(10)
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Status != 500 || entries[0].DurationMS != 1862 {
		t.Fatalf("newest entry = %#v", entries[0])
	}
	if entries[1].Path != "/v1/chat/completions" || entries[1].Model != "gpt-4o" {
		t.Fatalf("parsed entry = %#v", entries[1])
	}
	if entries[1].ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %q, want high", entries[1].ReasoningEffort)
	}

	stats := buffer.Stats()
	if stats.Requests != 2 || stats.Errors != 1 || stats.AverageMillis != 1074 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestLogBufferClear(t *testing.T) {
	buffer := NewLogBuffer(128)
	_, _ = buffer.Write([]byte("level=INFO msg=ready\n"))
	buffer.Clear()
	if got := buffer.Entries(0); len(got) != 0 {
		t.Fatalf("entries after clear = %d, want 0", len(got))
	}
}
