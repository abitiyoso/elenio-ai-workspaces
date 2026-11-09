-- Tenancy and identity directory (WCP-10). Users are global principals keyed by token issuer+subject;
-- all authority is tenant-scoped through memberships and role_assignments.

CREATE TABLE tenants (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    external_ref text NOT NULL UNIQUE CHECK (length(external_ref) BETWEEN 1 AND 256),
    name         text NOT NULL CHECK (length(name) BETWEEN 1 AND 200),
    status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    issuer       text NOT NULL CHECK (length(issuer) BETWEEN 1 AND 512),
    subject      text NOT NULL CHECK (length(subject) BETWEEN 1 AND 256),
    display_name text NOT NULL DEFAULT '',
    -- service principals (agents, connectors) can never hold human approval authority
    kind         text NOT NULL DEFAULT 'human' CHECK (kind IN ('human', 'service')),
    status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, subject)
);

CREATE TABLE memberships (
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    user_id    uuid NOT NULL REFERENCES users (id),
    status     text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'removed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id)
);
CREATE INDEX memberships_user_idx ON memberships (user_id);

CREATE TABLE workspaces (
    id         uuid NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id  uuid NOT NULL REFERENCES tenants (id),
    key        text NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]{0,62}$'),
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, key)
);

CREATE TABLE role_assignments (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL,
    user_id     uuid NOT NULL,
    role        text NOT NULL CHECK (role IN ('workspace_member', 'case_owner', 'workflow_designer', 'workflow_publisher',
                                              'sme_contributor', 'domain_approver', 'ai_ops', 'tenant_admin')),
    scope_type  text NOT NULL DEFAULT 'tenant' CHECK (scope_type IN ('tenant', 'workspace', 'case')),
    scope_id    uuid,
    granted_by  uuid REFERENCES users (id),
    valid_from  timestamptz NOT NULL DEFAULT now(),
    valid_until timestamptz,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships (tenant_id, user_id),
    CHECK ((scope_type = 'tenant') = (scope_id IS NULL)),
    CHECK (valid_until IS NULL OR valid_until > valid_from)
);
CREATE UNIQUE INDEX role_assignments_active_uidx
    ON role_assignments (tenant_id, user_id, role, scope_type, coalesce(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE revoked_at IS NULL;
