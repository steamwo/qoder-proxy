package qoder

import (
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

func TestBuildHeaders(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	h, err := BuildHeaders(body, BaseURL+ChatPath, credential.Credential{
		Token: "token", UserID: "user-1", MachineID: "machine-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Get("Cosy-Sigpath"); got != "/api/v2/service/pro/sse/agent_chat_generation" {
		t.Fatalf("Cosy-Sigpath=%q", got)
	}
	if got := h.Get("Cosy-Bodylength"); got != "17" {
		t.Fatalf("Cosy-Bodylength=%q", got)
	}
	if got := h.Get("Cosy-Machineid"); got != "machine-1" {
		t.Fatalf("Cosy-Machineid=%q", got)
	}
	if !strings.HasPrefix(h.Get("Authorization"), "Bearer COSY.") {
		t.Fatalf("unexpected Authorization header: %q", h.Get("Authorization"))
	}
}

func TestStartLoginUsesPKCEDeviceParameters(t *testing.T) {
	s, err := StartLogin()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"challenge=", "challenge_method=S256", "machine_id=", "nonce="} {
		if !strings.Contains(s.URL, want) {
			t.Fatalf("login URL %q missing %q", s.URL, want)
		}
	}
	if s.Verifier == "" || s.Nonce == "" || s.MachineID == "" {
		t.Fatal("login session missing generated values")
	}
}
