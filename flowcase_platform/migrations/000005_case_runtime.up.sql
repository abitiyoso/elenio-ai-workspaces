-- Phase 2 case runtime: tasks, approvals with immutable decisions, condition evaluation log,
-- configurable domain roles and approval delegations, and per-tenant case references.

-- Domain (pack) roles such as poc-owner are business assignments, not platform roles.
CREATE TABLE domain_role_assignments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL,
    user_id    uuid NOT NULL,
    role       text NOT NULL CHECK (role ~ '^[a-z][a-z0-9]*([.-][a-z0-9]+){0,10}$'),
    case_id    uuid,
    granted_by uuid REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now(),
    revoked_at timestamptz,
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships (tenant_id, user_id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id)
);
CREATE UNIQUE INDEX domain_role_assignments_active_uidx
    ON domain_role_assignments (tenant_id, user_id, role, coalesce(case_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE revoked_at IS NULL;

-- Explicit authority to decide approvals bound to a policy. Never derived from sample data.
CREATE TABLE policy_delegations (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   uuid NOT NULL,
    policy_ref  text NOT NULL CHECK (policy_ref ~ '^[a-z][a-z0-9]*([.-][a-z0-9]+){0,10}$'),
    user_id     uuid NOT NULL,
    granted_by  uuid REFERENCES users (id),
    valid_from  timestamptz NOT NULL DEFAULT now(),
    valid_until timestamptz,
    revoked_at  timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships (tenant_id, user_id),
    CHECK (valid_until IS NULL OR valid_until > valid_from)
);
CREATE UNIQUE INDEX policy_delegations_active_uidx ON policy_delegations (tenant_id, policy_ref, user_id) WHERE revoked_at IS NULL;

CREATE TABLE case_counters (
    tenant_id uuid NOT NULL REFERENCES tenants (id),
    year      integer NOT NULL,
    last      integer NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, year)
);

CREATE TABLE tasks (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    case_id           uuid NOT NULL,
    node_execution_id uuid NOT NULL UNIQUE,
    node_id           text NOT NULL,
    iteration         integer NOT NULL CHECK (iteration >= 1),
    title             text NOT NULL,
    assignee_role     text NOT NULL,
    instructions      text NOT NULL DEFAULT '',
    form_schema       jsonb,
    revision_note     text,
    status            text NOT NULL DEFAULT 'assigned' CHECK (status IN ('assigned', 'submitted', 'revision_requested', 'cancelled')),
    output            jsonb,
    completed_by      uuid,
    due_at            timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    version_num       integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, node_execution_id) REFERENCES node_executions (tenant_id, id),
    FOREIGN KEY (tenant_id, completed_by) REFERENCES memberships (tenant_id, user_id),
    CHECK ((status = 'submitted') = (completed_by IS NOT NULL AND completed_at IS NOT NULL) OR status = 'revision_requested')
);
CREATE INDEX tasks_inbox_idx ON tasks (tenant_id, status, assignee_role, created_at DESC);
CREATE INDEX tasks_case_idx ON tasks (tenant_id, case_id);

CREATE TABLE task_comments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  uuid NOT NULL,
    task_id    uuid NOT NULL,
    author_id  uuid NOT NULL,
    body       text NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, task_id) REFERENCES tasks (tenant_id, id),
    FOREIGN KEY (tenant_id, author_id) REFERENCES memberships (tenant_id, user_id)
);
CREATE INDEX task_comments_task_idx ON task_comments (task_id, created_at);

CREATE TABLE approval_requests (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    case_id           uuid NOT NULL,
    node_execution_id uuid NOT NULL UNIQUE,
    node_id           text NOT NULL,
    iteration         integer NOT NULL CHECK (iteration >= 1),
    title             text NOT NULL,
    policy_ref        text NOT NULL,
    return_to         text,
    -- SHA-256 of the case state the decision covers (definition hash + variables + gate + iteration)
    subject_hash      text NOT NULL CHECK (subject_hash ~ '^[0-9a-f]{64}$'),
    subject           jsonb NOT NULL,
    status            text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'returned', 'cancelled')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    decided_at        timestamptz,
    version_num       integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, node_execution_id) REFERENCES node_executions (tenant_id, id)
);
CREATE INDEX approval_requests_inbox_idx ON approval_requests (tenant_id, status, policy_ref, created_at DESC);

CREATE TABLE approval_decisions (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     uuid NOT NULL,
    approval_id   uuid NOT NULL UNIQUE,
    decided_by    uuid NOT NULL,
    decision      text NOT NULL CHECK (decision IN ('approve', 'reject', 'return')),
    rationale     text NOT NULL CHECK (length(rationale) BETWEEN 5 AND 4000),
    subject_hash  text NOT NULL CHECK (subject_hash ~ '^[0-9a-f]{64}$'),
    authority     jsonb NOT NULL,
    decision_hash text NOT NULL CHECK (decision_hash ~ '^[0-9a-f]{64}$'),
    decided_at    timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, approval_id) REFERENCES approval_requests (tenant_id, id),
    FOREIGN KEY (tenant_id, decided_by) REFERENCES memberships (tenant_id, user_id)
);

CREATE FUNCTION flowcase_append_only() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'FC001';
END;
$$;

CREATE TRIGGER approval_decisions_append_only
    BEFORE UPDATE OR DELETE ON approval_decisions
    FOR EACH ROW EXECUTE FUNCTION flowcase_append_only();

-- Defence in depth: AI agents and other service principals can never record an approval decision.
CREATE FUNCTION flowcase_human_decider() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM users WHERE id = NEW.decided_by AND kind = 'human' AND status = 'active') THEN
        RAISE EXCEPTION 'approval decisions require an active human principal' USING ERRCODE = 'FC003';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER approval_decisions_human_only
    BEFORE INSERT ON approval_decisions
    FOR EACH ROW EXECUTE FUNCTION flowcase_human_decider();

CREATE TABLE condition_evaluations (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         uuid NOT NULL,
    case_id           uuid NOT NULL,
    node_execution_id uuid NOT NULL UNIQUE,
    node_id           text NOT NULL,
    expression        jsonb NOT NULL,
    inputs            jsonb NOT NULL,
    result            boolean NOT NULL,
    target            text NOT NULL,
    evaluated_at      timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, node_execution_id) REFERENCES node_executions (tenant_id, id)
);

CREATE TRIGGER condition_evaluations_append_only
    BEFORE UPDATE OR DELETE ON condition_evaluations
    FOR EACH ROW EXECUTE FUNCTION flowcase_append_only();

-- Successor lookups for rework (find the execution scheduled after a given one).
ALTER TABLE node_executions ADD COLUMN predecessor_id uuid;
CREATE INDEX node_executions_predecessor_idx ON node_executions (predecessor_id) WHERE predecessor_id IS NOT NULL;
