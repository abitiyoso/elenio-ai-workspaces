DROP TABLE IF EXISTS idempotency_keys;
ALTER TABLE workflow_versions DROP COLUMN IF EXISTS change_note;
DROP INDEX IF EXISTS workflow_versions_one_open_draft_uidx;
