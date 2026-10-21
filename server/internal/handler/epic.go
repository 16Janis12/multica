package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type EpicResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ProjectID   *string `json:"project_id"`
	Identifier  string  `json:"identifier"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Color       string  `json:"color"`
	Status      string  `json:"status"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	TotalCount  int64   `json:"total_count"`
	DoneCount   int64   `json:"done_count"`
}

func epicToResponse(e db.Epic) EpicResponse {
	return EpicResponse{
		ID:          uuidToString(e.ID),
		WorkspaceID: uuidToString(e.WorkspaceID),
		ProjectID:   uuidToPtr(e.ProjectID),
		Identifier:  e.Identifier,
		Title:       e.Title,
		Description: textToPtr(e.Description),
		Color:       e.Color,
		Status:      e.Status,
		LeadType:    textToPtr(e.LeadType),
		LeadID:      uuidToPtr(e.LeadID),
		StartDate:   dateToPtr(e.StartDate),
		DueDate:     dateToPtr(e.DueDate),
		CreatedAt:   timestampToString(e.CreatedAt),
		UpdatedAt:   timestampToString(e.UpdatedAt),
	}
}

func (h *Handler) loadEpicIssueStats(ctx context.Context, workspaceID, epicID pgtype.UUID) (int64, int64) {
	terminalStatusKeys := h.projectTerminalIssueStatusKeys(ctx, workspaceID)
	stats, err := h.Queries.GetEpicIssueStats(ctx, db.GetEpicIssueStatsParams{
		WorkspaceID:        workspaceID,
		EpicIds:            []pgtype.UUID{epicID},
		TerminalStatusKeys: terminalStatusKeys,
	})
	if err != nil || len(stats) == 0 {
		return 0, 0
	}
	return stats[0].TotalCount, stats[0].DoneCount
}

var validEpicStatuses = []string{"planned", "in_progress", "completed", "cancelled"}

func validEpicStatus(status string) bool {
	for _, s := range validEpicStatuses {
		if s == status {
			return true
		}
	}
	return false
}

func validateEpicEnum(w http.ResponseWriter, field, value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", ")))
	return false
}

func (h *Handler) ListEpics(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	var projectFilter pgtype.UUID
	if p := r.URL.Query().Get("project_id"); p != "" {
		id, ok := parseUUIDOrBadRequest(w, p, "project_id")
		if !ok {
			return
		}
		projectFilter = id
	}

	var statusFilter pgtype.Text
	if s := r.URL.Query().Get("status"); s != "" {
		statusFilter = pgtype.Text{String: s, Valid: true}
	}

	epics, err := h.Queries.ListEpics(r.Context(), db.ListEpicsParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectFilter,
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list epics")
		return
	}

	statsMap := make(map[string]db.GetEpicIssueStatsRow)
	if len(epics) > 0 {
		epicIDs := make([]pgtype.UUID, len(epics))
		for i, e := range epics {
			epicIDs[i] = e.ID
		}
		terminalStatusKeys := h.projectTerminalIssueStatusKeys(r.Context(), wsUUID)
		stats, statsErr := h.Queries.GetEpicIssueStats(r.Context(), db.GetEpicIssueStatsParams{
			WorkspaceID:        wsUUID,
			EpicIds:            epicIDs,
			TerminalStatusKeys: terminalStatusKeys,
		})
		if statsErr == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.EpicID)] = s
			}
		}
	}

	resp := make([]EpicResponse, len(epics))
	for i, e := range epics {
		resp[i] = epicToResponse(e)
		if s, ok := statsMap[resp[i].ID]; ok {
			resp[i].TotalCount = s.TotalCount
			resp[i].DoneCount = s.DoneCount
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"epics": resp, "total": len(resp)})
}

