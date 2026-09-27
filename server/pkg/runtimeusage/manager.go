package runtimeusage

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultTTL is the cache retention duration for live quota snapshots.
const DefaultTTL = 60 * time.Second

type cacheEntry struct {
	snapshot  *RuntimeUsageSnapshot
	expiresAt time.Time
}

// Manager coordinates quota probers, caching, and singleflight deduplication.
type Manager struct {
	mu           sync.RWMutex
	probers      map[string]Prober
	cache        map[string]cacheEntry
	runtimeCache map[string]cacheEntry
	ttl          time.Duration
	group        singleflight.Group
}

// NewManager creates a new quota Manager.
func NewManager(ttl time.Duration) *Manager {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Manager{
		probers:      make(map[string]Prober),
		cache:        make(map[string]cacheEntry),
		runtimeCache: make(map[string]cacheEntry),
		ttl:          ttl,
	}
}

// SetRuntimeSnapshot caches a capacity snapshot reported directly by a runtime daemon.
func (m *Manager) SetRuntimeSnapshot(runtimeID string, snap *RuntimeUsageSnapshot) {
	if runtimeID == "" || snap == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimeCache[runtimeID] = cacheEntry{
		snapshot:  snap,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
}

// GetRuntimeSnapshot returns the latest daemon-reported capacity snapshot for a runtime.
func (m *Manager) GetRuntimeSnapshot(runtimeID string) *RuntimeUsageSnapshot {
	if runtimeID == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.runtimeCache[runtimeID]
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.snapshot
	}
	return nil
}

// Register registers a prober for a specific provider.
func (m *Manager) Register(p Prober) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.probers[p.Provider()] = p
}

// GetSnapshot retrieves the dual-window quota snapshot for a provider and model.
// Results are cached for the configured TTL, and concurrent lookups are deduplicated.
func (m *Manager) GetSnapshot(ctx context.Context, provider, model string) *RuntimeUsageSnapshot {
	if provider == "" {
		return nil
	}

	key := fmt.Sprintf("%s:%s", provider, model)

	// 1. Fast cache read
	m.mu.RLock()
	entry, ok := m.cache[key]
	m.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.snapshot
	}

	// 2. Lookup prober
	m.mu.RLock()
	prober, exists := m.probers[provider]
	m.mu.RUnlock()
	if !exists {
		return nil
	}

	// 3. Singleflight execution to prevent hammering the upstream endpoint
	result, err, _ := m.group.Do(key, func() (interface{}, error) {
		probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		snap, probeErr := prober.Probe(probeCtx, ProbeOptions{Model: model})
		if probeErr != nil {
			slog.Warn("runtime usage probe failed", "provider", provider, "model", model, "err", probeErr)
			// On error, return a snapshot carrying the error so it doesn't immediately re-probe
			return &RuntimeUsageSnapshot{
				Provider:      provider,
				EffectiveTier: CapacityUnknown,
				CheckedAt:     time.Now().UTC(),
				Error:         probeErr.Error(),
			}, nil
		}
		if snap == nil {
			return nil, fmt.Errorf("prober returned nil snapshot")
		}
		snap.ComputeEffectiveTier()
		return snap, nil
	})

	if err != nil || result == nil {
		return nil
	}

	snap := result.(*RuntimeUsageSnapshot)

	// 4. Update cache
	m.mu.Lock()
	m.cache[key] = cacheEntry{
		snapshot:  snap,
		expiresAt: time.Now().Add(m.ttl),
	}
	m.mu.Unlock()

	return snap
}

// Default is the process-wide default runtime usage manager.
var Default = NewManager(DefaultTTL)

func init() {
	Default.Register(NewAntigravityProber())
	Default.Register(NewCodexProber(""))
	Default.Register(NewClaudeProber())
}
