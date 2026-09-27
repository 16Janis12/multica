ALTER TABLE agent
    DROP COLUMN IF EXISTS runtime_candidate_ids,
    DROP COLUMN IF EXISTS routing_strategy;
