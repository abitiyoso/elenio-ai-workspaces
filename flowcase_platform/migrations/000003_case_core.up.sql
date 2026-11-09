-- Case core (WCP-03/04/09): pinned cases, execution tokens, node executions, append-only audit,
-- transactional outbox and inbox dedup. Engine behaviour arrives in Phase 2; these tables fix the
-- invariants the engine must respect.

CREATE TABLE cases (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL,
    workspace_id        uuid,
    reference           text NOT NULL CHECK (length(reference) BETWEEN 1 AND 64),
    title               text NOT NULL CHECK (length(title) BETWEEN 1 AND 300),
    workflow_version_id uuid NOT NULL,
    -- pinned copy of the version identity; must match workflow_versions at insert and never changes
    workflow_key        text NOT NULL,
    workflow_version    integer NOT NULL,
    definition_hash     text NOT NULL CHECK (definition_hash ~ '^[0-9a-f]{64}$'),
    status              text NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'paused', 'under_review', 'completed', 'cancelled', 'failed')),
    owner_id            uuid NOT NULL,
    variables           jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(variables) = 'object'),
    business_facets     jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(business_facets) = 'object'),
    due_at              timestamptz,
    created_by          uuid NOT NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    closed_at           timestamptz,
    version_num         integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, reference),
    FOREIGN KEY (tenant_id, workflow_version_id) REFERENCES workflow_versions (tenant_id, id),
    FOREIGN KEY (tenant_id, workspace_id) REFERENCES workspaces (tenant_id, id),
    FOREIGN KEY (tenant_id, owner_id) REFERENCES memberships (tenant_id, user_id),
    FOREIGN KEY (tenant_id, created_by) REFERENCES memberships (tenant_id, user_id)
);
CREATE INDEX cases_tenant_status_idx ON cases (tenant_id, status, updated_at DESC);

CREATE FUNCTION flowcase_guard_case_pin() RETURNS trigger
    LANGUAGE plpgsql AS
$$
DECLARE
    v record;
BEGIN
    IF TG_OP = 'INSERT' THEN
        SELECT wv.status, wv.version, wv.definition_hash, wd.key
        INTO v
        FROM workflow_versions wv
        JOIN workflow_definitions wd ON wd.id = wv.definition_id AND wd.tenant_id = wv.tenant_id
        WHERE wv.id = NEW.workflow_version_id AND wv.tenant_id = NEW.tenant_id;
        IF NOT FOUND OR v.status <> 'published' THEN
            RAISE EXCEPTION 'cases can only start from a published workflow version' USING ERRCODE = 'FC002';
        END IF;
        IF v.key <> NEW.workflow_key OR v.version <> NEW.workflow_version OR v.definition_hash <> NEW.definition_hash THEN
            RAISE EXCEPTION 'case pin does not match workflow version %', NEW.workflow_version_id USING ERRCODE = 'FC002';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.workflow_version_id IS DISTINCT FROM OLD.workflow_version_id
        OR NEW.workflow_key IS DISTINCT FROM OLD.workflow_key
        OR NEW.workflow_version IS DISTINCT FROM OLD.workflow_version
        OR NEW.definition_hash IS DISTINCT FROM OLD.definition_hash
        OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id THEN
        RAISE EXCEPTION 'case % is pinned to its workflow version', OLD.id USING ERRCODE = 'FC001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER cases_pin_guard
    BEFORE INSERT OR UPDATE ON cases
    FOR EACH ROW EXECUTE FUNCTION flowcase_guard_case_pin();

CREATE TABLE case_members (
    tenant_id  uuid NOT NULL,
    case_id    uuid NOT NULL,
    user_id    uuid NOT NULL,
    relation   text NOT NULL CHECK (relation IN ('owner', 'contributor', 'reviewer', 'viewer')),
    added_by   uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (case_id, user_id, relation),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships (tenant_id, user_id)
);
CREATE INDEX case_members_user_idx ON case_members (tenant_id, user_id);

CREATE TABLE execution_tokens (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL,
    case_id         uuid NOT NULL,
    parent_token_id uuid,
    split_node_id   text,
    branch_key      text NOT NULL DEFAULT 'root',
    status          text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'waiting', 'joined', 'completed', 'cancelled')),
    current_node_id text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    -- a split creates exactly one token per branch under a parent
    UNIQUE (case_id, parent_token_id, split_node_id, branch_key),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, parent_token_id) REFERENCES execution_tokens (tenant_id, id),
    CHECK ((parent_token_id IS NULL) = (split_node_id IS NULL))
);

