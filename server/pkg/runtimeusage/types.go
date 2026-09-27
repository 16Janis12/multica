package runtimeusage

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// CapacityTier represents the high-level capacity level of a runtime.
type CapacityTier string

const (
	CapacityAmple     CapacityTier = "AMPLE"     // > 50% remaining
	CapacityLow       CapacityTier = "LOW"       // 15% - 50% remaining
	CapacityCritical  CapacityTier = "CRITICAL"  // < 15% remaining
	CapacityExhausted CapacityTier = "EXHAUSTED" // 0% remaining / throttled
	CapacityUnknown   CapacityTier = "UNKNOWN"
)

// TierRank orders tiers from most restricted (0) to most available (3).
func (t CapacityTier) rank() int {
	switch t {
	case CapacityExhausted:
		return 0
	case CapacityCritical:
		return 1
	case CapacityLow:
		return 2
	case CapacityAmple:
		return 3
	default:
		return -1
	}
}

// MinTier returns the more restrictive of two capacity tiers.
func MinTier(a, b CapacityTier) CapacityTier {
	if a == CapacityUnknown {
		return b
	}
	if b == CapacityUnknown {
		return a
	}
	if a.rank() <= b.rank() {
		return a
	}
	return b
}

// ClassifyRemainingPercent classifies remaining quota percentage into a CapacityTier.
func ClassifyRemainingPercent(remainingPercent float64) CapacityTier {
	if remainingPercent <= 0 {
		return CapacityExhausted
	}
	if remainingPercent < 15.0 {
		return CapacityCritical
	}
	if remainingPercent <= 50.0 {
		return CapacityLow
	}
	return CapacityAmple
}

// WindowMetrics holds usage and reset timing for a specific quota window (e.g., 5-hour or 7-day).
type WindowMetrics struct {
	ID               string        `json:"id"`
	Label            string        `json:"label"` // e.g. "5-Hour Session", "Weekly Limit"
	UsedPercent      float64       `json:"used_percent"`
	RemainingPercent float64       `json:"remaining_percent"`
	ResetsAt         *time.Time    `json:"resets_at,omitempty"`
	TimeUntilReset   string        `json:"time_until_reset,omitempty"`
	Tier             CapacityTier  `json:"tier"`
}

// NewWindowMetrics builds a WindowMetrics struct from a used or remaining fraction and optional reset time.
func NewWindowMetrics(id, label string, remainingFraction float64, resetsAt *time.Time) WindowMetrics {
	if remainingFraction < 0 {
		remainingFraction = 0
	} else if remainingFraction > 1.0 {
		remainingFraction = 1.0
	}
	remainingPct := math.Round(remainingFraction*1000) / 10 // 1 decimal place
	usedPct := math.Round((1.0-remainingFraction)*1000) / 10

	tier := ClassifyRemainingPercent(remainingPct)
	var timeUntilReset string
	if resetsAt != nil {
		timeUntilReset = FormatDurationUntil(*resetsAt)
	}

	return WindowMetrics{
		ID:               id,
		Label:            label,
		UsedPercent:      usedPct,
		RemainingPercent: remainingPct,
		ResetsAt:         resetsAt,
		TimeUntilReset:   timeUntilReset,
		Tier:             tier,
	}
}

// RuntimeUsageSnapshot holds a unified snapshot of usage limits across both 5h and weekly horizons.
type RuntimeUsageSnapshot struct {
	Provider      string                   `json:"provider"` // "antigravity" | "claude" | "codex"
	EffectiveTier CapacityTier             `json:"effective_tier"`
	Session5h     *WindowMetrics           `json:"session_5h,omitempty"`
	Weekly7d      *WindowMetrics           `json:"weekly_7d,omitempty"`
	ResetCredits  *int64                   `json:"reset_credits,omitempty"`
	ModelBuckets  map[string]WindowMetrics `json:"model_buckets,omitempty"`
	CheckedAt     time.Time                `json:"checked_at"`
	Error         string                   `json:"error,omitempty"`
}

// ComputeEffectiveTier derives the lowest tier between the 5h session and weekly windows.
func (s *RuntimeUsageSnapshot) ComputeEffectiveTier() {
	if s.Error != "" && s.Session5h == nil && s.Weekly7d == nil {
		s.EffectiveTier = CapacityUnknown
		return
	}
	tier := CapacityAmple
	if s.Session5h != nil {
		tier = MinTier(tier, s.Session5h.Tier)
	}
	if s.Weekly7d != nil {
		tier = MinTier(tier, s.Weekly7d.Tier)
	}
	if s.Session5h == nil && s.Weekly7d == nil {
		tier = CapacityUnknown
	}
	s.EffectiveTier = tier
}

// FormatDurationUntil formats remaining time until reset into a clean compact string like "45m", "3h 20m", or "1d 5h".
func FormatDurationUntil(t time.Time) string {
	now := time.Now().UTC()
	diff := t.Sub(now)
	if diff <= 0 {
		return "ready"
	}

	days := int(diff.Hours()) / 24
	hours := int(diff.Hours()) % 24
	minutes := int(diff.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, " ")
}
