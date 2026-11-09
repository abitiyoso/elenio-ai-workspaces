-- Agent Studio runtime and governance (Phases 2-4): runs, trace, evidence,
-- outbox, evaluations, releases and feedback. Tenant data stays under RLS;
-- the only cross-tenant table is work_queue, which holds ids and leases only.

-- Platform-global tool catalog. Versions are published and immutable; tenants
-- bind them to draft agent versions.
INSERT INTO tools (tool_key, kind, owner, risk_level, lifecycle_status) VALUES
    ('kms.search', 'knowledge_retrieval', 'platform', 'medium', 'active'),
    ('math.calculate', 'deterministic_utility', 'platform', 'low', 'active')
ON CONFLICT (tool_key) DO NOTHING;

INSERT INTO tool_versions (tool_id, semver, adapter_ref, input_schema_json, output_schema_json, policy_json, checksum, published_at)
SELECT t.id, '1.0.0', v.adapter_ref, v.input_schema::jsonb, v.output_schema::jsonb, v.policy::jsonb,
       encode(sha256(convert_to(v.adapter_ref || v.input_schema || v.output_schema || v.policy, 'UTF8')), 'hex'), now()
FROM tools t
JOIN (VALUES
    ('kms.search', 'kms.retrieval.v1',
     '{"type":"object","additionalProperties":false,"required":["query"],"properties":{"query":{"type":"string","minLength":1,"maxLength":500},"top_k":{"type":"integer","minimum":1,"maximum":10}}}',
     '{"type":"object","required":["results"],"properties":{"results":{"type":"array"}}}',
     '{"side_effect":"none","max_calls":5,"timeout_ms":15000,"sensitivity":"tenant_confidential","sandbox":"fixtures_only"}'),
    ('math.calculate', 'builtin.math.v1',
     '{"type":"object","additionalProperties":false,"required":["expression"],"properties":{"expression":{"type":"string","minLength":1,"maxLength":200}}}',
     '{"type":"object","required":["value"],"properties":{"value":{"type":"number"}}}',
     '{"side_effect":"none","max_calls":20,"timeout_ms":1000,"sensitivity":"none","sandbox":"allowed"}')
) AS v(tool_key, adapter_ref, input_schema, output_schema, policy) ON v.tool_key = t.tool_key
ON CONFLICT (tool_id, semver) DO NOTHING;

CREATE TABLE agent_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    run_kind text NOT NULL CHECK (run_kind IN ('production', 'sandbox', 'evaluation')),
    caller_kind text NOT NULL CHECK (caller_kind IN ('user', 'service')),
    caller_subject text NOT NULL CHECK (length(caller_subject) BETWEEN 1 AND 320),
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9._:-]{8,128}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    external_correlation_id text CHECK (external_correlation_id IS NULL OR length(external_correlation_id) <= 128),
    case_ref text CHECK (case_ref IS NULL OR length(case_ref) <= 128),
    input_json jsonb NOT NULL CHECK (jsonb_typeof(input_json) = 'object'),
    evidence_input_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(evidence_input_json) = 'array'),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'needs_review', 'failed', 'cancelled')),
    model_mode text NOT NULL CHECK (model_mode IN ('provider', 'dev_mock')),
    pinned_json jsonb NOT NULL CHECK (jsonb_typeof(pinned_json) = 'object'),
    output_json jsonb,
    output_checksum text CHECK (output_checksum IS NULL OR output_checksum ~ '^[0-9a-f]{64}$'),
    validation_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    error_code text CHECK (error_code IS NULL OR error_code ~ '^[a-z][a-z0-9_]{0,63}$'),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts integer NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
    deadline_at timestamptz NOT NULL,
    cancel_requested boolean NOT NULL DEFAULT false,
    tokens_in integer NOT NULL DEFAULT 0,
    tokens_out integer NOT NULL DEFAULT 0,
    tool_calls integer NOT NULL DEFAULT 0,
    eval_run_id uuid,
    eval_case_id uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    started_at timestamptz,
    completed_at timestamptz,
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, caller_subject, idempotency_key),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id),
    CHECK (status NOT IN ('succeeded', 'needs_review', 'failed', 'cancelled') OR completed_at IS NOT NULL)
);
CREATE INDEX agent_runs_tenant_created_idx ON agent_runs (tenant_id, created_at DESC);
CREATE INDEX agent_runs_eval_idx ON agent_runs (tenant_id, eval_run_id) WHERE eval_run_id IS NOT NULL;

