-- name: ListSprints :many
SELECT * FROM sprint
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY start_date DESC, created_at DESC;

-- name: GetSprintInWorkspace :one
SELECT * FROM sprint
WHERE id = $1 AND workspace_id = $2;

-- name: GetActiveSprintInWorkspace :one
SELECT * FROM sprint
WHERE workspace_id = $1 AND status = 'active'
LIMIT 1;

-- name: CreateSprint :one
INSERT INTO sprint (
    workspace_id, project_id, name, goal, status,
    start_date, end_date, started_at, completed_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9
) RETURNING *;

-- name: UpdateSprint :one
UPDATE sprint SET
    name = COALESCE(sqlc.narg('name'), name),
    goal = CASE WHEN sqlc.arg('goal_set')::boolean THEN sqlc.narg('goal') ELSE goal END,
    project_id = CASE WHEN sqlc.arg('project_id_set')::boolean THEN sqlc.narg('project_id') ELSE project_id END,
    status = COALESCE(sqlc.narg('status'), status),
    start_date = COALESCE(sqlc.narg('start_date'), start_date),
    end_date = COALESCE(sqlc.narg('end_date'), end_date),
    started_at = CASE WHEN sqlc.arg('started_at_set')::boolean THEN sqlc.narg('started_at') ELSE started_at END,
    completed_at = CASE WHEN sqlc.arg('completed_at_set')::boolean THEN sqlc.narg('completed_at') ELSE completed_at END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: StartSprint :one
UPDATE sprint SET
    status = 'active',
    started_at = COALESCE(started_at, now()),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CompleteSprint :one
UPDATE sprint SET
    status = 'completed',
    completed_at = COALESCE(completed_at, now()),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteSprint :exec
DELETE FROM sprint WHERE id = $1 AND workspace_id = $2;

-- name: MoveIncompleteSprintIssuesToBacklog :exec
UPDATE issue SET
    sprint_id = NULL,
    updated_at = now()
WHERE sprint_id = $1
  AND workspace_id = $2
  AND status != ALL(sqlc.arg('terminal_status_keys')::text[]);

-- name: MoveIncompleteSprintIssuesToSprint :exec
UPDATE issue SET
    sprint_id = sqlc.arg('target_sprint_id')::uuid,
    updated_at = now()
WHERE sprint_id = $1
  AND workspace_id = $2
  AND status != ALL(sqlc.arg('terminal_status_keys')::text[]);

-- name: GetSprintIssueStats :many
SELECT sprint_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND sprint_id = ANY(sqlc.arg('sprint_ids')::uuid[])
GROUP BY sprint_id;

-- name: GetSprintBurndown :many
SELECT
    d::date AS date,
    count(i.id)::bigint AS total_issues,
    count(i.id) FILTER (WHERE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]) AND i.updated_at::date <= d::date)::bigint AS completed_issues,
    (count(i.id) - count(i.id) FILTER (WHERE i.status = ANY(sqlc.arg('terminal_status_keys')::text[]) AND i.updated_at::date <= d::date))::bigint AS remaining_issues
FROM sprint s
CROSS JOIN LATERAL generate_series(s.start_date::timestamp, s.end_date::timestamp, '1 day'::interval) AS d
LEFT JOIN issue i ON i.sprint_id = s.id AND i.workspace_id = s.workspace_id AND i.created_at::date <= d::date
WHERE s.id = $1 AND s.workspace_id = $2
GROUP BY d::date
ORDER BY d::date ASC;
