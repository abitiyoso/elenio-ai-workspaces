ALTER TABLE collection_memberships DROP CONSTRAINT IF EXISTS collection_memberships_membership_role_check;
ALTER TABLE collection_memberships ADD CONSTRAINT collection_memberships_membership_role_check
    CHECK (membership_role IN ('owner', 'steward', 'reviewer', 'contributor', 'reader'));
ALTER TABLE case_memberships DROP CONSTRAINT IF EXISTS case_memberships_membership_role_check;
ALTER TABLE case_memberships ADD CONSTRAINT case_memberships_membership_role_check
    CHECK (membership_role IN ('owner', 'steward', 'reviewer', 'contributor', 'reader'));

ALTER TABLE document_versions ADD COLUMN created_by UUID;
UPDATE document_versions v SET created_by=d.created_by FROM documents d
WHERE d.tenant_id=v.tenant_id AND d.id=v.document_id AND v.created_by IS NULL;
ALTER TABLE document_versions ALTER COLUMN created_by SET NOT NULL;
ALTER TABLE document_versions ADD COLUMN trust_status TEXT NOT NULL DEFAULT 'unverified'
    CHECK (trust_status IN ('approved', 'unverified', 'customer_source'));
ALTER TABLE document_versions ADD COLUMN publication_status TEXT NOT NULL DEFAULT 'draft'
    CHECK (publication_status IN ('draft', 'ready_for_review', 'published', 'expired', 'archived', 'revoked'));
ALTER TABLE document_versions ADD COLUMN submitted_by UUID;
ALTER TABLE document_versions ADD COLUMN submitted_at TIMESTAMPTZ;
ALTER TABLE document_versions ADD COLUMN published_at TIMESTAMPTZ;
ALTER TABLE document_versions ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE document_versions ADD COLUMN rejection_rationale TEXT;
ALTER TABLE document_versions ADD CONSTRAINT document_versions_review_status_check
    CHECK (review_status IN ('draft', 'pending', 'approved', 'rejected'));
CREATE INDEX document_versions_expiry_idx ON document_versions(expires_at) WHERE expires_at IS NOT NULL;
UPDATE document_versions v SET trust_status=ks.trust_status
FROM documents d JOIN knowledge_sources ks ON ks.tenant_id=d.tenant_id AND ks.document_id=d.id
WHERE d.tenant_id=v.tenant_id AND d.id=v.document_id;

ALTER TABLE documents ADD COLUMN current_version_id UUID;
ALTER TABLE documents ADD COLUMN published_version_id UUID;
ALTER TABLE documents ADD COLUMN revoked_at TIMESTAMPTZ;
ALTER TABLE documents ADD COLUMN deleted_at TIMESTAMPTZ;
UPDATE documents d SET current_version_id=(
    SELECT v.id FROM document_versions v
    WHERE v.tenant_id=d.tenant_id AND v.document_id=d.id
    ORDER BY v.version_number DESC LIMIT 1
) WHERE d.current_version_id IS NULL;
-- Preserve already published, approved Phase 2 evidence while leaving drafts unpublished.
UPDATE document_versions v SET publication_status='published',published_at=COALESCE(v.valid_from,v.created_at),valid_from=COALESCE(v.valid_from,v.created_at)
FROM documents d WHERE d.tenant_id=v.tenant_id AND d.id=v.document_id AND d.lifecycle_status='published'
  AND v.review_status='approved' AND v.trust_status='approved'
  AND v.id=(SELECT latest.id FROM document_versions latest WHERE latest.tenant_id=d.tenant_id AND latest.document_id=d.id
      AND latest.review_status='approved' AND latest.trust_status='approved' ORDER BY latest.version_number DESC LIMIT 1);
UPDATE documents d SET published_version_id=(
    SELECT v.id FROM document_versions v WHERE v.tenant_id=d.tenant_id AND v.document_id=d.id AND v.publication_status='published'
    ORDER BY v.version_number DESC LIMIT 1
)
WHERE d.lifecycle_status='published' AND EXISTS(
    SELECT 1 FROM document_versions v WHERE v.tenant_id=d.tenant_id AND v.document_id=d.id AND v.publication_status='published'
);
ALTER TABLE documents ADD CONSTRAINT documents_current_version_fk
    FOREIGN KEY (tenant_id, current_version_id) REFERENCES document_versions(tenant_id, id);
