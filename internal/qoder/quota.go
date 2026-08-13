package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

const QuotaURL = "https://openapi.qoder.sh/api/v2/quota/usage"

type QuotaWindow struct {
	Label            string
	Limit            float64
	Remaining        float64
	UsedPercent      float64
	RemainingPercent float64
	ResetAt          time.Time
}

type QuotaSnapshot struct {
	Plan         string
	User         *QuotaWindow
	Organization *QuotaWindow
	ExpiresAt    time.Time
	FetchedAt    time.Time
}

func FetchQuota(ctx context.Context, client *http.Client, cred credential.Credential) (QuotaSnapshot, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, QuotaURL, nil)
	if err != nil {
		return QuotaSnapshot{}, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "qoder-proxy/0.3.3-desktop-ui")
	resp, err := client.Do(req)
	if err != nil {
		return QuotaSnapshot{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return QuotaSnapshot{}, err
	}
	if resp.StatusCode/100 != 2 {
		return QuotaSnapshot{}, fmt.Errorf("qoder quota returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return QuotaSnapshot{}, fmt.Errorf("decode qoder quota: %w", err)
	}
	snapshot := ParseQuota(payload)
	snapshot.FetchedAt = time.Now()
	if snapshot.User == nil && snapshot.Organization == nil && snapshot.Plan == "" {
		return QuotaSnapshot{}, fmt.Errorf("qoder quota payload did not contain recognizable quota fields")
	}
	return snapshot, nil
}

func ParseQuota(payload map[string]any) QuotaSnapshot {
	root := objectMap(firstAny(payload, "data", "result", "payload"))
	if len(root) == 0 {
		root = payload
	}
	snap := QuotaSnapshot{Plan: firstString(root, "plan", "planType", "subscriptionTitle")}
	if snap.Plan == "" {
		snap.Plan = firstString(payload, "plan", "planType", "subscriptionTitle")
	}
	snap.ExpiresAt = parseTimestamp(firstAny(root, "expiresAt", "expires_at"))
	if snap.ExpiresAt.IsZero() {
		snap.ExpiresAt = parseTimestamp(firstAny(payload, "expiresAt", "expires_at"))
	}
	if user := objectMap(firstAny(root, "userQuota", "user_quota")); len(user) > 0 {
		snap.User = parseQuotaWindow("个人额度", user, quotaNumber(root, "totalUsagePercentage"), snap.ExpiresAt)
	}
	if org := objectMap(firstAny(root, "orgResourcePackage", "org_resource_package", "organizationQuota")); len(org) > 0 {
		w := parseQuotaWindow("组织资源包", org, 0, snap.ExpiresAt)
		if w != nil && (w.Limit > 0 || w.Remaining > 0) {
			snap.Organization = w
		}
	}
	if snap.Plan == "" && (snap.User != nil || snap.Organization != nil) {
		snap.Plan = "Qoder"
	}
	return snap
}

func parseQuotaWindow(label string, m map[string]any, fallbackUsed float64, fallbackReset time.Time) *QuotaWindow {
	limit := quotaNumber(m, "total", "limit", "quota", "max")
	remaining, hasRemaining := quotaNumberPresent(m, "remaining", "left", "available")
	used := quotaNumber(m, "percentage", "usedPercent", "used_percent")
	remainPct := quotaNumber(m, "remainingPercentage", "remaining_percentage")

	// The absolute remaining balance is the least ambiguous signal Qoder gives
	// us. Percentage fields have changed shape/meaning across API variants, so
	// whenever both total and remaining are explicitly present, derive both
	// percentages from those values instead of allowing a percentage field to
	// invert the dashboard from remaining to used quota.
	if limit > 0 && hasRemaining {
		remainPct = clampPercent(remaining / limit * 100)
		used = 100 - remainPct
	} else {
		if used == 0 && fallbackUsed > 0 {
			used = fallbackUsed
		}
		if used == 0 && remainPct > 0 {
			used = 100 - remainPct
		}
		if remainPct == 0 && used > 0 {
			remainPct = 100 - used
		}
		if used == 0 && remainPct == 0 && limit > 0 {
			remainPct = clampPercent(remaining / limit * 100)
			used = 100 - remainPct
		}
	}

	reset := parseTimestamp(firstAny(m, "resetAt", "reset_at", "expiresAt", "expires_at"))
	if reset.IsZero() {
		reset = fallbackReset
	}
	if limit == 0 && remaining == 0 && used == 0 && remainPct == 0 && reset.IsZero() {
		return nil
	}
	return &QuotaWindow{Label: label, Limit: limit, Remaining: remaining, UsedPercent: clampPercent(used), RemainingPercent: clampPercent(remainPct), ResetAt: reset}
}

func objectMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}
func firstAny(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v
		}
	}
	return nil
}
func quotaNumber(m map[string]any, keys ...string) float64 {
	v, _ := quotaNumberPresent(m, keys...)
	return v
}
func quotaNumberPresent(m map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		raw, exists := m[k]
		if !exists || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case float64:
			return v, true
		case float32:
			return float64(v), true
		case int:
			return float64(v), true
		case int64:
			return float64(v), true
		case json.Number:
			f, err := v.Float64()
			if err == nil {
				return f, true
			}
		case string:
			var f float64
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%f", &f); err == nil {
				return f, true
			}
		}
	}
	return 0, false
}
func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
func parseTimestamp(v any) time.Time {
	switch x := v.(type) {
	case float64:
		n := int64(x)
		if n > 10_000_000_000 {
			n /= 1000
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0)
		}
	case int64:
		n := x
		if n > 10_000_000_000 {
			n /= 1000
		}
		if n > 1_000_000_000 {
			return time.Unix(n, 0)
		}
	case string:
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(x)); err == nil {
			return t
		}
	}
	return time.Time{}
}
