package qoder

import (
	"fmt"
	"sort"
	"strings"
)

// ContextWindowOption is one live Qoder context_config tier.
type ContextWindowOption struct {
	Label      string `json:"label"`
	TokenCount int    `json:"token_count"`
	IsDefault  bool   `json:"is_default,omitempty"`
}

func contextConfig(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	config, _ := raw["context_config"].(map[string]any)
	return config
}

func contextWindows(raw map[string]any) []ContextWindowOption {
	config := contextConfig(raw)
	if len(config) == 0 {
		return nil
	}
	options := make([]ContextWindowOption, 0, len(config))
	for label, rawEntry := range config {
		entry, _ := rawEntry.(map[string]any)
		if entry == nil {
			continue
		}
		tokens := intField(entry, "token_count")
		if tokens <= 0 {
			continue
		}
		options = append(options, ContextWindowOption{
			Label:      strings.TrimSpace(label),
			TokenCount: tokens,
			IsDefault:  boolField(entry, "is_default"),
		})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].TokenCount == options[j].TokenCount {
			return options[i].Label < options[j].Label
		}
		return options[i].TokenCount < options[j].TokenCount
	})
	return options
}

// SupportedContextWindows returns the model's live Qoder context_config tiers.
func (m Model) SupportedContextWindows() []ContextWindowOption {
	return contextWindows(m.Raw)
}

// MaxContextTokens reports the largest configurable context tier when present,
// otherwise it falls back to Qoder's legacy max_input_tokens field.
func (m Model) MaxContextTokens() int {
	options := m.SupportedContextWindows()
	if len(options) == 0 {
		return m.MaxInputTokens
	}
	return options[len(options)-1].TokenCount
}

// NormalizeContextWindow validates an explicit per-model choice. Zero means auto.
func (m Model) NormalizeContextWindow(value int) (int, error) {
	return ResolveContextWindow(m.Raw, value, 0)
}

// ResolveContextWindow combines an explicit request value and a saved default.
// Invalid explicit values are rejected. Invalid saved defaults silently fall
// back to auto so server-side capability changes cannot break traffic.
func ResolveContextWindow(raw map[string]any, requested, savedDefault int) (int, error) {
	selected := requested
	usingSavedDefault := false
	if selected <= 0 {
		selected = savedDefault
		usingSavedDefault = selected > 0
	}
	if selected <= 0 {
		return 0, nil
	}

	options := contextWindows(raw)
	if len(options) == 0 {
		if usingSavedDefault {
			return 0, nil
		}
		return 0, fmt.Errorf("model does not support configurable context window")
	}
	for _, option := range options {
		if selected == option.TokenCount {
			return selected, nil
		}
	}
	if usingSavedDefault {
		return 0, nil
	}

	labels := make([]string, 0, len(options))
	for _, option := range options {
		if option.Label != "" {
			labels = append(labels, fmt.Sprintf("%s (%d)", option.Label, option.TokenCount))
		} else {
			labels = append(labels, fmt.Sprintf("%d", option.TokenCount))
		}
	}
	return 0, fmt.Errorf("unsupported context window %d; supported: %s", selected, strings.Join(labels, ", "))
}
