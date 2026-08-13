package qoder

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestUsageObserverPassesStreamThroughAndCountsOnce(t *testing.T) {
	inner := `{"choices":[{"index":0,"delta":{"content":"ok"}}],"usage":{"prompt_tokens":1234,"completion_tokens":56,"total_tokens":1290}}`
	envelope, err := json.Marshal(map[string]any{"statusCodeValue": 200, "body": inner})
	if err != nil {
		t.Fatal(err)
	}
	raw := "data: " + string(envelope) + "\n\ndata: [DONE]\n\n"

	calls := 0
	var got protocol.Usage
	body := observeUsageBody(io.NopCloser(strings.NewReader(raw)), func(usage protocol.Usage) {
		calls++
		got = usage
	})
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(data) != raw {
		t.Fatalf("usage observer changed stream bytes:\n got %q\nwant %q", string(data), raw)
	}
	if calls != 1 {
		t.Fatalf("observer calls=%d want=1", calls)
	}
	if got.InputTokens != 1234 || got.OutputTokens != 56 || got.TotalTokens != 1290 {
		t.Fatalf("usage=%+v", got)
	}
}

func TestUsageObserverIgnoresStreamsWithoutUsage(t *testing.T) {
	inner := `{"choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`
	envelope, err := json.Marshal(map[string]any{"statusCodeValue": 200, "body": inner})
	if err != nil {
		t.Fatal(err)
	}
	raw := "data: " + string(envelope) + "\n\ndata: [DONE]\n\n"

	calls := 0
	body := observeUsageBody(io.NopCloser(strings.NewReader(raw)), func(protocol.Usage) { calls++ })
	if _, err := io.ReadAll(body); err != nil {
		t.Fatal(err)
	}
	if err := body.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("observer calls=%d want=0", calls)
	}
}
