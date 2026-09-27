package handler

import (
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

func TestAgentToResponse_RuntimeCandidatePool(t *testing.T) {
	h := &Handler{}
	cand1 := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	cand2 := util.MustParseUUID("00000000-0000-0000-0000-000000000002")

	// Case 1: Agent with Candidate IDs and no pinned RuntimeID
	agentWithPool := db.Agent{
		ID:                  util.MustParseUUID("10000000-0000-0000-0000-000000000001"),
		WorkspaceID:         util.MustParseUUID("20000000-0000-0000-0000-000000000001"),
		Name:                "Pool Agent",
		RuntimeID:           pgtype.UUID{}, // No pinned runtime
		RuntimeCandidateIds: []pgtype.UUID{cand1, cand2},
		RoutingStrategy:     "capacity_headroom",
		RuntimeConfig:       []byte("{}"),
		CustomArgs:          []byte("[]"),
		Status:              "idle",
		PermissionMode:      "private",
	}

	resp := h.agentToResponse(agentWithPool)
	if !resp.RuntimeBound {
		t.Fatalf("expected RuntimeBound to be true when RuntimeCandidateIds is non-empty")
	}
	if len(resp.RuntimeCandidateIDs) != 2 {
		t.Fatalf("expected 2 candidate IDs, got %d", len(resp.RuntimeCandidateIDs))
	}
	if resp.RuntimeCandidateIDs[0] != "00000000-0000-0000-0000-000000000001" ||
		resp.RuntimeCandidateIDs[1] != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("unexpected candidate IDs: %v", resp.RuntimeCandidateIDs)
	}
	if resp.RoutingStrategy != "capacity_headroom" {
		t.Fatalf("expected routing_strategy capacity_headroom, got %s", resp.RoutingStrategy)
	}

	// Verify JSON serialization includes runtime_candidate_ids and routing_strategy
	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal AgentResponse: %v", err)
	}

	var jsonMap map[string]any
	if err := json.Unmarshal(encoded, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}

	cands, ok := jsonMap["runtime_candidate_ids"].([]any)
	if !ok || len(cands) != 2 {
		t.Fatalf("expected JSON to contain 2 runtime_candidate_ids, got %v", jsonMap["runtime_candidate_ids"])
	}
	if strat, ok := jsonMap["routing_strategy"].(string); !ok || strat != "capacity_headroom" {
		t.Fatalf("expected routing_strategy in JSON, got %v", jsonMap["routing_strategy"])
	}
}

func TestAgentToResponse_DefaultsWhenUnconfigured(t *testing.T) {
	h := &Handler{}
	agentUnbound := db.Agent{
		ID:             util.MustParseUUID("10000000-0000-0000-0000-000000000001"),
		WorkspaceID:    util.MustParseUUID("20000000-0000-0000-0000-000000000001"),
		Name:           "Unbound Agent",
		RuntimeID:      pgtype.UUID{},
		RuntimeConfig:  []byte("{}"),
		CustomArgs:     []byte("[]"),
		Status:         "idle",
		PermissionMode: "private",
	}

	resp := h.agentToResponse(agentUnbound)
	if resp.RuntimeBound {
		t.Fatalf("expected RuntimeBound to be false for unbound agent")
	}
	if len(resp.RuntimeCandidateIDs) != 0 {
		t.Fatalf("expected empty RuntimeCandidateIDs, got %v", resp.RuntimeCandidateIDs)
	}
	if resp.RoutingStrategy != "capacity_headroom" {
		t.Fatalf("expected default routing_strategy capacity_headroom, got %s", resp.RoutingStrategy)
	}
}
