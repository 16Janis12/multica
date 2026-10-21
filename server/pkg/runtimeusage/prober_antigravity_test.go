package runtimeusage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAntigravityUserAgents(t *testing.T) {
	uas := getAntigravityUserAgents()
	if len(uas) < 2 {
		t.Fatalf("expected at least 2 user agents, got %d", len(uas))
	}
	if !strings.HasPrefix(uas[0], "antigravity/2.23.0") {
		t.Errorf("expected first UA to start with antigravity/2.23.0, got %s", uas[0])
	}
	if !strings.HasPrefix(uas[1], "antigravity/hub/1.0.0") {
		t.Errorf("expected second UA to start with antigravity/hub/1.0.0, got %s", uas[1])
	}
}

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
    },
    {
      "displayName": "Claude and GPT models",
      "buckets": [
        {
          "bucketId": "3p-weekly",
          "displayName": "Weekly Limit Remaining",
          "window": "weekly",
          "resetTime": "2026-09-28T19:31:22Z",
          "remainingFraction": 0.8
        },
        {
          "bucketId": "3p-5h",
          "displayName": "Five Hour Limit Remaining",
          "window": "5h",
          "resetTime": "2026-09-27T18:19:32Z",
          "remainingFraction": 0.5
        }
      ]
    }
  ]
}`

	var receivedUA string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedUA = r.Header.Get("User-Agent")
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

	if !strings.HasPrefix(receivedUA, "antigravity/") {
		t.Errorf("expected received UA to start with antigravity/, got %s", receivedUA)
	}

	if len(raw.Groups) != 2 {
		t.Fatalf("Expected 2 groups, got %d", len(raw.Groups))
	}
	buckets := raw.Groups[0].Buckets
	if len(buckets) != 2 {
		t.Fatalf("Expected 2 buckets for group 0, got %d", len(buckets))
	}
}

func TestLiveAntigravityProbeIfAvailable(t *testing.T) {
	prober := NewAntigravityProber()
	tokenFile := prober.findTokenFile()
	if tokenFile == "" {
		t.Skip("No antigravity token file found, skipping live probe test")
	}

	// Test Gemini model probe
	snap, err := prober.Probe(context.Background(), ProbeOptions{Model: "gemini-3.8-flash"})
	if err != nil {
		t.Fatalf("Live antigravity probe failed for gemini: %v", err)
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
	t.Logf("Live Antigravity Gemini Snapshot: Tier=%s, 5h=%.1f%%, 7d=%.1f%%",
		snap.EffectiveTier, snap.Session5h.RemainingPercent, snap.Weekly7d.RemainingPercent)

	// Test 3P / Claude model probe
	snap3p, err := prober.Probe(context.Background(), ProbeOptions{Model: "claude-3-7-sonnet"})
	if err != nil {
		t.Fatalf("Live antigravity probe failed for claude: %v", err)
	}
	if snap3p == nil {
		t.Fatal("Expected non-nil 3p snapshot")
	}
	if snap3p.Session5h == nil {
		t.Error("Expected non-nil Session5h for 3p")
	}
	t.Logf("Live Antigravity Claude Snapshot: Tier=%s, 5h=%.1f%%, 7d=%.1f%%",
		snap3p.EffectiveTier, snap3p.Session5h.RemainingPercent, snap3p.Weekly7d.RemainingPercent)
}
