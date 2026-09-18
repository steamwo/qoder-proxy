package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

// Model carries live Qoder capabilities plus an optional local default used by desktop requests.
// Model 携带 Qoder 实时能力，以及桌面请求可选的本地默认值。
type Model struct {
	UpstreamID             string         `json:"upstream_id"`
	DisplayName            string         `json:"display_name"`
	Source                 string         `json:"source,omitempty"`
	IsReasoning            bool           `json:"is_reasoning,omitempty"`
	IsVL                   bool           `json:"is_vl,omitempty"`
	MaxInputTokens         int            `json:"max_input_tokens,omitempty"`
	MaxOutputTokens        int            `json:"max_output_tokens,omitempty"`
	Raw                    map[string]any `json:"-"`
	DefaultReasoningEffort string         `json:"-"`
}

// SupportedReasoningEfforts returns deterministic live choices so callers never persist stale capabilities.
// SupportedReasoningEfforts 返回确定顺序的实时选项，避免调用方持久化过期能力。
func (m Model) SupportedReasoningEfforts() []string {
	return reasoningEfforts(m.Raw)
}

// SupportsReasoningDisabled reports the live off capability because not every model accepts "none".
// SupportsReasoningDisabled 返回实时关闭能力，因为并非所有模型都接受 "none"。
func (m Model) SupportsReasoningDisabled() bool {
	return reasoningDisabled(m.Raw)
}

// NormalizeReasoningEffort applies the model default only when the request omitted a value, then validates it.
// Models without configurable depth silently ignore downstream depth hints instead of forwarding unsupported
// reasoningEffort parameters to Qoder. A model that explicitly supports disabling thinking may still accept none/off.
// NormalizeReasoningEffort 仅在请求未提供值时应用模型默认值；不支持思考深度的模型会静默忽略下游深度参数，
// 避免向 Qoder 透传不支持的 reasoningEffort。若模型明确支持关闭思考，none/off 仍可生效。
func (m Model) NormalizeReasoningEffort(value string) (string, error) {
	requested := strings.TrimSpace(value)
	usingDefault := requested == ""
	// An explicit auto/default must bypass the saved default so clients retain per-request control.
	// 显式 auto/default 必须绕过已保存默认值，确保客户端仍可逐请求控制。
	if requested == "" {
		requested = m.DefaultReasoningEffort
	}
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "", "auto", "default":
		return "", nil
	case "off":
		requested = "none"
	}

	efforts := m.SupportedReasoningEfforts()
	supportsDisabled := m.SupportsReasoningDisabled()

	// No live thinking controls at all: discard every downstream/default hint.
	// This is intentionally not an error because compatibility clients may send
	// reasoning_effort globally even when the selected model cannot consume it.
	if len(efforts) == 0 && !supportsDisabled {
		return "", nil
	}

	if requested == "none" {
		if supportsDisabled {
			return "none", nil
		}
		// A stale saved default must not break traffic after upstream capabilities change.
		// 上游能力变化后，过期的已保存默认值不能导致请求中断。
		if usingDefault {
			return "", nil
		}
		return "", fmt.Errorf("model %q does not support disabling thinking", m.DisplayName)
	}

	// Some models expose only an on/off thinking control and no depth/effort
	// choices. Ignore low/medium/high/etc. rather than leaking an unsupported
	// reasoningEffort parameter upstream.
	if len(efforts) == 0 {
		return "", nil
	}
	for _, effort := range efforts {
		if requested == strings.ToLower(effort) {
			return effort, nil
		}
	}
	if usingDefault {
		return "", nil
	}
	return "", fmt.Errorf("model %q does not support reasoning effort %q; supported efforts: %s", m.DisplayName, value, strings.Join(efforts, ", "))
}

func thinkingConfig(raw map[string]any) map[string]any {
	if raw == nil {
		return nil
	}
	v, _ := raw["thinking_config"].(map[string]any)
	return v
}

func reasoningDisabled(raw map[string]any) bool {
	config := thinkingConfig(raw)
	if config == nil {
		return false
	}
	disabled, ok := config["disabled"]
	return ok && disabled != nil
}

