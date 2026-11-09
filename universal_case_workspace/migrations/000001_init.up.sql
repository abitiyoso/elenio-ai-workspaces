-- Universal Case Workspace owns only presentation configuration and its own conversation state:
-- the domain pack registry, case <-> pack bindings, intake sessions and audit events. Cases, tasks,
-- approvals and artifacts stay in Flowcase; users stay in the Control Plane.

-- Immutable pack versions (insert-only; see trigger below).
CREATE TABLE workspace_pack_versions (
    pack_key     text NOT NULL CHECK (pack_key ~ '^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$'),
    version      text NOT NULL CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'),
    checksum     text NOT NULL CHECK (checksum ~ '^sha256:[0-9a-f]{64}$'),
    manifest     jsonb NOT NULL CHECK (jsonb_typeof(manifest) = 'object'),
    status       text NOT NULL CHECK (status IN ('draft', 'published', 'retired')),
    source_file  text NOT NULL DEFAULT '',
    stored_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pack_key, version)
);

CREATE FUNCTION workspace_pack_versions_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'workspace_pack_versions rows are immutable; publish a new version';
END $$;

CREATE TRIGGER workspace_pack_versions_no_update BEFORE UPDATE OR DELETE ON workspace_pack_versions
    FOR EACH ROW EXECUTE FUNCTION workspace_pack_versions_immutable();

-- A case is pinned to exactly one pack version, once. Never re-pinned implicitly.
CREATE TABLE workspace_case_bindings (
    tenant_key   text NOT NULL,
    case_id      text NOT NULL,
    pack_key     text NOT NULL,
    pack_version text NOT NULL,
    binding      text NOT NULL CHECK (binding IN ('pinned_at_creation', 'pinned_on_first_view')),
    bound_at     timestamptz NOT NULL DEFAULT now(),
    bound_by     text NOT NULL,
    PRIMARY KEY (tenant_key, case_id),
    FOREIGN KEY (pack_key, pack_version) REFERENCES workspace_pack_versions (pack_key, version)
);

-- Pre-case intake chat, private to the user who opened it.
CREATE TABLE workspace_intake_sessions (
    id              uuid PRIMARY KEY,
    tenant_key      text NOT NULL,
    user_id         text NOT NULL,
    pack_key        text NOT NULL,
    pack_version    text NOT NULL,
    workflow_key    text NOT NULL,
    -- snapshot of the pack's intake block at creation (agent, greeting, starters, fields)
    intake          jsonb NOT NULL CHECK (jsonb_typeof(intake) = 'object'),
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'created', 'abandoned')),
    draft           jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(draft) = 'object'),
    messages        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(messages) = 'array'),
    kb_status       text NOT NULL DEFAULT 'unknown' CHECK (kb_status IN ('covered', 'partial', 'not_covered', 'unknown')),
    kb_items        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(kb_items) = 'array'),
    sources         jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(sources) = 'array'),
    ready           boolean NOT NULL DEFAULT false,
    missing         jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(missing) = 'array'),
    pending_key     text,
    pending_run_id  text,
    pending_agent   text,
    pending_since   timestamptz,
    turn            integer NOT NULL DEFAULT 0 CHECK (turn >= 0),
    last_error      text,
    case_id         text,
    case_reference  text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    version_num     integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    FOREIGN KEY (pack_key, pack_version) REFERENCES workspace_pack_versions (pack_key, version),
    CHECK ((status = 'created') = (case_id IS NOT NULL)),
    CHECK (pending_run_id IS NULL OR pending_key IS NOT NULL),
    CHECK ((pending_key IS NULL) = (pending_since IS NULL))
);
CREATE INDEX workspace_intake_sessions_user_idx ON workspace_intake_sessions (tenant_key, user_id, updated_at DESC);

-- Append-only audit trail with redacted metadata (never message text, drafts or tokens).
CREATE TABLE workspace_audit_events (
    id              bigserial PRIMARY KEY,
    tenant_key      text NOT NULL DEFAULT '',
    actor_ref       text NOT NULL,
    action          text NOT NULL,
    resource_ref    text NOT NULL,
    correlation_id  text NOT NULL DEFAULT '',
    metadata        jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(metadata) = 'object'),
    occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workspace_audit_events_resource_idx ON workspace_audit_events (resource_ref, occurred_at DESC);
