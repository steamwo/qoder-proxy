package qoder

import (
	"fmt"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// sessionIDForRequest preserves one trustworthy downstream conversation/thread
// across turns and model switches while keeping the upstream identifier opaque.
// The downstream key is already protocol-namespaced by the server. Requests
// without a trustworthy client conversation key are isolated with a fresh UUID.
func sessionIDForRequest(req protocol.Request) (string, error) {
	if key := strings.TrimSpace(req.ClientSessionKey); key != "" {
		return stableHash("qoder-client-session", key), nil
	}
	id, err := randomUUID()
	if err != nil {
		return "", fmt.Errorf("generate Qoder session id: %w", err)
	}
	return id, nil
}
