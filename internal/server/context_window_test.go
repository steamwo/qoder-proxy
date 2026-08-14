package server

import (
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func serverContextRaw() map[string]any {
	return map[string]any{
		"context_config": map[string]any{
			"200K": map[string]any{"token_count": 200000},
			"1M":   map[string]any{"token_count": 1000000},
		},
	}
}

func TestBackendAppliesSavedContextDefaultAcrossProtocolBoundary(t *testing.T) {
	backend := &Backend{modelContextDefaults: map[string]int{"model-id": 1000000}}
	got, err := backend.applyContextDefault(protocol.Request{ModelID: "model-id", ModelConfig: serverContextRaw()})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextWindow != 1000000 {
		t.Fatalf("ContextWindow=%d", got.ContextWindow)
	}
}

func TestBackendStaleSavedContextFallsBackToAuto(t *testing.T) {
	backend := &Backend{modelContextDefaults: map[string]int{"model-id": 400000}}
	got, err := backend.applyContextDefault(protocol.Request{ModelID: "model-id", ModelConfig: serverContextRaw()})
	if err != nil {
		t.Fatal(err)
	}
	if got.ContextWindow != 0 {
		t.Fatalf("ContextWindow=%d, want auto", got.ContextWindow)
	}
}
