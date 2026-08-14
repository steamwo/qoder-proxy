package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type unifiedModelContextView struct {
	ModelID          string                      `json:"model_id"`
	MaxContextTokens int                         `json:"max_context_tokens"`
	Options          []qoder.ContextWindowOption `json:"options"`
	Default          int                         `json:"default"`
}

type unifiedModelContextDefaultRequest struct {
	ModelID       string `json:"model_id"`
	ContextWindow int    `json:"context_window"`
}

func (s *unifiedService) handleUnifiedModelContexts(w http.ResponseWriter, r *http.Request) {
	cred, err := s.store.Load()
	if err != nil || cred.Expired() {
		unifiedWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorize Qoder first"})
		return
	}
	settings := LoadSettings()
	s.mu.RLock()
	backend := s.backend
	s.mu.RUnlock()
	registry := qoder.NewRegistry(http.DefaultClient, cred)
	if backend != nil {
		registry = backend.Registry
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	models, err := registry.List(ctx)
	if err != nil {
		unifiedWriteJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	out := make([]unifiedModelContextView, 0, len(models))
	for _, model := range models {
		out = append(out, unifiedModelContextView{
			ModelID:          model.UpstreamID,
			MaxContextTokens: model.MaxContextTokens(),
			Options:          model.SupportedContextWindows(),
			Default:          settings.ModelContextDefaults[model.UpstreamID],
		})
	}
	unifiedWriteJSON(w, http.StatusOK, out)
}

func (s *unifiedService) handleUnifiedModelContextDefault(w http.ResponseWriter, r *http.Request) {
	var req unifiedModelContextDefaultRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&req); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid model context default payload"})
		return
	}
	cred, err := s.store.Load()
	if err != nil || cred.Expired() {
		unifiedWriteJSON(w, http.StatusUnauthorized, map[string]string{"error": "authorize Qoder first"})
		return
	}
	registry := qoder.NewRegistry(http.DefaultClient, cred)
	s.mu.RLock()
	if s.backend != nil {
		registry = s.backend.Registry
	}
	s.mu.RUnlock()
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	model, err := registry.Resolve(ctx, req.ModelID)
	if err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if _, err := model.NormalizeContextWindow(req.ContextWindow); err != nil {
		unifiedWriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	settings := LoadSettings()
	if settings.ModelContextDefaults == nil {
		settings.ModelContextDefaults = map[string]int{}
	}
	if req.ContextWindow <= 0 {
		delete(settings.ModelContextDefaults, model.UpstreamID)
	} else {
		settings.ModelContextDefaults[model.UpstreamID] = req.ContextWindow
	}
	if err := SaveSettings(settings); err != nil {
		unifiedWriteJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.mu.RLock()
	backend := s.backend
	s.mu.RUnlock()
	if backend != nil {
		backend.SetModelContextDefaults(settings.ModelContextDefaults)
	}
	unifiedWriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
