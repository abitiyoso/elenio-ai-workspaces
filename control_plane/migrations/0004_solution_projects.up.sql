-- Planning metadata only. Workflow execution, publication and agent release stay in their services.
CREATE TABLE solution_projects (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    workspace_id uuid NOT NULL,
    key text NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]{1,62}$'),
    revision integer NOT NULL DEFAULT 1 CHECK (revision > 0),
    definition jsonb NOT NULL CHECK (jsonb_typeof(definition) = 'object'),
    creation_key text NOT NULL,
    creation_hash text NOT NULL,
    created_by uuid NOT NULL REFERENCES users(id),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces(tenant_id, id),
    UNIQUE (workspace_id, key),
    UNIQUE (workspace_id, creation_key)
);
CREATE INDEX solution_projects_scope_idx ON solution_projects (tenant_id, workspace_id, id);

INSERT INTO permissions(key,resource_type,action,description,sensitive) VALUES
 ('solution:view','solution','view','Read solution planning metadata in the active workspace',false),
 ('solution:author','solution','author','Edit solution plans; does not authorize workflow/model publication or deployment',false);
INSERT INTO roles(key,display_name,description,scope,sensitive) VALUES
 ('solution_builder','Solution builder','Author solution plans in one workspace; no domain approvals or deployment authority','workspace',false);
INSERT INTO role_permissions(role_id,permission_key)
 SELECT r.id,p.key FROM roles r CROSS JOIN permissions p
 WHERE r.key IN ('solution_builder','workspace_admin') AND p.key IN ('solution:view','solution:author');
INSERT INTO role_permissions(role_id,permission_key)
 SELECT id,'catalog:view' FROM roles WHERE key='solution_builder';