CREATE TABLE node_executions (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL,
    case_id          uuid NOT NULL,
    token_id         uuid NOT NULL,
    node_id          text NOT NULL,
    node_type        text NOT NULL CHECK (node_type IN ('start', 'end', 'human_task', 'agent_task', 'service_task', 'human_approval',
                                                         'condition', 'parallel_split', 'parallel_join', 'timer', 'subworkflow')),
    iteration        integer NOT NULL DEFAULT 1 CHECK (iteration >= 1),
    status           text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'ready', 'running', 'waiting_human', 'waiting_external', 'completed', 'failed', 'cancelled', 'superseded')),
    idempotency_key  text NOT NULL UNIQUE,
    attempt          integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    max_attempts     integer NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    lease_owner      text,
    lease_expires_at timestamptz,
    next_attempt_at  timestamptz,
    wake_at          timestamptz,
    input            jsonb NOT NULL DEFAULT '{}',
    output           jsonb,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    completed_at     timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    version_num      integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, id),
    -- runtime contract: one execution per case + node + iteration + branch token
    UNIQUE (case_id, node_id, iteration, token_id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, token_id) REFERENCES execution_tokens (tenant_id, id),
    CHECK ((lease_owner IS NULL) = (lease_expires_at IS NULL))
);
CREATE INDEX node_executions_ready_idx ON node_executions (coalesce(next_attempt_at, created_at)) WHERE status = 'ready';
CREATE INDEX node_executions_lease_idx ON node_executions (lease_expires_at) WHERE status = 'running';
CREATE INDEX node_executions_timer_idx ON node_executions (wake_at) WHERE wake_at IS NOT NULL AND status = 'waiting_external';
CREATE INDEX node_executions_case_idx ON node_executions (tenant_id, case_id, status);

CREATE TABLE audit_events (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq             bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    tenant_id       uuid NOT NULL REFERENCES tenants (id),
    occurred_at     timestamptz NOT NULL DEFAULT now(),
    actor_type      text NOT NULL CHECK (actor_type IN ('user', 'service', 'system')),
    actor_id        uuid REFERENCES users (id),
    action          text NOT NULL CHECK (action ~ '^[a-z][a-z0-9_.]{2,100}$'),
    subject_type    text NOT NULL,
    subject_id      text NOT NULL,
    subject_version text,
    case_id         uuid,
    trace_id        text,
    -- redacted, non-secret context only
    data            jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(data) = 'object'),
    CHECK (actor_type = 'system' OR actor_id IS NOT NULL)
);
CREATE INDEX audit_events_tenant_time_idx ON audit_events (tenant_id, occurred_at DESC, seq DESC);
CREATE INDEX audit_events_subject_idx ON audit_events (tenant_id, subject_type, subject_id);

CREATE FUNCTION flowcase_audit_append_only() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only' USING ERRCODE = 'FC001';
END;
$$;

CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION flowcase_audit_append_only();

CREATE TRIGGER audit_events_no_truncate
    BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION flowcase_forbid_truncate();

CREATE TABLE outbox_events (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seq              bigint GENERATED ALWAYS AS IDENTITY UNIQUE,
    tenant_id        uuid NOT NULL REFERENCES tenants (id),
    topic            text NOT NULL CHECK (topic ~ '^[a-z][a-z0-9_.]{2,100}$'),
    aggregate_type   text NOT NULL,
    aggregate_id     text NOT NULL,
    dedupe_key       text NOT NULL UNIQUE,
    payload          jsonb NOT NULL DEFAULT '{}',
    trace_id         text,
    status           text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'dead')),
    attempts         integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    available_at     timestamptz NOT NULL DEFAULT now(),
    lease_owner      text,
    lease_expires_at timestamptz,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    processed_at     timestamptz,
    CHECK ((lease_owner IS NULL) = (lease_expires_at IS NULL))
);
CREATE INDEX outbox_events_pending_idx ON outbox_events (available_at, seq) WHERE status IN ('pending', 'processing');

-- At-most-once acceptance of inbound messages (agent callbacks, KMS source events).
CREATE TABLE inbox_dedup (
    source       text NOT NULL,
    message_id   text NOT NULL,
    tenant_id    uuid REFERENCES tenants (id),
    payload_hash text NOT NULL CHECK (payload_hash ~ '^[0-9a-f]{64}$'),
    received_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, message_id)
);
