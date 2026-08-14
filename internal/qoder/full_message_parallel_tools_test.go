package qoder

import (
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestParseStreamAssignsIndexesToParallelFullMessageTools(t *testing.T) {
	stream := outer(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_a","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"a\"}"}},{"id":"call_b","type":"function","function":{"name":"Read","arguments":"{\"file_path\":\"b\"}"}}]},"finish_reason":"tool_calls"}]}`) +
		"data: [DONE]\n\n"

	got := map[int]string{}
	ids := map[int]string{}
	if err := ParseStream(strings.NewReader(stream), func(ev protocol.Event) error {
		if ev.Kind == protocol.EventToolDelta {
			got[ev.ToolIndex] += ev.ToolArguments
			ids[ev.ToolIndex] = ev.ToolID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if ids[0] != "call_a" || !strings.Contains(got[0], `"a"`) {
		t.Fatalf("tool 0 id=%q args=%q", ids[0], got[0])
	}
	if ids[1] != "call_b" || !strings.Contains(got[1], `"b"`) {
		t.Fatalf("tool 1 id=%q args=%q", ids[1], got[1])
	}
}
