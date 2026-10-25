package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type SystemVitalsResponse struct {
	ServerVersion           string `json:"server_version"`
	TotalUsers              int64  `json:"total_users"`
	TotalWorkspaces         int64  `json:"total_workspaces"`
	SignupPolicy            string `json:"signup_policy"`
	InvitationAuthority     string `json:"invitation_authority"`
	WorkspaceCreationPolicy string `json:"workspace_creation_policy"`
	EnforceWorktree         bool   `json:"enforce_worktree"`
}

type UpdateSystemSettingRequest struct {
	Value json.RawMessage `json:"value"`
}

type UpdateUserStatusRequest struct {
	SystemRole       *string `json:"system_role,omitempty"`
	IsSuspended      *bool   `json:"is_suspended,omitempty"`
	SuspensionReason *string `json:"suspension_reason,omitempty"`
}

type CreateInstanceInvitationRequest struct {
	Email                string  `json:"email"`
	SystemRole           string  `json:"system_role"`
	DefaultWorkspaceID   *string `json:"default_workspace_id,omitempty"`
	DefaultWorkspaceRole *string `json:"default_workspace_role,omitempty"`
	ExpiresInDays        *int    `json:"expires_in_days,omitempty"`
}

type CreateInstanceInvitationResponse struct {
	Invitation db.InstanceInvitation `json:"invitation"`
	Token      string                `json:"token"`
	InviteURL  string                `json:"invite_url"`
}

type TransferWorkspaceOwnershipRequest struct {
	NewOwnerUserID string `json:"new_owner_user_id"`
}

// Helpers for reading dynamic policies
func (h *Handler) getWorkspaceCreationPolicy(ctx context.Context) string {
	setting, err := h.Queries.GetSystemSetting(ctx, "workspace_creation_policy")
	if err == nil && len(setting.Value) > 0 {
		var parsed struct {
			Policy string `json:"policy"`
		}
		if json.Unmarshal(setting.Value, &parsed) == nil && parsed.Policy != "" {
			return parsed.Policy
		}
	}
	if h.cfg.DisableWorkspaceCreation {
		return "disabled"
	}
	return "admins_only"
}

func (h *Handler) getSignupPolicy(ctx context.Context) (string, []string) {
	setting, err := h.Queries.GetSystemSetting(ctx, "signup_policy")
	if err == nil && len(setting.Value) > 0 {
		var parsed struct {
			Policy         string   `json:"policy"`
			AllowedDomains []string `json:"allowed_domains"`
		}
		if json.Unmarshal(setting.Value, &parsed) == nil && parsed.Policy != "" {
			return parsed.Policy, parsed.AllowedDomains
		}
	}
	if !h.cfg.AllowSignup {
		return "invite_only", h.cfg.AllowedEmailDomains
	}
	if len(h.cfg.AllowedEmailDomains) > 0 {
		return "domain_allowlist", h.cfg.AllowedEmailDomains
	}
	return "invite_only", nil
}

func (h *Handler) getInvitationAuthority(ctx context.Context) string {
	setting, err := h.Queries.GetSystemSetting(ctx, "invitation_authority")
	if err == nil && len(setting.Value) > 0 {
		var parsed struct {
			WhoCanInvite string `json:"who_can_invite"`
		}
		if json.Unmarshal(setting.Value, &parsed) == nil && parsed.WhoCanInvite != "" {
			return parsed.WhoCanInvite
		}
	}
	return "admins_only"
}

func (h *Handler) recordAuditLog(ctx context.Context, r *http.Request, actorID string, action, targetType, targetID string, changes any) {
	changesBytes, _ := json.Marshal(changes)
	actorUUID, err := util.ParseUUID(actorID)
	if err != nil {
		actorUUID = pgtype.UUID{}
	}
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	_, _ = h.Queries.CreateSystemAuditLog(ctx, db.CreateSystemAuditLogParams{
		ActorID:    actorUUID,
		ActorIp:    strToText(ip),
		Action:     action,
		TargetType: targetType,
		TargetID:   targetID,
		Changes:    changesBytes,
	})
}

