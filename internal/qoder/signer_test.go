package qoder

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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
	for _, want := range []string{"challenge=", "challenge_method=S256", "client_id=", "machine_id=", "nonce="} {
		if !strings.Contains(s.URL, want) {
			t.Fatalf("login URL %q missing %q", s.URL, want)
		}
	}
	if s.Verifier == "" || s.Nonce == "" || s.MachineID == "" {
		t.Fatal("login session missing generated values")
	}
}


func TestInferSignerUsesStableRuntimeContextAndVerifiedHeaders(t *testing.T) {
	signer, err := NewInferSigner(credential.Credential{
		Token: "token", UserID: "user-1", MachineID: "machine-1",
		OrganizationID: "org-1", OrganizationTags: []string{"tag-a", "tag-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("encoded")
	url := DefaultInferenceBaseURL + ChatEncodedPath
	first, err := signer.BuildHeaders(body, url, "model-1", "system")
	if err != nil {
		t.Fatal(err)
	}
	second, err := signer.BuildHeaders(body, url, "model-1", "system")
	if err != nil {
		t.Fatal(err)
	}
	if first.Get("Cosy-Key") == "" || first.Get("Cosy-Key") != second.Get("Cosy-Key") {
		t.Fatalf("runtime Cosy-Key changed across requests")
	}
	if got := first.Get("Cosy-Version"); got != InferProtocolVersion {
		t.Fatalf("Cosy-Version=%q", got)
	}
	for key, want := range map[string]string{
		"Connection":             "keep-alive",
		"Cosy-Business-Product": "cli",
		"Cosy-Business-Type":    "agent",
		"Cosy-Scene":            "assistant",
		"Cosy-Organization-Id":  "org-1",
		"Cosy-Organization-Tags": "tag-a,tag-b",
		"X-Model-Key":           "model-1",
		"X-Model-Source":        "system",
	} {
		if got := first.Get(key); got != want {
			t.Fatalf("%s=%q want %q", key, got, want)
		}
	}
	for _, obsolete := range []string{"Cosy-Bodyhash", "Cosy-Bodylength", "Cosy-Clientip", "Cosy-Machineos", "Cosy-Sigpath", "X-Request-Id"} {
		if got := first.Get(obsolete); got != "" {
			t.Fatalf("obsolete inference header %s=%q", obsolete, got)
		}
	}
}

func TestInferSignerOmitsEmptyOrganizationHeaders(t *testing.T) {
	signer, err := NewInferSigner(credential.Credential{Token: "token", UserID: "user-1", MachineID: "machine-1"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := signer.BuildHeaders([]byte("encoded"), DefaultInferenceBaseURL+ChatEncodedPath, "model-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := h["Cosy-Organization-Id"]; ok {
		t.Fatal("empty organization id header must be omitted")
	}
	if _, ok := h["Cosy-Organization-Tags"]; ok {
		t.Fatal("empty organization tags header must be omitted")
	}
}


func TestAuthStateRefreshesAndPersistsRotatedCredential(t *testing.T) {
	var persisted credential.Credential
	var refreshCalls int
	hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/api/v1/deviceToken/refresh":
			refreshCalls++
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"device_token":"new-token","refresh_token":"new-refresh","expires_in":7200,"refresh_token_expires_in":86400}`)),
				Request: r,
			}, nil
		case "/api/v1/userinfo":
			return &http.Response{
				StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"application/json"}},
				Body: io.NopCloser(strings.NewReader(`{"id":"u1","organization_id":"org-2","organization_tags":["tag-a"],"data_policy_agreed":true}`)),
				Request: r,
			}, nil
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
			return nil, nil
		}
	})}
	state := NewAuthState(hc, credential.Credential{
		Token: "old-token", RefreshToken: "old-refresh", UserID: "u1", MachineID: "m1",
		ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
		RefreshTokenExpiresAt: time.Now().Add(24 * time.Hour).Unix(),
	})
	state.SetPersist(func(c credential.Credential) error {
		persisted = c
		return nil
	})
	got, err := state.Credential(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh calls=%d", refreshCalls)
	}
	if got.Token != "new-token" || got.RefreshToken != "new-refresh" {
		t.Fatalf("refreshed credential=%#v", got)
	}
	if got.OrganizationID != "org-2" || len(got.OrganizationTags) != 1 || !got.DataPolicyAgreed {
		t.Fatalf("refreshed user profile=%#v", got)
	}
	if got.EncryptUserInfo == "" || got.CosyKey == "" {
		t.Fatal("runtime auth fields were not rebuilt")
	}
	if persisted.Token != "new-token" || persisted.CosyKey == "" {
		t.Fatalf("persisted credential=%#v", persisted)
	}
	signer, err := state.InferSigner(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if signer.Credential().CosyKey != got.CosyKey {
		t.Fatal("shared signer did not reuse refreshed runtime fields")
	}
}
