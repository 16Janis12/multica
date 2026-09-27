package runtimeusage

import (
	"context"
)

// ProbeOptions configures a quota retrieval probe.
type ProbeOptions struct {
	// Model is the active model assigned to the agent/runtime (e.g. "gemini-3.8-flash", "claude-3-7-sonnet").
	Model string
	// DaemonHealthURL or port if communicating with local daemon.
	DaemonHealthURL string
}

// Prober retrieves a live dual-window usage snapshot from an agent runtime provider.
type Prober interface {
	// Provider returns the identifier for this provider (e.g., "antigravity", "claude", "codex").
	Provider() string
	// Probe retrieves the current usage snapshot.
	Probe(ctx context.Context, opts ProbeOptions) (*RuntimeUsageSnapshot, error)
}
