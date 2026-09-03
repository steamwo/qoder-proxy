package qoder

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/steamwo/qoder-proxy/internal/credential"
)

type quotaRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f quotaRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type quotaTimeoutError struct{}

func (quotaTimeoutError) Error() string   { return "TLS handshake timeout" }
func (quotaTimeoutError) Timeout() bool   { return true }
func (quotaTimeoutError) Temporary() bool { return true }

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

func TestFetchQuotaRetriesTransientNetworkError(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: quotaRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, quotaTimeoutError{}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":{"subscriptionTitle":"Pro","userQuota":{"total":100,"remaining":75}}}`)),
			Request:    req,
		}, nil
	})}

	snapshot, err := FetchQuota(context.Background(), client, credential.Credential{Token: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d; want 2", attempts)
	}
	if snapshot.User == nil || snapshot.User.Remaining != 75 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestFetchQuotaDoesNotRetryPermanentNetworkError(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: quotaRoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		attempts++
		return nil, errors.New("permanent failure")
	})}

	_, err := FetchQuota(context.Background(), client, credential.Credential{Token: "test-token"})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("attempts=%d; want 1", attempts)
	}
}

func TestQuotaHTTPClientKeepsCustomTransport(t *testing.T) {
	transport := quotaRoundTripperFunc(func(req *http.Request) (*http.Response, error) { return nil, errors.New("unused") })
	client := &http.Client{Transport: transport}
	if got := quotaHTTPClient(client); got != client {
		t.Fatal("custom HTTP client should be left untouched")
	}
}

func TestQuotaHTTPClientScopesDefaultTLSHandshakeTimeout(t *testing.T) {
	client := quotaHTTPClient(http.DefaultClient)
	if client == http.DefaultClient {
		t.Fatal("quota client must not mutate http.DefaultClient")
	}
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type=%T", client.Transport)
	}
	if transport.TLSHandshakeTimeout != quotaTLSHandshakeTimeout {
		t.Fatalf("TLSHandshakeTimeout=%s; want %s", transport.TLSHandshakeTimeout, quotaTLSHandshakeTimeout)
	}
}
