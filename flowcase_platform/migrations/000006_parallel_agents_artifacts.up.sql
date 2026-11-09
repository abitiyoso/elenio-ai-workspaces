-- Phase 3: branch tokens per split execution, agent runs, connector invocations, timers,
-- immutable artifact versions with dependencies and evidence, and approval re-review.

-- A split re-run after rework creates fresh branch tokens, so identity is the split execution.
ALTER TABLE execution_tokens ADD COLUMN split_execution_id uuid;
ALTER TABLE execution_tokens DROP CONSTRAINT execution_tokens_case_id_parent_token_id_split_node_id_bran_key;
CREATE UNIQUE INDEX execution_tokens_branch_uidx ON execution_tokens (split_execution_id, branch_key) WHERE split_execution_id IS NOT NULL;

CREATE TABLE agent_runs (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    case_id           uuid NOT NULL,
    node_execution_id uuid NOT NULL,
    attempt           integer NOT NULL,
    run_key           text NOT NULL UNIQUE, -- idempotency key sent to the runtime
    provider          text NOT NULL,
    run_id            text NOT NULL,
    agent_ref         text NOT NULL,
    skill_version     text NOT NULL,
    status            text NOT NULL DEFAULT 'started'
        CHECK (status IN ('started', 'succeeded', 'failed', 'cancelled', 'timed_out', 'superseded')),
    input_hash        text NOT NULL CHECK (input_hash ~ '^[0-9a-f]{64}$'),
    evidence          jsonb NOT NULL DEFAULT '[]',
    output_checksum   text,
    trace_ids         jsonb NOT NULL DEFAULT '[]',
    polls             integer NOT NULL DEFAULT 0,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    UNIQUE (provider, run_id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, node_execution_id) REFERENCES node_executions (tenant_id, id)
);
CREATE INDEX agent_runs_exec_idx ON agent_runs (node_execution_id, attempt);

CREATE TABLE tool_invocations (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    case_id           uuid NOT NULL,
    node_execution_id uuid NOT NULL,
    connector_ref     text NOT NULL,
    operation         text NOT NULL,
    idempotency_key   text NOT NULL UNIQUE,
    params_redacted   jsonb NOT NULL DEFAULT '{}',
    output            jsonb,
    created_at        timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, node_execution_id) REFERENCES node_executions (tenant_id, id)
);

CREATE TABLE artifacts (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid NOT NULL,
    case_id          uuid NOT NULL,
    artifact_type    text NOT NULL,
    producer_node_id text NOT NULL,
    current_version  integer NOT NULL DEFAULT 0,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    version_num      integer NOT NULL DEFAULT 1,
    UNIQUE (tenant_id, id),
    UNIQUE (case_id, artifact_type),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id)
);

CREATE TABLE artifact_versions (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    artifact_id       uuid NOT NULL,
    version           integer NOT NULL CHECK (version >= 1),
    content           jsonb NOT NULL,
    checksum          text NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
    status            text NOT NULL DEFAULT 'ready_for_review'
        CHECK (status IN ('draft', 'ready_for_review', 'approved', 'stale', 'superseded')),
    produced_by_kind  text NOT NULL CHECK (produced_by_kind IN ('agent', 'human', 'revision')),
    produced_by_exec  uuid,
    produced_by_user  uuid,
    agent_run_id      uuid REFERENCES agent_runs (id),
    change_note       text NOT NULL DEFAULT '',
    stale_reason      text,
    created_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (artifact_id, version),
    FOREIGN KEY (tenant_id, artifact_id) REFERENCES artifacts (tenant_id, id)
);

CREATE FUNCTION flowcase_guard_artifact_version() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' OR NEW.content IS DISTINCT FROM OLD.content OR NEW.checksum IS DISTINCT FROM OLD.checksum
        OR NEW.artifact_id IS DISTINCT FROM OLD.artifact_id OR NEW.version IS DISTINCT FROM OLD.version THEN
        RAISE EXCEPTION 'artifact versions are immutable' USING ERRCODE = 'FC001';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER artifact_versions_immutable
    BEFORE UPDATE OR DELETE ON artifact_versions
    FOR EACH ROW EXECUTE FUNCTION flowcase_guard_artifact_version();

CREATE TABLE artifact_dependencies (
    artifact_version_id   uuid NOT NULL REFERENCES artifact_versions (id),
    depends_on_version_id uuid NOT NULL REFERENCES artifact_versions (id),
    PRIMARY KEY (artifact_version_id, depends_on_version_id)
);

CREATE TABLE artifact_evidence_refs (
    artifact_version_id uuid NOT NULL REFERENCES artifact_versions (id),
    source_id           text NOT NULL,
    source_version_id   text NOT NULL,
    title               text NOT NULL DEFAULT '',
    classification      text NOT NULL DEFAULT '',
    status              text NOT NULL DEFAULT 'valid' CHECK (status IN ('valid', 'stale', 'forbidden')),
    checked_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (artifact_version_id, source_id)
);
CREATE INDEX artifact_evidence_source_idx ON artifact_evidence_refs (source_id) WHERE status = 'valid';

ALTER TABLE approval_requests DROP CONSTRAINT approval_requests_status_check;
ALTER TABLE approval_requests ADD CONSTRAINT approval_requests_status_check
    CHECK (status IN ('pending', 'approved', 'rejected', 'returned', 'cancelled', 'invalidated'));
ALTER TABLE approval_requests ADD COLUMN kind text NOT NULL DEFAULT 'gate' CHECK (kind IN ('gate', 'rereview'));
ALTER TABLE approval_requests ADD COLUMN supersedes_approval_id uuid REFERENCES approval_requests (id);
ALTER TABLE approval_requests ADD COLUMN invalidated_reason text;

-- The exact artifact versions a decision covers.
CREATE TABLE approval_artifacts (
    approval_id         uuid NOT NULL REFERENCES approval_requests (id),
    artifact_version_id uuid NOT NULL REFERENCES artifact_versions (id),
    PRIMARY KEY (approval_id, artifact_version_id)
);
