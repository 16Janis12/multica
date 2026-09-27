package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/runtimeusage"
)

func TestGetRuntimeCapacity_SnapshotJSON(t *testing.T) {
	// Verify that RuntimeUsageSnapshot serializes as expected for the UI consumer
	now := time.Now()
	reset5h := now.Add(2 * time.Hour)
	reset7d := now.Add(48 * time.Hour)

	snap := &runtimeusage.RuntimeUsageSnapshot{
		Provider:      "antigravity",
		EffectiveTier: runtimeusage.CapacityAmple,
		Session5h: &runtimeusage.WindowMetrics{
			ID:               "session-5h",
			Label:            "5-Hour Session",
			UsedPercent:      15.0,
			RemainingPercent: 85.0,
			ResetsAt:         &reset5h,
			TimeUntilReset:   "2h 00m",
			Tier:             runtimeusage.CapacityAmple,
		},
		Weekly7d: &runtimeusage.WindowMetrics{
			ID:               "weekly-7d",
			Label:            "7-Day Weekly Limit",
			UsedPercent:      40.0,
			RemainingPercent: 60.0,
			ResetsAt:         &reset7d,
			TimeUntilReset:   "2d 00h",
			Tier:             runtimeusage.CapacityAmple,
		},
		CheckedAt: now,
	}

	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("failed to marshal snapshot: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	if m["provider"] != "antigravity" {
		t.Errorf("expected provider antigravity, got %v", m["provider"])
	}
	if m["effective_tier"] != "AMPLE" {
		t.Errorf("expected effective_tier AMPLE, got %v", m["effective_tier"])
	}

	session := m["session_5h"].(map[string]any)
	if session["remaining_percent"] != 85.0 {
		t.Errorf("expected remaining_percent 85.0, got %v", session["remaining_percent"])
	}
	if session["time_until_reset"] != "2h 00m" {
		t.Errorf("expected time_until_reset '2h 00m', got %v", session["time_until_reset"])
	}

	weekly := m["weekly_7d"].(map[string]any)
	if weekly["remaining_percent"] != 60.0 {
		t.Errorf("expected weekly remaining_percent 60.0, got %v", weekly["remaining_percent"])
	}
}

func TestGetRuntimeCapacity_Unauthenticated(t *testing.T) {
	h := &Handler{}
	req := httptest.NewRequest(http.MethodGet, "/api/runtimes/123/capacity", nil)
	w := httptest.NewRecorder()

	h.GetRuntimeCapacity(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("expected non-200 code for unauthenticated request, got %d", w.Code)
	}
}

func TestRuntimeCapacity_DaemonReportStoreAndLookup(t *testing.T) {
	testRuntimeID := "test-rt-capacity-123"
	snap := &runtimeusage.RuntimeUsageSnapshot{
		Provider:      "antigravity",
		EffectiveTier: runtimeusage.CapacityLow,
		Session5h: &runtimeusage.WindowMetrics{
			RemainingPercent: 15.7,
			Tier:             runtimeusage.CapacityLow,
		},
		Weekly7d: &runtimeusage.WindowMetrics{
			RemainingPercent: 30.2,
			Tier:             runtimeusage.CapacityLow,
		},
		CheckedAt: time.Now().UTC(),
	}

	runtimeusage.Default.SetRuntimeSnapshot(testRuntimeID, snap)
	retrieved := runtimeusage.Default.GetRuntimeSnapshot(testRuntimeID)
	if retrieved == nil {
		t.Fatal("expected retrieved snapshot to not be nil")
	}
	if retrieved.EffectiveTier != runtimeusage.CapacityLow {
		t.Errorf("expected EffectiveTier LOW, got %v", retrieved.EffectiveTier)
	}
	if retrieved.Session5h.RemainingPercent != 15.7 {
		t.Errorf("expected Session5h 15.7%%, got %v", retrieved.Session5h.RemainingPercent)
	}
	if retrieved.Weekly7d.RemainingPercent != 30.2 {
		t.Errorf("expected Weekly7d 30.2%%, got %v", retrieved.Weekly7d.RemainingPercent)
	}
}
