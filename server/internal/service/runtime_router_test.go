package service

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/runtimeusage"
	"github.com/multica-ai/multica/server/internal/util"
)

func makeTestRuntime(idStr, name, status string, lastSeen time.Time) db.AgentRuntime {
	return db.AgentRuntime{
		ID:         util.MustParseUUID(idStr),
		Name:       name,
		Status:     status,
		LastSeenAt: pgtype.Timestamptz{Time: lastSeen, Valid: !lastSeen.IsZero()},
		Provider:   "claude",
	}
}

func TestRankCandidateRuntimes_OnlineOverOffline(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "offline", now.Add(-10*time.Minute))
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)

	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: false, tier: runtimeusage.CapacityAmple, remainingPercent: 90},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityLow, remainingPercent: 30},
	}

	RankCandidateRuntimes(candidates, "capacity_headroom")

	if candidates[0].runtime.ID != rt2.ID {
		t.Fatalf("expected online runtime rt2 to be ranked first, got %s", candidates[0].runtime.Name)
	}
}

func TestRankCandidateRuntimes_AvoidExhausted(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "online", now)
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)

	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityExhausted, remainingPercent: 0},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityCritical, remainingPercent: 5},
	}

	RankCandidateRuntimes(candidates, "capacity_headroom")

	if candidates[0].runtime.ID != rt2.ID {
		t.Fatalf("expected non-exhausted rt2 to beat exhausted rt1, got %s", candidates[0].runtime.Name)
	}
}

func TestRankCandidateRuntimes_HighestCapacityHeadroom(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "online", now)
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)
	rt3 := makeTestRuntime("00000000-0000-0000-0000-000000000003", "rt3", "online", now)

	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityLow, remainingPercent: 25},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 85},
		{runtime: rt3, isOnline: true, tier: runtimeusage.CapacityCritical, remainingPercent: 10},
	}

	RankCandidateRuntimes(candidates, "capacity_headroom")

	if candidates[0].runtime.ID != rt2.ID {
		t.Fatalf("expected rt2 (Ample) to be first, got %s", candidates[0].runtime.Name)
	}
	if candidates[1].runtime.ID != rt1.ID {
		t.Fatalf("expected rt1 (Low) to be second, got %s", candidates[1].runtime.Name)
	}
	if candidates[2].runtime.ID != rt3.ID {
		t.Fatalf("expected rt3 (Critical) to be third, got %s", candidates[2].runtime.Name)
	}
}

func TestRankCandidateRuntimes_RemainingPercentWithinSameTier(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "online", now)
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)

	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 60},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 95},
	}

	RankCandidateRuntimes(candidates, "capacity_headroom")

	if candidates[0].runtime.ID != rt2.ID {
		t.Fatalf("expected rt2 (95%%) to beat rt1 (60%%), got %s", candidates[0].runtime.Name)
	}
}

func TestRankCandidateRuntimes_LeastBusyFallback(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "online", now)
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)

	// Both have ~same capacity (within 5%), but rt1 has 3 active tasks while rt2 is idle
	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 82, activeTaskCount: 3},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 80, activeTaskCount: 0},
	}

	RankCandidateRuntimes(candidates, "capacity_headroom")

	if candidates[0].runtime.ID != rt2.ID {
		t.Fatalf("expected idle rt2 to beat busy rt1, got %s", candidates[0].runtime.Name)
	}
}

func TestRankCandidateRuntimes_LeastBusyStrategy(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "online", now)
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)

	// With "least_busy" strategy, activeTaskCount comes first
	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 90, activeTaskCount: 2},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityLow, remainingPercent: 30, activeTaskCount: 0},
	}

	RankCandidateRuntimes(candidates, "least_busy")

	if candidates[0].runtime.ID != rt2.ID {
		t.Fatalf("expected rt2 (0 active tasks) to beat rt1 (2 active tasks) under least_busy, got %s", candidates[0].runtime.Name)
	}
}

func TestRankCandidateRuntimes_StickyAffinityAndFailover(t *testing.T) {
	now := time.Now()
	rt1 := makeTestRuntime("00000000-0000-0000-0000-000000000001", "rt1", "online", now)
	rt2 := makeTestRuntime("00000000-0000-0000-0000-000000000002", "rt2", "online", now)

	// 1. Healthy sticky affinity: rt1 previously ran a task for this issue and is healthy (Ample/Low)
	candidates := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityLow, remainingPercent: 40, hasStickyAffinity: true},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 85, hasStickyAffinity: false},
	}
	RankCandidateRuntimes(candidates, "capacity_headroom")
	if candidates[0].runtime.ID != rt1.ID {
		t.Fatalf("expected healthy sticky rt1 to maintain continuity, got %s", candidates[0].runtime.Name)
	}

	// 2. Failover when sticky runtime is exhausted
	candidatesThrottled := []candidateEvaluation{
		{runtime: rt1, isOnline: true, tier: runtimeusage.CapacityExhausted, remainingPercent: 0, hasStickyAffinity: true},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityAmple, remainingPercent: 85, hasStickyAffinity: false},
	}
	RankCandidateRuntimes(candidatesThrottled, "capacity_headroom")
	if candidatesThrottled[0].runtime.ID != rt2.ID {
		t.Fatalf("expected failover to rt2 when sticky rt1 is exhausted, got %s", candidatesThrottled[0].runtime.Name)
	}

	// 3. Failover when sticky runtime goes offline
	candidatesOffline := []candidateEvaluation{
		{runtime: rt1, isOnline: false, tier: runtimeusage.CapacityAmple, remainingPercent: 90, hasStickyAffinity: true},
		{runtime: rt2, isOnline: true, tier: runtimeusage.CapacityLow, remainingPercent: 40, hasStickyAffinity: false},
	}
	RankCandidateRuntimes(candidatesOffline, "capacity_headroom")
	if candidatesOffline[0].runtime.ID != rt2.ID {
		t.Fatalf("expected failover to online rt2 when sticky rt1 is offline, got %s", candidatesOffline[0].runtime.Name)
	}
}

type mockCapacityProvider struct {
	snapshots map[string]*runtimeusage.RuntimeUsageSnapshot
}

func (m mockCapacityProvider) GetSnapshot(ctx context.Context, provider, model string) *runtimeusage.RuntimeUsageSnapshot {
	return m.snapshots[provider]
}
