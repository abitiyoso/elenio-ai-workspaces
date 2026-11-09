-- Tenant-owned tool drafts stay separate from the platform-global tools table.
-- They are not executable or bindable until a separately approved bridge is
-- implemented.

CREATE TABLE tenant_tool_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    tool_key text NOT NULL CHECK (tool_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    kind text NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
    risk_level text NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    lifecycle_status text NOT NULL DEFAULT 'draft' CHECK (lifecycle_status IN ('draft', 'archived')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, tool_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE tenant_tool_draft_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    tool_id uuid NOT NULL,
    semver text NOT NULL CHECK (semver ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
    adapter_ref text NOT NULL CHECK (adapter_ref ~ '^[a-z][a-z0-9_.-]{0,127}$'),
    input_schema_json jsonb NOT NULL CHECK (jsonb_typeof(input_schema_json) = 'object'),
    output_schema_json jsonb NOT NULL CHECK (jsonb_typeof(output_schema_json) = 'object'),
    policy_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(policy_json) = 'object'),
    checksum text CHECK (checksum IS NULL OR checksum ~ '^[0-9a-f]{64}$'),
    state text NOT NULL DEFAULT 'draft' CHECK (state = 'draft'),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, tool_id, semver),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, tool_id) REFERENCES tenant_tool_drafts (tenant_id, id)
);

CREATE INDEX tenant_tool_drafts_listing_idx
    ON tenant_tool_drafts (tenant_id, created_at DESC, id);
CREATE INDEX tenant_tool_draft_versions_listing_idx
    ON tenant_tool_draft_versions (tenant_id, tool_id, created_at DESC, id);

CREATE FUNCTION agent_studio_immutable_tenant_tool_draft_version() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.state <> 'draft' THEN
            RAISE EXCEPTION 'non-draft tenant tool version % cannot be deleted', OLD.id USING ERRCODE = 'AS001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.state <> 'draft' AND
       (NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.tool_id IS DISTINCT FROM OLD.tool_id OR
        NEW.semver IS DISTINCT FROM OLD.semver OR NEW.adapter_ref IS DISTINCT FROM OLD.adapter_ref OR
        NEW.input_schema_json IS DISTINCT FROM OLD.input_schema_json OR NEW.output_schema_json IS DISTINCT FROM OLD.output_schema_json OR
        NEW.policy_json IS DISTINCT FROM OLD.policy_json OR NEW.checksum IS DISTINCT FROM OLD.checksum OR
        NEW.state IS DISTINCT FROM OLD.state) THEN
        RAISE EXCEPTION 'non-draft tenant tool version % is immutable', OLD.id USING ERRCODE = 'AS001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER tenant_tool_draft_versions_immutable
    BEFORE UPDATE OR DELETE ON tenant_tool_draft_versions
    FOR EACH ROW EXECUTE FUNCTION agent_studio_immutable_tenant_tool_draft_version();

ALTER TABLE tenant_tool_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_tool_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON tenant_tool_drafts
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE tenant_tool_draft_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_tool_draft_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON tenant_tool_draft_versions
    USING (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = nullif(current_setting('app.tenant_id', true), '')::uuid);
