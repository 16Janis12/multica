package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
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

func TestIsTaskRuntimeAuthorized(t *testing.T) {
	primaryID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	cand1 := util.MustParseUUID("00000000-0000-0000-0000-000000000002")
	cand2 := util.MustParseUUID("00000000-0000-0000-0000-000000000003")
	unrelated := util.MustParseUUID("00000000-0000-0000-0000-000000000009")

	agent := db.Agent{
		RuntimeID:           primaryID,
		RuntimeCandidateIds: []pgtype.UUID{cand1, cand2},
	}

	if !isTaskRuntimeAuthorized(agent, primaryID) {
		t.Fatalf("expected primary runtime to be authorized")
	}
	if !isTaskRuntimeAuthorized(agent, cand1) {
		t.Fatalf("expected candidate 1 to be authorized")
	}
	if !isTaskRuntimeAuthorized(agent, cand2) {
		t.Fatalf("expected candidate 2 to be authorized")
	}
	if isTaskRuntimeAuthorized(agent, unrelated) {
		t.Fatalf("unrelated runtime must not be authorized")
	}
}

func TestSanitizeTaskAgentModel(t *testing.T) {
	// Compatible models stay intact
	m, tl, st := sanitizeTaskAgentModel("antigravity", "gemini-2.5-flash", "high", "standard")
	if m != "gemini-2.5-flash" || tl != "high" || st != "standard" {
		t.Fatalf("expected antigravity gemini model preserved, got %q, %q, %q", m, tl, st)
	}

	m, tl, st = sanitizeTaskAgentModel("claude", "claude-sonnet-4-6", "high", "")
	if m != "claude-sonnet-4-6" || tl != "high" {
		t.Fatalf("expected claude model preserved, got %q, %q", m, tl)
	}

	// Incompatible cross-provider models are cleared to default
	m, tl, st = sanitizeTaskAgentModel("claude", "gemini-2.5-flash", "high", "standard")
	if m != "" || tl != "" || st != "" {
		t.Fatalf("expected incompatible gemini model cleared for claude, got %q, %q, %q", m, tl, st)
	}

	m, tl, st = sanitizeTaskAgentModel("antigravity", "claude-sonnet-4-6", "high", "")
	if m != "" || tl != "" || st != "" {
		t.Fatalf("expected incompatible claude model cleared for antigravity, got %q, %q, %q", m, tl, st)
	}

	m, tl, st = sanitizeTaskAgentModel("codex", "gemini-2.5-flash", "", "")
	if m != "" {
		t.Fatalf("expected incompatible gemini model cleared for codex, got %q", m)
	}
}

func TestAgentToResponse_AllowMemberCandidates(t *testing.T) {
	h := &Handler{}
	agent := db.Agent{
		ID:                    util.MustParseUUID("10000000-0000-0000-0000-000000000001"),
		WorkspaceID:           util.MustParseUUID("20000000-0000-0000-0000-000000000001"),
		Name:                  "Member Candidates Agent",
		AllowMemberCandidates: true,
		RuntimeConfig:         []byte("{}"),
		CustomArgs:            []byte("[]"),
		Status:                "idle",
		PermissionMode:        "private",
	}

	resp := h.agentToResponse(agent)
	if !resp.AllowMemberCandidates {
		t.Fatalf("expected AllowMemberCandidates to be true")
	}

	encoded, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal AgentResponse: %v", err)
	}
	var jsonMap map[string]any
	if err := json.Unmarshal(encoded, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v", err)
	}
	if v, ok := jsonMap["allow_member_candidates"].(bool); !ok || !v {
		t.Fatalf("expected allow_member_candidates=true in JSON, got %v", jsonMap["allow_member_candidates"])
	}
}