ALTER TABLE documents ADD CONSTRAINT documents_published_version_fk
    FOREIGN KEY (tenant_id, published_version_id) REFERENCES document_versions(tenant_id, id);

CREATE TABLE document_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_id UUID NOT NULL,
    submitted_by UUID NOT NULL,
    submission_note TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'withdrawn')),
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_versions(tenant_id, id)
);
CREATE UNIQUE INDEX document_reviews_one_pending_per_version_idx
    ON document_reviews(tenant_id, version_id) WHERE status='pending';
CREATE INDEX document_reviews_queue_idx ON document_reviews(tenant_id, status, submitted_at DESC);

CREATE TABLE review_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    review_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_id UUID NOT NULL,
    submitter_id UUID NOT NULL,
    reviewer_id UUID NOT NULL,
    decision TEXT NOT NULL CHECK (decision IN ('approved', 'rejected')),
    rationale TEXT NOT NULL CHECK (length(btrim(rationale)) > 0),
    decided_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (reviewer_id <> submitter_id),
    FOREIGN KEY (tenant_id, review_id) REFERENCES document_reviews(tenant_id, id),
    FOREIGN KEY (tenant_id, document_id) REFERENCES documents(tenant_id, id),
    FOREIGN KEY (tenant_id, version_id) REFERENCES document_versions(tenant_id, id)
);
CREATE UNIQUE INDEX review_decisions_one_per_review_idx ON review_decisions(tenant_id, review_id);

CREATE TABLE publication_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    document_id UUID NOT NULL,
    version_id UUID NOT NULL,
    actor_id UUID NOT NULL,
    event_type TEXT NOT NULL CHECK (event_type IN ('published', 'revoked', 'expired', 'archived')),
    rationale TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX publication_events_resource_idx ON publication_events(tenant_id, document_id, created_at DESC);

CREATE TABLE audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'service', 'system')),
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    collection_id UUID,
    case_id UUID,
    version_id UUID,
    outcome TEXT NOT NULL CHECK (outcome IN ('allowed', 'denied', 'failed')),
    rationale TEXT NOT NULL DEFAULT '',
    correlation_id TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_tenant_time_idx ON audit_events(tenant_id, created_at DESC);
CREATE INDEX audit_events_resource_idx ON audit_events(tenant_id, resource_type, resource_id, created_at DESC);

CREATE TABLE outbox_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    event_type TEXT NOT NULL,
    aggregate_id UUID NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'processing', 'done', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 8 CHECK (max_attempts > 0),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_expires_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX outbox_claim_idx ON outbox_events(status, available_at, lease_expires_at, created_at);

CREATE TABLE service_clients (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    name TEXT NOT NULL,
    token_hash CHAR(64) NOT NULL UNIQUE,
    audience TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT ARRAY['retrieval:query']::text[],
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    CHECK (length(btrim(name)) > 0),
    CHECK (expires_at > created_at)
);
CREATE TABLE service_client_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    client_id UUID NOT NULL,
    collection_id UUID NOT NULL,
    case_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, client_id) REFERENCES service_clients(tenant_id, id) ON DELETE CASCADE,
    FOREIGN KEY (tenant_id, collection_id) REFERENCES collections(tenant_id, id),
    FOREIGN KEY (tenant_id, case_id, collection_id) REFERENCES case_scopes(tenant_id, case_id, collection_id)
);
CREATE UNIQUE INDEX service_client_collection_grant_idx ON service_client_grants(tenant_id, client_id, collection_id) WHERE case_id IS NULL;
CREATE UNIQUE INDEX service_client_case_grant_idx ON service_client_grants(tenant_id, client_id, case_id) WHERE case_id IS NOT NULL;
CREATE INDEX service_client_token_idx ON service_clients(token_hash) WHERE revoked_at IS NULL;

CREATE OR REPLACE FUNCTION kms_reject_immutable_mutation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE='55000';
END;
$$;
CREATE TRIGGER audit_events_append_only BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION kms_reject_immutable_mutation();
CREATE TRIGGER review_decisions_append_only BEFORE UPDATE OR DELETE ON review_decisions
    FOR EACH ROW EXECUTE FUNCTION kms_reject_immutable_mutation();
CREATE TRIGGER publication_events_append_only BEFORE UPDATE OR DELETE ON publication_events
    FOR EACH ROW EXECUTE FUNCTION kms_reject_immutable_mutation();