CREATE TABLE agent_run_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    seq integer NOT NULL CHECK (seq >= 1),
    event_type text NOT NULL CHECK (event_type ~ '^[a-z][a-z0-9_.]{0,63}$'),
    payload_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(payload_json) = 'object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, seq),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_runs (tenant_id, id)
);
CREATE INDEX agent_run_events_retention_idx ON agent_run_events (tenant_id, created_at);

-- Trace rows are append-only except for the transaction-local retention purge.
CREATE FUNCTION agent_studio_run_events_append_only() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' AND current_setting('agent_studio.retention_purge', true) = 'on' THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'run events are append-only' USING ERRCODE = 'AS001';
END;
$$;
CREATE TRIGGER agent_run_events_append_only BEFORE UPDATE OR DELETE ON agent_run_events
    FOR EACH ROW EXECUTE FUNCTION agent_studio_run_events_append_only();

CREATE TABLE agent_evidence_refs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    origin text NOT NULL CHECK (origin IN ('kms', 'fixture', 'flowcase')),
    source_id text NOT NULL CHECK (length(source_id) BETWEEN 1 AND 200),
    source_version text NOT NULL DEFAULT '',
    title text NOT NULL DEFAULT '',
    citation text NOT NULL DEFAULT '',
    anchor text NOT NULL DEFAULT '',
    trust_status text NOT NULL CHECK (trust_status IN ('approved', 'unverified', 'customer_source', 'expired', 'conflicted')),
    publication_status text NOT NULL DEFAULT '',
    cited boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (run_id, origin, source_id, source_version, anchor),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_runs (tenant_id, id)
);

CREATE TABLE outbox_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    run_id uuid NOT NULL,
    topic text NOT NULL CHECK (topic IN ('flowcase.run_result')),
    message_id text NOT NULL UNIQUE,
    payload_json jsonb NOT NULL CHECK (jsonb_typeof(payload_json) = 'object'),
    status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'dead')),
    attempts integer NOT NULL DEFAULT 0,
    last_error text CHECK (last_error IS NULL OR last_error ~ '^[a-z][a-z0-9_]{0,63}$'),
    created_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_runs (tenant_id, id)
);

-- Cross-tenant scheduling only: no payloads, prompts, outputs, or names.
CREATE TABLE work_queue (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('run', 'outbox')),
    tenant_id uuid NOT NULL,
    ref_id uuid NOT NULL,
    available_at timestamptz NOT NULL DEFAULT now(),
    lease_owner text,
    lease_expires_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (kind, ref_id)
);
CREATE INDEX work_queue_ready_idx ON work_queue (kind, available_at);

CREATE TABLE eval_suites (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    suite_key text NOT NULL CHECK (suite_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    description text NOT NULL DEFAULT '' CHECK (length(description) <= 2000),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, agent_id, suite_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id)
);

CREATE TABLE eval_cases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    suite_id uuid NOT NULL,
    case_key text NOT NULL CHECK (case_key ~ '^[a-z][a-z0-9_.-]{0,62}$'),
    classification text NOT NULL CHECK (classification IN ('positive', 'negative', 'adversarial', 'evidence_conflict', 'missing_evidence')),
    critical boolean NOT NULL DEFAULT false,
    input_json jsonb NOT NULL CHECK (jsonb_typeof(input_json) = 'object'),
    evidence_fixture_json jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(evidence_fixture_json) = 'array'),
    expectations_json jsonb NOT NULL DEFAULT '{}'::jsonb CHECK (jsonb_typeof(expectations_json) = 'object'),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, suite_id, case_key),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, suite_id) REFERENCES eval_suites (tenant_id, id)
);

CREATE TABLE eval_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    suite_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    version_fingerprint text NOT NULL CHECK (version_fingerprint ~ '^[0-9a-f]{64}$'),
    baseline_eval_run_id uuid,
    status text NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    summary_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, suite_id) REFERENCES eval_suites (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id)
);

