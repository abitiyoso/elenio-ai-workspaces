-- Flowcase remains the case/data authority; retain only solution ownership and immutable execution pins.
ALTER TABLE solution_projects ADD CONSTRAINT solution_projects_scoped_id UNIQUE (tenant_id, workspace_id, id);
CREATE TABLE solution_executions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    solution_id uuid NOT NULL,
    plan_revision integer NOT NULL CHECK (plan_revision > 0),
    workflow_key text NOT NULL,
    workflow_version integer NOT NULL CHECK (workflow_version > 0),
    created_by uuid NOT NULL REFERENCES users(id),
    request_key text NOT NULL,
    request_hash text NOT NULL,
    case_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, workspace_id, solution_id) REFERENCES solution_projects(tenant_id, workspace_id, id),
    UNIQUE (workspace_id, created_by, request_key),
    UNIQUE (tenant_id, case_id)
);
CREATE INDEX solution_executions_scope_idx ON solution_executions (tenant_id, workspace_id, solution_id, created_by, id);
