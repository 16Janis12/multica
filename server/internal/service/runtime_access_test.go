package service

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/dispatch"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCanAgentUseRuntime_PureLogic(t *testing.T) {
	wsID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	wsOther := util.MustParseUUID("00000000-0000-0000-0000-000000000002")
	ownerA := util.MustParseUUID("00000000-0000-0000-0000-000000000010")
	ownerB := util.MustParseUUID("00000000-0000-0000-0000-000000000020")
	rt1 := util.MustParseUUID("00000000-0000-0000-0000-000000000100")
	rt2 := util.MustParseUUID("00000000-0000-0000-0000-000000000200")

	ctx := context.Background()

	t.Run("public runtime in same workspace allowed", func(t *testing.T) {
		agent := db.Agent{WorkspaceID: wsID, OwnerID: ownerA}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsID, Visibility: "public"}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || !canUse {
			t.Fatalf("expected true, got %v (err: %v)", canUse, err)
		}
	})

	t.Run("public runtime in different workspace denied", func(t *testing.T) {
		agent := db.Agent{WorkspaceID: wsID, OwnerID: ownerA}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsOther, Visibility: "public"}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || canUse {
			t.Fatalf("expected false, got %v (err: %v)", canUse, err)
		}
	})

	t.Run("private runtime without owner denied", func(t *testing.T) {
		agent := db.Agent{WorkspaceID: wsID, OwnerID: ownerA}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsID, Visibility: "private"}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || canUse {
			t.Fatalf("expected false, got %v (err: %v)", canUse, err)
		}
	})

	t.Run("private runtime matching agent owner allowed", func(t *testing.T) {
		agent := db.Agent{WorkspaceID: wsID, OwnerID: ownerA}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsID, Visibility: "private", OwnerID: ownerA}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || !canUse {
			t.Fatalf("expected true, got %v (err: %v)", canUse, err)
		}
	})

	t.Run("private runtime non-matching owner without allow_member_candidates denied", func(t *testing.T) {
		agent := db.Agent{
			WorkspaceID:           wsID,
			OwnerID:               ownerA,
			RuntimeCandidateIds:   []pgtype.UUID{rt1},
			AllowMemberCandidates: false,
		}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsID, Visibility: "private", OwnerID: ownerB}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || canUse {
			t.Fatalf("expected false, got %v (err: %v)", canUse, err)
		}
	})

	t.Run("private runtime in candidate pool with allow_member_candidates allowed", func(t *testing.T) {
		agent := db.Agent{
			WorkspaceID:           wsID,
			OwnerID:               ownerA,
			RuntimeCandidateIds:   []pgtype.UUID{rt1},
			AllowMemberCandidates: true,
		}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsID, Visibility: "private", OwnerID: ownerB}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || !canUse {
			t.Fatalf("expected true, got %v (err: %v)", canUse, err)
		}
	})

	t.Run("private runtime NOT in candidate pool even with allow_member_candidates denied", func(t *testing.T) {
		agent := db.Agent{
			WorkspaceID:           wsID,
			OwnerID:               ownerA,
			RuntimeCandidateIds:   []pgtype.UUID{rt2}, // rt1 not in pool
			AllowMemberCandidates: true,
		}
		rt := db.AgentRuntime{ID: rt1, WorkspaceID: wsID, Visibility: "private", OwnerID: ownerB}
		canUse, err := CanAgentUseRuntime(ctx, nil, rt, agent)
		if err != nil || canUse {
			t.Fatalf("expected false, got %v (err: %v)", canUse, err)
		}
	})
}

func TestRuntimeVerdict_MemberCandidates(t *testing.T) {
	wsID := util.MustParseUUID("00000000-0000-0000-0000-000000000001")
	ownerA := util.MustParseUUID("00000000-0000-0000-0000-000000000010")
	ownerB := util.MustParseUUID("00000000-0000-0000-0000-000000000020")
	rt1 := util.MustParseUUID("00000000-0000-0000-0000-000000000100")

	t.Run("member candidate allowed when allow_member_candidates is true", func(t *testing.T) {
		agent := db.Agent{
			WorkspaceID:           wsID,
			OwnerID:               ownerA,
			RuntimeCandidateIds:   []pgtype.UUID{rt1},
			AllowMemberCandidates: true,
		}
		rt := db.AgentRuntime{
			ID:          rt1,
			WorkspaceID: wsID,
			Visibility:  "private",
			OwnerID:     ownerB,
			Status:      "online",
		}
		verdict := runtimeVerdict(rt, agent)
		if !verdict.Ready() {
			t.Fatalf("expected ready verdict, got %v (reason: %s, detail: %s)", verdict.Availability, verdict.Reason, verdict.Detail)
		}
	})

	t.Run("member candidate blocked when allow_member_candidates is false", func(t *testing.T) {
		agent := db.Agent{
			WorkspaceID:           wsID,
			OwnerID:               ownerA,
			RuntimeCandidateIds:   []pgtype.UUID{rt1},
			AllowMemberCandidates: false,
		}
		rt := db.AgentRuntime{
			ID:          rt1,
			WorkspaceID: wsID,
			Visibility:  "private",
			OwnerID:     ownerB,
			Status:      "online",
		}
		verdict := runtimeVerdict(rt, agent)
		if verdict.Availability != AgentBlocked || verdict.Reason != dispatch.ReasonRuntimeAccessDenied {
			t.Fatalf("expected AgentBlocked with ReasonRuntimeAccessDenied, got %v (%s)", verdict.Availability, verdict.Reason)
		}
	})
}
