-- Author draft review: an agent step configured with author_review stops after its result becomes an
-- artifact version, and the case owner submits the draft onward or requests a revision with feedback.
-- A review never decides a gate; it only controls when the draft reaches the next step.
CREATE TABLE draft_reviews (
    id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid NOT NULL,
    case_id             uuid NOT NULL,
    node_execution_id   uuid NOT NULL UNIQUE,
    node_id             text NOT NULL,
    iteration           integer NOT NULL CHECK (iteration >= 1),
    artifact_id         uuid NOT NULL,
    artifact_version_id uuid NOT NULL,
    status              text NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'submitted', 'revision_requested', 'cancelled')),
    feedback            text CHECK (feedback IS NULL OR length(feedback) <= 1000),
    decided_by          uuid,
    decided_at          timestamptz,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    version_num         integer NOT NULL DEFAULT 1 CHECK (version_num >= 1),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, case_id) REFERENCES cases (tenant_id, id),
    FOREIGN KEY (tenant_id, node_execution_id) REFERENCES node_executions (tenant_id, id),
    FOREIGN KEY (tenant_id, artifact_id) REFERENCES artifacts (tenant_id, id),
    FOREIGN KEY (tenant_id, artifact_version_id) REFERENCES artifact_versions (tenant_id, id),
    FOREIGN KEY (tenant_id, decided_by) REFERENCES memberships (tenant_id, user_id),
    CHECK ((status IN ('submitted', 'revision_requested')) = (decided_by IS NOT NULL AND decided_at IS NOT NULL))
);
CREATE INDEX draft_reviews_inbox_idx ON draft_reviews (tenant_id, status, created_at DESC);
CREATE INDEX draft_reviews_case_idx ON draft_reviews (tenant_id, case_id);
