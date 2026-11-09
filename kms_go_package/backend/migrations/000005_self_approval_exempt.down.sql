ALTER TABLE review_decisions DROP CONSTRAINT review_decisions_independent_reviewer;
-- NOT VALID: exempt self-reviews recorded while the exemption was on stay in the history.
ALTER TABLE review_decisions ADD CONSTRAINT review_decisions_check CHECK (reviewer_id <> submitter_id) NOT VALID;
ALTER TABLE review_decisions DROP COLUMN self_approval_exempt;
