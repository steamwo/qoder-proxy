package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (w *flushRecorder) Flush() {
	w.flushed = true
	w.ResponseRecorder.Flush()
}

func TestAccessLogPreservesFlusher(t *testing.T) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(old)

	s := &Server{}
	h := s.accessLog(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("logging middleware removed http.Flusher")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: ok\n\n"))
		f.Flush()
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	rw := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(rw, req)
	if !rw.flushed {
		t.Fatal("expected downstream Flush to be forwarded")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOptionsPreflightDoesNotReturn405OrRequireAuth(t *testing.T) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(old)

	s := &Server{APIKey: "secret"}
	req := httptest.NewRequest(http.MethodOptions, "/v1/models", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rw := httptest.NewRecorder()
	s.Handler().ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for CORS preflight, got %d: %s", rw.Code, rw.Body.String())
	}
	if got := rw.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("unexpected Access-Control-Allow-Origin: %q", got)
	}
	if got := rw.Header().Get("Access-Control-Allow-Headers"); got != "authorization, content-type" {
		t.Fatalf("unexpected Access-Control-Allow-Headers: %q", got)
	}
}

func TestPostModelsCompatibilityAlias(t *testing.T) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(old)

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"chat":[{"key":"internal-model-1","display_name":"Public Model"}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}
	cred := credential.Credential{Token: "token", UserID: "user", MachineID: "machine"}
	s := New(client, cred, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/models", nil)
	rw := httptest.NewRecorder()
	s.Handler().ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("expected POST /v1/models compatibility alias to return 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if !strings.Contains(rw.Body.String(), `"id":"Public Model"`) {
		t.Fatalf("expected display_name in model response, got %s", rw.Body.String())
	}
	if strings.Contains(rw.Body.String(), "internal-model-1") {
		t.Fatalf("upstream model id leaked in public model response: %s", rw.Body.String())
	}
}

func TestAnthropicXAPIKeyAuth(t *testing.T) {
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer slog.SetDefault(old)

	s := &Server{APIKey: "secret"}
	okReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	okReq.Header.Set("X-Api-Key", "secret")
	okRR := httptest.NewRecorder()
	s.Handler().ServeHTTP(okRR, okReq)
	if okRR.Code == http.StatusUnauthorized {
		t.Fatalf("x-api-key should be accepted for Anthropic requests: %s", okRR.Body.String())
	}

	badReq := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	badReq.Header.Set("X-Api-Key", "wrong")
	badRR := httptest.NewRecorder()
	s.Handler().ServeHTTP(badRR, badReq)
	if badRR.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", badRR.Code, badRR.Body.String())
	}
	body := badRR.Body.String()
	if !strings.Contains(body, `"type":"error"`) || !strings.Contains(body, `"type":"authentication_error"`) {
		t.Fatalf("expected Anthropic error shape, got %s", body)
	}
}