CREATE TABLE eval_results (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    eval_run_id uuid NOT NULL,
    case_id uuid NOT NULL,
    run_id uuid NOT NULL,
    passed boolean,
    critical boolean NOT NULL,
    checks_json jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_at timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    UNIQUE (eval_run_id, case_id),
    FOREIGN KEY (tenant_id, eval_run_id) REFERENCES eval_runs (tenant_id, id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES eval_cases (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_runs (tenant_id, id)
);

CREATE TABLE release_requests (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    eval_run_id uuid NOT NULL,
    version_fingerprint text NOT NULL CHECK (version_fingerprint ~ '^[0-9a-f]{64}$'),
    environment text NOT NULL CHECK (environment IN ('staging', 'production')),
    status text NOT NULL DEFAULT 'pending_review' CHECK (status IN ('pending_review', 'pending_ops', 'rejected', 'published', 'withdrawn')),
    requested_by text NOT NULL,
    request_note text NOT NULL DEFAULT '' CHECK (length(request_note) <= 2000),
    reviewed_by text,
    review_reason text,
    reviewed_at timestamptz,
    decided_by text,
    decision_reason text,
    decided_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id),
    FOREIGN KEY (tenant_id, eval_run_id) REFERENCES eval_runs (tenant_id, id),
    CHECK (reviewed_by IS NULL OR reviewed_by <> requested_by),
    CHECK (decided_by IS NULL OR (decided_by <> requested_by AND decided_by IS DISTINCT FROM reviewed_by))
);
CREATE UNIQUE INDEX release_requests_one_open_idx ON release_requests (tenant_id, agent_version_id, environment)
    WHERE status IN ('pending_review', 'pending_ops');

CREATE TABLE releases (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    agent_version_id uuid NOT NULL,
    environment text NOT NULL CHECK (environment IN ('staging', 'production')),
    release_request_id uuid,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'rolled_back')),
    rolled_back_from uuid,
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    ended_at timestamptz,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id),
    FOREIGN KEY (tenant_id, agent_version_id) REFERENCES agent_versions (tenant_id, id)
);
CREATE UNIQUE INDEX releases_one_active_idx ON releases (tenant_id, agent_id, environment) WHERE status = 'active';

CREATE TABLE feedback_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    agent_id uuid NOT NULL,
    run_id uuid,
    classification text NOT NULL CHECK (classification IN ('factual_error', 'missing_evidence', 'format', 'policy', 'style', 'other')),
    note text NOT NULL CHECK (length(note) BETWEEN 1 AND 4000),
    reusable_candidate boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'triaged', 'dismissed')),
    created_by text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, agent_id) REFERENCES agents (tenant_id, id),
    FOREIGN KEY (tenant_id, run_id) REFERENCES agent_runs (tenant_id, id)
);

-- Releases and approvals are history: no deletes, and only lifecycle columns move.
CREATE FUNCTION agent_studio_governance_history() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION '% rows cannot be deleted', TG_TABLE_NAME USING ERRCODE = 'AS001';
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.agent_version_id IS DISTINCT FROM OLD.agent_version_id
       OR NEW.created_by IS DISTINCT FROM OLD.created_by THEN
        RAISE EXCEPTION '% identity columns are immutable', TG_TABLE_NAME USING ERRCODE = 'AS001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE FUNCTION agent_studio_release_request_history() RETURNS trigger
    LANGUAGE plpgsql AS
$$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'release requests cannot be deleted' USING ERRCODE = 'AS001';
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.agent_version_id IS DISTINCT FROM OLD.agent_version_id
       OR NEW.requested_by IS DISTINCT FROM OLD.requested_by OR NEW.eval_run_id IS DISTINCT FROM OLD.eval_run_id
       OR NEW.version_fingerprint IS DISTINCT FROM OLD.version_fingerprint
       OR (OLD.reviewed_by IS NOT NULL AND NEW.reviewed_by IS DISTINCT FROM OLD.reviewed_by)
       OR (OLD.decided_by IS NOT NULL AND NEW.decided_by IS DISTINCT FROM OLD.decided_by)
       OR OLD.status IN ('rejected', 'published', 'withdrawn') THEN
        RAISE EXCEPTION 'release request history is immutable' USING ERRCODE = 'AS001';
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER releases_history BEFORE UPDATE OR DELETE ON releases
    FOR EACH ROW EXECUTE FUNCTION agent_studio_governance_history();
CREATE TRIGGER release_requests_history BEFORE UPDATE OR DELETE ON release_requests
    FOR EACH ROW EXECUTE FUNCTION agent_studio_release_request_history();

DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'agent_runs', 'agent_run_events', 'agent_evidence_refs', 'outbox_events',
        'eval_suites', 'eval_cases', 'eval_runs', 'eval_results',
        'release_requests', 'releases', 'feedback_items'
    ] LOOP
        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', table_name);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', table_name);
        EXECUTE format('CREATE POLICY tenant_scope ON %I USING (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = nullif(current_setting(''app.tenant_id'', true), '''')::uuid)', table_name);
    END LOOP;
END;
$$;
