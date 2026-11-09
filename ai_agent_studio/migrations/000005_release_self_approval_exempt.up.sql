-- Development walkthroughs may exempt listed people from the release
-- separation-of-duties rule (AGENT_STUDIO_SELF_APPROVAL_EXEMPT_SUBJECTS, refused
-- outside AGENT_STUDIO_ENV=development). The flag is set on the request when an
-- exempt person reviews or decides a request they requested or reviewed, so the
-- database still rejects every other same-person step and history shows which
-- releases skipped independent review.

ALTER TABLE release_requests ADD COLUMN self_approval_exempt boolean NOT NULL DEFAULT false;

DO $$
DECLARE c text;
BEGIN
    FOR c IN SELECT conname FROM pg_constraint
             WHERE conrelid = 'release_requests'::regclass AND contype = 'c'
               AND pg_get_constraintdef(oid) LIKE '%requested_by%'
               AND (pg_get_constraintdef(oid) LIKE '%reviewed_by%' OR pg_get_constraintdef(oid) LIKE '%decided_by%')
    LOOP
        EXECUTE format('ALTER TABLE release_requests DROP CONSTRAINT %I', c);
    END LOOP;
END;
$$;

ALTER TABLE release_requests
    ADD CONSTRAINT release_requests_independent_review
        CHECK (self_approval_exempt OR reviewed_by IS NULL OR reviewed_by <> requested_by),
    ADD CONSTRAINT release_requests_independent_decision
        CHECK (self_approval_exempt OR decided_by IS NULL OR (decided_by <> requested_by AND decided_by IS DISTINCT FROM reviewed_by));
