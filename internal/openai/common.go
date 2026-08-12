package openai

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/steamwo/qoder-proxy/internal/qoder"
)

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": message, "type": "invalid_request_error", "code": code}})
}

func writeBackendError(w http.ResponseWriter, err error, fallbackStatus int) {
	var upstreamErr *qoder.UpstreamError
	if errors.As(err, &upstreamErr) {
		writeJSON(w, upstreamErr.HTTPStatus, map[string]any{"error": map[string]any{
			"message": upstreamErr.Message,
			"type":    upstreamErr.Type,
			"code":    upstreamErr.PublicCode,
		}})
		return
	}
	writeError(w, fallbackStatus, "upstream_error", err.Error())
}

func backendErrorPayload(err error) map[string]any {
	var upstreamErr *qoder.UpstreamError
	if errors.As(err, &upstreamErr) {
		return map[string]any{"error": map[string]any{
			"message": upstreamErr.Message,
			"type":    upstreamErr.Type,
			"code":    upstreamErr.PublicCode,
		}}
	}
	return map[string]any{"error": map[string]any{
		"message": err.Error(), "type": "upstream_error", "code": "qoder_upstream_error",
	}}
}
