-- Phase 1 registry commands: one open draft per workflow, change notes, and idempotency keys for
-- create/command endpoints (claimed in the same transaction as the command they protect).

CREATE UNIQUE INDEX workflow_versions_one_open_draft_uidx
    ON workflow_versions (definition_id) WHERE status IN ('draft', 'validated');

ALTER TABLE workflow_versions ADD COLUMN change_note text NOT NULL DEFAULT '' CHECK (length(change_note) <= 2000);

CREATE TABLE idempotency_keys (
    tenant_id    uuid NOT NULL REFERENCES tenants (id),
    user_id      uuid NOT NULL REFERENCES users (id),
    scope        text NOT NULL CHECK (scope ~ '^[a-z][a-z0-9_.]{2,100}$'),
    key          text NOT NULL CHECK (key ~ '^[A-Za-z0-9._:-]{8,128}$'),
    request_hash text NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    resource_ref text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, user_id, scope, key)
);
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);
