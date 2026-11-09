-- KMS-side directory of principals admitted through AUTH_MODE=control_plane_jwt.
-- principal_id is the Control Plane user id (token "sub"). A verified token is accepted only when an
-- active row exists for (mapped tenant, sub). KMS tenant roles come ONLY from this table, never from the
-- token: a Control Plane platform admin is not automatically a KMS admin.
CREATE TABLE tenant_principals (
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    principal_id UUID NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
    roles TEXT[] NOT NULL DEFAULT '{}' CHECK (roles <@ ARRAY['platform_admin','knowledge_steward']::text[]),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, principal_id)
);
