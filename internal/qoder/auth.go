package qoder

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

type LoginSession struct {
	Verifier  string
	Nonce     string
	MachineID string
	URL       string
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func StartLogin() (LoginSession, error) {
	verifier, err := randomToken(48)
	if err != nil {
		return LoginSession{}, err
	}
	nonce, err := randomUUID()
	if err != nil {
		return LoginSession{}, err
	}
	machineID, err := randomUUID()
	if err != nil {
		return LoginSession{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	u, _ := url.Parse(LoginURL)
	q := u.Query()
	q.Set("challenge", challenge)
	q.Set("challenge_method", "S256")
	q.Set("machine_id", machineID)
	q.Set("nonce", nonce)
	u.RawQuery = q.Encode()
	return LoginSession{Verifier: verifier, Nonce: nonce, MachineID: machineID, URL: u.String()}, nil
}

func payloadRoot(payload map[string]any) map[string]any {
	current := payload
	for i := 0; i < 3; i++ {
		if firstString(current, "token", "access_token") != "" {
			return current
		}
		var nested map[string]any
		for _, key := range []string{"data", "result", "payload"} {
			if v, ok := current[key].(map[string]any); ok {
				nested = v
				break
			}
		}
		if nested == nil {
			return current
		}
		current = nested
	}
	return current
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func firstInt64(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return int64(v)
		case json.Number:
			n, _ := v.Int64()
			return n
		}
	}
	return 0
}

func PollLogin(ctx context.Context, client *http.Client, session LoginSession) (credential.Credential, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		cred, pending, err := pollLoginOnce(ctx, client, session)
		if err != nil {
			return credential.Credential{}, err
		}
		if !pending {
			return cred, nil
		}
		select {
		case <-ctx.Done():
			return credential.Credential{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

func pollLoginOnce(ctx context.Context, client *http.Client, session LoginSession) (credential.Credential, bool, error) {
	u, _ := url.Parse(PollURL)
	q := u.Query()
	q.Set("nonce", session.Nonce)
	q.Set("verifier", session.Verifier)
	q.Set("challenge_method", "S256")
	u.RawQuery = q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return credential.Credential{}, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body)
		return credential.Credential{}, true, nil
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		if resp.StatusCode/100 != 2 {
			return credential.Credential{}, false, fmt.Errorf("qoder login poll returned HTTP %d", resp.StatusCode)
		}
		return credential.Credential{}, false, fmt.Errorf("decode qoder login response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return credential.Credential{}, false, fmt.Errorf("qoder login poll returned HTTP %d: %s", resp.StatusCode, firstString(payload, "message", "error", "error_description"))
	}
	root := payloadRoot(payload)
	token := firstString(root, "token", "access_token")
	if token == "" {
		return credential.Credential{}, false, fmt.Errorf("qoder login response is missing token")
	}
	userInfo, _ := fetchUserInfo(ctx, client, token)
	userID := firstString(root, "user_id", "userId")
	if userID == "" {
		userID = firstString(userInfo, "id", "user_id", "userId")
	}
	if userID == "" {
		return credential.Credential{}, false, fmt.Errorf("qoder authorization succeeded but user identity could not be resolved")
	}
	expiresAt := firstInt64(root, "expires_at", "expire_time")
	if expiresAt > 10_000_000_000 {
		expiresAt /= 1000
	}
	if expiresAt == 0 {
		if expiresIn := firstInt64(root, "expires_in"); expiresIn > 0 {
			expiresAt = time.Now().Unix() + expiresIn
		}
	}
	return credential.Credential{
		Token:          token,
		RefreshToken:   firstString(root, "refresh_token"),
		UserID:         userID,
		MachineID:      session.MachineID,
		Name:           firstString(userInfo, "name", "username"),
		Email:          firstString(userInfo, "email"),
		OrganizationID: firstString(userInfo, "organization_id", "organizationId"),
		MemberID:       firstString(userInfo, "member_id", "memberId"),
		ExpiresAt:      expiresAt,
		CreatedAt:      time.Now().Unix(),
	}, false, nil
}

func fetchUserInfo(ctx context.Context, client *http.Client, token string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, UserInfoURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("qoder userinfo returned HTTP %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, err
	}
	return payloadRoot(payload), nil
}