func TestUpdateAgent_AllowMemberCandidatesWorkflow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ownerID := dbfx.User(t, "Pool Owner", "pool-owner@multica.test")
	dbfx.Member(t, testWorkspaceID, ownerID, "member")

	memberID := dbfx.User(t, "Pool Teammate", "pool-teammate@multica.test")
	dbfx.Member(t, testWorkspaceID, memberID, "member")

	outsiderID := dbfx.User(t, "Pool Outsider", "pool-outsider@multica.test")
	dbfx.Member(t, testWorkspaceID, outsiderID, "member")

	ownerRuntimeID := dbfx.Runtime(t, "Owner Runtime", testutil.Cols{
		"owner_id":   ownerID,
		"visibility": "private",
	})
	memberRuntimeID := dbfx.Runtime(t, "Member Runtime", testutil.Cols{
		"owner_id":   memberID,
		"visibility": "private",
	})
	outsiderRuntimeID := dbfx.Runtime(t, "Outsider Runtime", testutil.Cols{
		"owner_id":   outsiderID,
		"visibility": "private",
	})

	agentID := dbfx.Agent(t, "member-pool-test-agent", ownerRuntimeID, testutil.Cols{
		"owner_id":                ownerID,
		"runtime_candidate_ids":    []string{ownerRuntimeID},
		"allow_member_candidates": false,
	})

	// 1. When allow_member_candidates is false, teammate cannot update runtime_candidate_ids
	req := withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"runtime_candidate_ids": []string{ownerRuntimeID, memberRuntimeID}}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, req).Want(http.StatusForbidden)

	// 2. Owner toggles allow_member_candidates = true
	toggleReq := withURLParam(newRequestAs(ownerID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"allow_member_candidates": true}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, toggleReq).Want(http.StatusOK)

	// 3. Teammate cannot update other fields (e.g. name) even if allow_member_candidates is true
	nameReq := withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"name": "Hacked Name"}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, nameReq).Want(http.StatusForbidden)

	// 4. Teammate cannot add outsider's private runtime
	tryOutsiderReq := withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"runtime_candidate_ids": []string{outsiderRuntimeID}}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, tryOutsiderReq).Want(http.StatusForbidden)

	// 5. Teammate successfully adds their own private runtime
	addMemberReq := withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"runtime_candidate_ids": []string{memberRuntimeID}}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, addMemberReq).Want(http.StatusOK)

	// Verify in DB that ownerRuntimeID is preserved and memberRuntimeID is added
	var cands []string
	dbfx.QueryRow(t, "SELECT runtime_candidate_ids::text[] FROM agent WHERE id = $1", agentID).Scan(&cands)
	if len(cands) != 2 {
		t.Fatalf("expected 2 candidates in pool, got %v", cands)
	}

	// 6. Teammate removes their own runtime by sending an empty candidate list
	removeMemberReq := withURLParam(newRequestAs(memberID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"runtime_candidate_ids": []string{}}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, removeMemberReq).Want(http.StatusOK)

	// Verify that ownerRuntimeID is still preserved
	dbfx.QueryRow(t, "SELECT runtime_candidate_ids::text[] FROM agent WHERE id = $1", agentID).Scan(&cands)
	if len(cands) != 1 || cands[0] != ownerRuntimeID {
		t.Fatalf("expected owner candidate to remain in pool, got %v", cands)
	}

	// 7. Owner removes all candidates by sending an empty candidate list []string{}
	removeOwnerReq := withURLParam(newRequestAs(ownerID, http.MethodPatch, "/api/agents/"+agentID,
		map[string]any{"runtime_candidate_ids": []string{}}), "id", agentID)
	testutil.Call(t, testHandler.UpdateAgent, removeOwnerReq).Want(http.StatusOK)

	// Verify that candidate pool is now cleared
	var clearedCands []string
	dbfx.QueryRow(t, "SELECT runtime_candidate_ids::text[] FROM agent WHERE id = $1", agentID).Scan(&clearedCands)
	if len(clearedCands) != 0 {
		t.Fatalf("expected candidate pool to be cleared, got %v", clearedCands)
	}
}
