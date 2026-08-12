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
	if q.User == nil || q.User.Remaining != 750 || q.User.UsedPercent != 25 {
		t.Fatalf("user=%+v", q.User)
	}
	if q.Organization == nil || q.Organization.Remaining != 400 {
		t.Fatalf("org=%+v", q.Organization)
	}
	if q.ExpiresAt.IsZero() {
		t.Fatal("missing expiry")
	}
}
