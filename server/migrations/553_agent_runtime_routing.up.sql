-- Migration 553: Dynamic runtime routing for agents (pool candidates and strategy)
--
-- Adds runtime_candidate_ids and routing_strategy to agent.
-- When runtime_candidate_ids contains runtime UUIDs, task dispatch dynamically
-- evaluates health, quota capacity headroom, and least-busy metrics across the
-- pool, routing tasks to the optimal runtime while preserving agent identity.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE agent
    ADD COLUMN IF NOT EXISTS runtime_candidate_ids UUID[] NULL,
    ADD COLUMN IF NOT EXISTS routing_strategy TEXT NOT NULL DEFAULT 'capacity_headroom';
