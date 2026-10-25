-- name: GetSystemSetting :one
SELECT * FROM system_settings
WHERE key = $1;

-- name: ListSystemSettings :many
SELECT * FROM system_settings
ORDER BY key ASC;

-- name: UpsertSystemSetting :one
INSERT INTO system_settings (key, value, description, updated_by, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    description = COALESCE(EXCLUDED.description, system_settings.description),
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: CreateInstanceInvitation :one
INSERT INTO instance_invitation (
    email,
    token_hash,
    system_role,
    default_workspace_id,
    default_workspace_role,
    invited_by,
    expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetInstanceInvitationByTokenHash :one
SELECT * FROM instance_invitation
WHERE token_hash = $1
  AND expires_at > now()
  AND accepted_at IS NULL
  AND revoked_at IS NULL;

-- name: GetActiveInstanceInvitationByEmail :one
SELECT * FROM instance_invitation
WHERE email = $1
  AND expires_at > now()
  AND accepted_at IS NULL
  AND revoked_at IS NULL
ORDER BY created_at DESC
LIMIT 1;

-- name: HasActiveInstanceInvitationForEmail :one
SELECT EXISTS (
    SELECT 1 FROM instance_invitation
    WHERE email = $1
      AND expires_at > now()
      AND accepted_at IS NULL
      AND revoked_at IS NULL
);

-- name: ListInstanceInvitations :many
SELECT ii.id, ii.email, ii.token_hash, ii.system_role, ii.default_workspace_id, ii.default_workspace_role,
       ii.invited_by, ii.expires_at, ii.accepted_at, ii.revoked_at, ii.created_at,
       u.name as inviter_name, u.email as inviter_email
FROM instance_invitation ii
JOIN "user" u ON u.id = ii.invited_by
ORDER BY ii.created_at DESC;

-- name: RevokeInstanceInvitation :one
UPDATE instance_invitation
SET revoked_at = now()
WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: MarkInstanceInvitationAccepted :one
UPDATE instance_invitation
SET accepted_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateSystemAuditLog :one
INSERT INTO system_audit_log (
    actor_id,
    actor_ip,
    action,
    target_type,
    target_id,
    changes
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListSystemAuditLogs :many
SELECT sal.id, sal.actor_id, sal.actor_ip, sal.action, sal.target_type, sal.target_id, sal.changes, sal.created_at,
       u.name as actor_name, u.email as actor_email
FROM system_audit_log sal
LEFT JOIN "user" u ON u.id = sal.actor_id
ORDER BY sal.created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountTotalUsers :one
SELECT count(*) FROM "user";

-- name: CountInstanceOwners :one
SELECT count(*) FROM "user"
WHERE system_role = 'instance_owner';

-- name: CreateUserWithRole :one
INSERT INTO "user" (name, email, avatar_url, system_role)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: UpdateUserSystemRole :one
UPDATE "user" SET
    system_role = $2,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: SetUserSuspension :one
UPDATE "user" SET
    is_suspended = $2,
    suspended_at = CASE WHEN $2::boolean THEN now() ELSE NULL END,
    suspension_reason = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListAllUsersForAdmin :many
SELECT id, name, email, avatar_url, system_role, is_suspended, suspended_at, suspension_reason, created_at, updated_at
FROM "user"
ORDER BY created_at DESC;

-- name: ListAllWorkspacesForAdmin :many
SELECT 
    w.id,
    w.name,
    w.slug,
    w.description,
    w.created_at,
    w.updated_at,
    (SELECT count(*) FROM member m WHERE m.workspace_id = w.id) AS member_count,
    (SELECT count(*) FROM agent a WHERE a.workspace_id = w.id) AS agent_count,
    COALESCE((SELECT u.name FROM member m JOIN "user" u ON u.id = m.user_id WHERE m.workspace_id = w.id AND m.role = 'owner' LIMIT 1), '') AS owner_name,
    COALESCE((SELECT u.email FROM member m JOIN "user" u ON u.id = m.user_id WHERE m.workspace_id = w.id AND m.role = 'owner' LIMIT 1), '') AS owner_email
FROM workspace w
ORDER BY w.created_at DESC;
