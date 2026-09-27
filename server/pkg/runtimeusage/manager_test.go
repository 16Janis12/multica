package runtimeusage

import (
	"testing"
	"time"
)

func TestManagerRuntimeSnapshotStoreAndLookup(t *testing.T) {
	mgr := NewManager(60 * time.Second)
	rtID := "rt-test-abc-123"

	snap := &RuntimeUsageSnapshot{
		Provider:      "antigravity",
		EffectiveTier: CapacityLow,
		Session5h: &WindowMetrics{
			RemainingPercent: 15.7,
			Tier:             CapacityLow,
		},
		Weekly7d: &WindowMetrics{
			RemainingPercent: 30.2,
			Tier:             CapacityLow,
		},
		CheckedAt: time.Now().UTC(),
	}

	mgr.SetRuntimeSnapshot(rtID, snap)
	got := mgr.GetRuntimeSnapshot(rtID)
	if got == nil {
		t.Fatal("expected non-nil snapshot from GetRuntimeSnapshot")
	}
	if got.EffectiveTier != CapacityLow {
		t.Errorf("expected CapacityLow, got %v", got.EffectiveTier)
	}
	if got.Session5h.RemainingPercent != 15.7 {
		t.Errorf("expected Session5h 15.7%%, got %v", got.Session5h.RemainingPercent)
	}
	if got.Weekly7d.RemainingPercent != 30.2 {
		t.Errorf("expected Weekly7d 30.2%%, got %v", got.Weekly7d.RemainingPercent)
	}

	// Missing runtime should return nil
	if mgr.GetRuntimeSnapshot("non-existent") != nil {
		t.Errorf("expected nil for non-existent runtime")
	}
}
