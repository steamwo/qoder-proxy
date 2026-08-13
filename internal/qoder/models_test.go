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

func TestModelWithoutThinkingConfigIgnoresDownstreamEffort(t *testing.T) {
	model := Model{DisplayName: "Plain Model", Raw: map[string]any{"key": "plain"}}
	for _, input := range []string{"high", "medium", "low", "none", "off"} {
		got, err := model.NormalizeReasoningEffort(input)
		if err != nil {
			t.Fatalf("NormalizeReasoningEffort(%q): %v", input, err)
		}
		if got != "" {
			t.Fatalf("NormalizeReasoningEffort(%q)=%q, want suppressed", input, got)
		}
	}
}

func TestModelWithOnlyThinkingToggleIgnoresDepthButAllowsDisable(t *testing.T) {
	model := Model{
		DisplayName: "Toggle Model",
		Raw: map[string]any{"thinking_config": map[string]any{
			"disabled": map[string]any{},
			"enabled":  map[string]any{},
		}},
	}
	for _, input := range []string{"high", "medium", "low", "minimal"} {
		got, err := model.NormalizeReasoningEffort(input)
		if err != nil {
			t.Fatalf("NormalizeReasoningEffort(%q): %v", input, err)
		}
		if got != "" {
			t.Fatalf("NormalizeReasoningEffort(%q)=%q, want suppressed", input, got)
		}
	}
	for _, input := range []string{"none", "off"} {
		got, err := model.NormalizeReasoningEffort(input)
		if err != nil || got != "none" {
			t.Fatalf("NormalizeReasoningEffort(%q)=(%q, %v), want none", input, got, err)
		}
	}
}

// TestModelDefaultReasoningEffort verifies omission uses the saved default while explicit input wins.
// TestModelDefaultReasoningEffort 验证请求省略时使用已保存默认值，而显式输入优先。
func TestModelDefaultReasoningEffort(t *testing.T) {
	model := Model{
		DisplayName:            "Reason Model",
		DefaultReasoningEffort: "high",
		Raw: map[string]any{"thinking_config": map[string]any{
			"disabled": map[string]any{},
			"enabled":  map[string]any{"efforts": map[string]any{"low": map[string]any{}, "high": map[string]any{}}},
		}},
	}
	if got, err := model.NormalizeReasoningEffort(""); err != nil || got != "high" {
		t.Fatalf("default effort=(%q, %v), want high", got, err)
	}
	if got, err := model.NormalizeReasoningEffort("low"); err != nil || got != "low" {
		t.Fatalf("explicit effort=(%q, %v), want low", got, err)
	}
	if got, err := model.NormalizeReasoningEffort("auto"); err != nil || got != "" {
		t.Fatalf("explicit auto=(%q, %v), want empty", got, err)
	}
}

func TestPlainModelSavedReasoningDefaultIsAlsoSuppressed(t *testing.T) {
	model := Model{
		DisplayName:            "Plain Model",
		DefaultReasoningEffort: "high",
		Raw:                    map[string]any{"key": "plain"},
	}
	if got, err := model.NormalizeReasoningEffort(""); err != nil || got != "" {
		t.Fatalf("plain saved default=(%q, %v), want suppressed", got, err)
	}
}

// TestStaleModelDefaultFallsBackToAuto protects traffic when Qoder changes model capabilities.
// TestStaleModelDefaultFallsBackToAuto 在 Qoder 改变模型能力时保护请求流量。
func TestStaleModelDefaultFallsBackToAuto(t *testing.T) {
	model := Model{DisplayName: "Reason Model", DefaultReasoningEffort: "high", Raw: map[string]any{"thinking_config": map[string]any{
		"enabled": map[string]any{"efforts": map[string]any{"low": map[string]any{}}},
	}}}
	if got, err := model.NormalizeReasoningEffort(""); err != nil || got != "" {
		t.Fatalf("stale default=(%q, %v), want auto fallback", got, err)
	}
}
