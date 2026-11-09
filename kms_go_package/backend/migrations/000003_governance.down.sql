DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM audit_events) OR EXISTS (SELECT 1 FROM review_decisions)
       OR EXISTS (SELECT 1 FROM publication_events) OR EXISTS (SELECT 1 FROM outbox_events)
       OR EXISTS (SELECT 1 FROM service_clients) OR EXISTS (SELECT 1 FROM document_reviews) THEN
        RAISE EXCEPTION 'refusing to remove Phase 3 governance data';
    END IF;
END
$$;

DROP TRIGGER IF EXISTS publication_events_append_only ON publication_events;
DROP TRIGGER IF EXISTS review_decisions_append_only ON review_decisions;
DROP TRIGGER IF EXISTS audit_events_append_only ON audit_events;
DROP FUNCTION IF EXISTS kms_reject_immutable_mutation();
DROP TABLE service_client_grants;
DROP TABLE service_clients;
DROP TABLE outbox_events;
DROP TABLE audit_events;
DROP TABLE publication_events;
DROP TABLE review_decisions;
DROP TABLE document_reviews;
ALTER TABLE documents DROP CONSTRAINT documents_published_version_fk;
ALTER TABLE documents DROP CONSTRAINT documents_current_version_fk;
ALTER TABLE documents DROP COLUMN deleted_at;
ALTER TABLE documents DROP COLUMN revoked_at;
ALTER TABLE documents DROP COLUMN published_version_id;
ALTER TABLE documents DROP COLUMN current_version_id;
ALTER TABLE document_versions DROP CONSTRAINT document_versions_review_status_check;
ALTER TABLE document_versions DROP COLUMN rejection_rationale;
ALTER TABLE document_versions DROP COLUMN revoked_at;
ALTER TABLE document_versions DROP COLUMN published_at;
ALTER TABLE document_versions DROP COLUMN submitted_at;
ALTER TABLE document_versions DROP COLUMN submitted_by;
ALTER TABLE document_versions DROP COLUMN publication_status;
ALTER TABLE document_versions DROP COLUMN trust_status;
ALTER TABLE document_versions DROP COLUMN created_by;
ALTER TABLE case_memberships DROP CONSTRAINT case_memberships_membership_role_check;
ALTER TABLE case_memberships ADD CONSTRAINT case_memberships_membership_role_check
    CHECK (membership_role IN ('owner', 'steward', 'contributor', 'reader'));
ALTER TABLE collection_memberships DROP CONSTRAINT collection_memberships_membership_role_check;
ALTER TABLE collection_memberships ADD CONSTRAINT collection_memberships_membership_role_check
    CHECK (membership_role IN ('owner', 'steward', 'contributor', 'reader'));
