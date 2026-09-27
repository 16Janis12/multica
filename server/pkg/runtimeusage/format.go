package runtimeusage

import (
	"fmt"
	"strings"
)

// FormatRosterCapacity returns a compact single-line badge for inclusion in squad leader briefings.
// Example: "[AMPLE | 5h: 98% left, 7d: 43% left]"
func FormatRosterCapacity(snap *RuntimeUsageSnapshot) string {
	if snap == nil || snap.EffectiveTier == CapacityUnknown {
		return ""
	}

	var parts []string
	if snap.Session5h != nil {
		part := fmt.Sprintf("5h: %.0f%% left", snap.Session5h.RemainingPercent)
		if snap.Session5h.Tier == CapacityLow || snap.Session5h.Tier == CapacityCritical {
			if snap.Session5h.TimeUntilReset != "" {
				part += fmt.Sprintf(" (resets in %s)", snap.Session5h.TimeUntilReset)
			}
		}
		parts = append(parts, part)
	}

	if snap.Weekly7d != nil {
		part := fmt.Sprintf("7d: %.0f%% left", snap.Weekly7d.RemainingPercent)
		if snap.Weekly7d.Tier == CapacityCritical {
			if snap.Weekly7d.TimeUntilReset != "" {
				part += fmt.Sprintf(" (resets in %s)", snap.Weekly7d.TimeUntilReset)
			}
		}
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return fmt.Sprintf("[%s]", snap.EffectiveTier)
	}

	return fmt.Sprintf("[%s | %s]", snap.EffectiveTier, strings.Join(parts, ", "))
}

// FormatTurnPrompt generates a cache-safe markdown block for worker agent turn prompts.
func FormatTurnPrompt(snap *RuntimeUsageSnapshot) string {
	if snap == nil || (snap.Session5h == nil && snap.Weekly7d == nil) {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n## Current Runtime Quota\n")

	if snap.Session5h != nil {
		resetInfo := ""
		if snap.Session5h.TimeUntilReset != "" {
			resetInfo = fmt.Sprintf(" (resets in %s)", snap.Session5h.TimeUntilReset)
		}
		sb.WriteString(fmt.Sprintf("- 5-Hour Session: %.1f%% remaining%s [%s]\n",
			snap.Session5h.RemainingPercent, resetInfo, snap.Session5h.Tier))
	}

	if snap.Weekly7d != nil {
		resetInfo := ""
		if snap.Weekly7d.TimeUntilReset != "" {
			resetInfo = fmt.Sprintf(" (resets in %s)", snap.Weekly7d.TimeUntilReset)
		}
		sb.WriteString(fmt.Sprintf("- 7-Day Weekly:   %.1f%% remaining%s [%s]\n",
			snap.Weekly7d.RemainingPercent, resetInfo, snap.Weekly7d.Tier))
	}

	// Add actionable guidance based on effective tier
	switch snap.EffectiveTier {
	case CapacityCritical, CapacityExhausted:
		sb.WriteString("\nNotice: Your runtime quota is critically low. Focus strictly on wrapping up or providing concise output. If completing the task requires significant additional generation or tool turns, leave a clear comment stating the quota boundary and stop or ask for reassignment.\n")
	case CapacityLow:
		sb.WriteString("\nNotice: Your 5-hour or weekly quota is running low. Avoid unnecessary explorations, large multi-file diffs, or redundant tool iterations.\n")
	}

	return sb.String()
}
