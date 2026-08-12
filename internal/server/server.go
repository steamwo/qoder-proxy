package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/steamwo/qoder-proxy/internal/anthropic"
	"github.com/steamwo/qoder-proxy/internal/credential"
	"github.com/steamwo/qoder-proxy/internal/openai"
	"github.com/steamwo/qoder-proxy/internal/protocol"
	"github.com/steamwo/qoder-proxy/internal/qoder"
)

type Backend struct {
	Registry *qoder.Registry
	Qoder    *qoder.Client
}

func (b *Backend) ResolveModel(ctx context.Context, name string) (qoder.Model, error) {
	return b.Registry.Resolve(ctx, name)
}
func (b *Backend) Chat(ctx context.Context, req protocol.Request) (*http.Response, error) {
	return b.Qoder.Chat(ctx, req)
}

func (b *Backend) ChatWithQueue(ctx context.Context, req protocol.Request, onQueue func(qoder.QueueInfo) error) (*http.Response, error) {
	return b.Qoder.ChatWithQueue(ctx, req, onQueue)
}

type Server struct {
	Backend *Backend
	APIKey  string
}

func New(httpClient *http.Client, cred credential.Credential, apiKey string) *Server {
	return &Server{Backend: &Backend{Registry: qoder.NewRegistry(httpClient, cred), Qoder: qoder.NewClient(httpClient, cred)}, APIKey: apiKey}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", s.models)
	// Compatibility alias: OpenAI specifies GET, but some local clients probe models with POST.
	mux.HandleFunc("POST /v1/models", s.models)
	mux.HandleFunc("POST /v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { openai.HandleChat(w, r, s.Backend) })
	mux.HandleFunc("POST /v1/responses", func(w http.ResponseWriter, r *http.Request) { openai.HandleResponses(w, r, s.Backend) })
	mux.HandleFunc("POST /v1/messages", func(w http.ResponseWriter, r *http.Request) { anthropic.HandleMessages(w, r, s.Backend) })
	return s.accessLog(s.cors(s.auth(mux)))
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
