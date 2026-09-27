package runtimeusage

import (
	"encoding/json"
	"testing"
	"time"
)

func TestClaudeResponseParsing(t *testing.T) {
	mockResponse := `{
  "five_hour": {
    "utilization": 22.5,
    "resets_at": "2026-09-27T19:30:00Z"
  },
  "seven_day": {
    "utilization": 58.0,
    "resets_at": "2026-10-02T12:00:00Z"
  },
  "limits": [
    {
      "kind": "weekly_scoped",
      "percent": 45.0,
      "resets_at": "2026-10-02T12:00:00Z",
      "scope": {
        "model": {
          "display_name": "Claude 3.7 Sonnet"
        }
      }
    }
  ]
}`

	var usageResp claudeUsageResponseRaw
	if err := json.Unmarshal([]byte(mockResponse), &usageResp); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	snap := &RuntimeUsageSnapshot{
		Provider:     "claude",
		ModelBuckets: make(map[string]WindowMetrics),
		CheckedAt:    time.Now().UTC(),
	}

	normalizeFraction := func(val float64) float64 {
		if val > 1.0 {
			return (100.0 - val) / 100.0
		}
		return 1.0 - val
	}

	if usageResp.FiveHour != nil && usageResp.FiveHour.Utilization != nil {
		remFrac := normalizeFraction(*usageResp.FiveHour.Utilization)
		m := NewWindowMetrics("claude-session", "5-Hour Session", remFrac, nil)
		snap.Session5h = &m
	}

	if usageResp.SevenDay != nil && usageResp.SevenDay.Utilization != nil {
		remFrac := normalizeFraction(*usageResp.SevenDay.Utilization)
		m := NewWindowMetrics("claude-weekly", "Weekly Limit", remFrac, nil)
		snap.Weekly7d = &m
	}

	snap.ComputeEffectiveTier()

	if snap.Session5h == nil || snap.Session5h.RemainingPercent != 77.5 {
		t.Errorf("Expected 5h remaining 77.5%%, got %v", snap.Session5h)
	}
	if snap.Weekly7d == nil || snap.Weekly7d.RemainingPercent != 42.0 {
		t.Errorf("Expected 7d remaining 42.0%%, got %v", snap.Weekly7d)
	}
	if snap.EffectiveTier != CapacityLow {
		t.Errorf("Expected effective tier LOW (min of AMPLE and LOW), got %v", snap.EffectiveTier)
	}
}
