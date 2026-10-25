DROP TABLE IF EXISTS system_audit_log;
DROP TABLE IF EXISTS instance_invitation;
DROP TABLE IF EXISTS system_settings;

ALTER TABLE "user"
    DROP COLUMN IF EXISTS suspension_reason,
    DROP COLUMN IF EXISTS suspended_at,
    DROP COLUMN IF EXISTS is_suspended,
    DROP COLUMN IF EXISTS system_role;
