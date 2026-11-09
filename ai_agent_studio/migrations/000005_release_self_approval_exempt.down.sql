-- Restores the unconditional separation-of-duties checks. Fails while any
-- request was completed under the development exemption; those rows have to be
-- reviewed first, since the original constraints would reject them.
ALTER TABLE release_requests
    DROP CONSTRAINT IF EXISTS release_requests_independent_review,
    DROP CONSTRAINT IF EXISTS release_requests_independent_decision,
    ADD CHECK (reviewed_by IS NULL OR reviewed_by <> requested_by),
    ADD CHECK (decided_by IS NULL OR (decided_by <> requested_by AND decided_by IS DISTINCT FROM reviewed_by)),
    DROP COLUMN IF EXISTS self_approval_exempt;
