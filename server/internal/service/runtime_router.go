package service

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/runtimeusage"
	"github.com/multica-ai/multica/server/internal/util"
)

// RuntimeCapacityProvider abstracts usage snapshot retrieval for testability.
type RuntimeCapacityProvider interface {
	GetSnapshot(ctx context.Context, provider, model string) *runtimeusage.RuntimeUsageSnapshot
}

// defaultCapacityProvider forwards to runtimeusage.Default.
type defaultCapacityProvider struct{}

func (d defaultCapacityProvider) GetSnapshot(ctx context.Context, provider, model string) *runtimeusage.RuntimeUsageSnapshot {
	return runtimeusage.Default.GetSnapshot(ctx, provider, model)
}

// candidateEvaluation holds scoring criteria for one candidate runtime.
type candidateEvaluation struct {
	runtime           db.AgentRuntime
	isOnline          bool
	tier              runtimeusage.CapacityTier
	remainingPercent  float64
	activeTaskCount   int64
	hasStickyAffinity bool
}

// isCandidateOnline returns true if runtime status is "online" or heartbeat is fresh (< 3m).
func isCandidateOnline(rt db.AgentRuntime, now time.Time) bool {
	if rt.Status == "online" {
		return true
	}
	if rt.LastSeenAt.Valid && now.Sub(rt.LastSeenAt.Time) < 3*time.Minute {
		return true
	}
	return false
}

// extractRemainingPercent retrieves the lowest remaining quota percent across windows.
func extractRemainingPercent(snap *runtimeusage.RuntimeUsageSnapshot) float64 {
	if snap == nil {
		return 100.0
	}
	rem := 100.0
	hasMetric := false
	if snap.Session5h != nil {
		rem = snap.Session5h.RemainingPercent
		hasMetric = true
	}
	if snap.Weekly7d != nil {
		if !hasMetric || snap.Weekly7d.RemainingPercent < rem {
			rem = snap.Weekly7d.RemainingPercent
		}
		hasMetric = true
	}
	return rem
}

// RankCandidateRuntimes sorts candidate evaluations from best to worst.
func RankCandidateRuntimes(candidates []candidateEvaluation, strategy string) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]

		// 1. Online candidates strictly precede offline candidates.
		if a.isOnline != b.isOnline {
			return a.isOnline
		}

		// If strategy is "least_busy", prioritize active task count first, then quota.
		if strategy == "least_busy" {
			if a.activeTaskCount != b.activeTaskCount {
				return a.activeTaskCount < b.activeTaskCount
			}
		}

		// 2. Do not route to EXHAUSTED runtimes if an unexhausted online runtime is available.
		aExhausted := a.tier == runtimeusage.CapacityExhausted
		bExhausted := b.tier == runtimeusage.CapacityExhausted
		if aExhausted != bExhausted {
			return !aExhausted
		}

		// 3. Affinity check: If a candidate has sticky session continuity on this issue
		// and is healthy (not critical or exhausted), preserve continuity.
		if a.hasStickyAffinity != b.hasStickyAffinity {
			if a.hasStickyAffinity && a.tier.Rank() >= runtimeusage.CapacityLow.Rank() {
				return true
			}
			if b.hasStickyAffinity && b.tier.Rank() >= runtimeusage.CapacityLow.Rank() {
				return false
			}
		}

		// 4. Capacity Tier: higher headroom tier wins.
		// CapacityAmple (3) > CapacityLow (2) > CapacityUnknown (-1) > CapacityCritical (1) > CapacityExhausted (0)
		aRank := a.tier.Rank()
		bRank := b.tier.Rank()
		if a.tier == runtimeusage.CapacityUnknown {
			aRank = 2 // treat Unknown as Low/Neutral
		}
		if b.tier == runtimeusage.CapacityUnknown {
			bRank = 2
		}
		if aRank != bRank {
			return aRank > bRank
		}

		// 5. Remaining quota percentage: if difference is notable (> 5%), higher headroom wins.
		diff := a.remainingPercent - b.remainingPercent
		if diff > 5.0 {
			return true
		} else if diff < -5.0 {
			return false
		}

		// 6. Least-busy fallback: candidate with fewer active tasks wins.
		if a.activeTaskCount != b.activeTaskCount {
			return a.activeTaskCount < b.activeTaskCount
		}

		// 7. Deterministic tie-breaker on UUID string.
		return util.UUIDToString(a.runtime.ID) < util.UUIDToString(b.runtime.ID)
	})
}

