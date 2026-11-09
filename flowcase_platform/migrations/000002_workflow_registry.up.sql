-- Workflow registry (WCP-01). A definition is the logical key; each version is a separate row whose
-- content becomes immutable once published (PRD §9 invariant 1, UAT-02).

CREATE TABLE workflow_definitions (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL REFERENCES tenants (id),
    key         text NOT NULL CHECK (key ~ '^[a-z][a-z0-9-]{0,62}$'),
    name        text NOT NULL CHECK (length(name) BETWEEN 3 AND 200),
    description text NOT NULL DEFAULT '',
    domain_pack text CHECK (domain_pack IS NULL OR domain_pack ~ '^[a-z][a-z0-9-]{0,62}$'),
    status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
    created_by  uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    version_num integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES memberships (tenant_id, user_id)
);

CREATE TABLE workflow_versions (
    id                      uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id               uuid NOT NULL,
    definition_id           uuid NOT NULL,
    version                 integer NOT NULL CHECK (version >= 1),
    schema_version          text NOT NULL,
    status                  text NOT NULL CHECK (status IN ('draft', 'validated', 'published', 'deprecated')),
    definition              jsonb NOT NULL CHECK (jsonb_typeof(definition) = 'object'),
    definition_hash         text CHECK (definition_hash IS NULL OR definition_hash ~ '^[0-9a-f]{64}$'),
    derived_from_version_id uuid,
    created_by              uuid NOT NULL,
    created_at              timestamptz NOT NULL DEFAULT now(),
    updated_by              uuid,
    updated_at              timestamptz NOT NULL DEFAULT now(),
    validated_at            timestamptz,
    published_by            uuid,
    published_at            timestamptz,
    deprecated_by           uuid,
    deprecated_at           timestamptz,
    version_num             integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (definition_id, version),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, definition_id) REFERENCES workflow_definitions (tenant_id, id),
    FOREIGN KEY (tenant_id, derived_from_version_id) REFERENCES workflow_versions (tenant_id, id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES memberships (tenant_id, user_id),
    FOREIGN KEY (tenant_id, published_by) REFERENCES memberships (tenant_id, user_id),
    CHECK (status NOT IN ('published', 'deprecated')
           OR (definition_hash IS NOT NULL AND published_by IS NOT NULL AND published_at IS NOT NULL))
);
CREATE INDEX workflow_versions_published_idx ON workflow_versions (tenant_id, definition_id, version DESC)
    WHERE status = 'published';

-- SQLSTATE FC001 = immutable record violation; the Go layer maps it to a 409 conflict.
CREATE FUNCTION flowcase_guard_workflow_version() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status IN ('published', 'deprecated') THEN
            RAISE EXCEPTION 'workflow version % is published and cannot be deleted', OLD.id USING ERRCODE = 'FC001';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.status IN ('published', 'deprecated') THEN
        IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
            OR NEW.definition_id IS DISTINCT FROM OLD.definition_id
            OR NEW.version IS DISTINCT FROM OLD.version
            OR NEW.schema_version IS DISTINCT FROM OLD.schema_version
            OR NEW.definition IS DISTINCT FROM OLD.definition
            OR NEW.definition_hash IS DISTINCT FROM OLD.definition_hash
            OR NEW.derived_from_version_id IS DISTINCT FROM OLD.derived_from_version_id
            OR NEW.created_by IS DISTINCT FROM OLD.created_by
            OR NEW.created_at IS DISTINCT FROM OLD.created_at
            OR NEW.published_by IS DISTINCT FROM OLD.published_by
            OR NEW.published_at IS DISTINCT FROM OLD.published_at
            OR NEW.status NOT IN ('published', 'deprecated') THEN
            RAISE EXCEPTION 'workflow version % is published and immutable', OLD.id USING ERRCODE = 'FC001';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER workflow_versions_immutable
    BEFORE UPDATE OR DELETE ON workflow_versions
    FOR EACH ROW EXECUTE FUNCTION flowcase_guard_workflow_version();

CREATE FUNCTION flowcase_forbid_truncate() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    RAISE EXCEPTION 'TRUNCATE is not permitted on %', TG_TABLE_NAME USING ERRCODE = 'FC001';
END;
$$;

CREATE TRIGGER workflow_versions_no_truncate
    BEFORE TRUNCATE ON workflow_versions
    FOR EACH STATEMENT EXECUTE FUNCTION flowcase_forbid_truncate();
