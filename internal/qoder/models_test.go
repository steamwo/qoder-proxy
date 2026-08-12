package qoder

import "testing"

func TestModelItemsMapUsesMapKeyAsUpstreamID(t *testing.T) {
	items := modelItems(map[string]any{
		"anon-2": map[string]any{"display_name": "Claude Sonnet"},
		"anon-1": map[string]any{"display_name": "GPT"},
	})
	if len(items) != 2 {
		t.Fatalf("len=%d", len(items))
	}
	if got := firstString(items[0], "key"); got != "anon-1" {
		t.Fatalf("first key=%q", got)
	}
	if got := firstString(items[1], "key"); got != "anon-2" {
		t.Fatalf("second key=%q", got)
	}
}

func TestModelReasoningEffortCapabilities(t *testing.T) {
	model := Model{
		DisplayName: "Reasoning Model",
		Raw: map[string]any{
			"thinking_config": map[string]any{
				"disabled": map[string]any{"description": "off"},
				"enabled": map[string]any{
					"efforts": map[string]any{
						"high":   map[string]any{},
						"low":    map[string]any{},
						"medium": map[string]any{},
					},
				},
			},
		},
	}
	got := model.SupportedReasoningEfforts()
	want := []string{"high", "low", "medium"}
	if len(got) != len(want) {
		t.Fatalf("efforts=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("efforts=%v", got)
		}
	}
	if !model.SupportsReasoningDisabled() {
		t.Fatal("expected disabled thinking support")
	}
	for input, wantEffort := range map[string]string{"HIGH": "high", "off": "none", "none": "none", "auto": ""} {
		gotEffort, err := model.NormalizeReasoningEffort(input)
		if err != nil {
			t.Fatalf("NormalizeReasoningEffort(%q): %v", input, err)
		}
		if gotEffort != wantEffort {
			t.Fatalf("NormalizeReasoningEffort(%q)=%q want %q", input, gotEffort, wantEffort)
		}
	}
	if _, err := model.NormalizeReasoningEffort("xhigh"); err == nil {
		t.Fatal("expected unsupported effort error")
	}
}

func TestModelWithoutThinkingConfigRejectsEffort(t *testing.T) {
	model := Model{DisplayName: "Plain Model", Raw: map[string]any{"key": "plain"}}
	if _, err := model.NormalizeReasoningEffort("high"); err == nil {
		t.Fatal("expected configurable reasoning error")
	}
}
