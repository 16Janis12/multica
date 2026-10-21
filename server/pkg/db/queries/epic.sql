-- name: ListEpics :many
SELECT * FROM epic
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC;

-- name: GetEpicInWorkspace :one
SELECT * FROM epic
WHERE id = $1 AND workspace_id = $2;

-- name: GetEpicByIdentifier :one
SELECT * FROM epic
WHERE workspace_id = $1 AND identifier = $2;

-- name: GetNextEpicNumber :one
SELECT COALESCE(MAX(
    NULLIF(regexp_replace(identifier, '^.*-E', ''), '')::integer
), 0) + 1 AS next_number
FROM epic
WHERE workspace_id = $1;

-- name: CreateEpic :one
INSERT INTO epic (
    workspace_id, project_id, identifier, title, description,
    color, status, lead_type, lead_id, start_date, due_date
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: UpdateEpic :one
UPDATE epic SET
    title = COALESCE(sqlc.narg('title'), title),
    description = CASE WHEN sqlc.arg('description_set')::boolean THEN sqlc.narg('description') ELSE description END,
    color = COALESCE(sqlc.narg('color'), color),
    status = COALESCE(sqlc.narg('status'), status),
    project_id = CASE WHEN sqlc.arg('project_id_set')::boolean THEN sqlc.narg('project_id') ELSE project_id END,
    lead_type = CASE WHEN sqlc.arg('lead_type_set')::boolean THEN sqlc.narg('lead_type') ELSE lead_type END,
    lead_id = CASE WHEN sqlc.arg('lead_id_set')::boolean THEN sqlc.narg('lead_id') ELSE lead_id END,
    start_date = CASE WHEN sqlc.arg('start_date_set')::boolean THEN sqlc.narg('start_date') ELSE start_date END,
    due_date = CASE WHEN sqlc.arg('due_date_set')::boolean THEN sqlc.narg('due_date') ELSE due_date END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteEpic :exec
DELETE FROM epic WHERE id = $1 AND workspace_id = $2;

-- name: GetEpicIssueStats :many
SELECT epic_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count
FROM issue
WHERE workspace_id = sqlc.arg('workspace_id')::uuid
  AND epic_id = ANY(sqlc.arg('epic_ids')::uuid[])
GROUP BY epic_id;

-- name: GetEpicDetailedStats :one
SELECT
    count(*)::bigint AS total_count,
    count(*) FILTER (WHERE status = ANY(sqlc.arg('terminal_status_keys')::text[]))::bigint AS done_count,
    count(*) FILTER (WHERE status NOT IN ('cancelled') AND status != ALL(sqlc.arg('terminal_status_keys')::text[]) AND status NOT IN ('backlog', 'todo'))::bigint AS in_progress_count,
    count(*) FILTER (WHERE status IN ('backlog', 'todo'))::bigint AS todo_count
FROM issue
WHERE epic_id = $1 AND workspace_id = $2;