type CreateEpicRequest struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Status      *string `json:"status"`
	ProjectID   *string `json:"project_id"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
}

func (h *Handler) validateLeadPair(ctx context.Context, r *http.Request, wsUUID pgtype.UUID, leadType pgtype.Text, leadID pgtype.UUID) (int, string) {
	if !leadType.Valid && !leadID.Valid {
		return 0, ""
	}
	if leadType.Valid != leadID.Valid {
		return http.StatusBadRequest, "lead_type and lead_id must be provided together"
	}
	switch leadType.String {
	case "member":
		if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
			UserID:      leadID,
			WorkspaceID: wsUUID,
		}); err != nil {
			return http.StatusBadRequest, "lead_id does not refer to a member of this workspace"
		}
		return 0, ""
	case "agent":
		agent, err := h.Queries.GetAgentInWorkspace(ctx, db.GetAgentInWorkspaceParams{
			ID:          leadID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			return http.StatusBadRequest, "lead_id does not refer to an agent of this workspace"
		}
		if agent.ArchivedAt.Valid {
			return http.StatusBadRequest, "cannot set archived agent as lead"
		}
		return 0, ""
	default:
		return http.StatusBadRequest, "lead_type must be 'member' or 'agent'"
	}
}

func (h *Handler) CreateEpic(w http.ResponseWriter, r *http.Request) {
	var req CreateEpicRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	status := "planned"
	if req.Status != nil && *req.Status != "" {
		status = *req.Status
		if !validateEpicEnum(w, "status", status, validEpicStatuses) {
			return
		}
	}

	color := "#6366f1"
	if req.Color != nil && strings.TrimSpace(*req.Color) != "" {
		c, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		color = c
	}

	var projectID pgtype.UUID
	if req.ProjectID != nil && *req.ProjectID != "" {
		pID, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
			ID:          pID,
			WorkspaceID: wsUUID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "project not found in this workspace")
			return
		}
		projectID = pID
	}

	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil && *req.LeadType != "" {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil && *req.LeadID != "" {
		lID, ok := parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
		if !ok {
			return
		}
		leadID = lID
	}
	if status, msg := h.validateLeadPair(r.Context(), r, wsUUID, leadType, leadID); status != 0 {
		writeError(w, status, msg)
		return
	}

	var startDate pgtype.Date
	if req.StartDate != nil && *req.StartDate != "" {
		d, err := util.ParseCalendarDate(*req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
			return
		}
		startDate = d
	}

	var dueDate pgtype.Date
	if req.DueDate != nil && *req.DueDate != "" {
		d, err := util.ParseCalendarDate(*req.DueDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid due_date, expected YYYY-MM-DD")
			return
		}
		dueDate = d
	}

	if startDate.Valid && dueDate.Valid && startDate.Time.After(dueDate.Time) {
		writeError(w, http.StatusBadRequest, "start_date must be before or equal to due_date")
		return
	}

	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	nextNumber, err := h.Queries.GetNextEpicNumber(r.Context(), wsUUID)
	if err != nil {
		slog.Error("create epic: get next number", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to generate epic identifier")
		return
	}
	identifier := fmt.Sprintf("%s-E%d", prefix, nextNumber)

	var description pgtype.Text
	if req.Description != nil {
		description = pgtype.Text{String: *req.Description, Valid: true}
	}

	epic, err := h.Queries.CreateEpic(r.Context(), db.CreateEpicParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectID,
		Identifier:  identifier,
		Title:       req.Title,
		Description: description,
		Color:       color,
		Status:      status,
		LeadType:    leadType,
		LeadID:      leadID,
		StartDate:   startDate,
		DueDate:     dueDate,
	})
	if err != nil {
		slog.Error("create epic failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create epic")
		return
	}

	resp := epicToResponse(epic)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	var epic db.Epic
	var err error
	if idUUID, parseErr := util.ParseUUID(id); parseErr == nil {
		epic, err = h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
			ID:          idUUID,
			WorkspaceID: wsUUID,
		})
	} else {
		epic, err = h.Queries.GetEpicByIdentifier(r.Context(), db.GetEpicByIdentifierParams{
			WorkspaceID: wsUUID,
			Identifier:  strings.ToUpper(strings.TrimSpace(id)),
		})
	}
	if err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}

	resp := epicToResponse(epic)
	resp.TotalCount, resp.DoneCount = h.loadEpicIssueStats(r.Context(), wsUUID, epic.ID)
	writeJSON(w, http.StatusOK, resp)
}

type UpdateEpicRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Color       *string `json:"color"`
	Status      *string `json:"status"`
	ProjectID   *string `json:"project_id"`
	LeadType    *string `json:"lead_type"`
	LeadID      *string `json:"lead_id"`
	StartDate   *string `json:"start_date"`
	DueDate     *string `json:"due_date"`
}

func (h *Handler) UpdateEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "epic id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	epic, err := h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req UpdateEpicRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateEpicParams{
		ID:          epic.ID,
		WorkspaceID: epic.WorkspaceID,
	}

	if req.Title != nil {
		trimmed := strings.TrimSpace(*req.Title)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		params.Title = pgtype.Text{String: trimmed, Valid: true}
	}

	if _, ok := rawFields["description"]; ok {
		params.DescriptionSet = true
		if req.Description != nil {
			params.Description = pgtype.Text{String: *req.Description, Valid: true}
		}
	}

	if req.Color != nil {
		c, err := normalizeColor(*req.Color)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		params.Color = pgtype.Text{String: c, Valid: true}
	}

	if req.Status != nil {
		if !validateEpicEnum(w, "status", *req.Status, validEpicStatuses) {
			return
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}

	if _, ok := rawFields["project_id"]; ok {
		params.ProjectIDSet = true
		if req.ProjectID != nil && *req.ProjectID != "" {
			pID, ok := parseUUIDOrBadRequest(w, *req.ProjectID, "project_id")
			if !ok {
				return
			}
			if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
				ID:          pID,
				WorkspaceID: wsUUID,
			}); err != nil {
				writeError(w, http.StatusBadRequest, "project not found in this workspace")
				return
			}
			params.ProjectID = pID
		}
	}

	effectiveLeadType := epic.LeadType
	effectiveLeadID := epic.LeadID

	if _, ok := rawFields["lead_type"]; ok {
		params.LeadTypeSet = true
		if req.LeadType != nil && *req.LeadType != "" {
			params.LeadType = pgtype.Text{String: *req.LeadType, Valid: true}
			effectiveLeadType = params.LeadType
		} else {
			params.LeadType = pgtype.Text{Valid: false}
			effectiveLeadType = pgtype.Text{Valid: false}
		}
	}

	if _, ok := rawFields["lead_id"]; ok {
		params.LeadIDSet = true
		if req.LeadID != nil && *req.LeadID != "" {
			lID, ok := parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
			if !ok {
				return
			}
			params.LeadID = lID
			effectiveLeadID = lID
		} else {
			params.LeadID = pgtype.UUID{Valid: false}
			effectiveLeadID = pgtype.UUID{Valid: false}
		}
	}

	if status, msg := h.validateLeadPair(r.Context(), r, wsUUID, effectiveLeadType, effectiveLeadID); status != 0 {
		writeError(w, status, msg)
		return
	}

	effectiveStartDate := epic.StartDate
	if _, ok := rawFields["start_date"]; ok {
		params.StartDateSet = true
		if req.StartDate != nil && *req.StartDate != "" {
			d, err := util.ParseCalendarDate(*req.StartDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
				return
			}
			params.StartDate = d
			effectiveStartDate = d
		} else {
			params.StartDate = pgtype.Date{Valid: false}
			effectiveStartDate = pgtype.Date{Valid: false}
		}
	}

	effectiveDueDate := epic.DueDate
	if _, ok := rawFields["due_date"]; ok {
		params.DueDateSet = true
		if req.DueDate != nil && *req.DueDate != "" {
			d, err := util.ParseCalendarDate(*req.DueDate)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid due_date, expected YYYY-MM-DD")
				return
			}
			params.DueDate = d
			effectiveDueDate = d
		} else {
			params.DueDate = pgtype.Date{Valid: false}
			effectiveDueDate = pgtype.Date{Valid: false}
		}
	}

	if effectiveStartDate.Valid && effectiveDueDate.Valid && effectiveStartDate.Time.After(effectiveDueDate.Time) {
		writeError(w, http.StatusBadRequest, "start_date must be before or equal to due_date")
		return
	}

	updatedEpic, err := h.Queries.UpdateEpic(r.Context(), params)
	if err != nil {
		slog.Error("update epic failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update epic")
		return
	}

	resp := epicToResponse(updatedEpic)
	resp.TotalCount, resp.DoneCount = h.loadEpicIssueStats(r.Context(), wsUUID, updatedEpic.ID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteEpic(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "epic id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	if _, err := h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}

	if err := h.Queries.DeleteEpic(r.Context(), db.DeleteEpicParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("delete epic failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete epic")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type EpicStatsResponse struct {
	TotalCount      int64 `json:"total_count"`
	DoneCount       int64 `json:"done_count"`
	InProgressCount int64 `json:"in_progress_count"`
	TodoCount       int64 `json:"todo_count"`
}

func (h *Handler) GetEpicStats(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "epic id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, err := h.Queries.GetEpicInWorkspace(r.Context(), db.GetEpicInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "epic not found")
		return
	}

	terminalStatusKeys := h.projectTerminalIssueStatusKeys(r.Context(), wsUUID)
	stats, err := h.Queries.GetEpicDetailedStats(r.Context(), db.GetEpicDetailedStatsParams{
		EpicID:             idUUID,
		WorkspaceID:        wsUUID,
		TerminalStatusKeys: terminalStatusKeys,
	})
	if err != nil {
		slog.Error("get epic stats failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get epic stats")
		return
	}

	writeJSON(w, http.StatusOK, EpicStatsResponse{
		TotalCount:      stats.TotalCount,
		DoneCount:       stats.DoneCount,
		InProgressCount: stats.InProgressCount,
		TodoCount:       stats.TodoCount,
	})
}
