-- Agent Studio registry foundation (Phase 1).
-- Tenant identity is supplied by a verified host adapter; no tenant is seeded here.

CREATE TABLE agent_pack_templates (
    agent_key text PRIMARY KEY CHECK (agent_key ~ '^sim\.[a-z][a-z0-9_]{0,62}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    domain text NOT NULL CHECK (length(domain) BETWEEN 1 AND 100),
    source_pack text NOT NULL,
    status text NOT NULL DEFAULT 'unconfigured' CHECK (status IN ('unconfigured', 'configured')),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- These are identity templates only. SME roles/owners and tool grants are not
-- seeded because the sample pack does not establish organizational authority.
INSERT INTO agent_pack_templates (agent_key, display_name, domain, source_pack) VALUES
    ('sim.intake', 'Intake Agent', 'business-requirements', 'sim-b2b-six-agent'),
    ('sim.solution_matching', 'Solution Matching Agent', 'capability-fit', 'sim-b2b-six-agent'),
    ('sim.standard_project', 'Standard Project Agent', 'delivery-planning', 'sim-b2b-six-agent'),
    ('sim.design', 'Design Agent', 'architecture', 'sim-b2b-six-agent'),
    ('sim.cost_challenge', 'Cost Challenge Agent', 'costing-review', 'sim-b2b-six-agent'),
    ('sim.proposal', 'Proposal Agent', 'proposal-composition', 'sim-b2b-six-agent')
ON CONFLICT (agent_key) DO NOTHING;

CREATE TABLE llm_providers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    provider_key text NOT NULL CHECK (provider_key ~ '^[a-z][a-z0-9_-]{0,62}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    adapter_type text NOT NULL CHECK (adapter_type IN ('openai', 'openai_compatible', 'azure_openai', 'ollama', 'custom')),
    base_url text NOT NULL,
    secret_ref text,
    status text NOT NULL DEFAULT 'pending_test' CHECK (status IN ('pending_test', 'active', 'disabled', 'error')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, provider_key),
    UNIQUE (tenant_id, id),
    CHECK (secret_ref IS NULL OR secret_ref ~ '^[a-zA-Z][a-zA-Z0-9+.-]*://[^[:space:]]+$')
);

CREATE TABLE llm_model_deployments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    provider_id uuid NOT NULL,
    deployment_key text NOT NULL CHECK (deployment_key ~ '^[a-z][a-z0-9_-]{0,62}$'),
    provider_model_id text NOT NULL CHECK (length(provider_model_id) BETWEEN 1 AND 256),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    capabilities_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(capabilities_json) = 'object'),
    max_output_tokens integer NOT NULL CHECK (max_output_tokens BETWEEN 1 AND 1000000),
    timeout_ms integer NOT NULL CHECK (timeout_ms BETWEEN 100 AND 600000),
    cost_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(cost_policy_json) = 'object'),
    status text NOT NULL DEFAULT 'pending_test' CHECK (status IN ('pending_test', 'active', 'disabled', 'error')),
    config_version integer NOT NULL DEFAULT 1 CHECK (config_version > 0),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, deployment_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, provider_id) REFERENCES llm_providers (tenant_id, id)
);

CREATE TABLE model_approvals (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    deployment_id uuid NOT NULL,
    environment text NOT NULL CHECK (length(environment) BETWEEN 1 AND 64),
    purpose text NOT NULL CHECK (length(purpose) BETWEEN 1 AND 128),
    state text NOT NULL CHECK (state IN ('pending', 'approved', 'rejected', 'revoked')),
    capability_check_ref text,
    decision_reason text NOT NULL DEFAULT '',
    decided_by text,
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, deployment_id, environment, purpose),
    FOREIGN KEY (tenant_id, deployment_id) REFERENCES llm_model_deployments (tenant_id, id),
    CHECK (
        (state = 'approved' AND decided_by IS NOT NULL AND decided_at IS NOT NULL)
        OR (state <> 'approved' AND decided_by IS NULL AND decided_at IS NULL)
    )
);