func (h *Handler) GetSystemVitals(w http.ResponseWriter, r *http.Request) {
	totalUsers, _ := h.Queries.CountTotalUsers(r.Context())
	workspaces, _ := h.Queries.ListAllWorkspacesForAdmin(r.Context())
	signupPolicy, _ := h.getSignupPolicy(r.Context())
	creationPolicy := h.getWorkspaceCreationPolicy(r.Context())
	invitationAuthority := h.getInvitationAuthority(r.Context())

	resp := SystemVitalsResponse{
		ServerVersion:           h.cfg.ServerVersion,
		TotalUsers:              totalUsers,
		TotalWorkspaces:         int64(len(workspaces)),
		SignupPolicy:            signupPolicy,
		InvitationAuthority:     invitationAuthority,
		WorkspaceCreationPolicy: creationPolicy,
		EnforceWorktree:         true,
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ListSystemSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.Queries.ListSystemSettings(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list settings")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (h *Handler) UpdateSystemSetting(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	key := chi.URLParam(r, "key")
	if key == "" {
		writeError(w, http.StatusBadRequest, "setting key is required")
		return
	}

	var req UpdateSystemSettingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	actorUUID, _ := util.ParseUUID(actorID)
	updated, err := h.Queries.UpsertSystemSetting(r.Context(), db.UpsertSystemSettingParams{
		Key:         key,
		Value:       req.Value,
		Description: pgtype.Text{},
		UpdatedBy:   actorUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update setting")
		return
	}

	h.recordAuditLog(r.Context(), r, actorID, "setting.update", "system_setting", key, string(req.Value))
	writeJSON(w, http.StatusOK, updated)
}

func (h *Handler) ListUsersForAdmin(w http.ResponseWriter, r *http.Request) {
	users, err := h.Queries.ListAllUsersForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *Handler) UpdateUserStatus(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	targetUserIDStr := chi.URLParam(r, "id")
	targetUserUUID, err := util.ParseUUID(targetUserIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	var req UpdateUserStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	targetUser, err := h.Queries.GetUser(r.Context(), targetUserUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	if req.SystemRole != nil {
		role := *req.SystemRole
		if role != "standard" && role != "instance_admin" && role != "instance_owner" {
			writeError(w, http.StatusBadRequest, "invalid system role")
			return
		}
		// Prevent demoting the last instance owner
		if targetUser.SystemRole == "instance_owner" && role != "instance_owner" {
			ownerCount, _ := h.Queries.CountInstanceOwners(r.Context())
			if ownerCount <= 1 {
				writeError(w, http.StatusBadRequest, "cannot demote the sole instance owner")
				return
			}
		}
		updated, err := h.Queries.UpdateUserSystemRole(r.Context(), db.UpdateUserSystemRoleParams{
			ID:         targetUserUUID,
			SystemRole: role,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update user role")
			return
		}
		targetUser = updated
		h.recordAuditLog(r.Context(), r, actorID, "user.role_change", "user", targetUserIDStr, map[string]string{
			"new_role": role,
		})
	}

	if req.IsSuspended != nil {
		suspended := *req.IsSuspended
		reason := ""
		if req.SuspensionReason != nil {
			reason = *req.SuspensionReason
		}
		// Prevent suspending the caller themselves or the instance owner
		if targetUserIDStr == actorID && suspended {
			writeError(w, http.StatusBadRequest, "cannot suspend your own account")
			return
		}
		if targetUser.SystemRole == "instance_owner" && suspended {
			writeError(w, http.StatusBadRequest, "cannot suspend an instance owner")
			return
		}
		updated, err := h.Queries.SetUserSuspension(r.Context(), db.SetUserSuspensionParams{
			ID:               targetUserUUID,
			IsSuspended:      suspended,
			SuspensionReason: strToText(reason),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update suspension")
			return
		}
		targetUser = updated
		h.recordAuditLog(r.Context(), r, actorID, "user.suspension_change", "user", targetUserIDStr, map[string]any{
			"is_suspended": suspended,
			"reason":       reason,
		})
	}

	writeJSON(w, http.StatusOK, targetUser)
}

func (h *Handler) ListWorkspacesForAdmin(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.Queries.ListAllWorkspacesForAdmin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list workspaces")
		return
	}
	writeJSON(w, http.StatusOK, workspaces)
}

func (h *Handler) TransferWorkspaceOwnership(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceIDStr := chi.URLParam(r, "id")
	workspaceUUID, err := util.ParseUUID(workspaceIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}

	var req TransferWorkspaceOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	newOwnerUUID, err := util.ParseUUID(req.NewOwnerUserID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid new owner user id")
		return
	}

	// Verify target user exists
	if _, err := h.Queries.GetUser(r.Context(), newOwnerUUID); err != nil {
		writeError(w, http.StatusNotFound, "target user not found")
		return
	}

	// Check if new owner is already a member
	existingMember, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      newOwnerUUID,
		WorkspaceID: workspaceUUID,
	})
	if err == nil {
		// Update existing membership to owner
		_, err = h.Queries.UpdateMemberRole(r.Context(), db.UpdateMemberRoleParams{
			ID:   existingMember.ID,
			Role: "owner",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to elevate member to owner")
			return
		}
	} else {
		// Insert member as owner
		_, err = h.Queries.CreateMember(r.Context(), db.CreateMemberParams{
			WorkspaceID: workspaceUUID,
			UserID:      newOwnerUUID,
			Role:        "owner",
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to assign workspace owner")
			return
		}
	}

	h.recordAuditLog(r.Context(), r, actorID, "workspace.transfer_owner", "workspace", workspaceIDStr, map[string]string{
		"new_owner": req.NewOwnerUserID,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "transferred"})
}

func (h *Handler) ListInstanceInvitations(w http.ResponseWriter, r *http.Request) {
	invitations, err := h.Queries.ListInstanceInvitations(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}
	writeJSON(w, http.StatusOK, invitations)
}

func (h *Handler) CreateInstanceInvitation(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req CreateInstanceInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "valid email address is required")
		return
	}

	systemRole := "standard"
	if req.SystemRole == "instance_admin" {
		systemRole = "instance_admin"
	}

	var defaultWorkspaceUUID pgtype.UUID
	if req.DefaultWorkspaceID != nil && *req.DefaultWorkspaceID != "" {
		parsed, err := util.ParseUUID(*req.DefaultWorkspaceID)
		if err == nil {
			defaultWorkspaceUUID = parsed
		}
	}

	defaultRole := ptrToText(req.DefaultWorkspaceRole)
	if defaultRole.String == "" {
		defaultRole = strToText("member")
	}

	// Generate 32-byte cryptographic token
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate invite token")
		return
	}
	rawToken := "mci_" + hex.EncodeToString(tokenBytes)
	tokenHash := auth.HashToken(rawToken)

	expiresDays := 7
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		expiresDays = *req.ExpiresInDays
	}
	expiresAt := time.Now().Add(time.Duration(expiresDays) * 24 * time.Hour)

	actorUUID, _ := util.ParseUUID(actorID)
	invitation, err := h.Queries.CreateInstanceInvitation(r.Context(), db.CreateInstanceInvitationParams{
		Email:                email,
		TokenHash:            tokenHash,
		SystemRole:           systemRole,
		DefaultWorkspaceID:   defaultWorkspaceUUID,
		DefaultWorkspaceRole: defaultRole,
		InvitedBy:            actorUUID,
		ExpiresAt:            pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}

	appURL := resolveFrontendAppURL()
	if appURL == "" {
		appURL = "http://localhost:3000"
	}
	inviteURL := fmt.Sprintf("%s/join?token=%s", appURL, rawToken)

	h.recordAuditLog(r.Context(), r, actorID, "invite.create", "instance_invitation", uuidToString(invitation.ID), map[string]string{
		"email":       email,
		"system_role": systemRole,
	})

	writeJSON(w, http.StatusCreated, CreateInstanceInvitationResponse{
		Invitation: invitation,
		Token:      rawToken,
		InviteURL:  inviteURL,
	})
}

func (h *Handler) RevokeInstanceInvitation(w http.ResponseWriter, r *http.Request) {
	actorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	invitationIDStr := chi.URLParam(r, "id")
	invitationUUID, err := util.ParseUUID(invitationIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid invitation id")
		return
	}

	revoked, err := h.Queries.RevokeInstanceInvitation(r.Context(), invitationUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke invitation")
		return
	}

	h.recordAuditLog(r.Context(), r, actorID, "invite.revoke", "instance_invitation", invitationIDStr, nil)
	writeJSON(w, http.StatusOK, revoked)
}

func (h *Handler) ListSystemAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := int32(50)
	offset := int32(0)

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = int32(parsed)
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = int32(parsed)
		}
	}

	logs, err := h.Queries.ListSystemAuditLogs(r.Context(), db.ListSystemAuditLogsParams{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list audit logs")
		return
	}
	writeJSON(w, http.StatusOK, logs)
}