func reasoningEfforts(raw map[string]any) []string {
	config := thinkingConfig(raw)
	if config == nil {
		return nil
	}
	enabled, _ := config["enabled"].(map[string]any)
	if enabled == nil {
		return nil
	}
	efforts, _ := enabled["efforts"].(map[string]any)
	if len(efforts) == 0 {
		return nil
	}
	keys := make([]string, 0, len(efforts))
	for key := range efforts {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

type Registry struct {
	client *http.Client
	cred   credential.Credential
	auth   *AuthState

	mu        sync.RWMutex
	models    []Model
	byDisplay map[string]Model
	byID      map[string]Model
	fetchedAt time.Time
	ttl       time.Duration
}

func NewRegistry(client *http.Client, cred credential.Credential) *Registry {
	return NewRegistryWithAuth(client, NewAuthState(client, cred))
}

func NewRegistryWithAuth(client *http.Client, auth *AuthState) *Registry {
	if client == nil {
		client = http.DefaultClient
	}
	var cred credential.Credential
	if auth != nil {
		cred = auth.CredentialSnapshot()
	}
	return &Registry{client: client, cred: cred, auth: auth, ttl: 5 * time.Minute}
}

func (r *Registry) Refresh(ctx context.Context) error {
	started := time.Now()
	slog.Debug("refreshing qoder models")
	cred := r.cred
	var err error
	if r.auth != nil {
		cred, err = r.auth.Credential(ctx)
		if err != nil {
			slog.Error("qoder model authentication refresh failed", "duration_ms", time.Since(started).Milliseconds(), "error", err)
			return err
		}
	}
	models, err := fetchModels(ctx, r.client, cred)
	if err != nil {
		slog.Error("qoder model refresh failed", "duration_ms", time.Since(started).Milliseconds(), "error", err)
		return err
	}
	byDisplay := make(map[string]Model, len(models))
	byID := make(map[string]Model, len(models))
	for _, m := range models {
		byID[m.UpstreamID] = m
		if existing, ok := byDisplay[m.DisplayName]; !ok || m.UpstreamID < existing.UpstreamID {
			// A public OpenAI model ID must be unique. If Qoder ever returns the same
			// display_name for multiple internal IDs, choose deterministically while
			// keeping every internal ID available through the hidden byID index.
			byDisplay[m.DisplayName] = m
		}
	}
	publicModels := make([]Model, 0, len(byDisplay))
	for _, m := range byDisplay {
		publicModels = append(publicModels, m)
	}
	sort.Slice(publicModels, func(i, j int) bool {
		return publicModels[i].DisplayName < publicModels[j].DisplayName
	})
	r.mu.Lock()
	r.models, r.byDisplay, r.byID, r.fetchedAt = publicModels, byDisplay, byID, time.Now()
	r.mu.Unlock()
	slog.Info("qoder models refreshed", "models", len(publicModels), "upstream_models", len(models), "duration_ms", time.Since(started).Milliseconds())
	return nil
}

func (r *Registry) ensure(ctx context.Context) error {
	r.mu.RLock()
	fresh := len(r.models) > 0 && time.Since(r.fetchedAt) < r.ttl
	r.mu.RUnlock()
	if fresh {
		return nil
	}
	return r.Refresh(ctx)
}

func (r *Registry) List(ctx context.Context) ([]Model, error) {
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]Model(nil), r.models...)
	return out, nil
}

func (r *Registry) Resolve(ctx context.Context, publicName string) (Model, error) {
	if err := r.ensure(ctx); err != nil {
		return Model{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.byDisplay[publicName]; ok {
		slog.Debug("model resolved", "display_name", publicName, "upstream_model", m.UpstreamID)
		return m, nil
	}
	// Keep upstream IDs as a hidden compatibility/debugging input, but never expose them in /v1/models.
	if m, ok := r.byID[publicName]; ok {
		slog.Warn("upstream model id used directly", "upstream_model", publicName, "display_name", m.DisplayName)
		return m, nil
	}
	return Model{}, fmt.Errorf("model %q not found", publicName)
}

func fetchModels(ctx context.Context, client *http.Client, cred credential.Credential) ([]Model, error) {
	url := BaseURL + ModelsPath
	headers, err := BuildHeaders(nil, url, cred)
	if err != nil {
		return nil, err
	}
	headers.Set("Accept", "application/json")
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept-Encoding", "identity")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header = headers
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("qoder models returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	items := modelItems(payload["chat"])
	models := make([]Model, 0, len(items))
	for _, item := range items {
		id := firstString(item, "key", "model", "model_id", "modelId", "id")
		if id == "" {
			continue
		}
		display := firstString(item, "display_name", "displayName", "label", "title", "name")
		if display == "" {
			display = id
		}
		models = append(models, Model{
			UpstreamID: id, DisplayName: display,
			Source:      firstString(item, "source"),
			IsReasoning: boolField(item, "is_reasoning"), IsVL: boolField(item, "is_vl"),
			MaxInputTokens: intField(item, "max_input_tokens"), MaxOutputTokens: intField(item, "max_output_tokens"),
			Raw: cloneMap(item),
		})
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].DisplayName == models[j].DisplayName {
			return models[i].UpstreamID < models[j].UpstreamID
		}
		return models[i].DisplayName < models[j].DisplayName
	})
	return models, nil
}

func modelItems(v any) []map[string]any {
	var out []map[string]any
	switch chat := v.(type) {
	case []any:
		for _, raw := range chat {
			if m, ok := raw.(map[string]any); ok {
				out = append(out, m)
			}
		}
	case map[string]any:
		keys := make([]string, 0, len(chat))
		for k := range chat {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if m, ok := chat[key].(map[string]any); ok {
				copy := cloneMap(m)
				if firstString(copy, "key") == "" {
					copy["key"] = key
				}
				out = append(out, copy)
			}
	}
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func boolField(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }
func intField(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case int:
		return v
	default:
		return 0
	}
}