CREATE TABLE agents (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_key text NOT NULL CHECK (agent_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    display_name text NOT NULL CHECK (length(display_name) BETWEEN 1 AND 200),
    domain text NOT NULL CHECK (length(domain) BETWEEN 1 AND 100),
    owner_subject text,
    lifecycle_status text NOT NULL DEFAULT 'draft' CHECK (lifecycle_status IN ('draft', 'active', 'archived')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, agent_key),
    UNIQUE (tenant_id, id)
);

CREATE TABLE agent_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    semver text NOT NULL CHECK (semver ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
    manifest_json jsonb NOT NULL CHECK (jsonb_typeof(manifest_json) = 'object'),
    input_schema_json jsonb NOT NULL CHECK (jsonb_typeof(input_schema_json) = 'object'),
    output_schema_json jsonb NOT NULL CHECK (jsonb_typeof(output_schema_json) = 'object'),
    model_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(model_policy_json) = 'object'),
    tool_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(tool_policy_json) = 'object'),
    checksum text CHECK (checksum IS NULL OR checksum ~ '^[0-9a-f]{64}$'),
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'published', 'deprecated')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_by text,
    published_at timestamptz,
    UNIQUE (tenant_id, agent_id, semver),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id),
    CHECK (state = 'draft' OR (checksum IS NOT NULL AND published_by IS NOT NULL AND published_at IS NOT NULL))
);
CREATE INDEX agent_versions_published_idx ON agent_versions (tenant_id, agent_id, created_at DESC)
    WHERE state = 'published';

CREATE TABLE agent_model_bindings (
    tenant_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    model_deployment_id uuid NOT NULL,
    fallback_deployment_id uuid,
    override_policy_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(override_policy_json) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, agent_version_id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id),
    FOREIGN KEY (tenant_id, model_deployment_id) REFERENCES llm_model_deployments (tenant_id, id),
    FOREIGN KEY (tenant_id, fallback_deployment_id) REFERENCES llm_model_deployments (tenant_id, id)
);

CREATE TABLE skills (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    skill_key text NOT NULL CHECK (skill_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    domain text NOT NULL CHECK (length(domain) BETWEEN 1 AND 100),
    owner_subject text,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, agent_id, skill_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id)
);

CREATE TABLE skill_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    skill_id uuid NOT NULL,
    semver text NOT NULL CHECK (semver ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
    instructions text NOT NULL,
    procedures_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(procedures_json) = 'array'),
    examples_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(examples_json) = 'array'),
    forbidden_actions_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(forbidden_actions_json) = 'array'),
    output_schema_json jsonb NOT NULL CHECK (jsonb_typeof(output_schema_json) = 'object'),
    checksum text CHECK (checksum IS NULL OR checksum ~ '^[0-9a-f]{64}$'),
    state text NOT NULL DEFAULT 'draft' CHECK (state IN ('draft', 'published', 'deprecated')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    published_by text,
    published_at timestamptz,
    UNIQUE (tenant_id, skill_id, semver),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, skill_id) REFERENCES skills (tenant_id, id),
    CHECK (state = 'draft' OR (checksum IS NOT NULL AND published_by IS NOT NULL AND published_at IS NOT NULL))
);

CREATE TABLE agent_skill_bindings (
    tenant_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    skill_version_id uuid NOT NULL,
    PRIMARY KEY (tenant_id, agent_version_id, skill_version_id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id),
    FOREIGN KEY (tenant_id, skill_version_id) REFERENCES skill_versions (tenant_id, id)
);

CREATE TABLE tools (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tool_key text NOT NULL CHECK (tool_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    kind text NOT NULL CHECK (length(kind) BETWEEN 1 AND 64),
    owner text NOT NULL,
    risk_level text NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    lifecycle_status text NOT NULL DEFAULT 'draft' CHECK (lifecycle_status IN ('draft', 'active', 'archived')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tool_key)
);

CREATE TABLE tool_versions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tool_id uuid NOT NULL REFERENCES tools (id),
    semver text NOT NULL CHECK (semver ~ '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'),
    adapter_ref text NOT NULL,
    input_schema_json jsonb NOT NULL CHECK (jsonb_typeof(input_schema_json) = 'object'),
    output_schema_json jsonb NOT NULL CHECK (jsonb_typeof(output_schema_json) = 'object'),
    policy_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(policy_json) = 'object'),
    checksum text CHECK (checksum IS NULL OR checksum ~ '^[0-9a-f]{64}$'),
    published_at timestamptz,
    UNIQUE (tool_id, semver),
    CHECK (published_at IS NULL OR checksum IS NOT NULL)
);

