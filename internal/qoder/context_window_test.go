package qoder

import "testing"

func contextTestRaw() map[string]any {
	return map[string]any{
		"context_config": map[string]any{
			"200K": map[string]any{"token_count": 200000, "is_default": true},
			"400K": map[string]any{"token_count": 400000},
			"1M":   map[string]any{"token_count": 1000000},
		},
	}
}

func TestSupportedContextWindowsAndMax(t *testing.T) {
	model := Model{MaxInputTokens: 180000, Raw: contextTestRaw()}
	options := model.SupportedContextWindows()
	if len(options) != 3 {
		t.Fatalf("options=%v", options)
	}
	if options[0].Label != "200K" || options[0].TokenCount != 200000 || !options[0].IsDefault {
		t.Fatalf("first option=%+v", options[0])
	}
	if options[2].Label != "1M" || options[2].TokenCount != 1000000 {
		t.Fatalf("last option=%+v", options[2])
	}
	if got := model.MaxContextTokens(); got != 1000000 {
		t.Fatalf("MaxContextTokens=%d", got)
	}
}

func TestContextWindowExplicitValidation(t *testing.T) {
	model := Model{DisplayName: "Context Model", Raw: contextTestRaw()}
	for _, value := range []int{0, 200000, 400000, 1000000} {
		got, err := model.NormalizeContextWindow(value)
		if err != nil || got != value {
			t.Fatalf("NormalizeContextWindow(%d)=(%d,%v)", value, got, err)
		}
	}
	if _, err := model.NormalizeContextWindow(180000); err == nil {
		t.Fatal("expected unsupported context window error")
	}
}

func TestSavedContextWindowFallsBackToAutoWhenCapabilitiesChange(t *testing.T) {
	got, err := ResolveContextWindow(contextTestRaw(), 0, 180000)
	if err != nil || got != 0 {
		t.Fatalf("stale saved default=(%d,%v), want auto", got, err)
	}
	got, err = ResolveContextWindow(contextTestRaw(), 0, 1000000)
	if err != nil || got != 1000000 {
		t.Fatalf("saved 1M default=(%d,%v)", got, err)
	}
}

func TestLegacyModelUsesMaxInputOnlyForDisplay(t *testing.T) {
	model := Model{MaxInputTokens: 180000, Raw: map[string]any{"max_input_tokens": 180000}}
	if got := model.MaxContextTokens(); got != 180000 {
		t.Fatalf("MaxContextTokens=%d", got)
	}
	if _, err := model.NormalizeContextWindow(200000); err == nil {
		t.Fatal("expected explicit context window rejection without context_config")
	}
	if got, err := ResolveContextWindow(model.Raw, 0, 200000); err != nil || got != 0 {
		t.Fatalf("saved default on legacy model=(%d,%v), want auto", got, err)
	}
}
