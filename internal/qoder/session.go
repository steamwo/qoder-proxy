package qoder

import (
	"fmt"
	"strings"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

// sessionIDForRequest preserves a trustworthy client conversation/agent across
// turns while keeping the upstream identifier opaque. Requests without a
// client-provided session key are isolated with a fresh random UUID.
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
