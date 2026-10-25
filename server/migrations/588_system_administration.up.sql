ALTER TABLE "user"
    ADD COLUMN system_role TEXT NOT NULL DEFAULT 'standard'
        CHECK (system_role IN ('instance_owner', 'instance_admin', 'standard')),
    ADD COLUMN is_suspended BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN suspended_at TIMESTAMPTZ,
    ADD COLUMN suspension_reason TEXT;

CREATE INDEX idx_user_system_role ON "user"(system_role) WHERE system_role != 'standard';

CREATE TABLE system_settings (
    key TEXT PRIMARY KEY,
    value JSONB NOT NULL,
    description TEXT,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO system_settings (key, value, description)
VALUES 
    ('signup_policy', '{"policy": "invite_only", "allowed_domains": []}'::jsonb, 'Global registration and onboarding policy: open, invite_only, domain_allowlist, closed'),
    ('invitation_authority', '{"who_can_invite": "admins_only"}'::jsonb, 'Who can invite new users to the instance: admins_only, workspace_admins, all_members'),
    ('workspace_creation_policy', '{"policy": "admins_only", "max_workspaces_per_user": 0}'::jsonb, 'Who can create workspaces: admins_only, anyone, disabled'),
    ('execution_safety', '{"enforce_worktree": true, "max_task_turns": 40, "global_timeout_seconds": 2700}'::jsonb, 'Runtime safety and runaway loop circuit breakers');

CREATE TABLE instance_invitation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL,
    token_hash TEXT UNIQUE NOT NULL,
    system_role TEXT NOT NULL DEFAULT 'standard'
        CHECK (system_role IN ('instance_admin', 'standard')),
    default_workspace_id UUID REFERENCES workspace(id) ON DELETE SET NULL,
    default_workspace_role TEXT CHECK (default_workspace_role IN ('owner', 'admin', 'member')),
    invited_by UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '7 days'),
    accepted_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_instance_invitation_lookup 
    ON instance_invitation(email, expires_at) 
    WHERE accepted_at IS NULL AND revoked_at IS NULL;

CREATE TABLE system_audit_log (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    actor_ip TEXT,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    changes JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_log_created_at ON system_audit_log(created_at DESC);
CREATE INDEX idx_audit_log_target ON system_audit_log(target_type, target_id);
