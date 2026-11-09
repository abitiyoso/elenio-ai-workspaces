-- Recreates intake_sessions exactly as 000009_intake_sessions.up.sql does.
CREATE TABLE intake_sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       uuid NOT NULL,
    user_id         uuid NOT NULL,
    workflow_key    text NOT NULL CHECK (workflow_key ~ '^[a-z][a-z0-9-]{0,62}$'),
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'created', 'abandoned')),
    draft           jsonb NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(draft) = 'object'),
    -- [{role, content, at, sources?, updated_fields?, kb_items?, quick_replies?, unresolved?}]
    messages        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(messages) = 'array'),
    kb_status       text NOT NULL DEFAULT 'unknown' CHECK (kb_status IN ('covered', 'partial', 'not_covered', 'unknown')),
    kb_items        jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(kb_items) = 'array'),
    sources         jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(sources) = 'array'),
    ready           boolean NOT NULL DEFAULT false,
    missing         jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(missing) = 'array'),
    -- a turn in flight: pending_key is set before the run starts, pending_run_id once it started
    pending_key     text,
    pending_run_id  text,
    pending_agent   text,
    pending_since   timestamptz,
    turn            integer NOT NULL DEFAULT 0 CHECK (turn >= 0),
    last_error      text,
    case_id         uuid,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    version_num     integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES memberships (tenant_id, user_id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    CHECK ((status = 'created') = (case_id IS NOT NULL)),
    CHECK (pending_run_id IS NULL OR pending_key IS NOT NULL),
    CHECK ((pending_key IS NULL) = (pending_since IS NULL))
);
CREATE INDEX intake_sessions_user_idx ON intake_sessions (tenant_id, user_id, updated_at DESC);
