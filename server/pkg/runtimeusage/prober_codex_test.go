package runtimeusage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCodexRPCResponseParsing(t *testing.T) {
	mockResponse := `{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "rateLimits": {
      "limitId": "codex-sub",
      "planType": "pro",
      "primary": {
        "usedPercent": 88.0,
        "windowDurationMins": 300,
        "resetsAt": 1759000000
      },
      "secondary": {
        "usedPercent": 35.0,
        "windowDurationMins": 10080,
        "resetsAt": 1759500000
      }
    },
    "rateLimitResetCredits": {
      "availableCount": 2
    }
  }
}`

	var rpcResp codexRPCResponse
	if err := json.Unmarshal([]byte(mockResponse), &rpcResp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if rpcResp.Result == nil || rpcResp.Result.RateLimits == nil {
		t.Fatal("Expected non-nil result")
	}

	rl := rpcResp.Result.RateLimits
	snap := &RuntimeUsageSnapshot{
		Provider:  "codex",
		CheckedAt: time.Now().UTC(),
	}

	if rpcResp.Result.RateLimitResetCredits != nil {
		snap.ResetCredits = &rpcResp.Result.RateLimitResetCredits.AvailableCount
	}

	if rl.Primary != nil {
		remFrac := (100.0 - rl.Primary.UsedPercent) / 100.0
		m := NewWindowMetrics("codex-session", "5-Hour Session", remFrac, nil)
		snap.Session5h = &m
	}

	if rl.Secondary != nil {
		remFrac := (100.0 - rl.Secondary.UsedPercent) / 100.0
		m := NewWindowMetrics("codex-weekly", "Weekly Limit", remFrac, nil)
		snap.Weekly7d = &m
	}

	snap.ComputeEffectiveTier()

	if snap.Session5h == nil || snap.Session5h.RemainingPercent != 12.0 {
		t.Errorf("Expected 5h remaining 12.0%%, got %v", snap.Session5h)
	}
	if snap.Weekly7d == nil || snap.Weekly7d.RemainingPercent != 65.0 {
		t.Errorf("Expected 7d remaining 65.0%%, got %v", snap.Weekly7d)
	}
	if snap.EffectiveTier != CapacityCritical {
		t.Errorf("Expected effective tier CRITICAL (< 15%%), got %v", snap.EffectiveTier)
	}
	if snap.ResetCredits == nil || *snap.ResetCredits != 2 {
		t.Errorf("Expected 2 reset credits, got %v", snap.ResetCredits)
	}
}
