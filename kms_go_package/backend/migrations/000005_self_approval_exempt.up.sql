-- Development walkthroughs may let one listed principal review its own submission (SELF_APPROVAL_EXEMPT_SUBJECTS,
-- refused outside APP_ENV=development). The database still refuses reviewer = submitter unless the decision row
-- says it was taken under that exemption.
ALTER TABLE review_decisions ADD COLUMN self_approval_exempt BOOLEAN NOT NULL DEFAULT false;
DO $$
DECLARE c text;
BEGIN
    FOR c IN SELECT conname FROM pg_constraint WHERE conrelid = 'review_decisions'::regclass AND contype = 'c'
        AND pg_get_constraintdef(oid) ILIKE '%reviewer_id <> submitter_id%' LOOP
        EXECUTE format('ALTER TABLE review_decisions DROP CONSTRAINT %I', c);
    END LOOP;
END $$;
-- NOT VALID still checks every insert; it only skips old rows, which matters after a down/up round trip
-- (exempt self-reviews then lose their flag and the append-only trigger forbids re-marking them).
ALTER TABLE review_decisions ADD CONSTRAINT review_decisions_independent_reviewer
    CHECK (reviewer_id <> submitter_id OR self_approval_exempt) NOT VALID;
