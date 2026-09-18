package qoder

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
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
	q.Set("client_id", ProductionClientID)
	q.Set("machine_id", machineID)
	q.Set("nonce", nonce)
	u.RawQuery = q.Encode()
	return LoginSession{Verifier: verifier, Nonce: nonce, MachineID: machineID, URL: u.String()}, nil
}

func payloadRoot(payload map[string]any) map[string]any {
	current := payload
	for i := 0; i < 3; i++ {
		if firstString(current, "device_token", "token", "access_token") != "" {
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

func firstBool(m map[string]any, keys ...string) bool {
	value, _ := firstBoolPresent(m, keys...)
	return value
}

func firstBoolPresent(m map[string]any, keys ...string) (bool, bool) {
	for _, key := range keys {
		if v, ok := m[key].(bool); ok {
			return v, true
		}
	}
	return false, false
}

func firstStringSlice(m map[string]any, keys ...string) []string {
	for _, key := range keys {
		switch values := m[key].(type) {
		case []string:
			return append([]string(nil), values...)
		case []any:
			out := make([]string, 0, len(values))
			for _, value := range values {
				if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
					out = append(out, strings.TrimSpace(text))
				}
			}
			return out
		}
	}
	return nil
}

func firstExpiry(m map[string]any, absoluteKeys, durationKeys []string) int64 {
	for _, key := range absoluteKeys {
		switch value := m[key].(type) {
		case float64:
			n := int64(value)
			if n > 10_000_000_000 {
				n /= 1000
			}
			if n > 0 {
				return n
			}
		case json.Number:
			if n, err := value.Int64(); err == nil {
				if n > 10_000_000_000 {
					n /= 1000
				}
				if n > 0 {
					return n
				}
			}
		case string:
			text := strings.TrimSpace(value)
			if t, err := time.Parse(time.RFC3339, text); err == nil {
				return t.Unix()
			}
			var n int64
			if _, err := fmt.Sscan(text, &n); err == nil && n > 0 {
				if n > 10_000_000_000 {
					n /= 1000
				}
				return n
			}
		}
	}
	if seconds := firstInt64(m, durationKeys...); seconds > 0 {
		return time.Now().Unix() + seconds
	}
	return 0
}

const currentRuntimeProfileVersion = 1

func applyUserInfoProfile(cred credential.Credential, userInfo map[string]any) credential.Credential {
	if value := firstString(userInfo, "id", "user_id", "userId"); value != "" {
		cred.UserID = value
	}
	if value := firstString(userInfo, "name", "username"); value != "" {
		cred.Name = value
	}
	if value := firstString(userInfo, "email"); value != "" {
		cred.Email = value
	}
	if value := firstString(userInfo, "organization_id", "organizationId"); value != "" {
		cred.OrganizationID = value
	}
	if tags := firstStringSlice(userInfo, "organization_tags", "organizationTags"); tags != nil {
		cred.OrganizationTags = tags
	} else if cred.OrganizationTags == nil {
		cred.OrganizationTags = []string{}
	}
	if value, ok := firstBoolPresent(userInfo, "data_policy_agreed", "dataPolicyAgreed"); ok {
		cred.DataPolicyAgreed = value
	}
	if value := firstString(userInfo, "member_id", "memberId"); value != "" {
		cred.MemberID = value
	}
	cred.RuntimeProfileVersion = currentRuntimeProfileVersion
	return cred
}

func enrichCredentialProfile(ctx context.Context, client *http.Client, cred credential.Credential) (credential.Credential, error) {
	userInfo, err := fetchUserInfo(ctx, client, cred.Token)
	if err != nil {
		return cred, err
	}
	next := applyUserInfoProfile(cred, userInfo)
	next.EncryptUserInfo = ""
	next.CosyKey = ""
	next, err = EnsureRuntimeAuthFields(next)
	if err != nil {
		return cred, err
	}
	return next, nil
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
	token := firstString(root, "device_token", "token", "access_token")
	if token == "" {
		return credential.Credential{}, false, fmt.Errorf("qoder login response is missing token")
	}
	userInfo, userInfoErr := fetchUserInfo(ctx, client, token)
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
	cred := credential.Credential{
		Token:                 token,
		RefreshToken:          firstString(root, "refresh_token"),
		RefreshTokenExpiresAt: firstExpiry(root, []string{"refresh_token_expires_at", "refresh_token_expire_time"}, []string{"refresh_token_expires_in"}),
		UserID:                userID,
		MachineID:             session.MachineID,
		ExpiresAt:             expiresAt,
		CreatedAt:             time.Now().Unix(),
	}
	if userInfoErr == nil {
		cred = applyUserInfoProfile(cred, userInfo)
		cred, err = EnsureRuntimeAuthFields(cred)
		if err != nil {
			return credential.Credential{}, false, fmt.Errorf("prepare qoder runtime authentication: %w", err)
		}
	}
	return cred, false, nil
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


// RefreshCredential rotates the device token and rebuilds the long-lived COSY
// runtime fields. It is intentionally independent of the local credential store
// so headless and embedded callers can decide how persistence is handled.
func RefreshCredential(ctx context.Context, client *http.Client, cred credential.Credential) (credential.Credential, error) {
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return cred, fmt.Errorf("qoder credential has no refresh_token")
	}
	if client == nil {
		client = http.DefaultClient
	}
	body, _ := json.Marshal(map[string]string{"refresh_token": cred.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, RefreshURL, bytes.NewReader(body))
	if err != nil {
		return cred, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return cred, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return cred, err
	}
	if resp.StatusCode/100 != 2 {
		return cred, fmt.Errorf("qoder token refresh returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return cred, fmt.Errorf("decode qoder refresh response: %w", err)
	}
	root := payloadRoot(payload)
	token := firstString(root, "device_token", "token", "access_token")
	if token == "" {
		return cred, fmt.Errorf("qoder refresh response is missing token")
	}

	next := cred
	next.Token = token
	if rotated := firstString(root, "refresh_token"); rotated != "" {
		next.RefreshToken = rotated
	}
	if expiresAt := firstExpiry(root, []string{"expires_at", "expire_time"}, []string{"expires_in"}); expiresAt > 0 {
		next.ExpiresAt = expiresAt
	}
	if refreshExpiresAt := firstExpiry(root, []string{"refresh_token_expires_at", "refresh_token_expire_time"}, []string{"refresh_token_expires_in"}); refreshExpiresAt > 0 {
		next.RefreshTokenExpiresAt = refreshExpiresAt
	}
	if userInfo, userErr := fetchUserInfo(ctx, client, token); userErr == nil {
		next = applyUserInfoProfile(next, userInfo)
	}
	next.EncryptUserInfo = ""
	next.CosyKey = ""
	if next.RuntimeProfileVersion >= currentRuntimeProfileVersion {
		next, err = EnsureRuntimeAuthFields(next)
		if err != nil {
			return cred, fmt.Errorf("rebuild qoder runtime authentication: %w", err)
		}
	}
	return next, nil
}

// AuthState is shared by model discovery and inference so token/profile refreshes
// are coalesced and committed atomically without holding a mutex across network I/O.
const (
	credentialRefreshSkew       = time.Hour
	authNetworkTimeout          = 30 * time.Second
	authUpdateRetryDelay        = 30 * time.Second
	credentialPersistRetryDelay = 30 * time.Second
)

type AuthState struct {
	mu      sync.Mutex
	client  *http.Client
	cred    credential.Credential
	signer  *InferSigner
	persist func(credential.Credential) error

	updating       chan struct{}
	updateRetryAt  time.Time
	lastUpdateErr  error
	persistDirty   bool
	persistRetryAt time.Time
}

func NewAuthState(client *http.Client, cred credential.Credential) *AuthState {
	if client == nil {
		client = http.DefaultClient
	}
	return &AuthState{client: client, cred: cred}
}

func (a *AuthState) SetPersist(fn func(credential.Credential) error) {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.persist = fn
	a.mu.Unlock()
	a.persistDirtyBestEffort(false)
}

func (a *AuthState) CredentialSnapshot() credential.Credential {
	if a == nil {
		return credential.Credential{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cred
}

func sameCredentialGeneration(a, b credential.Credential) bool {
	return a.Token == b.Token &&
		a.RefreshToken == b.RefreshToken &&
		a.CosyKey == b.CosyKey &&
		a.EncryptUserInfo == b.EncryptUserInfo &&
		a.RuntimeProfileVersion == b.RuntimeProfileVersion
}

func (a *AuthState) persistDirtyBestEffort(force bool) {
	if a == nil {
		return
	}
	a.mu.Lock()
	now := time.Now()
	if !a.persistDirty || a.persist == nil || (!force && now.Before(a.persistRetryAt)) {
		a.mu.Unlock()
		return
	}
	persist := a.persist
	snapshot := a.cred
	a.persistRetryAt = now.Add(credentialPersistRetryDelay)
	a.mu.Unlock()

	err := persist(snapshot)

	a.mu.Lock()
	if err == nil && sameCredentialGeneration(a.cred, snapshot) {
		a.persistDirty = false
		a.persistRetryAt = time.Time{}
	} else if err != nil {
		a.persistDirty = true
		a.persistRetryAt = time.Now().Add(credentialPersistRetryDelay)
	}
	a.mu.Unlock()
	if err != nil {
		slog.Warn("persist refreshed qoder credential failed; will retry", "error", err)
	}
}

func (a *AuthState) ensureReady(ctx context.Context, forceRefresh bool) error {
	if a == nil {
		return fmt.Errorf("qoder auth state is unavailable")
	}
	a.persistDirtyBestEffort(false)
	for {
		a.mu.Lock()
		now := time.Now()
		nowUnix := now.Unix()
		cred := a.cred
		accessExpired := cred.ExpiresAt > 0 && nowUnix >= cred.ExpiresAt
		refreshExpired := cred.RefreshTokenExpiresAt > 0 && nowUnix >= cred.RefreshTokenExpiresAt
		needsMigration := cred.RuntimeProfileVersion < currentRuntimeProfileVersion
		needsRefresh := forceRefresh || (cred.ExpiresAt > 0 && cred.ExpiresAt-int64(credentialRefreshSkew/time.Second) <= nowUnix)

		if refreshExpired && (forceRefresh || accessExpired) {
			a.mu.Unlock()
			return fmt.Errorf("qoder refresh token expired; authorize again")
		}
		if needsRefresh && strings.TrimSpace(cred.RefreshToken) == "" {
			if forceRefresh || accessExpired {
				a.mu.Unlock()
				return fmt.Errorf("qoder credential expired and cannot be refreshed; authorize again")
			}
			needsRefresh = false
		}
		if !needsMigration && !needsRefresh {
			a.mu.Unlock()
			a.persistDirtyBestEffort(false)
			return nil
		}
		if a.updating != nil {
			done := a.updating
			a.mu.Unlock()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-done:
				// A concurrent successful update satisfies a force-refresh request too.
				forceRefresh = false
				continue
			}
		}
		if now.Before(a.updateRetryAt) {
			err := a.lastUpdateErr
			if needsMigration || forceRefresh || accessExpired {
				a.mu.Unlock()
				if err != nil {
					return err
				}
				return fmt.Errorf("qoder authentication update is temporarily unavailable")
			}
			a.mu.Unlock()
			return nil
		}

		done := make(chan struct{})
		a.updating = done
		client := a.client
		snapshot := cred
		doRefresh := needsRefresh
		a.mu.Unlock()

		updateCtx, cancel := context.WithTimeout(ctx, authNetworkTimeout)
		var next credential.Credential
		var err error
		if doRefresh {
			next, err = RefreshCredential(updateCtx, client, snapshot)
		} else {
			next, err = enrichCredentialProfile(updateCtx, client, snapshot)
		}
		cancel()

		a.mu.Lock()
		if err == nil {
			a.cred = next
			a.signer = nil
			a.updateRetryAt = time.Time{}
			a.lastUpdateErr = nil
			a.persistDirty = true
		} else {
			a.updateRetryAt = time.Now().Add(authUpdateRetryDelay)
			a.lastUpdateErr = err
		}
		close(done)
		a.updating = nil
		a.mu.Unlock()

		if err != nil {
			if needsMigration || forceRefresh || accessExpired {
				return err
			}
			slog.Warn("qoder credential refresh deferred", "error", err)
			return nil
		}
		a.persistDirtyBestEffort(false)
		forceRefresh = false
	}
}

func (a *AuthState) Credential(ctx context.Context) (credential.Credential, error) {
	if err := a.ensureReady(ctx, false); err != nil {
		return credential.Credential{}, err
	}
	a.mu.Lock()
	cred := a.cred
	a.mu.Unlock()
	return cred, nil
}

func (a *AuthState) ForceRefresh(ctx context.Context) error {
	return a.ensureReady(ctx, true)
}

func (a *AuthState) InferSigner(ctx context.Context) (*InferSigner, error) {
	if err := a.ensureReady(ctx, false); err != nil {
		return nil, err
	}

	a.mu.Lock()
	if a.signer != nil {
		signer := a.signer
		a.mu.Unlock()
		return signer, nil
	}
	signer, err := NewInferSigner(a.cred)
	if err != nil {
		a.mu.Unlock()
		return nil, err
	}
	normalized := signer.Credential()
	if normalized.EncryptUserInfo != a.cred.EncryptUserInfo || normalized.CosyKey != a.cred.CosyKey {
		a.cred = normalized
		a.persistDirty = true
	}
	a.signer = signer
	a.mu.Unlock()
	a.persistDirtyBestEffort(false)
	return signer, nil
}