CREATE FUNCTION agent_studio_immutable_published_tool_version() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.published_at IS NOT NULL THEN
            RAISE EXCEPTION 'published tool version % cannot be deleted', OLD.id USING ERRCODE = 'AS001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.published_at IS NOT NULL AND
       (NEW.tool_id IS DISTINCT FROM OLD.tool_id OR NEW.semver IS DISTINCT FROM OLD.semver OR
        NEW.adapter_ref IS DISTINCT FROM OLD.adapter_ref OR NEW.input_schema_json IS DISTINCT FROM OLD.input_schema_json OR
        NEW.output_schema_json IS DISTINCT FROM OLD.output_schema_json OR NEW.policy_json IS DISTINCT FROM OLD.policy_json OR
        NEW.checksum IS DISTINCT FROM OLD.checksum OR NEW.published_at IS DISTINCT FROM OLD.published_at) THEN
        RAISE EXCEPTION 'published tool version % is immutable', OLD.id USING ERRCODE = 'AS001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER tool_versions_immutable BEFORE UPDATE OR DELETE ON tool_versions
    FOR EACH ROW EXECUTE FUNCTION agent_studio_immutable_published_tool_version();

CREATE TABLE agent_tool_bindings (
    tenant_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    tool_version_id uuid NOT NULL REFERENCES tool_versions (id),
    action_scope_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(action_scope_json) = 'object'),
    PRIMARY KEY (tenant_id, agent_version_id, tool_version_id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id)
);

CREATE FUNCTION agent_studio_guard_draft_bindings() RETURNS trigger
    LANGUAGE plpgsql AS
$$
DECLARE
    version_id uuid;
    row_tenant uuid;
    version_state text;
BEGIN
    IF TG_OP = 'DELETE' THEN
        version_id := OLD.agent_version_id;
        row_tenant := OLD.tenant_id;
    ELSE
        version_id := NEW.agent_version_id;
        row_tenant := NEW.tenant_id;
    END IF;
    SELECT state INTO version_state FROM agent_versions
      WHERE id = version_id AND tenant_id = row_tenant;
    IF version_state IS DISTINCT FROM 'draft' THEN
        RAISE EXCEPTION 'bindings can only change while their agent version is a draft' USING ERRCODE = 'AS001';
    END IF;
    IF TG_OP = 'UPDATE' THEN
        SELECT state INTO version_state FROM agent_versions
          WHERE id = OLD.agent_version_id AND tenant_id = OLD.tenant_id;
        IF version_state IS DISTINCT FROM 'draft' THEN
            RAISE EXCEPTION 'published agent bindings cannot be moved or changed' USING ERRCODE = 'AS001';
        END IF;
    END IF;
    IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER agent_model_bindings_draft_only BEFORE INSERT OR UPDATE OR DELETE ON agent_model_bindings
    FOR EACH ROW EXECUTE FUNCTION agent_studio_guard_draft_bindings();
CREATE TRIGGER agent_skill_bindings_draft_only BEFORE INSERT OR UPDATE OR DELETE ON agent_skill_bindings
    FOR EACH ROW EXECUTE FUNCTION agent_studio_guard_draft_bindings();
CREATE TRIGGER agent_tool_bindings_draft_only BEFORE INSERT OR UPDATE OR DELETE ON agent_tool_bindings
    FOR EACH ROW EXECUTE FUNCTION agent_studio_guard_draft_bindings();

