package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/steamwo/qoder-proxy/internal/anthropic"
	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/openai"
	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

// Backend shares model defaults across every public protocol adapter.
// Backend 在所有公开协议适配器之间共享模型默认值。
type Backend struct {
	Registry               *qoder.Registry
	Qoder                  *qoder.Client
	defaultsMu             sync.RWMutex
	modelReasoningDefaults map[string]string
	modelContextDefaults   map[string]int
}

// ResolveModel attaches the stable-ID reasoning default after live discovery so stale values are revalidated downstream.
// ResolveModel 在实时发现后附加稳定 ID 默认思考值，使过期值仍会在下游重新校验。
func (b *Backend) ResolveModel(ctx context.Context, name string) (qoder.Model, error) {
	model, err := b.Registry.Resolve(ctx, name)
	if err != nil {
		return qoder.Model{}, err
	}
	b.defaultsMu.RLock()
	model.DefaultReasoningEffort = b.modelReasoningDefaults[model.UpstreamID]
	b.defaultsMu.RUnlock()
	return model, nil
}

// SetModelReasoningDefaults replaces one immutable snapshot so live desktop changes are race-free.
// SetModelReasoningDefaults 替换一份不可变快照，使桌面端实时修改不会产生数据竞争。
func (b *Backend) SetModelReasoningDefaults(defaults map[string]string) {
	cloned := make(map[string]string, len(defaults))
	for modelID, effort := range defaults {
		cloned[modelID] = effort
	}
	b.defaultsMu.Lock()
	b.modelReasoningDefaults = cloned
	b.defaultsMu.Unlock()
}

// SetModelContextDefaults replaces the saved per-model context-window snapshot.
func (b *Backend) SetModelContextDefaults(defaults map[string]int) {
	cloned := make(map[string]int, len(defaults))
	for modelID, tokens := range defaults {
		cloned[modelID] = tokens
	}
	b.defaultsMu.Lock()
	b.modelContextDefaults = cloned
	b.defaultsMu.Unlock()
}

// SetCredentialPersister lets long-running desktop/service processes persist
// rotated refresh tokens and stable runtime authentication fields.
func (b *Backend) SetCredentialPersister(persist func(credential.Credential) error) {
	if b == nil || b.Qoder == nil || b.Qoder.Auth == nil {
		return
	}
	b.Qoder.Auth.SetPersist(persist)
}

func (b *Backend) applyContextDefault(req protocol.Request) (protocol.Request, error) {
	b.defaultsMu.RLock()
	savedDefault := b.modelContextDefaults[req.ModelID]
	b.defaultsMu.RUnlock()
	contextWindow, err := qoder.ResolveContextWindow(req.ModelConfig, req.ContextWindow, savedDefault)
	if err != nil {
		return protocol.Request{}, err
	}
	req.ContextWindow = contextWindow
	return req, nil
}

func bindClientSession(ctx context.Context, req protocol.Request) protocol.Request {
	if key := clientSessionKeyFromContext(ctx); key != "" {
		req.ClientSessionKey = key
	}
	if key := clientTurnKeyFromContext(ctx); key != "" {
		req.ClientTurnKey = key
	}
	return req
}

// Chat keeps the protocol boundary thin so one upstream client owns request behavior.
// Chat 保持协议边界精简，由单一上游客户端统一请求行为。
func (b *Backend) Chat(ctx context.Context, req protocol.Request) (*http.Response, error) {
	normalized := bindClientSession(ctx, normalizeQoderRequest(req))
	var err error
	normalized, err = b.applyContextDefault(normalized)
	if err != nil {
		return nil, err
	}
	resp, err := b.Qoder.Chat(ctx, normalized)
	if err != nil {
		return nil, err
	}
	if normalized.ClaudeToolCompatibility {
		return qoder.GuardToolResponse(resp, normalized.Tools), nil
	}
	return resp, nil
}

// ChatWithQueue preserves queue callbacks without duplicating retry policy in adapters.
// ChatWithQueue 保留排队回调，避免各适配器重复重试策略。
func (b *Backend) ChatWithQueue(ctx context.Context, req protocol.Request, onQueue func(qoder.QueueInfo) error) (*http.Response, error) {
	normalized := bindClientSession(ctx, normalizeQoderRequest(req))
	var err error
	normalized, err = b.applyContextDefault(normalized)
	if err != nil {
		return nil, err
	}
	resp, err := b.Qoder.ChatWithQueue(ctx, normalized, onQueue)
	if err != nil {
		return nil, err
	}
	if normalized.ClaudeToolCompatibility {
		return qoder.GuardToolResponse(resp, normalized.Tools), nil
	}
	return resp, nil
}

// Server owns the public handler and authentication configuration.
// Server 管理公开处理器与认证配置。
type Server struct {
	Backend *Backend
	APIKey  string
}

// New creates a server without desktop-only defaults so CLI behavior stays unchanged.
// New 创建不含桌面专属默认值的服务，确保 CLI 行为不变。
func New(httpClient *http.Client, cred credential.Credential, apiKey string) *Server {
	auth := qoder.NewAuthState(httpClient, cred)
	client := qoder.NewClientWithAuth(httpClient, auth)
	client.UsageObserver = qoder.RecordRuntimeUsage
	return &Server{Backend: &Backend{Registry: qoder.NewRegistryWithAuth(httpClient, auth), Qoder: client}, APIKey: apiKey}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.models)
	// Compatibility alias: OpenAI specifies GET, but some local clients probe models with POST.
	mux.HandleFunc("POST /v1/models", s.models)
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { openai.HandleChat(w, r, s.Backend) })
	mux.HandleFunc("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) { openai.HandleResponses(w, r, s.Backend) })
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) { anthropic.HandleMessagesCompatible(w, r, s.Backend) })
	return s.accessLog(s.cors(s.auth(clientSession(mux))))
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *responseRecorder) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseRecorder) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += n
	return n, err
}

func (w *responseRecorder) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		requestedHeaders := strings.TrimSpace(r.Header.Get("Access-Control-Request-Headers"))
		if requestedHeaders == "" {
			requestedHeaders = "Authorization, Content-Type, OpenAI-Beta, X-Api-Key, Anthropic-Version, Anthropic-Beta"
		}
		w.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rw := &responseRecorder{ResponseWriter: w}
		slog.Debug("request started", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr, "user_agent", r.UserAgent())
		next.ServeHTTP(rw, r)
		status := rw.status
		if status == 0 {
			status = http.StatusOK
		}
		attrs := []any{"method", r.Method, "path", r.URL.Path, "status", status, "bytes", rw.bytes, "duration_ms", time.Since(started).Milliseconds()}
		if status >= 500 {
			slog.Error("request completed", attrs...)
		} else if status >= 400 {
			slog.Warn("request completed", attrs...)
		} else {
			slog.Info("request completed", attrs...)
		}
	})
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.APIKey != "" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got == "" {
				got = r.Header.Get("X-Api-Key")
			}
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.APIKey)) != 1 {
				slog.Warn("authentication failed", "method", r.Method, "path", r.URL.Path, "remote", r.RemoteAddr)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				if r.URL.Path == "/v1/messages" {
					_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"message": "invalid API key", "type": "authentication_error"}})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "invalid API key", "type": "authentication_error", "code": "invalid_api_key"}})
				}
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	models, err := s.Backend.Registry.List(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error", "code": "model_discovery_failed"}})
		return
	}
	data := make([]any, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]any{"id": m.DisplayName, "object": "model", "created": 0, "owned_by": "qoder"})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": data})
}
