package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// UserCanAccessAgent reports whether the given user has access to the agent.
// Access is granted if:
// 1. The user is a workspace owner or admin.
// 2. The user is the agent owner (and an active member of the workspace).
// 3. The agent is public_to and the user matches the invocation targets
//    (target_type 'workspace' for any workspace member, or target_type 'member' matching user_id).
// If the user is no longer a member of the workspace, or the agent is private and the user
// is neither agent owner nor workspace owner/admin, access is denied.
func UserCanAccessAgent(ctx context.Context, q *db.Queries, userID pgtype.UUID, agent db.Agent) (bool, error) {
	if !userID.Valid || q == nil {
		return false, nil
	}
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: agent.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	// Workspace owner and admin have access to all agents in the workspace.
	if member.Role == "owner" || member.Role == "admin" {
		return true, nil
	}
	// Agent owner has access to their own agent.
	if agent.OwnerID.Valid && agent.OwnerID == userID {
		return true, nil
	}
	// Public_to agents permit members listed in invocation targets.
	if agent.PermissionMode == "public_to" {
		targets, err := q.ListAgentInvocationTargets(ctx, agent.ID)
		if err != nil {
			return false, err
		}
		userIDStr := util.UUIDToString(userID)
		for _, t := range targets {
			switch t.TargetType {
			case "workspace":
				return true, nil
			case "member":
				if util.UUIDToString(t.TargetID) == userIDStr {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// CanAgentUseRuntime determines whether an agent is authorized to use a given runtime.
// For public runtimes:
//   - Usable by any agent in the same workspace.
// For private runtimes:
//   - An ownerless private runtime is never usable.
//   - The runtime owner MUST have access to the agent (UserCanAccessAgent). If the runtime
//     owner has lost access to the agent, the runtime becomes unavailable.
//   - If the agent owner matches the runtime owner, execution is permitted.
//   - If the agent owner does not match the runtime owner, execution is permitted only if
//     the runtime is an authorized candidate pool member (in agent.RuntimeCandidateIds)
//     and the agent allows member candidates (agent.AllowMemberCandidates).
func CanAgentUseRuntime(ctx context.Context, q *db.Queries, rt db.AgentRuntime, agent db.Agent) (bool, error) {
	if rt.WorkspaceID != agent.WorkspaceID {
		return false, nil
	}
	if rt.Visibility == "public" {
		return true, nil
	}
	// Private runtime
	if !rt.OwnerID.Valid {
		return false, nil
	}

	// Security requirement: If the Runtime Owner has no access to the agent,
	// the runtime becomes unavailable for this agent.
	if q != nil {
		hasAccess, err := UserCanAccessAgent(ctx, q, rt.OwnerID, agent)
		if err != nil {
			return false, err
		}
		if !hasAccess {
			return false, nil
		}
	}

	// If agent owner matches runtime owner, access is granted.
	if agent.OwnerID.Valid && agent.OwnerID == rt.OwnerID {
		return true, nil
	}

	// Member candidate pool contribution:
	// Allowed if agent permits member candidates and the runtime is in candidate pool.
	if agent.AllowMemberCandidates {
		for _, candID := range agent.RuntimeCandidateIds {
			if candID == rt.ID {
				return true, nil
			}
		}
	}

	return false, nil
}