CREATE TABLE audit_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    actor_subject text NOT NULL,
    action text NOT NULL CHECK (length(action) BETWEEN 1 AND 128),
    resource_ref text NOT NULL,
    before_hash text CHECK (before_hash IS NULL OR before_hash ~ '^[0-9a-f]{64}$'),
    after_hash text CHECK (after_hash IS NULL OR after_hash ~ '^[0-9a-f]{64}$'),
    correlation_id text,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE FUNCTION agent_studio_immutable_published_version() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.state IN ('published', 'deprecated') THEN
            RAISE EXCEPTION 'published version % cannot be deleted', OLD.id USING ERRCODE = 'AS001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.state IN ('published', 'deprecated') AND
       (NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.agent_id IS DISTINCT FROM OLD.agent_id OR
        NEW.semver IS DISTINCT FROM OLD.semver OR NEW.manifest_json IS DISTINCT FROM OLD.manifest_json OR
        NEW.input_schema_json IS DISTINCT FROM OLD.input_schema_json OR NEW.output_schema_json IS DISTINCT FROM OLD.output_schema_json OR
        NEW.model_policy_json IS DISTINCT FROM OLD.model_policy_json OR NEW.tool_policy_json IS DISTINCT FROM OLD.tool_policy_json OR
        NEW.checksum IS DISTINCT FROM OLD.checksum OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at OR
        NEW.published_by IS DISTINCT FROM OLD.published_by OR NEW.published_at IS DISTINCT FROM OLD.published_at OR NEW.state NOT IN ('published', 'deprecated')) THEN
        RAISE EXCEPTION 'published version % is immutable', OLD.id USING ERRCODE = 'AS001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER agent_versions_immutable BEFORE UPDATE OR DELETE ON agent_versions
    FOR EACH ROW EXECUTE FUNCTION agent_studio_immutable_published_version();

CREATE FUNCTION agent_studio_immutable_published_skill_version() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.state IN ('published', 'deprecated') THEN
            RAISE EXCEPTION 'published skill version % cannot be deleted', OLD.id USING ERRCODE = 'AS001';
        END IF;
        RETURN OLD;
    END IF;
    IF OLD.state IN ('published', 'deprecated') AND
       (NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.skill_id IS DISTINCT FROM OLD.skill_id OR
        NEW.semver IS DISTINCT FROM OLD.semver OR NEW.instructions IS DISTINCT FROM OLD.instructions OR
        NEW.procedures_json IS DISTINCT FROM OLD.procedures_json OR NEW.examples_json IS DISTINCT FROM OLD.examples_json OR
        NEW.forbidden_actions_json IS DISTINCT FROM OLD.forbidden_actions_json OR NEW.output_schema_json IS DISTINCT FROM OLD.output_schema_json OR
        NEW.checksum IS DISTINCT FROM OLD.checksum OR NEW.created_by IS DISTINCT FROM OLD.created_by OR NEW.created_at IS DISTINCT FROM OLD.created_at OR
        NEW.published_by IS DISTINCT FROM OLD.published_by OR NEW.published_at IS DISTINCT FROM OLD.published_at OR NEW.state NOT IN ('published', 'deprecated')) THEN
        RAISE EXCEPTION 'published skill version % is immutable', OLD.id USING ERRCODE = 'AS001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER skill_versions_immutable BEFORE UPDATE OR DELETE ON skill_versions
    FOR EACH ROW EXECUTE FUNCTION agent_studio_immutable_published_skill_version();

CREATE FUNCTION agent_studio_append_only_audit() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    RAISE EXCEPTION 'audit events are append-only' USING ERRCODE = 'AS001';
END;
$$;
CREATE TRIGGER audit_events_append_only BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION agent_studio_append_only_audit();

-- Missing tenant context is denied. The API must set this from its verified
-- AuthAdapter inside each transaction. The DB role must not own these tables.
DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'llm_providers', 'llm_model_deployments', 'model_approvals', 'agents',
        'agent_versions', 'agent_model_bindings', 'skills', 'skill_versions',
        'agent_skill_bindings', 'agent_tool_bindings', 'audit_events'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format('CREATE POLICY tenant_scope ON %I USING (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)', table_name);
    END LOOP;
END;
$$;
