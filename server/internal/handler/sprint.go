package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type SprintResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ProjectID   *string `json:"project_id"`
	Name        string  `json:"name"`
	Goal        *string `json:"goal"`
	Status      string  `json:"status"`
	StartDate   string  `json:"start_date"`
	EndDate     string  `json:"end_date"`
	StartedAt   *string `json:"started_at"`
	CompletedAt *string `json:"completed_at"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
	TotalCount  int64   `json:"total_count"`
	DoneCount   int64   `json:"done_count"`
}

func sprintToResponse(s db.Sprint) SprintResponse {
	return SprintResponse{
		ID:          uuidToString(s.ID),
		WorkspaceID: uuidToString(s.WorkspaceID),
		ProjectID:   uuidToPtr(s.ProjectID),
		Name:        s.Name,
		Goal:        textToPtr(s.Goal),
		Status:      s.Status,
		StartDate:   *dateToPtr(s.StartDate),
		EndDate:     *dateToPtr(s.EndDate),
		StartedAt:   timestampToPtr(s.StartedAt),
		CompletedAt: timestampToPtr(s.CompletedAt),
		CreatedAt:   timestampToString(s.CreatedAt),
		UpdatedAt:   timestampToString(s.UpdatedAt),
	}
}

func (h *Handler) loadSprintIssueStats(ctx context.Context, workspaceID, sprintID pgtype.UUID) (int64, int64) {
	terminalStatusKeys := h.projectTerminalIssueStatusKeys(ctx, workspaceID)
	stats, err := h.Queries.GetSprintIssueStats(ctx, db.GetSprintIssueStatsParams{
		WorkspaceID:        workspaceID,
		SprintIds:          []pgtype.UUID{sprintID},
		TerminalStatusKeys: terminalStatusKeys,
	})
	if err != nil || len(stats) == 0 {
		return 0, 0
	}
	return stats[0].TotalCount, stats[0].DoneCount
}

var validSprintStatuses = []string{"planned", "active", "completed", "cancelled"}

func validSprintStatus(status string) bool {
	for _, s := range validSprintStatuses {
		if s == status {
			return true
		}
	}
	return false
}

func validateSprintEnum(w http.ResponseWriter, field, value string, allowed []string) bool {
	for _, a := range allowed {
		if value == a {
			return true
		}
	}
	writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid %s %q; valid values: %s", field, value, strings.Join(allowed, ", ")))
	return false
}

func (h *Handler) ListSprints(w http.ResponseWriter, r *http.Request) {
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

	sprints, err := h.Queries.ListSprints(r.Context(), db.ListSprintsParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectFilter,
		Status:      statusFilter,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sprints")
		return
	}

	statsMap := make(map[string]db.GetSprintIssueStatsRow)
	if len(sprints) > 0 {
		sprintIDs := make([]pgtype.UUID, len(sprints))
		for i, s := range sprints {
			sprintIDs[i] = s.ID
		}
		terminalStatusKeys := h.projectTerminalIssueStatusKeys(r.Context(), wsUUID)
		stats, statsErr := h.Queries.GetSprintIssueStats(r.Context(), db.GetSprintIssueStatsParams{
			WorkspaceID:        wsUUID,
			SprintIds:          sprintIDs,
			TerminalStatusKeys: terminalStatusKeys,
		})
		if statsErr == nil {
			for _, s := range stats {
				statsMap[uuidToString(s.SprintID)] = s
			}
		}
	}

	resp := make([]SprintResponse, len(sprints))
	for i, s := range sprints {
		resp[i] = sprintToResponse(s)
		if st, ok := statsMap[resp[i].ID]; ok {
			resp[i].TotalCount = st.TotalCount
			resp[i].DoneCount = st.DoneCount
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sprints": resp, "total": len(resp)})
}

func (h *Handler) GetActiveSprint(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	if _, ok := requireUserID(w, r); !ok {
		return
	}

	sprint, err := h.Queries.GetActiveSprintInWorkspace(r.Context(), wsUUID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{"sprint": nil})
			return
		}
		slog.Error("get active sprint failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get active sprint")
		return
	}

	resp := sprintToResponse(sprint)
	resp.TotalCount, resp.DoneCount = h.loadSprintIssueStats(r.Context(), wsUUID, sprint.ID)
	writeJSON(w, http.StatusOK, map[string]any{"sprint": resp})
}

type CreateSprintRequest struct {
	Name      string  `json:"name"`
	Goal      *string `json:"goal"`
	ProjectID *string `json:"project_id"`
	StartDate string  `json:"start_date"`
	EndDate   string  `json:"end_date"`
}

func (h *Handler) CreateSprint(w http.ResponseWriter, r *http.Request) {
	var req CreateSprintRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	if req.StartDate == "" || req.EndDate == "" {
		writeError(w, http.StatusBadRequest, "start_date and end_date are required")
		return
	}

	startDate, err := util.ParseCalendarDate(req.StartDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
		return
	}

	endDate, err := util.ParseCalendarDate(req.EndDate)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid end_date, expected YYYY-MM-DD")
		return
	}

	if startDate.Time.After(endDate.Time) {
		writeError(w, http.StatusBadRequest, "start_date must be before or equal to end_date")
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

	var goal pgtype.Text
	if req.Goal != nil {
		goal = pgtype.Text{String: *req.Goal, Valid: true}
	}

	sprint, err := h.Queries.CreateSprint(r.Context(), db.CreateSprintParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectID,
		Name:        req.Name,
		Goal:        goal,
		Status:      "planned",
		StartDate:   startDate,
		EndDate:     endDate,
	})
	if err != nil {
		slog.Error("create sprint failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create sprint")
		return
	}

	resp := sprintToResponse(sprint)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) GetSprint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "sprint id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	sprint, err := h.Queries.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "sprint not found")
		return
	}

	resp := sprintToResponse(sprint)
	resp.TotalCount, resp.DoneCount = h.loadSprintIssueStats(r.Context(), wsUUID, sprint.ID)
	writeJSON(w, http.StatusOK, resp)
}

type UpdateSprintRequest struct {
	Name      *string `json:"name"`
	Goal      *string `json:"goal"`
	ProjectID *string `json:"project_id"`
	Status    *string `json:"status"`
	StartDate *string `json:"start_date"`
	EndDate   *string `json:"end_date"`
}

func (h *Handler) UpdateSprint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "sprint id")
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

	sprint, err := h.Queries.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "sprint not found")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var req UpdateSprintRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	var rawFields map[string]json.RawMessage
	_ = json.Unmarshal(bodyBytes, &rawFields)

	params := db.UpdateSprintParams{
		ID:          sprint.ID,
		WorkspaceID: sprint.WorkspaceID,
	}

	if req.Name != nil {
		trimmed := strings.TrimSpace(*req.Name)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "name cannot be empty")
			return
		}
		params.Name = pgtype.Text{String: trimmed, Valid: true}
	}

	if _, ok := rawFields["goal"]; ok {
		params.GoalSet = true
		if req.Goal != nil {
			params.Goal = pgtype.Text{String: *req.Goal, Valid: true}
		}
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

	if req.Status != nil {
		if !validateSprintEnum(w, "status", *req.Status, validSprintStatuses) {
			return
		}
		if *req.Status == "active" && sprint.Status != "active" {
			active, err := h.Queries.GetActiveSprintInWorkspace(r.Context(), wsUUID)
			if err == nil && active.ID != sprint.ID {
				writeError(w, http.StatusConflict, fmt.Sprintf("workspace already has an active sprint (%s)", active.Name))
				return
			}
			params.StartedAtSet = true
			params.StartedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		} else if *req.Status == "completed" && sprint.Status != "completed" {
			params.CompletedAtSet = true
			params.CompletedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}

	effectiveStartDate := sprint.StartDate
	if req.StartDate != nil && *req.StartDate != "" {
		d, err := util.ParseCalendarDate(*req.StartDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid start_date, expected YYYY-MM-DD")
			return
		}
		params.StartDate = d
		effectiveStartDate = d
	}

	effectiveEndDate := sprint.EndDate
	if req.EndDate != nil && *req.EndDate != "" {
		d, err := util.ParseCalendarDate(*req.EndDate)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid end_date, expected YYYY-MM-DD")
			return
		}
		params.EndDate = d
		effectiveEndDate = d
	}

	if effectiveStartDate.Time.After(effectiveEndDate.Time) {
		writeError(w, http.StatusBadRequest, "start_date must be before or equal to end_date")
		return
	}

	updatedSprint, err := h.Queries.UpdateSprint(r.Context(), params)
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "workspace already has an active sprint")
			return
		}
		slog.Error("update sprint failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update sprint")
		return
	}

	resp := sprintToResponse(updatedSprint)
	resp.TotalCount, resp.DoneCount = h.loadSprintIssueStats(r.Context(), wsUUID, updatedSprint.ID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) DeleteSprint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "sprint id")
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

	if _, err := h.Queries.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusNotFound, "sprint not found")
		return
	}

	if err := h.Queries.DeleteSprint(r.Context(), db.DeleteSprintParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	}); err != nil {
		slog.Error("delete sprint failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete sprint")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) StartSprint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "sprint id")
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

	sprint, err := h.Queries.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "sprint not found")
		return
	}

	if sprint.Status == "active" {
		writeError(w, http.StatusBadRequest, "sprint is already active")
		return
	}
	if sprint.Status == "completed" || sprint.Status == "cancelled" {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot start %s sprint", sprint.Status))
		return
	}

	active, err := h.Queries.GetActiveSprintInWorkspace(r.Context(), wsUUID)
	if err == nil && active.ID != sprint.ID {
		writeError(w, http.StatusConflict, fmt.Sprintf("workspace already has an active sprint (%s)", active.Name))
		return
	}

	startedSprint, err := h.Queries.StartSprint(r.Context(), db.StartSprintParams{
		ID:          sprint.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "workspace already has an active sprint")
			return
		}
		slog.Error("start sprint failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to start sprint")
		return
	}

	resp := sprintToResponse(startedSprint)
	resp.TotalCount, resp.DoneCount = h.loadSprintIssueStats(r.Context(), wsUUID, startedSprint.ID)
	writeJSON(w, http.StatusOK, resp)
}

type CompleteSprintRequest struct {
	TargetSprintID *string `json:"target_sprint_id"`
}

func (h *Handler) CompleteSprint(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "sprint id")
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

	sprint, err := h.Queries.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "sprint not found")
		return
	}

	if sprint.Status == "completed" {
		writeError(w, http.StatusBadRequest, "sprint is already completed")
		return
	}

	var req CompleteSprintRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	terminalStatusKeys := h.projectTerminalIssueStatusKeys(r.Context(), wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("complete sprint begin tx failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete sprint")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	if req.TargetSprintID != nil && *req.TargetSprintID != "" {
		targetUUID, ok := parseUUIDOrBadRequest(w, *req.TargetSprintID, "target_sprint_id")
		if !ok {
			return
		}
		if targetUUID == sprint.ID {
			writeError(w, http.StatusBadRequest, "cannot move issues to the sprint being completed")
			return
		}
		targetSprint, err := qtx.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
			ID:          targetUUID,
			WorkspaceID: wsUUID,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "target sprint not found in this workspace")
			return
		}
		if targetSprint.Status == "completed" || targetSprint.Status == "cancelled" {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot move issues to a %s sprint", targetSprint.Status))
			return
		}
		if err := qtx.MoveIncompleteSprintIssuesToSprint(r.Context(), db.MoveIncompleteSprintIssuesToSprintParams{
			SprintID:           sprint.ID,
			WorkspaceID:        wsUUID,
			TargetSprintID:     targetUUID,
			TerminalStatusKeys: terminalStatusKeys,
		}); err != nil {
			slog.Error("move incomplete issues to target sprint failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to move incomplete issues to target sprint")
			return
		}
	} else {
		if err := qtx.MoveIncompleteSprintIssuesToBacklog(r.Context(), db.MoveIncompleteSprintIssuesToBacklogParams{
			SprintID:           sprint.ID,
			WorkspaceID:        wsUUID,
			TerminalStatusKeys: terminalStatusKeys,
		}); err != nil {
			slog.Error("move incomplete issues to backlog failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to move incomplete issues to backlog")
			return
		}
	}

	completedSprint, err := qtx.CompleteSprint(r.Context(), db.CompleteSprintParams{
		ID:          sprint.ID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		slog.Error("complete sprint failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete sprint")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("complete sprint commit failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to complete sprint")
		return
	}

	resp := sprintToResponse(completedSprint)
	resp.TotalCount, resp.DoneCount = h.loadSprintIssueStats(r.Context(), wsUUID, completedSprint.ID)
	writeJSON(w, http.StatusOK, resp)
}

type SprintBurndownPoint struct {
	Date      string  `json:"date"`
	Remaining int64   `json:"remaining"`
	Ideal     float64 `json:"ideal"`
	Completed int64   `json:"completed"`
}

func (h *Handler) GetSprintBurndown(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	workspaceID := h.resolveWorkspaceID(r)
	idUUID, ok := parseUUIDOrBadRequest(w, id, "sprint id")
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}

	sprint, err := h.Queries.GetSprintInWorkspace(r.Context(), db.GetSprintInWorkspaceParams{
		ID:          idUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "sprint not found")
		return
	}

	terminalStatusKeys := h.projectTerminalIssueStatusKeys(r.Context(), wsUUID)
	rows, err := h.Queries.GetSprintBurndown(r.Context(), db.GetSprintBurndownParams{
		ID:                 sprint.ID,
		WorkspaceID:        wsUUID,
		TerminalStatusKeys: terminalStatusKeys,
	})
	if err != nil {
		slog.Error("get sprint burndown failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to get sprint burndown")
		return
	}

	points := make([]SprintBurndownPoint, 0, len(rows))
	if len(rows) == 0 {
		writeJSON(w, http.StatusOK, points)
		return
	}

	var maxTotal int64
	for _, row := range rows {
		if row.TotalIssues > maxTotal {
			maxTotal = row.TotalIssues
		}
	}
	initialTotal := rows[0].TotalIssues
	if initialTotal == 0 && maxTotal > 0 {
		initialTotal = maxTotal
	}

	n := len(rows) - 1
	for i, row := range rows {
		var ideal float64
		if n > 0 {
			ideal = float64(initialTotal) * (1.0 - float64(i)/float64(n))
			if ideal < 0 {
				ideal = 0
			}
		}
		points = append(points, SprintBurndownPoint{
			Date:      row.Date.Time.Format("2006-01-02"),
			Remaining: row.RemainingIssues,
			Ideal:     ideal,
			Completed: row.CompletedIssues,
		})
	}

	writeJSON(w, http.StatusOK, points)
}
