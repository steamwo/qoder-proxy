package qoder

import (
	"testing"

	"github.com/steamwo/qoder-proxy/internal/protocol"
)

func TestSessionIDForRequestReusesStableClientSession(t *testing.T) {
	req := protocol.Request{ClientSessionKey: "claude-code/session/s1/agent/a1"}
	first, err := sessionIDForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionIDForRequest(req)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || first != second {
		t.Fatalf("stable client session must reuse Qoder session: %q vs %q", first, second)
	}
}

func TestSessionIDForRequestSeparatesSubagents(t *testing.T) {
	first, err := sessionIDForRequest(protocol.Request{ClientSessionKey: "claude-code/session/s1/agent/a1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionIDForRequest(protocol.Request{ClientSessionKey: "claude-code/session/s1/agent/a2"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("different subagents shared Qoder session %q", first)
	}
}

func TestSessionIDForRequestIsolatesUnidentifiedRequests(t *testing.T) {
	first, err := sessionIDForRequest(protocol.Request{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := sessionIDForRequest(protocol.Request{})
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || first == second {
		t.Fatalf("unidentified requests must be isolated: %q vs %q", first, second)
	}
}
