CREATE TABLE epic (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    identifier TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    color TEXT NOT NULL DEFAULT '#6366f1',
    status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'in_progress', 'completed', 'cancelled')),
    lead_type TEXT CHECK (lead_type IN ('member', 'agent')),
    lead_id UUID,
    start_date DATE,
    due_date DATE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, identifier)
);

CREATE INDEX idx_epic_workspace ON epic(workspace_id);
CREATE INDEX idx_epic_project ON epic(project_id);

CREATE TABLE sprint (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    goal TEXT,
    status TEXT NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'active', 'completed', 'cancelled')),
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sprint_workspace ON sprint(workspace_id);
CREATE INDEX idx_sprint_project ON sprint(project_id);
CREATE UNIQUE INDEX idx_sprint_single_active ON sprint(workspace_id) WHERE status = 'active';

ALTER TABLE issue ADD COLUMN epic_id UUID REFERENCES epic(id) ON DELETE SET NULL;
ALTER TABLE issue ADD COLUMN sprint_id UUID REFERENCES sprint(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_epic ON issue(epic_id);
CREATE INDEX idx_issue_sprint ON issue(sprint_id);
