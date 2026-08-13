package qoder

import "testing"

func TestParseQuota(t *testing.T) {
	payload := map[string]any{"data": map[string]any{
		"subscriptionTitle":    "Pro",
		"expiresAt":            float64(1790000000000),
		"totalUsagePercentage": float64(25),
		"userQuota":            map[string]any{"total": float64(1000), "remaining": float64(750)},
		"orgResourcePackage":   map[string]any{"total": float64(500), "remaining": float64(400), "percentage": float64(20)},
	}}
	q := ParseQuota(payload)
	if q.Plan != "Pro" {
		t.Fatalf("plan=%q", q.Plan)
	}
	if q.User == nil || q.User.Remaining != 750 || q.User.UsedPercent != 25 || q.User.RemainingPercent != 75 {
		t.Fatalf("user=%+v", q.User)
	}
	if q.Organization == nil || q.Organization.Remaining != 400 || q.Organization.RemainingPercent != 80 || q.Organization.UsedPercent != 20 {
		t.Fatalf("org=%+v", q.Organization)
	}
	if q.ExpiresAt.IsZero() {
		t.Fatal("missing expiry")
	}
}

func TestParseQuotaPrefersExplicitRemainingOverAmbiguousPercentage(t *testing.T) {
	payload := map[string]any{"data": map[string]any{
		"subscriptionTitle":    "Pro",
		"totalUsagePercentage": float64(80),
		"userQuota": map[string]any{
			"total":      float64(1000),
			"remaining":  float64(800),
			"percentage": float64(80),
		},
	}}
	q := ParseQuota(payload)
	if q.User == nil {
		t.Fatal("missing user quota")
	}
	if q.User.Remaining != 800 {
		t.Fatalf("remaining=%v", q.User.Remaining)
	}
	if q.User.RemainingPercent != 80 {
		t.Fatalf("remaining percent=%v; want 80", q.User.RemainingPercent)
	}
	if q.User.UsedPercent != 20 {
		t.Fatalf("used percent=%v; want 20", q.User.UsedPercent)
	}
}

func TestParseQuotaKeepsZeroRemainingAsExplicitBalance(t *testing.T) {
	payload := map[string]any{"data": map[string]any{
		"userQuota": map[string]any{
			"total":               float64(1000),
			"remaining":           float64(0),
			"remainingPercentage": float64(100),
		},
	}}
	q := ParseQuota(payload)
	if q.User == nil {
		t.Fatal("missing user quota")
	}
	if q.User.RemainingPercent != 0 || q.User.UsedPercent != 100 {
		t.Fatalf("user=%+v", q.User)
	}
}
