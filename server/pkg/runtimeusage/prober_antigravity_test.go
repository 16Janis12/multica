package runtimeusage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAntigravityProberParsing(t *testing.T) {
	mockResponse := `{
  "groups": [
    {
      "displayName": "Gemini Models",
      "buckets": [
        {
          "bucketId": "gemini-weekly",
          "displayName": "Weekly Limit Remaining",
          "window": "weekly",
          "resetTime": "2026-09-28T19:31:22Z",
          "remainingFraction": 0.432
        },
        {
          "bucketId": "gemini-5h",
          "displayName": "Five Hour Limit Remaining",
          "window": "5h",
          "resetTime": "2026-09-27T18:19:32Z",
          "remainingFraction": 0.985
        }
      ]
    }
  ]
}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(mockResponse))
	}))
	defer server.Close()

	p := &AntigravityProber{
		httpClient: server.Client(),
	}

	// Override endpoints for test
	oldEndpoints := antigravityQuotaEndpoints
	antigravityQuotaEndpoints = []string{server.URL}
	defer func() { antigravityQuotaEndpoints = oldEndpoints }()

	raw, err := p.fetchQuotaSummary(context.Background(), "mock_access_token", "test-project")
	if err != nil {
		t.Fatalf("fetchQuotaSummary failed: %v", err)
	}

	if len(raw.Groups) != 1 {
		t.Fatalf("Expected 1 group, got %d", len(raw.Groups))
	}
	buckets := raw.Groups[0].Buckets
	if len(buckets) != 2 {
		t.Fatalf("Expected 2 buckets, got %d", len(buckets))
	}
}

func TestLiveAntigravityProbeIfAvailable(t *testing.T) {
	prober := NewAntigravityProber()
	tokenFile := prober.findTokenFile()
	if tokenFile == "" {
		t.Skip("No antigravity token file found, skipping live probe test")
	}

	snap, err := prober.Probe(context.Background(), ProbeOptions{Model: "gemini-3.8-flash"})
	if err != nil {
		t.Fatalf("Live antigravity probe failed: %v", err)
	}

	if snap == nil {
		t.Fatal("Expected non-nil snapshot")
	}
	if snap.Provider != "antigravity" {
		t.Errorf("Expected provider antigravity, got %s", snap.Provider)
	}
	if snap.Session5h == nil {
		t.Error("Expected non-nil Session5h")
	}
	if snap.Weekly7d == nil {
		t.Error("Expected non-nil Weekly7d")
	}
	if snap.EffectiveTier == CapacityUnknown {
		t.Errorf("Expected known effective tier, got %v", snap.EffectiveTier)
	}
	t.Logf("Live Antigravity Snapshot: Tier=%s, 5h=%.1f%%, 7d=%.1f%%",
		snap.EffectiveTier, snap.Session5h.RemainingPercent, snap.Weekly7d.RemainingPercent)
}