// ResolveOptimalRuntime evaluates candidate runtimes for an agent and returns
// the optimal runtime UUID according to health, capacity headroom, and load.
func (s *TaskService) ResolveOptimalRuntime(
	ctx context.Context,
	agent db.Agent,
	issueID pgtype.UUID,
) (pgtype.UUID, error) {
	return s.resolveOptimalRuntimeWithProvider(ctx, agent, issueID, defaultCapacityProvider{})
}

func (s *TaskService) resolveOptimalRuntimeWithProvider(
	ctx context.Context,
	agent db.Agent,
	issueID pgtype.UUID,
	capProvider RuntimeCapacityProvider,
) (pgtype.UUID, error) {
	// If no candidate pool is defined, fall back to agent.RuntimeID.
	if len(agent.RuntimeCandidateIds) == 0 {
		return agent.RuntimeID, nil
	}

	runtimes, err := s.Queries.GetAgentRuntimes(ctx, agent.RuntimeCandidateIds)
	if err != nil || len(runtimes) == 0 {
		if agent.RuntimeID.Valid {
			return agent.RuntimeID, nil
		}
		return pgtype.UUID{}, fmt.Errorf("failed to load candidate runtimes for agent: %w", err)
	}

	// 1. Fetch active task counts for candidates.
	activeCounts := make(map[string]int64)
	counts, err := s.Queries.CountActiveTasksByRuntimes(ctx, agent.RuntimeCandidateIds)
	if err == nil {
		for _, c := range counts {
			if c.RuntimeID.Valid {
				activeCounts[util.UUIDToString(c.RuntimeID)] = c.ActiveCount
			}
		}
	}

	// 2. Check sticky affinity on issue if present.
	var stickyRuntimeID string
	if issueID.Valid {
		prevRt, err := s.Queries.GetLatestTaskRuntimeForIssueAndAgent(ctx, db.GetLatestTaskRuntimeForIssueAndAgentParams{
			IssueID: issueID,
			AgentID: agent.ID,
		})
		if err == nil && prevRt.Valid {
			stickyRuntimeID = util.UUIDToString(prevRt)
		}
	}

	// 3. Build evaluations.
	now := time.Now()
	evals := make([]candidateEvaluation, 0, len(runtimes))
	model := ""
	if agent.Model.Valid {
		model = agent.Model.String
	}

	for _, rt := range runtimes {
		rtIDStr := util.UUIDToString(rt.ID)
		online := isCandidateOnline(rt, now)

		var tier runtimeusage.CapacityTier = runtimeusage.CapacityUnknown
		var remPercent float64 = 100.0

		if capProvider != nil {
			snap := capProvider.GetSnapshot(ctx, rt.Provider, model)
			if snap != nil {
				tier = snap.EffectiveTier
				remPercent = extractRemainingPercent(snap)
			}
		}

		evals = append(evals, candidateEvaluation{
			runtime:           rt,
			isOnline:          online,
			tier:              tier,
			remainingPercent:  remPercent,
			activeTaskCount:   activeCounts[rtIDStr],
			hasStickyAffinity: stickyRuntimeID != "" && stickyRuntimeID == rtIDStr,
		})
	}

	strategy := agent.RoutingStrategy
	if strategy == "" {
		strategy = "capacity_headroom"
	}
	RankCandidateRuntimes(evals, strategy)

	best := evals[0]
	// If even the top ranked candidate is offline, check if agent.RuntimeID is valid.
	if !best.isOnline && agent.RuntimeID.Valid {
		return agent.RuntimeID, nil
	}

	slog.Info("resolved optimal runtime for task dispatch",
		"agent_id", util.UUIDToString(agent.ID),
		"selected_runtime_id", util.UUIDToString(best.runtime.ID),
		"selected_runtime_name", best.runtime.Name,
		"tier", string(best.tier),
		"remaining_percent", best.remainingPercent,
		"active_tasks", best.activeTaskCount,
		"sticky", best.hasStickyAffinity,
		"is_online", best.isOnline,
	)

	return best.runtime.ID, nil
}
// resolveAgentTaskRuntime determines the target runtime for an agent run,
// consulting the candidate pool router if dynamic routing is configured,
// or falling back to the agent's pinned RuntimeID.
func (s *TaskService) resolveAgentTaskRuntime(ctx context.Context, agent db.Agent, issueID pgtype.UUID) (pgtype.UUID, error) {
	if len(agent.RuntimeCandidateIds) > 0 {
		resolved, err := s.ResolveOptimalRuntime(ctx, agent, issueID)
		if err == nil && resolved.Valid {
			return resolved, nil
		}
	}
	if agent.RuntimeID.Valid {
		return agent.RuntimeID, nil
	}
	return pgtype.UUID{}, fmt.Errorf("agent has no runtime")
}

