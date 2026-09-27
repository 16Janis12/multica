package runtimeusage

import (
	"strings"
	"testing"
	"time"
)

func TestClassifyRemainingPercent(t *testing.T) {
	tests := []struct {
		pct  float64
		want CapacityTier
	}{
		{100.0, CapacityAmple},
		{55.0, CapacityAmple},
		{50.0, CapacityLow},
		{20.0, CapacityLow},
		{15.0, CapacityLow},
		{14.9, CapacityCritical},
		{5.0, CapacityCritical},
		{0.0, CapacityExhausted},
		{-5.0, CapacityExhausted},
	}

	for _, tt := range tests {
		got := ClassifyRemainingPercent(tt.pct)
		if got != tt.want {
			t.Errorf("ClassifyRemainingPercent(%v) = %v, want %v", tt.pct, got, tt.want)
		}
	}
}

func TestMinTier(t *testing.T) {
	if got := MinTier(CapacityAmple, CapacityLow); got != CapacityLow {
		t.Errorf("MinTier(Ample, Low) = %v, want %v", got, CapacityLow)
	}
	if got := MinTier(CapacityCritical, CapacityExhausted); got != CapacityExhausted {
		t.Errorf("MinTier(Critical, Exhausted) = %v, want %v", got, CapacityExhausted)
	}
	if got := MinTier(CapacityUnknown, CapacityAmple); got != CapacityAmple {
		t.Errorf("MinTier(Unknown, Ample) = %v, want %v", got, CapacityAmple)
	}
}

func TestFormatRosterCapacity(t *testing.T) {
	resetTime := time.Now().UTC().Add(45 * time.Minute)
	snap := &RuntimeUsageSnapshot{
		Provider:      "antigravity",
		EffectiveTier: CapacityLow,
		Session5h: &WindowMetrics{
			ID:               "5h",
			RemainingPercent: 12.0,
			Tier:             CapacityCritical,
			TimeUntilReset:   "45m",
			ResetsAt:         &resetTime,
		},
		Weekly7d: &WindowMetrics{
			ID:               "weekly",
			RemainingPercent: 74.0,
			Tier:             CapacityAmple,
		},
	}

	formatted := FormatRosterCapacity(snap)
	if !strings.Contains(formatted, "LOW") {
		t.Errorf("Expected formatted string to contain effective tier LOW, got: %s", formatted)
	}
	if !strings.Contains(formatted, "5h: 12% left") {
		t.Errorf("Expected formatted string to contain 5h percent, got: %s", formatted)
	}
	if !strings.Contains(formatted, "resets in 45m") {
		t.Errorf("Expected formatted string to contain reset info, got: %s", formatted)
	}
	if !strings.Contains(formatted, "7d: 74% left") {
		t.Errorf("Expected formatted string to contain 7d percent, got: %s", formatted)
	}
}

func TestFormatTurnPrompt(t *testing.T) {
	snap := &RuntimeUsageSnapshot{
		Provider:      "claude",
		EffectiveTier: CapacityCritical,
		Session5h: &WindowMetrics{
			ID:               "claude-session",
			RemainingPercent: 8.5,
			Tier:             CapacityCritical,
			TimeUntilReset:   "22m",
		},
		Weekly7d: &WindowMetrics{
			ID:               "claude-weekly",
			RemainingPercent: 65.0,
			Tier:             CapacityAmple,
		},
	}

	prompt := FormatTurnPrompt(snap)
	if !strings.Contains(prompt, "Current Runtime Quota") {
		t.Errorf("Expected prompt to contain header, got: %s", prompt)
	}
	if !strings.Contains(prompt, "5-Hour Session: 8.5% remaining (resets in 22m) [CRITICAL]") {
		t.Errorf("Expected 5h session line, got: %s", prompt)
	}
	if !strings.Contains(prompt, "critically low") {
		t.Errorf("Expected critical warning notice, got: %s", prompt)
	}
}
